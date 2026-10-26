package agentcolors

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestAssignmentsSurviveRestartAndDoNotMove(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		names := make([]string, 100)
		for i := range names {
			names[i] = fmt.Sprintf("agent-%d", i)
		}
		first, err := s.Assign("a", names)
		if err != nil {
			t.Fatal(err)
		}
		seen := map[float64]bool{}
		for name, hue := range first {
			if seen[hue] {
				t.Fatalf("duplicate hue %v for %s", hue, name)
			}
			seen[hue] = true
		}
		reopened, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		second, err := reopened.Assign("a", append(names, "new-agent"))
		if err != nil {
			t.Fatal(err)
		}
		for name, hue := range first {
			if second[name] != hue {
				t.Fatalf("%s moved", name)
			}
		}
		other, err := reopened.Assign("b", []string{names[0]})
		if err != nil {
			t.Fatal(err)
		}
		if other[names[0]] != first[names[0]] {
			t.Fatal("workspace assignment not independent")
		}
	})
}

func TestFailedReservationChangesNothing(t *testing.T) {
	st := statetest.NewFaulty(state.NewMemory())
	s, err := Open(st)
	if err != nil {
		t.Fatal(err)
	}
	st.Fail(statetest.ErrInjected)
	if _, err := s.Assign("a", []string{"one"}); !errors.Is(err, statetest.ErrInjected) {
		t.Fatal(err)
	}
	st.Fail(nil)
	got, err := s.Assign("a", []string{"one"})
	if err != nil || got["one"] != Preferred("one") {
		t.Fatalf("%v %v", got, err)
	}
}

func TestConcurrentAssignmentsAreUnique(t *testing.T) {
	s, _ := Open(nil)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Go(func() {
			if _, err := s.Assign("a", []string{fmt.Sprintf("agent-%d", i)}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	seen := map[float64]bool{}
	for _, hue := range s.tenants["a"] {
		if seen[hue] {
			t.Fatal("collision")
		}
		seen[hue] = true
	}
}

func TestReservationLimitFailsClosed(t *testing.T) {
	s, _ := Open(nil)
	for i := 0; i < Limit; i++ {
		s.tenant("a")[fmt.Sprintf("a%d", i)] = float64(i) / 100
	}
	if _, err := s.Assign("a", []string{"extra"}); err == nil {
		t.Fatal("accepted extra reservation")
	}
}

func TestOpenRefusesDuplicateStoredHues(t *testing.T) {
	st := state.NewMemory()
	table := state.NewTable[float64](st, "agent-colors")
	if err := table.Put("a", "one", 15); err != nil {
		t.Fatal(err)
	}
	if err := table.Put("a", "two", 15); err != nil {
		t.Fatal(err)
	}
	if store, err := Open(st); err == nil || store != nil {
		t.Fatal("accepted colliding stored hues")
	}
}

func TestBuiltinRenameKeepsTheOriginalColor(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		before, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		old, err := before.Assign("a", []string{"warp-analyst", "warp-writer", "warp-builder", "helper"})
		if err != nil {
			t.Fatal(err)
		}
		after, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		current, err := after.Assign("a", []string{"humagen-analyst", "humagen-writer", "humagen-builder", "helper"})
		if err != nil {
			t.Fatal(err)
		}
		for _, suffix := range []string{"analyst", "writer", "builder"} {
			if current["humagen-"+suffix] != old["warp-"+suffix] {
				t.Fatalf("%s changed color", suffix)
			}
		}
		if current["helper"] != old["helper"] {
			t.Fatal("another agent moved")
		}
	})
}
