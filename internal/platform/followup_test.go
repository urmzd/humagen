package platform

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/saige/flow/runtime"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/join"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

// followupModel classifies every follow-up as it says, with a reason, and
// lets no agent join on its own.
type followupModel string

func (followupModel) Ask(context.Context, join.Candidate, string) (join.Decision, error) {
	return join.Decision{Reason: "Not mine."}, nil
}

func (m followupModel) Followup(context.Context, string, string) (join.Followup, error) {
	return join.Followup{Outcome: string(m), Reason: "The person changed the request."}, nil
}

// followupBench is a platform whose runs work until the test ends them, so a
// message can be posted while one does. Nothing runs them, and asking one
// to stop is only recorded.
type followupBench struct {
	p        *Platform
	mu       sync.Mutex
	stops    []runtime.StopRequest
	ana, bo  auth.Principal
	thread   string
	first    runs.Run
	ctx      context.Context
	tenantID string
}

func newFollowupBench(t *testing.T, router join.Asker) *followupBench {
	t.Helper()
	b := &followupBench{ctx: t.Context(), tenantID: "acme"}
	p, err := New(Config{
		Workspace: t.TempDir(),
		Router:    func(string) join.Asker { return router },
		Stops: func(_ context.Context, req runtime.StopRequest) error {
			b.mu.Lock()
			defer b.mu.Unlock()
			b.stops = append(b.stops, req)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	p.asked = nil
	b.p = p
	if _, err := p.Agents.Create(b.ctx, b.tenantID, wire.AgentSpec{Name: "helper", Model: "offline", Provider: "offline", SystemPrompt: "Help."}, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.CreateProject(b.ctx, b.tenantID, projects.Writer{By: "ada"}, ProjectInput{Name: "Field"}); err != nil {
		t.Fatal(err)
	}
	b.members(t, "ana", "bo")
	b.ana = auth.Principal{Tenant: b.tenantID, Subject: "ana", Name: "Ana", Kind: auth.User, Roles: []auth.Role{auth.Operator}}
	b.bo = auth.Principal{Tenant: b.tenantID, Subject: "bo", Name: "Bo", Kind: auth.User, Roles: []auth.Role{auth.Operator}}
	// They have signed in, so the directory holds their roles.
	p.SeePerson(b.ana)
	p.SeePerson(b.bo)
	started, err := p.StartThread(b.ctx, b.ana, ThreadInput{Project: "Field", Visibility: threads.Shared, Text: "@helper build the report"})
	if err != nil || len(started.Runs) != 1 {
		t.Fatalf("start = %+v, %v", started, err)
	}
	b.thread, b.first = started.Thread.ID, started.Runs[0]
	return b
}

// members makes exactly these people members of Field.
func (b *followupBench) members(t *testing.T, who ...string) {
	t.Helper()
	pr, _ := b.p.Projects.Tree(b.tenantID).Get("Field")
	entries := []projects.Entry{{Who: "ada", Role: projects.Owner}}
	for _, w := range who {
		entries = append(entries, projects.Entry{Who: w, Role: projects.Member})
	}
	if _, err := b.p.Projects.Update(b.tenantID, "Field", pr.Version, projects.Writer{By: "ada"}, time.Now(), func(pr *projects.Project) error {
		pr.Members = entries
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func (b *followupBench) post(t *testing.T, who auth.Principal, text string) Posted {
	t.Helper()
	out, err := b.p.PostMessage(b.ctx, who, b.thread, MessageInput{Text: text})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// end ends a run as its own events would, and acts on it ending.
func (b *followupBench) end(t *testing.T, id string) {
	t.Helper()
	run, ended := b.p.over(func() (runs.Run, bool) {
		cur, err := b.p.Runs.Get(b.tenantID, id)
		if err != nil {
			t.Fatal(err)
		}
		if cur.Stopping {
			return b.p.Runs.Stop(b.tenantID, id, "partial", "asked", b.p.now())
		}
		return b.p.Runs.End(b.tenantID, id, runs.Completed, "done", "", b.p.now())
	})
	if !ended {
		t.Fatalf("run %s did not end", id)
	}
	b.p.ended(b.ctx, run)
}

func (b *followupBench) turns(t *testing.T) []runs.Run {
	t.Helper()
	rr, err := b.p.Runs.Conversation(b.tenantID, b.thread)
	if err != nil {
		t.Fatal(err)
	}
	return rr
}

func (b *followupBench) message(t *testing.T, id string) threads.Message {
	t.Helper()
	m, err := b.p.Threads.Message(b.tenantID, b.thread, id)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func (b *followupBench) stopped() []runtime.StopRequest {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.stops)
}

// A message posted while a run works is queued: it starts nothing and stops
// nothing, and what it asks starts once the run has ended, exactly once,
// however often the ending is acted on or the release retried.
func TestAQueuedFollowupStartsOnceTheRunSettles(t *testing.T) {
	b := newFollowupBench(t, followupModel(join.FollowupQueue))
	posted := b.post(t, b.ana, "@helper and add the appendix")
	f := posted.Message.Followup
	if f == nil || f.Outcome != threads.FollowupQueue || f.Handled || !slices.Equal(f.Waiting, []string{b.first.ID}) {
		t.Fatalf("follow-up = %+v", f)
	}
	if len(posted.Runs) != 0 || posted.Rule != threads.RuleFollowup || len(b.stopped()) != 0 {
		t.Fatalf("posted = %+v, stops %+v", posted, b.stopped())
	}
	if again := b.p.releaseFollowups(b.ctx, b.tenantID, b.thread); again != nil || len(b.turns(t)) != 1 {
		t.Fatalf("released while the run works: %v, %d turns", again, len(b.turns(t)))
	}
	b.end(t, b.first.ID)
	rr := b.turns(t)
	if len(rr) != 2 || rr[1].Message != posted.Message.ID || rr[1].Text != "@helper and add the appendix" || rr[1].Agent != "helper" {
		t.Fatalf("turns = %+v", rr)
	}
	if m := b.message(t, posted.Message.ID); !m.Followup.Handled || m.Followup.Outcome != threads.FollowupQueue {
		t.Fatalf("message = %+v", m.Followup)
	}
	// A repeated completion, a repeated release, and a start retried after
	// a crash before the message was marked all join the run it started.
	first, _ := b.p.Runs.Get(b.tenantID, b.first.ID)
	b.p.ended(b.ctx, first)
	if err := b.p.releaseFollowups(b.ctx, b.tenantID, b.thread); err != nil {
		t.Fatal(err)
	}
	th, _ := b.p.Threads.Get(b.tenantID, b.thread)
	again, err := b.p.startFollowup(b.ctx, b.tenantID, th, b.message(t, posted.Message.ID))
	if err != nil || len(again) != 1 || again[0].ID != rr[1].ID {
		t.Fatalf("a retried start = %+v, %v", again, err)
	}
	if n := len(b.turns(t)); n != 2 {
		t.Fatalf("%d turns after repeats, want 2", n)
	}
}

// A replace asks the working run to stop, with its reason, and starts what
// it asks once the run has stopped. A verb first forces it whatever the
// model says, and is left out of what runs.
func TestAReplaceStopsTheRunThenStarts(t *testing.T) {
	for _, tc := range []struct {
		name, model, text, cause, runs string
	}{
		{"the model", join.FollowupReplace, "@helper the quarterly one instead", "replace: The person changed the request.", "@helper the quarterly one instead"},
		{"a verb", join.FollowupQueue, "@helper replace: the quarterly one", "replace: the quarterly one", "@helper the quarterly one"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := newFollowupBench(t, followupModel(tc.model))
			posted := b.post(t, b.ana, tc.text)
			if f := posted.Message.Followup; f == nil || f.Outcome != threads.FollowupReplace || f.Handled || len(posted.Runs) != 0 {
				t.Fatalf("posted = %+v", posted)
			}
			stops := b.stopped()
			if len(stops) != 1 || stops[0].RunID != b.first.ID || stops[0].Reason != tc.cause || stops[0].By != "ana" {
				t.Fatalf("stops = %+v", stops)
			}
			if r, _ := b.p.Runs.Get(b.tenantID, b.first.ID); !r.Stopping || len(b.turns(t)) != 1 {
				t.Fatalf("before it stopped: %s, %d turns", r.Status, len(b.turns(t)))
			}
			b.end(t, b.first.ID)
			rr := b.turns(t)
			if len(rr) != 2 || rr[0].Status != runs.Stopped || rr[1].Text != tc.runs || rr[1].Message != posted.Message.ID {
				t.Fatalf("turns = %+v", rr)
			}
		})
	}
}

// A stop asks the working run to stop with its reason, starts nothing, then
// or after, and the reason stays on the message.
func TestAStopStopsTheRunAndStartsNothing(t *testing.T) {
	for _, tc := range []struct {
		name, model, text, reason string
	}{
		{"the model", join.FollowupStop, "@helper hold on", "The person changed the request."},
		{"a verb", join.FollowupQueue, "stop: wrong repository", "wrong repository"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := newFollowupBench(t, followupModel(tc.model))
			posted := b.post(t, b.ana, tc.text)
			f := posted.Message.Followup
			if f == nil || f.Outcome != threads.FollowupStop || !f.Handled || f.Reason != tc.reason || len(posted.Runs) != 0 {
				t.Fatalf("posted = %+v", posted)
			}
			if stops := b.stopped(); len(stops) != 1 || stops[0].Reason != "stop: "+tc.reason {
				t.Fatalf("stops = %+v", stops)
			}
			b.end(t, b.first.ID)
			if rr := b.turns(t); len(rr) != 1 || rr[0].Status != runs.Stopped {
				t.Fatalf("turns = %+v", rr)
			}
			if m := b.message(t, posted.Message.ID); m.Followup.Reason != tc.reason || m.Followup.Outcome != threads.FollowupStop {
				t.Fatalf("message = %+v", m.Followup)
			}
		})
	}
}

// With no model to ask, a follow-up queues.
func TestWithNoModelAFollowupQueues(t *testing.T) {
	b := newFollowupBench(t, join.Words)
	posted := b.post(t, b.ana, "@helper hold on")
	if f := posted.Message.Followup; f == nil || f.Outcome != threads.FollowupQueue || len(b.stopped()) != 0 {
		t.Fatalf("posted = %+v", posted)
	}
}

// Queued messages run one after another, in the order they were posted.
func TestQueuedFollowupsRunInTurn(t *testing.T) {
	b := newFollowupBench(t, followupModel(join.FollowupQueue))
	second := b.post(t, b.ana, "@helper and the appendix")
	third := b.post(t, b.bo, "@helper and the index")
	if second.Message.Followup == nil || third.Message.Followup == nil {
		t.Fatalf("follow-ups = %+v, %+v", second.Message.Followup, third.Message.Followup)
	}
	b.end(t, b.first.ID)
	rr := b.turns(t)
	if len(rr) != 2 || rr[1].Message != second.Message.ID {
		t.Fatalf("after the first ended = %+v", rr)
	}
	if m := b.message(t, third.Message.ID); m.Followup.Handled {
		t.Fatal("the third ran beside the second")
	}
	b.end(t, rr[1].ID)
	rr = b.turns(t)
	if len(rr) != 3 || rr[2].Message != third.Message.ID || rr[2].Actor.User != "bo" {
		t.Fatalf("after the second ended = %+v", rr)
	}
}

// Someone who may not stop the working run does not interrupt it: their
// message is no follow-up, and a follow-up's stop is checked again as it is
// applied.
func TestAPosterWhoMayNotStopTheRunCannotInterruptIt(t *testing.T) {
	b := newFollowupBench(t, followupModel(join.FollowupStop))
	vic := auth.Principal{Tenant: b.tenantID, Subject: "vic", Name: "Vic", Kind: auth.User, Roles: []auth.Role{auth.Operator}}
	th, _ := b.p.Threads.Get(b.tenantID, b.thread)
	if f, err := b.p.followupOf(b.ctx, vic, th, "stop"); err != nil || f != nil {
		t.Fatalf("a follow-up for someone who may not stop the run = %+v, %v", f, err)
	}
	m := threads.Message{ID: "m-9", By: "vic", Followup: &threads.Followup{Outcome: threads.FollowupStop, Waiting: []string{b.first.ID}, Handled: true}}
	if _, err := b.p.followThrough(b.ctx, vic, b.p.Projects.Tree(b.tenantID), th, m, true); err != nil {
		t.Fatal(err)
	}
	if stops := b.stopped(); len(stops) != 0 {
		t.Fatalf("stops = %+v", stops)
	}
	if r, _ := b.p.Runs.Get(b.tenantID, b.first.ID); r.Status.Terminal() || r.Stopping {
		t.Fatalf("the run = %s", r.Status)
	}
}

// What a queued message asks starts for its poster as they stand when it is
// released: one no longer a member starts nothing, and the message is
// settled all the same.
func TestAccessRevokedBeforeReleaseStartsNothing(t *testing.T) {
	b := newFollowupBench(t, followupModel(join.FollowupQueue))
	posted := b.post(t, b.bo, "@helper and the appendix")
	if posted.Message.Followup == nil {
		t.Fatalf("posted = %+v", posted)
	}
	b.members(t, "ana")
	b.end(t, b.first.ID)
	if rr := b.turns(t); len(rr) != 1 {
		t.Fatalf("turns = %+v", rr)
	}
	if m := b.message(t, posted.Message.ID); !m.Followup.Handled {
		t.Fatalf("message = %+v", m.Followup)
	}
	if pending, _ := b.p.Threads.Pending(b.tenantID, b.thread, 0); len(pending) != 0 {
		t.Fatalf("pending = %+v", pending)
	}
}
