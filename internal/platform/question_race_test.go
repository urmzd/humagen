package platform_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/fiber/pkg/envelope"
	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
	"github.com/urmzd/humagen/pkg/threads"
)

// askingOlu starts a run that asks the billing expert, olu, for a PO number,
// with the fold that raises the question's message held until the returned
// func is called. The run shows it is parked on the question while the
// message is not raised yet.
func askingOlu(t *testing.T, cfg platform.Config) (*platform.Platform, runs.Run, func()) {
	t.Helper()
	c := &cast{continues: true,
		says:  map[string][][]saigetypes.Delta{"asker": {asked(map[string]any{"question": "What is the PO number?", "asked_of": "sme:billing"}), say("Filed.")}},
		tools: map[string][]string{"asker": {"ask_person"}},
	}
	p := stage(t, c, cfg)
	field(t, p)
	cur, err := p.ProjectExperts(tenant, "Field")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.SetProjectExperts(tenant, "Field", ada, platform.Experts{Version: cur.Version, Experts: []projects.Expert{{Who: "olu", Topics: []string{"billing"}}}}); err != nil {
		t.Fatal(err)
	}
	hold := make(chan struct{})
	p.BeforeQuestionRaised(func() {
		select {
		case <-hold:
		case <-time.After(20 * time.Second):
		}
	})
	r, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "api-ask", Agent: "asker", Text: "File it", Project: "Globex", Actor: envelope.Actor{ID: admin.Subject}})
	if err != nil {
		t.Fatal(err)
	}
	run := await(t, p, r.ID, runs.Parked)
	if run.Question == nil {
		t.Fatal("the run is parked on no question")
	}
	return p, run, sync1(func() { close(hold) })
}

// sync1 runs fn at most once, however often it is called.
func sync1(fn func()) func() {
	done := false
	return func() {
		if !done {
			done = true
			fn()
		}
	}
}

// questionMessages is every message kept under a question's key.
func questionMessages(p *platform.Platform, run runs.Run) []inbox.Message {
	all, _ := p.Inbox.Snapshot(tenant)
	var out []inbox.Message
	for _, m := range all {
		if m.Key == "question:"+run.ID+":"+run.Question.ID {
			out = append(out, m)
		}
	}
	return out
}

// A reply posted in the thread in the moment between the run showing its
// question and the question's message being raised answers it, as an
// answer through the inbox does, and the question has one message.
func TestAReplyPostedBeforeTheQuestionIsRaisedAnswersIt(t *testing.T) {
	p, run, release := askingOlu(t, platform.Config{})
	defer release()
	ref := &threads.AnswerRef{Run: run.ID, Question: run.Question.ID}
	if _, err := p.PostMessage(t.Context(), olu, run.Thread, platform.MessageInput{Text: "PO-77", Answers: ref}); err != nil {
		t.Fatalf("a reply before the question's message was raised = %v", err)
	}
	release()
	await(t, p, run.ID, runs.Completed)
	got := questionMessages(p, run)
	if len(got) != 1 {
		t.Fatalf("the question has %d messages, want 1", len(got))
	}
	if o := got[0].Parts[0].Outcome; got[0].State != inbox.Resolved || o == nil || o.By != "olu" {
		t.Fatalf("the question's message is %s with outcome %+v, want resolved by olu", got[0].State, o)
	}
}

// An answer whose question's message cannot be kept says so, and is not
// taken for an answer to a message that asks nothing.
func TestAnAnswerWhoseQuestionCannotBeKeptSaysSo(t *testing.T) {
	st := statetest.NewFaulty(state.NewMemory())
	p, run, release := askingOlu(t, platform.Config{Backends: platform.Backends{State: st}})
	defer release()
	st.Fail(statetest.ErrInjected)
	_, err := p.AnswerQuestion(t.Context(), olu, run.ID, run.Question.ID, platform.QuestionAnswer{Action: platform.ActReply, Text: "PO-77"})
	st.Fail(nil)
	if !errors.Is(err, statetest.ErrInjected) || errors.Is(err, platform.ErrAction) || strings.Contains(err.Error(), "asks no question") {
		t.Fatalf("an answer whose message could not be kept = %v, want the store's error", err)
	}
	if got, _ := p.Runs.Get(tenant, run.ID); got.Status != runs.Parked {
		t.Fatalf("the refused answer moved the run to %s", got.Status)
	}
	// Once the store keeps it again, the same answer is taken.
	if _, err := p.AnswerQuestion(t.Context(), olu, run.ID, run.Question.ID, platform.QuestionAnswer{Action: platform.ActReply, Text: "PO-77"}); err != nil {
		t.Fatalf("the answer once the store keeps it = %v", err)
	}
	release()
	await(t, p, run.ID, runs.Completed)
	if got := questionMessages(p, run); len(got) != 1 {
		t.Fatalf("the question has %d messages, want 1", len(got))
	}
}
