package knowledge

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

const releases = "Merging to main runs the release workflow. It builds, signs and uploads the binaries.\n\nA release is tagged by the release bot, never by hand."

// eachSeeded runs fn on a seeded store of each backend: in memory, and on
// Postgres when its test database is named.
func eachSeeded(t *testing.T, fn func(t *testing.T, s *Store)) {
	t.Helper()
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		s, err := Open(open(t))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Create("acme", "handbook", "How we work", "ada", now); err != nil {
			t.Fatal(err)
		}
		for _, d := range [][2]string{{"Releases", releases}, {"On call", "The on-call engineer acknowledges a page within five minutes."}} {
			if _, err := s.Add("acme", "handbook", d[0], "", d[1], "ada", now); err != nil {
				t.Fatal(err)
			}
		}
		fn(t, s)
	})
}

func docTitled(t *testing.T, s *Store, title string) Document {
	t.Helper()
	docs, err := s.Documents("acme", "handbook")
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range docs {
		if d.Title == title {
			return d
		}
	}
	t.Fatalf("no document titled %q in %v", title, docs)
	return Document{}
}

func found(t *testing.T, s *Store, query string) int {
	t.Helper()
	hits, err := s.Search("acme", []string{"handbook"}, query, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	return len(hits)
}

func TestReplaceKeepsTheDocumentAndChangesItsText(t *testing.T) {
	eachSeeded(t, testReplaceKeepsTheDocumentAndChangesItsText)
}

func testReplaceKeepsTheDocumentAndChangesItsText(t *testing.T, s *Store) {
	was := docTitled(t, s, "Releases")
	if was.Digest != Digest("Releases", releases) || was.Digest == Digest("Releases, revised", releases) || was.Digest == Digest("Releases", releases+".") {
		t.Fatalf("digest = %q", was.Digest)
	}
	before := sources(t, s, "acme")[0]
	later := now.Add(time.Hour)
	got, err := s.ReplaceForScope("acme", "handbook", was.ID, "Releases, revised", "  Zeppelins are launched on Tuesdays.  ", was.Digest, later, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != was.ID || got.Title != "Releases, revised" || got.Digest != Digest("Releases, revised", "Zeppelins are launched on Tuesdays.") || !got.AddedAt.Equal(was.AddedAt) || got.AddedBy != "ada" || got.UpdatedAt == nil || !got.UpdatedAt.Equal(later) || got.Bytes != len("Zeppelins are launched on Tuesdays.") {
		t.Fatalf("replaced = %+v, was %+v", got, was)
	}
	if kept := docTitled(t, s, "Releases, revised"); kept.ID != was.ID || kept.Digest != got.Digest {
		t.Fatalf("kept = %+v", kept)
	}
	if found(t, s, "zeppelins tuesdays") != 1 || found(t, s, "release workflow binaries") != 0 {
		t.Fatal("search does not read the new text alone")
	}
	after := sources(t, s, "acme")[0]
	if after.Documents != 2 || after.ContentVersion <= before.ContentVersion {
		t.Fatalf("source = %+v, was %+v", after, before)
	}
}

func TestAReplacementThatIsRefusedLeavesTheDocumentAsItWas(t *testing.T) {
	eachSeeded(t, testAReplacementThatIsRefusedLeavesTheDocumentAsItWas)
}

func testAReplacementThatIsRefusedLeavesTheDocumentAsItWas(t *testing.T, s *Store) {
	was := docTitled(t, s, "Releases")
	for name, try := range map[string]func() error{
		"a stale digest": func() error {
			_, err := s.ReplaceForScope("acme", "handbook", was.ID, "Releases", "Zeppelins.", "not-the-digest", now, nil)
			return err
		},
		"no digest where there is one": func() error {
			_, err := s.ReplaceForScope("acme", "handbook", was.ID, "Releases", "Zeppelins.", "", now, nil)
			return err
		},
		"no text": func() error {
			_, err := s.ReplaceForScope("acme", "handbook", was.ID, "Releases", "  ", was.Digest, now, nil)
			return err
		},
		"more than a source holds": func() error {
			_, err := s.ReplaceForScope("acme", "handbook", was.ID, "Releases", strings.Repeat("zeppelin ", MaxImportBytes/8), was.Digest, now, nil)
			return err
		},
		"a source whose audience changed": func() error {
			_, err := s.ReplaceForScope("acme", "handbook", was.ID, "Releases", "Zeppelins.", was.Digest, now, &SourceConfig{Provider: "manual", Mode: Indexed})
			return err
		},
		"another tenant": func() error {
			_, err := s.ReplaceForScope("globex", "handbook", was.ID, "Releases", "Zeppelins.", was.Digest, now, nil)
			return err
		},
		"a document that is not there": func() error {
			_, err := s.ReplaceForScope("acme", "handbook", "nope", "Releases", "Zeppelins.", was.Digest, now, nil)
			return err
		},
	} {
		err := try()
		if err == nil {
			t.Fatalf("%s: replaced", name)
		}
		switch name {
		case "a stale digest", "no digest where there is one":
			if !errors.Is(err, ErrChanged) {
				t.Fatalf("%s: %v", name, err)
			}
		case "another tenant", "a document that is not there":
			if !errors.Is(err, ErrNotFound) {
				t.Fatalf("%s: %v", name, err)
			}
		}
		if kept := docTitled(t, s, "Releases"); kept.ID != was.ID || kept.Digest != was.Digest || kept.Bytes != was.Bytes || kept.UpdatedAt != nil || kept.Excluded {
			t.Fatalf("%s: the document is %+v, was %+v", name, kept, was)
		}
		if found(t, s, "release workflow binaries") == 0 || found(t, s, "zeppelin") != 0 {
			t.Fatalf("%s: search no longer reads the old text", name)
		}
	}
}

func TestAnExcludedDocumentStaysOutOfSearchThroughAReplacement(t *testing.T) {
	eachSeeded(t, testAnExcludedDocumentStaysOutOfSearchThroughAReplacement)
}

func testAnExcludedDocumentStaysOutOfSearchThroughAReplacement(t *testing.T, s *Store) {
	was := docTitled(t, s, "Releases")
	if _, err := s.SetExcluded("acme", "handbook", was.ID, true, now); err != nil {
		t.Fatal(err)
	}
	// Refused: still left out, and the old text still not found.
	if _, err := s.ReplaceForScope("acme", "handbook", was.ID, "Releases", "Zeppelins.", "stale", now, nil); !errors.Is(err, ErrChanged) {
		t.Fatalf("err = %v", err)
	}
	if kept := docTitled(t, s, "Releases"); !kept.Excluded || found(t, s, "release workflow binaries") != 0 {
		t.Fatalf("after a refusal: %+v", kept)
	}
	got, err := s.ReplaceForScope("acme", "handbook", was.ID, "Releases", "Zeppelins are launched on Tuesdays.", was.Digest, now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Excluded || got.ID != was.ID || found(t, s, "zeppelins tuesdays") != 0 || found(t, s, "release workflow binaries") != 0 {
		t.Fatalf("replaced = %+v", got)
	}
	if src := sources(t, s, "acme")[0]; src.Excluded != 1 || src.Documents != 2 {
		t.Fatalf("source = %+v", src)
	}
	// Put back, it is found by its new text.
	if _, err := s.SetExcluded("acme", "handbook", was.ID, false, now); err != nil {
		t.Fatal(err)
	}
	if found(t, s, "zeppelins tuesdays") != 1 {
		t.Fatal("the new text is not found once the document is put back")
	}
}

func TestOfTwoReplacementsFromOneDigestOneIsRefused(t *testing.T) {
	eachSeeded(t, testOfTwoReplacementsFromOneDigestOneIsRefused)
}

func testOfTwoReplacementsFromOneDigestOneIsRefused(t *testing.T, s *Store) {
	was := docTitled(t, s, "Releases")
	const writers = 8
	errs := make([]error, writers)
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = s.ReplaceForScope("acme", "handbook", was.ID, "Releases", "Zeppelins are launched on Tuesdays.", was.Digest, now, nil)
		}()
	}
	wg.Wait()
	won := 0
	for _, err := range errs {
		switch {
		case err == nil:
			won++
		case !errors.Is(err, ErrChanged):
			t.Fatalf("err = %v", err)
		}
	}
	docs, _ := s.Documents("acme", "handbook")
	if kept := docTitled(t, s, "Releases"); won != 1 || len(docs) != 2 || kept.ID != was.ID || kept.Digest != Digest("Releases", "Zeppelins are launched on Tuesdays.") || found(t, s, "zeppelins tuesdays") != 1 {
		t.Fatalf("%d replaced, %d documents, kept %+v", won, len(docs), kept)
	}
}

// A document kept before digests were has none, and is replaced from "".
func TestADocumentWithNoDigestIsReplacedFromNone(t *testing.T) {
	eachSeeded(t, testADocumentWithNoDigestIsReplacedFromNone)
}

func testADocumentWithNoDigestIsReplacedFromNone(t *testing.T, s *Store) {
	was := docTitled(t, s, "Releases")
	items, err := s.kept.All("acme", docPrefix("handbook"), MaxImportDocuments)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.Value.Document.ID == was.ID {
			old := *it.Value.Document
			old.Digest = ""
			b := s.kept.Batch()
			b.PutAt("acme", it.Key, stored{Document: &old}, it.Version)
			if err := b.Commit(); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := s.ReplaceForScope("acme", "handbook", was.ID, "Releases", "Zeppelins.", was.Digest, now, nil); !errors.Is(err, ErrChanged) {
		t.Fatalf("err = %v", err)
	}
	got, err := s.ReplaceForScope("acme", "handbook", was.ID, "Releases", "Zeppelins.", "", now, nil)
	if err != nil || got.ID != was.ID || got.Digest != Digest("Releases", "Zeppelins.") {
		t.Fatalf("replaced = %+v, %v", got, err)
	}
}

// The condition covers everything a replacement replaces. A writer that read
// the document before another changed only its title, or only its text, is
// refused, and what the other wrote stays.
func TestAStaleReplacementIsRefusedWhicheverPartChanged(t *testing.T) {
	eachSeeded(t, func(t *testing.T, s *Store) {
		for _, tt := range []struct {
			name, title, text string
		}{
			{"only the title changed", "Releases, renamed", releases},
			{"only the text changed", "Releases", "Zeppelins are launched on Tuesdays."},
		} {
			docs, _ := s.Documents("acme", "handbook")
			read := docs[0]
			other, err := s.ReplaceForScope("acme", "handbook", read.ID, tt.title, tt.text, read.Digest, now, nil)
			if err != nil || other.Digest == read.Digest {
				t.Fatalf("%s: the other writer: %+v, %v", tt.name, other, err)
			}
			// The stale writer changes only a title, then only a text.
			for _, stale := range [][2]string{{"Stale title", tt.text}, {tt.title, "Stale text."}, {"Stale title", "Stale text."}} {
				if _, err := s.ReplaceForScope("acme", "handbook", read.ID, stale[0], stale[1], read.Digest, now, nil); !errors.Is(err, ErrChanged) {
					t.Fatalf("%s: a stale replacement to %q: %v", tt.name, stale, err)
				}
			}
			docs, _ = s.Documents("acme", "handbook")
			if kept := docs[0]; kept.ID != read.ID || kept.Title != tt.title || kept.Digest != Digest(tt.title, tt.text) || found(t, s, "stale") != 0 {
				t.Fatalf("%s: kept %+v", tt.name, kept)
			}
			// Read again, the same writer is taken.
			if _, err := s.ReplaceForScope("acme", "handbook", read.ID, "Releases", releases, other.Digest, now, nil); err != nil {
				t.Fatalf("%s: from the digest read again: %v", tt.name, err)
			}
		}
	})
}

// Two writers hold different states of one document and both replace it from
// the digest they read. In either order and at once, one is taken and the
// other refused, and the document is all of one state: never a part of each,
// and never the loser's over the winner's.
func TestTwoWritersOfDifferentStatesLeaveAllOfOne(t *testing.T) {
	type draft struct{ title, text string }
	older := draft{"Releases, as of Monday", "Monday. " + strings.Repeat("Cinnabar is logged. ", 400)}
	newer := draft{"Releases, as of Tuesday", "Tuesday. " + strings.Repeat("Sapphire is logged too. ", 500)}
	whole := func(t *testing.T, s *Store, id string, want draft) {
		t.Helper()
		doc, texts, err := s.DocumentTextForScope("acme", "handbook", id, nil)
		if err != nil {
			t.Fatal(err)
		}
		if doc.Title != want.title || doc.Digest != Digest(want.title, want.text) || doc.Bytes != len(strings.TrimSpace(want.text)) || strings.Join(texts, "") != strings.Join(split(strings.TrimSpace(want.text)), "") {
			t.Fatalf("the document is not all of %q: %+v, %d passages", want.title, doc, len(texts))
		}
	}
	for _, order := range []struct {
		name          string
		first, second draft
	}{{"the older first", older, newer}, {"the newer first", newer, older}} {
		t.Run(order.name, func(t *testing.T) {
			eachSeeded(t, func(t *testing.T, s *Store) {
				read := docTitled(t, s, "Releases")
				if _, err := s.ReplaceForScope("acme", "handbook", read.ID, order.first.title, order.first.text, read.Digest, now, nil); err != nil {
					t.Fatal(err)
				}
				if _, err := s.ReplaceForScope("acme", "handbook", read.ID, order.second.title, order.second.text, read.Digest, now, nil); !errors.Is(err, ErrChanged) {
					t.Fatalf("the second writer: %v", err)
				}
				whole(t, s, read.ID, order.first)
			})
		})
	}
	t.Run("at once", func(t *testing.T) {
		eachSeeded(t, func(t *testing.T, s *Store) {
			read := docTitled(t, s, "Releases")
			states := []draft{older, newer, older, newer, older, newer}
			errs := make([]error, len(states))
			var wg sync.WaitGroup
			for i, st := range states {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, errs[i] = s.ReplaceForScope("acme", "handbook", read.ID, st.title, st.text, read.Digest, now, nil)
				}()
			}
			wg.Wait()
			won := -1
			for i, err := range errs {
				switch {
				case err == nil && won < 0:
					won = i
				case err == nil:
					t.Fatalf("writers %d and %d were both taken", won, i)
				case !errors.Is(err, ErrChanged):
					t.Fatalf("writer %d: %v", i, err)
				}
			}
			if won < 0 {
				t.Fatal("no writer was taken")
			}
			whole(t, s, read.ID, states[won])
		})
	})
}
