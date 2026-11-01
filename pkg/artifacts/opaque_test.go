package artifacts_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/urmzd/humagen/pkg/artifacts"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func opaque(t *testing.T, id string) {
	t.Helper()
	if _, err := strconv.Atoi(strings.TrimPrefix(id, "art-")); err == nil || len(id) < 26 {
		t.Fatalf("artifact identity exposes an ordinal: %q", id)
	}
}

func TestOpaqueArtifactsPreservePrivateOrderingAndDeletedCursors(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		x, err := artifacts.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		first := put(t, x, "acme", artifacts.Artifact{Title: "first.md", Thread: "public"})
		put(t, x, "acme", artifacts.Artifact{Title: "secret.md", Thread: "private"})
		last := put(t, x, "acme", artifacts.Artifact{Title: "last.md", Thread: "public"})
		opaque(t, first.ID)
		opaque(t, last.ID)
		wire, err := json.Marshal(last)
		if err != nil {
			t.Fatal(err)
		}
		for _, private := range []string{`"order":`, `"record":`, `"next":`, `"legacy_max":`} {
			if strings.Contains(string(wire), private) {
				t.Fatalf("private ordering in artifact response: %s", wire)
			}
		}
		keep := func(a artifacts.Artifact) bool { return a.Thread == "public" }
		page, cursor, err := x.Page("acme", artifacts.Query{Limit: 1, Keep: keep})
		if err != nil || ids(page) != last.ID || cursor == "" {
			t.Fatalf("first page: %v %q %v", page, cursor, err)
		}
		raw, _ := base64.RawURLEncoding.DecodeString(cursor)
		if string(raw) != last.ID {
			t.Fatal("cursor exposes a private order")
		}
		if _, err := x.Delete("acme", last.ID); err != nil {
			t.Fatal(err)
		}
		again, err := artifacts.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		page, _, err = again.Page("acme", artifacts.Query{Before: cursor, Keep: keep})
		if err != nil || ids(page) != first.ID {
			t.Fatalf("deleted cursor after restart: %v %v", page, err)
		}
		if _, _, err := again.Page("other", artifacts.Query{Before: cursor}); !errors.Is(err, artifacts.ErrCursor) {
			t.Fatal("cursor crossed tenants")
		}
	})
}

func TestOpaqueArtifactsKeepLegacyIDsPathsVersionsAndCursorBoundary(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		legacy := artifacts.Artifact{ID: "art-1", Title: "plan.md", Thread: "t", Kind: artifacts.Document, Version: 1}
		raw, _ := json.Marshal(map[string]any{"artifact": map[string]any{"id": legacy.ID, "title": legacy.Title, "thread": legacy.Thread, "kind": legacy.Kind, "version": 1, "key": "artifacts/art-1"}})
		if err := st.Write(t.Context(), "artifacts", state.Put("acme", "artifact/"+state.Seq(1), raw), state.Put("acme", "next", []byte(`{"next":3}`))); err != nil {
			t.Fatal(err)
		}
		x, err := artifacts.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		v2 := put(t, x, "acme", artifacts.Artifact{Title: "plan.md", Thread: "t"})
		other := put(t, x, "acme", artifacts.Artifact{Title: "other.md", Thread: "t"})
		again, err := artifacts.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		got, err := again.Get("acme", "art-1")
		if err != nil || got.Key != "artifacts/art-1" {
			t.Fatalf("legacy path changed: %v %v", got, err)
		}
		versions, err := again.Versions("acme", "art-1")
		if err != nil || ids(versions) != v2.ID+" art-1" || versions[0].Version != 2 {
			t.Fatalf("mixed versions: %v %v", versions, err)
		}
		page, _, err := again.Page("acme", artifacts.Query{})
		if err != nil || ids(page) != other.ID+" "+v2.ID {
			t.Fatalf("mixed order: %v %v", page, err)
		}
		oldCursor := base64.RawURLEncoding.EncodeToString([]byte("art-3"))
		page, _, err = again.Page("acme", artifacts.Query{Before: oldCursor})
		if err != nil || len(page) != 0 {
			t.Fatalf("legacy cursor changed latest-version semantics: %v %v", page, err)
		}
		if _, _, err := again.Page("acme", artifacts.Query{Before: base64.RawURLEncoding.EncodeToString([]byte("art-5"))}); !errors.Is(err, artifacts.ErrCursor) {
			t.Fatal("guessed numeric cursor probes opaque ordering")
		}
	})
}

func TestOpaqueArtifactsFailedMetadataWriteAndConcurrentReservations(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		fault := statetest.NewFaulty(st)
		x, err := artifacts.Open(fault)
		if err != nil {
			t.Fatal(err)
		}
		first := put(t, x, "acme", artifacts.Artifact{Title: "one.md", Thread: "t"})
		counterBefore, err := st.Get(t.Context(), "artifacts", "acme", "next")
		if err != nil {
			t.Fatal(err)
		}
		id := x.NextID("acme")
		fault.Fail(statetest.ErrInjected)
		a := artifacts.Artifact{ID: id, Key: "artifacts/" + id, Title: "two.md", Thread: "t", Kind: artifacts.Document}
		if _, err := x.Publish("acme", a); !errors.Is(err, statetest.ErrInjected) {
			t.Fatal(err)
		}
		if usageOf(t, x, "acme").Count != 1 {
			t.Fatal("failed metadata became visible")
		}
		counterAfter, err := st.Get(t.Context(), "artifacts", "acme", "next")
		if err != nil || string(counterBefore) != string(counterAfter) {
			t.Fatal("failed write advanced durable order")
		}
		fault.Fail(nil)
		if _, err := x.Publish("acme", a); err != nil {
			t.Fatal(err)
		}
		again, err := artifacts.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		page, _, _ := again.Page("acme", artifacts.Query{})
		if ids(page) != id+" "+first.ID {
			t.Fatal("failed write changed replay order")
		}
		var wg sync.WaitGroup
		var seen sync.Map
		for i := range 50 {
			wg.Go(func() {
				id := again.NextID("acme")
				if _, exists := seen.LoadOrStore(id, true); exists {
					t.Errorf("duplicate reservation %q", id)
				}
				if _, err := again.Publish("acme", artifacts.Artifact{ID: id, Key: "artifacts/" + id, Title: fmt.Sprintf("f%d.md", i), Thread: "t", Kind: artifacts.Document}); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		beforeRestart, _, err := again.Page("acme", artifacts.Query{Limit: artifacts.MaxPage})
		if err != nil {
			t.Fatal(err)
		}
		replay, err := artifacts.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if usageOf(t, replay, "acme").Count != 52 {
			t.Fatal("concurrent writes lost artifacts")
		}
		afterRestart, _, err := replay.Page("acme", artifacts.Query{Limit: artifacts.MaxPage})
		if err != nil || ids(beforeRestart) != ids(afterRestart) {
			t.Fatal("concurrent publication order changed on replay")
		}
	})
}
