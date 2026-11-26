package agentcolors

import (
	"errors"
	"testing"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// An agent moves to a free hue, keeps it after a restart, and frees the one
// it held; a hue another agent holds is refused.
func TestChooseMovesToAFreeHue(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, _ := Open(st)
		got, err := s.Assign("acme", []string{"scout", "writer"})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Choose("acme", "scout", got["writer"]); !errors.Is(err, ErrTaken) {
			t.Fatalf("taking writer's hue: %v, want ErrTaken", err)
		}
		free := 345.0
		for _, h := range got {
			if h == free {
				t.Fatalf("test hue %v is already held", free)
			}
		}
		old := got["scout"]
		if err := s.Choose("acme", "scout", free); err != nil {
			t.Fatal(err)
		}
		if err := s.Choose("acme", "scout", free); err != nil {
			t.Fatalf("choosing the hue it holds: %v", err)
		}
		reopened, _ := Open(st)
		again, err := reopened.Assign("acme", []string{"scout"})
		if err != nil || again["scout"] != free {
			t.Fatalf("after a restart scout is %v, %v; want %v", again, err, free)
		}
		if err := reopened.Choose("acme", "writer", old); err != nil {
			t.Fatalf("the hue scout left is free: %v", err)
		}
		if err := s.Choose("acme", "scout", 360); err == nil {
			t.Fatal("a hue of 360 was accepted")
		}
		taken, err := s.Taken("acme")
		if err != nil || len(taken) != 2 || taken["writer"] != old {
			t.Fatalf("taken is %v, %v", taken, err)
		}
	})
}
