package drive_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/blob"
	"github.com/urmzd/humagen/pkg/drive"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

var at = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

func put(p, data string) drive.Change { return drive.Change{Path: p, Data: []byte(data)} }

func read(t *testing.T, d *drive.Store, tenant string, who drive.Actor, tree drive.Tree, version int64, p string) string {
	t.Helper()
	b, _, err := d.ReadFile(context.Background(), tenant, who, tree, version, p)
	if err != nil {
		t.Fatalf("read %s@%d: %v", p, version, err)
	}
	return string(b)
}

// countingBlobs records each tenant and key a put reaches the blob store
// with.
type countingBlobs struct {
	blob.Store
	mu   sync.Mutex
	puts map[string]int
}

func (c *countingBlobs) Put(ctx context.Context, tenant, key string, r io.Reader, max int64) (blob.Info, error) {
	c.mu.Lock()
	c.puts[tenant+"/"+key]++
	c.mu.Unlock()
	return c.Store.Put(ctx, tenant, key, r, max)
}

func TestVersionsRestoreAndSurviveARestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		ctx := context.Background()
		st, blobs := open(t), blob.NewMemory()
		d, err := drive.Open(st, blobs)
		if err != nil {
			t.Fatal(err)
		}
		tree := drive.Person("ada")
		ada := drive.Actor{Subject: "ada"}
		v1, err := d.Commit(ctx, "acme", ada, tree, drive.CommitInput{Base: 0, Changes: []drive.Change{put("notes/a.md", "one"), put("plan.md", "draft")}, By: "ada", At: at})
		if err != nil || v1.N != 1 || len(v1.Entries) != 2 {
			t.Fatalf("v1 = %+v, %v", v1, err)
		}
		v2, err := d.Commit(ctx, "acme", ada, tree, drive.CommitInput{Base: 1, Changes: []drive.Change{put("notes/a.md", "two"), {Path: "plan.md", Delete: true}}, By: "ada", At: at})
		if err != nil || v2.N != 2 || v2.Parent != 1 || len(v2.Entries) != 1 {
			t.Fatalf("v2 = %+v, %v", v2, err)
		}
		// The same content again writes no version.
		same, err := d.Commit(ctx, "acme", ada, tree, drive.CommitInput{Base: drive.Head, Changes: []drive.Change{put("notes/a.md", "two")}})
		if err != nil || same.N != 2 {
			t.Fatalf("a commit that changes nothing = %+v, %v", same, err)
		}
		// A change based on a version the path has changed since conflicts.
		if _, err := d.Commit(ctx, "acme", ada, tree, drive.CommitInput{Base: 1, Changes: []drive.Change{put("notes/a.md", "late")}}); !errors.Is(err, drive.ErrConflict) {
			t.Fatalf("stale base = %v", err)
		}
		// But one to a path nobody touched since applies.
		if _, err := d.Commit(ctx, "acme", ada, tree, drive.CommitInput{Base: 1, Changes: []drive.Change{put("other.md", "x")}}); err != nil {
			t.Fatal(err)
		}
		r, err := d.Restore(ctx, "acme", ada, tree, 1, "ada", at)
		if err != nil || r.N != 4 || r.Restored != 1 {
			t.Fatalf("restore = %+v, %v", r, err)
		}
		if got := read(t, d, "acme", ada, tree, 0, "notes/a.md"); got != "one" {
			t.Fatalf("after restore, head reads %q", got)
		}
		if got := read(t, d, "acme", ada, tree, 2, "notes/a.md"); got != "two" {
			t.Fatalf("version 2 reads %q", got)
		}

		again, err := drive.Open(st, blobs)
		if err != nil {
			t.Fatal(err)
		}
		hist, err := again.History(ctx, "acme", ada, tree, 0, 10)
		if err != nil || len(hist) != 4 || hist[0].N != 4 || hist[3].N != 1 || hist[0].Entries != nil {
			t.Fatalf("history after restart = %+v, %v", hist, err)
		}
		if got := read(t, again, "acme", ada, tree, 0, "plan.md"); got != "draft" {
			t.Fatalf("restored file after restart = %q", got)
		}
		page, _ := again.History(ctx, "acme", ada, tree, 3, 1)
		if len(page) != 1 || page[0].N != 2 {
			t.Fatalf("history before 3 = %+v", page)
		}
	})
}

func TestContentIsStoredOncePerTenant(t *testing.T) {
	ctx := context.Background()
	blobs := &countingBlobs{Store: blob.NewMemory(), puts: map[string]int{}}
	d, err := drive.Open(state.NewMemory(), blobs)
	if err != nil {
		t.Fatal(err)
	}
	same := "the same bytes"
	for _, who := range []string{"ada", "bob"} {
		for i := range 3 {
			if _, err := d.Commit(ctx, "acme", drive.Actor{Subject: who}, drive.Person(who), drive.CommitInput{Base: drive.Head, Changes: []drive.Change{put(fmt.Sprintf("copy%d.md", i), same)}}); err != nil {
				t.Fatal(err)
			}
		}
	}
	// A store opened afresh, with nothing cached, still does not put twice.
	again, _ := drive.Open(state.NewMemory(), blobs)
	if _, err := again.Commit(ctx, "acme", drive.Actor{Subject: "cy"}, drive.Person("cy"), drive.CommitInput{Base: drive.Head, Changes: []drive.Change{put("x.md", same)}}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Commit(ctx, "globex", drive.Actor{Subject: "eve"}, drive.Person("eve"), drive.CommitInput{Base: drive.Head, Changes: []drive.Change{put("x.md", same)}}); err != nil {
		t.Fatal(err)
	}
	// Seven files in one tenant are one blob; another tenant has its own.
	if len(blobs.puts) != 2 {
		t.Fatalf("puts = %v, want one per tenant", blobs.puts)
	}
	for k, n := range blobs.puts {
		if n != 1 {
			t.Fatalf("%s was put %d times", k, n)
		}
	}
}

func TestAPersonsTreeIsTheirsAlone(t *testing.T) {
	ctx := context.Background()
	d, _ := drive.Open(state.NewMemory(), blob.NewMemory())
	tree := drive.Person("ada")
	if _, err := d.Commit(ctx, "acme", drive.Actor{Subject: "ada"}, tree, drive.CommitInput{Base: drive.Head, Changes: []drive.Change{put("secret.md", "mine")}}); err != nil {
		t.Fatal(err)
	}
	everything := func(drive.Tree) drive.Grant {
		return drive.Grant{Read: true, Write: true, Propose: true, Approve: true}
	}
	for _, who := range []drive.Actor{{}, {Subject: "bob"}, {Subject: "bob", Grants: everything}} {
		if _, err := d.Head(ctx, "acme", who, tree); !errors.Is(err, drive.ErrNotFound) {
			t.Errorf("%+v read ada's tree: %v", who.Subject, err)
		}
		if _, err := d.Commit(ctx, "acme", who, tree, drive.CommitInput{Base: drive.Head, Changes: []drive.Change{put("x", "y")}}); !errors.Is(err, drive.ErrNotFound) {
			t.Errorf("%+v wrote ada's tree: %v", who.Subject, err)
		}
		if hits, _ := d.Search(ctx, "acme", who, []drive.Tree{tree}, "mine", 10); len(hits) != 0 {
			t.Errorf("%+v found ada's file", who.Subject)
		}
	}
	// Another tenant's ada is another person.
	if v, err := d.Head(ctx, "globex", drive.Actor{Subject: "ada"}, tree); err != nil || len(v.Entries) != 0 {
		t.Fatalf("globex's ada sees %+v, %v", v, err)
	}
}

func TestProjectTreesTakeProposalsAndApproval(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		ctx := context.Background()
		st, blobs := open(t), blob.NewMemory()
		refuse := func(_ drive.Tree, _ string, data []byte) error {
			if strings.Contains(string(data), "secret") {
				return errors.New("no")
			}
			return nil
		}
		d, _ := drive.Open(st, blobs, drive.WithScreen(refuse))
		tree := drive.Agent("Field", "lead")
		member := drive.Actor{Subject: "bob", Grants: func(drive.Tree) drive.Grant { return drive.Grant{Read: true, Propose: true} }}
		owner := drive.Actor{Subject: "ada", Grants: func(drive.Tree) drive.Grant {
			return drive.Grant{Read: true, Write: true, Propose: true, Approve: true}
		}}
		if _, err := d.Commit(ctx, "acme", member, tree, drive.CommitInput{Base: drive.Head, Changes: []drive.Change{put("a.md", "x")}}); !errors.Is(err, drive.ErrForbidden) {
			t.Fatalf("a member committed: %v", err)
		}
		if _, err := d.Propose(ctx, "acme", member, tree, drive.ProposeInput{Changes: []drive.Change{put("a.md", "a secret")}}); !errors.Is(err, drive.ErrRefused) {
			t.Fatalf("the screen let through %v", err)
		}
		p, err := d.Propose(ctx, "acme", member, tree, drive.ProposeInput{Base: drive.Head, Changes: []drive.Change{put("a.md", "ship on Tuesday")}, By: "bob", At: at})
		if err != nil {
			t.Fatal(err)
		}
		if v, _ := d.Head(ctx, "acme", owner, tree); len(v.Entries) != 0 {
			t.Fatal("a proposal changed the head")
		}
		if _, err := d.Approve(ctx, "acme", member, tree, p.ID, "bob", at); !errors.Is(err, drive.ErrForbidden) {
			t.Fatalf("a member approved: %v", err)
		}
		again, _ := drive.Open(st, blobs, drive.WithScreen(refuse))
		held, _ := again.Proposals("acme", owner, tree)
		if len(held) != 1 || held[0].ID != p.ID {
			t.Fatalf("proposals after restart = %+v", held)
		}
		v, err := again.Approve(ctx, "acme", owner, tree, p.ID, "ada", at)
		if err != nil || v.N != 1 || len(v.Resolves) != 1 || v.By != "ada" {
			t.Fatalf("approve = %+v, %v", v, err)
		}
		if open, _ := again.Proposals("acme", owner, tree); len(open) != 0 {
			t.Fatal("an approved proposal is still open")
		}
		q, _ := again.Propose(ctx, "acme", member, tree, drive.ProposeInput{Base: drive.Head, Changes: []drive.Change{{Path: "a.md", Delete: true}}})
		if err := again.Discard(ctx, "acme", owner, tree, q.ID, "test"); err != nil {
			t.Fatal(err)
		}
		if got := read(t, again, "acme", owner, tree, 0, "a.md"); got != "ship on Tuesday" {
			t.Fatalf("a discarded proposal changed %q", got)
		}
	})
}

func TestPathsAndBoundsAreChecked(t *testing.T) {
	ctx := context.Background()
	d, _ := drive.Open(state.NewMemory(), blob.NewMemory())
	ada := drive.Actor{Subject: "ada"}
	for _, p := range []string{"", "/etc/passwd", "../x", "a/../../b", "a//b", ".", "a\\b", "a\x00b"} {
		if _, err := d.Commit(ctx, "acme", ada, drive.Person("ada"), drive.CommitInput{Base: drive.Head, Changes: []drive.Change{put(p, "x")}}); !errors.Is(err, drive.ErrInvalid) {
			t.Errorf("path %q = %v", p, err)
		}
	}
	big := drive.Change{Path: "big", Data: make([]byte, drive.MaxFileBytes+1)}
	if _, err := d.Commit(ctx, "acme", ada, drive.Person("ada"), drive.CommitInput{Base: drive.Head, Changes: []drive.Change{big}}); !errors.Is(err, drive.ErrTooLarge) {
		t.Fatalf("a file over the bound = %v", err)
	}
	if _, err := d.Search(ctx, "acme", ada, make([]drive.Tree, drive.MaxSearchTrees+1), "x", 1); !errors.Is(err, drive.ErrInvalid) {
		t.Fatalf("an unbounded search = %v", err)
	}
	for _, tree := range []drive.Tree{{}, {Kind: drive.PersonKind}, {Kind: drive.AgentKind, Project: "x"}, {Kind: drive.ProjectKind, Person: "ada"}} {
		if _, err := d.Head(ctx, "acme", drive.System, tree); !errors.Is(err, drive.ErrInvalid) {
			t.Errorf("tree %+v = %v", tree, err)
		}
	}
}

func TestOldVersionsArePruned(t *testing.T) {
	ctx := context.Background()
	d, _ := drive.Open(state.NewMemory(), blob.NewMemory())
	ada := drive.Actor{Subject: "ada"}
	for i := range drive.MaxVersions + 5 {
		if _, err := d.Commit(ctx, "acme", ada, drive.Person("ada"), drive.CommitInput{Base: drive.Head, Changes: []drive.Change{put("n.md", fmt.Sprint(i))}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.Version(ctx, "acme", ada, drive.Person("ada"), 5); !errors.Is(err, drive.ErrNotFound) {
		t.Fatalf("a pruned version = %v", err)
	}
	if _, err := d.Restore(ctx, "acme", ada, drive.Person("ada"), 6, "ada", at); err != nil {
		t.Fatalf("the oldest kept version cannot be restored: %v", err)
	}
}

func TestSearchRanksAndChecksAccess(t *testing.T) {
	ctx := context.Background()
	d, _ := drive.Open(state.NewMemory(), blob.NewMemory())
	ada := drive.Actor{Subject: "ada", Grants: func(t drive.Tree) drive.Grant { return drive.Grant{Read: t.Project == "Field"} }}
	_, _ = d.Commit(ctx, "acme", drive.System, drive.Project("Field"), drive.CommitInput{Base: drive.Head, Changes: []drive.Change{put("a.md", "renewal in March"), put("b.md", "renewal")}})
	_, _ = d.Commit(ctx, "acme", drive.System, drive.Project("Hidden"), drive.CommitInput{Base: drive.Head, Changes: []drive.Change{put("c.md", "renewal in March")}})
	hits, err := d.Search(ctx, "acme", ada, []drive.Tree{drive.Project("Field"), drive.Project("Hidden")}, "March renewal", 10)
	if err != nil || len(hits) != 2 || hits[0].Path != "a.md" || hits[0].Score != 2 || !strings.Contains(hits[0].Snippet, "March") {
		t.Fatalf("hits = %+v, %v", hits, err)
	}
	_, _ = d.Commit(ctx, "acme", drive.System, drive.Project("Field"), drive.CommitInput{Base: drive.Head, Changes: []drive.Change{{Path: "a.md", Delete: true}}})
	if hits, _ := d.Search(ctx, "acme", ada, []drive.Tree{drive.Project("Field")}, "March", 10); len(hits) != 0 {
		t.Fatalf("the index kept a deleted file: %+v", hits)
	}
}

func TestInstallDiffAndFinish(t *testing.T) {
	ctx := context.Background()
	d, _ := drive.Open(state.NewMemory(), blob.NewMemory())
	ada := drive.Actor{Subject: "ada", Grants: func(drive.Tree) drive.Grant { return drive.Grant{Read: true, Propose: true} }}
	_, _ = d.Commit(ctx, "acme", ada, drive.Person("ada"), drive.CommitInput{Base: drive.Head, Changes: []drive.Change{put("keep.md", "k"), put("edit.md", "e"), put("gone.md", "g")}})
	_, _ = d.Commit(ctx, "acme", drive.System, drive.Agent("", "lead"), drive.CommitInput{Base: drive.Head, Changes: []drive.Change{put("notes/x.md", "x")}})

	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	target := drive.RootTarget(root)
	// What was there before is not part of the tree.
	_ = target.WriteFile("me/memory/stale.md", []byte("left over"))
	cache, err := drive.NewDiskCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	me, err := d.Install(ctx, "acme", ada, drive.Mount{Tree: drive.Person("ada"), Dir: drive.Person("ada").Mount()}, target, cache)
	if err != nil {
		t.Fatal(err)
	}
	lead, err := d.Install(ctx, "acme", ada, drive.Mount{Tree: drive.Agent("", "lead"), Dir: drive.Agent("", "lead").Mount()}, target, cache)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := root.Stat("me/memory/stale.md"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("install left a file the tree does not hold")
	}
	if b, _ := root.ReadFile("project/memory/lead/notes/x.md"); string(b) != "x" {
		t.Fatalf("installed agent note = %q", b)
	}
	if changes, _ := drive.Diff(me, target); len(changes) != 0 {
		t.Fatalf("an untouched install differs: %+v", changes)
	}
	_ = target.WriteFile("me/memory/edit.md", []byte("edited"))
	_ = target.WriteFile("me/memory/new/deep.md", []byte("new"))
	_ = target.Remove("me/memory/gone.md")
	_ = target.WriteFile("project/memory/lead/notes/y.md", []byte("learned"))

	done, err := d.Finish(ctx, ada, me, target, "ada", "run", at)
	if err != nil || done.Version != 2 {
		t.Fatalf("finish /me = %+v, %v", done, err)
	}
	head, _ := d.Head(ctx, "acme", ada, drive.Person("ada"))
	var paths []string
	for _, e := range head.Entries {
		paths = append(paths, e.Path)
	}
	if strings.Join(paths, " ") != "edit.md keep.md new/deep.md" {
		t.Fatalf("committed paths = %v", paths)
	}
	done, err = d.Finish(ctx, ada, lead, target, "ada", "run", at)
	if err != nil || len(done.Proposals) != 1 || done.Version != 0 {
		t.Fatalf("finish agent memory = %+v, %v", done, err)
	}
	if v, _ := d.Head(ctx, "acme", drive.System, drive.Agent("", "lead")); len(v.Entries) != 1 {
		t.Fatal("a run's write to project memory skipped approval")
	}
	ro, _ := d.Install(ctx, "acme", ada, drive.Mount{Tree: drive.Agent("", "lead"), Dir: "ro", ReadOnly: true}, target, nil)
	_ = target.WriteFile("ro/notes/z.md", []byte("z"))
	if done, _ := d.Finish(ctx, ada, ro, target, "ada", "run", at); len(done.Dropped) != 1 || len(done.Proposals) != 0 {
		t.Fatalf("a read-only mount kept %+v", done)
	}
}

func TestConcurrentCommitsKeepEveryChange(t *testing.T) {
	ctx := context.Background()
	d, _ := drive.Open(state.NewMemory(), blob.NewMemory())
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := d.Commit(ctx, "acme", drive.System, drive.Project("Field"), drive.CommitInput{Base: drive.Head, Changes: []drive.Change{put(fmt.Sprintf("f%d.md", i), "x")}}); err != nil {
				t.Error(err)
			}
			_, _ = d.Search(ctx, "acme", drive.System, []drive.Tree{drive.Project("Field")}, "x", 5)
		}()
	}
	wg.Wait()
	v, _ := d.Head(ctx, "acme", drive.System, drive.Project("Field"))
	if v.N != 20 || len(v.Entries) != 20 {
		t.Fatalf("head = %d with %d files", v.N, len(v.Entries))
	}
}

// Every record's key starts with its tree's key, the owner it is sharded by.
func TestEveryKeyCarriesItsOwner(t *testing.T) {
	ctx := context.Background()
	st := state.NewMemory()
	d, _ := drive.Open(st, blob.NewMemory())
	tree := drive.Agent("Field/sub", "lead")
	_, _ = d.Commit(ctx, "acme", drive.System, tree, drive.CommitInput{Base: drive.Head, Changes: []drive.Change{put("a.md", "x")}})
	_, _ = d.Propose(ctx, "acme", drive.System, tree, drive.ProposeInput{Changes: []drive.Change{put("b.md", "y")}})
	for _, ns := range []string{"drive.trees", "drive.versions"} {
		recs, err := st.Load(ctx, ns)
		if err != nil || len(recs) == 0 {
			t.Fatalf("%s: %v, %d records", ns, err, len(recs))
		}
		for _, r := range recs {
			if !strings.HasPrefix(r.Key, tree.Key()+"#") {
				t.Errorf("%s key %q does not start with %q", ns, r.Key, tree.Key())
			}
		}
	}
}
