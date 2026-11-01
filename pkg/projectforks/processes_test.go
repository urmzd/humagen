package projectforks_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/urmzd/humagen/pkg/projectforks"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// Two processes on one database see each other's plans at once, a plan two
// processes start at once is kept once, and plans started at once through
// both are each kept and counted.
func TestTwoProcessesAgree(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, _ := projectforks.Open(a)
		two, _ := projectforks.Open(b)
		first, err := one.Start("acme", "ana", plan("copied"), at)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := two.Get("acme", "copied", "ana"); err != nil || got.ID != first.ID {
			t.Fatalf("the other process reads %+v, %v", got, err)
		}
		if err := two.Failure("acme", "copied", "ana", errors.New("stopped"), at); err != nil {
			t.Fatal(err)
		}
		if got, _ := one.Get("acme", "copied", "ana"); got.Error == "" {
			t.Fatalf("the first process reads %+v", got)
		}

		const n = 16
		ids := make([]string, n)
		var wg sync.WaitGroup
		for i := range n {
			s := []*projectforks.Store{one, two}[i%2]
			wg.Go(func() {
				// Every writer starts "shared", and one plan of its own.
				r, err := s.Start("acme", "ana", plan("shared"), at)
				if err != nil {
					t.Error(err)
					return
				}
				ids[i] = r.ID
				if _, err := s.Start("acme", "ana", plan(fmt.Sprintf("copy-%02d", i)), at); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		for _, id := range ids {
			if id != ids[0] {
				t.Fatalf("one destination was started as %s and %s", ids[0], id)
			}
		}
		for i := range n {
			if _, err := one.Get("acme", fmt.Sprintf("copy-%02d", i), "ana"); err != nil {
				t.Fatalf("copy-%02d: %v", i, err)
			}
		}
		raw, err := a.Lookup(context.Background(), "project-forks", "acme", "#count")
		if err != nil {
			t.Fatal(err)
		}
		var count int
		if err := json.Unmarshal(raw.Value, &count); err != nil || count != n+2 {
			t.Fatalf("count = %d, %v; want %d", count, err, n+2)
		}
	})
}

// Plans an older release kept without a count are counted once, though two
// processes start plans at the same time, and the cap holds.
func TestTwoProcessesCountOnceAndKeepTheCap(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		seed, _ := projectforks.Open(a)
		for i := range projectforks.MaxOperations - 2 {
			if _, err := seed.Start("acme", "ana", plan(fmt.Sprintf("old-%03d", i)), at); err != nil {
				t.Fatal(err)
			}
		}
		// What an older release wrote had no count.
		if err := a.Write(context.Background(), "project-forks", state.Delete("acme", "#count")); err != nil {
			t.Fatal(err)
		}
		one, _ := projectforks.Open(a)
		two, _ := projectforks.Open(b)
		var made, limited sync.Map
		var wg sync.WaitGroup
		for i := range 8 {
			s := []*projectforks.Store{one, two}[i%2]
			wg.Go(func() {
				_, err := s.Start("acme", "ana", plan(fmt.Sprintf("new-%d", i)), at)
				switch {
				case err == nil:
					made.Store(i, true)
				case errors.Is(err, projectforks.ErrLimit):
					limited.Store(i, true)
				default:
					t.Error(err)
				}
			})
		}
		wg.Wait()
		n := 0
		made.Range(func(_, _ any) bool { n++; return true })
		if n != 2 {
			t.Fatalf("%d plans started past %d kept, want 2", n, projectforks.MaxOperations-2)
		}
		raw, err := b.Lookup(context.Background(), "project-forks", "acme", "#count")
		if err != nil {
			t.Fatal(err)
		}
		var count int
		if err := json.Unmarshal(raw.Value, &count); err != nil || count != projectforks.MaxOperations {
			t.Fatalf("count = %d, %v; want %d", count, err, projectforks.MaxOperations)
		}
	})
}
