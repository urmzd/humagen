package knowledge

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/state"
)

// Every session below is made up for these tests.
const sessionURI = "chatgpt:generated-0001"

func count(n int) *int { return &n }

func at(minute int) *time.Time {
	t := time.Date(2026, 10, 7, 8, minute, 0, 0, time.UTC)
	return &t
}

// generated is a made-up session of turns alternating asked and answered.
func generated(turns int) Imported {
	im := Imported{Tool: ToolChatGPT, ID: "generated-0001", StartedAt: at(0), EndedAt: at(turns), Turns: []ImportedTurn{}, Branches: 2,
		Omitted: ImportedOmitted{Turns: count(3), Results: count(1), CutTurns: count(0), Attachments: count(0)}}
	for i := range turns {
		speaker := SpeakerUser
		if i%2 == 1 {
			speaker = SpeakerAssistant
		}
		im.Turns = append(im.Turns, ImportedTurn{Speaker: speaker, At: at(i), Text: fmt.Sprintf("generated turn %d", i)})
	}
	return im
}

// keptStructure reads the record a document's structure is kept in.
func keptStructure(t *testing.T, s *Store, tenant, name, id string) (*importedRecord, bool) {
	t.Helper()
	rows, err := s.docs(tenant, name)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.doc.ID != id {
			continue
		}
		return structureAt(t, s, tenant, name, row.seq)
	}
	t.Fatalf("no document %q", id)
	return nil, false
}

func structureAt(t *testing.T, s *Store, tenant, name string, seq int64) (*importedRecord, bool) {
	t.Helper()
	it, err := s.kept.Lookup(tenant, importedKey(name, seq))
	if errors.Is(err, state.ErrNotFound) {
		return nil, false
	}
	if err != nil {
		t.Fatal(err)
	}
	return it.Value.Imported, true
}

func origin(t *testing.T, s *Store, id string) Origin {
	t.Helper()
	got, err := s.OriginForScope("acme", "handbook", id, nil)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestAnImportedStructureIsKeptWithTheRevisionItWasWrittenWith(t *testing.T) {
	eachSeeded(t, func(t *testing.T, s *Store) {
		want := generated(4)
		doc, err := s.AddImportedForScope("acme", "handbook", "A generated chat", sessionURI, "# A generated chat\n\n## Asked\n\ngenerated turn 0", "ada", now, nil, want)
		if err != nil {
			t.Fatal(err)
		}
		got := origin(t, s, doc.ID)
		if got.Unknown != "" || got.Session == nil || !reflect.DeepEqual(*got.Session, want) || got.Document.Digest != doc.Digest || got.Document.ID != doc.ID {
			t.Fatalf("origin = %+v, session = %+v", got, got.Session)
		}
		// What is read is a copy: a caller cannot change what is kept.
		got.Session.Turns[0].Text, *got.Session.Omitted.Turns = "changed", 99
		if again := origin(t, s, doc.ID); !reflect.DeepEqual(*again.Session, want) {
			t.Fatalf("a caller changed what is kept: %+v", again.Session)
		}
		rec, ok := keptStructure(t, s, "acme", "handbook", doc.ID)
		if !ok || rec.Document != doc.Digest {
			t.Fatalf("kept = %+v", rec)
		}
		// A document nobody imported, written as documents were before
		// structures were kept, has none.
		legacy := docTitled(t, s, "Releases")
		if got := origin(t, s, legacy.ID); got.Session != nil || got.Unknown != NotKept || got.Document.ID != legacy.ID {
			t.Fatalf("legacy origin = %+v", got)
		}
		// Leaving a document out of search does not change its revision,
		// and an excluded document is not read.
		if _, err := s.SetExcluded("acme", "handbook", doc.ID, true, now); err != nil {
			t.Fatal(err)
		}
		if _, err := s.OriginForScope("acme", "handbook", doc.ID, nil); !errors.Is(err, ErrNotFound) {
			t.Fatalf("excluded: err = %v", err)
		}
		if _, err := s.SetExcluded("acme", "handbook", doc.ID, false, now); err != nil {
			t.Fatal(err)
		}
		if got := origin(t, s, doc.ID); got.Session == nil || !reflect.DeepEqual(*got.Session, want) {
			t.Fatalf("put back: %+v", got)
		}
	})
}

func TestAnEditOfAnImportedDocumentLeavesItsStructureUntrusted(t *testing.T) {
	eachSeeded(t, func(t *testing.T, s *Store) {
		text := "# A generated chat\n\n## Asked\n\ngenerated turn 0"
		doc, err := s.AddImportedForScope("acme", "handbook", "A generated chat", sessionURI, text, "ada", now, nil, generated(2))
		if err != nil {
			t.Fatal(err)
		}
		// An ordinary edit, of the title alone or of the text, carries no
		// structure. The one kept names the digest it was written with,
		// which is no longer the document's.
		for i, edit := range [][2]string{{"A chat, renamed", text}, {"A chat, renamed", text + "\n\nA note somebody added."}} {
			was := origin(t, s, doc.ID).Document
			edited, err := s.ReplaceForScope("acme", "handbook", doc.ID, edit[0], edit[1], was.Digest, now.Add(time.Duration(i+1)*time.Minute), nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := origin(t, s, doc.ID); got.Session != nil || got.Unknown != Changed || got.Document.Digest != edited.Digest {
				t.Fatalf("after edit %d: %+v", i, got)
			}
			if rec, ok := keptStructure(t, s, "acme", "handbook", doc.ID); !ok || rec.Document != doc.Digest {
				t.Fatalf("after edit %d, kept = %+v", i, rec)
			}
		}
		// The structure describes the title and text it was written with:
		// put back to exactly those, the document is what it describes
		// again. Any other text is not.
		back, err := s.ReplaceForScope("acme", "handbook", doc.ID, "A generated chat", text, origin(t, s, doc.ID).Document.Digest, now.Add(time.Hour), nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := origin(t, s, doc.ID); back.Digest != doc.Digest || got.Session == nil || !reflect.DeepEqual(*got.Session, generated(2)) {
			t.Fatalf("put back: %+v", got)
		}
	})
}

func TestAnImportReplacesADocumentAndItsStructureTogether(t *testing.T) {
	eachSeeded(t, func(t *testing.T, s *Store) {
		doc, err := s.AddImportedForScope("acme", "handbook", "A generated chat", sessionURI, "two turns", "ada", now, nil, generated(2))
		if err != nil {
			t.Fatal(err)
		}
		// Refused from a digest the document does not have: neither the
		// document nor its structure changes.
		if _, err := s.ReplaceImportedForScope("acme", "handbook", doc.ID, "A generated chat", "four turns", "stale", now, nil, generated(4)); !errors.Is(err, ErrChanged) {
			t.Fatalf("stale digest: err = %v", err)
		}
		if got := origin(t, s, doc.ID); got.Document.Digest != doc.Digest || got.Session == nil || len(got.Session.Turns) != 2 {
			t.Fatalf("after a refusal: %+v", got)
		}
		// Refused when the structure is of another conversation than the
		// document's URI names.
		other := generated(4)
		other.ID = "generated-0002"
		if _, err := s.ReplaceImportedForScope("acme", "handbook", doc.ID, "A generated chat", "four turns", doc.Digest, now, nil, other); !errors.Is(err, ErrInvalid) {
			t.Fatalf("another conversation: err = %v", err)
		}
		if got := origin(t, s, doc.ID); got.Document.Digest != doc.Digest || got.Session == nil || len(got.Session.Turns) != 2 {
			t.Fatalf("after a refusal: %+v", got)
		}
		grown, err := s.ReplaceImportedForScope("acme", "handbook", doc.ID, "A generated chat", "four turns", doc.Digest, now.Add(time.Minute), nil, generated(4))
		if err != nil {
			t.Fatal(err)
		}
		got := origin(t, s, doc.ID)
		if grown.ID != doc.ID || got.Document.Digest != grown.Digest || got.Session == nil || !reflect.DeepEqual(*got.Session, generated(4)) {
			t.Fatalf("grown: %+v, session %+v", got, got.Session)
		}

		// Two imports at once, each of another state of the session, from
		// the one digest: one is taken, and the structure kept is the one
		// written with the text kept.
		const writers = 6
		errs := make([]error, writers)
		var wg sync.WaitGroup
		for i := range writers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, errs[i] = s.ReplaceImportedForScope("acme", "handbook", doc.ID, "A generated chat", fmt.Sprintf("state %d", i), grown.Digest, now.Add(time.Hour), nil, generated(10+i))
			}()
		}
		wg.Wait()
		won := -1
		for i, err := range errs {
			switch {
			case err == nil && won < 0:
				won = i
			case err == nil:
				t.Fatalf("two were taken: %d and %d", won, i)
			case !errors.Is(err, ErrChanged):
				t.Fatalf("writer %d: %v", i, err)
			}
		}
		if won < 0 {
			t.Fatal("none was taken")
		}
		got = origin(t, s, doc.ID)
		if got.Document.Digest != Digest("A generated chat", fmt.Sprintf("state %d", won)) || got.Session == nil || len(got.Session.Turns) != 10+won {
			t.Fatalf("writer %d won, kept %+v with %d turns", won, got.Document, len(got.Session.Turns))
		}
	})
}

func TestRemovingADocumentOrItsSourceRemovesItsStructure(t *testing.T) {
	eachSeeded(t, func(t *testing.T, s *Store) {
		var seqs []int64
		var ids []string
		for i := range 2 {
			im := generated(2)
			im.ID = fmt.Sprintf("generated-%04d", i)
			doc, err := s.AddImportedForScope("acme", "handbook", "A generated chat", "chatgpt:"+im.ID, fmt.Sprintf("chat %d", i), "ada", now, nil, im)
			if err != nil {
				t.Fatal(err)
			}
			ids = append(ids, doc.ID)
		}
		rows, err := s.docs("acme", "handbook")
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if row.doc.URI != "" {
				seqs = append(seqs, row.seq)
			}
		}
		if len(seqs) != 2 {
			t.Fatalf("rows = %+v", rows)
		}
		for _, seq := range seqs {
			if _, ok := structureAt(t, s, "acme", "handbook", seq); !ok {
				t.Fatalf("no structure at %d", seq)
			}
		}
		if err := s.Remove("acme", "handbook", ids[0], now); err != nil {
			t.Fatal(err)
		}
		if _, ok := structureAt(t, s, "acme", "handbook", seqs[0]); ok {
			t.Fatal("a removed document's structure is still kept")
		}
		if _, err := s.OriginForScope("acme", "handbook", ids[0], nil); !errors.Is(err, ErrNotFound) {
			t.Fatalf("removed: err = %v", err)
		}
		if _, ok := structureAt(t, s, "acme", "handbook", seqs[1]); !ok {
			t.Fatal("removing one document removed another's structure")
		}
		if err := s.Delete("acme", "handbook"); err != nil {
			t.Fatal(err)
		}
		if _, ok := structureAt(t, s, "acme", "handbook", seqs[1]); ok {
			t.Fatal("a deleted source's structure is still kept")
		}
		// A source made again under the name holds none of it.
		if _, err := s.Create("acme", "handbook", "", "ada", now); err != nil {
			t.Fatal(err)
		}
		if _, err := s.OriginForScope("acme", "handbook", ids[1], nil); !errors.Is(err, ErrNotFound) {
			t.Fatalf("after the source was made again: err = %v", err)
		}
	})
}

func TestAStructureIsItsTenantsAndItsSourcesAlone(t *testing.T) {
	eachSeeded(t, func(t *testing.T, s *Store) {
		doc, err := s.AddImportedForScope("acme", "handbook", "A generated chat", sessionURI, "two turns", "ada", now, nil, generated(2))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Create("globex", "handbook", "", "bo", now); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Create("acme", "other", "", "ada", now); err != nil {
			t.Fatal(err)
		}
		for _, tt := range []struct{ tenant, source string }{{"globex", "handbook"}, {"globex", "nope"}, {"acme", "other"}, {"acme", "nope"}} {
			if got, err := s.OriginForScope(tt.tenant, tt.source, doc.ID, nil); !errors.Is(err, ErrNotFound) {
				t.Fatalf("%s/%s: origin = %+v, err = %v", tt.tenant, tt.source, got, err)
			}
		}
		// A source whose configuration is not the one the reader was
		// authorized for is not read.
		if _, err := s.OriginForScope("acme", "handbook", doc.ID, &SourceConfig{Provider: "manual", Mode: Indexed}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("another configuration: err = %v", err)
		}
	})
}

func TestAStructurePastItsBoundsIsRefusedAndNothingIsWritten(t *testing.T) {
	eachSeeded(t, func(t *testing.T, s *Store) {
		change := func(fn func(*Imported)) Imported {
			im := generated(2)
			fn(&im)
			return im
		}
		big := strings.Repeat("x", MaxImportedTurnBytes)
		for name, im := range map[string]Imported{
			"an unknown tool":       change(func(im *Imported) { im.Tool = "other" }),
			"no tool":               change(func(im *Imported) { im.Tool = "" }),
			"no id":                 change(func(im *Imported) { im.ID = "" }),
			"another id":            change(func(im *Imported) { im.ID = "generated-0002" }),
			"another tool":          change(func(im *Imported) { im.Tool = ToolClaude }),
			"a long id":             change(func(im *Imported) { im.ID = strings.Repeat("x", MaxImportedID+1) }),
			"too many turns":        generated(MaxImportedTurns + 1),
			"an unknown speaker":    change(func(im *Imported) { im.Turns[0].Speaker = "system" }),
			"an empty turn":         change(func(im *Imported) { im.Turns[0].Text = "" }),
			"a long turn":           change(func(im *Imported) { im.Turns[0].Text = big + "x" }),
			"a zero byte":           change(func(im *Imported) { im.Turns[0].Text = "a\x00b" }),
			"text that is not text": change(func(im *Imported) { im.Turns[0].Text = "a\xffb" }),
			"a negative count":      change(func(im *Imported) { im.Omitted.Turns = count(-1) }),
			"negative branches":     change(func(im *Imported) { im.Branches = -1 }),
			"too many bytes": change(func(im *Imported) {
				for range MaxImportedBytes/MaxImportedTurnBytes + 1 {
					im.Turns = append(im.Turns, ImportedTurn{Speaker: SpeakerAssistant, Text: big})
				}
			}),
		} {
			uri := sessionURI
			if name == "a long id" {
				uri = "chatgpt:" + im.ID
			}
			if _, err := s.AddImportedForScope("acme", "handbook", "A generated chat", uri, "two turns", "ada", now, nil, im); !errors.Is(err, ErrInvalid) {
				t.Errorf("%s: err = %v", name, err)
			}
		}
		if docs, err := s.Documents("acme", "handbook"); err != nil || len(docs) != 2 {
			t.Fatalf("a refused structure left a document: %v, %v", docs, err)
		}
		// At the bounds it is kept, with every count it states: one the
		// import knows, and one it does not.
		full := generated(MaxImportedTurns)
		full.Omitted = ImportedOmitted{Turns: count(1200), Results: count(340), CutTurns: count(2), Attachments: nil}
		doc, err := s.AddImportedForScope("acme", "handbook", "A generated chat", sessionURI, "the newest turns", "ada", now, nil, full)
		if err != nil {
			t.Fatal(err)
		}
		got := origin(t, s, doc.ID)
		if got.Session == nil || len(got.Session.Turns) != MaxImportedTurns || !reflect.DeepEqual(got.Session.Omitted, full.Omitted) || got.Session.Omitted.Attachments != nil || *got.Session.Omitted.Turns != 1200 {
			t.Fatalf("at the bounds: %+v", got.Session)
		}
	})
}

func TestAStructureOfAnotherRevisionIsNotTrusted(t *testing.T) {
	eachSeeded(t, func(t *testing.T, s *Store) {
		doc, err := s.AddImportedForScope("acme", "handbook", "A generated chat", sessionURI, "two turns", "ada", now, nil, generated(2))
		if err != nil {
			t.Fatal(err)
		}
		rows, err := s.docs("acme", "handbook")
		if err != nil {
			t.Fatal(err)
		}
		seq := rows[len(rows)-1].seq
		put := func(rec importedRecord) {
			t.Helper()
			b := s.kept.Batch()
			b.Put("acme", importedKey("handbook", seq), stored{Imported: &rec})
			if err := b.Commit(); err != nil {
				t.Fatal(err)
			}
		}
		// Records no write of this store makes: a structure kept with
		// another revision's digest, with none, and one that is not valid.
		put(importedRecord{Session: generated(2), Document: Digest("A generated chat", "another text")})
		if got := origin(t, s, doc.ID); got.Session != nil || got.Unknown != Changed {
			t.Fatalf("another revision: %+v", got)
		}
		put(importedRecord{Session: generated(2)})
		if got := origin(t, s, doc.ID); got.Session != nil || got.Unknown != Changed {
			t.Fatalf("no digest: %+v", got)
		}
		bad := generated(2)
		bad.ID = "generated-0002"
		put(importedRecord{Session: bad, Document: doc.Digest})
		if got := origin(t, s, doc.ID); got.Session != nil || got.Unknown != Malformed {
			t.Fatalf("another conversation's: %+v", got)
		}
	})
}
