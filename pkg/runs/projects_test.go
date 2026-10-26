package runs

import (
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/fiber/pkg/agentrt/payload"
	"github.com/urmzd/fiber/pkg/envelope"
	"github.com/urmzd/fiber/pkg/runtime"

	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// scanned is what the counters replace: every conversation summed by the
// project it is filed in, but those in leave.
func scanned(x *Index, tenant string, leave []string) map[string]ProjectCount {
	out := map[string]ProjectCount{}
	for _, s := range x.Threads(tenant, 0) {
		if slices.Contains(leave, s.ID) {
			continue
		}
		c := out[s.Project]
		c.Threads++
		c.Active += s.Active
		if s.UpdatedAt.After(c.ActiveAt) {
			c.ActiveAt = s.UpdatedAt
		}
		out[s.Project] = c
	}
	return out
}

func sameCounts(t *testing.T, x *Index, tenant string) {
	t.Helper()
	want := scanned(x, tenant, nil)
	equalCounts(t, tenant+" counters", x.ProjectCounts(tenant), want)
	mark := x.Opened(tenant)
	equalCounts(t, tenant+" each conversation's counts", x.ProjectCountsWhere(tenant, mark, func(string) bool { return true }), want)
	equalCounts(t, tenant+" counters as of now", x.ProjectCountsWhere(tenant, mark, nil), want)
}

func equalCounts(t *testing.T, what string, got, want map[string]ProjectCount) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: %v, a scan %v", what, got, want)
	}
	for name, w := range want {
		g := got[name]
		if g.Threads != w.Threads || g.Active != w.Active || !g.ActiveAt.Equal(w.ActiveAt) {
			t.Fatalf("%s %q: %+v, a scan %+v", what, name, g, w)
		}
	}
}

// busy records a tenant's runs across projects: conversations at the root and
// in two projects, a follow-up that names another project and stays where its
// conversation is filed, a call, and every way a run ends.
func busy(t *testing.T, x *Index, tenant string) {
	t.Helper()
	at := func(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }
	api := func(id, project, thread string, s int) string {
		meta := map[string]string{MetaProject: project}
		if thread != "" {
			meta[MetaThread], meta[MetaPrev] = thread, thread
		}
		runID := runtime.RunID(tenant, id)
		if err := x.Record(runID, envelope.Tag{ID: id, Tenant: tenant, Agent: "lead", Origin: OriginAPI, Text: "go", Meta: meta, At: at(s)}, 1); err != nil {
			t.Fatal(err)
		}
		return runID
	}
	event := func(runID string, kind envelope.Kind, body payload.Lifecycle, s int) {
		b, err := payload.Encode(body)
		if err != nil {
			t.Fatal(err)
		}
		x.Observe(envelope.Event{RunID: runID, Tenant: tenant, Kind: kind, Payload: b, At: at(s)})
	}
	root := api("a", "", "", 1)
	launch := api("b", "Launch", "", 2)
	followUp := api("c", "Field", launch, 3)
	field := api("d", "Field", "", 4)
	parked := api("e", "Field", "", 5)

	caller := envelope.Tag{ID: "d", Tenant: tenant, Agent: "lead", Origin: OriginAPI, Text: "go", Meta: map[string]string{MetaProject: "Field"}, At: at(4)}
	called, err := call.New(caller, call.Request{Key: "k1", Agent: "helper", Text: "help", Mode: call.Subagent}, call.State{}, call.Limits{MaxDepth: 2, MaxFanOut: 2}, at(6))
	if err != nil {
		t.Fatal(err)
	}
	child := runtime.RunID(tenant, called.ID)
	if err := x.Record(child, called, 1); err != nil {
		t.Fatal(err)
	}

	event(root, envelope.KindStarted, payload.Lifecycle{}, 7)
	event(root, envelope.KindCompleted, payload.Lifecycle{Text: "done"}, 8)
	event(launch, envelope.KindStarted, payload.Lifecycle{}, 9)
	event(launch, envelope.KindFailed, payload.Lifecycle{Error: "x", Terminal: true}, 10)
	x.AskStop(followUp, at(11))
	x.Stop(followUp, "partial", "", at(12))
	event(parked, envelope.KindParked, payload.Lifecycle{Reason: "a question"}, 13)
	// The caller answers while its call still works, so it is held open,
	// then settles once the call is done.
	event(field, envelope.KindCompleted, payload.Lifecycle{Text: "done"}, 14)
	x.End(child, Completed, "helped", "", at(15))
	x.Settle(field, at(16))
}

func TestProjectCountsAgreeWithAScanOfEveryRun(t *testing.T) {
	x := NewIndex()
	busy(t, x, "acme")
	busy(t, x, "globex")
	sameCounts(t, x, "acme")
	sameCounts(t, x, "globex")

	got := x.ProjectCounts("acme")
	// The follow-up named Field and stays in Launch's conversation.
	if got["Launch"].Threads != 1 || got["Field"].Threads != 2 || got[""].Threads != 1 {
		t.Fatalf("threads by project %+v", got)
	}
	// Only the parked run is still in flight.
	if got["Field"].Active != 1 || got["Launch"].Active != 0 || got[""].Active != 0 {
		t.Fatalf("active by project %+v", got)
	}
	if want := t0.Add(16 * time.Second); !got["Field"].ActiveAt.Equal(want) {
		t.Fatalf("Field active at %v, want %v", got["Field"].ActiveAt, want)
	}
	if len(x.ProjectCounts("initech")) != 0 {
		t.Fatal("a tenant with no runs has counts")
	}
}

// A reader's counts leave out the conversations they may not read, as if
// those were never filed: how many there are, how many of their runs are in
// flight, and when they were last active.
func TestProjectCountsLeaveOutWhatAReaderMayNotRead(t *testing.T) {
	x := NewIndex()
	busy(t, x, "acme")
	parked := runtime.RunID("acme", "e")
	got := x.ProjectCountsWhere("acme", x.Opened("acme"), func(thread string) bool { return thread != parked })
	equalCounts(t, "without the parked conversation", got, scanned(x, "acme", []string{parked}))
	if got["Field"].Threads != 1 || got["Field"].Active != 0 {
		t.Fatalf("Field without the parked conversation counts %+v", got["Field"])
	}
	if none := x.ProjectCountsWhere("acme", x.Opened("acme"), func(string) bool { return false }); len(none) != 0 {
		t.Fatalf("a reader who may read nothing counts %v", none)
	}
}

func TestProjectCountsAreReadBackAfterARestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		x, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		busy(t, x, "acme")
		if err := x.Flush(); err != nil {
			t.Fatal(err)
		}
		again, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		before, after := x.ProjectCounts("acme"), again.ProjectCounts("acme")
		if len(before) != len(after) {
			t.Fatalf("before %v, after %v", before, after)
		}
		for name, b := range before {
			if a := after[name]; a.Threads != b.Threads || a.Active != b.Active || !a.ActiveAt.Equal(b.ActiveAt) {
				t.Fatalf("%q: before %+v, after %+v", name, b, a)
			}
		}
		sameCounts(t, again, "acme")
	})
}

// The conversations runs of origin system opened are known, in the order
// they were recorded and again after a restart. A run that joins a
// conversation opens none, and neither does a run of another origin.
func TestSystemThreadsAreTheConversationsSystemRunsOpened(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		x, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		busy(t, x, "acme")
		record := func(tenant, id, origin string, meta map[string]string) string {
			t.Helper()
			runID := runtime.RunID(tenant, id)
			if err := x.Record(runID, envelope.Tag{ID: id, Tenant: tenant, Agent: "scribe", Origin: origin, Text: "title it", Meta: meta, At: t0.Add(time.Minute)}, 1); err != nil {
				t.Fatal(err)
			}
			return runID
		}
		title := record("acme", "sys-title-a", OriginSystem, nil)
		memory := record("acme", "sys-memory-a", OriginSystem, map[string]string{MetaProject: "Field"})
		record("acme", "api-again", OriginAPI, map[string]string{MetaThread: title, MetaPrev: title})
		record("globex", "sys-title-b", OriginSystem, nil)
		if got := x.SystemThreads("acme"); !slices.Equal(got, []string{title, memory}) {
			t.Fatalf("acme's system threads = %v, want %v", got, []string{title, memory})
		}
		if got := x.SystemThreads("initech"); len(got) != 0 {
			t.Fatalf("a tenant with no runs has system threads %v", got)
		}
		sameCounts(t, x, "acme")
		if err := x.Flush(); err != nil {
			t.Fatal(err)
		}
		again, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if got := again.SystemThreads("acme"); !slices.Equal(got, []string{title, memory}) {
			t.Fatalf("after a restart acme's system threads = %v", got)
		}
	})
}

// A reader's counts are read as of a mark. A conversation opened since is
// left out, with or without a keep, while a run that joins a conversation
// opened before the mark is counted in it. A later mark counts the new one,
// and a mark taken after a restart counts what was read back.
func TestProjectCountsAreReadAsOfAMark(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		x, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if got := x.Opened("acme"); got != 0 {
			t.Fatalf("a tenant with no runs has opened %d conversations", got)
		}
		busy(t, x, "acme")
		busy(t, x, "globex")
		mark := x.Opened("acme")
		if mark != 4 || x.Opened("globex") != 4 {
			t.Fatalf("acme opened %d conversations and globex %d, not the 4 each busy opens", mark, x.Opened("globex"))
		}
		before := scanned(x, "acme", nil)
		record := func(id string, meta map[string]string, s int) string {
			t.Helper()
			runID := runtime.RunID("acme", id)
			if err := x.Record(runID, envelope.Tag{ID: id, Tenant: "acme", Agent: "scribe", Origin: OriginSystem, Text: "title it", Meta: meta, At: t0.Add(time.Duration(s) * time.Minute)}, 1); err != nil {
				t.Fatal(err)
			}
			return runID
		}
		// Opened after the mark: a housekeeping conversation at the root and
		// another in Field, and one more conversation in Launch.
		title := record("sys-title-late", nil, 1)
		record("sys-memory-late", map[string]string{MetaProject: "Field"}, 2)
		if err := x.Record(runtime.RunID("acme", "late"), envelope.Tag{ID: "late", Tenant: "acme", Agent: "lead", Origin: OriginAPI, Text: "go", Meta: map[string]string{MetaProject: "Launch"}, At: t0.Add(3 * time.Minute)}, 1); err != nil {
			t.Fatal(err)
		}
		equalCounts(t, "as of the mark", x.ProjectCountsWhere("acme", mark, nil), before)
		equalCounts(t, "as of the mark, keeping all", x.ProjectCountsWhere("acme", mark, func(string) bool { return true }), before)
		if got := x.ProjectCountsWhere("acme", mark, func(thread string) bool { return thread != title }); got[""].Threads != 1 || got["Field"].Threads != 2 || got["Launch"].Threads != 1 {
			t.Fatalf("as of the mark, keeping all but one late conversation, the counts are %+v", got)
		}
		// A mark from before any run counts nothing, and one past the end
		// counts everything.
		if got := x.ProjectCountsWhere("acme", 0, nil); len(got) != 0 {
			t.Fatalf("as of no conversations the counts are %+v", got)
		}
		equalCounts(t, "past the end", x.ProjectCountsWhere("acme", 1_000, nil), scanned(x, "acme", nil))
		// Another tenant's mark is its own.
		equalCounts(t, "globex as of its mark", x.ProjectCountsWhere("globex", x.Opened("globex"), nil), scanned(x, "globex", nil))
		// A run that joins a conversation opened before the mark is counted
		// in it.
		joined := runtime.RunID("acme", "a")
		if err := x.Record(runtime.RunID("acme", "again"), envelope.Tag{ID: "again", Tenant: "acme", Agent: "lead", Origin: OriginAPI, Text: "and", Meta: map[string]string{MetaThread: joined, MetaPrev: joined}, At: t0.Add(4 * time.Minute)}, 1); err != nil {
			t.Fatal(err)
		}
		if got := x.ProjectCountsWhere("acme", mark, nil)[""]; got.Threads != 1 || got.Active != 1 || !got.ActiveAt.Equal(t0.Add(4*time.Minute)) {
			t.Fatalf("the root as of the mark, after a run joined its conversation, counts %+v", got)
		}
		later := x.Opened("acme")
		if later != mark+3 {
			t.Fatalf("acme opened %d conversations, not %d", later, mark+3)
		}
		equalCounts(t, "as of a later mark", x.ProjectCountsWhere("acme", later, nil), scanned(x, "acme", nil))

		if err := x.Flush(); err != nil {
			t.Fatal(err)
		}
		again, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if got := again.Opened("acme"); got != later {
			t.Fatalf("after a restart acme opened %d conversations, not %d", got, later)
		}
		sameCounts(t, again, "acme")
	})
}

func TestProjectCountsHoldUnderRacingRunsAndReaders(t *testing.T) {
	x := NewIndex()
	var wg sync.WaitGroup
	for i := range 40 {
		wg.Go(func() {
			id := fmt.Sprintf("r%d", i)
			project := []string{"", "Launch", "Field"}[i%3]
			runID := runtime.RunID("acme", id)
			tag := envelope.Tag{ID: id, Tenant: "acme", Agent: "lead", Origin: OriginAPI, Text: "go",
				Meta: map[string]string{MetaProject: project}, At: t0.Add(time.Duration(i) * time.Millisecond)}
			if err := x.Record(runID, tag, 1); err != nil {
				t.Error(err)
				return
			}
			if i%2 == 0 {
				x.End(runID, Completed, "done", "", t0.Add(time.Second+time.Duration(i)*time.Millisecond))
			}
		})
		wg.Go(func() { _ = x.ProjectCounts("acme") })
	}
	wg.Wait()
	sameCounts(t, x, "acme")
	total := 0
	for _, c := range x.ProjectCounts("acme") {
		total += c.Active
	}
	if total != 20 {
		t.Fatalf("%d runs in flight, want the 20 that did not end", total)
	}
}

func TestThreadPageKeepsTheProjectsAsked(t *testing.T) {
	x := NewIndex()
	busy(t, x, "acme")
	busy(t, x, "globex")
	ids := func(q ThreadQuery) []string {
		t.Helper()
		page, _, err := x.ThreadPage("acme", q)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, s := range page {
			out = append(out, s.Project)
		}
		slices.Sort(out)
		return out
	}
	if got := ids(ThreadQuery{}); !slices.Equal(got, []string{"", "Field", "Field", "Launch"}) {
		t.Fatalf("no filter = %v", got)
	}
	if got := ids(ThreadQuery{Projects: []string{"Field"}}); !slices.Equal(got, []string{"Field", "Field"}) {
		t.Fatalf("one project = %v", got)
	}
	if got := ids(ThreadQuery{Projects: []string{"Field", "Launch"}}); !slices.Equal(got, []string{"Field", "Field", "Launch"}) {
		t.Fatalf("a subtree = %v", got)
	}
	if got := ids(ThreadQuery{Projects: []string{""}}); !slices.Equal(got, []string{""}) {
		t.Fatalf("the root = %v", got)
	}
	if got := ids(ThreadQuery{Projects: []string{}}); len(got) != 0 {
		t.Fatalf("an empty list kept %v", got)
	}
	// The filter applies before the limit, so a page is a page of matches.
	page, next, err := x.ThreadPage("acme", ThreadQuery{Projects: []string{"Field"}, Limit: 1})
	if err != nil || len(page) != 1 || page[0].Project != "Field" || next == "" {
		t.Fatalf("first page %v next %q err %v", page, next, err)
	}
	rest, next, err := x.ThreadPage("acme", ThreadQuery{Projects: []string{"Field"}, Limit: 1, Before: next})
	if err != nil || len(rest) != 1 || rest[0].Project != "Field" || rest[0].ID == page[0].ID || next != "" {
		t.Fatalf("second page %v next %q err %v", rest, next, err)
	}
}

func TestCurrentPlacementCountsKeepExecutionAttribution(t *testing.T) {
	x := NewIndex()
	id := runtime.RunID("acme", "move")
	if err := x.Record(id, envelope.Tag{ID: "move", Tenant: "acme", Agent: "helper", Origin: OriginAPI, Text: "hi", Meta: map[string]string{MetaProject: "personal-opaque"}, At: t0}, 1); err != nil {
		t.Fatal(err)
	}
	got := x.ProjectCountsPlaced("acme", x.Opened("acme"), nil, func(thread, original string) string {
		if thread != id || original != "personal-opaque" {
			t.Fatal("bad resolver input")
		}
		return "Field"
	})
	if got["Field"].Threads != 1 || got["personal-opaque"].Threads != 0 {
		t.Fatalf("placement counts: %v", got)
	}
	raw, err := x.Get("acme", id)
	if err != nil || raw.Project != "personal-opaque" || x.ProjectCounts("acme")["personal-opaque"].Threads != 1 {
		t.Fatal("operational projection changed accounting attribution")
	}
}
