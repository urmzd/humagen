package platform

import (
	"testing"
	"time"

	"github.com/urmzd/saige/flow/runtime"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/runs"
)

// slackPlatform is backgroundPlatform where ada is an operator, so a Slack
// app may post and read for her.
func slackPlatform(t *testing.T) (*Platform, runs.Run, runtime.Request) {
	t.Helper()
	pol, err := auth.ParsePolicy("test", `
role viewer { allow read on * }
role operator extends viewer { allow write on runs }
grant operator to subject "ada"
`)
	if err != nil {
		t.Fatal(err)
	}
	return backgroundPlatformWith(t, pol)
}

// slackReporting adds to a background platform the Slack client "app", which
// maps U0ADA to ada, and returns its machine.
func slackReporting(t *testing.T, p *Platform, id, machine string) auth.Principal {
	t.Helper()
	_, err := p.CreateClient(t.Context(), "acme", "test", ClientInput{
		Client: clients.Client{
			ID: id, Name: id, Surface: clients.Slack, Enabled: true, DefaultAgent: "lead",
			Identity: clients.Identity{AppID: "A0" + id, Team: "T0TEAM"},
			Slack:    &clients.SlackOptions{Sessions: true, Machine: machine, People: map[string]string{"U0ADA": "ada"}},
			Home:     &clients.Home{Project: "Field"},
		},
		Token:         clients.SecretInput{Value: "token-" + id},
		SigningSecret: clients.SecretInput{Value: "secret-" + id},
	})
	if err != nil {
		t.Fatal(err)
	}
	who := p.byPolicy("acme", machine)
	who.Kind, who.Scopes = auth.Machine, []auth.Permission{auth.RunsRead, auth.RunsWrite}
	return who
}

// askedInSlack is a turn ada had with lead in the app's Slack thread
// C0PLAUD/1700000000.000100, and the request its tools are given.
func askedInSlack(t *testing.T, p *Platform, machine auth.Principal, req runtime.Request) (runs.Run, runtime.Request) {
	t.Helper()
	got, err := p.StartThread(t.Context(), machine, ThreadInput{
		Text: "tell me when the Plaud outage is over", ActAs: &ActAs{Client: "app", User: "U0ADA"},
		Surface: &SurfaceRef{Client: "app", Channel: "C0PLAUD", TS: "1700000000.000100", AppUser: "UAPP"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Runs) != 1 || got.Thread.Surface == nil {
		t.Fatalf("the Slack message started %+v in %+v", got.Runs, got.Thread)
	}
	run := awaitEnd(t, p, got.Runs[0].ID)
	req.RunID = run.ID
	return run, req
}

// heardFor waits for the result a background task sent ada, and returns what
// a machine hears of it.
func heardFor(t *testing.T, p *Platform, machine auth.Principal, key string) []SurfaceNotice {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if m, ok := p.Inbox.ByKey("acme", key); ok {
			return p.SurfaceNoticesFor(t.Context(), machine, inbox.Notice{ID: m.ID, Kind: m.Kind, State: m.State})
		}
		if time.Now().After(deadline) {
			t.Fatalf("no result under %q", key)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A watch asked in the app's Slack thread reports there, when it fires and
// when it runs out: the machine hears of it for ada, with the thread to
// reply in and the checks' session to open. One stopped tells nobody.
func TestBackgroundWorkAskedInSlackRepliesInTheThread(t *testing.T) {
	p, _, req := slackPlatform(t)
	machine := slackReporting(t, p, "app", "svc-slack")
	asked, req := askedInSlack(t, p, machine, req)

	v, err := useBackground(t, p, req, ToolWatchStart, map[string]any{"condition": "the Plaud outage is over", "every": "10m", "for": "2h"})
	if err != nil {
		t.Fatal(err)
	}
	if v.Session != asked.Thread {
		t.Fatalf("the watch was asked in %s, not the Slack thread %s", v.Session, asked.Thread)
	}
	p.checkBackground(t.Context())
	it := awaitState(t, p, v.ID)
	heard := heardFor(t, p, machine, keyBackground+it.ID)
	want := SurfaceReply{Channel: "C0PLAUD", TS: "1700000000.000100", Session: it.Thread}
	if len(heard) != 1 || heard[0].For != (ActAs{Client: "app", User: "U0ADA"}) || heard[0].Reply == nil || *heard[0].Reply != want {
		t.Fatalf("the machine hears of the fired watch as %+v, want a reply in %+v", heard, want)
	}

	// Running out of time reports the same way.
	out, err := useBackground(t, p, req, ToolWatchStart, map[string]any{"condition": "the status page is green"})
	if err != nil {
		t.Fatal(err)
	}
	expired, err := p.Background.Expire("acme", out.ID, time.Now().Add(7*24*time.Hour))
	if err := p.backgroundEnded("acme", expired, err); err != nil {
		t.Fatal(err)
	}
	heard = heardFor(t, p, machine, keyBackground+out.ID)
	if m, _ := p.Inbox.ByKey("acme", keyBackground+out.ID); m.Subject != "Stopped watching: the status page is green" {
		t.Fatalf("the expired watch says %q", m.Subject)
	}
	if len(heard) != 1 || heard[0].Reply == nil || heard[0].Reply.Channel != "C0PLAUD" {
		t.Fatalf("the machine hears of the expired watch as %+v", heard)
	}

	// A watch a person stopped tells nobody, in Slack or anywhere.
	stop, err := useBackground(t, p, req, ToolWatchStart, map[string]any{"condition": "the queue drains"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.StopBackground(p.byPolicy("acme", "ada"), stop.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := p.Inbox.ByKey("acme", keyBackground+stop.ID); ok {
		t.Fatal("a stopped watch told ada")
	}

	// notify from the Slack thread's own turn replies there too, with no
	// other session to open.
	if _, _, err := p.UseTool(t.Context(), req, ToolNotify, map[string]any{"subject": "Plaud is back"}); err != nil {
		t.Fatal(err)
	}
	var key string
	page, _ := p.Inbox.Snapshot("acme")
	for _, m := range page {
		if m.Subject == "Plaud is back" {
			key = m.Key
		}
	}
	heard = heardFor(t, p, machine, key)
	if len(heard) != 1 || heard[0].Reply == nil || *heard[0].Reply != (SurfaceReply{Channel: "C0PLAUD", TS: "1700000000.000100"}) {
		t.Fatalf("the machine hears of notify as %+v", heard)
	}
}

// What a run that read private context found never reaches a shared Slack
// thread: the machine hears of it for ada with no thread to reply in, so she
// is told in her direct messages, as a schedule's private answer is kept for
// its creator.
func TestABackgroundResultFromPrivateContextIsSentToThePersonAlone(t *testing.T) {
	p, _, req := slackPlatform(t)
	machine := slackReporting(t, p, "app", "svc-slack")
	asked, req := askedInSlack(t, p, machine, req)
	if err := p.ContextFlags.Mark("acme", asked.ID); err != nil {
		t.Fatal(err)
	}
	v, err := useBackground(t, p, req, ToolTaskStart, map[string]any{"ask": "read my mail for the Plaud invoice"})
	if err != nil {
		t.Fatal(err)
	}
	p.checkBackground(t.Context())
	it := awaitState(t, p, v.ID)
	heard := heardFor(t, p, machine, keyBackground+it.ID)
	if len(heard) != 1 || heard[0].For != (ActAs{Client: "app", User: "U0ADA"}) || heard[0].Reply != nil {
		t.Fatalf("the machine hears of a private task as %+v, want ada's direct messages", heard)
	}
}

// Work asked anywhere but a Slack thread reaches ada in her direct messages
// through every client that maps her; work asked in one client's Slack
// thread is reported by that client alone; and a result that background
// work did not send is not forwarded at all.
func TestBackgroundWorkAskedElsewhereIsSentToTheirDirectMessages(t *testing.T) {
	p, asked, req := slackPlatform(t)
	machine := slackReporting(t, p, "app", "svc-slack")
	other := slackReporting(t, p, "other", "svc-other")
	v, err := useBackground(t, p, req, ToolTaskStart, map[string]any{"ask": "find every caller of Open"})
	if err != nil {
		t.Fatal(err)
	}
	if v.Session != asked.Thread {
		t.Fatalf("the task was asked in %s", v.Session)
	}
	p.checkBackground(t.Context())
	it := awaitState(t, p, v.ID)
	for who, client := range map[*auth.Principal]string{&machine: "app", &other: "other"} {
		heard := heardFor(t, p, *who, keyBackground+it.ID)
		if len(heard) != 1 || heard[0].For != (ActAs{Client: client, User: "U0ADA"}) || heard[0].Reply != nil {
			t.Fatalf("%s hears of a task asked in the console as %+v", client, heard)
		}
	}

	_, slackReq := askedInSlack(t, p, machine, req)
	w, err := useBackground(t, p, slackReq, ToolTaskStart, map[string]any{"ask": "list the Plaud incidents"})
	if err != nil {
		t.Fatal(err)
	}
	p.checkBackground(t.Context())
	awaitState(t, p, w.ID)
	if heard := heardFor(t, p, other, keyBackground+w.ID); len(heard) != 0 {
		t.Fatalf("a client whose thread did not ask hears of it: %+v", heard)
	}
	if heard := heardFor(t, p, machine, keyBackground+w.ID); len(heard) != 1 || heard[0].Reply == nil {
		t.Fatalf("the client whose thread asked hears of it as %+v", heard)
	}

	plain, _, err := p.Inbox.Raise("acme", "delegate:x", inbox.Message{Kind: inbox.KindResult, To: "ada", Subject: "lead finished", Source: inbox.Source{Project: "Field"}, Parts: []inbox.Part{{Type: inbox.PartText, State: inbox.PartInfo, Text: "done"}}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if heard := p.SurfaceNoticesFor(t.Context(), machine, inbox.Notice{ID: plain.ID, Kind: inbox.KindResult, State: inbox.Open}); len(heard) != 0 {
		t.Fatalf("a result background work did not send is forwarded: %+v", heard)
	}
}
