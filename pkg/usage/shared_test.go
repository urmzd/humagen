package usage

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/policy"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// processes is n humagen processes' ledgers on one database: each its own
// pool, its own shared state store and its own counters. The store returned
// is the first process's.
func processes(t *testing.T, n int) ([]*Ledger, state.Store) {
	t.Helper()
	out := make([]*Ledger, n)
	var first state.Store
	for i, p := range statetest.Pools(t, n) {
		st, err := state.OpenPostgresShared(t.Context(), p)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = st
		}
		c, err := OpenCounters(t.Context(), p)
		if err != nil {
			t.Fatal(err)
		}
		if out[i], err = Open(st, WithCounters(c)); err != nil {
			t.Fatal(err)
		}
	}
	return out, first
}

// Many racers in two processes admit against one run limit. Exactly the
// limit is let in, and both processes read the same count.
func TestSharedAdmissionHoldsTheLimitAcrossProcesses(t *testing.T) {
	ls, _ := processes(t, 2)
	const limit, racers = 7, 40
	var in atomic.Int64
	var wg sync.WaitGroup
	for i := range racers {
		wg.Go(func() {
			l := ls[i%len(ls)]
			ok, err := l.Admit(Entry{Tenant: "acme", Project: "Launch", Agent: "lead", RunID: fmt.Sprintf("r%d", i), At: t0.Add(time.Duration(i) * time.Second)},
				nil, Quota{Scope: launch, Kind: QuotaRuns, Since: t0, Max: limit})
			if err != nil {
				t.Error(err)
			}
			if ok {
				in.Add(1)
			}
		})
	}
	wg.Wait()
	if in.Load() != limit {
		t.Fatalf("admitted %d runs under a limit of %d", in.Load(), limit)
	}
	for i, l := range ls {
		if u := l.used("acme", launch, t0); u.Runs != limit || u.Concurrent != limit {
			t.Fatalf("process %d reads %+v, want %d runs, %d in flight", i, u, limit, limit)
		}
	}
}

// A run's place is released by whichever process sees it end, and the
// other process's next admission sees the room.
func TestSharedConcurrencyIsReleasedAcrossProcesses(t *testing.T) {
	ls, _ := processes(t, 2)
	underOne := Quota{Scope: all, Kind: QuotaConcurrent, Max: 1}
	if ok, err := ls[0].Admit(Entry{Tenant: "acme", Agent: "lead", RunID: "r1", At: t0}, nil, underOne); err != nil || !ok {
		t.Fatalf("first admission: %v, %v", ok, err)
	}
	if ok, err := ls[1].Admit(Entry{Tenant: "acme", Agent: "lead", RunID: "r2", At: t0}, nil, underOne); err != nil || ok {
		t.Fatalf("second process admitted beside a run in flight: %v, %v", ok, err)
	}
	if _, err := ls[0].Append(Entry{Kind: Ended, Tenant: "acme", RunID: "r1", At: t0.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if ok, err := ls[1].Admit(Entry{Tenant: "acme", Agent: "lead", RunID: "r2", At: t0.Add(time.Minute)}, nil, underOne); err != nil || !ok {
		t.Fatalf("second process refused after the first run ended: %v, %v", ok, err)
	}
}

// Spend reported to two processes, some calls to both, is counted once
// per call, every process reads the whole of it, and a ledger rebuilt from
// the entries both wrote answers the same.
func TestSharedSpendIsCountedOnceAcrossProcesses(t *testing.T) {
	ls, _ := processes(t, 2)
	for i, l := range ls {
		admit(l, "acme", "Launch", "lead", fmt.Sprintf("r%d", i), t0)
	}
	var wg sync.WaitGroup
	var counted atomic.Int64
	for i := range 60 {
		for _, l := range ls {
			wg.Go(func() {
				// Every call is delivered to both processes.
				if call(l, "acme", fmt.Sprintf("r%d", i%2), fmt.Sprintf("call-%d", i), t0.Add(time.Duration(i)*time.Second),
					Spend{Micros: 100, ModelCalls: 1, Tokens: 10, Other: []policy.Cost{{Micros: 5, Currency: "EUR"}}}) {
					counted.Add(1)
				}
			})
		}
	}
	wg.Wait()
	if counted.Load() != 60 {
		t.Fatalf("%d calls counted, want 60: one per call", counted.Load())
	}
	// r0 settles a total that covers calls neither process saw.
	if _, _, err := ls[1].Record(Entry{Kind: Settled, Tenant: "acme", Project: "Launch", Agent: "lead", RunID: "r0", Key: "run:r0",
		Spend: &Spend{Micros: 3100, ModelCalls: 31, Tokens: 310}, At: t0.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	want := policy.Usage{Runs: 2, Concurrent: 2, Spend: 6100, Tokens: 610, ModelCalls: 61, OtherCosts: []policy.Cost{{Micros: 300, Currency: "EUR"}}}
	for i, l := range ls {
		if u := l.used("acme", launch, t0); !reflect.DeepEqual(u, want) {
			t.Fatalf("process %d reads %+v\nwant %+v", i, u, want)
		}
	}
	if u := Rebuild(ls[0].all("acme")).used("acme", launch, t0); !reflect.DeepEqual(u, want) {
		t.Fatalf("rebuilt from the entries: %+v\nwant %+v", u, want)
	}
}

// oldLedger writes entries as a release before this layout kept them:
// every tenant's in the tenant "", under its place alone.
func oldLedger(t *testing.T, st state.Store, entries []Entry) {
	t.Helper()
	b := state.NewTable[Entry](st, "usage").Batch()
	for i, e := range entries {
		b.Put("", state.Seq(int64(i+1)), e)
	}
	if err := b.Commit(); err != nil {
		t.Fatal(err)
	}
}

var oldEntries = []Entry{
	{Kind: Admitted, Tenant: "acme", Project: "Launch", Agent: "lead", RunID: "r1", At: t0},
	{Kind: Admitted, Tenant: "acme", Project: "Launch", Agent: "lead", RunID: "r2", At: t0},
	{Kind: Spent, Tenant: "acme", Project: "Launch", Agent: "lead", RunID: "r1", Key: "c1", Spend: &Spend{Micros: 250, ModelCalls: 1, Tokens: 40}, At: t0},
	{Kind: Ended, Tenant: "acme", RunID: "r2", At: t0},
	{Kind: Granted, Tenant: "acme", Project: "Launch", Agent: "lead", RunID: "r1", At: t0, Scope: &launch, Grant: &policy.Grant{Spend: 9}, By: "ana"},
	{Kind: Admitted, Tenant: "globex", Agent: "lead", RunID: "g1", At: t0},
}

// The entries of a release before this layout, opened on counters for the
// first time by two processes at once, fill the counters once and move to
// their tenants once.
func TestSharedCountersAreFilledFromAnEarlierLedger(t *testing.T) {
	pools := statetest.Pools(t, 2)
	var sts [2]state.Store
	for i, p := range pools {
		st, err := state.OpenPostgresShared(t.Context(), p)
		if err != nil {
			t.Fatal(err)
		}
		sts[i] = st
	}
	oldLedger(t, sts[0], oldEntries)
	want := Rebuild(oldEntries).used("acme", launch, t0)

	var ls [2]*Ledger
	var wg sync.WaitGroup
	for i, p := range pools {
		wg.Go(func() {
			c, err := OpenCounters(context.Background(), p)
			if err != nil {
				t.Error(err)
				return
			}
			if ls[i], err = Open(sts[i], WithCounters(c)); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if t.Failed() {
		t.FailNow()
	}
	for i, l := range ls {
		if u := l.used("acme", launch, t0); !reflect.DeepEqual(u, want) {
			t.Fatalf("process %d reads %+v\nwant %+v", i, u, want)
		}
		if n := len(l.all("acme")); n != 5 {
			t.Fatalf("process %d reads %d entries, want 5", i, n)
		}
		if g := l.granted("acme", "r1", launch, t0); g.Spend != 9 {
			t.Fatalf("process %d reads the old grant as %+v", i, g)
		}
	}
	if recs, _, err := sts[0].List(t.Context(), "usage", "", state.Query{Prefix: "0", Limit: 10}); err != nil || len(recs) != 0 {
		t.Fatalf("old entries left behind: %d, %v", len(recs), err)
	}
	// The call already counted is not counted again, and new entries take
	// places past the old ones.
	if call(ls[1], "acme", "r1", "c1", t0, Spend{Micros: 250, ModelCalls: 1, Tokens: 40}) {
		t.Fatal("a call counted before the counters existed was counted again")
	}
	admit(ls[0], "acme", "Launch", "lead", "r3", t0)
	if e := ls[1].all("acme"); len(e) != 6 || e[5].RunID != "r3" {
		t.Fatalf("after a new admission: %+v", e)
	}
}

// Without counters, two processes converting at once move each entry once,
// so nothing is counted twice.
func TestTwoProcessesConvertOnce(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		oldLedger(t, a, oldEntries)
		var ls [2]*Ledger
		var wg sync.WaitGroup
		for i, st := range []state.Store{a, b} {
			wg.Go(func() {
				var err error
				if ls[i], err = Open(st); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		if t.Failed() {
			t.FailNow()
		}
		want := Rebuild(oldEntries)
		for i, l := range ls {
			for _, tenant := range []string{"acme", "globex"} {
				if got, w := l.used(tenant, all, t0), want.used(tenant, all, t0); !reflect.DeepEqual(got, w) {
					t.Fatalf("process %d, %s: %+v, want %+v", i, tenant, got, w)
				}
			}
			if n := len(l.all("acme")); n != 5 {
				t.Fatalf("process %d reads %d entries, want 5", i, n)
			}
		}
	})
}

// Without counters, many racers in one process admit exactly the limit, and
// so does a judge, which reads under the ledger's lock.
func TestAdmitHoldsTheLimitInMemory(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		l, err := Open(open(t))
		if err != nil {
			t.Fatal(err)
		}
		const limit = 5
		var in atomic.Int64
		var wg sync.WaitGroup
		for i := range 50 {
			wg.Go(func() {
				ok, err := l.Admit(Entry{Tenant: "acme", Agent: "lead", RunID: fmt.Sprintf("r%d", i), At: t0}, nil, Quota{Scope: all, Kind: QuotaRuns, Since: t0, Max: limit})
				if err != nil {
					t.Error(err)
				}
				if ok {
					in.Add(1)
				}
			})
		}
		wg.Wait()
		if in.Load() != limit {
			t.Fatalf("admitted %d under a limit of %d", in.Load(), limit)
		}
	})
}
