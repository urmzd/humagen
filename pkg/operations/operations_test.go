package operations_test

import (
	"errors"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestDurableControlsAndTightening(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := operations.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		at := time.Now().UTC()
		c, err := s.SetControl("acme", "helper", operations.Control{Paused: true, Manager: "person"}, 0, "person", at)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.SetControl("acme", "helper", c, 0, "person", at); !errors.Is(err, operations.ErrConflict) {
			t.Fatalf("stale write = %v", err)
		}
		scopes := []operations.Scope{{Kind: "workspace"}, {Kind: "project", Name: "p"}, {Kind: "agent", Name: "helper"}}
		for i, level := range []operations.Level{operations.Ask, operations.Never, operations.Allow} {
			_, err := s.SetPolicy("acme", operations.Policy{Scope: scopes[i], Rules: map[operations.Category]operations.Level{operations.CallAgents: level}}, 0, "person", at)
			if err != nil {
				t.Fatal(err)
			}
		}
		s, err = operations.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if !s.Paused("acme", "helper") || s.Paused("other", "helper") {
			t.Fatal("pause lost or crossed tenant")
		}
		if got := s.Decision("acme", scopes, operations.CallAgents); got != operations.Never {
			t.Fatalf("decision = %s", got)
		}
		if got := s.Decision("acme", scopes, operations.WriteFiles); got != operations.Unspecified {
			t.Fatalf("missing rule grants = %s", got)
		}
	})
}

func TestAssignmentReservationAndRestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, _ := operations.Open(st)
		at := time.Now().UTC()
		a, err := s.SetAssignment("t", operations.Assignment{ID: "a", Agent: "helper", Project: "p", Owner: "owner", Goal: "Inspect the queue", DoneWhen: "Report queued items", State: operations.Requested, DueAt: at.Add(time.Hour)}, 0, "owner", at)
		if err != nil {
			t.Fatal(err)
		}
		a, err = s.Reserve("t", a.ID, "run", a.Version, at)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.Reserve("t", a.ID, "other", a.Version, at); !errors.Is(err, operations.ErrConflict) {
			t.Fatalf("duplicate admission = %v", err)
		}
		s, err = operations.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := s.Assignment("t", "a"); got.RunID != "run" || !got.DueAt.Equal(a.DueAt) {
			t.Fatalf("lost pending work: %+v", got)
		}
		if err = s.Settle("t", "run", true, at); err != nil {
			t.Fatal(err)
		}
		got, _ := s.Assignment("t", "a")
		if got.State != operations.Result {
			t.Fatalf("settled = %+v", got)
		}
		if got := s.Assignments("t", 200, func(a operations.Assignment) bool { return a.Project == "hidden" }); len(got) != 0 {
			t.Fatal("hidden assignment leaked")
		}
	})
}

func TestFailedWriteAndContractBounds(t *testing.T) {
	st := statetest.NewFaulty(state.NewMemory())
	s, _ := operations.Open(st)
	at := time.Now().UTC()
	st.Fail(statetest.ErrInjected)
	if _, err := s.SetControl("t", "a", operations.Control{Paused: true}, 0, "x", at); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("write = %v", err)
	}
	if s.Paused("t", "a") {
		t.Fatal("failed pause changed memory")
	}
	st.Fail(nil)
	h := operations.Handoff{ID: "h", From: "a", To: "b", Project: "p", Goal: "Review", DoneWhen: "Reviewed", State: operations.Requested, Chain: []string{"a", "b", "a"}}
	if _, err := s.SetHandoff("t", h, 0, at); !errors.Is(err, operations.ErrEscalation) {
		t.Fatalf("cycle = %v", err)
	}
	h.Chain = []string{"a", "b", "c", "d", "e"}
	if _, err := s.SetHandoff("t", h, 0, at); !errors.Is(err, operations.ErrEscalation) {
		t.Fatalf("long chain = %v", err)
	}
}

func TestDeadlinesSurviveRestartWithoutInventingCompletion(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, _ := operations.Open(st)
		at := time.Now().UTC()
		a, err := s.SetAssignment("t", operations.Assignment{ID: "overdue", Agent: "helper", Project: "p", Owner: "person", Goal: "Review", DoneWhen: "Review recorded", DueAt: at.Add(-time.Hour), State: operations.Requested}, 0, "person", at)
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.Reserve("t", a.ID, "still-running", a.Version, at)
		if err != nil {
			t.Fatal(err)
		}
		s, err = operations.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		notices, err := s.Due("t", at)
		if err != nil || len(notices) != 1 {
			t.Fatalf("deadline = %+v, %v", notices, err)
		}
		a, _ = s.Assignment("t", a.ID)
		if a.State != operations.NeedsPerson || a.RunID != "still-running" {
			t.Fatalf("deadline invented an ending: %+v", a)
		}
		if again, err := s.Due("t", at); err != nil || len(again) != 1 || again[0].ID != notices[0].ID {
			t.Fatal("unposted notice lost after recovery")
		}
	})
}
