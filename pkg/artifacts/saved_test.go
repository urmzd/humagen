package artifacts_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/urmzd/humagen/pkg/artifacts"
	"github.com/urmzd/humagen/pkg/state"
)

func TestSavedArtifactIsAtomicAcrossProcessesAndRestart(t *testing.T) {
	st := state.NewMemory()
	x, err := artifacts.Open(st)
	if err != nil {
		t.Fatal(err)
	}
	const count = 16
	results := make(chan artifacts.Artifact, count)
	var wg sync.WaitGroup
	for range count {
		wg.Go(func() {
			process, err := artifacts.Open(st)
			if err != nil {
				t.Error(err)
				return
			}
			id := process.NextID("acme")
			a, err := process.PublishOnce("acme", artifacts.Artifact{ID: id, Key: "blobs/" + id, Thread: "thread", Title: "answer.md", Kind: artifacts.Document, Size: 10}, "source-content-title")
			if err != nil {
				t.Error(err)
				return
			}
			results <- a
		})
	}
	wg.Wait()
	close(results)
	var first string
	for a := range results {
		if first == "" {
			first = a.ID
		}
		if a.ID != first || a.Version != 1 {
			t.Fatalf("duplicate publication: %+v", a)
		}
	}
	if first == "" {
		t.Fatal("no artifact saved")
	}
	u, err := x.Usage("acme")
	if err != nil || u.Count != 1 || u.Bytes != 10 {
		t.Fatalf("usage = %+v, %v", u, err)
	}
	reopened, err := artifacts.Open(st)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := reopened.Saved("acme", "source-content-title")
	if err != nil || saved.ID != first {
		t.Fatalf("restart lookup = %+v, %v", saved, err)
	}
	if _, err := reopened.Saved("other", "source-content-title"); !errors.Is(err, artifacts.ErrNotFound) {
		t.Fatalf("tenant lookup = %v", err)
	}
	if _, err := reopened.Delete("acme", first); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Saved("acme", "source-content-title"); !errors.Is(err, artifacts.ErrNotFound) {
		t.Fatalf("deleted saved = %v", err)
	}
	id := reopened.NextID("acme")
	again, err := reopened.PublishOnce("acme", artifacts.Artifact{ID: id, Key: "blobs/" + id, Thread: "thread", Title: "answer.md", Kind: artifacts.Document}, "source-content-title")
	if err != nil || again.ID != id {
		t.Fatalf("resave after deletion = %+v, %v", again, err)
	}
}
