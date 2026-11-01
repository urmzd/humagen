package memory

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/blob"
	"github.com/urmzd/humagen/pkg/drive"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func openShared(t *testing.T, st state.Store) *Store {
	t.Helper()
	s, err := openOn(st, blob.NewState(st))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Two processes on one database, each with its own store, read each other's
// notes at once, and notes written to one owner through both at the same
// time are each kept, as are the uses each counts.
func TestTwoProcessesAgree(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, two := openShared(t, a), openShared(t, b)
		first, err := one.Add("acme", "lead", "Releases go out on Tuesdays.", Person, Approved, "ada", now)
		if err != nil {
			t.Fatal(err)
		}
		if got := listOf(t, two, "acme", "lead"); len(got) != 1 || got[0].ID != first.ID {
			t.Fatalf("the other process reads %+v", got)
		}
		p, err := two.Add("acme", "lead", "The team prefers short answers.", Consolidated, Pending, "run-1", now)
		if err != nil {
			t.Fatal(err)
		}
		if got := pendingOf(t, one, "acme"); len(got) != 1 || got[0].Note.ID != p.ID {
			t.Fatalf("the other process holds %+v", got)
		}
		if n, err := one.Approve("acme", "lead", []string{p.ID}); n != 1 || err != nil {
			t.Fatalf("Approve = %d, %v", n, err)
		}

		const writers = 20
		var wg sync.WaitGroup
		for i := range writers {
			s := []*Store{one, two}[i%2]
			wg.Go(func() {
				status := Approved
				if i%4 == 1 {
					status = Pending
				}
				if _, err := s.Add("acme", "lead", fmt.Sprintf("Note number %d.", i), Person, status, "ada", now); err != nil {
					t.Error(err)
				}
				prompt, err := s.Prompt("acme", "lead")
				if err != nil {
					t.Error(err)
				}
				if err := s.UsedRendered("acme", "lead", prompt, now); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		for _, s := range []*Store{one, two} {
			notes := listOf(t, s, "acme", "lead")
			if len(notes) != writers+2 {
				t.Fatalf("%d notes, want %d", len(notes), writers+2)
			}
			seen := map[string]bool{}
			for _, n := range notes {
				if seen[n.Text] {
					t.Fatalf("a note is held twice: %q", n.Text)
				}
				seen[n.Text] = true
			}
			// Every writer counted the first note once.
			if notes[0].ID != first.ID || notes[0].Uses != writers {
				t.Fatalf("the first note = %+v, want %d uses", notes[0], writers)
			}
		}
	})
}

// Notes kept before notes were files are moved into files once, though two
// processes open the store at the same time.
func TestTwoProcessesExportOnce(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
		table := state.NewTable[stored](a, "memory")
		for _, owner := range []string{"lead", ProjectOwner("web")} {
			if err := table.Put("acme", owner, stored{Next: 3, Origins: map[string]string{"m1": "run-1"}, Notes: []Note{
				{ID: "m1", Text: "Releases go out on Tuesdays.", Source: Person, Status: Approved, By: "ada", At: at, Uses: 2},
				{ID: "m2", Text: "The team prefers short answers.", Source: Consolidated, Status: Pending, By: "run-2", At: at},
			}}); err != nil {
				t.Fatal(err)
			}
		}
		var one, two *Store
		var wg sync.WaitGroup
		wg.Go(func() { one = openShared(t, a) })
		wg.Go(func() { two = openShared(t, b) })
		wg.Wait()
		if t.Failed() {
			return
		}
		d, err := drive.Open(b, blob.NewState(b))
		if err != nil {
			t.Fatal(err)
		}
		for _, owner := range []string{"lead", ProjectOwner("web")} {
			for _, s := range []*Store{one, two} {
				notes := listOf(t, s, "acme", owner)
				if len(notes) != 2 || notes[0].ID != "m1" || notes[0].Uses != 2 || notes[1].Status != Pending {
					t.Fatalf("%s notes = %+v", owner, notes)
				}
			}
			tree, _ := TreeOf(owner)
			hist, err := d.History(context.Background(), "acme", drive.System, tree, 0, 10)
			if err != nil || len(hist) != 1 {
				t.Fatalf("%s: %d versions, %v", owner, len(hist), err)
			}
			if ps, _ := d.Proposals("acme", drive.System, tree); len(ps) != 1 {
				t.Fatalf("%s proposals = %+v", owner, ps)
			}
			if r, err := table.Lookup("acme", owner); err != nil || !r.Value.Exported || r.Value.Notes != nil {
				t.Fatalf("%s record = %+v, %v", owner, r.Value, err)
			}
		}
		if o := originsOf(t, two, "acme", []string{"run-1"}); len(o) != 2 {
			t.Fatalf("origins = %+v", o)
		}
	})
}
