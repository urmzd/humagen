package artifacts_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/urmzd/humagen/pkg/artifacts"
	"github.com/urmzd/humagen/pkg/state"
)

func draft(x *artifacts.Index, thread, title string) artifacts.Artifact {
	id := x.NextID("acme")
	return artifacts.Artifact{ID: id, Key: "blobs/" + id, Thread: thread, Title: title, Kind: artifacts.Document, Size: 10}
}

// A publish that names the version it follows lands only while that version
// is its file's latest, so nobody replaces a version they have not read.
func TestPublishAfterRefusesAStaleVersion(t *testing.T) {
	x, err := artifacts.Open(state.NewMemory())
	if err != nil {
		t.Fatal(err)
	}
	first, err := x.Publish("acme", draft(x, "thread", "plan.md"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := x.PublishAfter("acme", draft(x, "thread", "plan.md"), first.ID)
	if err != nil || second.Version != 2 {
		t.Fatalf("following the latest = %+v, %v", second, err)
	}
	// The first version is no longer the latest: following it stores nothing.
	if _, err := x.PublishAfter("acme", draft(x, "thread", "plan.md"), first.ID); !errors.Is(err, artifacts.ErrStale) {
		t.Fatalf("following a replaced version = %v, want ErrStale", err)
	}
	// A version of another file, and one that does not exist, are no base.
	other, err := x.Publish("acme", draft(x, "thread", "notes.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, after := range []string{other.ID, "art-missing"} {
		if _, err := x.PublishAfter("acme", draft(x, "thread", "plan.md"), after); !errors.Is(err, artifacts.ErrStale) {
			t.Fatalf("following %s = %v, want ErrStale", after, err)
		}
	}
	// Another tenant's version is no base either.
	if _, err := x.PublishAfter("globex", draft(x, "thread", "plan.md"), second.ID); !errors.Is(err, artifacts.ErrStale) {
		t.Fatalf("following another tenant's version = %v, want ErrStale", err)
	}
	if _, err := x.PublishAfter("acme", draft(x, "thread", "plan.md"), ""); !errors.Is(err, artifacts.ErrInvalid) {
		t.Fatalf("following nothing = %v, want ErrInvalid", err)
	}
	versions, err := x.Versions("acme", second.ID)
	if err != nil || len(versions) != 2 || versions[0].ID != second.ID {
		t.Fatalf("versions = %+v, %v", versions, err)
	}
	// Once the latest is deleted, the one before it is the latest again, and
	// the next version takes the number after the live ones.
	if _, err := x.Delete("acme", second.ID); err != nil {
		t.Fatal(err)
	}
	if again, err := x.PublishAfter("acme", draft(x, "thread", "plan.md"), first.ID); err != nil || again.Version != 2 {
		t.Fatalf("following the latest after a delete = %+v, %v", again, err)
	}
}

// Many processes that all read the same latest version cannot all follow it:
// one publishes, and the rest are told it is stale.
func TestPublishAfterAdmitsOneOfManyProcesses(t *testing.T) {
	st := state.NewMemory()
	x, err := artifacts.Open(st)
	if err != nil {
		t.Fatal(err)
	}
	first, err := x.Publish("acme", draft(x, "thread", "plan.md"))
	if err != nil {
		t.Fatal(err)
	}
	const count = 16
	var wg sync.WaitGroup
	var mu sync.Mutex
	published, stale := 0, 0
	for range count {
		wg.Go(func() {
			process, err := artifacts.Open(st)
			if err != nil {
				t.Error(err)
				return
			}
			_, err = process.PublishAfter("acme", draft(process, "thread", "plan.md"), first.ID)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				published++
			case errors.Is(err, artifacts.ErrStale):
				stale++
			default:
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if published != 1 || stale != count-1 {
		t.Fatalf("published %d and refused %d of %d", published, stale, count)
	}
	versions, err := x.Versions("acme", first.ID)
	if err != nil || len(versions) != 2 {
		t.Fatalf("versions = %d, %v", len(versions), err)
	}
}
