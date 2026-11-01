package catalog

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// Two processes on one database see each other's connectors and crawls at
// once, including a search the other process had read before the crawl.
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
		seed(t, one)
		if hits, err := two.Search("acme", []string{"warehouse"}, "invoice", 5); err != nil || len(hits) != 1 {
			t.Fatalf("the other process finds %v, %v", paths(hits), err)
		}
		if _, err := one.Replace("acme", "warehouse", []Entry{table("analytics.billing.refunds", "Every refund")}, at.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		if hits, _ := two.Search("acme", []string{"warehouse"}, "invoice", 5); len(hits) != 0 {
			t.Fatalf("the other process still finds %v", paths(hits))
		}
		if hits, _ := two.Search("acme", []string{"warehouse"}, "refund", 5); len(hits) != 1 {
			t.Fatalf("the other process misses the new crawl: %v", paths(hits))
		}
		if _, err := two.Publish("acme", "warehouse", "ana", at); err != nil {
			t.Fatal(err)
		}
		if c, err := one.Connector("acme", "warehouse"); err != nil || c.Published == nil {
			t.Fatalf("the other process reads %+v, %v", c, err)
		}
		var tenants []string
		if err := two.EachTenant(func(tenant string) error { tenants = append(tenants, tenant); return nil }); err != nil || !slices.Equal(tenants, []string{"acme"}) {
			t.Fatalf("tenants = %v, %v", tenants, err)
		}
	})
}

// Crawls of one connector through both processes at once are each kept, in
// some order: the generation counts every one, and the entries are exactly
// one crawl's.
func TestTwoProcessesCrawlOneConnector(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, _ := Open(a)
		two, _ := Open(b)
		if _, err := one.Declare("acme", Connector{Name: "warehouse", System: "bigquery", Owner: "ana"}); err != nil {
			t.Fatal(err)
		}
		const crawls = 12
		var wg sync.WaitGroup
		for i := range crawls {
			s := []*Store{one, two}[i%2]
			wg.Go(func() {
				var es []Entry
				for j := range 3 {
					es = append(es, table(fmt.Sprintf("crawl%02d.t%d", i, j), "a table"))
				}
				if _, err := s.Replace("acme", "warehouse", es, at); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		c, err := two.Connector("acme", "warehouse")
		if err != nil || c.Generation != crawls || c.Entries != 3 {
			t.Fatalf("connector = %+v, %v", c, err)
		}
		got := list(t, one, "acme", []string{"warehouse"}, 100)
		if len(got) != 3 || got[0].Path[:7] != got[2].Path[:7] {
			t.Fatalf("entries mix crawls: %+v", got)
		}
	})
}

// Connectors declared through both processes at once stop at MaxConnectors.
func TestTwoProcessesKeepTheConnectorBound(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, _ := Open(a)
		two, _ := Open(b)
		for i := range MaxConnectors - 5 {
			if _, err := one.Declare("acme", Connector{Name: fmt.Sprintf("c%03d", i), System: "bigquery"}); err != nil {
				t.Fatal(err)
			}
		}
		var kept, refused atomic.Int64
		var wg sync.WaitGroup
		for i := MaxConnectors - 5; i < MaxConnectors+10; i++ {
			s := []*Store{one, two}[i%2]
			wg.Go(func() {
				_, err := s.Declare("acme", Connector{Name: fmt.Sprintf("c%03d", i), System: "bigquery"})
				switch {
				case err == nil:
					kept.Add(1)
				case errors.Is(err, ErrInvalid):
					refused.Add(1)
				default:
					t.Error(err)
				}
			})
		}
		wg.Wait()
		cs, err := two.Connectors("acme")
		if err != nil || len(cs) != MaxConnectors || kept.Load() != 5 || refused.Load() != 10 {
			t.Fatalf("%d connectors, kept %d, refused %d (%v)", len(cs), kept.Load(), refused.Load(), err)
		}
	})
}
