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
// about or the first line of a question, and whether the reader is asked.
func TestAWaitingSessionSaysWhatItWaitsForAndOnWhom(t *testing.T) {
	at := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	approval := runs.Run{Status: runs.Parked, UpdatedAt: at, Question: &event.Question{
		ID: "q1", Kind: event.QuestionApproval, Tool: &event.QuestionTool{Name: "file_write"}, Approvers: []string{"ada"},
	}}
	question := runs.Run{Status: runs.Parked, UpdatedAt: at.Add(time.Minute), Questions: []event.Question{
		{ID: "q2", Kind: event.QuestionReply, Prompt: "\n  Which repositories should I check?\nAll of them?"},
	}}
	if what, kind, you := waitingOn([]runs.Run{approval}, "ada", true); what != "file_write" || kind != WaitingApproval || !you {
		t.Fatalf("approval = %q %q %v", what, kind, you)
	}
	if _, _, you := waitingOn([]runs.Run{approval}, "bob", true); you {
		t.Fatal("an approval names its approvers: bob is not asked")
	}
	what, kind, you := waitingOn([]runs.Run{approval, question}, "bob", true)
	if what != "Which repositories should I check?" || kind != WaitingQuestion || !you {
		t.Fatalf("the latest parked run is a question that names nobody: %q %q %v", what, kind, you)
	}
	if _, _, you := waitingOn([]runs.Run{question}, "bob", false); you {
		t.Fatal("a question that names nobody asks those taking part")
	}
	if what, kind, _ := waitingOn([]runs.Run{{Status: runs.Completed, UpdatedAt: at}}, "ada", true); what != "" || kind != "" {
		t.Fatalf("nothing waits: %q %q", what, kind)
	}
	// A run's questions are read as Waits orders them: Questions before Question.
	both := runs.Run{Status: runs.Parked, UpdatedAt: at,
		Question:  &event.Question{ID: "old", Kind: event.QuestionReply, Prompt: "the single one"},
		Questions: []event.Question{{ID: "a", Kind: event.QuestionReply, Prompt: "the first of the batch"}},
	}
	if what, _, _ := waitingOn([]runs.Run{both}, "ada", true); what != "the first of the batch" {
		t.Fatalf("read %q, not the first question Waits gives", what)
	}
}

// A parent parked on its delegation to a child agent waits on no person, so
// nobody is told it waits for them; only a question a person answers counts.
func TestADelegationWaitsOnNobody(t *testing.T) {
	at := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	for _, tool := range []string{"session_call", "session_listen", "github-reviewer"} {
		delegation := runs.Run{Status: runs.Parked, UpdatedAt: at, Question: &event.Question{
			ID: "d1", Kind: event.QuestionDelegation, Tool: &event.QuestionTool{Name: tool},
		}}
		if what, kind, you := waitingOn([]runs.Run{delegation}, "ada", true); what != "" || kind != "" || you {
			t.Fatalf("a delegation through %s reads as %q %q %v", tool, what, kind, you)
		}
	}
	// A batch that holds a delegation and an approval waits on the approval.
	mixed := runs.Run{Status: runs.Parked, UpdatedAt: at, Questions: []event.Question{
		{ID: "d1", Kind: event.QuestionDelegation, Tool: &event.QuestionTool{Name: "session_call"}},
		{ID: "q1", Kind: event.QuestionApproval, Tool: &event.QuestionTool{Name: "deploy"}, Approvers: []string{"ada"}},
	}}
	if what, kind, you := waitingOn([]runs.Run{mixed}, "ada", true); what != "deploy" || kind != WaitingApproval || !you {
		t.Fatalf("the approval beside a delegation = %q %q %v", what, kind, you)
	}
	// A question without an id cannot be answered, so it is not one.
	if what, _, _ := waitingOn([]runs.Run{{Status: runs.Parked, UpdatedAt: at, Question: &event.Question{Kind: event.QuestionReply, Prompt: "?"}}}, "ada", true); what != "" {
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
