package agentcolors

import (
	"fmt"
	"sync"
	"testing"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// Two processes on one database, each with its own store, read each other's
// reservations at once, and agents given hues through either at the same
// time each get one no other agent holds.
func TestTwoProcessesAgree(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, _ := Open(a)
		two, _ := Open(b)
		first, err := one.Assign("acme", []string{"scout"})
		if err != nil {
			t.Fatal(err)
		}
		again, err := two.Assign("acme", []string{"scout"})
		if err != nil || again["scout"] != first["scout"] {
			t.Fatalf("the other process gives %v, %v; want %v", again, err, first)
		}
		const agents = 40
		var wg sync.WaitGroup
		for i := range agents {
			s := []*Store{one, two}[i%2]
			wg.Go(func() {
				if _, err := s.Assign("acme", []string{fmt.Sprintf("agent-%02d", i)}); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		names := []string{"scout"}
		for i := range agents {
			names = append(names, fmt.Sprintf("agent-%02d", i))
		}
		got, err := one.Assign("acme", names)
		if err != nil {
			t.Fatal(err)
		}
		other, err := two.Assign("acme", names)
		if err != nil {
			t.Fatal(err)
		}
		seen := map[float64]string{}
		for name, hue := range got {
			if other[name] != hue {
				t.Fatalf("%s is %v to one and %v to the other", name, hue, other[name])
			}
			if was, dup := seen[hue]; dup {
				t.Fatalf("%s and %s share hue %v", was, name, hue)
			}
			seen[hue] = name
		}
		if len(seen) != agents+1 {
			t.Fatalf("%d hues, want %d", len(seen), agents+1)
		}
	})
}

// A tenant kept before the record of every reservation keeps its hues, and
// two processes reserving at once on it agree.
func TestTwoProcessesConvertOnce(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		old := state.NewTable[float64](a, "agent-colors")
		for i := range 12 {
			if err := old.Put("acme", fmt.Sprintf("old-%d", i), float64(i)*30); err != nil {
				t.Fatal(err)
			}
		}
		one, _ := Open(a)
		two, _ := Open(b)
		var wg sync.WaitGroup
		for i := range 8 {
			s := []*Store{one, two}[i%2]
			wg.Go(func() {
				if _, err := s.Assign("acme", []string{fmt.Sprintf("new-%d", i)}); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		hues, _, err := two.reservations(t.Context(), "acme")
		if err != nil || len(hues) != 20 {
			t.Fatalf("%d reservations, %v", len(hues), err)
		}
		seen := map[float64]bool{}
		for name, hue := range hues {
			if seen[hue] {
				t.Fatalf("hue %v given twice", hue)
			}
			seen[hue] = true
			var i int
			if _, err := fmt.Sscanf(name, "old-%d", &i); err == nil && hue != float64(i)*30 {
				t.Fatalf("%s moved to %v", name, hue)
			}
		}
	})
}
