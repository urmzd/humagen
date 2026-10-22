package threads_test

import (
	"strings"
	"testing"

	"github.com/urmzd/fiber/pkg/runtime"

	"github.com/urmzd/warp/pkg/runs"
	"github.com/urmzd/warp/pkg/threads"
)

func answered(agent, message, answer string) runs.Run {
	return runs.Run{Agent: agent, Message: message, Status: runs.Completed, Answer: answer}
}

func TestTheTranscriptNamesPeopleAndAgents(t *testing.T) {
	turns := []threads.Turn{
		threads.MessageTurn(threads.Message{By: "okta|dana", Name: "Dana", Text: "@ignite @scribe draft the note"}),
		threads.RunTurn(answered("ignite", "m-1", "Here is a draft.")),
		threads.RunTurn(answered("scribe", "m-1", "And another.")),
		threads.MessageTurn(threads.Message{By: "okta|ana", Text: "Shorter, please"}),
		threads.RunTurn(runs.Run{Agent: "ignite", Message: "m-2", Status: runs.Stopped, Answer: "Short"}),
		threads.RunTurn(runs.Run{Agent: "scribe", Message: "m-2", Status: runs.Failed}),
		threads.RunTurn(runs.Run{Agent: "ignite", Message: "m-2", Status: runs.Running}),
	}
	want := strings.Join([]string{
		"Conversation so far:",
		"",
		"Dana: @ignite @scribe draft the note",
		"",
		"ignite: Here is a draft.",
		"",
		"scribe: And another.",
		"",
		"okta|ana: Shorter, please",
		"",
		"ignite: Short\n" + runtime.StopMarker,
		"",
		"scribe: (this reply failed)",
		"",
		"ignite: (still working)",
	}, "\n")
	if got := threads.Transcript(turns, 10_000); got != want {
		t.Fatalf("transcript =\n%s\nwant\n%s", got, want)
	}
}

func TestAConversationFromBeforeThreadsReadsAsItAlwaysHas(t *testing.T) {
	turns := []threads.Turn{
		threads.RunTurn(runs.Run{Agent: "ignite", Text: "What changed?", Status: runs.Completed, Answer: "Two things."}),
		threads.MessageTurn(threads.Message{By: "okta|dana", Name: "Dana", Text: "Which two?"}),
	}
	want := "Conversation so far:\n\nUser: What changed?\nignite: Two things.\n\nDana: Which two?"
	if got := threads.Transcript(turns, 10_000); got != want {
		t.Fatalf("transcript =\n%s\nwant\n%s", got, want)
	}
}

func TestTheTranscriptKeepsTheNewestTurnsThatFit(t *testing.T) {
	var turns []threads.Turn
	for _, text := range []string{"first", "second", "third", "fourth"} {
		turns = append(turns, threads.MessageTurn(threads.Message{By: "d", Name: "Dana", Text: text}))
	}
	got := threads.Transcript(turns, len("Dana: third")+len("Dana: fourth"))
	if want := "Conversation so far:\n(2 earlier turns omitted)\n\nDana: third\n\nDana: fourth"; got != want {
		t.Fatalf("transcript =\n%s\nwant\n%s", got, want)
	}
	// The newest turn is always sent, however long.
	if got := threads.Transcript(turns, 1); !strings.HasSuffix(got, "Dana: fourth") || !strings.Contains(got, "(3 earlier turns omitted)") {
		t.Fatalf("a limit smaller than a turn = %q", got)
	}
}
