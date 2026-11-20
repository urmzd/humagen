package knowledge

import (
	"errors"
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

// olderServerRewrites writes a source's record back as a server on an
// earlier release does: decoded without the id it does not know, and
// written again.
func olderServerRewrites(t *testing.T, st state.Store, tenant, name string) {
	t.Helper()
	if _, err := state.NewTable[map[string]any](st, namespace).Update(tenant, sourceKey(name), func(r map[string]any, ok bool) (map[string]any, error) {
		if src, _ := r["source"].(map[string]any); src != nil {
			delete(src, "id")
		}
		return r, nil
	}); err != nil {
		t.Fatal(err)
	}
}

// A source with no id, kept before ids or rewritten by a server on an
// earlier release, is given one once: the id is kept through a restart, a
// second call keeps it, and a source that is not there is not found.
func TestASourceWithNoIDIsGivenOneOnce(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Create("acme", "handbook", "How we work", "ada", now); err != nil {
			t.Fatal(err)
		}
		olderServerRewrites(t, st, "acme", "handbook")
		if got, err := s.Get("acme", "handbook"); err != nil || got.ID != "" {
			t.Fatalf("after an older server's write: %q, %v", got.ID, err)
		}
		given, err := s.GiveID("acme", "handbook")
		if err != nil || !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(given.ID) {
			t.Fatalf("given %q, %v", given.ID, err)
		}
		again, err := s.GiveID("acme", "handbook")
		if err != nil || again.ID != given.ID {
			t.Fatalf("given again %q, was %q, %v", again.ID, given.ID, err)
		}
		reopened, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := reopened.Get("acme", "handbook"); err != nil || got.ID != given.ID || got.Description != "How we work" {
			t.Fatalf("after a restart: %+v, %v", got, err)
		}
		if _, err := reopened.GiveID("acme", "no-such-source"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("a source that is not there: %v", err)
		}
	})
}
