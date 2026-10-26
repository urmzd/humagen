package platform_test

import (
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestChangeAuditUsesPrincipalAndInboxRetention(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		now := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
		p, err := platform.New(platform.Config{Backends: platform.Backends{State: st}, Workspace: t.TempDir(), Now: func() time.Time { return now }, InboxRetention: map[string]time.Duration{"short": 7 * 24 * time.Hour}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = p.Close() })
		if _, err := p.BeginAudit(auth.Principal{}, audit.Change{Action: "memory.forget", Resource: "memory", ID: "one"}); err == nil {
			t.Fatal("anonymous audit accepted")
		}
		for _, tenant := range []string{"short", "default"} {
			who := auth.Principal{Tenant: tenant, Subject: "ana"}
			attempt, err := p.BeginAudit(who, audit.Change{Action: "memory.forget", Resource: "memory", ID: "one", Subject: "ana"})
			if err != nil {
				t.Fatal(err)
			}
			if err := attempt.Finish(nil); err != nil {
				t.Fatal(err)
			}
		}
		now = now.Add(8 * 24 * time.Hour)
		p.RetainInbox()
		replay, err := audit.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		q := audit.Query{From: now.Add(-10 * 24 * time.Hour), To: now, Limit: 10}
		short, err := replay.List("short", q, nil)
		if err != nil {
			t.Fatal(err)
		}
		kept, err := replay.List("default", q, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(short) != 0 || len(kept) != 1 || kept[0].Actor != "ana" || kept[0].Outcome != audit.Succeeded {
			t.Fatalf("retention/actor: short=%+v kept=%+v", short, kept)
		}
	})
}
