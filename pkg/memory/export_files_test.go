package memory

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/blob"
	"github.com/urmzd/humagen/pkg/drive"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// Notes kept before notes were files move into files once, and opening again,
// or again after a crash between the files and the mark, writes nothing new.
func TestNotesAreExportedToFilesOnce(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st, blobs := open(t), blob.NewMemory()
		at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
		legacy := stored{Next: 7, Origins: map[string]string{"m3": "run-1"}, Notes: []Note{
			{ID: "m3", Text: "Releases go out on Tuesdays.", Source: Person, Status: Approved, By: "ada", At: at, Uses: 3, LastUsedAt: at, ReviewedBy: "ada", ReviewedAt: at, Details: Details{Kind: Decision}},
			{ID: "m5", Text: "The team prefers short answers.", Source: Consolidated, Status: Pending, By: "run-2", At: at},
			{ID: "m6", Text: "Launch is in Q4.", Source: Person, Status: Approved, By: "ada", At: at.Add(time.Hour)},
		}}
		table := state.NewTable[stored](st, "memory")
		if err := table.Put("acme", "lead", legacy); err != nil {
			t.Fatal(err)
		}
		check := func(s *Store) {
			t.Helper()
			notes := s.List("acme", "lead")
			if len(notes) != 3 || notes[0].ID != "m3" || notes[1].ID != "m5" || notes[2].ID != "m6" {
				t.Fatalf("notes = %+v", notes)
			}
			if n := notes[0]; n.Uses != 3 || n.Kind != Decision || n.ReviewedBy != "ada" || !n.At.Equal(at) || n.Status != Approved {
				t.Fatalf("an exported note lost its provenance: %+v", n)
			}
			if notes[1].Status != Pending || notes[1].By != "run-2" {
				t.Fatalf("a proposal = %+v", notes[1])
			}
			if got := s.Prompt("acme", "lead"); got != "What you remember from earlier conversations:\n- Releases go out on Tuesdays.\n- Launch is in Q4." {
				t.Fatalf("prompt = %q", got)
			}
			if o := s.ReviewedOrigins("acme", []string{"run-1"}); len(o) != 1 || o[0].Note.ID != "m3" {
				t.Fatalf("origins = %+v", o)
			}
		}
		versions := func(d *drive.Store) int {
			t.Helper()
			h, err := d.History(context.Background(), "acme", drive.System, drive.Agent("", "lead"), 0, 10)
			if err != nil {
				t.Fatal(err)
			}
			return len(h)
		}
		reopen := func() (*Store, *drive.Store) {
			t.Helper()
			d, err := drive.Open(st, blobs, drive.WithScreen(Screen))
			if err != nil {
				t.Fatal(err)
			}
			s, err := Open(st, d)
			if err != nil {
				t.Fatal(err)
			}
			return s, d
		}
		s, d := reopen()
		check(s)
		head, _ := d.Head(context.Background(), "acme", drive.System, drive.Agent("", "lead"))
		if len(head.Entries) != 2 || head.Entries[0].Path != "notes/m3.md" {
			t.Fatalf("head = %+v", head.Entries)
		}
		b, _, _ := d.ReadFile(context.Background(), "acme", drive.System, drive.Agent("", "lead"), 0, "notes/m3.md")
		if !strings.HasSuffix(string(b), "\nReleases go out on Tuesdays.\n") || !strings.Contains(string(b), "kind: decision\n") || strings.Contains(string(b), "uses") {
			t.Fatalf("file = %q", b)
		}
		if ps, _ := d.Proposals("acme", drive.System, drive.Agent("", "lead")); len(ps) != 1 || ps[0].ID != "m5" {
			t.Fatalf("proposals = %+v", ps)
		}
		raw, _ := st.Get(context.Background(), "memory", "acme", "lead")
		if strings.Contains(string(raw), "Tuesdays") || !strings.Contains(string(raw), `"exported":true`) {
			t.Fatalf("the record still holds notes: %s", raw)
		}

		s, d = reopen()
		check(s)
		if n := versions(d); n != 1 {
			t.Fatalf("opening again wrote %d versions", n)
		}

		// A crash after the files were written and before the mark: the
		// export runs again and finds nothing to do.
		if err := table.Put("acme", "lead", legacy); err != nil {
			t.Fatal(err)
		}
		s, d = reopen()
		check(s)
		if n := versions(d); n != 1 {
			t.Fatalf("a repeated export wrote %d versions", n)
		}
		if ps, _ := d.Proposals("acme", drive.System, drive.Agent("", "lead")); len(ps) != 1 {
			t.Fatalf("a repeated export proposed again: %+v", ps)
		}
	})
}

// A note written as a file by a run, without a header, is a note, and one
// approved through the drive reaches the notes without a restart.
func TestFilesWrittenElsewhereAreNotes(t *testing.T) {
	ctx := context.Background()
	d, _ := drive.Open(state.NewMemory(), blob.NewMemory(), drive.WithScreen(Screen))
	s, err := Open(nil, d)
	if err != nil {
		t.Fatal(err)
	}
	owner := PersonalOwner("ada")
	if _, err := d.Commit(ctx, "acme", drive.Actor{Subject: "ada"}, drive.Person("ada"), drive.CommitInput{Base: drive.Head, Changes: []drive.Change{
		{Path: "notes/n1.md", Data: []byte("Prefers tables.\n")},
		{Path: "notes/bad.md", Data: []byte(strings.Repeat("x", MaxChars+1))},
		{Path: "scratch.md", Data: []byte("not a note")},
	}, By: "run-9"}); err != nil {
		t.Fatal(err)
	}
	notes := s.List("acme", owner)
	if len(notes) != 1 || notes[0].Text != "Prefers tables." || notes[0].By != "run-9" || notes[0].Status != Approved {
		t.Fatalf("notes = %+v", notes)
	}
	p, err := d.Propose(ctx, "acme", drive.System, drive.Project("Field"), drive.ProposeInput{Changes: []drive.Change{{Path: "notes/n2.md", Data: []byte("Ship on Tuesday.")}}, By: "run-10"})
	if err != nil {
		t.Fatal(err)
	}
	pending := s.Pending("acme")
	if len(pending) != 1 || pending[0].Note.ID != "n2" || pending[0].Agent != ProjectOwner("Field") {
		t.Fatalf("pending = %+v", pending)
	}
	if n, err := s.Approve("acme", ProjectOwner("Field"), []string{"n2"}); n != 1 || err != nil {
		t.Fatalf("approve = %d, %v", n, err)
	}
	if ps, _ := d.Proposals("acme", drive.System, drive.Project("Field")); len(ps) != 0 {
		t.Fatalf("the drive proposal %s is still open: %+v", p.ID, ps)
	}
	if got := s.Prompt("acme", ProjectOwner("Field")); !strings.Contains(got, "Ship on Tuesday.") {
		t.Fatalf("prompt = %q", got)
	}
	// A restore through the drive is what the notes become.
	if _, err := d.Restore(ctx, "acme", drive.Actor{Subject: "ada"}, drive.Person("ada"), 1, "ada", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add("acme", owner, "Works in Toronto.", Person, Approved, "ada", time.Now()); err != nil {
		t.Fatal(err)
	}
	h, _ := d.History(ctx, "acme", drive.System, drive.Person("ada"), 0, 10)
	if _, err := d.Restore(ctx, "acme", drive.Actor{Subject: "ada"}, drive.Person("ada"), h[len(h)-1].N, "ada", time.Now()); err != nil {
		t.Fatal(err)
	}
	if notes := s.List("acme", owner); len(notes) != 1 || notes[0].Text != "Prefers tables." {
		t.Fatalf("after restore = %+v", notes)
	}
}

// Project memory is screened by saige's policy; a person's own is not
// refused by the drive, though a note still is by the notes.
func TestProjectMemoryIsScreened(t *testing.T) {
	ctx := context.Background()
	d, _ := drive.Open(state.NewMemory(), blob.NewMemory(), drive.WithScreen(Screen))
	if _, err := d.Propose(ctx, "acme", drive.System, drive.Project("Field"), drive.ProposeInput{Changes: []drive.Change{{Path: "notes/x.md", Data: []byte("Ada is at ada@example.com.")}}}); err == nil {
		t.Fatal("personal data reached project memory")
	}
	// The header of a note is not screened: a reviewer may be an address.
	if _, err := d.Propose(ctx, "acme", drive.System, drive.Project("Field"), drive.ProposeInput{Changes: []drive.Change{{Path: "notes/y.md", Data: render(Note{Text: "Ship on Tuesday.", By: "ada@example.com"})}}}); err != nil {
		t.Fatal(err)
	}
}
