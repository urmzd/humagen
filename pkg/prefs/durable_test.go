package prefs

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/urmzd/warp/pkg/state"
	"github.com/urmzd/warp/pkg/state/statetest"
)

func TestRestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
		if _, err := s.Set("acme", "ada", Preferences{DisplayName: "Ada", Theme: Dark, Accent: "teal"}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.SetPinned("acme", "ada", []string{"t-2", "t-1"}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.MarkOnboarded("acme", "ada", at); err != nil {
			t.Fatal(err)
		}
		if _, err := s.SetPhoto("acme", "ada", png); err != nil {
			t.Fatal(err)
		}
		if _, err := s.SetPhoto("acme", "bob", jpeg); err != nil {
			t.Fatal(err)
		}
		if _, err := s.ClearPhoto("acme", "bob"); err != nil {
			t.Fatal(err)
		}

		again, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		got := again.Get("acme", "ada")
		if got.DisplayName != "Ada" || got.Theme != Dark || got.Accent != "teal" || !got.OnboardedAt.Equal(at) ||
			len(got.Pinned) != 2 || got.Pinned[0] != "t-2" || !got.HasPhoto || got.PhotoVersion != 1 {
			t.Fatalf("after restart = %+v", got)
		}
		data, mime, err := again.Photo("acme", "ada")
		if err != nil || mime != "image/png" || !bytes.Equal(data, png) {
			t.Fatalf("photo after restart = %d bytes, %q, %v", len(data), mime, err)
		}
		if b := again.Get("acme", "bob"); b.HasPhoto || b.PhotoVersion != 2 {
			t.Fatalf("bob after restart = %+v", b)
		}
		if _, _, err := again.Photo("acme", "bob"); !errors.Is(err, ErrNoPhoto) {
			t.Fatalf("bob's cleared photo = %v", err)
		}
		if other := again.Get("other", "ada"); other.DisplayName != "" {
			t.Fatalf("another tenant sees %+v", other)
		}
	})
}

func TestFailedWriteChangesNothing(t *testing.T) {
	f := statetest.NewFaulty(state.NewMemory())
	s, err := Open(f)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Set("acme", "ada", Preferences{DisplayName: "Ada", Theme: Dark, Accent: "teal"}); err != nil {
		t.Fatal(err)
	}
	f.Fail(statetest.ErrInjected)
	if _, err := s.Set("acme", "ada", Preferences{DisplayName: "Eve", Theme: Light, Accent: "blue"}); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Set = %v", err)
	}
	if _, err := s.SetPhoto("acme", "ada", png); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("SetPhoto = %v", err)
	}
	if _, err := s.MarkOnboarded("acme", "new", time.Now()); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("MarkOnboarded = %v", err)
	}
	if got := s.Get("acme", "ada"); got.DisplayName != "Ada" || got.HasPhoto || got.PhotoVersion != 0 {
		t.Fatalf("after failed writes = %+v", got)
	}
	if got := s.Get("acme", "new"); !got.OnboardedAt.IsZero() {
		t.Fatalf("a failed write onboarded %+v", got)
	}
	f.Fail(nil)
	again, _ := Open(f)
	if got := again.Get("acme", "ada"); got.DisplayName != "Ada" {
		t.Fatalf("after restart = %+v", got)
	}
}
