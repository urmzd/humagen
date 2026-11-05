package platform

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
	"github.com/urmzd/orchagent/flow/event"
)

type completionFault struct {
	state.Store
	failInbox atomic.Bool
}

func (s *completionFault) Write(ctx context.Context, ns string, ops ...state.Op) error {
	if ns == "inbox" && s.failInbox.Load() {
		return statetest.ErrInjected
	}
	return s.Store.Write(ctx, ns, ops...)
}

func TestCompletionRecoversUndeliveredAnswerAfterCrash(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := &completionFault{Store: open(t)}
		cfg := Config{Workspace: t.TempDir(), Backends: Backends{State: st}}
		one, err := New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		at := time.Now().UTC()
		tag := wire.NewEvent(wire.Raise{ID: "scheduled", Tenant: "acme", Agent: "lead", Origin: "cron", At: at, Text: "Recap", Context: map[string]string{wire.MetaCronSchedule: "daily", MetaDeliver: DeliverInbox, metaCreatedBy: "ada"}})
		if err := one.Runs.Record("r1", tag, 1); err != nil {
			t.Fatal(err)
		}
		r, ended := one.Runs.Observe(event.Envelope{Tenant: "acme", Run: "r1", Type: event.TypeCompleted, Time: at.Add(time.Second), Data: event.Completed{Text: "Work is finished", Usage: &event.RunUsage{}}})
		if !ended {
			t.Fatal("not ended")
		}
		// Simulate process death after the terminal transition, before ended runs.
		two, err := New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		st.failInbox.Store(true)
		two.reconcile(t.Context())
		pending, err := two.Runs.PendingCompletions()
		if err != nil || len(pending) != 1 {
			t.Fatalf("failed delivery removed work: %v, %v", pending, err)
		}
		if _, ok := two.Inbox.ByKey("acme", "result:r1"); ok {
			t.Fatal("failed answer was delivered")
		}
		// Another process resumes only incomplete actions and finishes the marker.
		st.failInbox.Store(false)
		three, err := New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		three.reconcile(t.Context())
		m, ok := three.Inbox.ByKey("acme", "result:r1")
		if !ok || m.To != "ada" || m.Subject != "Work is finished" {
			t.Fatalf("answer = %+v", m)
		}
		pending, err = three.Runs.PendingCompletions()
		if err != nil || len(pending) != 0 {
			t.Fatalf("completion = %v, %v", pending, err)
		}
		if _, _, err := three.Inbox.Withdraw("acme", m.ID, "ada", "Done", at); err != nil {
			t.Fatal(err)
		}
		// Crash after effect but before ack can replay, including after dismissal.
		if err := three.deliverAnswer(t.Context(), r); err != nil {
			t.Fatal(err)
		}
		again, ok := three.Inbox.ByKey("acme", "result:r1")
		if !ok || again.ID != m.ID || again.State != inbox.Dismissed {
			t.Fatalf("duplicate or reopened message: %+v", again)
		}
		three.ended(t.Context(), r)
		if got, _ := three.Runs.Get("acme", "r1"); got.Status != runs.Completed {
			t.Fatal(got.Status)
		}
	})
}

func TestCompletionRetriesHousekeepingCatalogRead(t *testing.T) {
	p, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	// A transient catalog read is not a missing optional agent.
	p.Agents = &completionCatalogFailure{AgentStore: p.Agents}
	err = p.startHook(t.Context(), runs.Run{Tenant: "acme", ID: "r1"}, "lead", hookTitle, "r1", "title")
	if !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("catalog error = %v", err)
	}
}

type completionCatalogFailure struct{ wire.AgentStore }

func (*completionCatalogFailure) Get(context.Context, string, string, int) (wire.Revision, error) {
	return wire.Revision{}, statetest.ErrInjected
}
