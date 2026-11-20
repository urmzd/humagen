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
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/join"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/humagen/pkg/watches"
)

// watchPlatform runs a platform with one offline agent, a project "Field"
// that ada owns, and a turn ada had with the agent there. It returns the
// request the turn's tools are given.
func watchPlatform(t *testing.T) (*Platform, runtime.Request) {
	t.Helper()
	p, err := New(Config{Workspace: t.TempDir(), Router: func(string) join.Asker { return join.Words }})
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
	run, err := p.Converse(ctx, "acme", Turn{ID: "api-watch", Agent: "lead", Text: "Tell me when the deploy is green", Project: "Field", Actor: event.Actor{User: "ada"}})
	if err != nil {
		t.Fatal(err)
	}
	awaitWatchRun(t, p, run.ID)
	return p, runtime.Request{RunID: run.ID, Tenant: "acme", Definition: native.Revision{Spec: native.Spec{Name: "lead"}, Version: wire.VersionOf(1)}}
}

func awaitWatchRun(t *testing.T, p *Platform, id string) runs.Run {
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

func useWatchTool(t *testing.T, p *Platform, req runtime.Request, name string, args map[string]any) (string, error) {
	t.Helper()
	out, _, err := p.UseTool(t.Context(), req, name, args)
	return out, err
}

// A watch started in a turn is checked in the background, in a private
// conversation of its own, and what the check found reaches the person's
// inbox once, with a link to the conversation that asked.
func TestWatchChecksInTheBackgroundAndTellsThePerson(t *testing.T) {
	p, req := watchPlatform(t)
	out, err := useWatchTool(t, p, req, ToolWatchStart, map[string]any{"condition": "the deploy is green", "how": "read the workflow status", "every": "10m", "for": "2h"})
	if err != nil {
		t.Fatal(err)
	}
	var v watchView
	if err := json.Unmarshal([]byte(out), &v); err != nil || v.ID == "" || v.State != "watching" || v.Every != "10m0s" {
		t.Fatalf("watch_start: %s %v", out, err)
	}
	list, err := useWatchTool(t, p, req, ToolWatchList, nil)
	if err != nil || !strings.Contains(list, v.ID) {
		t.Fatalf("watch_list: %s %v", list, err)
	}

	p.checkWatches(t.Context())
	w, err := p.Watches.Get("acme", v.ID)
	if err != nil || w.Running == "" || w.Thread == "" || w.Checks != 1 {
		t.Fatalf("the first check did not start: %+v %v", w, err)
	}
	check := awaitWatchRun(t, p, w.Running)
	if check.Thread != w.Thread || check.Project != "Field" || personOf(check.Actor) != "ada" {
		t.Fatalf("the check ran in the wrong place or for the wrong person: %+v", check)
	}
	th, err := p.ThreadRecord("acme", w.Thread)
	if err != nil || th.Visibility != threads.Private || th.Session == nil || th.Session.Parent != "watch://"+w.ID {
		t.Fatalf("the checks' conversation: %+v %v", th, err)
	}

	// The offline model answers something other than NONE, which is
	// the condition holding.
	deadline := time.Now().Add(10 * time.Second)
	for w.State == watches.Watching && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		w, _ = p.Watches.Get("acme", v.ID)
	}
	if w.State != watches.Met || w.Result == "" {
		t.Fatalf("the watch did not end as met: %+v", w)
	}
	page, _ := p.Inbox.Snapshot("acme")
	found := 0
	for _, m := range page {
		if m.Kind == inbox.KindResult && m.To == "ada" && m.Source.Thread == v.Session {
			found++
		}
	}
	if found != 1 {
		t.Fatalf("want one message about the watch in ada's inbox, got %d: %+v", found, page)
	}

	// The sweep after it starts nothing more.
	p.checkWatches(t.Context())
	if again, _ := p.Watches.Get("acme", v.ID); again.Checks != 1 {
		t.Fatalf("an ended watch was checked again: %+v", again)
	}
}

// A check that finds nothing schedules the next; one that runs out of time
// tells the person; one stopped tells nobody.
func TestWatchSettles(t *testing.T) {
	p, req := watchPlatform(t)
	out, err := useWatchTool(t, p, req, ToolWatchStart, map[string]any{"condition": "the queue is empty", "every": "1h", "for": "1h"})
	if err != nil {
		t.Fatal(err)
	}
	var v watchView
	_ = json.Unmarshal([]byte(out), &v)
	run := runs.Run{ID: "r-none", Tenant: "acme", Origin: runs.OriginAPI, Status: runs.Completed, Answer: "None."}
	if _, err := p.Watches.Begin("acme", v.ID, run.ID, p.now()); err != nil {
		t.Fatal(err)
	}
	if err := p.settleWatch(run, v.ID); err != nil {
		t.Fatal(err)
	}
	w, _ := p.Watches.Get("acme", v.ID)
	if w.State != watches.Expired {
		t.Fatalf("one hour at one hour ends after one check: %+v", w)
	}

	out, _ = useWatchTool(t, p, req, ToolWatchStart, map[string]any{"condition": "the build passes"})
	_ = json.Unmarshal([]byte(out), &v)
	if _, err := useWatchTool(t, p, req, ToolWatchStop, map[string]any{"id": v.ID}); err != nil {
		t.Fatal(err)
	}
	if w, _ := p.Watches.Get("acme", v.ID); w.State != watches.Stopped {
		t.Fatalf("watch_stop: %+v", w)
	}
	if _, err := p.StopWatch(auth.Principal{Tenant: "acme", Subject: "bo"}, v.ID); !errors.Is(err, watches.ErrNotFound) {
		t.Fatalf("another person stops ada's watch: %v", err)
	}
	if _, err := useWatchTool(t, p, req, ToolWatchStart, map[string]any{"condition": "x", "every": "1m"}); !errors.Is(err, watches.ErrInvalid) {
		t.Fatalf("a check every minute: %v", err)
	}
}

// notify lands in the person's inbox once per distinct message, and a run
// that acts for nobody may not send one.
func TestNotify(t *testing.T) {
	p, req := watchPlatform(t)
	for range 2 {
		if _, err := useWatchTool(t, p, req, ToolNotify, map[string]any{"subject": "Report is ready", "text": "It is in the Field project."}); err != nil {
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
	awaitWatchRun(t, p, nobody.ID)
	req.RunID = nobody.ID
	if _, err := useWatchTool(t, p, req, ToolNotify, map[string]any{"subject": "x"}); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("a run for nobody notified: %v", err)
	}
}
