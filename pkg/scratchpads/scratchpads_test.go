package scratchpads

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

var at = time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)

func TestScratchpadsCreateRenameEditDeleteAndConfine(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s := Open(st)
		ctx := t.Context()
		a, err := s.Create(ctx, "acme", "t1", " Plan ", "# Plan", "ana", at)
		if err != nil {
			t.Fatal(err)
		}
		if a.Name != "Plan" || a.Version != 1 || a.Size != 6 || !strings.HasPrefix(a.ID, "sp-") {
			t.Fatalf("created = %+v", a)
		}
		if _, err := s.Create(ctx, "acme", "t1", "plan", "", "ana", at); !errors.Is(err, ErrConflict) {
			t.Fatalf("a name taken in another case = %v", err)
		}
		b, err := s.Create(ctx, "acme", "t1", "Risks", "", "ana", at)
		if err != nil {
			t.Fatal(err)
		}
		// Edit from the version read; a stale version changes nothing.
		a2, err := s.Update(ctx, "acme", "t1", a.ID, "Plan", "# Plan\n- one", a.Version, "bo", at)
		if err != nil || a2.Version != 2 || a2.UpdatedBy != "bo" {
			t.Fatalf("edit = %+v %v", a2, err)
		}
		if _, err := s.Update(ctx, "acme", "t1", a.ID, "Plan", "stale", a.Version, "bo", at); !errors.Is(err, ErrConflict) {
			t.Fatalf("stale edit = %v", err)
		}
		// Rename keeps the id and the content; renaming onto a taken name fails whole.
		if _, err := s.Update(ctx, "acme", "t1", a.ID, "risks", a2.Content, a2.Version, "bo", at); !errors.Is(err, ErrConflict) {
			t.Fatalf("rename onto a taken name = %v", err)
		}
		r, err := s.Update(ctx, "acme", "t1", a.ID, "Launch plan", a2.Content, a2.Version, "bo", at)
		if err != nil || r.ID != a.ID || r.Name != "Launch plan" {
			t.Fatalf("rename = %+v %v", r, err)
		}
		got, err := s.Get(ctx, "acme", "t1", a.ID)
		if err != nil || got.Content != "# Plan\n- one" || got.Version != r.Version {
			t.Fatalf("get = %+v %v", got, err)
		}
		if p, err := s.ByName(ctx, "acme", "t1", "LAUNCH PLAN"); err != nil || p.ID != a.ID {
			t.Fatalf("by name = %+v %v", p, err)
		}
		list, err := s.List(ctx, "acme", "t1")
		if err != nil || len(list) != 2 || list[0].Name != "Launch plan" || list[0].Content != "" || list[0].Size == 0 {
			t.Fatalf("list = %+v %v", list, err)
		}
		// Another tenant and another conversation see none of it.
		if _, err := s.Get(ctx, "other", "t1", a.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("other tenant = %v", err)
		}
		if l, _ := s.List(ctx, "acme", "t2"); len(l) != 0 {
			t.Fatal("another conversation's list", l)
		}
		if err := s.Delete(ctx, "acme", "t1", b.ID, b.Version+1); !errors.Is(err, ErrConflict) {
			t.Fatalf("stale delete = %v", err)
		}
		if err := s.Delete(ctx, "acme", "t1", b.ID, b.Version); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Get(ctx, "acme", "t1", b.ID); !errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
		// Written through: another store on the same database reads it.
		if p, err := Open(st).Get(ctx, "acme", "t1", a.ID); err != nil || p.Name != "Launch plan" {
			t.Fatalf("reopened = %+v %v", p, err)
		}
		// A deleted conversation takes its scratchpads with it, and only its own.
		if _, err := s.Create(ctx, "acme", "t2", "Kept", "", "ana", at); err != nil {
			t.Fatal(err)
		}
		if err := s.DeleteThread(ctx, "acme", "t1"); err != nil {
			t.Fatal(err)
		}
		if l, _ := s.List(ctx, "acme", "t1"); len(l) != 0 {
			t.Fatal("scratchpads outlived their conversation", l)
		}
		if l, _ := s.List(ctx, "acme", "t2"); len(l) != 1 {
			t.Fatal("another conversation's scratchpads were removed", l)
		}
	})
}

func TestScratchpadBounds(t *testing.T) {
	s := Open(nil)
	ctx := t.Context()
	for _, name := range []string{"", "  ", "a/b", "a\\b", strings.Repeat("x", MaxName+1), "tab\there"} {
		if _, err := s.Create(ctx, "acme", "t1", name, "", "ana", at); !errors.Is(err, ErrInvalid) {
			t.Errorf("name %q = %v", name, err)
		}
	}
	for _, content := range []string{strings.Repeat("x", MaxContent+1), "nul\x00", "\xff"} {
		if _, err := s.Create(ctx, "acme", "t1", "n", content, "ana", at); !errors.Is(err, ErrInvalid) {
			t.Errorf("content of %d bytes = %v", len(content), err)
		}
	}
	for i := range MaxPerThread {
		if _, err := s.Create(ctx, "acme", "t1", fmt.Sprintf("pad %d", i), "", "ana", at); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Create(ctx, "acme", "t1", "one too many", "", "ana", at); !errors.Is(err, ErrInvalid) {
		t.Fatalf("past the bound = %v", err)
	}
	if l, _ := s.List(ctx, "acme", "t1"); len(l) != MaxPerThread {
		t.Fatalf("list = %d", len(l))
	}
	if _, err := s.List(ctx, "acme", "a/b"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a thread with a slash = %v", err)
	}
}
