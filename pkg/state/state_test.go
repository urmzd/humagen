package state_test

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
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

func TestTablePerRequest(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		tab := state.NewTable[doc](open(t), "docs")
		b := tab.Batch()
		b.Create("t", "a", doc{Name: "a", N: 1})
		b.Create("t", "b", doc{Name: "b", N: 2})
		if err := b.Commit(); err != nil {
			t.Fatal(err)
		}
		it, err := tab.Lookup("t", "a")
		if err != nil || it.Value != (doc{Name: "a", N: 1}) || it.Version != 1 || it.Key != "a" {
			t.Fatalf("Lookup = %+v, %v", it, err)
		}
		b = tab.Batch()
		b.PutAt("t", "a", doc{Name: "a", N: 3}, it.Version)
		if err := b.Commit(); err != nil {
			t.Fatal(err)
		}
		b = tab.Batch()
		b.PutAt("t", "a", doc{Name: "a", N: 4}, it.Version)
		if err := b.Commit(); !errors.Is(err, state.ErrConflict) {
			t.Fatalf("a stale PutAt = %v", err)
		}
		b = tab.Batch()
		b.DeleteAt("t", "b", 0)
		if err := b.Commit(); !errors.Is(err, state.ErrInvalid) {
			t.Fatalf("DeleteAt(0) = %v", err)
		}
		items, next, err := tab.List("t", state.Query{Limit: 1})
		if err != nil || len(items) != 1 || items[0].Value.N != 3 || items[0].Version != 2 || next != "a" {
			t.Fatalf("List = %+v, %q, %v", items, next, err)
		}
		items, next, err = tab.List("t", state.Query{After: next, Limit: 1})
		if err != nil || len(items) != 1 || items[0].Key != "b" || next != "" {
			t.Fatalf("List after = %+v, %q, %v", items, next, err)
		}
		if _, err := tab.Lookup("t", "c"); !errors.Is(err, state.ErrNotFound) {
			t.Fatalf("Lookup(missing) = %v", err)
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
	if _, err := tab.Lookup("t", "a"); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("Lookup = %v", err)
	}
	if items, next, err := tab.List("t", state.Query{Limit: 1}); len(items) != 0 || next != "" || err != nil {
		t.Fatal("a nil table listed something")
	}
	b = tab.Batch()
	b.Create("t", "a", doc{})
	b.PutAt("t", "a", doc{}, 0)
	b.DeleteAt("t", "a", 0)
	if b.Len() != 0 || b.Commit() != nil {
		t.Fatal("a nil table's batch collected a conditional change")
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

// Two processes open one database at once, and each reads what the other
// wrote: OpenPostgres takes no lock.
func TestPostgresTakesNoLock(t *testing.T) {
	st, pool := statetest.OpenPostgres(t)
	ctx := context.Background()
	for range 2 {
		other, err := state.OpenPostgres(ctx, pool)
		if err != nil {
			t.Fatalf("a second open = %v", err)
		}
		if err := other.Write(ctx, "ns", state.Create("t", "k", []byte("v"))); err != nil && !errors.Is(err, state.ErrConflict) {
			t.Fatal(err)
		}
		if err := other.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if v, err := st.Get(ctx, "ns", "t", "k"); err != nil || string(v) != "v" {
		t.Fatalf("Get = %q, %v", v, err)
	}
	shared, err := state.OpenPostgresShared(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if v, err := shared.Get(ctx, "ns", "t", "k"); err != nil || string(v) != "v" {
		t.Fatalf("shared Get = %q, %v", v, err)
	}
}

// Two processes changing one record by compare-and-set lose no change.
func TestProcessesAgreeByVersion(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		ctx := context.Background()
		if err := a.Write(ctx, "ns", state.Create("t", "n", []byte("0"))); err != nil {
			t.Fatal(err)
		}
		const each = 20
		var wg sync.WaitGroup
		for i := range 8 {
			st := []state.Store{a, b}[i%2]
			wg.Go(func() {
				for range each {
					for {
						r, err := st.Lookup(ctx, "ns", "t", "n")
						if err != nil {
							t.Error(err)
							return
						}
						n, _ := strconv.Atoi(string(r.Value))
						err = st.Write(ctx, "ns", state.PutAt("t", "n", []byte(strconv.Itoa(n+1)), r.Version))
						if errors.Is(err, state.ErrConflict) {
							continue
						}
						if err != nil {
							t.Error(err)
						}
						break
					}
				}
			})
		}
		wg.Wait()
		for _, st := range []state.Store{a, b} {
			if v, err := st.Get(ctx, "ns", "t", "n"); err != nil || string(v) != strconv.Itoa(8*each) {
				t.Fatalf("n = %q, %v; want %d", v, err, 8*each)
			}
		}
	})
}
