package prefs

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestProjectPinsAreIndependentBoundedAndPrivate(t *testing.T) {
	s := NewStore()
	if got := get(t, s, "acme", "ada").PinnedProjects; got == nil || len(got) != 0 {
		t.Fatalf("default project pins = %#v", got)
	}
	if _, err := s.SetPinned("acme", "ada", []string{"t1"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.SetPinnedProjects("acme", "ada", []string{"Launch", "~", "Launch"})
	if err != nil || !slices.Equal(got.PinnedProjects, []string{"Launch", "~"}) || !slices.Equal(got.Pinned, []string{"t1"}) {
		t.Fatalf("pin project = %+v, %v", got, err)
	}
	got.PinnedProjects[0] = "Changed"
	if got := get(t, s, "acme", "ada").PinnedProjects; got[0] != "Launch" {
		t.Fatalf("caller mutated stored project pins: %v", got)
	}
	for _, names := range [][]string{{""}, {"bad/path"}, {"<b>"}, make([]string, MaxPinned+1)} {
		if _, err := s.SetPinnedProjects("acme", "ada", names); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid project pins %v: %v", names, err)
		}
	}
	if _, err := s.SetPinned("acme", "ada", nil); err != nil {
		t.Fatal(err)
	}
	got, err = s.Set("acme", "ada", Preferences{Theme: Dark, Accent: "teal"})
	if err != nil || !slices.Equal(got.PinnedProjects, []string{"Launch", "~"}) {
		t.Fatalf("another preference dropped project pins: %+v, %v", got, err)
	}
	if len(get(t, s, "acme", "bo").PinnedProjects) != 0 || len(get(t, s, "other", "ada").PinnedProjects) != 0 {
		t.Fatal("project pins escaped their person or tenant")
	}
}

func TestProjectPinsSurviveRestartAndOlderPreferences(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.SetPinnedProjects("acme", "ada", []string{"Launch", "~"}); err != nil {
			t.Fatal(err)
		}
		if err := st.Write(context.Background(), "prefs", state.Put("acme", "bo", json.RawMessage(`{"display_name":"Bo","theme":"dark","accent":"teal","pinned":["t1"]}`))); err != nil {
			t.Fatal(err)
		}
		again, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if got := get(t, again, "acme", "ada").PinnedProjects; !slices.Equal(got, []string{"Launch", "~"}) {
			t.Fatalf("after restart = %v", got)
		}
		if got := get(t, again, "acme", "bo"); got.PinnedProjects == nil || len(got.PinnedProjects) != 0 || !slices.Equal(got.Pinned, []string{"t1"}) {
			t.Fatalf("older preferences = %+v", got)
		}
	})
}
