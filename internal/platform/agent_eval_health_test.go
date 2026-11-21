package platform

import (
	"fmt"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/evals"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
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
		n    int
		more bool
	}{
		{"owner", owner, MaxFlagged, true},
		{"same-tenant nonparticipant", stranger, 1, false},
		{"no evals:read", noQueue, 0, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			h, err := p.AgentEvaluationHealth(c.who, BuiltinBuilder)
			if err != nil {
				t.Fatal(err)
			}
			if h.Flagged != c.n || h.FlaggedMore != c.more {
				t.Fatalf("flagged = %d, more = %v; want %d, %v", h.Flagged, h.FlaggedMore, c.n, c.more)
			}
		})
	}
	t.Run("another tenant", func(t *testing.T) {
		h, err := p.AgentEvaluationHealth(other, BuiltinBuilder)
		if err == nil && (h.Flagged != 0 || h.FlaggedMore) {
			t.Fatalf("another tenant's count = %d, more = %v", h.Flagged, h.FlaggedMore)
		}
	})
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

// The count is of visible runs whatever the queue holds before them: a
// thousand candidates on a private run leave a stranger's count of the one
// run they may read where it was.
func TestFlaggedCountIsStableUnderManyHiddenCandidates(t *testing.T) {
	p, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	privateEvalTraceRun(t, p, "private-run", "ana")
	public := wire.NewEvent(wire.Raise{ID: "public-run", Origin: runs.OriginAPI, Tenant: "acme", Agent: BuiltinBuilder, At: time.Now()})
	if err := p.Runs.Record("public-run", public, 1); err != nil {
		t.Fatal(err)
	}
	save := func(run, rater string) {
		c := evals.Candidate{Run: run, Agent: BuiltinBuilder, Input: "q", Reason: "wrong", At: time.Now()}
		if err := p.Evals.SaveCandidate("acme", rater, c); err != nil {
			t.Fatal(err)
		}
	}
	stranger := auth.Principal{Tenant: "acme", Subject: "bob", Kind: auth.User, Roles: []auth.Role{auth.Operator}}
	count := func() (int, bool) {
		h, err := p.AgentEvaluationHealth(stranger, BuiltinBuilder)
		if err != nil {
			t.Fatal(err)
		}
		return h.Flagged, h.FlaggedMore
	}
	save("public-run", "bob")
	if n, more := count(); n != 1 || more {
		t.Fatalf("before: flagged = %d, more = %v", n, more)
	}
	for i := range state.MaxList {
		save("private-run", fmt.Sprintf("rater-%d", i))
	}
	if n, more := count(); n != 1 || more {
		t.Fatalf("after a thousand hidden candidates: flagged = %d, more = %v", n, more)
	}
}
