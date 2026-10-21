package evals

import (
	"errors"
	"reflect"
	"testing"

	"github.com/urmzd/warp/pkg/state"
	"github.com/urmzd/warp/pkg/state/statetest"
)

func TestRestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		suite := agentSuite(Case{ID: "c1", Input: "hi", Checks: []Check{{Kind: Contains, Value: "hello"}}})
		if err := s.Put("acme", suite, false); err != nil {
			t.Fatal(err)
		}
		if err := s.Put("acme", Suite{Name: "gone", Target: Agent, Subject: "lead", Cases: suite.Cases}, false); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Begin("acme", Suite{Name: "gone", Target: Agent, Subject: "lead", Cases: suite.Cases}, 1, "ada", now); err != nil {
			t.Fatal(err)
		}
		if err := s.Delete("acme", "gone"); err != nil {
			t.Fatal(err)
		}
		done, err := s.Begin("acme", suite, 3, "ada", now)
		if err != nil {
			t.Fatal(err)
		}
		done.Finish([]Result{{CaseID: "c1", Passed: true, Output: "hello"}}, nil, now)
		if err := s.Save("acme", done); err != nil {
			t.Fatal(err)
		}
		going, err := s.Begin("acme", suite, 4, "bob", now)
		if err != nil {
			t.Fatal(err)
		}

		again, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := again.Get("acme", "smoke"); err != nil || !reflect.DeepEqual(got, suite) {
			t.Fatalf("suite after restart = %+v, %v", got, err)
		}
		if _, err := again.Get("acme", "gone"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("a deleted suite came back: %v", err)
		}
		runs, _ := again.Runs("acme", "smoke")
		if len(runs) != 2 || runs[0].ID != going.ID || runs[1].ID != done.ID || runs[1].Status != Passed || runs[1].Results[0].Output != "hello" {
			t.Fatalf("runs after restart = %+v", runs)
		}
		// Nothing carries on the run that was going, so it is not left open.
		if runs[0].Status != Errored || runs[0].Error != Interrupted {
			t.Fatalf("the interrupted run = %+v", runs[0])
		}
		next, err := again.Begin("acme", suite, 5, "bob", now)
		if err != nil || next.ID == going.ID || next.ID == done.ID {
			t.Fatalf("Begin after restart = %+v, %v", next, err)
		}
		if len(again.List("globex")) != 0 {
			t.Fatal("another tenant sees the suites")
		}
	})
}

func TestRestartKeepsTheRunCap(t *testing.T) {
	st := state.NewMemory()
	s, _ := Open(st)
	suite := agentSuite(Case{ID: "c1", Input: "hi", Checks: []Check{{Kind: Contains, Value: "x"}}})
	if err := s.Put("acme", suite, false); err != nil {
		t.Fatal(err)
	}
	for range maxRuns + 3 {
		run, err := s.Begin("acme", suite, 1, "ada", now)
		if err != nil {
			t.Fatal(err)
		}
		run.Finish(nil, nil, now)
		if err := s.Save("acme", run); err != nil {
			t.Fatal(err)
		}
	}
	again, _ := Open(st)
	runs, _ := again.Runs("acme", "smoke")
	if len(runs) != maxRuns || runs[len(runs)-1].ID != "ev-4" {
		t.Fatalf("after restart: %d runs, oldest %s", len(runs), runs[len(runs)-1].ID)
	}
}

func TestFailedWriteChangesNothing(t *testing.T) {
	f := statetest.NewFaulty(state.NewMemory())
	s, _ := Open(f)
	suite := agentSuite(Case{ID: "c1", Input: "hi", Checks: []Check{{Kind: Contains, Value: "x"}}})
	if err := s.Put("acme", suite, false); err != nil {
		t.Fatal(err)
	}
	f.Fail(statetest.ErrInjected)
	if _, err := s.Begin("acme", suite, 1, "ada", now); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Begin = %v", err)
	}
	if err := s.Delete("acme", "smoke"); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Delete = %v", err)
	}
	f.Fail(nil)
	// The failed Begin left no open run behind and used no id.
	run, err := s.Begin("acme", suite, 1, "ada", now)
	if err != nil || run.ID != "ev-1" {
		t.Fatalf("Begin after the store recovered = %+v, %v", run, err)
	}
}
