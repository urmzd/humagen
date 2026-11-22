package platform_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/saige/flow/isolation"
	"github.com/urmzd/saige/flow/isolation/isolationtest"
)

// commandMessages is a session's messages that record a command, oldest
// first, and how many of its messages are runs.
func commandMessages(v platform.SessionView) (cmds []threads.Message, runs int) {
	for _, m := range v.Messages {
		if m.Run != nil {
			runs++
		}
		if m.Message != nil && m.Message.Command != nil {
			cmds = append(cmds, *m.Message)
		}
	}
	return cmds, runs
}

// A "!" message runs its command at once in the session's sandbox and is
// posted with what it printed. It starts no run. The session's agent reads
// the command and its output with the next message, ahead of its words,
// and the run records only the words as what was asked.
func TestABangCommandIsPostedWithItsOutputAndTheNextMessageCarriesIt(t *testing.T) {
	leaser := &isolationtest.Leaser{Dir: t.TempDir()}
	p := startWith(t, platform.Config{Sandbox: isolation.Sandbox{Leaser: leaser, Name: "test"}, SandboxContained: true})
	v, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Agent: "lead", Text: "!echo hello; echo oops >&2; exit 3", Command: true})
	if err != nil {
		t.Fatal(err)
	}
	cmds, started := commandMessages(v)
	if started != 0 || len(cmds) != 1 {
		t.Fatalf("the command started %d runs and posted %d commands: %+v", started, len(cmds), v.Messages)
	}
	c := cmds[0]
	if c.Text != "!echo hello; echo oops >&2; exit 3" || c.Command.Command != "echo hello; echo oops >&2; exit 3" || c.Command.ExitCode != 3 || c.Command.TimedOut || c.Command.Output != "hello\noops\n" || c.By != admin.Subject {
		t.Fatalf("the command message = %+v %+v", c, c.Command)
	}
	if len(leaser.Instances()) != 1 || len(leaser.Open()) != 0 {
		t.Fatalf("instances = %d, open = %d", len(leaser.Instances()), len(leaser.Open()))
	}

	// A second command, from the session itself, follows the first.
	if _, err := p.PostSession(t.Context(), admin, v.ID, platform.MessageInput{Text: "!ls", Command: true}); err != nil {
		t.Fatal(err)
	}
	next, err := p.PostSession(t.Context(), admin, v.ID, platform.MessageInput{Text: "What did it print?"})
	if err != nil {
		t.Fatal(err)
	}
	var run runs.Run
	for _, m := range next.Messages {
		if m.Run != nil {
			run = *m.Run
		}
	}
	if run.ID == "" {
		t.Fatalf("the message after the commands started no run: %+v", next.Messages)
	}
	text := wire.TextOf(run.Tag())
	order := []string{
		"Commands people ran by hand in this session's sandbox",
		`ran "echo hello; echo oops >&2; exit 3"; exit 3 after `, `output "hello\noops\n"`,
		`ran "ls"; exit 0 after `,
		"End of the commands. The message you are answering follows.\n\nWhat did it print?",
	}
	at := 0
	for _, want := range order {
		i := strings.Index(text[at:], want)
		if i < 0 {
			t.Fatalf("the agent was sent, without %q in order:\n%s", want, text)
		}
		at += i + len(want)
	}
	if run.Text != "What did it print?" {
		t.Fatalf("the run records %q as asked", run.Text)
	}
	await(t, p, run.ID, runs.Completed)

	// The message after that one carries no command again.
	again, err := p.PostSession(t.Context(), admin, v.ID, platform.MessageInput{Text: "And now?"})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range again.Messages {
		if m.Run != nil && m.Run.ID != run.ID && strings.Contains(wire.TextOf(m.Run.Tag()), "Commands people ran") {
			t.Fatalf("a later message was sent the commands again:\n%s", wire.TextOf(m.Run.Tag()))
		}
	}

	// The record is the session's: it reads the same after a reload.
	read, err := p.ReadSession(t.Context(), admin, v.ID, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if kept, _ := commandMessages(read); len(kept) != 2 || kept[0].Command.Output != "hello\noops\n" || kept[1].Command.Command != "ls" {
		t.Fatalf("the session reads %+v", kept)
	}
}

// A command runs only where the deployment's sandbox is contained, only for
// someone who may post in the session, and only from a message that is a
// command and nothing more. A refused first command leaves no session.
func TestABangCommandIsRefusedWhereItCouldReachTheHostOrIsNotTheirs(t *testing.T) {
	t.Run("an unisolated deployment starts no session", func(t *testing.T) {
		p := start(t, call.Limits{})
		before, _ := p.ListSessions(t.Context(), admin, "", "", "", "")
		_, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Agent: "lead", Text: "!echo hi", Command: true})
		if !errors.Is(err, platform.ErrUnisolated) {
			t.Fatalf("err = %v", err)
		}
		if after, _ := p.ListSessions(t.Context(), admin, "", "", "", ""); len(after) != len(before) {
			t.Fatalf("a refused command left a session: %d, then %d", len(before), len(after))
		}
	})

	leaser := &isolationtest.Leaser{Dir: t.TempDir()}
	p := startWith(t, platform.Config{Sandbox: isolation.Sandbox{Leaser: leaser, Name: "test"}, SandboxContained: true})
	v, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Agent: "lead", Text: "Notes", Command: false})
	if err != nil {
		t.Fatal(err)
	}
	await(t, p, sessionRun(t, v).ID, runs.Completed)
	leased := len(leaser.Instances())
	bang := platform.MessageInput{Text: "!echo hi", Command: true}
	t.Run("another workspace", func(t *testing.T) {
		other := auth.Principal{Subject: admin.Subject, Tenant: "globex", Kind: auth.User, Roles: []auth.Role{auth.Admin}}
		if _, err := p.PostMessage(t.Context(), other, v.ID, bang); !errors.Is(err, threads.ErrNotFound) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("someone the private session is not shared with", func(t *testing.T) {
		if _, err := p.PostMessage(t.Context(), operator, v.ID, bang); !errors.Is(err, threads.ErrNotFound) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("a reader", func(t *testing.T) {
		if _, err := p.PostMessage(t.Context(), viewer, v.ID, bang); err == nil {
			t.Fatal("a reader ran a command")
		}
	})
	t.Run("not a command, or a command with more", func(t *testing.T) {
		for name, in := range map[string]platform.MessageInput{
			"no !":       {Text: "echo hi", Command: true},
			"nothing":    {Text: "!   ", Command: true},
			"too long":   {Text: "!" + strings.Repeat("a", platform.MaxPersonCommand+1), Command: true},
			"with files": {Text: "!ls", Command: true, Uploads: []platform.Upload{{Name: "a.txt", Data: []byte("hi")}}},
		} {
			if _, err := p.PostMessage(t.Context(), admin, v.ID, in); !errors.Is(err, threads.ErrInvalid) {
				t.Errorf("%s: err = %v", name, err)
			}
		}
	})
	if n := len(leaser.Instances()); n != leased {
		t.Fatalf("a refused command leased an instance: %d, then %d", leased, n)
	}
}

// A command can hold a secret, so it is never sent to the title model and
// never shown as the title. The session waits for its first message that
// asks for something, and is named from that message alone.
func TestACommandIsNeverSentToTheTitleModel(t *testing.T) {
	f := &titles{name: "Checking the release build"}
	leaser := &isolationtest.Leaser{Dir: t.TempDir()}
	p := startWith(t, platform.Config{
		Limits:           call.Limits{},
		Sandbox:          isolation.Sandbox{Leaser: leaser, Name: "test"},
		SandboxContained: true,
		Titler:           func(string, string) platform.Titler { return f },
	})
	secret := "!curl -H 'Authorization: Bearer sk-live-123' https://example.test/build"
	v, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Agent: "lead", Text: secret, Command: true})
	if err != nil {
		t.Fatal(err)
	}
	p.WaitTitles()
	th := record(t, p, v.ID)
	if th.Title != runs.CommandTitle || th.TitleSource != threads.TitlePlaceholder {
		t.Fatalf("a command session is titled %q (%s)", th.Title, th.TitleSource)
	}
	// A second command names nothing either.
	if _, err := p.PostSession(t.Context(), admin, v.ID, platform.MessageInput{Text: "!echo sk-live-456", Command: true}); err != nil {
		t.Fatal(err)
	}
	p.WaitTitles()
	if n := len(f.calls()); n != 0 {
		t.Fatalf("a command went to the title model: %+v", f.calls())
	}
	// The first message that asks for something names it, from its own words.
	if title := postTitle(t, p, v.ID, "why does the release build fail on the second step"); title != "Why does the release build fail on…" && title != "Checking the release build" {
		t.Fatalf("title %q", title)
	}
	p.WaitTitles()
	asked := f.calls()
	if len(asked) != 1 || strings.Contains(asked[0].Message, "sk-live") || strings.Contains(asked[0].Answer, "sk-live") {
		t.Fatalf("asked %+v", asked)
	}
	if got := record(t, p, v.ID); got.Title != "Checking the release build" || got.TitleSource != threads.TitleGenerated {
		t.Fatalf("title %q (%s)", got.Title, got.TitleSource)
	}
}
