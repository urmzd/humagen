package drive_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/urmzd/humagen/pkg/blob"
	"github.com/urmzd/humagen/pkg/drive"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func openOn(t *testing.T, st state.Store) *drive.Store {
	t.Helper()
	d, err := drive.Open(st, blob.NewState(st))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// Two processes on one database, each with its own store, see each other's
// commits and proposals at once, and commits to one tree from both at the
// same time are each kept, one version each, in one line of parents.
func TestTwoProcessesAgree(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		ctx := context.Background()
		one, two := openOn(t, a), openOn(t, b)
		tree := drive.Project("Field")
		if _, err := one.Commit(ctx, "acme", drive.System, tree, drive.CommitInput{Base: drive.Head, Changes: []drive.Change{put("a.md", "first")}}); err != nil {
			t.Fatal(err)
		}
		if got := read(t, two, "acme", drive.System, tree, 0, "a.md"); got != "first" {
			t.Fatalf("the other process reads %q", got)
		}
		p, err := two.Propose(ctx, "acme", drive.System, tree, drive.ProposeInput{Base: drive.Head, Changes: []drive.Change{put("b.md", "proposed")}})
		if err != nil {
			t.Fatal(err)
		}
		if held, err := one.Proposals("acme", drive.System, tree); err != nil || len(held) != 1 || held[0].ID != p.ID {
			t.Fatalf("the other process holds %+v, %v", held, err)
		}
		trees, _, err := two.Trees(ctx, "acme", "", 10)
		if err != nil || len(trees) != 1 || trees[0] != tree {
			t.Fatalf("trees = %+v, %v", trees, err)
		}
		if _, err := one.Approve(ctx, "acme", drive.System, tree, p.ID, "ada", at); err != nil {
			t.Fatal(err)
		}
		if held, _ := two.Proposals("acme", drive.System, tree); len(held) != 0 {
			t.Fatalf("an approved proposal is open in the other process: %+v", held)
		}

		const writers = 24
		var wg sync.WaitGroup
		for i := range writers {
			d := []*drive.Store{one, two}[i%2]
			wg.Go(func() {
				if _, err := d.Commit(ctx, "acme", drive.System, tree, drive.CommitInput{Base: drive.Head, Changes: []drive.Change{put(fmt.Sprintf("f%02d.md", i), "x")}}); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		for _, d := range []*drive.Store{one, two} {
			head, err := d.Head(ctx, "acme", drive.System, tree)
			if err != nil || head.N != writers+2 || len(head.Entries) != writers+2 {
				t.Fatalf("head = version %d with %d files, %v", head.N, len(head.Entries), err)
			}
			hist, err := d.History(ctx, "acme", drive.System, tree, 0, drive.MaxPage)
			if err != nil || len(hist) != writers+2 {
				t.Fatalf("history = %d versions, %v", len(hist), err)
			}
			for i, v := range hist {
				if v.N != int64(writers+2-i) || v.Parent != v.N-1 {
					t.Fatalf("version %d = %d with parent %d", i, v.N, v.Parent)
				}
			}
		}
	})
}

// Two processes committing to one path from the same parent cannot both
// win: one writes the next version and the other is told of the conflict.
func TestTwoProcessesCannotBothWinFromOneParent(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		ctx := context.Background()
		one, two := openOn(t, a), openOn(t, b)
		tree := drive.Person("ada")
		ada := drive.Actor{Subject: "ada"}
		for round := range 8 {
			base, err := one.Head(ctx, "acme", ada, tree)
			if err != nil {
				t.Fatal(err)
			}
			var wg sync.WaitGroup
			errs := make([]error, 2)
			start := make(chan struct{})
			for i, d := range []*drive.Store{one, two} {
				wg.Go(func() {
					<-start
					_, errs[i] = d.Commit(ctx, "acme", ada, tree, drive.CommitInput{Base: base.N, Changes: []drive.Change{put("plan.md", fmt.Sprintf("round %d by %d", round, i))}})
				})
			}
			close(start)
			wg.Wait()
			won := 0
			for _, err := range errs {
				switch {
				case err == nil:
					won++
				case !errors.Is(err, drive.ErrConflict):
					t.Fatal(err)
				}
			}
			head, _ := two.Head(ctx, "acme", ada, tree)
			if won != 1 || head.N != base.N+1 || head.Parent != base.N {
				t.Fatalf("round %d: %d won, head %d from %d", round, won, head.N, head.Parent)
			}
		}
	})
}

// A process that cached a tree reads the tree another process dropped and
// wrote again as it now is, the same as a process with nothing cached.
func TestDroppingTheCacheChangesNothing(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		ctx := context.Background()
		one, two := openOn(t, a), openOn(t, b)
		tree := drive.Project("Field")
		if _, err := one.Commit(ctx, "acme", drive.System, tree, drive.CommitInput{Base: drive.Head, Changes: []drive.Change{put("a.md", "old life renewal")}}); err != nil {
			t.Fatal(err)
		}
		if got := read(t, one, "acme", drive.System, tree, 0, "a.md"); got != "old life renewal" {
			t.Fatal(got)
		}
		if hits, _ := one.Search(ctx, "acme", drive.System, []drive.Tree{tree}, "renewal", 5); len(hits) != 1 {
			t.Fatalf("hits = %+v", hits)
		}
		if err := two.Drop(ctx, "acme", tree, "test"); err != nil {
			t.Fatal(err)
		}
		if _, err := two.Commit(ctx, "acme", drive.System, tree, drive.CommitInput{Base: drive.Head, Changes: []drive.Change{put("a.md", "new life")}}); err != nil {
			t.Fatal(err)
		}
		fresh := openOn(t, a)
		for name, d := range map[string]*drive.Store{"cached": one, "fresh": fresh} {
			if got := read(t, d, "acme", drive.System, tree, 1, "a.md"); got != "new life" {
				t.Fatalf("%s reads version 1 as %q", name, got)
			}
			if hits, _ := d.Search(ctx, "acme", drive.System, []drive.Tree{tree}, "renewal", 5); len(hits) != 0 {
				t.Fatalf("%s searched the dropped life: %+v", name, hits)
			}
		}
	})
}

// Trees written before the directory, with versions kept apart from their
// heads and heads that do not name their proposals, are listed once though
// two processes open the store at the same time, and read and written as
// they always were.
func TestTwoProcessesListOldTreesOnce(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		ctx := context.Background()
		tree := drive.Agent("", "lead")
		other := drive.Project("Field")
		blobs := blob.NewState(a)
		// Content, as an older release kept it.
		hash := func(data string) string {
			d, _ := drive.Open(state.NewMemory(), blobs)
			v, err := d.Commit(ctx, "acme", drive.System, drive.Project("seed"), drive.CommitInput{Base: drive.Head, Changes: []drive.Change{put("x", data)}})
			if err != nil {
				t.Fatal(err)
			}
			return v.Entries[0].Hash
		}
		h1 := hash("remembered")
		h2 := hash("proposed")
		raw := func(v any) []byte {
			b, _ := json.Marshal(v)
			return b
		}
		v1 := drive.Version{Tree: tree, N: 1, Entries: []drive.Entry{{Path: "notes/a.md", Hash: h1, Size: 10}}, By: "ada", At: at}
		if err := a.Write(ctx, "drive.versions", state.Put("acme", tree.Key()+"#v/"+state.Seq(1), raw(v1))); err != nil {
			t.Fatal(err)
		}
		if err := a.Write(ctx, "drive.trees",
			state.Put("acme", tree.Key()+"#h", raw(map[string]any{"tree": tree, "version": 1, "oldest": 1})),
			state.Put("acme", tree.Key()+"#p/p1", raw(drive.Proposal{ID: "p1", Tree: tree, Base: 1, Changes: []drive.Change{{Path: "notes/b.md", Hash: h2, Size: 8}}, At: at})),
			state.Put("globex", other.Key()+"#p/p2", raw(drive.Proposal{ID: "p2", Tree: other, Changes: []drive.Change{{Path: "c.md", Hash: h2, Size: 8}}, At: at})),
		); err != nil {
			t.Fatal(err)
		}

		var one, two *drive.Store
		var wg sync.WaitGroup
		wg.Go(func() { one = openOn(t, a) })
		wg.Go(func() { two = openOn(t, b) })
		wg.Wait()
		if t.Failed() {
			return
		}
		for _, c := range []struct {
			tenant string
			want   drive.Tree
		}{{"acme", tree}, {"globex", other}} {
			trees, _, err := two.Trees(ctx, c.tenant, "", 10)
			if err != nil || len(trees) != 1 || trees[0] != c.want {
				t.Fatalf("%s trees = %+v, %v", c.tenant, trees, err)
			}
		}
		if got := read(t, two, "acme", drive.System, tree, 0, "notes/a.md"); got != "remembered" {
			t.Fatalf("an old head reads %q", got)
		}
		if _, err := one.Approve(ctx, "acme", drive.System, tree, "p1", "ada", at); err != nil {
			t.Fatal(err)
		}
		if got := read(t, two, "acme", drive.System, tree, 1, "notes/a.md"); got != "remembered" {
			t.Fatalf("an old version reads %q", got)
		}
		if got := read(t, two, "acme", drive.System, tree, 2, "notes/b.md"); got != "proposed" {
			t.Fatalf("the approved old proposal reads %q", got)
		}
		if held, _ := two.Proposals("globex", drive.System, other); len(held) != 1 {
			t.Fatalf("an old proposal on a tree with no head = %+v", held)
		}
		if err := two.Drop(ctx, "globex", other, "test"); err != nil {
			t.Fatal(err)
		}
		if held, _ := one.Proposals("globex", drive.System, other); len(held) != 0 {
			t.Fatalf("a dropped tree holds %+v", held)
		}
	})
}
