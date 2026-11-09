package platform

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/evals"
	"github.com/urmzd/orchagent/flow/event"
	"github.com/urmzd/orchagent/flow/runtime"
	"github.com/urmzd/orchagent/flow/turn"
	saigetypes "github.com/urmzd/saige/agent/types"
)

func TestBuilderScorecardRequiresExplicitRegressionOverrideAndRejectsStaleCard(t *testing.T) {
	engine := turn.Func(func(_ context.Context, d turn.Pinned, _ turn.Input, _ func(context.Context, event.Body) error) (turn.Result, error) {
		if strings.Contains(d.Spec.SystemPrompt, "omit") {
			return turn.Result{Text: "bad"}, nil
		}
		return turn.Result{Text: "evidence"}, nil
	})
	p, err := New(Config{Workspace: t.TempDir(), Runtime: engine, TestBuiltinProvider: "offline", TestBuiltinModel: "offline"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	_, err = p.Agents.Create(t.Context(), "acme", wire.AgentSpec{Name: "writer", Provider: "offline", Model: "offline", SystemPrompt: "cite evidence"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	req := runtime.Request{Tenant: "acme", Definition: turn.Pinned{Spec: turn.Spec{Name: BuiltinBuilder}}}
	suite := evals.Suite{Name: "evidence", Target: evals.Agent, Subject: "writer", Cases: []evals.Case{{ID: "1", Input: "question", Checks: []evals.Check{{Kind: evals.Contains, Value: "evidence"}}}}}
	raw, _ := json.Marshal(suite)
	write := p.builderWriteEvalTool(req)
	if _, needed := declaredApproval(req, p.toolDeclaration(ToolWriteEval), saigetypes.ToolCallPart{}); !needed {
		t.Fatal("suite save lost approval")
	}
	if _, err = write.Execute(t.Context(), map[string]any{"suite": string(raw)}); err != nil {
		t.Fatalf("new suite could not be written: %v", err)
	}
	cardRaw, err := p.builderPreviewChangeTool(req).Execute(t.Context(), map[string]any{"name": "writer", "system_prompt": "omit evidence"})
	if err != nil {
		t.Fatal(err)
	}
	var card struct {
		Change evals.Change `json:"change"`
	}
	if err = json.Unmarshal([]byte(cardRaw), &card); err != nil {
		t.Fatal(err)
	}
	if card.Change.Passed || !card.Change.Evaluated {
		t.Fatalf("missing measured regression: %+v", card)
	}
	apply := p.builderApplyChangeTool(req)
	if _, needed := declaredApproval(req, p.toolDeclaration(ToolApplyChange), saigetypes.ToolCallPart{}); !needed {
		t.Fatal("apply lost approval")
	}
	args := map[string]any{"name": "writer", "change": card.Change.ID}
	if _, err = apply.Execute(t.Context(), args); err == nil {
		t.Fatal("regression saved without reason")
	}
	args["override_reason"] = "Accepted for the rollout experiment"
	if _, err = apply.Execute(t.Context(), args); err != nil {
		t.Fatalf("approved override refused: %v", err)
	}
	if _, err = apply.Execute(t.Context(), args); err == nil {
		t.Fatal("stale card applied again")
	}
	kept, err := p.Evals.Change("acme", "writer", card.Change.ID)
	if err != nil || kept.OverrideReason == "" {
		t.Fatalf("override missing: %+v %v", kept, err)
	}
}
