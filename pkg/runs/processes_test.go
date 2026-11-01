package runs

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/event"
	"github.com/urmzd/humagen/fiber/pkg/runtime"
	"github.com/urmzd/humagen/internal/wire"

	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func two(t *testing.T, a, b state.Store) (*Index, *Index) {
	t.Helper()
	one, err := Open(a)
	if err != nil {
		t.Fatal(err)
	}
	other, err := Open(b)
	if err != nil {
		t.Fatal(err)
	}
	return one, other
}

func lifecycle(t *testing.T, tenant, id, kind string, seq uint64, body wire.Lifecycle) event.Envelope {
	t.Helper()
	b, err := wire.LifecycleBody(kind, body)
	if err != nil {
		t.Fatal(err)
	}
	return event.Envelope{Run: id, Tenant: tenant, Type: kind, Seq: seq, Data: b, Time: t0.Add(time.Duration(seq) * time.Second)}
}

// A run recorded through one process is read through another at once, and a
// stop asked through either is seen by both: the stop works from any
// replica.
func TestTwoProcessesShareRunsAndStops(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, other := two(t, a, b)
		record(t, one, "acme", "r1", "lead", "api", 0, map[string]string{MetaThread: "th"})
		r, err := other.Get("acme", "r1")
		if err != nil || r.Agent != "lead" || r.Thread != "th" || r.Status != Accepted {
			t.Fatalf("the other process reads %+v, %v", r, err)
		}
		if _, err := other.Get("globex", "r1"); err == nil {
			t.Fatal("another tenant reads the run")
		}
		if r, ok := other.AskStop("acme", "r1", t0.Add(time.Second)); !ok || !r.Stopping {
			t.Fatalf("AskStop = %+v, %v", r, ok)
		}
		if r, _ := one.Get("acme", "r1"); !r.Stopping || !r.StopRequestedAt.Equal(t0.Add(time.Second)) {
			t.Fatalf("the stop is not seen by the process that recorded the run: %+v", r)
		}
		if _, ended := one.Observe(lifecycle(t, "acme", "r1", event.TypeStopped, 3, wire.Lifecycle{Text: "partial"})); !ended {
			t.Fatal("the stopped event did not end the run")
		}
		if r, _ := other.Get("acme", "r1"); r.Status != Stopped || r.Answer != "partial" || r.Stopping {
			t.Fatalf("the other process reads %+v", r)
		}
		if th := other.Threads("acme", 0); len(th) != 1 || th[0].ID != "th" || th[0].Active != 0 {
			t.Fatalf("threads = %+v", th)
		}
		if got := other.Unfinished(); len(got) != 0 {
			t.Fatalf("unfinished = %+v", got)
		}
	})
}

// Two processes that each fold every event, as replicas do until delivery is
// split between them, change a run once per event: one of them, and only
// one, is told the event ended the run, and a usage report both fold, with
// an accounting id or without one, is
// counted once.
func TestTwoProcessesFoldAnEventOnce(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, other := two(t, a, b)
		const runs = 6
		for i := range runs {
			record(t, one, "acme", fmt.Sprintf("r%d", i), "lead", "api", 0, nil)
		}
		var ended atomic.Int32
		var wg sync.WaitGroup
		for _, x := range []*Index{one, other} {
			wg.Go(func() {
				for i := range runs {
					id := fmt.Sprintf("r%d", i)
					x.Observe(lifecycle(t, "acme", id, event.TypeStarted, 1, wire.Lifecycle{Attempt: 1}))
					for n := range 5 {
						x.ObserveUsage(event.Envelope{Run: id, Tenant: "acme", Type: event.TypeUsageCall, Seq: uint64(2 + n),
							Data: event.UsageCall{AccountingID: map[bool]string{true: fmt.Sprintf("%s-call-%d", id, n)}[n%2 == 0], PromptTokens: 10, CompletionTokens: 1}})
					}
					if _, end := x.Observe(lifecycle(t, "acme", id, event.TypeCompleted, 9, wire.Lifecycle{Text: "done"})); end {
						ended.Add(1)
					}
				}
			})
		}
		wg.Wait()
		if got := ended.Load(); got != runs {
			t.Fatalf("%d endings reported for %d runs", got, runs)
		}
		for i := range runs {
			r, _ := other.Get("acme", fmt.Sprintf("r%d", i))
			if r.Status != Completed || r.Usage == nil || r.Usage.Own.ModelCalls != 5 || r.Usage.Own.PromptTokens != 50 {
				t.Fatalf("run %d = %+v, usage %+v", i, r, r.Usage)
			}
		}
		if s := one.Summarize("acme", t0, t0.Add(time.Hour), time.Hour); s.Total.Runs != runs || s.Total.Completed != runs || s.Total.Active != 0 {
			t.Fatalf("summary = %+v", s.Total)
		}
	})
}

// Writers in two processes changing one run, recording runs into one
// conversation and settling calls into one caller lose nothing: each change
// is written at the version it read, and read again when it conflicts.
func TestTwoProcessesWritingTogetherLoseNothing(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		one, other := two(t, a, b)
		root := wire.NewEvent(wire.Raise{ID: "root", Tenant: "acme", Agent: "lead", Origin: OriginAPI, Text: "go", At: t0, Context: map[string]string{MetaThread: "th"}})
		rootID := runtime.RunID("acme", root.ID)
		if err := one.Record(rootID, root, 1); err != nil {
			t.Fatal(err)
		}
		const writers, each = 8, 10
		var wg sync.WaitGroup
		for i := range writers {
			x := []*Index{one, other}[i%2]
			wg.Go(func() {
				for n := range each {
					x.Spend("acme", rootID, 7)
					x.Called("acme", rootID, 0)
					// A turn in the same conversation, from either process.
					tag := wire.NewEvent(wire.Raise{ID: fmt.Sprintf("turn-%d-%d", i, n), Tenant: "acme", Agent: fmt.Sprintf("agent-%d", i%3), Origin: OriginAPI, Text: "more", At: t0.Add(time.Minute), Context: map[string]string{MetaThread: "th", MetaPrev: rootID}})
					if err := x.Record(runtime.RunID("acme", tag.ID), tag, 1); err != nil {
						t.Error(err)
					}
				}
			})
		}
		wg.Wait()
		r, _ := other.Get("acme", rootID)
		if r.Spent != writers*each*7 || r.Calls != writers*each {
			t.Fatalf("root spent %d with %d calls, want %d with %d", r.Spent, r.Calls, writers*each*7, writers*each)
		}
		turns, err := one.Conversation("acme", "th")
		if err != nil || len(turns) != writers*each+1 {
			t.Fatalf("%d turns, %v; want %d", len(turns), err, writers*each+1)
		}
		seen := map[string]bool{}
		for _, turn := range turns {
			if seen[turn.ID] {
				t.Fatalf("turn %s read twice", turn.ID)
			}
			seen[turn.ID] = true
		}
		th := other.Threads("acme", 0)
		if len(th) != 1 || th[0].Turns != writers*each+1 || th[0].Active != writers*each+1 || th[0].Branches != 1 || len(th[0].Agents) != 4 {
			t.Fatalf("thread = %+v", th)
		}
		if n := one.ProjectCounts("acme")[""]; n.Threads != 1 || n.Active != writers*each+1 {
			t.Fatalf("project count = %+v", n)
		}
		if l := other.List("acme", 0); len(l) != writers*each+1 {
			t.Fatalf("%d runs listed", len(l))
		}
	})
}

// A caller answering in one process while the run attached to it ends in
// another is held open until that run ends, and then settles: it is never
// left finishing with nothing attached.
func TestTwoProcessesSettleAnAttachedCall(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		for round := range 10 {
			one, other := two(t, a, b)
			tenant := fmt.Sprintf("t%d", round)
			root := wire.NewEvent(wire.Raise{ID: "root", Tenant: tenant, Agent: "lead", Origin: OriginAPI, Text: "go", At: t0})
			rootID := runtime.RunID(tenant, root.ID)
			if err := one.Record(rootID, root, 1); err != nil {
				t.Fatal(err)
			}
			child, err := call.New(root, call.Request{Key: "k", Agent: "helper", Text: "help", Mode: call.Subagent}, call.State{}, call.Limits{MaxDepth: 3, MaxFanOut: 3}, t0)
			if err != nil {
				t.Fatal(err)
			}
			childID := runtime.RunID(tenant, child.ID)
			if err := other.Record(childID, child, 1); err != nil {
				t.Fatal(err)
			}
			var wg sync.WaitGroup
			var endedRoot atomic.Bool
			wg.Go(func() {
				if _, ended := one.Observe(lifecycle(t, tenant, rootID, event.TypeCompleted, 5, wire.Lifecycle{Text: "answer"})); ended {
					endedRoot.Store(true)
				}
			})
			wg.Go(func() {
				if _, ended := other.Observe(lifecycle(t, tenant, childID, event.TypeCompleted, 5, wire.Lifecycle{Text: "helped"})); !ended {
					t.Error("the attached run did not end")
				}
				// What the platform does when an attached run ends.
				if _, ended := other.Settle(tenant, rootID, t0.Add(time.Minute)); ended {
					endedRoot.Store(true)
				}
			})
			wg.Wait()
			r, _ := one.Get(tenant, rootID)
			if r.Status != Completed || r.Finishing || !endedRoot.Load() {
				t.Fatalf("round %d: root = %s finishing %v, ended %v", round, r.Status, r.Finishing, endedRoot.Load())
			}
		}
	})
}

// Runs an older release kept one per key are moved once, in the order they
// were recorded, though four processes open the index at the same time.
func TestTwoProcessesConvertOnce(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, a, b state.Store) {
		old := state.NewTable[storedRun](a, oldRuns)
		for i := range 30 {
			id := fmt.Sprintf("old-%02d", i)
			status := Completed
			if i%5 == 0 {
				status = Running
			}
			s := storedRun{Seq: int64(i + 1), Run: Run{
				ID: id, Tenant: "acme", Agent: "lead", Origin: OriginAPI, Text: fmt.Sprintf("ask %d", i), Thread: fmt.Sprintf("th-%d", i%3),
				Root: id, Status: status, CreatedAt: t0.Add(time.Duration(i) * time.Second), UpdatedAt: t0.Add(time.Duration(i) * time.Second),
			}}
			if err := old.Put("acme", id, s); err != nil {
				t.Fatal(err)
			}
		}
		if err := state.NewTable[string](a, oldTitles).Put("acme", "th-1", "Named"); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		xs := make([]*Index, 4)
		for i := range xs {
			wg.Go(func() {
				x, err := Open([]state.Store{a, b}[i%2])
				if err != nil {
					t.Error(err)
				}
				xs[i] = x
			})
		}
		wg.Wait()
		if t.Failed() {
			return
		}
		l := xs[1].List("acme", 0)
		if len(l) != 30 {
			t.Fatalf("%d runs after converting, want 30", len(l))
		}
		for i, r := range l {
			if want := fmt.Sprintf("old-%02d", 29-i); r.ID != want {
				t.Fatalf("run %d is %s, want %s", i, r.ID, want)
			}
		}
		th := xs[2].Threads("acme", 0)
		if len(th) != 3 {
			t.Fatalf("threads = %+v", th)
		}
		turns, active := 0, 0
		for _, s := range th {
			turns += s.Turns
			active += s.Active
			if (s.ID == "th-1") != (s.Title == "Named" && s.Named) {
				t.Fatalf("title of %s = %q", s.ID, s.Title)
			}
		}
		if turns != 30 || active != 6 || len(xs[3].Unfinished()) != 6 {
			t.Fatalf("%d turns and %d active after converting", turns, active)
		}
	})
}
