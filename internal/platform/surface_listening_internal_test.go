package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/urmzd/humagen/pkg/join"
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

	// A thread whose first message mentioned the app.
	root, err := post("C0TEAM", "1700000000.000100", "", ThreadInput{Text: "what broke the build?"})
	if err != nil || len(root.Runs) != 1 || !root.Thread.Listening {
		t.Fatalf("a mention in a thread's first message: %d runs, listening %v, %v", len(root.Runs), root.Thread.Listening, err)
	}
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
	if err != nil || len(later.Runs) != 1 || later.Thread.Listening {
		t.Fatalf("a mention in a later reply: %d runs, listening %v, %v", len(later.Runs), later.Thread.Listening, err)
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
	if err != nil || dm.Thread.Listening {
		t.Fatalf("a direct message: listening %v, %v", dm.Thread.Listening, err)
	}
}
