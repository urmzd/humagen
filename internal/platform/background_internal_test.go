package platform

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/runtime"
	"github.com/urmzd/saige/flow/turn/native"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/background"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/join"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

// backgroundPlatform runs a platform with one offline agent, a project
// "Field" that ada owns, and a turn ada had with the agent there. It returns
// the turn's run and the request its tools are given.
func backgroundPlatform(t *testing.T) (*Platform, runs.Run, runtime.Request) {
	t.Helper()
	return backgroundPlatformWith(t, nil)
}

// backgroundPlatformWith is backgroundPlatform under a workspace policy.
func backgroundPlatformWith(t *testing.T, pol *auth.Policy) (*Platform, runs.Run, runtime.Request) {
	t.Helper()
	p, err := New(Config{Workspace: t.TempDir(), Policy: pol, Router: func(string) join.Asker { return join.Words }})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("platform stopped with %v", err)
		}
	})
	if _, err := p.Agents.Create(ctx, "acme", wire.AgentSpec{Name: "lead", Model: "offline", Provider: "offline", SystemPrompt: "Lead."}, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.CreateProject(ctx, "acme", projects.Writer{By: "ada"}, ProjectInput{Name: "Field"}); err != nil {
		t.Fatal(err)
	}
	run, err := p.Converse(ctx, "acme", Turn{ID: "api-bg", Agent: "lead", Text: "Tell me when the deploy is green", Project: "Field", Actor: event.Actor{User: "ada"}})
	if err != nil {
		t.Fatal(err)
	}
	run = awaitEnd(t, p, run.ID)
	return p, run, runtime.Request{RunID: run.ID, Tenant: "acme", Definition: native.Revision{Spec: native.Spec{Name: "lead"}, Version: wire.VersionOf(1)}}
}

func awaitEnd(t *testing.T, p *Platform, id string) runs.Run {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		r, err := p.Runs.Get("acme", id)
		if err == nil && r.Status.Terminal() {
			return r
		}
		if time.Now().After(deadline) {
			t.Fatalf("run %s did not end: %q %v", id, r.Status, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// awaitState waits for a background task to leave Active, and returns it.
func awaitState(t *testing.T, p *Platform, id string) background.Item {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		it, err := p.Background.Get("acme", id)
		if err == nil && it.State != background.Active {
			return it
		}
		if time.Now().After(deadline) {
			t.Fatalf("background task %s did not end: %+v %v", id, it, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func useBackground(t *testing.T, p *Platform, req runtime.Request, name string, args map[string]any) (BackgroundView, error) {
	t.Helper()
	out, _, err := p.UseTool(t.Context(), req, name, args)
	var v BackgroundView
	if err == nil {
		if err := json.Unmarshal([]byte(out), &v); err != nil {
			t.Fatalf("%s answered %s: %v", name, out, err)
		}
	}
	return v, err
}

// notices are the results in ada's inbox linked to a conversation, once
// there is at least one: a task's notice is written just after it ends, in
// the same completion step, so it is waited for.
func notices(p *Platform, thread string) []inbox.Message {
	deadline := time.Now().Add(10 * time.Second)
	for {
		page, _ := p.Inbox.Snapshot("acme")
		var out []inbox.Message
		for _, m := range page {
			if m.Kind == inbox.KindResult && m.To == "ada" && m.Source.Thread == thread {
				out = append(out, m)
			}
		}
		if len(out) > 0 || time.Now().After(deadline) {
			return out
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A watch is checked in the background, in a private session of its own,
// and what the check found reaches the person's inbox once, linked to the
// conversation that asked.
func TestWatchChecksInTheBackgroundAndTellsThePerson(t *testing.T) {
	p, asked, req := backgroundPlatform(t)
	v, err := useBackground(t, p, req, ToolWatchStart, map[string]any{"condition": "the deploy is green", "how": "read the workflow status", "every": "10m", "for": "2h"})
	if err != nil {
		t.Fatal(err)
	}
	if v.Kind != "watch" || v.State != "active" || v.Every != "10m0s" || v.Session != asked.Thread {
		t.Fatalf("watch_start: %+v", v)
	}
	p.checkBackground(t.Context())
	it, err := p.Background.Get("acme", v.ID)
	if err != nil || it.Thread == "" || it.Runs != 1 {
		t.Fatalf("the first check did not start: %+v %v", it, err)
	}
	check := awaitEnd(t, p, it.Running)
	if check.Thread != it.Thread || check.Project != "Field" || personOf(check.Actor) != "ada" {
		t.Fatalf("the check ran in the wrong place or for the wrong person: %+v", check)
	}
	th, err := p.ThreadRecord("acme", it.Thread)
	if err != nil || th.Visibility != threads.Private || th.Session == nil || th.Session.Parent != "watch://"+it.ID {
		t.Fatalf("the checks' session: %+v %v", th, err)
	}
	// The offline model answers something other than NONE: it happened.
	it = awaitState(t, p, v.ID)
	if it.State != background.Done || it.Result == "" {
		t.Fatalf("the watch did not end as done: %+v", it)
	}
	if got := notices(p, asked.Thread); len(got) != 1 {
		t.Fatalf("want one notice about the watch, got %+v", got)
	}
	p.checkBackground(t.Context())
	if again, _ := p.Background.Get("acme", v.ID); again.Runs != 1 {
		t.Fatalf("an ended watch ran again: %+v", again)
	}
}

// A goal's turns run in the session that set it, one after another, and the
// person is told when its turns run out.
func TestGoalKeepsTakingTurnsInItsSession(t *testing.T) {
	p, asked, req := backgroundPlatform(t)
	v, err := useBackground(t, p, req, ToolGoalStart, map[string]any{"goal": "every test passes", "done_when": "go test ./... passes", "turns": "2"})
	if err != nil {
		t.Fatal(err)
	}
	if v.Kind != "goal" || !v.Working || v.Runs != 1 || v.Limit != 2 || v.Thread != asked.Thread {
		t.Fatalf("goal_start: %+v", v)
	}
	// The turn that set the goal ended before the goal was kept, as a tool
	// called after the fact; its end is settled as a completion would.
	if err := p.settleBackgroundRun(t.Context(), asked); err != nil {
		t.Fatal(err)
	}
	it := awaitState(t, p, v.ID)
	if it.State != background.Expired || it.Runs != 2 || it.Last == "" {
		t.Fatalf("two turns without GOAL MET: %+v", it)
	}
	turn, err := p.Runs.Get("acme", it.Last)
	if err != nil || turn.Thread != asked.Thread || personOf(turn.Actor) != "ada" || wire.Meta(turn.Tag())[metaBackground] != it.ID {
		t.Fatalf("the second turn was not in the goal's session for ada: %+v %v", turn, err)
	}
	got := notices(p, asked.Thread)
	if len(got) != 1 || !strings.HasPrefix(got[0].Subject, "Goal not met") {
		t.Fatalf("want one goal notice, got %+v", got)
	}
}

func TestGoalOutcomes(t *testing.T) {
	done := runs.Run{Status: runs.Completed}
	for answer, want := range map[string]background.Outcome{
		"Fixed the last test.\nGOAL MET":                {Done: true, Result: "Fixed the last test."},
		"All green.\n**Goal met.**":                     {Done: true, Result: "All green."},
		"Tried twice.\nGOAL BLOCKED: no access to repo": {Blocked: true, Result: "no access to repo\n\nTried twice."},
		"Fixed two of five.":                            {Result: "Fixed two of five."},
	} {
		done.Answer = answer
		if got := backgroundOutcome(background.Goal, done); got != want {
			t.Errorf("%q: got %+v, want %+v", answer, got, want)
		}
	}
	if got := backgroundOutcome(background.Watch, runs.Run{Status: runs.Completed, Answer: "None."}); got.Done {
		t.Errorf("a watch's NONE is done: %+v", got)
	}
	if got := backgroundOutcome(background.Task, runs.Run{Status: runs.Failed}); !got.Failed {
		t.Errorf("a failed task run: %+v", got)
	}
}

// A task runs once in a session of its own, and its answer reaches the
// person linked to that session, so they can open the work.
func TestTaskRunsInItsOwnSessionAndTellsThePerson(t *testing.T) {
	p, asked, req := backgroundPlatform(t)
	v, err := useBackground(t, p, req, ToolTaskStart, map[string]any{"ask": "find every caller of Open"})
	if err != nil {
		t.Fatal(err)
	}
	p.checkBackground(t.Context())
	it := awaitState(t, p, v.ID)
	if it.State != background.Done || it.Runs != 1 || it.Thread == "" || it.Thread == asked.Thread {
		t.Fatalf("task: %+v", it)
	}
	if got := notices(p, it.Thread); len(got) != 1 || !strings.HasPrefix(got[0].Subject, "Done: ") {
		t.Fatalf("want one notice linked to the task's session, got %+v", got)
	}
}

// Stopping tells nobody; another person cannot stop it; background work does
// not start more of itself; limits hold.
func TestBackgroundStopAndBounds(t *testing.T) {
	p, _, req := backgroundPlatform(t)
	v, err := useBackground(t, p, req, ToolWatchStart, map[string]any{"condition": "the build passes"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.StopBackground(auth.Principal{Tenant: "acme", Subject: "bo"}, v.ID); !errors.Is(err, background.ErrNotFound) {
		t.Fatalf("another person stops ada's watch: %v", err)
	}
	stopped, err := useBackground(t, p, req, ToolBackgroundStop, map[string]any{"id": v.ID})
	if err != nil || stopped.State != "stopped" {
		t.Fatalf("background_stop: %+v %v", stopped, err)
	}
	list, err := p.ListBackground(p.byPolicy("acme", "ada"))
	if err != nil || len(list) != 1 || list[0].ID != v.ID {
		t.Fatalf("list: %+v %v", list, err)
	}
	if mine, _ := p.ListBackground(p.byPolicy("acme", "bo")); len(mine) != 0 {
		t.Fatalf("bo sees ada's work: %+v", mine)
	}
	if _, err := useBackground(t, p, req, ToolWatchStart, map[string]any{"condition": "x", "every": "1m"}); !errors.Is(err, background.ErrInvalid) {
		t.Fatalf("a check every minute: %v", err)
	}

	task, _ := useBackground(t, p, req, ToolTaskStart, map[string]any{"ask": "search"})
	p.checkBackground(t.Context())
	it := awaitState(t, p, task.ID)
	inner := req
	inner.RunID = it.Last
	if _, err := useBackground(t, p, inner, ToolTaskStart, map[string]any{"ask": "more"}); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("a background run started more background work: %v", err)
	}
}

// notify lands in the person's inbox once per distinct message, and a run
// that acts for nobody may not send one.
func TestNotify(t *testing.T) {
	p, _, req := backgroundPlatform(t)
	for range 2 {
		if _, _, err := p.UseTool(t.Context(), req, ToolNotify, map[string]any{"subject": "Report is ready", "text": "It is in the Field project."}); err != nil {
			t.Fatal(err)
		}
	}
	page, _ := p.Inbox.Snapshot("acme")
	n := 0
	for _, m := range page {
		if m.Subject == "Report is ready" && m.To == "ada" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("want the notice once, got %d", n)
	}
	nobody, err := p.Converse(t.Context(), "acme", Turn{ID: "api-nobody", Agent: "lead", Text: "hi", Project: "Field"})
	if err != nil {
		t.Fatal(err)
	}
	awaitEnd(t, p, nobody.ID)
	req.RunID = nobody.ID
	if _, _, err := p.UseTool(t.Context(), req, ToolNotify, map[string]any{"subject": "x"}); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("a run for nobody notified: %v", err)
	}
}
