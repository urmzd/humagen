package threads_test

import (
	"errors"
	"testing"

	"github.com/urmzd/warp/pkg/state"
	"github.com/urmzd/warp/pkg/state/statetest"
	"github.com/urmzd/warp/pkg/threads"
)

var slackThread = threads.Surface{Client: "slack-acme", Channel: "C0123", TS: "1730000000.000100"}

// One surface's thread is one thread: starting it again is refused, naming
// the thread it is, and it is found by its surface. Its surface never
// changes, and another workspace's surface of the same ids is its own.
func TestOneSurfaceIsOneThread(t *testing.T) {
	s := threads.NewStore()
	in := shared("Globex", "renewals")
	in.Surface = &slackThread
	th, _ := start(t, s, "acme", in, msg("dana", "who has the PO?"))
	if _, _, err := s.Start("acme", in, msg("dana", "again"), t0); !errors.Is(err, threads.ErrExists) {
		t.Fatalf("starting a surface's thread twice = %v", err)
	}
	if got, ok := s.BySurface("acme", slackThread); !ok || got.ID != th.ID || *got.Surface != slackThread {
		t.Fatalf("by its surface = %+v, %v", got, ok)
	}
	if _, ok := s.BySurface("globex", slackThread); ok {
		t.Fatal("another workspace finds acme's thread by its surface")
	}
	if _, err := s.Update("acme", th.ID, th.Version, t0, func(t *threads.Thread) error {
		t.Surface = &threads.Surface{Client: "slack-acme", Channel: "C0123", TS: "1"}
		return nil
	}); !errors.Is(err, threads.ErrInvalid) {
		t.Fatalf("moving a thread to another surface = %v", err)
	}
	bad := shared("Globex", "renewals")
	bad.Surface = &threads.Surface{Client: "Slack Acme", Channel: "C0123", TS: "1"}
	if _, _, err := s.Start("acme", bad, msg("dana", "x"), t0); !errors.Is(err, threads.ErrInvalid) {
		t.Fatalf("a surface with no client id = %v", err)
	}
}

func TestASurfacesThreadIsFoundAfterARestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := threads.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		in := shared("Globex", "renewals")
		in.Surface = &slackThread
		th, _ := start(t, s, "acme", in, msg("dana", "who has the PO?"))
		again, err := threads.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if got, ok := again.BySurface("acme", slackThread); !ok || got.ID != th.ID {
			t.Fatalf("after a restart = %+v, %v", got, ok)
		}
	})
}
