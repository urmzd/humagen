package platform

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/alerts"
	"github.com/urmzd/humagen/pkg/artifacts"
	"github.com/urmzd/humagen/pkg/blob"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/policy"
	"github.com/urmzd/humagen/pkg/state"
)

// Keep A's first saved-identity read stale until B has committed its save.
// Each replica has its own publication mutex, as separate processes do.
type savedArtifactLookupBarrier struct {
	state.Store
	armed   atomic.Bool
	missing chan struct{}
	release chan struct{}
}

func (s *savedArtifactLookupBarrier) Lookup(ctx context.Context, namespace, tenant, key string) (state.Record, error) {
	rec, err := s.Store.Lookup(ctx, namespace, tenant, key)
	if namespace == "artifacts" && strings.HasPrefix(key, "saved/") && errors.Is(err, state.ErrNotFound) && s.armed.CompareAndSwap(true, false) {
		close(s.missing)
		select {
		case <-s.release:
		case <-ctx.Done():
			return state.Record{}, ctx.Err()
		}
	}
	return rec, err
}

func TestConcurrentArtifactSaveReturnsExistingAtStorageLimitWithoutAlert(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	shared := state.NewMemory()
	blobs := blob.NewMemory()
	barrier := &savedArtifactLookupBarrier{Store: shared, missing: make(chan struct{}), release: make(chan struct{})}
	var once sync.Once
	unblock := func() { once.Do(func() { close(barrier.release) }) }
	defer unblock()
	replica := func(st state.Store) *Platform {
		t.Helper()
		p, err := New(Config{Workspace: t.TempDir(), Backends: Backends{State: st}, Blobs: blobs})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = p.Close() })
		return p
	}
	a, b := replica(barrier), replica(shared)
	const tenant = "save-race"
	content := []byte("saved answer")
	if _, err := a.Policies.Put(tenant, policy.Policy{Scope: policy.Scope{Kind: policy.Tenant}, Period: policy.Day, MaxStorage: int64(len(content)), Action: policy.Block}, "test", time.Now()); err != nil {
		t.Fatal(err)
	}
	pub := Publication{Tenant: tenant, Thread: "conversation", Path: "answer.md", Title: "answer.md", By: "ada", saveIdentity: "same-answer"}
	area := memArea{name: pub.Path, data: content}
	type result struct {
		artifact artifacts.Artifact
		err      error
	}
	done := make(chan result, 1)
	barrier.armed.Store(true)
	go func() { saved, err := a.publishFrom(ctx, area, pub); done <- result{saved, err} }()
	select {
	case <-barrier.missing:
	case <-ctx.Done():
		t.Fatal("first replica never reached identity lookup", ctx.Err())
	}
	saved, err := b.publishFrom(ctx, area, pub)
	if err != nil {
		t.Fatal(err)
	}
	unblock()
	var first result
	select {
	case first = <-done:
	case <-ctx.Done():
		t.Fatal("first replica did not finish", ctx.Err())
	}
	if first.err != nil || first.artifact.ID != saved.ID || first.artifact.Version != 1 {
		t.Fatalf("retry at quota returned %+v, %v; want existing %s", first.artifact, first.err, saved.ID)
	}
	usage, err := a.Artifacts.Usage(tenant)
	if err != nil || usage.Count != 1 || usage.Bytes != int64(len(content)) {
		t.Fatalf("duplicate counted toward quota: %+v, %v", usage, err)
	}
	raised, err := a.Alerts.Alerts(tenant)
	if err != nil {
		t.Fatal(err)
	}
	for _, alert := range raised {
		if alert.Kind == alerts.Budget {
			t.Fatalf("idempotent save raised storage alert: %+v", alert)
		}
	}
	notices, _, err := a.Inbox.Page(tenant, inbox.Query{Kind: inbox.KindBudget})
	if err != nil || len(notices) != 0 {
		t.Fatalf("idempotent save created budget inbox notice: %+v, %v", notices, err)
	}
}
