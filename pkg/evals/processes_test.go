package evals

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// Two processes on one database, each with its own store, read each other's
// suites and runs at once; suites created through either at the same time
// are each kept; a suite run through both at once opens one run; and runs of
// different suites begun at once are numbered without a repeat.
func TestTwoProcessesAgree(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, err := Open(a)
		if err != nil {
			t.Fatal(err)
		}
		two, err := Open(b)
		if err != nil {
			t.Fatal(err)
		}
		stores := []*Store{one, two}
		cases := []Case{{ID: "c1", Input: "hi", Checks: []Check{{Kind: Contains, Value: "x"}}}}
		suite := agentSuite(cases...)
		if err := one.Put("acme", suite, false); err != nil {
			t.Fatal(err)
		}
		if got, err := two.Get("acme", "smoke"); err != nil || got.Name != "smoke" {
			t.Fatalf("the other process reads %+v, %v", got, err)
		}

		const n = 12
		var wg sync.WaitGroup
		for i := range n {
			wg.Go(func() {
				s := Suite{Name: fmt.Sprintf("s-%02d", i), Target: Agent, Subject: "lead", Cases: cases}
				if err := stores[i%2].Put("acme", s, false); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		if got, err := two.List("acme"); err != nil || len(got) != n+1 {
			t.Fatalf("%d suites, want %d: %v", len(got), n+1, err)
		}

		var opened atomic.Int32
		for i := range n {
			wg.Go(func() {
				_, err := stores[i%2].Begin("acme", suite, 1, "ada", now)
				switch {
				case err == nil:
					opened.Add(1)
				case !errors.Is(err, ErrBusy):
					t.Error(err)
				}
			})
		}
		wg.Wait()
		if opened.Load() != 1 {
			t.Fatalf("%d runs of one suite opened at once", opened.Load())
		}

		ids := make([]string, n)
		for i := range n {
			wg.Go(func() {
				s, err := stores[i%2].Get("acme", fmt.Sprintf("s-%02d", i))
				if err != nil {
					t.Error(err)
					return
				}
				run, err := stores[i%2].Begin("acme", s, 1, "ada", now)
				if err != nil {
					t.Error(err)
				}
				ids[i] = run.ID
			})
		}
		wg.Wait()
		seen := map[string]bool{"ev-1": true}
		for _, id := range ids {
			if seen[id] {
				t.Fatalf("run id %q issued twice: %v", id, ids)
			}
			seen[id] = true
		}

		// A run saved through one is read so through the other.
		runs, err := one.Runs("acme", "smoke")
		if err != nil || len(runs) != 1 {
			t.Fatalf("runs = %+v, %v", runs, err)
		}
		run := runs[0]
		run.Finish([]Result{{CaseID: "c1", Passed: true}}, nil, now)
		if err := one.Save("acme", run); err != nil {
			t.Fatal(err)
		}
		if got, _ := two.Runs("acme", "smoke"); got[0].Status != Passed {
			t.Fatalf("the other process reads %+v", got[0])
		}
		if err := two.Delete("acme", "smoke"); err != nil {
			t.Fatal(err)
		}
		if _, err := one.Runs("acme", "smoke"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("a deleted suite's runs: %v", err)
		}
		if got, _ := one.List("globex"); len(got) != 0 {
			t.Fatal("another tenant sees acme's suites")
		}
	})
}
