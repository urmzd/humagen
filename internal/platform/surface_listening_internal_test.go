package platform

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/join"
	"github.com/urmzd/humagen/pkg/people"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

// When the app listens in Slack, by event:
//
//   - a mention in a channel thread's first message starts the thread and it
//     is listened to: a later message there that addresses nobody is read,
//     and answered only when the agent says it calls for it;
//   - a mention in a later reply is answered once: that thread is not
//     listened to, and a message there that addresses nobody is not kept;
//   - where there is no thread, a message that addresses nobody starts none;
//   - a direct message needs no listening.
func TestWhenTheAppListensInASlackThread(t *testing.T) {
	p, _, _ := slackPlatform(t)
	machine := slackReporting(t, p, "app", "svc-slack")
	wanted := true
	p.cfg.Router = func(string) join.Asker {
		return join.AskerFunc(func(_ context.Context, c join.Candidate, _ string) (join.Decision, error) {
			return join.Decision{Agent: c.Name, Join: wanted, Reason: "the test said so"}, nil
		})
	}
	post := func(channel, ts, message string, in ThreadInput) (Posted, error) {
		t.Helper()
		in.ActAs = &ActAs{Client: "app", User: "U0ADA"}
		in.Surface = &SurfaceRef{Client: "app", Channel: channel, TS: ts, Message: message, AppUser: "UAPP"}
		got, err := p.StartThread(t.Context(), machine, in)
		for _, r := range got.Runs {
			awaitEnd(t, p, r.ID)
		}
		return got, err
	}
	said := func(thread string) int {
		t.Helper()
		msgs, _, err := p.Threads.Messages("acme", thread, "", 50)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, m := range msgs {
			if m.By == "ada" {
				n++
			}
		}
		return n
	}

	// A thread whose first message mentioned the app, listened to once it answered.
	root, err := post("C0TEAM", "1700000000.000100", "", ThreadInput{Text: "what broke the build?"})
	if err != nil || len(root.Runs) != 1 || !root.Thread.Addressed {
		t.Fatalf("a mention in a thread's first message: %d runs, addressed %v, %v", len(root.Runs), root.Thread.Addressed, err)
	}
	awaitListening(t, p, root.Thread.ID)
	heard, err := post("C0TEAM", "1700000000.000100", "1700000000.000200", ThreadInput{Text: "it was the migration, can you confirm?", Listen: true})
	if err != nil || len(heard.Runs) != 1 {
		t.Fatalf("a message the agent says is for it: %d runs, %v", len(heard.Runs), err)
	}
	wanted = false
	aside, err := post("C0TEAM", "1700000000.000100", "1700000000.000300", ThreadInput{Text: "lunch at noon?", Listen: true})
	if err != nil || len(aside.Runs) != 0 || aside.Rule != threads.RulePostOnly {
		t.Fatalf("a message the agent says is not for it: %d runs, rule %q, %v", len(aside.Runs), aside.Rule, err)
	}
	if n := said(root.Thread.ID); n != 3 {
		t.Fatalf("the listened thread keeps %d of ada's messages, want all 3", n)
	}

	// A thread a later reply's mention started: answered once, not listened to.
	wanted = true
	later, err := post("C0TEAM", "1700000000.000500", "1700000000.000600", ThreadInput{Text: "can you summarise this thread?"})
	if err != nil || len(later.Runs) != 1 || later.Thread.Addressed {
		t.Fatalf("a mention in a later reply: %d runs, addressed %v, %v", len(later.Runs), later.Thread.Addressed, err)
	}
	if _, err := post("C0TEAM", "1700000000.000500", "1700000000.000700", ThreadInput{Text: "thanks, and the other one?", Listen: true}); !errors.Is(err, ErrNotListening) {
		t.Fatalf("a message that addresses nobody in a thread not listened to: %v", err)
	}
	if n := said(later.Thread.ID); n != 1 {
		t.Fatalf("the thread not listened to keeps %d of ada's messages, want the one that mentioned the app", n)
	}

	// No thread: nothing starts, nothing is kept.
	if _, err := post("C0TEAM", "1700000000.000900", "1700000000.000950", ThreadInput{Text: "anyone around?", Listen: true}); !errors.Is(err, ErrNotListening) {
		t.Fatalf("a message that addresses nobody where there is no thread: %v", err)
	}
	if _, ok, _ := p.Threads.BySurface("acme", threads.Surface{Client: "app", Channel: "C0TEAM", TS: "1700000000.000900"}); ok {
		t.Fatal("a message that addressed nobody started a thread")
	}

	// A direct message is always addressed to the app.
	dm, err := post("D0ADA", "1700000000.001000", "", ThreadInput{Text: "hello"})
	if err != nil || dm.Thread.Addressed || dm.Thread.Listening {
		t.Fatalf("a direct message: addressed %v, listening %v, %v", dm.Thread.Addressed, dm.Thread.Listening, err)
	}
}

// awaitListening waits for a thread's answered run to start listening there.
func awaitListening(t *testing.T, p *Platform, id string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		th, err := p.Threads.Get("acme", id)
		if err == nil && th.Listening {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("thread %s is not listening: %v", id, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// An addressed thread is listened to only once the app has answered there:
// a refused mention (no run), a failed run, a run still quiet with no answer
// text, and a first turn waiting on a person start no listening, so a later
// message that addresses nobody there runs nothing. A quiet run that answers
// later starts it when it ends.
func TestAThreadIsListenedToOnlyOnceTheAppHasAnswered(t *testing.T) {
	p, _, _ := slackPlatform(t)
	machine := slackReporting(t, p, "app", "svc-slack")
	p.cfg.Router = func(string) join.Asker {
		return join.AskerFunc(func(_ context.Context, c join.Candidate, _ string) (join.Decision, error) {
			return join.Decision{Agent: c.Name, Join: true, Reason: "it would take anything"}, nil
		})
	}
	// An answered thread to copy: its place, people and agents.
	answered, err := p.StartThread(t.Context(), machine, ThreadInput{
		Text:    "what broke the build?",
		ActAs:   &ActAs{Client: "app", User: "U0ADA"},
		Surface: &SurfaceRef{Client: "app", Channel: "C0TEAM", TS: "1700000000.000100", AppUser: "UAPP"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range answered.Runs {
		awaitEnd(t, p, r.ID)
	}
	awaitListening(t, p, answered.Thread.ID)
	model, err := p.Threads.Get("acme", answered.Thread.ID)
	if err != nil {
		t.Fatal(err)
	}

	// A thread whose first message mentioned the app, and whose turn did not answer.
	addressed := func(ts string) threads.Thread {
		t.Helper()
		th := model
		th.ID, th.Version, th.Listening, th.Listener = "", 0, false, nil
		sf := *model.Surface
		sf.TS = ts
		th.Surface = &sf
		got, err := p.Threads.Create("acme", th, p.now())
		if err != nil {
			t.Fatal(err)
		}
		if !got.Addressed || got.Listening {
			t.Fatalf("a new addressed thread: addressed %v, listening %v", got.Addressed, got.Listening)
		}
		return got
	}
	// Ada's mention there, as the platform keeps it.
	asked := func(th threads.Thread) threads.Message {
		t.Helper()
		m, _, err := p.Threads.Post("acme", th.ID, threads.Message{By: "ada", Name: "Ada", Text: "what broke the build?", SurfaceID: th.Surface.TS}, p.now())
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	reply := func(th threads.Thread, ts string) (Posted, error) {
		t.Helper()
		return p.StartThread(t.Context(), machine, ThreadInput{
			Text:    "and the rollback?",
			Listen:  true,
			ActAs:   &ActAs{Client: "app", User: "U0ADA"},
			Surface: &SurfaceRef{Client: "app", Channel: "C0TEAM", TS: th.Surface.TS, Message: ts, AppUser: "UAPP"},
		})
	}
	unheard := func(name string, th threads.Thread) {
		t.Helper()
		got, err := reply(th, "1700000099.000900")
		if !errors.Is(err, ErrNotListening) || len(got.Runs) != 0 {
			t.Fatalf("%s: an unmentioned reply ran %d, %v; want none and ErrNotListening", name, len(got.Runs), err)
		}
		if now, _ := p.Threads.Get("acme", th.ID); now.Listening {
			t.Fatalf("%s: the thread is listening", name)
		}
	}

	refused := addressed("1700000010.000100")
	unheard("a refused mention", refused)

	failed := addressed("1700000020.000100")
	if err := p.listenOnAnswer(runs.Run{Tenant: "acme", Thread: failed.ID, Status: runs.Failed, Answer: "half an answer"}); err != nil {
		t.Fatal(err)
	}
	unheard("a failed run", failed)

	paused := addressed("1700000030.000100")
	if err := p.listenOnAnswer(runs.Run{Tenant: "acme", Thread: paused.ID, Status: runs.Parked}); err != nil {
		t.Fatal(err)
	}
	unheard("a first turn waiting on a person", paused)

	quiet := addressed("1700000040.000100")
	if err := p.listenOnAnswer(runs.Run{Tenant: "acme", Thread: quiet.ID, Status: runs.Running}); err != nil {
		t.Fatal(err)
	}
	if err := p.listenOnAnswer(runs.Run{Tenant: "acme", Thread: quiet.ID, Status: runs.Completed, Answer: "  "}); err != nil {
		t.Fatal(err)
	}
	unheard("a quiet run with no answer text", quiet)

	// The quiet run answers later: from then on the thread listens to Ada.
	if err := p.answered("acme", quiet.ID, asked(quiet).ID); err != nil {
		t.Fatal(err)
	}
	if now, _ := p.Threads.Get("acme", quiet.ID); !now.Listening || now.Listener == nil || now.Listener.Person != "ada" {
		t.Fatalf("a quiet run that answered later: listening %v, listener %+v", now.Listening, now.Listener)
	}
	heard, err := reply(quiet, "1700000041.000200")
	if err != nil || len(heard.Runs) != 1 {
		t.Fatalf("an unmentioned reply once the app answered: %d runs, %v", len(heard.Runs), err)
	}
	for _, r := range heard.Runs {
		awaitEnd(t, p, r.ID)
	}

	// A thread a later mention started is never listened to, answered or not.
	later := model
	later.ID, later.Version, later.Listening, later.Listener, later.Addressed = "", 0, false, nil, false
	sf := *model.Surface
	sf.TS = "1700000050.000100"
	later.Surface = &sf
	lt, err := p.Threads.Create("acme", later, p.now())
	if err != nil {
		t.Fatal(err)
	}
	if err := p.answered("acme", lt.ID, asked(lt).ID); err != nil {
		t.Fatal(err)
	}
	unheard("a thread a later mention started", lt)
}

// A thread's listener is shown by handle, as its other people are: a
// listener given by email never comes back as the email (N1).
func TestAThreadsListenerIsShownByHandle(t *testing.T) {
	p, _, _ := slackPlatform(t)
	const email = "ada.lovelace@example.com"
	if _, err := p.People.Seen("acme", people.Sighting{Subject: email, Name: "Ada Lovelace", Source: people.SignIn}, time.Now()); err != nil {
		t.Fatal(err)
	}
	shown := p.shower("acme")
	th := shown.thread(threads.Thread{Listening: true, Listener: &threads.Listener{Person: email, Message: "1700000000.000100"}})
	want := shown.person(email).Handle
	if want == "" || th.Listener == nil || th.Listener.Person != want || strings.Contains(th.Listener.Person, "@") {
		t.Fatalf("the listener is shown as %+v, want handle %q", th.Listener, want)
	}
	if th.Listener.Message != "1700000000.000100" {
		t.Fatalf("the listener's mention is %q", th.Listener.Message)
	}
}

// A failed mention answered on Retry starts listening for that mention's
// asker: the retried turn carries the message the failed one answered
// (F3), and the newest-mention rule still applies to it.
func TestAFailedMentionAnsweredOnRetryStartsListening(t *testing.T) {
	// Nothing listens here, so a turn on an OpenAI model fails.
	t.Setenv("OPENAI_BASE_URL", "http://127.0.0.1:1/v1")
	p, _, _ := slackPlatform(t)
	machine := slackReporting(t, p, "app", "svc-slack")
	ctx := t.Context()
	revise := func(provider, model string) {
		t.Helper()
		cur, err := p.Agents.Get(ctx, "acme", "lead", 0)
		if err != nil {
			t.Fatal(err)
		}
		spec := cur.Spec.Clone()
		spec.Provider, spec.Model = provider, model
		if _, err := p.Agents.Revise(ctx, "acme", spec, cur.Rev, "test"); err != nil {
			t.Fatal(err)
		}
	}
	revise("openai", "none")
	got, err := p.StartThread(ctx, machine, ThreadInput{
		Text:    "what broke the build?",
		ActAs:   &ActAs{Client: "app", User: "U0ADA"},
		Surface: &SurfaceRef{Client: "app", Channel: "C0TEAM", TS: "1700000000.000100", AppUser: "UAPP"},
	})
	if err != nil || len(got.Runs) != 1 {
		t.Fatalf("the mention: %d runs, %v", len(got.Runs), err)
	}
	failed := awaitEnd(t, p, got.Runs[0].ID)
	if failed.Status != runs.Failed {
		t.Fatalf("the mention's run ended %q, want failed", failed.Status)
	}
	if th, _ := p.Threads.Get("acme", got.Thread.ID); th.Listening {
		t.Fatal("a failed mention started listening")
	}

	// Retry as the Slack app asks it, the same words after the failed turn
	// for the same person, with the agent working again: its answer is the
	// mention's.
	revise("offline", "offline")
	again, err := p.Converse(ctx, "acme", Turn{ID: "api-slack-retry", Agent: failed.Agent, Text: failed.Text, After: failed.ID, Actor: failed.Actor, Client: "app"})
	if err != nil {
		t.Fatal(err)
	}
	if m := wire.Meta(again.Tag())[runs.MetaMessage]; m == "" || m != wire.Meta(failed.Tag())[runs.MetaMessage] {
		t.Fatalf("the retry answers message %q, want the failed turn's %q", m, wire.Meta(failed.Tag())[runs.MetaMessage])
	}
	if done := awaitEnd(t, p, again.ID); done.Status != runs.Completed || done.Answer == "" {
		t.Fatalf("the retry ended %q with %q", done.Status, done.Answer)
	}
	awaitListening(t, p, got.Thread.ID)
	th, err := p.Threads.Get("acme", got.Thread.ID)
	if err != nil || th.Listener == nil || th.Listener.Person != "ada" || th.Listener.Message != "1700000000.000100" {
		t.Fatalf("after the retry the thread listens to %+v, %v; want ada's mention", th.Listener, err)
	}
}
