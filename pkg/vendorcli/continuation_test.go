package vendorcli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	saigetypes "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/isolation"

	"github.com/urmzd/humagen/pkg/continuation"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

// The imported conversations here are generated for the tests.

// continuationTurn is a turn of a conversation continued from an imported
// one, as the platform writes it: the trigger's context, the imported lines
// and the turns since, and what is asked.
func continuationTurn(thread string, imported []threads.ImportTurn, since []exchange, ask string) saigetypes.Message {
	turns := threads.ImportSeed(&threads.Import{Tool: threads.ImportChatGPT, Context: imported})
	for _, e := range since {
		turns = append(turns, threads.RunTurn(runs.Run{Agent: "coder", Text: e.ask, Answer: e.answer, Status: runs.Completed}))
	}
	s := "Context from the trigger:\n- " + runs.MetaAsk + ": " + ask + "\n- " + runs.MetaThread + ": " + thread + "\n"
	return user(s + "\n" + threads.Ask(turns, 1<<20, nil, threads.Asker{Name: "Dev"}, ask))
}

var generatedImported = []threads.ImportTurn{{Speaker: threads.ImportUser, Text: "hello"}, {Speaker: threads.ImportAssistant, Text: "hi there"}}

// claimed is the context of a platform turn of one run, marked by the
// platform as a continuation's turn with rule, as the runtime and the
// platform put it there.
func claimed(t *testing.T, run string, rule *continuation.Rule) context.Context {
	t.Helper()
	ctx := isolation.WithRun(context.Background(), isolation.Unisolated(isolation.Request{Tenant: "acme", Run: run}))
	if rule != nil {
		continuation.Mark("acme", run, *rule)
		t.Cleanup(func() { continuation.Release("acme", run) })
	}
	return ctx
}

func streamIn(ctx context.Context, p saigetypes.Provider, msgs ...saigetypes.Message) error {
	ch, err := p.Stream(ctx, saigetypes.Request{Messages: msgs})
	if err != nil {
		return err
	}
	for d := range ch {
		if e, ok := d.(saigetypes.ErrorDelta); ok {
			err = e.Error
		}
	}
	return err
}

// An imported history holds no session a real conversation noted with the
// same words unmarked, and the continuation's own next turn holds its own.
func TestAnImportedHistoryHoldsNoSessionButTheContinuationsNextTurnHoldsItsOwn(t *testing.T) {
	first := continuationTurn("th-0000000000000001", generatedImported, nil, "go on")
	h1 := historyOf(text(first.(saigetypes.UserMessage).Parts))
	if !h1.known || len(h1.lines) != 2 || h1.omitted {
		t.Fatalf("the first turn's history: %+v", h1)
	}
	// A conversation in which a person said "hello" and was answered "hi
	// there", as its first turn's note keeps it.
	real := note{Kind: ClaudeCode, Session: "real-session", Asked: digest("hello"), Answered: digest("hi there")}
	if real.holds(h1) {
		t.Fatal("the imported history holds a real conversation's session")
	}
	own := (&provider{kind: ClaudeCode}).noteOf("own-session", h1, "Looking. Fixed.")
	next := continuationTurn("th-0000000000000001", generatedImported, []exchange{{"go on", "Looking. Fixed."}}, "and then?")
	h2 := historyOf(text(next.(saigetypes.UserMessage).Parts))
	if !own.holds(h2) {
		t.Fatalf("the continuation's next turn does not hold its own session: %+v", h2)
	}
	if real.holds(h2) {
		t.Fatal("the next turn holds a real conversation's session")
	}
}

// The words of an imported line are anyone's to write: a person can type
// "(imported, not said here) hello" in a conversation of their own. Then the
// digests of an imported history do match that conversation's session, and
// only the rule the platform passes through the run claim keeps a
// continuation from taking it.
func TestAnImportedHistoryThatRepeatsARealConversationCannotSelectItsSession(t *testing.T) {
	command, continued, _ := sessions(t, ClaudeCode)
	state := t.TempDir()
	p := build(t, ClaudeCode, Config{Command: command, StateDir: state}, "default")
	// That conversation's first turn, noted as the program noted it.
	n, _ := json.Marshal(note{Kind: ClaudeCode, Dir: providerDir(t, p), Session: "real-session", Asked: digest("(imported, not said here) hello"), Answered: digest("(imported, not said here) hi there"), At: time.Now()})
	if err := os.MkdirAll(filepath.Join(state, "begun"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "begun", "real-session"), n, 0o600); err != nil {
		t.Fatal(err)
	}
	turn := continuationTurn("th-0000000000000002", generatedImported, nil, "go on")
	// The continuation's first turn, and a turn the platform marks as a
	// later one of the same continuation, continue nothing.
	if err := streamIn(claimed(t, "r-first", &continuation.Rule{Thread: "th-0000000000000002", First: true}), p, turn); err != nil {
		t.Fatal(err)
	}
	if err := streamIn(claimed(t, "r-later", &continuation.Rule{Thread: "th-0000000000000002"}), p, turn); err != nil {
		t.Fatal(err)
	}
	if a, b := continued(0), continued(1); a != "" || b != "" {
		t.Fatalf("a continuation continued %q and %q", a, b)
	}
	// The same text in a turn the platform did not mark does select it:
	// what the claim's rule prevents is real.
	if err := streamIn(claimed(t, "r-unmarked", nil), p, turn); err != nil {
		t.Fatal(err)
	}
	if got := continued(2); got != "real-session" {
		t.Fatalf("the unmarked turn continued %q; the test does not show the risk", got)
	}
}

// A continuation's first turn starts a new session; its next turn continues
// that session and no other.
func TestAContinuationKeepsItsOwnSession(t *testing.T) {
	command, continued, answer := sessions(t, ClaudeCode)
	p := build(t, ClaudeCode, Config{Command: command}, "default")
	const thread = "th-0000000000000003"
	if err := streamIn(claimed(t, "r-1", &continuation.Rule{Thread: thread, First: true}), p, continuationTurn(thread, generatedImported, nil, "go on")); err != nil {
		t.Fatal(err)
	}
	if err := streamIn(claimed(t, "r-2", &continuation.Rule{Thread: thread}), p, continuationTurn(thread, generatedImported, []exchange{{"go on", answer}}, "and then?")); err != nil {
		t.Fatal(err)
	}
	if a, b := continued(0), continued(1); a != "" || b != "sess-0" {
		t.Fatalf("continued %q then %q", a, b)
	}
	// What it was first asked held the imported lines.
	if got := strings.Join(args(t, filepath.Dir(command)+"/runs", 0), "\n"); !strings.Contains(got, "imported from chatgpt, the assistant there: (imported, not said here) hi there") {
		t.Fatalf("the first turn was not sent the imported lines: %s", got)
	}
}

// /resume in a continuation is refused, saying what to do instead, and the
// program does not run.
func TestResumeInAContinuationIsRefused(t *testing.T) {
	command, record := program(t, claudeOut, 0)
	p := build(t, ClaudeCode, Config{Command: command}, "default")
	const thread = "th-0000000000000004"
	for _, rule := range []continuation.Rule{{Thread: thread, First: true}, {Thread: thread}} {
		err := streamIn(claimed(t, "r-resume", &rule), p, continuationTurn(thread, generatedImported, nil, "/resume 67f9a5c0-ca00-4dbe-8ece-3808aef67c44 go on"))
		if !errors.Is(err, ErrSession) || !errors.Is(err, ErrContinuationResume) || !strings.Contains(err.Error(), "start a plain conversation") {
			t.Fatalf("err = %v", err)
		}
	}
	if runs, _ := filepath.Glob(filepath.Join(record, "*.args")); len(runs) != 0 {
		t.Fatalf("the program ran %d times", len(runs))
	}
}
