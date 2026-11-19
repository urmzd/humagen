package knowledge

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/state"
)

// Every session below is made up for these tests.
const sessionURI = "chatgpt:generated-0001"

func at(minute int) *time.Time {
	t := time.Date(2026, 10, 7, 8, minute, 0, 0, time.UTC)
	return &t
}

// generated is a made-up session of turns alternating asked and answered.
func generated(turns int) Imported {
	im := Imported{Tool: ToolChatGPT, ID: "generated-0001", StartedAt: at(0), EndedAt: at(turns), Turns: []ImportedTurn{}, Branches: 2,
		Omitted: ImportedOmitted{Turns: 3, Results: 1, CutTurns: 0, Attachments: 0}}
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
		got.Session.Turns[0].Text, got.Session.StartedAt = "changed", at(59)
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
		if got := origin(t, s, doc.ID); got.Session == nil || got.ImportedBy != "ada" || got.ImportedAt == nil || !got.ImportedAt.Equal(now) || doc.StructureBytes == 0 {
			t.Fatalf("imported: %+v, %+v", got, doc)
		}
		// An ordinary edit, of the title alone or of the text, carries no
		// structure and leaves the one kept untrusted, still naming who
		// imported it. The structure itself is not kept.
		for i, edit := range [][2]string{{"A chat, renamed", text}, {"A chat, renamed", text + "\n\nA note somebody added."}} {
			was := origin(t, s, doc.ID).Document
			edited, err := s.ReplaceForScope("acme", "handbook", doc.ID, edit[0], edit[1], was.Digest, now.Add(time.Duration(i+1)*time.Minute), nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := origin(t, s, doc.ID); got.Session != nil || got.Unknown != Changed || got.ImportedBy != "ada" || got.Document.Digest != edited.Digest || edited.StructureBytes != 0 {
				t.Fatalf("after edit %d: %+v", i, got)
			}
			if rec, ok := keptStructure(t, s, "acme", "handbook", doc.ID); !ok || !rec.Edited || rec.Session != nil {
				t.Fatalf("after edit %d, kept = %+v", i, rec)
			}
		}
		// Put back to exactly the title and text it was imported with, it
		// is still not trusted: an edit came after the import, and only an
		// import writes a trusted structure.
		back, err := s.ReplaceForScope("acme", "handbook", doc.ID, "A generated chat", text, origin(t, s, doc.ID).Document.Digest, now.Add(time.Hour), nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := origin(t, s, doc.ID); back.Digest != doc.Digest || got.Session != nil || got.Unknown != Changed {
			t.Fatalf("put back: %+v", got)
		}
		// An import writes it again, as the importer asserts it.
		again, err := s.ReplaceImportedForScope("acme", "handbook", doc.ID, "A generated chat", text+" again", back.Digest, "bo", now.Add(2*time.Hour), nil, generated(2))
		if err != nil {
			t.Fatal(err)
		}
		if got := origin(t, s, doc.ID); got.Session == nil || got.ImportedBy != "bo" || got.Document.Digest != again.Digest {
			t.Fatalf("imported again: %+v", got)
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
		if _, err := s.ReplaceImportedForScope("acme", "handbook", doc.ID, "A generated chat", "four turns", "stale", "ada", now, nil, generated(4)); !errors.Is(err, ErrChanged) {
			t.Fatalf("stale digest: err = %v", err)
		}
		if got := origin(t, s, doc.ID); got.Document.Digest != doc.Digest || got.Session == nil || len(got.Session.Turns) != 2 {
			t.Fatalf("after a refusal: %+v", got)
		}
		// Refused when the structure is of another conversation than the
		// document's URI names.
		other := generated(4)
		other.ID = "generated-0002"
		if _, err := s.ReplaceImportedForScope("acme", "handbook", doc.ID, "A generated chat", "four turns", doc.Digest, "ada", now, nil, other); !errors.Is(err, ErrInvalid) {
			t.Fatalf("another conversation: err = %v", err)
		}
		if got := origin(t, s, doc.ID); got.Document.Digest != doc.Digest || got.Session == nil || len(got.Session.Turns) != 2 {
			t.Fatalf("after a refusal: %+v", got)
		}
		grown, err := s.ReplaceImportedForScope("acme", "handbook", doc.ID, "A generated chat", "four turns", doc.Digest, "ada", now.Add(time.Minute), nil, generated(4))
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
				_, errs[i] = s.ReplaceImportedForScope("acme", "handbook", doc.ID, "A generated chat", fmt.Sprintf("state %d", i), grown.Digest, "ada", now.Add(time.Hour), nil, generated(10+i))
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
			"a negative count":      change(func(im *Imported) { im.Omitted.Turns = -1 }),
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
		full.Omitted = ImportedOmitted{Turns: 1200, Results: 340, CutTurns: 2, Attachments: 5}
		doc, err := s.AddImportedForScope("acme", "handbook", "A generated chat", sessionURI, "the newest turns", "ada", now, nil, full)
		if err != nil {
			t.Fatal(err)
		}
		got := origin(t, s, doc.ID)
		if got.Session == nil || len(got.Session.Turns) != MaxImportedTurns || !reflect.DeepEqual(got.Session.Omitted, full.Omitted) || got.Session.Omitted.Turns != 1200 || doc.StructureBytes != importedSize(full) {
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
		put(importedRecord{Session: new(generated(2)), Document: Digest("A generated chat", "another text"), By: "ada", At: now})
		if got := origin(t, s, doc.ID); got.Session != nil || got.Unknown != Changed {
			t.Fatalf("another revision: %+v", got)
		}
		put(importedRecord{Session: new(generated(2)), By: "ada", At: now})
		if got := origin(t, s, doc.ID); got.Session != nil || got.Unknown != Changed {
			t.Fatalf("no digest: %+v", got)
		}
		bad := generated(2)
		bad.ID = "generated-0002"
		put(importedRecord{Session: &bad, Document: doc.Digest, By: "ada", At: now})
		if got := origin(t, s, doc.ID); got.Session != nil || got.Unknown != Malformed {
			t.Fatalf("another conversation's: %+v", got)
		}
		// One that names nobody who imported it is not trusted either.
		put(importedRecord{Session: new(generated(2)), Document: doc.Digest})
		if got := origin(t, s, doc.ID); got.Session != nil || got.Unknown != Malformed {
			t.Fatalf("no importer: %+v", got)
		}
	})
}

// marked is a structure whose first turn names the text it was written for.
func marked(text string) Imported {
	im := generated(2)
	im.Turns[0].Text = "for " + text
	return im
}

// Importers, ordinary editors and readers at once. Whenever a read answers
// with a trusted structure, it is the one written with the text whose
// digest the read's document carries.
func TestAReaderNeverTrustsAStructureOfAnotherRevision(t *testing.T) {
	eachSeeded(t, func(t *testing.T, s *Store) {
		const title = "A generated chat"
		doc, err := s.AddImportedForScope("acme", "handbook", title, sessionURI, "state 0", "ada", now, nil, marked("state 0"))
		if err != nil {
			t.Fatal(err)
		}
		var stop atomic.Bool
		var wg sync.WaitGroup
		var seq, trusted, writes atomic.Int64
		write := func(imported bool) {
			defer wg.Done()
			for !stop.Load() {
				got, err := s.OriginForScope("acme", "handbook", doc.ID, nil)
				if err != nil {
					continue
				}
				text := fmt.Sprintf("state %d", seq.Add(1))
				if imported {
					_, err = s.ReplaceImportedForScope("acme", "handbook", doc.ID, title, text, got.Document.Digest, "ada", now.Add(time.Minute), nil, marked(text))
				} else {
					_, err = s.ReplaceForScope("acme", "handbook", doc.ID, title, text, got.Document.Digest, now.Add(time.Minute), nil)
				}
				if err == nil {
					writes.Add(1)
				} else if !errors.Is(err, ErrChanged) && !errors.Is(err, state.ErrBusy) && !strings.Contains(err.Error(), "busy") {
					t.Errorf("write: %v", err)
				}
			}
		}
		read := func() {
			defer wg.Done()
			for !stop.Load() {
				got, err := s.OriginForScope("acme", "handbook", doc.ID, nil)
				if err != nil || got.Session == nil {
					continue
				}
				trusted.Add(1)
				if text := strings.TrimPrefix(got.Session.Turns[0].Text, "for "); Digest(title, text) != got.Document.Digest {
					t.Errorf("a structure written for %q was trusted with the document's digest %s", text, got.Document.Digest)
					stop.Store(true)
				}
			}
		}
		for range 3 {
			wg.Add(3)
			go write(true)
			go write(false)
			go read()
		}
		time.Sleep(2 * time.Second)
		stop.Store(true)
		wg.Wait()
		if writes.Load() == 0 || trusted.Load() == 0 {
			t.Fatalf("the race was not exercised: %d writes, %d trusted reads", writes.Load(), trusted.Load())
		}
	})
}

// A document removed and added again as the same session, without a
// structure, does not pick up the removed one's.
func TestADocumentAddedAgainAfterItsRemovalHasNoStructure(t *testing.T) {
	eachSeeded(t, func(t *testing.T, s *Store) {
		doc, err := s.AddImportedForScope("acme", "handbook", "A generated chat", sessionURI, "same text", "ada", now, nil, marked("same text"))
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Remove("acme", "handbook", doc.ID, now); err != nil {
			t.Fatal(err)
		}
		again, err := s.AddForScope("acme", "handbook", "A generated chat", sessionURI, "same text", "ada", now, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := origin(t, s, again.ID); got.Session != nil || got.Unknown != NotKept || got.ImportedBy != "" {
			t.Fatalf("added again without a structure, read %+v", got)
		}
	})
}

// The structures of one source are bounded together. Past the bound an
// import is refused whole, with a reason that says which limit, and nothing
// is cut or written; removing a document makes room again.
func TestASourcesStructuresAreBoundedTogether(t *testing.T) {
	eachSeeded(t, func(t *testing.T, s *Store) {
		big := func(id string) Imported {
			im := generated(0)
			im.ID = id
			for i := 0; importedSize(im) < MaxImportedBytes-MaxImportedTurnBytes-200; i++ {
				im.Turns = append(im.Turns, ImportedTurn{Speaker: SpeakerAssistant, Text: strings.Repeat("g", MaxImportedTurnBytes-100)})
			}
			return im
		}
		fit := MaxImportedSourceBytes / importedSize(big("generated-0000"))
		var last Document
		for i := range fit {
			id := fmt.Sprintf("generated-%04d", i)
			doc, err := s.AddImportedForScope("acme", "handbook", "A generated chat", "chatgpt:"+id, "chat "+id, "ada", now, nil, big(id))
			if err != nil {
				t.Fatalf("structure %d of %d: %v", i, fit, err)
			}
			last = doc
		}
		before, _ := s.Documents("acme", "handbook")
		_, err := s.AddImportedForScope("acme", "handbook", "A generated chat", "chatgpt:generated-over", "chat over", "ada", now, nil, big("generated-over"))
		if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "limit reached") || !strings.Contains(err.Error(), "structures") {
			t.Fatalf("past the bound: %v", err)
		}
		// A replacement that would grow past it is refused the same way,
		// and leaves the document and its structure as they were.
		grown := big(strings.TrimPrefix(last.URI, "chatgpt:"))
		grown.Turns = append(grown.Turns, ImportedTurn{Speaker: SpeakerUser, Text: strings.Repeat("g", MaxImportedTurnBytes-100)})
		if importedSize(grown) <= MaxImportedBytes {
			if _, err := s.ReplaceImportedForScope("acme", "handbook", last.ID, "A generated chat", "grown", last.Digest, "ada", now, nil, grown); err != nil && !strings.Contains(err.Error(), "limit reached") {
				t.Fatalf("growing past the bound: %v", err)
			}
		}
		if after, _ := s.Documents("acme", "handbook"); len(after) != len(before) {
			t.Fatalf("a refused import changed the documents: %d, then %d", len(before), len(after))
		}
		if err := s.Remove("acme", "handbook", last.ID, now); err != nil {
			t.Fatal(err)
		}
		if _, err := s.AddImportedForScope("acme", "handbook", "A generated chat", "chatgpt:generated-over", "chat over", "ada", now, nil, big("generated-over")); err != nil {
			t.Fatalf("after a removal: %v", err)
		}
	})
}

// A structure as an import route reads it: every field the contract
// requires is there, and a thin or loose one is refused rather than read as
// zeros.
func TestAThinStructureIsRefusedAsTheContractRequires(t *testing.T) {
	full := `{"tool":"chatgpt","id":"generated-0001","turns":[{"speaker":"user","text":"generated"}],"branches":0,"omitted":{"turns":0,"results":0,"cut_turns":0,"attachments":0}}`
	if im, err := ParseImported([]byte(full)); err != nil || im.Tool != ToolChatGPT || len(im.Turns) != 1 {
		t.Fatalf("a full structure: %+v, %v", im, err)
	}
	for name, raw := range map[string]string{
		"no turns":         `{"tool":"chatgpt","id":"generated-0001","branches":0,"omitted":{"turns":0,"results":0,"cut_turns":0,"attachments":0}}`,
		"null turns":       `{"tool":"chatgpt","id":"generated-0001","turns":null,"branches":0,"omitted":{"turns":0,"results":0,"cut_turns":0,"attachments":0}}`,
		"no omitted":       `{"tool":"chatgpt","id":"generated-0001","turns":[],"branches":0}`,
		"a count missing":  `{"tool":"chatgpt","id":"generated-0001","turns":[],"branches":0,"omitted":{"turns":0,"results":0,"cut_turns":0}}`,
		"a null count":     `{"tool":"chatgpt","id":"generated-0001","turns":[],"branches":0,"omitted":{"turns":0,"results":0,"cut_turns":null,"attachments":0}}`,
		"no branches":      `{"tool":"chatgpt","id":"generated-0001","turns":[],"omitted":{"turns":0,"results":0,"cut_turns":0,"attachments":0}}`,
		"a turn's speaker": `{"tool":"chatgpt","id":"generated-0001","turns":[{"text":"generated"}],"branches":0,"omitted":{"turns":0,"results":0,"cut_turns":0,"attachments":0}}`,
		"an unknown field": strings.Replace(full, `"branches":0`, `"branches":0,"trusted":true`, 1),
		"not an object":    `[]`,
	} {
		if _, err := ParseImported([]byte(raw)); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

// sized is a generated structure of id whose JSON is exactly n bytes.
func sized(t *testing.T, id string, n int) Imported {
	t.Helper()
	im := generated(0)
	im.ID = id
	for importedSize(im) < n-MaxImportedTurnBytes-64 {
		im.Turns = append(im.Turns, ImportedTurn{Speaker: SpeakerAssistant, Text: strings.Repeat("g", MaxImportedTurnBytes-100)})
	}
	im.Turns = append(im.Turns, ImportedTurn{Speaker: SpeakerUser, Text: "g"})
	last := &im.Turns[len(im.Turns)-1]
	last.Text = strings.Repeat("g", 1+n-importedSize(im))
	if got := importedSize(im); got != n {
		t.Fatalf("sized %d: got %d", n, got)
	}
	return im
}

// The source bound is at most MaxImportedSourceBytes: a structure that
// lands the source exactly on it is kept, one a byte more is refused.
func TestASourcesStructureBoundIsExactToTheByte(t *testing.T) {
	eachSeeded(t, func(t *testing.T, s *Store) {
		per := MaxImportedBytes - 1024
		total := 0
		for i := range MaxImportedSourceBytes / per {
			id := fmt.Sprintf("generated-%04d", i)
			doc, err := s.AddImportedForScope("acme", "handbook", "A generated chat", "chatgpt:"+id, "chat "+id, "ada", now, nil, sized(t, id, per))
			if err != nil {
				t.Fatalf("%d: %v", i, err)
			}
			total += doc.StructureBytes
		}
		rest := MaxImportedSourceBytes - total
		if _, err := s.AddImportedForScope("acme", "handbook", "A generated chat", "chatgpt:generated-over", "over", "ada", now, nil, sized(t, "generated-over", rest+1)); err == nil || !strings.Contains(err.Error(), "limit reached") {
			t.Fatalf("a byte past the bound: err = %v", err)
		}
		if _, err := s.AddImportedForScope("acme", "handbook", "A generated chat", "chatgpt:generated-edge", "edge", "ada", now, nil, sized(t, "generated-edge", rest)); err != nil {
			t.Fatalf("exactly at the bound: %v", err)
		}
	})
}

// Imports racing into the last room of a source: what is kept never passes
// the bound.
func TestRacingImportsNeverPassTheSourceBound(t *testing.T) {
	eachSeeded(t, func(t *testing.T, s *Store) {
		per := MaxImportedBytes - 1024
		for i := range MaxImportedSourceBytes/per - 2 {
			id := fmt.Sprintf("generated-%04d", i)
			if _, err := s.AddImportedForScope("acme", "handbook", "A generated chat", "chatgpt:"+id, "chat "+id, "ada", now, nil, sized(t, id, per)); err != nil {
				t.Fatal(err)
			}
		}
		racers := make([]Imported, 8)
		for i := range racers {
			racers[i] = sized(t, fmt.Sprintf("generated-race-%d", i), per)
		}
		var wg sync.WaitGroup
		for i, im := range racers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := s.AddImportedForScope("acme", "handbook", "A generated chat", "chatgpt:"+im.ID, fmt.Sprintf("race %d", i), "ada", now, nil, im)
				if err != nil && !strings.Contains(err.Error(), "limit reached") {
					t.Errorf("racer %d: %v", i, err)
				}
			}()
		}
		wg.Wait()
		docs, err := s.Documents("acme", "handbook")
		if err != nil {
			t.Fatal(err)
		}
		sum := 0
		for _, d := range docs {
			sum += d.StructureBytes
		}
		if sum > MaxImportedSourceBytes {
			t.Fatalf("racing imports passed the bound: %d", sum)
		}
	})
}

// A reimport with the title and text the document already has restores
// only its structure: the digest, the updated time, the source's content
// version and the document's vectors are as they were, and the structure is
// trusted and names who restored it. It is still taken only from the digest
// the caller read. A reimport that changes the text replaces everything, as
// before.
func TestARestoreWritesOnlyTheStructure(t *testing.T) {
	eachSeeded(t, func(t *testing.T, s *Store) {
		const title, text = "A generated chat", "the same text"
		doc, err := s.AddForScope("acme", "handbook", title, sessionURI, text, "ada", now, nil)
		if err != nil {
			t.Fatal(err)
		}
		rows, _ := s.docs("acme", "handbook")
		var seq int64
		for _, r := range rows {
			if r.doc.ID == doc.ID {
				seq = r.seq
			}
		}
		b := s.kept.Batch()
		b.Put("acme", vecKey("handbook", seq), stored{Vectors: &vectorRecord{Model: "m", Data: []string{"AAAA"}}})
		if err := b.Commit(); err != nil {
			t.Fatal(err)
		}
		before, _ := s.Get("acme", "handbook")
		if _, err := s.ReplaceImportedForScope("acme", "handbook", doc.ID, title, text, "stale", "bo", now.Add(time.Hour), nil, generated(2)); !errors.Is(err, ErrChanged) {
			t.Fatalf("a restore from a stale digest: %v", err)
		}
		restored, err := s.ReplaceImportedForScope("acme", "handbook", doc.ID, title, text, doc.Digest, "bo", now.Add(time.Hour), nil, generated(2))
		if err != nil {
			t.Fatal(err)
		}
		after, _ := s.Get("acme", "handbook")
		_, vecErr := s.kept.Lookup("acme", vecKey("handbook", seq))
		if restored.Digest != doc.Digest || restored.UpdatedAt != nil || after.ContentVersion != before.ContentVersion || vecErr != nil || restored.StructureBytes == 0 {
			t.Fatalf("a restore rewrote the revision: digest same %v, updated %v, content version %d to %d, vectors: %v, structure bytes %d",
				restored.Digest == doc.Digest, restored.UpdatedAt, before.ContentVersion, after.ContentVersion, vecErr, restored.StructureBytes)
		}
		if got := origin(t, s, doc.ID); got.Session == nil || got.ImportedBy != "bo" || got.Document.StructureBytes != restored.StructureBytes {
			t.Fatalf("restored: %+v", got)
		}
		// A real change of the text replaces everything.
		changed, err := s.ReplaceImportedForScope("acme", "handbook", doc.ID, title, "another text", doc.Digest, "bo", now.Add(2*time.Hour), nil, generated(4))
		if err != nil {
			t.Fatal(err)
		}
		later, _ := s.Get("acme", "handbook")
		_, vecErr = s.kept.Lookup("acme", vecKey("handbook", seq))
		if changed.UpdatedAt == nil || later.ContentVersion == after.ContentVersion || !errors.Is(vecErr, state.ErrNotFound) {
			t.Fatalf("a reimport that changed the text: updated %v, content version %d to %d, vectors: %v", changed.UpdatedAt, after.ContentVersion, later.ContentVersion, vecErr)
		}
	})
}
