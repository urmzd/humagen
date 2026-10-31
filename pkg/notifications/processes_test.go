package notifications_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/notifications"
	"github.com/urmzd/humagen/pkg/prefs"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// Two processes on one database, each with its own store, see each other's
// deliveries at once. Reconciling one person's candidates through both at
// the same time keeps every candidate and delivers each once, and
// acknowledging different deliveries through both at once keeps every
// acknowledgment.
func TestTwoProcessesAgree(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		for _, open := range []func(state.Store) (*notifications.Store, error){notifications.Open, notifications.OpenEmail} {
			one, err := open(a)
			if err != nil {
				t.Fatal(err)
			}
			two, err := open(b)
			if err != nil {
				t.Fatal(err)
			}
			now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
			cut := now.Add(-time.Hour)
			policy := prefs.DefaultPersonal()
			policy.Notifications = map[string]prefs.Delivery{"mention": prefs.Immediate}
			const n = 24
			all := make([]notifications.Candidate, n)
			for i := range all {
				all[i] = notifications.Candidate{Message: fmt.Sprintf("m%02d", i), Kind: "mention", Stamp: now}
			}
			lookup := func(id string) (notifications.Candidate, bool) {
				for _, c := range all {
					if c.Message == id {
						return c, true
					}
				}
				return notifications.Candidate{}, false
			}

			if _, err := one.Reconcile("acme", "ada", "page-2", nil, policy, now, cut, lookup); err != nil {
				t.Fatal(err)
			}
			if got, err := two.Cursor("acme", "ada"); err != nil || got != "page-2" {
				t.Fatalf("the other process reads cursor %q, %v", got, err)
			}

			var wg sync.WaitGroup
			for i := range n {
				s := []*notifications.Store{one, two}[i%2]
				wg.Go(func() {
					if _, err := s.Reconcile("acme", "ada", "page-2", all[i:i+1], policy, now, cut, lookup); err != nil {
						t.Error(err)
					}
				})
			}
			wg.Wait()
			deliveries := func(s *notifications.Store) []notifications.Delivery {
				t.Helper()
				got, err := s.Reconcile("acme", "ada", "page-2", all, policy, now, cut, lookup)
				if err != nil {
					t.Fatal(err)
				}
				return got
			}
			for _, s := range []*notifications.Store{one, two} {
				got := deliveries(s)
				seen := map[string]bool{}
				for _, d := range got {
					for _, m := range d.Messages {
						if seen[m] {
							t.Fatalf("%s delivered twice", m)
						}
						seen[m] = true
					}
				}
				// MaxDeliveries caps one answer; every candidate is kept.
				if len(seen) != min(n, notifications.MaxDeliveries) {
					t.Fatalf("%d messages delivered, want %d", len(seen), n)
				}
			}

			got := deliveries(one)
			for i, d := range got {
				s := []*notifications.Store{one, two}[i%2]
				wg.Go(func() {
					if err := s.Ack("acme", "ada", d.ID, func(string) bool { return true }); err != nil {
						t.Error(err)
					}
				})
			}
			wg.Wait()
			if left := deliveries(two); len(left) != 0 {
				t.Fatalf("lost an acknowledgment: %+v", left)
			}
			if left, err := two.Reconcile("other", "ada", "", nil, policy, now, cut, lookup); err != nil || len(left) != 0 {
				t.Fatalf("another tenant reads %+v, %v", left, err)
			}

			var tenants []string
			if err := two.EachTenant(func(tenant string) error { tenants = append(tenants, tenant); return nil }); err != nil {
				t.Fatal(err)
			}
			if len(tenants) != 1 || tenants[0] != "acme" {
				t.Fatalf("tenants %v", tenants)
			}
			if err := two.Retain("acme", now.Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
			// Retention dropped every acknowledged row, so the same
			// candidates are new again.
			if again := deliveries(one); len(again) != n {
				t.Fatalf("retention kept rows: %d deliveries, want %d", len(again), n)
			}
		}
	})
}
