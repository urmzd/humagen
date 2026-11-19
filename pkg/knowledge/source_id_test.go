package knowledge

import (
	"regexp"
	"testing"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// A source's id is set when it is created and kept through every write and
// a restart; a source made again under the same name has another, and a
// source kept before ids reads as none and keeps none.
func TestASourceKeepsItsIDAndASourceMadeAgainHasAnother(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		first, err := s.Create("acme", "handbook", "How we work", "ada", now)
		if err != nil {
			t.Fatal(err)
		}
		if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(first.ID) {
			t.Fatalf("id %q", first.ID)
		}
		doc, err := s.Add("acme", "handbook", "Releases", "", "Merging to main runs the release workflow.", "ada", now)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.SetExcluded("acme", "handbook", doc.ID, true, now); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Configure("acme", "handbook", SourceConfig{Mode: Indexed, Provider: "manual", Owner: "ada"}, now); err != nil {
			t.Fatal(err)
		}
		again, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := again.Get("acme", "handbook"); err != nil || got.ID != first.ID {
			t.Fatalf("after writes and a restart: %q, %v", got.ID, err)
		}
		if err := again.Delete("acme", "handbook"); err != nil {
			t.Fatal(err)
		}
		made, err := again.Create("acme", "handbook", "Something else", "ben", now)
		if err != nil {
			t.Fatal(err)
		}
		if made.ID == "" || made.ID == first.ID {
			t.Fatalf("made again: %q, was %q", made.ID, first.ID)
		}
		// A source kept before ids: none is read, and none is given by a
		// later write.
		if _, err := again.update("acme", "handbook", func(src *Source) error { src.ID = ""; return nil }); err != nil {
			t.Fatal(err)
		}
		if _, err := again.Add("acme", "handbook", "Later", "", "A later document.", "ben", now); err != nil {
			t.Fatal(err)
		}
		if got, err := again.Get("acme", "handbook"); err != nil || got.ID != "" {
			t.Fatalf("legacy: %q, %v", got.ID, err)
		}
	})
}
