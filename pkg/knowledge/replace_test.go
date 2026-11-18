package knowledge

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

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
	s := seeded(t)
	was := docTitled(t, s, "Releases")
	if was.Digest != TextDigest("Merging to main runs the release workflow. It builds, signs and uploads the binaries.\n\nA release is tagged by the release bot, never by hand.") {
		t.Fatalf("digest = %q", was.Digest)
	}
	before := sources(t, s, "acme")[0]
	later := now.Add(1)
	got, err := s.ReplaceForScope("acme", "handbook", was.ID, "Releases, revised", "  Zeppelins are launched on Tuesdays.  ", was.Digest, later, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != was.ID || got.Title != "Releases, revised" || got.Digest != TextDigest("Zeppelins are launched on Tuesdays.") || !got.AddedAt.Equal(was.AddedAt) || got.AddedBy != "ada" || got.UpdatedAt == nil || !got.UpdatedAt.Equal(later) || got.Bytes != len("Zeppelins are launched on Tuesdays.") {
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
	s := seeded(t)
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
		if kept := docTitled(t, s, "Releases"); kept != was {
			t.Fatalf("%s: the document is %+v, was %+v", name, kept, was)
		}
		if found(t, s, "release workflow binaries") == 0 || found(t, s, "zeppelin") != 0 {
			t.Fatalf("%s: search no longer reads the old text", name)
		}
	}
}

func TestAnExcludedDocumentStaysOutOfSearchThroughAReplacement(t *testing.T) {
	s := seeded(t)
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
	s := seeded(t)
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
	if kept := docTitled(t, s, "Releases"); won != 1 || len(docs) != 2 || kept.ID != was.ID || kept.Digest != TextDigest("Zeppelins are launched on Tuesdays.") || found(t, s, "zeppelins tuesdays") != 1 {
		t.Fatalf("%d replaced, %d documents, kept %+v", won, len(docs), kept)
	}
}

// A document kept before digests were has none, and is replaced from "".
func TestADocumentWithNoDigestIsReplacedFromNone(t *testing.T) {
	s := seeded(t)
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
	if err != nil || got.ID != was.ID || got.Digest != TextDigest("Zeppelins.") {
		t.Fatalf("replaced = %+v, %v", got, err)
	}
}
