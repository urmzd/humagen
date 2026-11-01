package artifacts_test

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/urmzd/humagen/pkg/artifacts"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func openIndex(t *testing.T, st state.Store) *artifacts.Index {
	t.Helper()
	x, err := artifacts.Open(st)
	if err != nil {
		t.Fatal(err)
	}
	return x
}

// Two processes on one database, each with its own index, see each other's
// files at once, and files published through both at the same time are each
// kept: versions of one file numbered without a gap or a repeat, and the
// totals counting every one.
func TestTwoProcessesAgree(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, two := openIndex(t, a), openIndex(t, b)
		first := put(t, one, "acme", artifacts.Artifact{Title: "plan.md", Thread: "t", Size: 1, Project: "Launch", Agent: "lead"})
		if got, err := two.Get("acme", first.ID); err != nil || got.ID != first.ID {
			t.Fatalf("the other process reads %+v, %v", got, err)
		}

		const writers = 24
		var wg sync.WaitGroup
		for i := range writers {
			x := []*artifacts.Index{one, two}[i%2]
			wg.Go(func() {
				title := "plan.md"
				if i%3 == 0 {
					title = fmt.Sprintf("f%02d.md", i)
				}
				id := x.NextID("acme")
				if _, err := x.Publish("acme", artifacts.Artifact{ID: id, Key: "artifacts/" + id, Title: title, Thread: "t", Kind: artifacts.Document, Size: 1, Project: "Launch", Agent: "lead"}); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		for _, x := range []*artifacts.Index{one, two} {
			versions, err := x.Versions("acme", first.ID)
			if err != nil {
				t.Fatal(err)
			}
			want := 1 + writers - writers/3
			if len(versions) != want {
				t.Fatalf("%d versions, want %d", len(versions), want)
			}
			for i, v := range versions {
				if v.Version != want-i {
					t.Fatalf("version %d is numbered %d", i, v.Version)
				}
			}
			u := usageOf(t, x, "acme")
			if u.Count != writers+1 || u.Bytes != writers+1 || usedOf(t, x, "acme", "Launch") != writers+1 {
				t.Fatalf("usage = %+v", u)
			}
			page, _, err := x.Page("acme", artifacts.Query{Limit: artifacts.MaxPage})
			if err != nil || len(page) != 1+writers/3 || page[0].Title == "" {
				t.Fatalf("page = %d files, %v", len(page), err)
			}
		}
		// Deleting the newest version through one shows the one before it
		// through the other.
		versions, _ := one.Versions("acme", first.ID)
		if _, err := one.Delete("acme", versions[0].ID); err != nil {
			t.Fatal(err)
		}
		page, _, _ := two.Page("acme", artifacts.Query{Limit: artifacts.MaxPage})
		found := false
		for _, f := range page {
			found = found || f.ID == versions[1].ID
		}
		if !found {
			t.Fatalf("the version before a deleted one is not listed: %v", ids(page))
		}
	})
}

// Records kept before ids, files and totals had their own keys are given
// them once, though two processes open the index at the same time.
func TestTwoProcessesIndexOnce(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		record := func(id string, version int, size int64) []byte {
			raw, _ := json.Marshal(map[string]any{"artifact": map[string]any{"id": id, "title": "plan.md", "thread": "t", "kind": "document", "version": version, "size": size, "project": "Launch", "key": "artifacts/" + id}})
			return raw
		}
		if err := a.Write(t.Context(), "artifacts",
			state.Put("acme", "artifact/"+state.Seq(1), record("art-1", 1, 10)),
			state.Put("acme", "artifact/"+state.Seq(2), record("art-2", 2, 5)),
			state.Put("acme", "next", []byte(`{"next":2}`)),
		); err != nil {
			t.Fatal(err)
		}
		var one, two *artifacts.Index
		var wg sync.WaitGroup
		wg.Go(func() { one = openIndex(t, a) })
		wg.Go(func() { two = openIndex(t, b) })
		wg.Wait()
		if t.Failed() {
			return
		}
		for _, x := range []*artifacts.Index{one, two} {
			if v, err := x.Versions("acme", "art-1"); err != nil || ids(v) != "art-2 art-1" {
				t.Fatalf("versions = %v, %v", ids(v), err)
			}
			if page, _, err := x.Page("acme", artifacts.Query{}); err != nil || ids(page) != "art-2" {
				t.Fatalf("page = %v, %v", ids(page), err)
			}
			if u := usageOf(t, x, "acme"); u.Count != 2 || u.Bytes != 15 {
				t.Fatalf("usage = %+v", u)
			}
		}
		v3 := put(t, two, "acme", artifacts.Artifact{Title: "plan.md", Thread: "t", Size: 1})
		if v3.Version != 3 {
			t.Fatalf("the next version = %d", v3.Version)
		}
	})
}
