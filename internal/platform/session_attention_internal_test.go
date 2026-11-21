package platform

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/saige/flow/event"
)

// A session someone else started that the reader posted in, was mentioned
// in or was added to is one they joined; their own is not.
func TestASessionIsJoinedByThoseWhoTakePartWithoutStartingIt(t *testing.T) {
	th := threads.Thread{CreatedBy: "ada", Participants: []threads.Participant{
		{Kind: threads.Person, Name: "ada"}, {Kind: threads.Person, Name: "bob"}, {Kind: threads.Agent, Name: "carol"},
	}}
	for subject, want := range map[string]bool{"ada": false, "bob": true, "carol": false, "dan": false, "": false} {
		if got := joinedSession(th, subject); got != want {
			t.Errorf("joined by %q = %v, want %v", subject, got, want)
		}
	}
}

// A parked run says what it waits for in words: the tool an approval asks
// about or the first line of a question. Whom it waits on is the inbox's
// (openQuestions), not the run's.
func TestAWaitingSessionSaysWhatItWaitsFor(t *testing.T) {
	at := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	approval := runs.Run{Status: runs.Parked, UpdatedAt: at, Question: &event.Question{
		ID: "q1", Kind: event.QuestionApproval, Tool: &event.QuestionTool{Name: "file_write"}, Approvers: []string{"ada"},
	}}
	question := runs.Run{Status: runs.Parked, UpdatedAt: at.Add(time.Minute), Questions: []event.Question{
		{ID: "q2", Kind: event.QuestionReply, Prompt: "\n  Which repositories should I check?\nAll of them?"},
	}}
	if what, kind := waitingOn([]runs.Run{approval}); what != "file_write" || kind != WaitingApproval {
		t.Fatalf("approval = %q %q", what, kind)
	}
	if what, kind := waitingOn([]runs.Run{approval, question}); what != "Which repositories should I check?" || kind != WaitingQuestion {
		t.Fatalf("the latest parked run = %q %q", what, kind)
	}
	if what, kind := waitingOn([]runs.Run{{Status: runs.Completed, UpdatedAt: at}}); what != "" || kind != "" {
		t.Fatalf("nothing waits: %q %q", what, kind)
	}
	// A run's questions are read as Waits orders them: Questions before Question.
	both := runs.Run{Status: runs.Parked, UpdatedAt: at,
		Question:  &event.Question{ID: "old", Kind: event.QuestionReply, Prompt: "the single one"},
		Questions: []event.Question{{ID: "a", Kind: event.QuestionReply, Prompt: "the first of the batch"}},
	}
	if what, _ := waitingOn([]runs.Run{both}); what != "the first of the batch" {
		t.Fatalf("read %q, not the first question Waits gives", what)
	}
}

// A parent parked on its delegation to a child agent waits on no person.
func TestADelegationWaitsOnNobody(t *testing.T) {
	at := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	for _, tool := range []string{"session_call", "session_listen", "github-reviewer"} {
		delegation := runs.Run{Status: runs.Parked, UpdatedAt: at, Question: &event.Question{
			ID: "d1", Kind: event.QuestionDelegation, Tool: &event.QuestionTool{Name: tool},
		}}
		if what, kind := waitingOn([]runs.Run{delegation}); what != "" || kind != "" {
			t.Fatalf("a delegation through %s reads as %q %q", tool, what, kind)
		}
	}
	mixed := runs.Run{Status: runs.Parked, UpdatedAt: at, Questions: []event.Question{
		{ID: "d1", Kind: event.QuestionDelegation, Tool: &event.QuestionTool{Name: "session_call"}},
		{ID: "q1", Kind: event.QuestionApproval, Tool: &event.QuestionTool{Name: "deploy"}},
	}}
	if what, kind := waitingOn([]runs.Run{mixed}); what != "deploy" || kind != WaitingApproval {
		t.Fatalf("the approval beside a delegation = %q %q", what, kind)
	}
	if what, _ := waitingOn([]runs.Run{{Status: runs.Parked, UpdatedAt: at, Question: &event.Question{Kind: event.QuestionReply, Prompt: "?"}}}); what != "" {
		t.Fatalf("a question without an id reads as %q", what)
	}
}

// A session reads as failed only while its latest turn failed, with the
// first line of why; a turn after it, answered or still going, clears it.
// A run moved out of a project does not name the project it left.
func TestASessionsFailureIsItsLatestTurns(t *testing.T) {
	at := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	th := threads.Thread{Project: "launch", Channel: "general"}
	failed := runs.Run{Status: runs.Failed, UpdatedAt: at, Project: "launch", Channel: "general", Error: "provider: rate limited (429)\nretry after 30s"}
	if got := latestFailure([]runs.Run{failed}, th); got != "provider: rate limited (429)" {
		t.Fatalf("failure = %q", got)
	}
	if got := latestFailure([]runs.Run{{Status: runs.Failed, UpdatedAt: at, Project: "launch", Channel: "general"}}, th); got != "No reason was recorded." {
		t.Fatalf("a failure without an error = %q", got)
	}
	for _, later := range []runs.Status{runs.Completed, runs.Running, runs.Parked} {
		if got := latestFailure([]runs.Run{failed, {Status: later, UpdatedAt: at.Add(time.Minute)}}, th); got != "" {
			t.Fatalf("after a %s turn the failure is %q", later, got)
		}
	}
	moved := runs.Run{Status: runs.Failed, UpdatedAt: at, Project: "acquisition-x", Channel: "general", Error: "acquisition-x: the data room is locked"}
	if got := latestFailure([]runs.Run{moved}, th); strings.Contains(got, "acquisition-x") || got != "[Private context]: the data room is locked" {
		t.Fatalf("a moved run's failure = %q", got)
	}
}

// waiting_on_you is always sent, so a reader who is not asked is told so
// rather than left to a default.
func TestWaitingOnYouIsAlwaysSent(t *testing.T) {
	for _, you := range []bool{false, true} {
		out, err := json.Marshal(ThreadSummary{WaitingFor: "deploy", WaitingKind: WaitingApproval, WaitingOnYou: you})
		if err != nil {
			t.Fatal(err)
		}
		if want := fmt.Sprintf(`"waiting_on_you":%v`, you); !strings.Contains(string(out), want) {
			t.Fatalf("%s lacks %s", out, want)
		}
	}
}
