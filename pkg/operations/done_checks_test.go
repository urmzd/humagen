package operations_test

import (
	"errors"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestChecksHoldAnAnswerToWhatItSays(t *testing.T) {
	a := operations.Assignment{DoneChecks: []operations.Check{
		{Kind: operations.Contains, Value: "DONE:"},
		{Kind: operations.NotContains, Value: "TODO"},
		{Kind: operations.Matches, Value: `PR #\d+`},
	}}
	if unmet := a.Unmet("DONE: merged in PR #41."); len(unmet) != 0 {
		t.Fatalf("unmet = %v", unmet)
	}
	unmet := a.Unmet("Working on it. TODO: open the pull request.")
	if len(unmet) != 3 || unmet[0].String() != `the answer contains "DONE:"` || unmet[2].String() != `the answer matches "PR #\\d+"` {
		t.Fatalf("unmet = %v", unmet)
	}
	exact := operations.Assignment{DoneChecks: []operations.Check{{Kind: operations.Equals, Value: "yes"}}}
	if len(exact.Unmet(" yes\n")) != 0 || len(exact.Unmet("yes, mostly")) != 1 {
		t.Fatal("equals compares the whole answer, spacing aside")
	}
	if a.Continues() != operations.DefaultContinue || (operations.Assignment{}).Continues() != 0 {
		t.Fatal("an assignment with checks continues a default number of times, and one without never")
	}
}

func TestContinueMovesAnAssignmentToItsNextRunOnce(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		s, err := operations.Open(open(t))
		if err != nil {
			t.Fatal(err)
		}
		at := time.Now().UTC()
		a, err := s.SetAssignment("acme", operations.Assignment{ID: "report", Agent: "worker", Owner: "ada", Goal: "g", DoneWhen: "d", State: operations.Requested,
			DoneChecks: []operations.Check{{Kind: operations.Contains, Value: "DONE"}}, MaxContinue: 2}, 0, "ada", at)
		if err != nil {
			t.Fatal(err)
		}
		// Work that is not in progress has nothing to continue.
		if _, err := s.Continue("acme", a.ID, "r1", "r2", at); !errors.Is(err, operations.ErrConflict) {
			t.Fatalf("continue before a run = %v", err)
		}
		a, err = s.Reserve("acme", a.ID, "r1", a.Version, at)
		if err != nil {
			t.Fatal(err)
		}
		version := a.Version
		next, err := s.Continue("acme", a.ID, "r1", "r2", at)
		if err != nil || next.RunID != "r2" || next.Continued != 1 || next.State != operations.InProgress || next.Version != version {
			t.Fatalf("continued = %+v, %v", next, err)
		}
		// The same step again changes nothing, as after an interruption.
		again, err := s.Continue("acme", a.ID, "r1", "r2", at)
		if err != nil || again.Continued != 1 {
			t.Fatalf("repeated = %+v, %v", again, err)
		}
		// A run it does not hold cannot move it.
		if _, err := s.Continue("acme", a.ID, "r1", "r3", at); !errors.Is(err, operations.ErrConflict) {
			t.Fatalf("continue from a stale run = %v", err)
		}
		if _, err := s.Continue("acme", a.ID, "r2", "r3", at); err != nil {
			t.Fatal(err)
		}
		// It continues no more often than it may.
		if _, err := s.Continue("acme", a.ID, "r3", "r4", at); !errors.Is(err, operations.ErrConflict) {
			t.Fatalf("a third continuation of two = %v", err)
		}
		// The run it now holds settles it, and the earlier ones do not.
		if err := s.Settle("acme", "r1", true, at); err != nil {
			t.Fatal(err)
		}
		if got, _, _ := s.Assignment("acme", a.ID); got.State != operations.InProgress {
			t.Fatalf("an earlier run settled it: %+v", got)
		}
		if held, ok, err := s.AssignmentOfRun("acme", "r3"); err != nil || !ok || held.ID != a.ID {
			t.Fatalf("assignment of run = %+v, %v, %v", held, ok, err)
		}
		if _, ok, _ := s.AssignmentOfRun("acme", "r1"); ok {
			t.Fatal("a run it no longer holds still names it")
		}
		if err := s.Settle("acme", "r3", true, at); err != nil {
			t.Fatal(err)
		}
		got, _, _ := s.Assignment("acme", a.ID)
		if got.State != operations.Result || got.Continued != 2 {
			t.Fatalf("settled = %+v", got)
		}
		// Work started again counts its continuations from none.
		got.State = operations.Accepted
		got, err = s.SetAssignment("acme", got, got.Version, "ada", at)
		if err != nil {
			t.Fatal(err)
		}
		if got, err = s.Reserve("acme", a.ID, "r9", got.Version, at); err != nil || got.Continued != 0 {
			t.Fatalf("restarted = %+v, %v", got, err)
		}
	})
}
