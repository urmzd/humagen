package platform_test

import (
	"errors"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
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
