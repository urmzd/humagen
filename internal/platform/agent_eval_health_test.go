package platform

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/evals"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/saige/flow/event"
)

// Flagged runs are counted from the runs a caller may read only: a
// nonparticipant is told neither how many private runs were flagged nor that
// any were, and the count past MaxFlagged is said only to whom it is true.
func TestFlaggedCountsOnlyTheRunsTheCallerMayRead(t *testing.T) {
	p, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	// Many private runs in ana's private thread, and one public run.
	privateEvalTraceRun(t, p, "private-0", "ana")
	for i := 1; i < MaxFlagged+5; i++ {
		id := fmt.Sprintf("private-%d", i)
		tag := wire.NewEvent(wire.Raise{ID: id, Origin: runs.OriginAPI, Tenant: "acme", Agent: BuiltinBuilder, Actor: event.Actor{ID: "ana"}, At: time.Now(), Context: map[string]string{runs.MetaThread: "th-1234567890abcdef"}})
		if err := p.Runs.Record(id, tag, 1); err != nil {
			t.Fatal(err)
		}
	}
	public := wire.NewEvent(wire.Raise{ID: "public-run", Origin: runs.OriginAPI, Tenant: "acme", Agent: BuiltinBuilder, At: time.Now()})
	if err := p.Runs.Record("public-run", public, 1); err != nil {
		t.Fatal(err)
	}
	flag := func(run string, raters int) {
		for i := range raters {
			c := evals.Candidate{Run: run, Agent: BuiltinBuilder, Input: "q", Reason: "wrong", At: time.Now()}
			if err := p.Evals.SaveCandidate("acme", fmt.Sprintf("%s-rater-%d", run, i), c); err != nil {
				t.Fatal(err)
			}
		}
	}
	for i := range MaxFlagged + 5 {
		flag(fmt.Sprintf("private-%d", i), 1)
	}
	// One run rated down by many people is one run.
	flag("public-run", MaxFlagged+5)

	owner := auth.Principal{Tenant: "acme", Subject: "ana", Kind: auth.User, Roles: []auth.Role{auth.Admin}}
	stranger := auth.Principal{Tenant: "acme", Subject: "bob", Kind: auth.User, Roles: []auth.Role{auth.Operator}}
	noQueue := auth.Principal{Tenant: "acme", Subject: "agents-only", Kind: auth.Machine, Scopes: []auth.Permission{auth.AgentsRead, auth.RunsRead}}
	other := owner
	other.Tenant = "other"

	for _, c := range []struct {
		name string
		who  auth.Principal
		n    int // -1: the count is left out
		more bool
	}{
		{"owner", owner, MaxFlagged, true},
		{"same-tenant nonparticipant", stranger, 1, false},
		// Left out, not zero: they may not read the queue.
		{"no evals:read", noQueue, -1, false},
		// Zero, not left out: they may read the queue and it holds none of theirs.
		{"another tenant", other, 0, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			h, err := p.AgentEvaluationHealth(c.who, BuiltinBuilder)
			if err != nil {
				t.Fatal(err)
			}
			if flaggedOf(h) != c.n || h.FlaggedMore != c.more {
				t.Fatalf("flagged = %d, more = %v; want %d, %v", flaggedOf(h), h.FlaggedMore, c.n, c.more)
			}
		})
	}
}

// flaggedOf is the flagged count health states, or -1 when it states none.
func flaggedOf(h AgentEvaluationHealth) int {
	if h.Flagged == nil {
		return -1
	}
	return *h.Flagged
}

// A schedule is read from the runs the reader may see: a replay of a private
// run that is going makes the suite busy for its owner, and for a stranger
// moves neither busy nor the next run.
func TestScheduleIsReadFromTheRunsTheReaderMaySee(t *testing.T) {
	p, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	privateEvalTraceRun(t, p, "original", "ana")
	suite := evalHistorySuite()
	suite.EverySeconds = 6 * 3600
	if err := p.Evals.Put("acme", suite, false); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if _, err := p.Evals.Begin("acme", suite, 1, "ana", started); err != nil {
		t.Fatal(err)
	}
	// The replay is removed: the suite is public, its going run is not.
	suite.Cases[0].Trace = nil
	if err := p.Evals.Put("acme", suite, true); err != nil {
		t.Fatal(err)
	}
	owner := auth.Principal{Tenant: "acme", Subject: "ana", Kind: auth.User, Roles: []auth.Role{auth.Admin}}
	stranger := auth.Principal{Tenant: "acme", Subject: "bob", Kind: auth.User, Roles: []auth.Role{auth.Operator}}
	if !p.Seer(stranger).Suite(suite) {
		t.Fatal("the suite is not public")
	}

	runs, schedule, err := p.EvalSuiteListing(owner, suite)
	if err != nil || len(runs) != 1 || !schedule.Busy || !schedule.Next.Equal(started.Add(6*time.Hour)) {
		t.Fatalf("owner: runs = %d, schedule = %+v, %v", len(runs), schedule, err)
	}
	before := time.Now()
	runs, schedule, err = p.EvalSuiteListing(stranger, suite)
	if err != nil || len(runs) != 0 {
		t.Fatalf("stranger runs = %+v %v", runs, err)
	}
	// Nothing they may see has run, so it is due; nothing says a run is going.
	if !schedule.Scheduled || schedule.Busy || schedule.Next.Before(before) || schedule.Next.After(time.Now()) {
		t.Fatalf("stranger schedule = %+v", schedule)
	}
}

// The count reads the window the review queue reads, so the two agree: a
// stranger's count is the runs the queue shows them, whatever candidates
// they cannot see are added, and nothing in it says those were added.
func TestFlaggedCountAgreesWithTheQueue(t *testing.T) {
	p, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	privateEvalTraceRun(t, p, "private-run", "ana")
	for i := range 5 {
		id := fmt.Sprintf("public-%d", i)
		tag := wire.NewEvent(wire.Raise{ID: id, Origin: runs.OriginAPI, Tenant: "acme", Agent: BuiltinBuilder, At: time.Now()})
		if err := p.Runs.Record(id, tag, 1); err != nil {
			t.Fatal(err)
		}
	}
	save := func(run, rater string) {
		c := evals.Candidate{Run: run, Agent: BuiltinBuilder, Input: "q", Reason: "wrong", At: time.Now()}
		if err := p.Evals.SaveCandidate("acme", rater, c); err != nil {
			t.Fatal(err)
		}
	}
	stranger := auth.Principal{Tenant: "acme", Subject: "bob", Kind: auth.User, Roles: []auth.Role{auth.Operator}}
	queued := func() int {
		runs := map[string]bool{}
		after := ""
		for {
			page, err := p.ListEvalCandidates(stranger, BuiltinBuilder, MaxEvalCandidatePage, after)
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range page.Candidates {
				runs[c.Run] = true
			}
			if page.Next == "" {
				return len(runs)
			}
			after = page.Next
		}
	}
	count := func() (int, bool) {
		h, err := p.AgentEvaluationHealth(stranger, BuiltinBuilder)
		if err != nil {
			t.Fatal(err)
		}
		return flaggedOf(h), h.FlaggedMore
	}
	for i := range 5 {
		save(fmt.Sprintf("public-%d", i), "bob")
	}
	if n, more := count(); n != 5 || more || queued() != 5 {
		t.Fatalf("before: flagged = %d, more = %v, queue shows %d", n, more, queued())
	}
	for i := range 3 * state.MaxList {
		save("private-run", fmt.Sprintf("rater-%d", i))
	}
	n, more := count()
	if n != queued() || more {
		t.Fatalf("after hidden candidates: flagged = %d, more = %v, queue shows %d runs", n, more, queued())
	}
}

// Whether the queue holds candidates past the window it reads is known
// before anyone's visibility is applied, so health does not say it: a reader
// who sees none of over a thousand candidates is told zero and nothing more,
// as they are when the queue is empty, and the queue shows them none.
func TestFlaggedSaysNothingOfCandidatesTheCallerCannotSee(t *testing.T) {
	p, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	privateEvalTraceRun(t, p, "private-run", "ana")
	stranger := auth.Principal{Tenant: "acme", Subject: "bob", Kind: auth.User, Roles: []auth.Role{auth.Operator}}
	owner := auth.Principal{Tenant: "acme", Subject: "ana", Kind: auth.User, Roles: []auth.Role{auth.Operator}}
	empty, err := p.AgentEvaluationHealth(stranger, BuiltinBuilder)
	if err != nil {
		t.Fatal(err)
	}
	for i := range state.MaxList + 200 {
		c := evals.Candidate{Run: "private-run", Agent: BuiltinBuilder, Input: "q", Reason: "wrong", At: time.Now()}
		if err := p.Evals.SaveCandidate("acme", fmt.Sprintf("rater-%d", i), c); err != nil {
			t.Fatal(err)
		}
	}
	h, err := p.AgentEvaluationHealth(stranger, BuiltinBuilder)
	if err != nil {
		t.Fatal(err)
	}
	if flaggedOf(h) != 0 || h.FlaggedMore || flaggedOf(empty) != 0 || empty.FlaggedMore {
		t.Fatalf("a stranger to every candidate: flagged = %d, more = %v; with an empty queue %d, %v", flaggedOf(h), h.FlaggedMore, flaggedOf(empty), empty.FlaggedMore)
	}
	page, err := p.ListEvalCandidates(stranger, BuiltinBuilder, MaxEvalCandidatePage, "")
	if err != nil || len(page.Candidates) != 0 {
		t.Fatalf("the stranger's queue = %d rows, %v", len(page.Candidates), err)
	}
	// The owner reads the one run all of them rate: one, and no more.
	h, err = p.AgentEvaluationHealth(owner, BuiltinBuilder)
	if err != nil || flaggedOf(h) != 1 || h.FlaggedMore {
		t.Fatalf("owner: flagged = %d, more = %v, %v", flaggedOf(h), h.FlaggedMore, err)
	}
}

// A health refresh never uses break-glass. Every reader's answer (results,
// samples, flagged) is the runs they read in their own right, no more and no
// fewer, and refreshing writes no read-audit record: a governor's private
// runs they take no part in are left out of all three alike.
func TestAHealthRefreshRecordsNoReads(t *testing.T) {
	p, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	if _, err := p.Projects.Create("acme", projects.Project{Name: "Field", Members: []projects.Entry{{Who: "ana", Role: projects.Owner}, {Who: "bob", Role: projects.Viewer}}}, projects.Writer{By: "ana", Govern: true}, time.Now()); err != nil {
		t.Fatal(err)
	}
	// A checked, flagged run in each kind of place.
	seed := func(run, thread, project string, vis threads.Visibility, participants ...string) {
		ctx := map[string]string{}
		if thread != "" {
			th := threads.Thread{ID: thread, Channel: "general", Project: project, Visibility: vis, CreatedBy: "ana"}
			for _, who := range participants {
				th.Participants = append(th.Participants, threads.Participant{Kind: threads.Person, Name: who})
			}
			if _, err := p.Threads.Create("acme", th, time.Now()); err != nil {
				t.Fatal(err)
			}
			ctx[runs.MetaThread] = thread
		}
		if project != "" {
			ctx[runs.MetaProject] = project
		}
		tag := wire.NewEvent(wire.Raise{ID: run, Origin: runs.OriginAPI, Tenant: "acme", Agent: BuiltinBuilder, Actor: event.Actor{ID: "ana"}, At: time.Now(), Context: ctx})
		if err := p.Runs.Record(run, tag, 1); err != nil {
			t.Fatal(err)
		}
		if err := p.Evals.SaveOnline("acme", evals.Online{Run: run, Agent: BuiltinBuilder, Passed: true, At: time.Now()}); err != nil {
			t.Fatal(err)
		}
		c := evals.Candidate{Run: run, Agent: BuiltinBuilder, Input: "q", Reason: "wrong", At: time.Now()}
		if err := p.Evals.SaveCandidate("acme", "rater-"+run, c); err != nil {
			t.Fatal(err)
		}
	}
	seed("private", "th-00000000000000a1", "", threads.Private)
	seed("private-with-gov", "th-00000000000000a2", "", threads.Private, "gov")
	seed("project", "th-00000000000000a3", "Field", threads.Shared)
	seed("public", "", "", "")
	// More private runs than one page of results, so the cost of a leak is plain.
	for i := range 20 {
		seed(fmt.Sprintf("private-%02d", i), fmt.Sprintf("th-%016x", 0xb00+i), "", threads.Private)
	}

	admin := func(subject string) auth.Principal {
		return auth.Principal{Tenant: "acme", Subject: subject, Kind: auth.User, Roles: []auth.Role{auth.Admin}}
	}
	operator := func(subject string) auth.Principal {
		return auth.Principal{Tenant: "acme", Subject: subject, Kind: auth.User, Roles: []auth.Role{auth.Operator}}
	}
	// The governor may read the private run and the project's, by exception
	// only (checked on a holding seer, which records nothing until disclosed).
	for _, run := range []string{"private", "project", "private-07"} {
		check := p.Seer(admin("gov")).Snapshot().Holding()
		if !check.deciding("check", func() bool { return check.RunID(run) }) || !check.exceptional("check") {
			t.Fatalf("the governor does not read %s by break-glass", run)
		}
	}
	audited := func() int {
		rows, err := p.ReadAudit.List("acme", time.Now().Add(-time.Hour), time.Now().Add(time.Minute), 100000)
		if err != nil {
			t.Fatal(err)
		}
		return len(rows)
	}
	all := []string{"private", "private-with-gov", "project", "public"}
	for i := range 20 {
		all = append(all, fmt.Sprintf("private-%02d", i))
	}
	for _, c := range []struct {
		name string
		who  auth.Principal
		own  []string
	}{
		{"owner who takes part in every thread", admin("ana"), all},
		{"governor with part in one private thread, no member of the project", admin("gov"), []string{"private-with-gov", "public"}},
		{"stranger who is a member of the project", operator("bob"), []string{"project", "public"}},
		{"stranger operator, no member of the project", operator("carol"), []string{"public"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			before := audited()
			for range 3 {
				h, err := p.AgentEvaluationHealth(c.who, BuiltinBuilder)
				if err != nil {
					t.Fatal(err)
				}
				got := []string{}
				for _, res := range h.Results {
					got = append(got, res.Run)
				}
				slices.Sort(got)
				want := slices.Sorted(slices.Values(c.own))
				if !slices.Equal(got, want) || h.Samples != len(want) || h.Passed != len(want) {
					t.Fatalf("results = %v (samples %d, passed %d); want the runs read in their own right, %v", got, h.Samples, h.Passed, want)
				}
				if flaggedOf(h) != min(len(want), MaxFlagged) || h.FlaggedMore {
					t.Fatalf("flagged = %d, more = %v; want %d", flaggedOf(h), h.FlaggedMore, len(want))
				}
			}
			if n := audited() - before; n != 0 {
				t.Fatalf("three health refreshes wrote %d read-audit records", n)
			}
		})
	}
	if n := audited(); n != 0 {
		t.Fatalf("health wrote %d read-audit records in all", n)
	}
}

// A suite deleted after it was listed is gone, which the listing skips.
func TestASuiteListingOfADeletedSuiteIsNotFound(t *testing.T) {
	p, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	suite := evalHistorySuite()
	suite.Cases[0].Trace = nil
	if err := p.Evals.Put("acme", suite, false); err != nil {
		t.Fatal(err)
	}
	if err := p.Evals.Delete("acme", suite.Name); err != nil {
		t.Fatal(err)
	}
	who := auth.Principal{Tenant: "acme", Subject: "ana", Kind: auth.User, Roles: []auth.Role{auth.Admin}}
	if _, _, err := p.EvalSuiteListing(who, suite); !errors.Is(err, evals.ErrNotFound) {
		t.Fatalf("listing a deleted suite: %v", err)
	}
}
