package auditrefs

import (
	"fmt"
	"sync"
	"testing"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// Two processes on one database asking for a tenant's first reference at
// the same time agree on its seed: one is created, and both derive the same
// reference from it, now and after either reopens.
func TestTwoProcessesAgree(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		stores := make([]*Store, 8)
		for i := range stores {
			s, err := Open([]state.Store{a, b}[i%2])
			if err != nil {
				t.Fatal(err)
			}
			stores[i] = s
		}
		ids := make([]string, len(stores))
		var wg sync.WaitGroup
		for i, s := range stores {
			wg.Go(func() {
				id, err := s.ID("acme", "project", "42")
				if err != nil {
					t.Error(err)
				}
				ids[i] = id
			})
		}
		wg.Wait()
		for i, id := range ids {
			if id == "" || id != ids[0] {
				t.Fatalf("reference %d = %q, want %q", i, id, ids[0])
			}
		}
		again, err := Open(b)
		if err != nil {
			t.Fatal(err)
		}
		if id, err := again.ID("acme", "project", "42"); err != nil || id != ids[0] {
			t.Fatalf("after reopening: %q, %v", id, err)
		}
		recs, _, err := a.List(t.Context(), namespace, "acme", state.Query{Limit: 10})
		if err != nil || len(recs) != 1 {
			t.Fatalf("%d seeds, %v", len(recs), err)
		}
		for i := range 4 {
			if id, err := stores[i].ID(fmt.Sprintf("t%d", i), "project", "42"); err != nil || id == ids[0] {
				t.Fatalf("tenant t%d: %q, %v", i, id, err)
			}
		}
	})
}
