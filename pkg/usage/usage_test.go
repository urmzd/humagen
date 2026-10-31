package usage

import (
	"fmt"
	"math/rand/v2"
	"reflect"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/policy"
)

var t0 = time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)

var (
	all    = policy.Scope{Kind: policy.Tenant}
	launch = policy.Scope{Kind: policy.Project, Name: "Launch"}
	lead   = policy.Scope{Kind: policy.Agent, Name: "lead"}
)

func admit(l *Ledger, tenant, project, agent, id string, at time.Time) {
	_, _ = l.Append(Entry{Kind: Admitted, Tenant: tenant, Project: project, Agent: agent, RunID: id, At: at})
}

func TestUsageByScopeAndPeriod(t *testing.T) {
	l := NewLedger()
	admit(l, "acme", "Launch", "lead", "r1", t0.Add(-time.Hour)) // yesterday
	admit(l, "acme", "Launch", "lead", "r2", t0)                 // the first instant of today
	admit(l, "acme", "", "helper", "r3", t0.Add(time.Hour))
	admit(l, "globex", "Launch", "lead", "r9", t0.Add(time.Hour))
	if _, err := l.Append(Entry{Kind: Ended, Tenant: "acme", RunID: "r1", At: t0.Add(2 * time.Hour)}); err != nil {
		t.Fatal(err)
	}

	for name, tc := range map[string]struct {
		scope      policy.Scope
		since      time.Time
		runs, live int
	}{
		"tenant, today":      {all, t0, 2, 2},
		"tenant, this month": {all, policy.PeriodStart(policy.Month, t0), 3, 2},
		"project, today":     {launch, t0, 1, 1},
		"agent, this month":  {lead, policy.PeriodStart(policy.Month, t0), 2, 1},
	} {
		got := l.Usage("acme", tc.scope, tc.since)
		if got.Runs != tc.runs || got.Concurrent != tc.live {
			t.Errorf("%s: %d runs, %d in flight, want %d and %d", name, got.Runs, got.Concurrent, tc.runs, tc.live)
		}
	}
	if got := l.Usage("initech", all, t0); !reflect.DeepEqual(got, policy.Usage{}) {
		t.Errorf("a tenant with no runs used %+v", got)
	}
}

// The counts are a projection of the entries: replaying the entries into an
// empty ledger must answer every question the same way.
func TestRebuildMatchesTheLiveLedger(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	l := NewLedger()
	projects, agents := []string{"", "Launch", "Billing"}, []string{"lead", "helper", "oncall"}
	var open []string
	at := t0
	for i := range 2000 {
		at = at.Add(time.Duration(rng.IntN(600)) * time.Second)
		switch {
		case len(open) > 0 && rng.IntN(3) == 0:
			j := rng.IntN(len(open))
			if _, err := l.Append(Entry{Kind: Ended, Tenant: "acme", RunID: open[j], At: at}); err != nil {
				t.Fatal(err)
			}
			open = append(open[:j], open[j+1:]...)
		case rng.IntN(20) == 0:
			if _, err := l.Append(Entry{Kind: Refused, Tenant: "acme", Agent: "lead", At: at, Scope: &launch, Version: 1, Limit: policy.LimitRuns}); err != nil {
				t.Fatal(err)
			}
		case len(open) > 0 && rng.IntN(3) == 0:
			// A call, sometimes delivered twice, sometimes unpriced or in
			// another currency, and sometimes a run's settled total.
			run := open[rng.IntN(len(open))]
			sp := &Spend{Micros: int64(rng.IntN(5000)), ModelCalls: 1, Tokens: int64(rng.IntN(900))}
			switch rng.IntN(5) {
			case 0:
				sp.Micros, sp.Unpriced = 0, 1
			case 1:
				sp.Micros, sp.Other = 0, []policy.Cost{{Micros: 70, Currency: "EUR"}}
			}
			kind, key := Spent, fmt.Sprintf("%s/a1/%d", run, rng.IntN(4))
			if rng.IntN(6) == 0 {
				kind, key = Settled, "run:"+run
				sp.Micros += 10_000
			}
			if _, err := l.Append(Entry{Kind: kind, Tenant: "acme", Project: projects[i%3], Agent: agents[i%3], RunID: run, Key: key, Spend: sp, At: at}); err != nil {
				t.Fatal(err)
			}
		default:
			id := fmt.Sprintf("r%d", i)
			admit(l, "acme", projects[rng.IntN(3)], agents[rng.IntN(3)], id, at)
			open = append(open, id)
		}
	}

	rebuilt := Rebuild(l.Entries())
	scopes := []policy.Scope{all, launch, {Kind: policy.Project, Name: "Billing"}, lead, {Kind: policy.Agent, Name: "oncall"}}
	for _, scope := range scopes {
		for _, since := range []time.Time{t0, policy.PeriodStart(policy.Day, at), policy.PeriodStart(policy.Month, at), at} {
			live, replayed := l.Usage("acme", scope, since), rebuilt.Usage("acme", scope, since)
			if !reflect.DeepEqual(live, replayed) {
				t.Fatalf("%v since %s: live %+v, rebuilt %+v", scope, since.Format(time.RFC3339), live, replayed)
			}
		}
	}
	if len(rebuilt.Notices("acme", 0)) != len(l.Notices("acme", 0)) {
		t.Fatal("the rebuilt ledger lost warnings or refusals")
	}
}

func TestOldAdmissionsAreDropped(t *testing.T) {
	l := NewLedger()
	admit(l, "acme", "", "lead", "old", t0)
	admit(l, "acme", "", "lead", "new", t0.Add(keepAdmissions+25*time.Hour)) // days are kept whole
	if got := l.Usage("acme", all, time.Time{}); got.Runs != 1 {
		t.Fatalf("%d runs kept, want only the recent one", got.Runs)
	}
	if len(l.Entries()) != 1 {
		t.Fatalf("%d entries kept", len(l.Entries()))
	}
}

// A client's usage is the runs it raised: in flight and in the period, and
// nobody else's.
func TestUsageByClient(t *testing.T) {
	l := NewLedger()
	raise := func(client, id string) {
		if _, err := l.Append(Entry{Kind: Admitted, Tenant: "acme", Agent: "lead", Client: client, RunID: id, At: t0}); err != nil {
			t.Fatal(err)
		}
	}
	raise("docs-app", "r1")
	raise("docs-app", "r2")
	raise("ops-app", "r3")
	raise("", "r4") // from the console
	if _, err := l.Append(Entry{Kind: Admitted, Tenant: "globex", Agent: "lead", Client: "docs-app", RunID: "r9", At: t0}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Append(Entry{Kind: Ended, Tenant: "acme", RunID: "r1", At: t0.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}

	docs := policy.Scope{Kind: policy.Client, Name: "docs-app"}
	if got := l.Usage("acme", docs, t0); got.Runs != 2 || got.Concurrent != 1 {
		t.Fatalf("docs-app: %+v, want 2 runs and 1 in flight", got)
	}
	if got := l.Usage("acme", policy.Scope{Kind: policy.Client, Name: "nobody"}, t0); got.Runs != 0 || got.Concurrent != 0 {
		t.Fatalf("a client that raised nothing: %+v", got)
	}
	if got := l.Usage("acme", all, t0); got.Runs != 4 {
		t.Fatalf("the tenant: %+v", got)
	}
	// Rebuilding from the entries gives the same answer.
	if got := Rebuild(l.Entries()).Usage("acme", docs, t0); got.Runs != 2 || got.Concurrent != 1 {
		t.Fatalf("rebuilt docs-app: %+v", got)
	}
}

func call(l *Ledger, tenant, run, key string, at time.Time, sp Spend) bool {
	ok, _ := l.Append(Entry{Kind: Spent, Tenant: tenant, Project: "Launch", Agent: "lead", RunID: run, Key: key, Spend: &sp, At: at})
	return ok
}

// A model call is counted once however often it is delivered, and a run's
// settled total adds only what its calls did not: a redelivered terminal
// event, or a reconciler pass after it, adds nothing.
func TestARunsSpendIsCountedOnceDespiteRedeliveryAndSettlement(t *testing.T) {
	l := NewLedger()
	one := Spend{Micros: 300, ModelCalls: 1, Tokens: 1000}
	if !call(l, "acme", "r1", "r1/a1/1", t0, one) || call(l, "acme", "r1", "r1/a1/1", t0, one) {
		t.Fatal("the first delivery of a call counts and the second does not")
	}
	call(l, "acme", "r1", "r1/a1/2", t0, one)
	// The run ended having made three calls: one of them was never seen.
	settle := func() (Spend, bool) {
		added, ok, _ := l.Record(Entry{Kind: Settled, Tenant: "acme", Project: "Launch", Agent: "lead", RunID: "r1", Key: "run:r1",
			Spend: &Spend{Micros: 900, ModelCalls: 3, Tokens: 3000}, At: t0.Add(time.Minute)})
		return added, ok
	}
	added, ok := settle()
	if !ok || added.Micros != 300 || added.ModelCalls != 1 || added.Tokens != 1000 {
		t.Fatalf("settling added %+v (%v), want the one call that was missed", added, ok)
	}
	if _, again := settle(); again {
		t.Fatal("a second settlement of the same run was counted")
	}
	got := l.Usage("acme", launch, t0)
	if got.Spend != 900 || got.ModelCalls != 3 || got.Tokens != 3000 {
		t.Fatalf("usage = %+v, want the run's three calls once", got)
	}
	if run := l.RunSpend("acme", "r1"); run.Micros != 900 {
		t.Fatalf("the run's own spend is %+v", run)
	}
	// Another tenant reusing the key is its own call, and is not seen here.
	if !call(l, "globex", "r1", "r1/a1/1", t0, one) {
		t.Fatal("another tenant's call with the same key was refused")
	}
	if got := l.Usage("acme", all, t0); got.Spend != 900 {
		t.Fatalf("another tenant's spend leaked: %+v", got)
	}
	if !reflect.DeepEqual(Rebuild(l.Entries()).Usage("acme", all, t0), l.Usage("acme", all, t0)) {
		t.Fatal("the rebuilt ledger counts spend differently")
	}
}

// A call with no price, or priced in another currency, is never counted as
// dollars: it is reported apart, and its tokens still count.
func TestUnpricedAndOtherCurrencyCallsAreNotDollars(t *testing.T) {
	l := NewLedger()
	call(l, "acme", "r1", "a", t0, Spend{Micros: 500, ModelCalls: 1, Tokens: 10})
	call(l, "acme", "r1", "b", t0, Spend{ModelCalls: 1, Tokens: 20, Unpriced: 1})
	call(l, "acme", "r1", "c", t0, Spend{ModelCalls: 1, Tokens: 30, Other: []policy.Cost{{Micros: 800, Currency: "EUR"}}})
	call(l, "acme", "r1", "d", t0, Spend{ModelCalls: 1, Tokens: 40, Other: []policy.Cost{{Micros: 200, Currency: "EUR"}}})
	got := l.Usage("acme", all, t0)
	if got.Spend != 500 || got.Tokens != 100 || got.ModelCalls != 4 || got.UnpricedCalls != 1 || !got.Unmetered() ||
		!reflect.DeepEqual(got.OtherCosts, []policy.Cost{{Micros: 1000, Currency: "EUR"}}) {
		t.Fatalf("usage = %+v", got)
	}
}

// A call whose event arrives after its run settled is already in the settled
// total, so it adds nothing; one that goes beyond the total adds only the
// excess. The same holds in the other order.
func TestALateCallAfterSettlementIsNotCountedTwice(t *testing.T) {
	settle := func(l *Ledger, sp Spend) Spend {
		added, _, _ := l.Record(Entry{Kind: Settled, Tenant: "acme", Agent: "lead", RunID: "r1", Key: "run:r1", Spend: &sp, At: t0})
		return added
	}
	one := Spend{Micros: 4500, ModelCalls: 1, Tokens: 1100}

	settledFirst := NewLedger()
	settle(settledFirst, one)
	call(settledFirst, "acme", "r1", "r1/a1/1", t0, one)
	if got := settledFirst.Usage("acme", all, t0); got.Spend != 4500 || got.ModelCalls != 1 {
		t.Fatalf("settled then called: %+v, want the one call", got)
	}
	// A second late call is beyond the settled total, and counts.
	call(settledFirst, "acme", "r1", "r1/a1/2", t0, one)
	if got := settledFirst.Usage("acme", all, t0); got.Spend != 9000 || got.ModelCalls != 2 {
		t.Fatalf("a call beyond the settled total: %+v", got)
	}

	calledFirst := NewLedger()
	call(calledFirst, "acme", "r1", "r1/a1/1", t0, one)
	if added := settle(calledFirst, one); !added.IsZero() {
		t.Fatalf("settling a run whose call was counted added %+v", added)
	}
	if got := calledFirst.Usage("acme", all, t0); got.Spend != 4500 || got.ModelCalls != 1 {
		t.Fatalf("called then settled: %+v", got)
	}
	for _, l := range []*Ledger{settledFirst, calledFirst} {
		if !reflect.DeepEqual(Rebuild(l.Entries()).Usage("acme", all, t0), l.Usage("acme", all, t0)) {
			t.Fatal("the rebuilt ledger disagrees")
		}
	}
}

// Where an entry counts is its own time, not the order it was appended in: an
// entry stamped before the one before it, or before the period, hides nothing
// that is in the period.
func TestEntriesOutOfOrderAndAcrossAPeriodBoundaryAreCountedInTheirOwnPeriod(t *testing.T) {
	l := NewLedger()
	today := t0.Add(10 * time.Hour)
	call(l, "acme", "r1", "a", today, Spend{Micros: 9000, ModelCalls: 1})
	call(l, "acme", "r2", "b", t0.Add(-time.Second), Spend{Micros: 100, ModelCalls: 1}) // yesterday, appended after
	call(l, "acme", "r3", "c", t0.Add(time.Hour), Spend{Micros: 1, ModelCalls: 1})      // earlier today, appended after
	admit(l, "acme", "", "lead", "r4", t0.Add(2*time.Hour))
	admit(l, "acme", "", "lead", "r5", t0.Add(-2*time.Hour))
	got := l.Usage("acme", all, policy.PeriodStart(policy.Day, today))
	if got.Spend != 9001 || got.Runs != 1 {
		t.Fatalf("today = %+v, want 9001 and one run", got)
	}
	month := l.Usage("acme", all, policy.PeriodStart(policy.Month, today))
	if month.Spend != 9101 || month.Runs != 2 {
		t.Fatalf("this month = %+v", month)
	}
	// A period that starts inside a day reads that day's entries.
	if mid := l.Usage("acme", all, t0.Add(5*time.Hour)); mid.Spend != 9000 || mid.Runs != 0 {
		t.Fatalf("since mid-morning = %+v", mid)
	}
	if !reflect.DeepEqual(Rebuild(l.Entries()).Usage("acme", all, t0), got) {
		t.Fatal("the rebuilt ledger disagrees")
	}
}

// A run in a child project counts toward each ancestor it was admitted
// within, whether a period is read from whole days' totals or from the
// entries of the day it starts inside. An entry with no Within, written
// before projects nested, counts toward its own project alone.
func TestWithinCountsFromDayTotalsAndFromInsideADay(t *testing.T) {
	l := NewLedger()
	field := policy.Scope{Kind: policy.Project, Name: "Field"}
	globex := policy.Scope{Kind: policy.Project, Name: "Globex"}
	at := func(e Entry) {
		t.Helper()
		if _, err := l.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	// Yesterday, in Globex under Field; today, in Initech under Field and in
	// Renewals under Globex under Field; and one from before nesting.
	at(Entry{Kind: Admitted, Tenant: "acme", Project: "Globex", Within: []string{"Field"}, Agent: "lead", RunID: "r1", At: t0.Add(-2 * time.Hour)})
	at(Entry{Kind: Admitted, Tenant: "acme", Project: "Initech", Within: []string{"Field"}, Agent: "lead", RunID: "r2", At: t0.Add(time.Hour)})
	at(Entry{Kind: Admitted, Tenant: "acme", Project: "Renewals", Within: []string{"Globex", "Field"}, Agent: "lead", RunID: "r3", At: t0.Add(3 * time.Hour)})
	at(Entry{Kind: Admitted, Tenant: "acme", Project: "Globex", Agent: "lead", RunID: "old", At: t0.Add(4 * time.Hour)})
	at(Entry{Kind: Spent, Tenant: "acme", Project: "Initech", Within: []string{"Field"}, Agent: "lead", RunID: "r2", Key: "r2/1", Spend: &Spend{Micros: 700, ModelCalls: 1, Tokens: 10}, At: t0.Add(2 * time.Hour)})
	at(Entry{Kind: Spent, Tenant: "acme", Project: "Renewals", Within: []string{"Globex", "Field"}, Agent: "lead", RunID: "r3", Key: "r3/1", Spend: &Spend{Micros: 300, ModelCalls: 1, Tokens: 5}, At: t0.Add(3 * time.Hour)})
	at(Entry{Kind: Ended, Tenant: "acme", RunID: "r2", At: t0.Add(5 * time.Hour)})

	for name, c := range map[string]struct {
		scope      policy.Scope
		since      time.Time
		runs, live int
		spend      int64
		tokens     int64
	}{
		// Since midnight: today's totals.
		"Field, from today's totals":  {scope: field, since: t0, runs: 2, live: 2, spend: 1000, tokens: 15},
		"Globex, from today's totals": {scope: globex, since: t0, runs: 2, live: 3, spend: 300, tokens: 5},
		// Since two in the morning: inside the day, entry by entry.
		"Field, from inside the day":  {scope: field, since: t0.Add(2 * time.Hour), runs: 1, live: 2, spend: 1000, tokens: 15},
		"Globex, from inside the day": {scope: globex, since: t0.Add(2 * time.Hour), runs: 2, live: 3, spend: 300, tokens: 5},
		// Since yesterday: a day inside, then a whole day.
		"Field, across two days": {scope: field, since: t0.Add(-3 * time.Hour), runs: 3, live: 2, spend: 1000, tokens: 15},
	} {
		got := l.Usage("acme", c.scope, c.since)
		if got.Runs != c.runs || got.Concurrent != c.live || got.Spend != c.spend || got.Tokens != c.tokens {
			t.Errorf("%s: %+v, want %d runs, %d in flight, %d spent, %d tokens", name, got, c.runs, c.live, c.spend, c.tokens)
		}
		if replayed := Rebuild(l.Entries()).Usage("acme", c.scope, c.since); !reflect.DeepEqual(replayed, got) {
			t.Errorf("%s: rebuilt %+v, live %+v", name, replayed, got)
		}
	}
	// The tenant counts each run once, however many projects it counts in.
	if got := l.Usage("acme", all, t0); got.Runs != 3 || got.Spend != 1000 {
		t.Fatalf("tenant = %+v", got)
	}
	// A project a run is not within is not charged for it.
	if got := l.Usage("acme", policy.Scope{Kind: policy.Project, Name: "Initech"}, t0); got.Runs != 1 || got.Spend != 700 {
		t.Fatalf("Initech = %+v", got)
	}
}

// Rebuild replays Within as the live ledger counted it, entries from before
// nesting included, for every project of the tree.
func TestRebuildAgreesAlongTheTree(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	l := NewLedger()
	places := []struct {
		project string
		within  []string
	}{{"", nil}, {"Field", nil}, {"Globex", []string{"Field"}}, {"Renewals", []string{"Globex", "Field"}}, {"Globex", nil}}
	at := t0
	var open []int
	for i := range 1500 {
		at = at.Add(time.Duration(rng.IntN(900)) * time.Second)
		if len(open) > 0 && rng.IntN(2) == 0 {
			j := open[rng.IntN(len(open))]
			pl := places[j%len(places)]
			id := fmt.Sprintf("r%d", j)
			kind, key := Spent, fmt.Sprintf("%s/%d", id, i)
			if rng.IntN(5) == 0 {
				kind, key = Settled, "run:"+id
			}
			if _, err := l.Append(Entry{Kind: kind, Tenant: "acme", Project: pl.project, Within: pl.within, Agent: "lead", RunID: id, Key: key,
				Spend: &Spend{Micros: int64(rng.IntN(4000)), ModelCalls: 1, Tokens: int64(rng.IntN(500))}, At: at}); err != nil {
				t.Fatal(err)
			}
			continue
		}
		pl := places[i%len(places)]
		if _, err := l.Append(Entry{Kind: Admitted, Tenant: "acme", Project: pl.project, Within: pl.within, Agent: "lead", RunID: fmt.Sprintf("r%d", i), At: at}); err != nil {
			t.Fatal(err)
		}
		open = append(open, i)
	}
	rebuilt := Rebuild(l.Entries())
	for _, name := range []string{"Field", "Globex", "Renewals"} {
		scope := policy.Scope{Kind: policy.Project, Name: name}
		for _, since := range []time.Time{t0, policy.PeriodStart(policy.Day, at), at.Add(-90 * time.Minute), policy.PeriodStart(policy.Month, at)} {
			if live, replayed := l.Usage("acme", scope, since), rebuilt.Usage("acme", scope, since); !reflect.DeepEqual(live, replayed) {
				t.Fatalf("%s since %s: live %+v, rebuilt %+v", name, since, live, replayed)
			}
		}
	}
}

// A grant counts for its own run, in its own scope, within the period asked
// about, and is kept: a ledger rebuilt from the entries reads the same grants,
// and the grant is among the notices with who made it.
func TestGrantsCountForTheirRunAndScopeAndSurviveARebuild(t *testing.T) {
	l := NewLedger()
	admit(l, "acme", "Launch", "lead", "r1", t0)
	grantOf := func(run string, scope policy.Scope, g policy.Grant, at time.Time) {
		t.Helper()
		if _, err := l.Append(Entry{Kind: Granted, Tenant: "acme", Project: "Launch", Agent: "lead", RunID: run,
			At: at, Scope: &scope, Version: 2, Limit: policy.LimitSpend, Grant: &g, By: "ana"}); err != nil {
			t.Fatal(err)
		}
	}
	grantOf("r1", launch, policy.Grant{Spend: 5}, t0.Add(-time.Hour)) // yesterday
	grantOf("r1", launch, policy.Grant{Spend: 7}, t0.Add(time.Hour))
	grantOf("r1", launch, policy.Grant{Tokens: 100}, t0.Add(2*time.Hour))
	grantOf("r1", all, policy.Grant{Spend: 11}, t0.Add(time.Hour))
	grantOf("r2", launch, policy.Grant{Spend: 13}, t0.Add(time.Hour))

	if got := l.Granted("acme", "r1", launch, t0); got != (policy.Grant{Spend: 7, Tokens: 100}) {
		t.Errorf("r1 in Launch today = %+v, want 7 and 100 tokens", got)
	}
	if got := l.Granted("acme", "r1", launch, t0.Add(-24*time.Hour)); got.Spend != 12 {
		t.Errorf("r1 in Launch since yesterday = %+v, want 12", got)
	}
	if got := l.Granted("globex", "r1", launch, t0); got != (policy.Grant{}) {
		t.Errorf("another tenant reads %+v", got)
	}
	if n := l.Grants("acme", "r1", launch); n != 3 {
		t.Errorf("r1 was granted %d times in Launch, want 3", n)
	}
	if got := Rebuild(l.Entries()).Granted("acme", "r1", launch, t0); got != l.Granted("acme", "r1", launch, t0) {
		t.Errorf("rebuilt = %+v, live = %+v", got, l.Granted("acme", "r1", launch, t0))
	}
	if n := l.Notices("acme", 1); len(n) != 1 || n[0].Kind != Granted || n[0].By != "ana" || n[0].Grant == nil {
		t.Errorf("newest notice = %+v, want the grant with who made it", n)
	}
}
