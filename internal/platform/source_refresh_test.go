package platform

import (
	"context"
	"github.com/urmzd/fiber/pkg/mcptools"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/knowledge"
	"testing"
	"time"
)

type scheduledSourceBoundary struct {
	calls int
	cause knowledge.Cause
}

func (b *scheduledSourceBoundary) Fetch(context.Context, string, mcptools.Secret) ([]knowledge.ImportDocument, error) {
	b.calls++
	if b.cause != "" {
		return nil, knowledge.ProviderError{Cause: b.cause}
	}
	return []knowledge.ImportDocument{{ID: "doc", Title: "Runbook", Text: "retained source content"}}, nil
}
func TestScheduledSourceRefreshUsesCurrentStandingPolicyAndStopsAfterFailure(t *testing.T) {
	pol, err := auth.ParsePolicy("scheduled-source", `role admin { allow * on * }
grant admin to subject "owner"`)
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Now()
	boundary := &scheduledSourceBoundary{}
	p, err := New(Config{Workspace: t.TempDir(), Now: func() time.Time { return clock }, Policy: pol, CredentialRefs: map[string][]string{"tenant": {"op://People/{subject}/"}}, Ext: Extensions{SourceProviders: map[string]knowledge.Provider{"fixture": boundary}, Resolver: mcptools.ResolverFunc(func(context.Context, string) (mcptools.Secret, error) {
		return mcptools.NewSecret("test-source-only"), nil
	})}})
	if err != nil {
		t.Fatal(err)
	}
	who := p.byPolicy("tenant", "owner")
	_, err = p.CreateSource(who, "scheduled", "", &knowledge.SourceConfig{Mode: knowledge.Indexed, Provider: "fixture", Origin: "folder", Project: "~", SyncEverySeconds: 60, StopAfterSeconds: 300})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.ConnectSource(who, "scheduled", "op://People/owner/source"); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(time.Minute)
	p.refreshSourceOnce(t.Context())
	if boundary.calls != 1 {
		t.Fatal("authorized due refresh was not read")
	}
	first, _ := p.Knowledge.Get("tenant", "scheduled")
	if first.Lifecycle.LastGoodAt == nil {
		t.Fatal("successful refresh not retained")
	}
	p.refreshSourceOnce(t.Context())
	if boundary.calls != 1 {
		t.Fatal("not-due source refreshed twice")
	}
	p.cfg.Policy = nil
	clock = clock.Add(time.Minute)
	p.refreshSourceOnce(t.Context())
	if boundary.calls != 1 {
		t.Fatal("removed standing authorization still refreshed")
	}
	p.cfg.Policy = pol
	boundary.cause = knowledge.AuthExpired
	p.refreshSourceOnce(t.Context())
	if boundary.calls != 2 {
		t.Fatal("due failure not attempted")
	}
	clock = clock.Add(time.Hour)
	p.refreshSourceOnce(t.Context())
	if boundary.calls != 2 {
		t.Fatal("failed source automatically retried without operator repair")
	}
	after, _ := p.Knowledge.Get("tenant", "scheduled")
	if after.ContentVersion != first.ContentVersion || after.Documents != 1 || after.Lifecycle.Status != knowledge.NeedsAttention {
		t.Fatal("failure replaced last good copy")
	}
}
