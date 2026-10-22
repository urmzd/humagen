package prefs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/urmzd/warp/pkg/state"
	"github.com/urmzd/warp/pkg/state/statetest"
)

func TestMutedIsBoundedAndNeverTheWorkspace(t *testing.T) {
	s := NewStore()
	if got := s.Get("acme", "ada"); got.Muted == nil || len(got.Muted) != 0 {
		t.Fatalf("default muted: %#v", got.Muted)
	}
	got, err := s.SetMuted("acme", "ada", []string{"Globex", "Field", "Globex"})
	if err != nil || !slices.Equal(got.Muted, []string{"Globex", "Field"}) {
		t.Fatalf("got %v, %v", got.Muted, err)
	}
	// Choosing a theme or pinning leaves what is muted alone.
	if got, _ := s.Set("acme", "ada", Preferences{Theme: Dark, Accent: "teal"}); len(got.Muted) != 2 {
		t.Fatalf("set dropped muted: %v", got.Muted)
	}
	if got, _ := s.SetPinned("acme", "ada", []string{"t1"}); len(got.Muted) != 2 {
		t.Fatalf("pinning dropped muted: %v", got.Muted)
	}
	tooMany := make([]string, MaxMuted+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("p-%d", i)
	}
	for name, names := range map[string][]string{
		"the root":         {""},
		"the root address": {"~"},
		"not a name":       {"<b>"},
		"a path":           {"Field/Globex"},
		"too many":         tooMany,
	} {
		if _, err := s.SetMuted("acme", "ada", names); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: got %v", name, err)
		}
	}
	if got := s.Get("acme", "ada"); !slices.Equal(got.Muted, []string{"Globex", "Field"}) {
		t.Fatalf("a refused change was kept: %v", got.Muted)
	}
	if got := s.Get("acme", "bo"); len(got.Muted) != 0 {
		t.Fatalf("another person mutes %v", got.Muted)
	}
}

func TestMutedSurvivesARestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.SetMuted("acme", "ada", []string{"Globex"}); err != nil {
			t.Fatal(err)
		}
		// bo's preferences as they were written before muting existed.
		if err := st.Write(context.Background(), "prefs", state.Put("acme", "bo", json.RawMessage(`{"display_name":"Bo","theme":"dark","accent":"teal","has_photo":false,"photo_version":0,"pinned":[]}`))); err != nil {
			t.Fatal(err)
		}
		again, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if got := again.Get("acme", "ada").Muted; !slices.Equal(got, []string{"Globex"}) {
			t.Fatalf("after restart = %v", got)
		}
		// Preferences kept before muting existed read as muting nothing.
		if got := again.Get("acme", "bo"); got.Muted == nil || len(got.Muted) != 0 || got.DisplayName != "Bo" {
			t.Fatalf("bo after restart = %#v", got)
		}
	})
}
