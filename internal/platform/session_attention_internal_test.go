package platform

import (
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
		Kind: "approval", Tool: &event.QuestionTool{Name: "file_write"}, Approvers: []string{"ada"},
	}}
	question := runs.Run{Status: runs.Parked, UpdatedAt: at.Add(time.Minute), Questions: []event.Question{
		{Kind: "question", Prompt: "\n  Which repositories should I check?\nAll of them?"},
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
}

// A session reads as failed only while its latest turn failed, with the
// first line of why; a turn after it, answered or still going, clears it.
func TestASessionsFailureIsItsLatestTurns(t *testing.T) {
	at := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	failed := runs.Run{Status: runs.Failed, UpdatedAt: at, Error: "provider: rate limited (429)\nretry after 30s"}
	if got := latestFailure([]runs.Run{failed}); got != "provider: rate limited (429)" {
		t.Fatalf("failure = %q", got)
	}
	if got := latestFailure([]runs.Run{{Status: runs.Failed, UpdatedAt: at}}); got != "No reason was recorded." {
		t.Fatalf("a failure without an error = %q", got)
	}
	for _, later := range []runs.Status{runs.Completed, runs.Running, runs.Parked} {
		if got := latestFailure([]runs.Run{failed, {Status: later, UpdatedAt: at.Add(time.Minute)}}); got != "" {
			t.Fatalf("after a %s turn the failure is %q", later, got)
		}
	}
}
