package usage

import (
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

// pair is two processes' ledgers on one database: without counters on the
// stores statetest.Processes gives, and with counters on two pools.
func pairs(t *testing.T, fn func(t *testing.T, a, b *Ledger)) {
	t.Helper()
	statetest.Processes(t, func(t *testing.T, sa, sb state.Store) {
		a, err := Open(sa)
		if err != nil {
			t.Fatal(err)
		}
		b, err := Open(sb)
		if err != nil {
			t.Fatal(err)
		}
		fn(t, a, b)
	})
	t.Run("counters", func(t *testing.T) {
		ls, _ := processes(t, 2)
		fn(t, ls[0], ls[1])
	})
}

// What one process writes, the other reads at once: counts, a run's spend,
// notices, grants and the entries themselves.
func TestTwoProcessesSeeEachOthersWrites(t *testing.T) {
	pairs(t, func(t *testing.T, a, b *Ledger) {
		admit(a, "acme", "Launch", "lead", "r1", t0)
		call(a, "acme", "r1", "r1/1", t0.Add(time.Minute), Spend{Micros: 300, ModelCalls: 1, Tokens: 30})
		if _, err := a.Append(Entry{Kind: Warned, Tenant: "acme", Agent: "lead", At: t0, Scope: &launch, Version: 1, Limit: policy.LimitSpend, Reason: "close"}); err != nil {
			t.Fatal(err)
		}
		if _, err := a.Append(Entry{Kind: Granted, Tenant: "acme", Project: "Launch", Agent: "lead", RunID: "r1", At: t0, Scope: &launch,
			Limit: policy.LimitSpend, Grant: &policy.Grant{Spend: 50}, By: "ana"}); err != nil {
			t.Fatal(err)
		}
		if u := b.used("acme", launch, t0); u.Runs != 1 || u.Concurrent != 1 || u.Spend != 300 || u.Tokens != 30 {
			t.Fatalf("the other process reads %+v", u)
		}
		if s := b.runSpend("acme", "r1"); s.Micros != 300 {
			t.Fatalf("the other process reads the run's spend as %+v", s)
		}
		if n := b.notices("acme", 0); len(n) != 2 || n[0].Kind != Granted || n[1].Reason != "close" {
			t.Fatalf("the other process reads notices %+v", n)
		}
		if g := b.granted("acme", "r1", launch, t0); g.Spend != 50 {
			t.Fatalf("the other process reads the grant as %+v", g)
		}
		if e := b.all("acme"); len(e) != 4 {
			t.Fatalf("the other process reads %d entries, want 4", len(e))
		}
		// And a run ended through the other leaves flight for both.
		if _, err := b.Append(Entry{Kind: Ended, Tenant: "acme", RunID: "r1", At: t0.Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
		if u := a.used("acme", launch, t0); u.Concurrent != 0 {
			t.Fatalf("after the other ended the run: %+v", u)
		}
	})
}

// Racers in two processes write at once: admissions, ends, and calls each
// delivered to both. Every entry is kept once: nothing is lost to a write
// that raced it, and no call is counted twice.
func TestTwoProcessesKeepEveryEntryOnce(t *testing.T) {
	pairs(t, func(t *testing.T, a, b *Ledger) {
		const runs, calls = 10, 40
		ls := []*Ledger{a, b}
		var wg sync.WaitGroup
		for i := range runs {
			wg.Go(func() {
				if _, err := ls[i%2].Append(Entry{Kind: Admitted, Tenant: "acme", Project: "Launch", Agent: "lead", RunID: fmt.Sprintf("r%d", i), At: t0}); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		var counted atomic.Int64
		for i := range calls {
			for _, l := range ls {
				wg.Go(func() {
					ok, err := l.Append(Entry{Kind: Spent, Tenant: "acme", Project: "Launch", Agent: "lead", RunID: fmt.Sprintf("r%d", i%runs),
						Key: fmt.Sprintf("call-%d", i), Spend: &Spend{Micros: 100, ModelCalls: 1, Tokens: 10}, At: t0.Add(time.Duration(i) * time.Second)})
					if err != nil {
						t.Error(err)
					}
					if ok {
						counted.Add(1)
					}
				})
			}
		}
		for i := range runs / 2 {
			wg.Go(func() {
				if _, err := ls[(i+1)%2].Append(Entry{Kind: Ended, Tenant: "acme", RunID: fmt.Sprintf("r%d", i), At: t0.Add(time.Hour)}); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		if counted.Load() != calls {
			t.Fatalf("%d calls counted, want %d", counted.Load(), calls)
		}
		want := policy.Usage{Runs: runs, Concurrent: runs / 2, Spend: 100 * calls, Tokens: 10 * calls, ModelCalls: calls}
		for i, l := range ls {
			if u := l.used("acme", launch, t0); !reflect.DeepEqual(u, want) {
				t.Fatalf("process %d reads %+v\nwant %+v", i, u, want)
			}
		}
		entries := a.all("acme")
		kinds := map[Kind]int{}
		keys := map[string]int{}
		for _, e := range entries {
			kinds[e.Kind]++
			if e.Key != "" {
				keys[e.Key]++
			}
		}
		if kinds[Admitted] != runs || kinds[Spent] != calls || kinds[Ended] != runs/2 || len(entries) != runs+calls+runs/2 {
			t.Fatalf("entries kept: %v of %d", kinds, len(entries))
		}
		for k, n := range keys {
			if n != 1 {
				t.Fatalf("call %s kept %d times", k, n)
			}
		}
		if u := Rebuild(entries).used("acme", launch, t0); !reflect.DeepEqual(u, want) {
			t.Fatalf("rebuilt from the entries: %+v", u)
		}
	})
}

// A grant approved on one replica is counted by every replica's judge: the
// other replica reads it from the shared ledger, and the run it was granted
// to is let go on past the limit there too.
func TestAGrantOnOneReplicaIsCountedByTheOthersJudge(t *testing.T) {
	ls, _ := processes(t, 2)
	pol := policy.Policy{Scope: launch, Period: policy.Day, SpendLimit: 1000, Action: policy.Approve, MaxGrant: 500}
	admit(ls[0], "acme", "Launch", "lead", "r1", t0)
	call(ls[0], "acme", "r1", "r1/1", t0, Spend{Micros: 1200, ModelCalls: 1})
	judge := func(l *Ledger) policy.Decision {
		g, err := l.Granted("acme", "r1", launch, policy.PeriodStart(pol.Period, t0))
		if err != nil {
			t.Fatal(err)
		}
		return policy.Exceeded([]policy.Applied{{Policy: pol, Usage: l.used("acme", launch, t0)}}, map[policy.Scope]policy.Grant{launch: g})
	}
	if d := judge(ls[1]); d.Verdict != policy.NeedsApproval {
		t.Fatalf("before the grant: %+v", d)
	}
	if _, err := ls[0].Append(Entry{Kind: Granted, Tenant: "acme", Project: "Launch", Agent: "lead", RunID: "r1", At: t0.Add(time.Minute),
		Scope: &launch, Limit: policy.LimitSpend, Grant: &policy.Grant{Spend: 500}, By: "ana"}); err != nil {
		t.Fatal(err)
	}
	for i, l := range ls {
		if d := judge(l); d.Verdict == policy.NeedsApproval || d.Verdict == policy.Deny {
			t.Fatalf("replica %d judges the granted run as %+v", i, d)
		}
		if n := l.grants("acme", "r1", launch); n != 1 {
			t.Fatalf("replica %d counts %d grants", i, n)
		}
	}
}

// The observer's entries for one run's events, folded twice by two
// processes at once (as after a lease moves), are kept once: a call with an
// AccountingID, one keyed by where it was published (run#seq), the run's
// settled total and its end.
func TestEventsFoldedTwiceByTwoProcessesAreKeptOnce(t *testing.T) {
	pairs(t, func(t *testing.T, a, b *Ledger) {
		admit(a, "acme", "Launch", "lead", "r1", t0)
		fold := []Entry{
			{Kind: Spent, Tenant: "acme", Project: "Launch", Agent: "lead", RunID: "r1", Key: "acct-1", Spend: &Spend{Micros: 100, ModelCalls: 1}, At: t0},
			{Kind: Spent, Tenant: "acme", Project: "Launch", Agent: "lead", RunID: "r1", Key: "r1#7", Spend: &Spend{Micros: 200, ModelCalls: 1}, At: t0},
			{Kind: Settled, Tenant: "acme", Project: "Launch", Agent: "lead", RunID: "r1", Key: "run:r1", Spend: &Spend{Micros: 450, ModelCalls: 3}, At: t0.Add(time.Minute)},
			{Kind: Ended, Tenant: "acme", RunID: "r1", At: t0.Add(time.Minute)},
		}
		var wg sync.WaitGroup
		kept := make([]atomic.Int64, len(fold))
		for range 2 {
			for _, l := range []*Ledger{a, b} {
				wg.Go(func() {
					for i, e := range fold {
						ok, err := l.Append(e)
						if err != nil {
							t.Error(err)
						}
						if ok {
							kept[i].Add(1)
						}
					}
				})
			}
		}
		wg.Wait()
		for i := range fold {
			if n := kept[i].Load(); n != 1 {
				t.Fatalf("%s %s kept %d times", fold[i].Kind, fold[i].Key, n)
			}
		}
		want := policy.Usage{Runs: 1, Spend: 450, ModelCalls: 3}
		for i, l := range []*Ledger{a, b} {
			if u := l.used("acme", launch, t0); !reflect.DeepEqual(u, want) {
				t.Fatalf("process %d reads %+v, want %+v", i, u, want)
			}
		}
		if e := b.all("acme"); len(e) != 1+len(fold) {
			t.Fatalf("%d entries kept, want %d", len(e), 1+len(fold))
		}
	})
}
