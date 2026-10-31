package contextflags

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// Two processes on one database, each with its own store, read each other's
// flags at once, and runs flagged through either at the same time are each
// kept and counted once.
func TestTwoProcessesAgree(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, _ := Open(a)
		two, _ := Open(b)
		if err := one.Mark("acme", "run-0"); err != nil {
			t.Fatal(err)
		}
		if !two.Has("acme", "run-0") || two.Has("other", "run-0") {
			t.Fatal("the other process does not read the flag")
		}
		const runs = 40
		var wg sync.WaitGroup
		for i := range runs {
			for _, s := range []*Store{one, two} {
				wg.Go(func() {
					if err := s.Mark("acme", fmt.Sprintf("run-%02d", i+1)); err != nil {
						t.Error(err)
					}
				})
			}
		}
		wg.Wait()
		for i := range runs {
			if !one.Has("acme", fmt.Sprintf("run-%02d", i+1)) {
				t.Fatalf("run-%02d lost", i+1)
			}
		}
		r, err := a.Lookup(t.Context(), namespace, "acme", count)
		if err != nil {
			t.Fatal(err)
		}
		var n int
		if err := json.Unmarshal(r.Value, &n); err != nil || n != runs+1 {
			t.Fatalf("count %d, want %d", n, runs+1)
		}
	})
}

// A tenant kept before the count is counted once, though two processes flag
// runs in it at the same time.
func TestTwoProcessesCountOldFlags(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		old := state.NewTable[bool](a, namespace)
		for i := range 25 {
			if err := old.Put("acme", fmt.Sprintf("old-%d", i), true); err != nil {
				t.Fatal(err)
			}
		}
		one, _ := Open(a)
		two, _ := Open(b)
		var wg sync.WaitGroup
		for i := range 10 {
			s := []*Store{one, two}[i%2]
			wg.Go(func() {
				if err := s.Mark("acme", fmt.Sprintf("new-%d", i)); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		r, err := b.Lookup(t.Context(), namespace, "acme", count)
		if err != nil {
			t.Fatal(err)
		}
		var n int
		if err := json.Unmarshal(r.Value, &n); err != nil || n != 35 {
			t.Fatalf("count %d, want 35", n)
		}
	})
}
