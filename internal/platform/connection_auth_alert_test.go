package platform

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/alerts"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/threads"
)

type authAlertFault struct {
	state.Store
	fail atomic.Bool
}

func (s *authAlertFault) Write(ctx context.Context, namespace string, ops ...state.Op) error {
	if namespace == "inbox" && s.fail.CompareAndSwap(true, false) {
		return errors.New("test transport unavailable")
	}
	return s.Store.Write(ctx, namespace, ops...)
}
func TestConnectionAuthFailureKeepsOnePrivateOwnerAlertAcrossReplicas(t *testing.T) {
	st := &authAlertFault{Store: state.NewMemory()}
	newReplica := func() *Platform {
		defs, err := wire.OpenAgents(st, "offline")
		if err != nil {
			t.Fatal(err)
		}
		p, err := New(Config{Workspace: t.TempDir(), Backends: Backends{State: st, Agents: defs}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = p.Close() })
		return p
	}
	p := newReplica()
	if _, err := p.Agents.Create(t.Context(), "acme", wire.AgentSpec{Name: BuiltinEntry, Model: "offline", Provider: "offline", SystemPrompt: "Help reconnect"}, "test"); err != nil {
		t.Fatal(err)
	}
	other := newReplica()
	st.fail.Store(true)
	if err := p.RecordConnectionAuthFailure(t.Context(), "acme", "ada", "notion"); err == nil {
		t.Fatal("transport failure not surfaced")
	}
	all, err := p.Alerts.Alerts("acme")
	if err != nil || len(all) != 1 {
		t.Fatalf("durable alert lost: %+v %v", all, err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Go(func() {
			if err := other.RecordConnectionAuthFailure(t.Context(), "acme", "ada", "notion"); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	all, err = other.Alerts.Alerts("acme")
	if err != nil || len(all) != 1 {
		t.Fatalf("replicas duplicated alert: %+v %v", all, err)
	}
	a := all[0]
	th, err := other.Threads.Get("acme", a.Session)
	if err != nil || th.Session == nil || th.Visibility != threads.Private || th.CreatedBy != "ada" || th.Session.Agent != BuiltinEntry || a.Action != "reconnect" || a.Title != "Reconnect Notion" {
		t.Fatalf("unsafe connection attention: %+v %+v %v", a, th, err)
	}
	m, ok := other.Inbox.ByKey("acme", alertKey(a.ID))
	if !ok || m.Source.Thread != a.Session {
		t.Fatalf("transport recovery lost anchor: %+v", m)
	}
	owner := auth.Principal{Kind: auth.User, Tenant: "acme", Subject: "ada", Roles: []auth.Role{auth.Operator}}
	stranger := owner
	stranger.Subject = "ben"
	if !other.Seer(owner).Alert(a) || other.Seer(stranger).Alert(a) {
		t.Fatal("connection alert does not enforce private owner")
	}
	for _, provider := range []string{"https://private.example/?token=SECRET", "notion\nSECRET", strings.Repeat("a", 101)} {
		if err := other.RecordConnectionAuthFailure(t.Context(), "acme", "ada", provider); !errors.Is(err, alerts.ErrInvalid) {
			t.Fatalf("unsafe provider accepted: %v", err)
		}
	}
	if err := other.ResolveConnectionAuthFailure(t.Context(), "acme", "ben", "notion"); err != nil {
		t.Fatal(err)
	}
	still, _ := other.Alerts.Alert("acme", a.ID)
	if !still.Open() {
		t.Fatal("another person's success resolved owner alert")
	}
	if err := other.ResolveConnectionAuthFailure(t.Context(), "acme", "ada", "notion"); err != nil {
		t.Fatal(err)
	}
	resolved, _ := other.Alerts.Alert("acme", a.ID)
	if resolved.Open() {
		t.Fatal("confirmed recovery left alert open")
	}
	if err := other.RecordConnectionAuthFailure(t.Context(), "acme", "ada", "notion"); err != nil {
		t.Fatal(err)
	}
	all, _ = other.Alerts.Alerts("acme")
	if len(all) != 2 {
		t.Fatalf("later genuine failure not raised: %+v", all)
	}
}
