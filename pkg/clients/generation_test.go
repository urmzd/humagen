package clients

import (
	"sync"
	"testing"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// Two processes count changes in one place: each sees the other's touches,
// and touches racing through both are each counted once.
func TestTheChangeCountIsShared(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, err := Open(a)
		if err != nil {
			t.Fatal(err)
		}
		two, err := Open(b)
		if err != nil {
			t.Fatal(err)
		}
		if n, err := two.Generation(); err != nil || n != 0 {
			t.Fatalf("fresh count = %d, %v", n, err)
		}
		if n, err := one.Touch(); err != nil || n != 1 {
			t.Fatalf("touch = %d, %v", n, err)
		}
		if n, err := two.Generation(); err != nil || n != 1 {
			t.Fatalf("the other process reads %d, %v", n, err)
		}
		var wg sync.WaitGroup
		for _, s := range []*Memory{one, two, one, two} {
			wg.Go(func() {
				for range 5 {
					if _, err := s.Touch(); err != nil {
						t.Error(err)
					}
				}
			})
		}
		wg.Wait()
		if n, err := one.Generation(); err != nil || n != 21 {
			t.Fatalf("count = %d, %v, want 21", n, err)
		}
	})
}
