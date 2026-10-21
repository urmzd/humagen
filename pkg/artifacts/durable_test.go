package artifacts_test

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/urmzd/warp/pkg/artifacts"
	"github.com/urmzd/warp/pkg/state"
	"github.com/urmzd/warp/pkg/state/statetest"
)

func TestRestart(t *testing.T) {
	at := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		x, err := artifacts.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		v1 := put(t, x, "acme", artifacts.Artifact{Title: "a.md", Thread: "t1", Size: 10, Agent: "lead", CreatedAt: at})
		v2 := put(t, x, "acme", artifacts.Artifact{Title: "a.md", Thread: "t1", Size: 12, Agent: "lead", CreatedAt: at})
		gone := put(t, x, "acme", artifacts.Artifact{Title: "b.md", Thread: "t1", Size: 5, CreatedAt: at})
		if _, err := x.Delete("acme", gone.ID); err != nil {
			t.Fatal(err)
		}
		put(t, x, "globex", artifacts.Artifact{Title: "a.md", Thread: "t9", Size: 1, CreatedAt: at})

		again, err := artifacts.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		got, err := again.Get("acme", v2.ID)
		if err != nil || !reflect.DeepEqual(got, v2) || got.Key != v2.Key || got.Version != 2 {
			t.Fatalf("after restart = %+v, %v, want %+v", got, err, v2)
		}
		versions, _ := again.Versions("acme", v1.ID)
		if len(versions) != 2 || versions[0].ID != v2.ID {
			t.Fatalf("versions after restart = %+v", versions)
		}
		if _, err := again.Get("acme", gone.ID); !errors.Is(err, artifacts.ErrNotFound) {
			t.Fatalf("a deleted artifact came back: %v", err)
		}
		if u := again.Usage("acme"); u.Bytes != 22 {
			t.Fatalf("usage after restart = %+v", u)
		}
		// The id of the deleted newest artifact is not issued again, so
		// its bytes' key is never reused.
		if id := again.NextID("acme"); id == gone.ID || id == v2.ID {
			t.Fatalf("a restarted index issued %q again", id)
		}
		v3 := put(t, again, "acme", artifacts.Artifact{Title: "a.md", Thread: "t1", Size: 1, CreatedAt: at})
		if v3.Version != 3 {
			t.Fatalf("the next version after restart = %d", v3.Version)
		}
	})
}

func TestFailedWriteChangesNothing(t *testing.T) {
	f := statetest.NewFaulty(state.NewMemory())
	x, _ := artifacts.Open(f)
	a := put(t, x, "acme", artifacts.Artifact{Title: "a.md", Thread: "t1", Size: 10})
	f.Fail(statetest.ErrInjected)
	if _, err := x.Publish("acme", artifacts.Artifact{ID: x.NextID("acme"), Key: "k", Thread: "t1", Title: "a.md", Kind: artifacts.Document}); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Publish = %v", err)
	}
	if _, err := x.Delete("acme", a.ID); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Delete = %v", err)
	}
	if v, _ := x.Versions("acme", a.ID); len(v) != 1 {
		t.Fatalf("after failed writes = %+v", v)
	}
}
