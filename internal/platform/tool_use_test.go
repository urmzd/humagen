package platform_test

import (
	"errors"
	"testing"

	"github.com/urmzd/saige/agent/agenttest"
	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

// A call waiting on a person is answered by its tool_use_id, from anywhere in
// its conversation, as its run's question is: only by whoever may answer it,
// once, and only in that conversation.
func TestAnApprovalIsAnsweredByItsToolUseID(t *testing.T) {
	p, v, parked := parkedDeploy(t)
	ctx := t.Context()
	tr, err := p.MessageTrace(ctx, admin, v.ID)
	if err != nil || len(tr.Nodes) != 1 || len(tr.Nodes[0].Children) != 1 {
		t.Fatalf("trace = %+v, %v", tr, err)
	}
	ops := tr.Nodes[0].Children[0]
	if len(ops.Tools) != 1 || len(ops.Context) != 1 || len(ops.Approvals) != 1 || ops.Approvals[0].Tool != ops.Tools[0].ID {
		t.Fatalf("ops's node = %+v", ops)
	}
	write, read := ops.Tools[0].ID, ops.Context[0].ID
	approve := platform.QuestionAnswer{Action: platform.ActApprove, Note: "ship it"}

	if _, err := p.AnswerToolUse(ctx, admin, v.ID, "call_123", approve); !errors.Is(err, threads.ErrInvalid) {
		t.Fatalf("a provider's call id = %v, want ErrInvalid", err)
	}
	if _, err := p.AnswerToolUse(ctx, admin, v.ID, read, approve); !errors.Is(err, threads.ErrNotFound) {
		t.Fatalf("a call nothing waits on = %v, want ErrNotFound", err)
	}
	other, err := p.StartSession(ctx, admin, platform.SessionInput{Project: "~", Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.AnswerToolUse(ctx, admin, other.ID, write, approve); !errors.Is(err, threads.ErrNotFound) {
		t.Fatalf("a call answered from another conversation = %v, want ErrNotFound", err)
	}
	if _, err := p.AnswerToolUse(ctx, operator, v.ID, write, approve); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("someone who may not approve = %v, want ErrForbidden", err)
	}
	m, err := p.AnswerToolUse(ctx, admin, v.ID+":"+parked.ID, write, approve)
	if err != nil || len(m.Parts) == 0 || m.Parts[0].Outcome == nil {
		t.Fatalf("approving by tool_use_id = %+v, %v", m, err)
	}
	if done := await(t, p, parked.ID, runs.Completed); done.Answer != "Shipped." {
		t.Fatalf("after the approval ops answered %q", done.Answer)
	}
	if again, err := p.AnswerToolUse(ctx, admin, v.ID, write, platform.QuestionAnswer{Action: platform.ActDeny}); err != nil || again.Parts[0].Outcome == nil || again.Parts[0].Outcome.Action != platform.ActApprove {
		t.Fatalf("answering again = %+v, %v; want the recorded approval", again, err)
	}
}

// A question an agent asks a person shows in its message's trace while it
// waits, with the call that asked it, and is answered by that call's id; the
// trace then shows the answer and who gave it.
func TestAWaitingQuestionShowsInTheTraceAndIsAnsweredByItsCall(t *testing.T) {
	c := &cast{continues: true, says: map[string][][]saigetypes.Delta{
		"lead": {agenttest.ToolCallResponse("ask-1", "ask_person", map[string]any{"question": "Ship today?", "options": []any{"yes", "no"}}), say("Shipping today.")},
	}, tools: map[string][]string{"lead": {"ask_person"}}}
	p := stage(t, c, platform.Config{Limits: call.Limits{MaxDepth: 2, MaxFanOut: 4}})
	setAgents(t, p, "~", platform.AgentsInput{Enable: []string{"*"}})
	seePeople(t, p, admin)
	ctx := t.Context()
	v, err := p.StartSession(ctx, admin, platform.SessionInput{Project: "~", Text: "@lead ship the release"})
	if err != nil {
		t.Fatal(err)
	}
	var lead runs.Run
	within(t, "lead asked its question", func() bool {
		r, ok := runOf(p, tenant, "lead")
		lead = r
		return ok && r.Status == runs.Parked && r.Question != nil
	})
	questionFor(t, p, lead.ID)
	asked := func() platform.TraceQuestion {
		t.Helper()
		tr, err := p.MessageTrace(ctx, admin, v.ID)
		if err != nil || len(tr.Nodes) != 1 || len(tr.Nodes[0].Questions) != 1 {
			t.Fatalf("trace = %+v, %v", tr, err)
		}
		return tr.Nodes[0].Questions[0]
	}
	q := asked()
	if q.Tool != "htu_"+lead.ID+"_1" || q.Kind != "choice" || q.Prompt != "Ship today?" || len(q.Options) != 2 || q.Status != platform.TraceAsked || q.Answer != "" {
		t.Fatalf("the waiting question = %+v", q)
	}
	if _, err := p.AnswerToolUse(ctx, admin, v.ID, q.Tool, platform.QuestionAnswer{Action: platform.ActChoose, Choice: "yes"}); err != nil {
		t.Fatal(err)
	}
	await(t, p, lead.ID, runs.Completed)
	if q := asked(); q.Status != platform.TraceAnswered || q.Answer != "yes" || q.By == "" {
		t.Fatalf("the answered question = %+v", q)
	}
}
