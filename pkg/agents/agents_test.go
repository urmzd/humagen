package agents_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/definition"
	"github.com/urmzd/humagen/fiber/pkg/definition/definitiontest"
	"github.com/urmzd/humagen/pkg/agents"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestSource(t *testing.T) {
	definitiontest.Run(t, func(*testing.T) definition.Source { return agents.New() })
}

func TestSourceOnState(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		definitiontest.Run(t, func(t *testing.T) definition.Source {
			s, err := agents.Open(open(t))
			if err != nil {
				t.Fatal(err)
			}
			return s
		})
	})
}

func TestRestartKeepsEverything(t *testing.T) {
	ctx := context.Background()
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := agents.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range []string{"1.10.0", "1.2.0"} {
			if _, err := s.Put(ctx, definitiontest.Record("acme", "docs-bot", v, "v"+v)); err != nil {
				t.Fatal(err)
			}
		}
		rec := definitiontest.Record("acme", "gone", "1.0.0", "x")
		rec.Policy = definition.Policy{TaggedOnly: true, WorkspaceAreas: []string{"b/", "a/"}}
		put, err := s.Put(ctx, rec)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Retire(ctx, "acme", "gone", "ada"); err != nil {
			t.Fatal(err)
		}

		again, err := agents.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		all, err := again.Versions(ctx, "acme", "docs-bot")
		if err != nil || len(all) != 2 || all[0].Version != "1.2.0" || all[1].Version != "1.10.0" {
			t.Fatalf("versions after restart = %+v, %v", all, err)
		}
		got, err := again.Get(ctx, "acme", "gone", "1.0.0")
		if err != nil || got.PolicyHash != put.PolicyHash || got.Digest != put.Digest || !got.CreatedAt.Equal(put.CreatedAt) {
			t.Fatalf("retired version after restart = %+v, %v", got, err)
		}
		if _, err := again.Latest(ctx, "acme", "gone"); !errors.Is(err, definition.ErrRetired) {
			t.Fatalf("latest of a retired name after restart = %v", err)
		}
		if r, ok := again.Retired("acme", "gone"); !ok || r.By != "ada" {
			t.Fatalf("retirement after restart = %+v, %v", r, ok)
		}
	})
}

func TestImportKeepsAuthorAndTime(t *testing.T) {
	ctx := context.Background()
	s := agents.New()
	at := time.Date(2025, 3, 4, 5, 6, 7, 0, time.UTC)
	a := definitiontest.Record("acme", "docs-bot", "1.0.0", "One.")
	a.CreatedAt = at
	b := definitiontest.Record("acme", "docs-bot", "2.0.0", "Two.")
	b.CreatedAt, b.CreatedBy = at.Add(time.Hour), "grace"
	if _, err := s.Import(ctx, []definition.Record{a, b}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, "acme", "docs-bot", "2.0.0")
	if err != nil || !got.CreatedAt.Equal(at.Add(time.Hour)) || got.CreatedBy != "grace" {
		t.Fatalf("imported = %+v, %v", got, err)
	}
	if _, err := s.Import(ctx, []definition.Record{a, b}); err != nil {
		t.Fatalf("import again: %v", err)
	}

	// All or none: a bad version leaves nothing written.
	c := definitiontest.Record("acme", "other", "1.0.0", "x")
	c.CreatedAt = at
	bad := definitiontest.Record("acme", "other", "2.0.0", "")
	bad.CreatedAt = at
	if _, err := s.Import(ctx, []definition.Record{c, bad}); !errors.Is(err, definition.ErrInvalid) {
		t.Fatalf("import with a bad version = %v", err)
	}
	if _, err := s.Versions(ctx, "acme", "other"); !errors.Is(err, definition.ErrNotFound) {
		t.Fatalf("a refused import wrote %v", err)
	}
	if _, err := s.Import(ctx, []definition.Record{definitiontest.Record("acme", "x", "1.0.0", "x")}); !errors.Is(err, definition.ErrInvalid) {
		t.Fatalf("import with no time = %v", err)
	}
}
