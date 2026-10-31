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
		if paused, err := s.Paused("acme", "helper"); err != nil || !paused {
			t.Fatalf("pause lost: %v", err)
		}
		if paused, err := s.Paused("other", "helper"); err != nil || paused {
			t.Fatalf("pause crossed tenant: %v", err)
		}
		if got, err := s.Decision("acme", scopes, operations.CallAgents); err != nil || got != operations.Never {
			t.Fatalf("decision = %s, %v", got, err)
		}
		if got, err := s.Decision("acme", scopes, operations.WriteFiles); err != nil || got != operations.Unspecified {
			t.Fatalf("missing rule grants = %s, %v", got, err)
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
		if got, _, _ := s.Assignment("t", "a"); got.RunID != "run" || !got.DueAt.Equal(a.DueAt) {
			t.Fatalf("lost pending work: %+v", got)
		}
		if err = s.Settle("t", "run", true, at); err != nil {
			t.Fatal(err)
		}
		got, _, _ := s.Assignment("t", "a")
		if got.State != operations.Result {
			t.Fatalf("settled = %+v", got)
		}
		if got, err := s.Assignments("t", 200, func(a operations.Assignment) bool { return a.Project == "hidden" }); err != nil || len(got) != 0 {
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
	if paused, err := s.Paused("t", "a"); err != nil || paused {
		t.Fatalf("failed pause was kept: %v", err)
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
		a, _, _ = s.Assignment("t", a.ID)
		if a.State != operations.NeedsPerson || a.RunID != "still-running" {
			t.Fatalf("deadline invented an ending: %+v", a)
		}
		if again, err := s.Due("t", at); err != nil || len(again) != 1 || again[0].ID != notices[0].ID {
			t.Fatal("unposted notice lost after recovery")
		}
	})
}

func TestReadRejectsMalformedPolicyRatherThanDroppingItsRestriction(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		for _, policy := range []operations.Policy{
			{Scope: operations.Scope{Kind: "workspace"}, Rules: map[operations.Category]operations.Level{operations.CallAgents: "unexpected"}},
			{Scope: operations.Scope{Kind: "workspace"}, Rules: map[operations.Category]operations.Level{"unknown": operations.Never}},
			{Scope: operations.Scope{Kind: "agent", Name: "different"}, Rules: map[operations.Category]operations.Level{operations.CallAgents: operations.Never}},
		} {
			st := open(t)
			table := state.NewTable[map[string]any](st, "operations")
			if err := table.Put("acme", "policy:workspace:", map[string]any{"policy": policy}); err != nil {
				t.Fatal(err)
			}
			s, err := operations.Open(st)
			if err != nil {
				t.Fatal(err)
			}
			scopes := []operations.Scope{{Kind: "workspace"}}
			if _, err := s.Decision("acme", scopes, operations.CallAgents); !errors.Is(err, operations.ErrInvalid) {
				t.Fatalf("malformed policy was accepted: %+v, %v", policy, err)
			}
		}
	})
}

func TestCompletedRunSettlesAfterItsDeadlineAcrossRestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := operations.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		at := time.Now().UTC()
		a, err := s.SetAssignment("t", operations.Assignment{ID: "late", Agent: "worker", Owner: "owner", Goal: "Review", DoneWhen: "Report", DueAt: at.Add(time.Minute), State: operations.Requested}, 0, "owner", at)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Reserve("t", a.ID, "native-run", a.Version, at); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Due("t", at.Add(2*time.Minute)); err != nil {
			t.Fatal(err)
		}
		s, err = operations.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Settle("t", "native-run", true, at.Add(3*time.Minute)); err != nil {
			t.Fatal(err)
		}
		a, _, _ = s.Assignment("t", a.ID)
		if a.State != operations.Result {
			t.Fatalf("deadline hid a real recorded result: %+v", a)
		}
	})
}
