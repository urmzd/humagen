package threads_test

import (
	"errors"
	"testing"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
	"github.com/urmzd/humagen/pkg/threads"
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
	if got, ok, _ := s.BySurface("acme", slackThread); !ok || got.ID != th.ID || *got.Surface != slackThread {
		t.Fatalf("by its surface = %+v, %v", got, ok)
	}
	if _, ok, _ := s.BySurface("globex", slackThread); ok {
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
		if got, ok, _ := again.BySurface("acme", slackThread); !ok || got.ID != th.ID {
			t.Fatalf("after a restart = %+v, %v", got, ok)
		}
	})
}

// A thread started from an announcement names it for good: only a surface's
// thread may, by the announcement's inbox id, it is kept across a restart,
// and no change moves it to another.
func TestAThreadStartedFromAnAnnouncementKeepsIt(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := threads.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		in := shared("Globex", "general")
		in.Surface, in.Announcement = &slackThread, "m-7"
		th, _ := start(t, s, "acme", in, msg("dana", "who has the PO?"))
		if th.Announcement != "m-7" {
			t.Fatalf("the thread names %q", th.Announcement)
		}
		if _, err := s.Update("acme", th.ID, th.Version, t0, func(t *threads.Thread) error {
			t.Announcement = "m-8"
			return nil
		}); !errors.Is(err, threads.ErrInvalid) {
			t.Fatalf("moving a thread to another announcement = %v", err)
		}
		again, err := threads.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if got, ok, _ := again.BySurface("acme", slackThread); !ok || got.Announcement != "m-7" {
			t.Fatalf("after a restart = %+v, %v", got, ok)
		}
		for name, bad := range map[string]func(*threads.Thread){
			"no surface":   func(t *threads.Thread) { t.Surface = nil },
			"not an id":    func(t *threads.Thread) { t.Announcement = "announce:Field:1" },
			"far too long": func(t *threads.Thread) { t.Announcement = "m-1234567890123456789" },
		} {
			in := shared("Globex", "general")
			in.Surface, in.Announcement = &threads.Surface{Client: "slack-acme", Channel: "C0123", TS: "2"}, "m-7"
			bad(&in)
			if _, _, err := s.Start("acme", in, msg("dana", "x"), t0); !errors.Is(err, threads.ErrInvalid) {
				t.Errorf("%s: %v", name, err)
			}
		}
	})
}
