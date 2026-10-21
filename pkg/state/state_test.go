package state_test

import (
	"context"
	"errors"
	"testing"

	"github.com/urmzd/warp/pkg/state"
	"github.com/urmzd/warp/pkg/state/statetest"
)

func TestConformance(t *testing.T) {
	for _, b := range statetest.Backends() {
		t.Run(b.Name, func(t *testing.T) { statetest.Run(t, b.Open) })
	}
}

type doc struct {
	Name string `json:"name"`
	N    int    `json:"n"`
}

func TestTable(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		tab := state.NewTable[doc](st, "docs")
		if err := tab.Put("t", "a", doc{Name: "a", N: 1}); err != nil {
			t.Fatal(err)
		}
		b := tab.Batch()
		b.Put("t", "b", doc{Name: "b", N: 2})
		b.Delete("t", "a")
		if err := b.Commit(); err != nil {
			t.Fatal(err)
		}
		var got []doc
		if err := tab.Load(func(tenant, key string, v doc) error {
			got = append(got, v)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0] != (doc{Name: "b", N: 2}) {
			t.Fatalf("Load = %+v", got)
		}
	})
}

func TestNilTableKeepsNothing(t *testing.T) {
	tab := state.NewTable[doc](nil, "docs")
	if tab.Enabled() {
		t.Fatal("a table on no store is enabled")
	}
	if err := tab.Put("t", "a", doc{}); err != nil {
		t.Fatal(err)
	}
	b := tab.Batch()
	b.Put("t", "a", doc{})
	if b.Len() != 0 || b.Commit() != nil {
		t.Fatal("a nil table's batch collected a change")
	}
	if err := tab.Load(func(string, string, doc) error { t.Fatal("loaded from nothing"); return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestFaultyFailsWrites(t *testing.T) {
	f := statetest.NewFaulty(state.NewMemory())
	tab := state.NewTable[doc](f, "docs")
	f.Fail(statetest.ErrInjected)
	if err := tab.Put("t", "a", doc{}); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Put = %v", err)
	}
	f.Fail(nil)
	if err := tab.Put("t", "a", doc{}); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresIsOneWriter(t *testing.T) {
	st, pool := statetest.OpenPostgres(t)
	ctx := context.Background()
	if _, err := state.OpenPostgres(ctx, pool); !errors.Is(err, state.ErrLocked) {
		t.Fatalf("a second open = %v, want ErrLocked", err)
	}
	if err := st.Write(ctx, "ns", state.Put("t", "k", []byte("v"))); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := state.OpenPostgres(ctx, pool)
	if err != nil {
		t.Fatalf("open after close = %v", err)
	}
	defer func() { _ = again.Close() }()
	if v, err := again.Get(ctx, "ns", "t", "k"); err != nil || string(v) != "v" {
		t.Fatalf("after reopen = %q, %v", v, err)
	}
	// Migrating again is harmless.
	if err := again.Close(); err != nil {
		t.Fatal(err)
	}
	third, err := state.OpenPostgres(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	_ = third.Close()
}
