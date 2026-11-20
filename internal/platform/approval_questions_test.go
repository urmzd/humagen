package platform_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	saigetypes "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/event"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

// questioned stages picker, which says hello, then on the next turn asks a
// person what question says, in a session set to mode. The approval agent
// gives answers, in order, to what it is asked.
func questioned(t *testing.T, mode string, question []saigetypes.Delta, answers ...string) (*platform.Platform, *cast, runs.Run) {
	t.Helper()
	script := [][]saigetypes.Delta{}
	for _, a := range answers {
		script = append(script, say(a))
	}
	c := &cast{continues: true,
		says: map[string][][]saigetypes.Delta{
			"picker":                 {say("Hello."), question, say("Done.")},
			platform.BuiltinApprover: script,
		},
		tools: map[string][]string{"picker": {"ask_person"}},
	}
	p := stage(t, c, platform.Config{OfflineHooks: true, TestBuiltinProvider: "offline", TestBuiltinModel: "offline"})
	// A session people post in, so the approval agent reads what they said.
	first := startThread(t, p, admin, platform.ThreadInput{Text: "@picker Hi"})
	if len(first.Runs) != 1 {
		t.Fatalf("started %d runs", len(first.Runs))
	}
	await(t, p, first.Runs[0].ID, runs.Completed)
	if _, err := p.SetApprovalMode(admin, first.Thread.ID, mode); err != nil {
		t.Fatal(err)
	}
	posted := post(t, p, admin, first.Thread.ID, platform.MessageInput{Text: "@picker Paint it blue, then check with me."})
	if len(posted.Runs) != 1 {
		t.Fatalf("started %d runs", len(posted.Runs))
	}
	second := posted.Runs[0]
	return p, c, second
}

// Run and Answer answers a choice the session already settles, as the
// person who chose it, through the path their own answer takes; the card
// says the approval agent answered and why, and the decision is audited
// with the answer.
func TestRunAndAnswerAnswersAChoiceTheSessionSettles(t *testing.T) {
	p, c, second := questioned(t, threads.ApprovalDecide, asking("Which colour?", "red", "blue"),
		`{"decision": "allow", "reason": "Ada asked for blue.", "references": ["admin: Paint it blue"], "answer": "Blue"}`)
	await(t, p, second.ID, runs.Completed)
	if o := answeredFor(t, p, second.ID); o.Action != platform.ActChoose || o.By != admin.Subject || o.Note != "blue" {
		t.Fatalf("outcome = %+v", o)
	}
	if j := judgementOn(t, p, second.ID); j.Decision != "allow" || j.Reason != "Ada asked for blue." || j.Run == "" {
		t.Fatalf("judgement = %+v", j)
	}
	var got []audit.Record
	within(t, "the answer was audited", func() bool {
		got = judgements(t, p)
		return len(got) == 1
	})
	a := got[0].After
	if got[0].Actor != admin.Subject || a["verdict"] != "allow" || a["kind"] != event.QuestionChoice || a["answer"] != "blue" || a["mode"] != threads.ApprovalDecide || a["tool"] != "" {
		t.Fatalf("audit = %+v", got[0])
	}
	asked := c.requests(platform.BuiltinApprover)
	if len(asked) != 1 {
		t.Fatalf("the approval agent was asked %d times", len(asked))
	}
	for _, want := range []string{"kind: choice", "asks: Which colour?", "- red", "- blue", "Paint it blue", "Run and Answer"} {
		if !strings.Contains(asked[0], want) {
			t.Fatalf("the approval agent was not shown %q:\n%s", want, asked[0])
		}
	}
}

// A reply is answered in the approval agent's words.
func TestRunAndAnswerAnswersAReply(t *testing.T) {
	p, _, second := questioned(t, threads.ApprovalDecide, asking("Which colour did you say?"),
		`{"decision": "allow", "reason": "Ada said it.", "references": [], "answer": "blue"}`)
	await(t, p, second.ID, runs.Completed)
	if o := answeredFor(t, p, second.ID); o.Action != platform.ActReply || o.Note != "blue" {
		t.Fatalf("outcome = %+v", o)
	}
}

// What the approval agent hands to a person, cannot be read, denies, or
// answers with something that is not an option waits for a person; each is
// audited as a hand-off with why, and the card says so.
func TestWhatRunAndAnswerDoesNotSettleWaitsForAPerson(t *testing.T) {
	for name, answer := range map[string]string{
		"ask":           `{"decision": "ask", "reason": "Ada has not said which colour.", "references": []}`,
		"unreadable":    "",
		"deny":          `{"decision": "deny", "reason": "No.", "references": []}`,
		"not an option": `{"decision": "allow", "reason": "Green is nice.", "references": [], "answer": "green"}`,
		"no answer":     `{"decision": "allow", "reason": "Fine.", "references": []}`,
	} {
		t.Run(name, func(t *testing.T) {
			p, _, second := questioned(t, threads.ApprovalDecide, asking("Which colour?", "red", "blue"), answer)
			second = await(t, p, second.ID, runs.Parked)
			if m := questionFor(t, p, second.ID); m.Kind != inbox.KindClarification {
				t.Fatalf("message = %+v", m)
			}
			within(t, "the hand-off was audited", func() bool {
				got := judgements(t, p)
				return len(got) == 1 && got[0].After["verdict"] == "ask" && got[0].After["reason"] != "" && got[0].After["answer"] == ""
			})
			if j := judgementOn(t, p, second.ID); j.Decision != "ask" {
				t.Fatalf("judgement = %+v", j)
			}
			time.Sleep(100 * time.Millisecond)
			if r, _ := p.Runs.Get(tenant, second.ID); r.Status != runs.Parked {
				t.Fatalf("the question was answered: %s", r.Status)
			}
		})
	}
}

// A question asked of someone else is never answered for them: the
// approval agent is not asked, and the hand-off is audited.
func TestRunAndAnswerNeverAnswersForSomeoneElse(t *testing.T) {
	for _, who := range []string{olu.Subject, "role:operator", threads.SMEName} {
		t.Run(who, func(t *testing.T) {
			p, c, second := questioned(t, threads.ApprovalDecide,
				asked(map[string]any{"question": "Which colour?", "options": []any{"red", "blue"}, "asked_of": who}),
				`{"decision": "allow", "reason": "Blue.", "references": [], "answer": "blue"}`)
			second = await(t, p, second.ID, runs.Parked)
			questionFor(t, p, second.ID)
			within(t, "the hand-off was audited", func() bool {
				got := judgements(t, p)
				return len(got) == 1 && got[0].After["verdict"] == "ask" && strings.Contains(got[0].After["reason"], "It asks "+who) && got[0].After["approval_run"] == ""
			})
			time.Sleep(100 * time.Millisecond)
			if r, _ := p.Runs.Get(tenant, second.ID); r.Status != runs.Parked {
				t.Fatalf("a question for %s was answered: %s", who, r.Status)
			}
			if got := c.requests(platform.BuiltinApprover); len(got) != 0 {
				t.Fatalf("the approval agent was asked %q", got)
			}
		})
	}
}

// Run alone, the default before Run and Answer, never answers a question:
// the approval agent is not asked and nothing is audited.
func TestRunLeavesQuestionsForAPerson(t *testing.T) {
	p, c, second := questioned(t, threads.ApprovalAuto, asking("Which colour?", "red", "blue"),
		`{"decision": "allow", "reason": "Blue.", "references": [], "answer": "blue"}`)
	second = await(t, p, second.ID, runs.Parked)
	questionFor(t, p, second.ID)
	time.Sleep(200 * time.Millisecond)
	if r, _ := p.Runs.Get(tenant, second.ID); r.Status != runs.Parked {
		t.Fatalf("Run answered a question: %s", r.Status)
	}
	if got := c.requests(platform.BuiltinApprover); len(got) != 0 {
		t.Fatalf("the approval agent was asked %q", got)
	}
	if got := judgements(t, p); len(got) != 0 {
		t.Fatalf("audited %+v", got)
	}
}

// Run and Answer is chosen by a person, audited as a change of mode, and
// still does what Run does.
func TestRunAndAnswerIsAModeAPersonChooses(t *testing.T) {
	p, _, first := judged(t, selfApproving, `{"decision": "allow", "reason": "The person asked lead to check it.", "references": []}`)
	th, err := p.SetApprovalMode(admin, first.Thread, threads.ApprovalDecide)
	if err != nil || !th.Approval.Auto() || !th.Approval.Decides() || th.Approval.By != admin.Subject {
		t.Fatalf("mode = %+v, %v", th.Approval, err)
	}
	rows, _ := p.Audit.List(tenant, audit.Query{From: time.Now().Add(-time.Hour), To: time.Now().Add(time.Hour), Limit: 50}, nil)
	if !slices.ContainsFunc(rows, func(r audit.Record) bool {
		return r.Action == "conversation.approval_mode" && r.After["mode"] == threads.ApprovalDecide && r.Before["mode"] == threads.ApprovalAsk
	}) {
		t.Fatalf("the change was not audited: %+v", rows)
	}
	// A call the policy asks about is decided as in Run.
	second := next(t, p, first)
	await(t, p, second.ID, runs.Completed)
	if o := answeredFor(t, p, second.ID); o.Action != platform.ActApprove || o.Note != "Allowed by the approval agent: The person asked lead to check it." {
		t.Fatalf("outcome = %+v", o)
	}
	within(t, "the decision was audited", func() bool {
		got := judgements(t, p)
		return len(got) == 1 && got[0].After["mode"] == threads.ApprovalDecide && got[0].After["kind"] == event.QuestionApproval
	})
}

// A question in work someone else asked for is theirs: Run and Answer,
// chosen by another person, hands it over without asking the approval
// agent.
func TestRunAndAnswerLeavesWhatSomeoneElseAskedFor(t *testing.T) {
	c := &cast{continues: true,
		says: map[string][][]saigetypes.Delta{
			"picker":                 {say("Hello."), asking("Which colour?", "red", "blue"), say("Done.")},
			platform.BuiltinApprover: {say(`{"decision": "allow", "reason": "Blue.", "references": [], "answer": "blue"}`)},
		},
		tools: map[string][]string{"picker": {"ask_person"}},
	}
	p := stage(t, c, platform.Config{OfflineHooks: true, TestBuiltinProvider: "offline", TestBuiltinModel: "offline"})
	first := startThread(t, p, admin, platform.ThreadInput{Text: "@picker Hi"})
	await(t, p, first.Runs[0].ID, runs.Completed)
	if _, err := p.SetApprovalMode(olu, first.Thread.ID, threads.ApprovalDecide); err != nil {
		t.Fatal(err)
	}
	posted := post(t, p, admin, first.Thread.ID, platform.MessageInput{Text: "@picker Paint it blue."})
	second := await(t, p, posted.Runs[0].ID, runs.Parked)
	questionFor(t, p, second.ID)
	within(t, "the hand-off was audited", func() bool {
		got := judgements(t, p)
		return len(got) == 1 && got[0].Actor == olu.Subject && strings.Contains(got[0].After["reason"], "Someone other than")
	})
	if got := c.requests(platform.BuiltinApprover); len(got) != 0 {
		t.Fatalf("the approval agent was asked %q", got)
	}
}
