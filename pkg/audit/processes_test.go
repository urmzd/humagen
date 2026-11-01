package audit

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// Two processes on one database see each other's attempts at once. Attempts
// begun through both at the same instant are each kept once with the
// tenant's count exact, an attempt finished through both at once settles
// once, and retaining through both at once removes each record once.
func TestTwoProcessesAgree(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, err := Open(a)
		if err != nil {
			t.Fatal(err)
		}
		two, err := Open(b)
		if err != nil {
			t.Fatal(err)
		}
		now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
		window := Query{From: now.Add(-time.Hour), To: now.Add(time.Hour), Limit: MaxRows}
		first, err := one.Begin("acme", "ada", Change{Action: "source.create", Resource: "source", ID: "first"}, now)
		if err != nil {
			t.Fatal(err)
		}
		rows, err := two.List("acme", window, nil)
		if err != nil || len(rows) != 1 || rows[0].ID != first.ID || rows[0].Outcome != Pending {
			t.Fatalf("the other process reads %+v, %v", rows, err)
		}

		// Both finish the first attempt at once: one outcome wins, the
		// other is refused as a conflicting outcome or agrees with it.
		var wg sync.WaitGroup
		results := make([]error, 2)
		for i, s := range []*Store{one, two} {
			wg.Go(func() {
				var result error
				if i == 1 {
					result = errors.New("failed")
				}
				results[i] = s.Finish("acme", first.ID, result, now.Add(time.Second))
			})
		}
		wg.Wait()
		if (results[0] == nil) == (results[1] == nil) || !errors.Is(errors.Join(results[0], results[1]), ErrConflict) {
			t.Fatalf("both outcomes: %v", results)
		}

		const writers = 40
		for i := range writers {
			s := []*Store{one, two}[i%2]
			wg.Go(func() {
				when := now.Add(time.Minute)
				if i%4 == 0 {
					when = now.Add(time.Duration(i) * time.Second)
				}
				if err := s.Record("acme", fmt.Sprintf("p%02d", i), Change{Action: "source.create", Resource: "source", ID: "x"}, when); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		rows, err = one.List("acme", window, nil)
		if err != nil || len(rows) != writers+1 {
			t.Fatalf("%d records, %v", len(rows), err)
		}
		for i, r := range rows {
			if r.Outcome == Pending {
				t.Fatalf("record %d still pending: %+v", i, r)
			}
			if i > 0 && newer(r, rows[i-1]) {
				t.Fatalf("not newest first: %+v", rows)
			}
		}
		if n, _, err := two.count("acme"); err != nil || n != writers+1 {
			t.Fatalf("count %d, %v", n, err)
		}

		for _, s := range []*Store{one, two, one, two} {
			wg.Go(func() {
				if err := s.Retain("acme", now.Add(time.Minute)); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		rows, err = two.List("acme", window, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rows {
			if r.At.Before(now.Add(time.Minute)) {
				t.Fatalf("kept %+v", r)
			}
			if err := one.Finish("acme", r.ID, nil, r.At); err != nil {
				t.Fatalf("kept record unreadable by ID: %v", err)
			}
		}
		if err := one.Finish("acme", first.ID, nil, now.Add(time.Second)); !errors.Is(err, ErrNotFound) {
			t.Fatalf("retained record still readable: %v", err)
		}
		if n, _, err := one.count("acme"); err != nil || n != len(rows) {
			t.Fatalf("count %d, holds %d, %v", n, len(rows), err)
		}
		var tenants []string
		if err := two.EachTenant(func(tenant string) error { tenants = append(tenants, tenant); return nil }); err != nil || len(tenants) != 1 || tenants[0] != "acme" {
			t.Fatalf("tenants %v, %v", tenants, err)
		}
	})
}

// newer orders records as List does: by instant, then by ID.
func newer(a, b Record) bool {
	if a.At.Equal(b.At) {
		return a.ID > b.ID
	}
	return a.At.After(b.At)
}

// Records kept by an older release, with no copy under t:, get one once,
// though four stores open at the same time, and the count follows.
func TestTwoProcessesConvertOnce(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
		for i := range 30 {
			r := Record{ID: fmt.Sprintf("a%020d", i+1), Actor: "ada", At: now.Add(time.Duration(i) * time.Second), Outcome: Pending, Change: Change{Action: "source.create", Resource: "source", ID: "old", Scope: "workspace"}}
			raw, _ := json.Marshal(r)
			next, _ := json.Marshal(i + 1)
			tenant := []string{"acme", "beta"}[i%2]
			if err := a.Write(t.Context(), namespace, state.Put(tenant, r.ID, raw), state.Put(tenant, "!next", next)); err != nil {
				t.Fatal(err)
			}
		}
		var wg sync.WaitGroup
		stores := make([]*Store, 4)
		for i := range stores {
			wg.Go(func() {
				s, err := Open([]state.Store{a, b}[i%2])
				if err != nil {
					t.Error(err)
				}
				stores[i] = s
			})
		}
		wg.Wait()
		if t.Failed() {
			return
		}
		window := Query{From: now.Add(-time.Hour), To: now.Add(time.Hour), Limit: MaxRows}
		for _, tenant := range []string{"acme", "beta"} {
			rows, err := stores[3].List(tenant, window, nil)
			if err != nil || len(rows) != 15 {
				t.Fatalf("%s: %d records after converting, %v", tenant, len(rows), err)
			}
			if err := stores[1].Finish(tenant, rows[0].ID, nil, now.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			if _, err := stores[2].Begin(tenant, "ada", Change{Action: "source.create", Resource: "source", ID: "new"}, now); err != nil {
				t.Fatal(err)
			}
			if n, _, err := stores[0].count(tenant); err != nil || n != 16 {
				t.Fatalf("%s: count %d, %v", tenant, n, err)
			}
			copies, _, err := a.List(t.Context(), namespace, tenant, state.Query{Prefix: indexPrefix, Limit: 100})
			if err != nil || len(copies) != 16 {
				t.Fatalf("%s: %d copies, %v", tenant, len(copies), err)
			}
		}
	})
}
