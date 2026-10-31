package readaudit_test

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/readaudit"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// Two processes on one database see each other's records at once, and
// reads recorded through both at the same instant are each kept once, with
// the tenant's count exact; retaining through both at once removes each
// record once.
func TestTwoProcessesAgree(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, err := readaudit.Open(a)
		if err != nil {
			t.Fatal(err)
		}
		two, err := readaudit.Open(b)
		if err != nil {
			t.Fatal(err)
		}
		at := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
		if err := one.Add("acme", readaudit.Record{Subject: "ana", Route: "GET /x", At: at}); err != nil {
			t.Fatal(err)
		}
		if got := list(t, two, "acme", at.Add(-time.Hour), at.Add(time.Hour)); len(got) != 1 || got[0].Subject != "ana" {
			t.Fatalf("the other process reads %+v", got)
		}

		const readers = 40
		var wg sync.WaitGroup
		for i := range readers {
			s := []*readaudit.Store{one, two}[i%2]
			wg.Go(func() {
				// Half at one instant, so keys made at once must not collide.
				when := at.Add(time.Minute)
				if i%4 == 0 {
					when = at.Add(time.Duration(i) * time.Second)
				}
				if err := s.Add("acme", readaudit.Record{Subject: fmt.Sprintf("p%02d", i), Route: "GET /x", At: when}); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		got, err := one.List("acme", at.Add(-time.Hour), at.Add(time.Hour), readaudit.MaxRows)
		if err != nil || len(got) != readers+1 {
			t.Fatalf("%d records, %v", len(got), err)
		}
		for i := 1; i < len(got); i++ {
			if got[i].At.After(got[i-1].At) {
				t.Fatalf("not newest first: %+v", got)
			}
		}
		if n := count(t, a, "acme"); n != readers+1 {
			t.Fatalf("count %d, want %d", n, readers+1)
		}

		// Both retain everything before the shared minute at once.
		for _, s := range []*readaudit.Store{one, two, one, two} {
			wg.Go(func() {
				if err := s.Retain("acme", at.Add(time.Minute)); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		kept := list(t, two, "acme", at.Add(-time.Hour), at.Add(time.Hour))
		for _, r := range kept {
			if r.At.Before(at.Add(time.Minute)) {
				t.Fatalf("kept %+v", r)
			}
		}
		if n := count(t, b, "acme"); n != len(kept) {
			t.Fatalf("count %d, holds %d", n, len(kept))
		}
		var tenants []string
		if err := two.EachTenant(func(tenant string) error { tenants = append(tenants, tenant); return nil }); err != nil || len(tenants) != 1 || tenants[0] != "acme" {
			t.Fatalf("tenants %v, %v", tenants, err)
		}
	})
}

// Records kept by an older release, one per global sequence number, are
// moved once, though four stores open at the same time.
func TestTwoProcessesConvertOnce(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		at := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
		for i := range 30 {
			raw, _ := json.Marshal(readaudit.Record{Subject: fmt.Sprintf("p%02d", i), Route: "GET /x", At: at.Add(time.Duration(30-i) * time.Second)})
			if err := a.Write(t.Context(), "read-audit", state.Put([]string{"acme", "beta"}[i%2], state.Seq(int64(i+1)), raw)); err != nil {
				t.Fatal(err)
			}
		}
		var wg sync.WaitGroup
		stores := make([]*readaudit.Store, 4)
		for i := range stores {
			wg.Go(func() {
				s, err := readaudit.Open([]state.Store{a, b}[i%2])
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
		for _, tenant := range []string{"acme", "beta"} {
			got := list(t, stores[3], tenant, at.Add(-time.Hour), at.Add(time.Hour))
			if len(got) != 15 {
				t.Fatalf("%s: %d records after converting, want 15", tenant, len(got))
			}
			for i := 1; i < len(got); i++ {
				if !got[i].At.Before(got[i-1].At) {
					t.Fatalf("%s out of order: %+v", tenant, got)
				}
			}
			if err := stores[0].Add(tenant, readaudit.Record{Subject: "new", Route: "GET /x", At: at}); err != nil {
				t.Fatal(err)
			}
			if n := count(t, a, tenant); n != 16 {
				t.Fatalf("%s: count %d, want 16", tenant, n)
			}
		}
		recs, err := a.Load(t.Context(), "read-audit")
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range recs {
			if len(r.Key) == 20 {
				t.Fatalf("left under an old key: %q/%q", r.Tenant, r.Key)
			}
		}
	})
}

func count(t *testing.T, st state.Store, tenant string) int {
	t.Helper()
	raw, err := st.Get(t.Context(), "read-audit", tenant, "count")
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := json.Unmarshal(raw, &n); err != nil {
		t.Fatal(err)
	}
	return n
}
