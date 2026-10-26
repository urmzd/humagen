package platform

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/urmzd/fiber/pkg/agentrt"

	"github.com/urmzd/fiber/pkg/runtime"
	"github.com/urmzd/humagen/pkg/operations"
	saigetypes "github.com/urmzd/saige/agent/types"
)

func TestOperationToolFailsClosedAndKeepsPersonScope(t *testing.T) {
	p, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	req := runtime.Request{Tenant: "t", Revision: agentrt.Revision{Spec: agentrt.Spec{Name: "a"}}}
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
	q, needed := tool.(agentrt.Approving).Approval(saigetypes.ToolCallPart{Name: "file_write"})
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
	req := runtime.Request{Tenant: "t", Meta: map[string]string{operations.MetaForceAsk: "true"}, Revision: agentrt.Revision{Spec: agentrt.Spec{Name: "a"}}}
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
	if len(got.Revision.Spec.Tools) != 0 || len(got.Revision.Spec.Access.Spawn) != 0 {
		t.Fatal("tightening added a grant")
	}

}
