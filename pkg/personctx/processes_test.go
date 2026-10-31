package personctx

import (
	"errors"
	"sync"
	"testing"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// Two processes on one database, each with its own store, read each other's
// writes at once, and people writing one context through both at the same
// time from the version they read each either land, one after another, or
// are told to read again: no write is lost unseen and none is applied twice.
func TestTwoProcessesAgree(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, _ := Open(a)
		two, _ := Open(b)
		first, err := one.Set("acme", "ada", input(0), at)
		if err != nil {
			t.Fatal(err)
		}
		got := get(t, two, "acme", "ada")
		if got.Version != 1 || got.Memory != first.Memory || len(got.ToolServers) != 1 {
			t.Fatalf("the other process reads %+v", got)
		}
		const writers = 20
		var wg sync.WaitGroup
		var mu sync.Mutex
		landed := 0
		for i := range writers {
			s := []*Store{one, two}[i%2]
			wg.Go(func() {
				for {
					cur, err := s.Get("acme", "ada")
					if err != nil {
						t.Error(err)
						return
					}
					_, err = s.Set("acme", "ada", input(cur.Version), at)
					if errors.Is(err, ErrVersion) {
						continue
					}
					if err != nil {
						t.Error(err)
						return
					}
					mu.Lock()
					landed++
					mu.Unlock()
					return
				}
			})
		}
		wg.Wait()
		got = get(t, one, "acme", "ada")
		if landed != writers || got.Version != writers+1 || got.Memory != first.Memory {
			t.Fatalf("%d writes landed, version %d, memory %q; want %d, %d, %q", landed, got.Version, got.Memory, writers, writers+1, first.Memory)
		}
	})
}
