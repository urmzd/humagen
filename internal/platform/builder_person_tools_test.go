package platform

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/evals"
	types "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/runtime"
	flowturn "github.com/urmzd/saige/flow/turn"
	"github.com/urmzd/saige/flow/turn/native"
)

func TestBuilderPersonalToolsRequireExplicitReviewedChange(t *testing.T) {
	engine := flowturn.Func(func(_ context.Context, d flowturn.Pinned, _ flowturn.Input, _ func(context.Context, event.Body) error) (flowturn.Result, error) {
		if slices.Contains(d.Spec.Tools, "person_tools") || slices.Contains(d.Spec.Tools, personToolsGrant) {
			return flowturn.Result{Text: "blocked"}, nil
		}
		return flowturn.Result{Text: "safe"}, nil
	})
	p, err := New(Config{Workspace: t.TempDir(), Runtime: engine, BuiltinProvider: "offline", BuiltinModel: "offline"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	before := wire.AgentSpec{Name: "writer", Provider: "offline", Model: "offline", SystemPrompt: "Write clearly"}
	if _, err = p.Agents.Create(t.Context(), "t", before, "test"); err != nil {
		t.Fatal(err)
	}
	if err = p.Evals.Put("t", evals.Suite{Name: "safe", Target: evals.Agent, Subject: before.Name, Cases: []evals.Case{{ID: "one", Input: "Write", Checks: []evals.Check{{Kind: evals.Contains, Value: "safe"}}}}}, false); err != nil {
		t.Fatal(err)
	}
	req := runtime.Request{Tenant: "t", Definition: flowturn.Pinned{Spec: flowturn.Spec{Name: BuiltinBuilder}}}
	preview := func(tools string) evals.Change {
		t.Helper()
		raw, err := p.builderPreviewChangeTool(req).Execute(t.Context(), map[string]any{"name": before.Name, "system_prompt": before.SystemPrompt, "tools": tools})
		if err != nil {
			t.Fatal(err)
		}
		var card struct {
			Change evals.Change `json:"change"`
			Tools  []string     `json:"tools"`
		}
		if err = json.Unmarshal([]byte(raw), &card); err != nil {
			t.Fatal(err)
		}
		if tools != "" && !slices.Equal(card.Tools, []string{"person_tools"}) {
			t.Fatalf("explicit grant missing from review card: %s", raw)
		}
		return card.Change
	}
	newAgent, _, _, err := p.prepareBuilderAgent(t.Context(), "t", map[string]any{"name": "new-agent", "system_prompt": "Write"})
	if err != nil || len(newAgent.Tools) != 0 {
		t.Fatalf("new agents must default to no connection tools: %+v %v", newAgent.Tools, err)
	}
	card := preview(personToolsGrant)
	if card.Passed || !card.Evaluated {
		t.Fatalf("tool grant skipped evals: %+v", card)
	}
	current, _ := p.Agents.Get(t.Context(), "t", before.Name, 0)
	if len(current.Spec.Tools) != 0 {
		t.Fatal("preview granted personal tools")
	}
	apply := p.builderApplyChangeTool(req)
	approval, ok := apply.(native.Approving)
	if !ok {
		t.Fatal("grant bypasses person approval")
	}
	args := map[string]any{"name": before.Name, "change": card.ID}
	question, required := approval.Approval(types.ToolCallPart{Arguments: args})
	if !required || len(question.Approvers) == 0 || !strings.Contains(question.Prompt, "separately authorized and reviewed") {
		t.Fatalf("approval hides the connection grant: %+v", question)
	}
	if _, err = apply.Execute(t.Context(), args); err == nil {
		t.Fatal("tool grant bypassed regression gate")
	}
	args["override_reason"] = "Explicitly approved for a controlled rollout"
	if _, err = apply.Execute(t.Context(), args); err != nil {
		t.Fatal(err)
	}
	current, _ = p.Agents.Get(t.Context(), "t", before.Name, 0)
	if !slices.Equal(current.Spec.Tools, []string{"person_tools"}) {
		t.Fatalf("approved grant missing: %+v", current.Spec.Tools)
	}
	card = preview("")
	if _, err = apply.Execute(t.Context(), map[string]any{"name": before.Name, "change": card.ID}); err != nil {
		t.Fatal(err)
	}
	current, _ = p.Agents.Get(t.Context(), "t", before.Name, 0)
	if len(current.Spec.Tools) != 0 {
		t.Fatal("explicit removal retained personal tools")
	}
	other := req
	other.Definition.Spec.Name = "writer"
	if _, err = p.builderPreviewChangeTool(other).Execute(t.Context(), map[string]any{"name": before.Name, "system_prompt": before.SystemPrompt, "tools": "person_tools"}); err == nil {
		t.Fatal("an ordinary agent can grant connection tools")
	}
}
