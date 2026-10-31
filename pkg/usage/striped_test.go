package usage

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/policy"
)

// race admits n runs from racers spread over ls, each under quotas, and
// returns how many were let in and how many were refused by a quota.
func race(t *testing.T, ls []*Ledger, n int, entry func(i int) Entry, quotas ...Quota) (in, full int64) {
	t.Helper()
	var admitted, refused atomic.Int64
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			ok, q, err := ls[i%len(ls)].AdmitQuotas(entry(i), nil, quotas)
			switch {
			case err != nil:
				t.Error(err)
			case ok:
				admitted.Add(1)
			case q != nil:
				refused.Add(1)
			default:
				t.Errorf("run %d refused by nobody", i)
			}
		})
	}
	wg.Wait()
	return admitted.Load(), refused.Load()
}

// Many racers over two processes against limits that span every stripe, and
// that a stripe's share alone cannot decide, admit exactly the limit, and
// every refusal names the quota.
func TestStripedQuotasAreExactAcrossProcesses(t *testing.T) {
	for _, limit := range []int{1, 3, 8, 13, 100} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			ls, _ := processes(t, 2)
			racers := limit*2 + 20
			in, full := race(t, ls, racers, func(i int) Entry {
				return Entry{Tenant: "acme", Project: "Launch", Agent: fmt.Sprintf("a%d", i%5), RunID: fmt.Sprintf("r%d", i), At: t0.Add(time.Duration(i) * time.Second)}
			}, Quota{Scope: all, Kind: QuotaRuns, Since: t0, Max: limit})
			if in != int64(limit) || full != int64(racers-limit) {
				t.Fatalf("admitted %d and refused %d of %d under %d", in, full, racers, limit)
			}
			for i, l := range ls {
				if u := l.used("acme", all, t0); u.Runs != limit || u.Concurrent != limit {
					t.Fatalf("process %d reads %+v", i, u)
				}
			}
		})
	}
}

// Quotas on several scopes at once each hold: runs in the tenant, and runs
// in flight in one project, with runs ending through the other process as
// they go.
func TestStripedQuotasHoldTogether(t *testing.T) {
	ls, _ := processes(t, 2)
	quotas := []Quota{
		{Scope: all, Kind: QuotaRuns, Since: t0, Max: 30},
		{Scope: launch, Kind: QuotaConcurrent, Max: 4},
	}
	var inFlight, peak, admitted atomic.Int64
	var wg sync.WaitGroup
	for i := range 80 {
		wg.Go(func() {
			l := ls[i%2]
			e := Entry{Tenant: "acme", Project: "Launch", Agent: "lead", RunID: fmt.Sprintf("r%d", i), At: t0.Add(time.Second)}
			ok, _, err := l.AdmitQuotas(e, nil, quotas)
			if err != nil || !ok {
				return
			}
			admitted.Add(1)
			now := inFlight.Add(1)
			for {
				p := peak.Load()
				if now <= p || peak.CompareAndSwap(p, now) {
					break
				}
			}
			time.Sleep(time.Millisecond)
			// Counted down before the end is recorded, so the count here
			// is never below the one the counters hold.
			inFlight.Add(-1)
			if _, err := ls[(i+1)%2].Append(Entry{Kind: Ended, Tenant: "acme", RunID: e.RunID, At: e.At}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if peak.Load() > 4 {
		t.Fatalf("%d runs in flight at once in Launch, over 4", peak.Load())
	}
	if admitted.Load() > 30 {
		t.Fatalf("admitted %d, over 30", admitted.Load())
	}
	if u := ls[0].used("acme", all, t0); u.Runs != int(admitted.Load()) || u.Concurrent != 0 {
		t.Fatalf("reads %+v after %d admitted and all ended", u, admitted.Load())
	}
}

// A limit lowered below what the stripes already hold refuses until the
// total is under it, though some stripes are under their new share.
func TestStripedQuotaHoldsALoweredLimit(t *testing.T) {
	ls, _ := processes(t, 2)
	in, _ := race(t, ls, 40, func(i int) Entry {
		return Entry{Tenant: "acme", Agent: "lead", RunID: fmt.Sprintf("r%d", i), At: t0}
	}, Quota{Scope: all, Kind: QuotaConcurrent, Max: 40})
	if in != 40 {
		t.Fatalf("admitted %d of 40", in)
	}
	// End ten, then lower the limit to 35: five may start, not ten.
	for i := range 10 {
		if _, err := ls[0].Append(Entry{Kind: Ended, Tenant: "acme", RunID: fmt.Sprintf("r%d", i), At: t0}); err != nil {
			t.Fatal(err)
		}
	}
	in, _ = race(t, ls, 30, func(i int) Entry {
		return Entry{Tenant: "acme", Agent: "lead", RunID: fmt.Sprintf("s%d", i), At: t0}
	}, Quota{Scope: all, Kind: QuotaConcurrent, Max: 35})
	if in != 5 {
		t.Fatalf("admitted %d under a lowered limit, want 5", in)
	}
}

// Runs counted before striped admission, in the day counts and the runs in
// flight, count against a quota.
func TestStripedQuotaCountsWhatAnOlderReleaseCounted(t *testing.T) {
	ls, _ := processes(t, 1)
	pool := ls[0].shared.pool
	ctx := t.Context()
	day := policy.PeriodStart(policy.Day, t0).Unix()
	if _, err := pool.Exec(ctx, `INSERT INTO humagen_usage_days (tenant, scope, day, runs) VALUES ('acme', 't', $1, 3)`, day); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO humagen_usage_active (tenant, run_id, project, within, agent, client) VALUES ('acme', 'old', '', '{}', 'lead', '')`); err != nil {
		t.Fatal(err)
	}
	// What a release before slots left in flight is given its slots when
	// the counters are opened.
	if _, err := pool.Exec(ctx, `DELETE FROM humagen_usage_meta WHERE id = 2`); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenCounters(ctx, pool); err != nil {
		t.Fatal(err)
	}
	in, _ := race(t, ls, 10, func(i int) Entry {
		return Entry{Tenant: "acme", Agent: "lead", RunID: fmt.Sprintf("r%d", i), At: t0}
	}, Quota{Scope: all, Kind: QuotaRuns, Since: t0, Max: 5})
	if in != 2 {
		t.Fatalf("admitted %d with 3 of 5 already counted", in)
	}
	in, _ = race(t, ls, 10, func(i int) Entry {
		return Entry{Tenant: "acme", Agent: "lead", RunID: fmt.Sprintf("s%d", i), At: t0}
	}, Quota{Scope: all, Kind: QuotaConcurrent, Max: 4})
	if in != 1 {
		t.Fatalf("admitted %d with 3 of 4 in flight", in)
	}
}

// In memory, a quota is judged under the ledger's lock, as the judge is.
func TestQuotasHoldInMemory(t *testing.T) {
	l := NewLedger()
	in, full := race(t, []*Ledger{l}, 30, func(i int) Entry {
		return Entry{Tenant: "acme", Project: "Launch", Agent: "lead", RunID: fmt.Sprintf("r%d", i), At: t0}
	}, Quota{Scope: launch, Kind: QuotaRuns, Since: t0, Max: 6}, Quota{Scope: all, Kind: QuotaConcurrent, Max: 9})
	if in != 6 || full != 24 {
		t.Fatalf("admitted %d, refused %d", in, full)
	}
}
