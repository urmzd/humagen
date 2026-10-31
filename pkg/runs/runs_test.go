package runs

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/wire"

	"github.com/urmzd/humagen/fiber/pkg/event"
	"github.com/urmzd/humagen/fiber/pkg/runtime"

	"github.com/urmzd/humagen/pkg/call"
)

var t0 = time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)

func record(t *testing.T, x *Index, tenant, id, agent, origin string, at time.Duration, meta map[string]string) {
	t.Helper()
	tag := wire.NewEvent(wire.Raise{ID: id, Tenant: tenant, Agent: agent, Origin: origin, Text: "go", Context: meta, At: t0.Add(at)})
	if err := x.Record(id, tag, 1); err != nil {
		t.Fatal(err)
	}
}

func end(t *testing.T, x *Index, tenant, id string, at time.Duration, kind string, body wire.Lifecycle) (Run, bool) {
	t.Helper()
	b, err := wire.LifecycleBody(kind, body)
	if err != nil {
		t.Fatal(err)
	}
	return x.Observe(event.Envelope{Run: id, Tenant: tenant, Type: kind, Data: b, Time: t0.Add(at)})
}

func TestObserveReportsAnEndingOnce(t *testing.T) {
	x := NewIndex()
	record(t, x, "acme", "r1", "lead", "api", 0, nil)
	if _, ended := end(t, x, "acme", "r1", time.Second, event.TypeFailed, wire.Lifecycle{Error: "timeout"}); ended {
		t.Fatal("a failure that will be retried ended the run")
	}
	r, ended := end(t, x, "acme", "r1", 2*time.Second, event.TypeFailed, wire.Lifecycle{Error: "timeout", Terminal: true})
	if !ended || r.Status != Failed || r.Error != "timeout" {
		t.Fatalf("got %+v, %v", r, ended)
	}
	if _, again := end(t, x, "acme", "r1", 3*time.Second, event.TypeCompleted, wire.Lifecycle{}); again {
		t.Fatal("an ended run ended twice")
	}
	if _, ok := end(t, x, "globex", "r1", time.Second, event.TypeCompleted, wire.Lifecycle{}); ok {
		t.Fatal("another tenant's event moved the run")
	}
}

func TestOnlyTrustedOriginsNameASchedule(t *testing.T) {
	x := NewIndex()
	meta := map[string]string{"cron_schedule": "nightly"}
	record(t, x, "acme", "a", "lead", "cron", 0, meta)
	record(t, x, "acme", "b", "lead", OriginManual, 0, meta)
	record(t, x, "acme", "c", "lead", "webhook", 0, meta)
	got := x.ForSchedule("acme", "nightly", 0, nil)
	if len(got) != 2 {
		t.Fatalf("got %d runs, want the cron and manual ones only", len(got))
	}
	if len(x.ForSchedule("globex", "nightly", 0, nil)) != 0 {
		t.Fatal("another tenant saw the schedule's runs")
	}
}

// A schedule's runs are found by its name, wherever each was filed, so a
// reader is given only those they keep, and the ones left out do not use up
// the limit.
func TestAScheduleKeepsOnlyTheRunsAReaderKeeps(t *testing.T) {
	x := NewIndex()
	record(t, x, "acme", "a", "lead", "cron", 0, map[string]string{"cron_schedule": "weekly", MetaProject: "Open"})
	record(t, x, "acme", "b", "lead", "cron", time.Second, map[string]string{"cron_schedule": "weekly", MetaProject: "Hidden"})
	record(t, x, "acme", "c", "lead", "cron", 2*time.Second, map[string]string{"cron_schedule": "weekly", MetaProject: "Hidden"})
	open := func(r Run) bool { return r.Project != "Hidden" }
	got := x.ForSchedule("acme", "weekly", 1, open)
	if len(got) != 1 || got[0].Project != "Open" {
		t.Fatalf("the reader was given %+v", got)
	}
	if all := x.ForSchedule("acme", "weekly", 0, nil); len(all) != 3 {
		t.Fatalf("with no filter, %d runs", len(all))
	}
}

func TestSummarize(t *testing.T) {
	x := NewIndex()
	// Three lead runs in minute 0 (1s, 2s, 9s; the last one fails), one helper
	// run in minute 2 still going, and one run in another tenant.
	for i, took := range []time.Duration{time.Second, 2 * time.Second, 9 * time.Second} {
		id := string(rune('a' + i))
		record(t, x, "acme", id, "lead", "api", time.Duration(i)*time.Second, nil)
		kind, body := event.TypeCompleted, wire.Lifecycle{}
		if i == 2 {
			kind, body = event.TypeFailed, wire.Lifecycle{Error: "x", Terminal: true}
		}
		end(t, x, "acme", id, time.Duration(i)*time.Second+took, kind, body)
	}
	record(t, x, "acme", "d", "helper", "cron", 2*time.Minute, nil)
	record(t, x, "globex", "z", "lead", "api", 0, nil)

	s := x.Summarize("acme", t0, t0.Add(3*time.Minute), time.Minute)
	if s.Total.Runs != 4 || s.Total.Completed != 2 || s.Total.Failed != 1 || s.Total.Active != 1 {
		t.Fatalf("total = %+v", s.Total)
	}
	if s.Total.P50 != 2000 || s.Total.P95 != 9000 {
		t.Fatalf("p50 = %d, p95 = %d", s.Total.P50, s.Total.P95)
	}
	if len(s.Buckets) != 3 || s.Buckets[0].Completed != 2 || s.Buckets[0].Failed != 1 || s.Buckets[2].Active != 1 {
		t.Fatalf("buckets = %+v", s.Buckets)
	}
	if len(s.ByAgent) != 2 || s.ByAgent[0].Name != "lead" || s.ByAgent[0].Runs != 3 {
		t.Fatalf("by agent = %+v", s.ByAgent)
	}
	if len(s.Slowest) != 3 || s.Slowest[0].ID != "c" {
		t.Fatalf("slowest = %+v", s.Slowest)
	}
}

func TestSummarizeBoundsItsBuckets(t *testing.T) {
	s := NewIndex().Summarize("acme", t0, t0.Add(30*24*time.Hour), time.Second)
	if len(s.Buckets) > MaxBuckets+1 {
		t.Fatalf("%d buckets", len(s.Buckets))
	}
	if s.Total.P50 != 0 {
		t.Fatal("percentile of nothing should be 0")
	}
}

func TestRecent(t *testing.T) {
	x := NewIndex()
	record(t, x, "acme", "a", "lead", "api", 0, nil)
	record(t, x, "acme", "b", "helper", "api", time.Minute, map[string]string{MetaProject: "Launch"})
	end(t, x, "acme", "b", 2*time.Minute, event.TypeFailed, wire.Lifecycle{Terminal: true})
	if w := x.Recent("acme", "", "", t0); w.Total != 2 || w.Failed != 1 {
		t.Fatalf("all = %+v", w)
	}
	if w := x.Recent("acme", "lead", "", t0); w.Total != 1 || w.Failed != 0 {
		t.Fatalf("lead = %+v", w)
	}
	if w := x.Recent("acme", "", "", t0.Add(30*time.Second)); w.Total != 1 {
		t.Fatalf("since = %+v", w)
	}
	if w := x.Recent("acme", "", "Launch", t0); w.Total != 1 || w.Failed != 1 {
		t.Fatalf("project = %+v", w)
	}
	if w := x.Recent("acme", "lead", "Launch", t0); w.Total != 0 {
		t.Fatalf("agent and project = %+v", w)
	}
	if w := x.Recent("globex", "", "", t0); w.Total != 0 {
		t.Fatalf("another tenant = %+v", w)
	}
}

func TestRecentSpendCountsEachRunsOwnCalls(t *testing.T) {
	x := NewIndex()
	record(t, x, "acme", "a", "lead", "api", 0, nil)
	record(t, x, "acme", "b", "lead", "api", time.Second, nil)
	own := func(micros int64, currency string) *event.RunUsage {
		return &event.RunUsage{
			Own:   event.Usage{ModelCalls: 1, Cost: &event.Cost{Micros: micros, Currency: currency}},
			Total: event.Usage{ModelCalls: 2, Cost: &event.Cost{Micros: 10 * micros, Currency: currency}},
		}
	}
	x.SetUsage("acme", "a", own(1500, "USD"), 0)
	x.SetUsage("acme", "b", own(900, "EUR"), 0)
	if w := x.Recent("acme", "", "", t0); w.Spent != 1500 {
		t.Fatalf("spent = %d, want only a's own dollars", w.Spent)
	}
}

func TestThreadPaging(t *testing.T) {
	x := NewIndex()
	// Seven conversations, each a minute after the last. Odd ones are
	// answered by helper and started by cron, and t3 has a failure.
	for i := range 7 {
		id := "t" + string(rune('0'+i))
		agent, origin := "lead", "api"
		if i%2 == 1 {
			agent, origin = "helper", "cron"
		}
		record(t, x, "acme", id, agent, origin, time.Duration(i)*time.Minute, nil)
		kind, body := event.TypeCompleted, wire.Lifecycle{}
		if i == 3 {
			kind, body = event.TypeFailed, wire.Lifecycle{Terminal: true}
		}
		if i != 6 {
			end(t, x, "acme", id, time.Duration(i)*time.Minute+time.Second, kind, body)
		}
	}
	record(t, x, "globex", "other", "lead", "api", 0, nil)
	ids := func(page []ThreadSummary) string {
		out := ""
		for _, s := range page {
			out += s.ID + " "
		}
		return out
	}

	// Pages walk from newest to oldest with no gap and no repeat.
	var seen string
	cursor := ""
	for range 5 {
		page, next, err := x.ThreadPage("acme", ThreadQuery{Limit: 3, Before: cursor})
		if err != nil {
			t.Fatal(err)
		}
		seen += ids(page)
		if cursor = next; cursor == "" {
			break
		}
	}
	if seen != "t6 t5 t4 t3 t2 t1 t0 " {
		t.Fatalf("walked %q", seen)
	}
	// A page that ends exactly at the last conversation has no next.
	if _, next, _ := x.ThreadPage("acme", ThreadQuery{Limit: 7}); next != "" {
		t.Fatalf("a full last page offered a next page: %q", next)
	}

	// Filters apply before the limit.
	page, next, _ := x.ThreadPage("acme", ThreadQuery{Origin: "api", Limit: 2})
	if ids(page) != "t6 t4 " || next == "" {
		t.Fatalf("origin page = %q, next %q", ids(page), next)
	}
	page, _, _ = x.ThreadPage("acme", ThreadQuery{Origin: "api", Limit: 2, Before: next})
	if ids(page) != "t2 t0 " {
		t.Fatalf("origin page two = %q", ids(page))
	}
	if page, _, _ := x.ThreadPage("acme", ThreadQuery{Agent: "helper"}); ids(page) != "t5 t3 t1 " {
		t.Fatalf("agent = %q", ids(page))
	}
	if page, _, _ := x.ThreadPage("acme", ThreadQuery{Standing: StandingFailed}); ids(page) != "t3 " {
		t.Fatalf("failed = %q", ids(page))
	}
	if page, _, _ := x.ThreadPage("acme", ThreadQuery{Standing: StandingRunning}); ids(page) != "t6 " {
		t.Fatalf("running = %q", ids(page))
	}

	// New activity between pages: a conversation on a later page moves to the
	// top. It is not repeated and nothing else is skipped.
	first, next, _ := x.ThreadPage("acme", ThreadQuery{Limit: 3})
	record(t, x, "acme", "t1-again", "helper", "api", 30*time.Minute, map[string]string{MetaThread: "t1"})
	rest, _, _ := x.ThreadPage("acme", ThreadQuery{Limit: 10, Before: next})
	if ids(first) != "t6 t5 t4 " || ids(rest) != "t3 t2 t0 " {
		t.Fatalf("after new activity: first %q, rest %q", ids(first), ids(rest))
	}
	if top, _, _ := x.ThreadPage("acme", ThreadQuery{Limit: 1}); ids(top) != "t1 " {
		t.Fatalf("the newly active conversation is not on top: %q", ids(top))
	}

	// Another tenant, a bad cursor, and the cap.
	if page, _, _ := x.ThreadPage("globex", ThreadQuery{}); ids(page) != "other " {
		t.Fatalf("other tenant = %q", ids(page))
	}
	if _, _, err := x.ThreadPage("acme", ThreadQuery{Before: "not a cursor"}); !errors.Is(err, ErrCursor) {
		t.Fatalf("bad cursor: %v", err)
	}
	if page, _, _ := x.ThreadPage("acme", ThreadQuery{Limit: 100000}); len(page) > MaxThreadPage {
		t.Fatalf("the limit was not capped: %d", len(page))
	}
}

// A run asked to stop is stopping, not stopped, until the runtime says how it
// ended. lifecycle.stopped ends it with its partial answer and the reason the
// runtime honoured. A completion that wins the race ends it completed, with
// no cause and no stopping left on it.
func TestAStopIsRequestedThenConfirmedByTheRuntime(t *testing.T) {
	x := NewIndex()
	record(t, x, "acme", "r1", "lead", "api", 0, nil)
	record(t, x, "acme", "r2", "lead", "api", 0, nil)
	for _, id := range []string{"r1", "r2"} {
		r, ok := x.AskStop("acme", id, t0.Add(time.Second))
		if !ok || !r.Stopping || r.Status.Terminal() || r.Cause != "" {
			t.Fatalf("asked = %+v, %v", r, ok)
		}
	}
	// Asking again keeps the first request.
	if r, _ := x.AskStop("acme", "r1", t0.Add(2*time.Second)); !r.StopRequestedAt.Equal(t0.Add(time.Second)) {
		t.Fatalf("a second request moved the first: %+v", r)
	}

	b, _ := json.Marshal(event.Stopped{Reason: "Stopped by ada", By: "ada", Text: "Half"})
	r, ended := x.Observe(event.Envelope{Run: "r1", Tenant: "acme", Type: event.TypeStopped, Data: wire.BodyOf(event.TypeStopped, b), Time: t0.Add(3 * time.Second)})
	if !ended || r.Status != Stopped || r.Answer != "Half" || r.Cause != "Stopped by ada" || r.Stopping {
		t.Fatalf("stopped = %+v, %v", r, ended)
	}
	r, ended = end(t, x, "acme", "r2", 3*time.Second, event.TypeCompleted, wire.Lifecycle{Text: "Whole"})
	if !ended || r.Status != Completed || r.Answer != "Whole" || r.Cause != "" || r.Stopping {
		t.Fatalf("completed first = %+v, %v", r, ended)
	}
	if _, ok := x.AskStop("acme", "r2", t0.Add(4*time.Second)); ok {
		t.Fatal("a run that ended was asked to stop")
	}
	if _, again := x.Observe(event.Envelope{Run: "r1", Tenant: "acme", Type: event.TypeStopped, Data: wire.BodyOf(event.TypeStopped, b), Time: t0.Add(5 * time.Second)}); again {
		t.Fatal("a redelivered lifecycle.stopped ended the run twice")
	}
}

func usageCall(t *testing.T, tenant, runID, key string, micros int64) event.Envelope {
	t.Helper()
	b, err := json.Marshal(event.UsageCall{AccountingID: key, PromptTokens: 10, Cost: &event.Cost{Micros: micros, Currency: "USD"}})
	if err != nil {
		t.Fatal(err)
	}
	return event.Envelope{Run: runID, Tenant: tenant, Type: event.TypeUsageCall, Data: wire.BodyOf(event.TypeUsageCall, b), Time: t0}
}

// A usage report is folded into a run only in the run's own tenant: another
// tenant's event naming the same run id changes nothing and is not returned.
func TestObserveUsageIsConfinedToTheRunsTenant(t *testing.T) {
	x := NewIndex()
	record(t, x, "acme", "r1", "lead", "api", 0, nil)
	if _, ok := x.ObserveUsage(usageCall(t, "globex", "r1", "k1", 4500)); ok {
		t.Fatal("another tenant's usage report was folded into the run")
	}
	if r, _ := x.Get("acme", "r1"); r.Usage != nil {
		t.Fatalf("the run took another tenant's usage: %+v", r.Usage)
	}
	r, ok := x.ObserveUsage(usageCall(t, "acme", "r1", "k1", 4500))
	if !ok || r.Usage == nil || r.Usage.Total.Cost.Micros != 4500 {
		t.Fatalf("the run's own report: %+v %v", r.Usage, ok)
	}
}

// Once a run's spending is closed, a late call changes neither its usage nor
// its Spent: it has settled, and perhaps settled into its caller.
func TestAClosedRunsSpentDoesNotMove(t *testing.T) {
	x := NewIndex()
	record(t, x, "acme", "r1", "lead", "api", 0, nil)
	x.Spend("acme", "r1", 100)
	x.SetUsage("acme", "r1", &event.RunUsage{Total: event.Usage{ModelCalls: 1}}, 50)
	if _, ok := x.Spend("acme", "r1", 900); ok {
		t.Fatal("a closed run took more spend")
	}
	x.SetUsage("acme", "r1", nil, 1000) // a second close
	if r, _ := x.Get("acme", "r1"); r.Spent != 150 || r.Usage.Total.ModelCalls != 1 {
		t.Fatalf("closed run = spent %d, usage %+v", r.Spent, r.Usage)
	}
}

// A call that ends after its caller settled climbs past it to the nearest
// ancestor still open, or the root, so the tree's spend is whole.
func TestACallEndingAfterItsCallerSettlesClimbsToTheRoot(t *testing.T) {
	x := NewIndex()
	root := wire.NewEvent(wire.Raise{ID: "root", Tenant: "acme", Agent: "lead", Origin: "api", Text: "go", At: t0})
	lim := call.Limits{MaxDepth: 3, MaxFanOut: 3}
	mid, err := call.New(root, call.Request{Key: "k1", Agent: "mid", Text: "go", Mode: call.Delegate}, call.State{}, lim, t0)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := call.New(mid, call.Request{Key: "k2", Agent: "leaf", Text: "go", Mode: call.Delegate}, call.State{}, lim, t0)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for _, tag := range []event.Envelope{mid, leaf} {
		id := runtime.RunID("acme", tag.ID)
		ids[tag.Agent] = id
		if err := x.Record(id, tag, 1); err != nil {
			t.Fatal(err)
		}
	}
	rootID := runtime.RunID("acme", "root")
	if err := x.Record(rootID, root, 1); err != nil {
		t.Fatal(err)
	}
	x.Spend("acme", ids["mid"], 300)
	x.Spend("acme", ids["leaf"], 700)
	// mid ends first and settles into the root; leaf ends after.
	x.SetUsage("acme", ids["mid"], nil, 0)
	m, _ := x.Get("acme", ids["mid"])
	x.SettleCall(m)
	x.SetUsage("acme", ids["leaf"], nil, 0)
	l, _ := x.Get("acme", ids["leaf"])
	x.SettleCall(l)
	x.SettleCall(l) // a second report changes nothing
	if got, _ := x.Get("acme", ids["mid"]); got.Spent != 1000 {
		t.Fatalf("mid spent %d, want its own and the leaf's", got.Spent)
	}
	if got, _ := x.Get("acme", rootID); got.Spent != 1000 {
		t.Fatalf("the root spent %d, want the whole tree's 1000", got.Spent)
	}
}
