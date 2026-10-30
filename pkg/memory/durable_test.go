package memory

import (
	"errors"
	"strings"
	"testing"

	"github.com/urmzd/humagen/pkg/blob"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestRestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		blobs := blob.NewMemory()
		s, err := openOn(st, blobs)
		if err != nil {
			t.Fatal(err)
		}
		a, _ := s.Add("acme", "lead", "Releases go out on Tuesdays.", Person, Approved, "ada", now)
		p, _ := s.Add("acme", "lead", "The team prefers short answers.", Consolidated, Pending, "run-1", now)
		gone, _ := s.Add("acme", "lead", "Forget me.", Person, Approved, "ada", now)
		if err := s.Remove("acme", "lead", gone.ID); err != nil {
			t.Fatal(err)
		}
		if n, err := s.Approve("acme", "lead", []string{p.ID}); n != 1 || err != nil {
			t.Fatalf("Approve = %d, %v", n, err)
		}
		dropped, _ := s.Add("acme", ProjectOwner("web"), "Shared.", Person, Approved, "ada", now)
		if err := s.Drop("acme", ProjectOwner("web")); err != nil {
			t.Fatal(err)
		}

		again, err := openOn(st, blobs)
		if err != nil {
			t.Fatal(err)
		}
		got := again.List("acme", "lead")
		if len(got) != 2 || got[0].ID != a.ID || got[1].ID != p.ID || got[1].Status != Approved || !got[0].At.Equal(now) {
			t.Fatalf("after restart = %+v", got)
		}
		if len(again.List("acme", ProjectOwner("web"))) != 0 {
			t.Fatal("a dropped owner came back")
		}
		// Ids are never issued twice, even those of notes that are gone.
		n, _ := again.Add("acme", "lead", "Something new.", Person, Approved, "ada", now)
		if n.ID == gone.ID || n.ID == dropped.ID || n.ID == a.ID || n.ID == p.ID {
			t.Fatalf("a restarted store issued %q again", n.ID)
		}
	})
}

func TestFailedWriteChangesNothing(t *testing.T) {
	f := statetest.NewFaulty(state.NewMemory())
	s, _ := openOn(f, blob.NewMemory())
	a, _ := s.Add("acme", "lead", "Kept.", Person, Approved, "ada", now)
	f.Fail(statetest.ErrInjected)
	if _, err := s.Add("acme", "lead", "Lost.", Person, Approved, "ada", now); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Add = %v", err)
	}
	if err := s.Remove("acme", "lead", a.ID); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Remove = %v", err)
	}
	if got := s.List("acme", "lead"); len(got) != 1 || got[0].ID != a.ID {
		t.Fatalf("after failed writes = %+v", got)
	}
	f.Fail(nil)
	n, _ := s.Add("acme", "lead", "Next.", Person, Approved, "ada", now)
	if s.next != 2 || n.ID == a.ID || !strings.HasPrefix(n.ID, "m_") {
		t.Fatalf("a failed add used an id: next is %q", n.ID)
	}
}

func TestPublicMemoryIDsRevealNoPrivateSequence(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		blobs := blob.NewMemory()
		s, err := openOn(st, blobs)
		if err != nil {
			t.Fatal(err)
		}
		a, err := s.Add("acme", "public", "First shared note", Person, Approved, "ada", now)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Add("acme", PersonalOwner("secret-person"), "Private unrelated note", Person, Approved, "secret-person", now); err != nil {
			t.Fatal(err)
		}
		b, err := s.Add("acme", "public", "Second shared note", Person, Approved, "ada", now)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(a.ID, "m_") || !strings.HasPrefix(b.ID, "m_") || a.ID == b.ID {
			t.Fatalf("public IDs expose a sequence: %q %q", a.ID, b.ID)
		}
		replay, err := openOn(st, blobs)
		if err != nil {
			t.Fatal(err)
		}
		got := replay.List("acme", "public")
		if len(got) != 2 || got[0].ID != a.ID || got[1].ID != b.ID {
			t.Fatal("opaque IDs changed on replay")
		}
	})
}
