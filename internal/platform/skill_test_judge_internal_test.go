package platform

import (
	"testing"

	"github.com/urmzd/saige/flow/event"

	"github.com/urmzd/humagen/pkg/runs"
)

// A completed skill test is judged only when its events hold it whole. A
// tool call that began executing and whose result is missing at the end of
// the final attempt is seen, though the completion closes the numbering; a
// call lost there together with its result cannot be, as nothing that
// arrived names it (ported from the second review).
func TestAJudgedSkillTestSeesAMissingToolResult(t *testing.T) {
	const stride = 1 << 20
	run := runs.Run{ID: "r", Status: runs.Completed}
	ev := func(seq uint64, b event.Body) event.Envelope {
		return event.Envelope{Run: "r", Seq: seq, Type: b.Type(), Data: b}
	}
	accepted := ev(1, event.Accepted{})
	started := ev(stride+1, event.Started{Attempt: 1})
	call := ev(stride+2, event.ToolCall{ID: "c", Name: ToolSkillFileRead, Phase: "executing"})
	done := ev(stride+3, event.ToolDone{ID: "c", Name: ToolSkillFileRead, Error: "refused"})
	completed := ev(2*stride, event.Completed{Attempt: 1})

	whole, missing := judgeSkillTest(run, []event.Envelope{accepted, started, call, done, completed})
	if missing != "" || whole.Passed {
		t.Fatalf("whole: %+v, %q", whole, missing)
	}
	if _, missing := judgeSkillTest(run, []event.Envelope{accepted, started, call, completed}); missing != eventsMissing {
		t.Fatalf("a call without its result: %q", missing)
	}
	if _, missing := judgeSkillTest(run, []event.Envelope{started, call, done, completed}); missing != eventsGone {
		t.Fatalf("the oldest events gone: %q", missing)
	}
	// The limit: a call and its result both lost at the end of the final
	// attempt leave nothing to see.
	if lost, missing := judgeSkillTest(run, []event.Envelope{accepted, started, completed}); missing != "" || !lost.Passed {
		t.Fatalf("both lost: %+v, %q", lost, missing)
	}
	// A run that did not complete fails at once, whatever arrived.
	stopped := runs.Run{ID: "r", Status: runs.Stopped}
	if got, missing := judgeSkillTest(stopped, []event.Envelope{started}); missing != "" || got.Passed || got.Reason != "the test did not complete: it stopped" {
		t.Fatalf("stopped: %+v, %q", got, missing)
	}
}
