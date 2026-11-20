package agentlooks

import (
	"errors"
	"strings"
	"testing"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// A look is kept across a restart, defaults are stored as empty, and a zero
// look returns the agent to the default.
func TestSetGetAndReset(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, _ := Open(st)
		got, err := s.Set("acme", "barbara", Look{DisplayName: "  Barbara  ", Shape: "Ghost", Face: "eyes", Hat: "crown", Finish: "fabric"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		want := Look{DisplayName: "Barbara", Shape: "ghost", Hat: "crown"}
		if got != want {
			t.Fatalf("kept %+v, want %+v", got, want)
		}
		reopened, _ := Open(st)
		looks, err := reopened.Get("acme", []string{"barbara", "rex"})
		if err != nil || len(looks) != 1 || looks["barbara"] != want {
			t.Fatalf("read %v, %v", looks, err)
		}
		if _, err := s.Set("acme", "barbara", Look{Face: "eyes"}, nil); err != nil {
			t.Fatal(err)
		}
		if looks, _ := s.Get("acme", []string{"barbara"}); len(looks) != 0 {
			t.Fatalf("a default look is still kept: %v", looks)
		}
		if looks, _ := s.Get("other", []string{"barbara"}); len(looks) != 0 {
			t.Fatalf("another workspace reads %v", looks)
		}
	})
}

// A display name is one agent's, ignoring case, and never another's handle.
func TestDisplayNamesAreUnique(t *testing.T) {
	s, _ := Open(nil)
	if _, err := s.Set("acme", "barbara", Look{DisplayName: "Barbara"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Set("acme", "reports", Look{DisplayName: "barbara"}, nil); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("a second Barbara: %v, want ErrNameTaken", err)
	}
	if _, err := s.Set("acme", "reports", Look{DisplayName: "Rex"}, []string{"rex", "reports"}); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("another agent's handle: %v, want ErrNameTaken", err)
	}
	if _, err := s.Set("acme", "rex", Look{DisplayName: "Rex"}, []string{"rex"}); err != nil {
		t.Fatalf("an agent's own handle as its name: %v", err)
	}
	if _, err := s.Set("acme", "barbara", Look{DisplayName: "Barbara", Hat: "beret"}, nil); err != nil {
		t.Fatalf("keeping its own name: %v", err)
	}
}

// Every problem is reported, and nothing invalid is kept.
func TestInvalidLooks(t *testing.T) {
	s, _ := Open(nil)
	for _, l := range []Look{
		{Shape: "dragon"},
		{Hat: "fedora"},
		{Glasses: "monocle"},
		{Face: "nose"},
		{Finish: "chrome"},
		{DisplayName: strings.Repeat("a", MaxDisplayName+1)},
		{DisplayName: "@here"},
		{DisplayName: "<b>x</b>"},
	} {
		if _, err := s.Set("acme", "barbara", l, nil); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v: %v, want ErrInvalid", l, err)
		}
	}
	if looks, _ := s.Get("acme", []string{"barbara"}); len(looks) != 0 {
		t.Fatalf("an invalid look was kept: %v", looks)
	}
	if _, err := s.Set("acme", "#looks", Look{Hat: "crown"}, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("the record's own key as a name: %v", err)
	}
}
