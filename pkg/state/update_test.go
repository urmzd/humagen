package state_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// Two processes counting one record through Update lose no change, and a
// function that leaves the record alone writes nothing.
func TestUpdateAcrossProcesses(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		tables := []*state.Table[int]{state.NewTable[int](a, "ns"), state.NewTable[int](b, "ns")}
		const each = 20
		var wg sync.WaitGroup
		for i := range 8 {
			tb := tables[i%2]
			wg.Go(func() {
				for range each {
					if _, err := tb.Update("t", "n", func(n int, _ bool) (int, error) { return n + 1, nil }); err != nil {
						t.Error(err)
					}
				}
			})
		}
		wg.Wait()
		for _, tb := range tables {
			it, err := tb.Lookup("t", "n")
			if err != nil || it.Value != 8*each {
				t.Fatalf("n = %+v, %v; want %d", it, err, 8*each)
			}
		}
		before, _ := tables[0].Lookup("t", "n")
		v, err := tables[1].Update("t", "n", func(int, bool) (int, error) { return 0, state.ErrUnchanged })
		after, _ := tables[0].Lookup("t", "n")
		if err != nil || v != 8*each || after.Version != before.Version {
			t.Fatalf("Update(ErrUnchanged) = %d, %v; version %d -> %d", v, err, before.Version, after.Version)
		}
		boom := errors.New("boom")
		if _, err := tables[0].Update("t", "n", func(int, bool) (int, error) { return 0, boom }); !errors.Is(err, boom) {
			t.Fatalf("Update kept its function's error as %v", err)
		}
	})
}

func TestRetryGivesUp(t *testing.T) {
	n := 0
	if err := state.Retry(func() error { n++; return state.ErrConflict }); !errors.Is(err, state.ErrBusy) || n != state.MaxTries {
		t.Fatalf("Retry = %v after %d tries", err, n)
	}
}

func TestAllIsBounded(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(t *testing.T) state.Store) {
		tb := state.NewTable[int](open(t), "ns")
		b := tb.Batch()
		for i := range 30 {
			b.Put("t", fmt.Sprintf("k:%02d", i), i)
		}
		b.Put("t", "other", 0)
		if err := b.Commit(); err != nil {
			t.Fatal(err)
		}
		got, err := tb.All("t", "k:", 30)
		if err != nil || len(got) != 30 || got[29].Value != 29 {
			t.Fatalf("All = %d records, %v", len(got), err)
		}
		if _, err := tb.All("t", "k:", 29); !errors.Is(err, state.ErrTooMany) {
			t.Fatalf("All over its bound = %v", err)
		}
	})
}

// The directory holds the tenants written before it existed and the ones
// marked after, once each, though two processes fill and mark it together.
func TestTenantsAcrossProcesses(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		tb := state.NewTable[int](a, "ns")
		for _, tenant := range []string{"acme", "beta", "acme"} {
			if err := tb.Put(tenant, "k:"+tenant, 1); err != nil {
				t.Fatal(err)
			}
		}
		var wg sync.WaitGroup
		dirs := make([]*state.Tenants, 4)
		for i := range dirs {
			wg.Go(func() {
				d, err := state.OpenTenants([]state.Store{a, b}[i%2], "ns")
				if err != nil {
					t.Error(err)
				}
				dirs[i] = d
			})
		}
		wg.Wait()
		if t.Failed() {
			return
		}
		for i := range 4 {
			wg.Go(func() {
				if err := dirs[i].Mark(fmt.Sprintf("t%d", i%2)); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		var got []string
		if err := dirs[3].Each(func(tenant string) error { got = append(got, tenant); return nil }); err != nil {
			t.Fatal(err)
		}
		if fmt.Sprint(got) != "[acme beta t0 t1]" {
			t.Fatalf("tenants = %v", got)
		}
		page, next, err := dirs[0].Page("acme", 2)
		if err != nil || fmt.Sprint(page) != "[beta t0]" || next != "t0" {
			t.Fatalf("Page = %v, %q, %v", page, next, err)
		}
	})
}
