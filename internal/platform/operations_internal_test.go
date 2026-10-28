package platform

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/wire"

	"github.com/urmzd/humagen/fiber/pkg/event"
	"github.com/urmzd/humagen/fiber/pkg/turn/native"

	"github.com/urmzd/humagen/fiber/pkg/runtime"
	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/runs"
	saigetypes "github.com/urmzd/saige/agent/types"
)

func TestOperationToolFailsClosedAndKeepsPersonScope(t *testing.T) {
	p, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	req := runtime.Request{Tenant: "t", Definition: native.Revision{Spec: native.Spec{Name: "a"}}}
	calls := 0
	tool := p.operationTool(req, &saigetypes.ToolFunc{Def: saigetypes.ToolDef{Name: "file_write"}, Fn: func(context.Context, map[string]any) (string, error) { calls++; return "ok", nil }}, operations.WriteFiles)
	rule := func(level operations.Level) {
		pol := p.Operations.Policy("t", operations.Scope{Kind: "workspace"})
		if _, err := p.Operations.SetPolicy("t", operations.Policy{Scope: pol.Scope, Rules: map[operations.Category]operations.Level{operations.WriteFiles: level}}, pol.Version, "manager", time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	rule(operations.Never)
	if _, err := tool.Execute(t.Context(), nil); !errors.Is(err, operations.ErrDenied) {
		t.Fatalf("Never = %v", err)
	}
	rule(operations.Ask)
	if _, err := tool.Execute(t.Context(), nil); !errors.Is(err, operations.ErrDenied) {
		t.Fatalf("unapproved Ask = %v", err)
	}
	q, needed := tool.(native.Approving).Approval(saigetypes.ToolCallPart{Name: "file_write"})
	if !needed || len(q.Approvers) == 0 {
		t.Fatal("Ask did not use native durable approval")
	}
	rule(operations.Allow)
	if _, err := tool.Execute(t.Context(), nil); err != nil || calls != 1 {
		t.Fatalf("allow existing grant = %v, %d", err, calls)
	}
	if _, err := p.Operations.SetControl("t", "a", operations.Control{Paused: true}, 0, "manager", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(t.Context(), nil); !errors.Is(err, operations.ErrPaused) || calls != 1 {
		t.Fatalf("paused tool executed: %v, %d", err, calls)
	}
}

func TestForceAskPreservesNeverAndDoesNotGrantTools(t *testing.T) {
	p, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	req := runtime.Request{Tenant: "t", Meta: map[string]string{operations.MetaForceAsk: "true"}, Definition: native.Revision{Spec: native.Spec{Name: "a"}}}
	if got := p.operationDecision(req, operations.Other); got != operations.Ask {
		t.Fatal(got)
	}
	if _, err := p.Operations.SetPolicy("t", operations.Policy{Scope: operations.Scope{Kind: "workspace"}, Rules: map[operations.Category]operations.Level{operations.Other: operations.Never}}, 0, "manager", time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := p.operationDecision(req, operations.Other); got != operations.Never {
		t.Fatal(got)
	}
	got, err := p.resolveOperations(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Definition.Spec.Tools) != 0 || len(got.Definition.Spec.Access.Spawn) != 0 {
		t.Fatal("tightening added a grant")
	}

}

type fallbackOperationTools struct{ tool saigetypes.Tool }

func (g fallbackOperationTools) Lookup(string) (saigetypes.Tool, error) { return g.tool, nil }
func (g fallbackOperationTools) ToolGroup(name string) (native.ToolGroup, error) {
	return native.ToolGroup{Name: name, Tools: []saigetypes.Tool{g.tool}}, nil
}
func TestFallbackToolGroupsCannotBypassOperations(t *testing.T) {
	p, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	raw := &saigetypes.ToolFunc{Def: saigetypes.ToolDef{Name: "custom__write"}, Fn: func(context.Context, map[string]any) (string, error) { calls++; return "ok", nil }}
	req := runtime.Request{Tenant: "t", Definition: native.Revision{Spec: native.Spec{Name: "a"}}}
	catalog := &operationsCatalog{p: p, req: req, inner: fallbackOperationTools{raw}}
	if _, err := p.Operations.SetPolicy("t", operations.Policy{Scope: operations.Scope{Kind: "workspace"}, Rules: map[operations.Category]operations.Level{operations.ReadContext: operations.Never}}, 0, "manager", time.Now()); err != nil {
		t.Fatal(err)
	}
	group, err := catalog.ToolGroup("mcp:custom")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := group.Tools[0].Execute(t.Context(), nil); !errors.Is(err, operations.ErrDenied) || calls != 0 {
		t.Fatalf("fallback Never bypass: %v, %d", err, calls)
	}
	catalog.req.Meta = map[string]string{operations.MetaForceAsk: "true"}
	group, err = catalog.ToolGroup("mcp:custom:write")
	if err != nil {
		t.Fatal(err)
	}
	if _, needed := group.Tools[0].(native.Approving).Approval(saigetypes.ToolCallPart{Name: "custom__write"}); !needed {
		t.Fatal("fallback ForceAsk bypass")
	}
	catalog.req.Meta = map[string]string{operations.MetaTools: "[]"}
	group, err = catalog.ToolGroup("mcp:custom:write")
	if err != nil || len(group.Tools) != 0 {
		t.Fatalf("fallback tool restriction bypass: %d, %v", len(group.Tools), err)
	}
}

func TestAnApprovedToolRechecksTightenedPolicyAndPause(t *testing.T) {
	p, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	req := runtime.Request{Tenant: "t", RunID: "run", Definition: native.Revision{Spec: native.Spec{Name: "a"}}, Resume: &native.Parked{Answers: map[string]event.Answer{"approved": {Decision: event.DecisionApprove}}}}
	calls := 0
	tool := p.operationTool(req, &saigetypes.ToolFunc{Def: saigetypes.ToolDef{Name: "file_write"}, Fn: func(context.Context, map[string]any) (string, error) { calls++; return "ok", nil }}, operations.WriteFiles)
	ctx := saigetypes.WithToolCallInfo(t.Context(), saigetypes.ToolCallInfo{ID: "approved", Name: "file_write"})
	_, err = p.Operations.SetPolicy("t", operations.Policy{Scope: operations.Scope{Kind: "workspace"}, Rules: map[operations.Category]operations.Level{operations.WriteFiles: operations.Ask}}, 0, "manager", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(ctx, nil); err != nil || calls != 1 {
		t.Fatalf("recorded approval refused: %v", err)
	}
	_, err = p.Operations.SetPolicy("t", operations.Policy{Scope: operations.Scope{Kind: "workspace"}, Rules: map[operations.Category]operations.Level{operations.WriteFiles: operations.Never}}, 1, "manager", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(ctx, nil); !errors.Is(err, operations.ErrDenied) || calls != 1 {
		t.Fatal("approved tool bypassed tightened Never")
	}
	_, err = p.Operations.SetControl("t", "a", operations.Control{Paused: true}, 0, "manager", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(ctx, nil); !errors.Is(err, operations.ErrPaused) || calls != 1 {
		t.Fatal("approved tool bypassed pause")
	}
}

func TestAdmissionKeepsTighteningBeforeRunRecordExists(t *testing.T) {
	p, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	p.admitting.Store("r", admitted{tenant: "t", person: "owner", operationMeta: map[string]string{operations.MetaForceAsk: "true", operations.MetaTools: "[]"}})
	req := runtime.Request{Tenant: "t", RunID: "r", Definition: native.Revision{Spec: native.Spec{Name: "a"}}}
	if got := p.operationDecision(req, operations.WriteFiles); got != operations.Ask {
		t.Fatal("admission lost ForceAsk")
	}
	tools, restricted, err := p.operationTools(req)
	if err != nil || !restricted || len(tools) != 0 {
		t.Fatal("admission lost pinned skill restriction")
	}
	_, err = p.Operations.SetPolicy("t", operations.Policy{Scope: operations.Scope{Kind: "personal", Name: "owner"}, Rules: map[operations.Category]operations.Level{operations.WriteFiles: operations.Never}}, 0, "owner", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got := p.operationDecision(req, operations.WriteFiles); got != operations.Never {
		t.Fatal("admission lost policy identity")
	}
}

func TestAssignmentRecoveryAdvancesPastFullPageOfUnfinishedRuns(t *testing.T) {
	p, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	at := time.Now()
	last := ""
	for i := 0; i <= operations.MaxResults; i++ {
		id := fmt.Sprintf("work-%03d", i)
		a, err := p.Operations.SetAssignment("t", operations.Assignment{ID: id, Agent: "a", Owner: "owner", Goal: "Review work", DoneWhen: "Report saved", State: operations.Requested}, 0, "owner", at)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.Operations.Reserve("t", id, id, a.Version, at); err != nil {
			t.Fatal(err)
		}
		last = id
	}
	tag := wire.NewEvent(wire.Raise{ID: last, Tenant: "t", Agent: "a", Origin: runs.OriginAPI, Text: "work", At: at})
	if err := p.Runs.Record(last, tag, 1); err != nil {
		t.Fatal(err)
	}
	p.Runs.End(last, runs.Completed, "done", "", at)
	p.settleOperations(t.Context())
	p.settleOperations(t.Context())
	a, _ := p.Operations.Assignment("t", last)
	if a.State != operations.Result {
		t.Fatal("a full page of unfinished reservations starved completed work")
	}
}

type operationStrictApprovalProbe struct{ *saigetypes.ToolFunc }

func (*operationStrictApprovalProbe) Approval(saigetypes.ToolCallPart) (native.ApprovalRequest, bool) {
	return native.ApprovalRequest{Prompt: "Stricter tool policy", Approvers: []string{"strict-reviewer"}, SelfApproval: false}, true
}
func TestAddedAskApprovalPreservesExplicitAudienceAndUnderlyingRestrictions(t *testing.T) {
	p, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	req := runtime.Request{Tenant: "t", Definition: native.Revision{Spec: native.Spec{Name: "a", Approvers: []string{"designated-reviewer"}, SelfApproval: true}}}
	if _, err := p.Operations.SetPolicy("t", operations.Policy{Scope: operations.Scope{Kind: "workspace"}, Rules: map[operations.Category]operations.Level{operations.Other: operations.Ask}}, 0, "owner", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Operations.SetControl("t", "a", operations.Control{Manager: "outside-manager"}, 0, "owner", time.Now()); err != nil {
		t.Fatal(err)
	}
	calls := 0
	base := &saigetypes.ToolFunc{Def: saigetypes.ToolDef{Name: "action"}, Fn: func(context.Context, map[string]any) (string, error) { calls++; return "ok", nil }}
	tool := p.operationTool(req, base, operations.Other)
	approval, needed := tool.(native.Approving).Approval(saigetypes.ToolCallPart{Name: "action"})
	if !needed || !approval.SelfApproval || !slices.Equal(approval.Approvers, []string{"designated-reviewer"}) {
		t.Fatalf("declared approval audience widened: %+v", approval)
	}
	fallbackReq := req
	fallbackReq.Definition.Spec.Approvers = nil
	fallbackReq.Definition.Spec.SelfApproval = false
	fallback := p.operationTool(fallbackReq, base, operations.Other)
	fallbackApproval, fallbackNeeded := fallback.(native.Approving).Approval(saigetypes.ToolCallPart{Name: "action"})
	if !fallbackNeeded || fallbackApproval.SelfApproval || !slices.Contains(fallbackApproval.Approvers, "outside-manager") {
		t.Fatalf("missing native audience lost the existing fallback: %+v", fallbackApproval)
	}
	strict := p.operationTool(req, &operationStrictApprovalProbe{ToolFunc: base}, operations.Other)
	approval, needed = strict.(native.Approving).Approval(saigetypes.ToolCallPart{Name: "action"})
	if !needed || approval.SelfApproval || !slices.Equal(approval.Approvers, []string{"strict-reviewer"}) {
		t.Fatalf("underlying tool approval weakened: %+v", approval)
	}
	if _, err := p.Operations.SetPolicy("t", operations.Policy{Scope: operations.Scope{Kind: "workspace"}, Rules: map[operations.Category]operations.Level{operations.Other: operations.Never}}, 1, "owner", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(t.Context(), nil); !errors.Is(err, operations.ErrDenied) || calls != 0 {
		t.Fatalf("self approval bypassed Never: %v, %d", err, calls)
	}
	if _, err := p.Operations.SetControl("t", "", operations.Control{Paused: true}, 0, "owner", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(t.Context(), nil); !errors.Is(err, operations.ErrPaused) || calls != 0 {
		t.Fatalf("self approval bypassed workspace pause: %v, %d", err, calls)
	}
}
