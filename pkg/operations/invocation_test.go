package operations_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/wire"

	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestClockInvocationReplayRejectsChangedAssignmentIdentity(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := operations.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		a, err := s.SetAssignment("t", operations.Assignment{ID: "a", Agent: "worker", Owner: "owner", Goal: "Work", DoneWhen: "Done", Cron: "0 0 * * *", Schedule: "nightly", State: operations.Requested}, 0, "owner", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		tag := wire.NewEvent(wire.Raise{ID: "nightly@1", Tenant: "t", Agent: "worker", Origin: "cron", Text: "Work", Context: map[string]string{operations.MetaAssignment: "a", "cron_schedule": "nightly"}, At: time.Now()})
		if _, err := s.ReserveInvocation("t", "a", tag, a.Version, time.Now()); err != nil {
			t.Fatal(err)
		}
		copied, _ := s.Invocation("t", "a")
		wire.SetMeta(&copied, operations.MetaAssignment, "changed")
		original, _ := s.Invocation("t", "a")
		if wire.Meta(original)[operations.MetaAssignment] != "a" {
			t.Fatal("caller changed durable invocation")
		}
		raw, err := st.Get(t.Context(), "operations", "t", "assignment:a")
		if err != nil {
			t.Fatal(err)
		}
		var record map[string]any
		if err := json.Unmarshal(raw, &record); err != nil {
			t.Fatal(err)
		}
		record["invocation"].(map[string]any)["data"].(map[string]any)["context"].(map[string]any)[operations.MetaAssignment] = "other"
		if err := state.NewTable[map[string]any](st, "operations").Put("t", "assignment:a", record); err != nil {
			t.Fatal(err)
		}
		if _, err := operations.Open(st); !errors.Is(err, operations.ErrInvalid) {
			t.Fatalf("altered invocation was accepted: %v", err)
		}
	})
}
