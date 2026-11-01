package runs

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/urmzd/humagen/internal/wire"

	"github.com/urmzd/humagen/fiber/pkg/event"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestLearningGuardBindsRetainedOutcomeAndTenantAcrossRestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		index, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		record(t, index, "acme", "r1", "lead", OriginAPI, 0, nil)
		before, _ := index.Get("acme", "r1")
		old := LearningSnapshot(before)
		action, _ := json.Marshal(event.Action{Name: "record_outcome", Data: json.RawMessage(`{"kind":"decision","summary":"Review the source","decisions":["Keep Tuesday"]}`)})
		index.Observe(event.Envelope{Tenant: "acme", Run: "r1", Type: event.TypeAction, Data: wire.BodyOf(event.TypeAction, action), Time: t0})
		end(t, index, "acme", "r1", 1, event.TypeCompleted, wire.Lifecycle{Text: "Ready"})
		called := false
		if err := index.GuardLearning("acme", "r1", old, func(Run) error { called = true; return nil }); !errors.Is(err, ErrLearningConflict) || called {
			t.Fatal("stale or active snapshot committed", err)
		}
		current, _ := index.Get("acme", "r1")
		token := LearningSnapshot(current)
		again, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if err := again.GuardLearning("acme", "r1", token, func(Run) error { called = true; return nil }); err != nil || !called {
			t.Fatal("durable snapshot changed after replay", err)
		}
		if err := again.GuardLearning("other", "r1", token, func(Run) error { t.Fatal("cross-tenant callback"); return nil }); !errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
	})
}
