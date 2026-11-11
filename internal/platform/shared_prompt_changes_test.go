package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/evals"
	"github.com/urmzd/humagen/pkg/prompts"
	types "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/runtime"
	"github.com/urmzd/saige/flow/turn"
)

func TestSharedPromptOverrideRequiresCompletedScoresReasonAndIndependentApproval(t *testing.T) {
	engine := turn.Func(func(_ context.Context, def turn.Pinned, _ turn.Input, _ func(context.Context, event.Body) error) (turn.Result, error) {
		if strings.Contains(def.Spec.SystemPrompt, "cannot execute") {
			return turn.Result{}, fmt.Errorf("provider unavailable")
		}
		if strings.Contains(def.Spec.SystemPrompt, "omit evidence") {
			return turn.Result{Text: "bad"}, nil
		}
		return turn.Result{Text: "evidence"}, nil
	})
	p, err := New(Config{Workspace: t.TempDir(), Runtime: engine})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	for _, name := range []string{"writer", "reviewer"} {
		if _, err = p.Agents.Create(t.Context(), "acme", wire.AgentSpec{Name: name, Provider: "offline", Model: "offline", SystemPrompt: "Cite evidence"}, "test"); err != nil {
			t.Fatal(err)
		}
		suite := evals.Suite{Name: name + "-proof", Target: evals.Agent, Subject: name, Cases: []evals.Case{}}
		for i := range 6 {
			suite.Cases = append(suite.Cases, evals.Case{ID: fmt.Sprint(i), Input: "question", Checks: []evals.Check{{Kind: evals.Contains, Value: "evidence"}}})
		}
		if err = p.Evals.Put("acme", suite, false); err != nil {
			t.Fatal(err)
		}
	}
	config := prompts.Config{Workspace: "omit evidence"}
	card, err := p.prepareSharedPromptChange(t.Context(), "acme", config)
	if err != nil {
		t.Fatal(err)
	}
	if card.Passed || !card.Evaluated || len(card.Scores) != 2 || len(card.Scores[0].Results) != 5 {
		t.Fatalf("missing sampled regression %+v", card)
	}
	policy, ok := declaredApproval(runtime.Request{Tenant: "acme"}, p.toolDeclaration(ToolApplyWorkspacePrompt), types.ToolCallPart{Arguments: map[string]any{"change": card.ID, "override_reason": "Accepted tradeoff"}})
	if !ok || policy.SelfApproval || len(policy.Approvers) != 1 || policy.Approvers[0] != "role:admin" || !strings.Contains(policy.Prompt, "Override") {
		t.Fatalf("override lost independent admin approval: %+v", policy)
	}
	if err = p.applySharedPromptChange(t.Context(), "acme", card.ID, ""); !errors.Is(err, evals.ErrRegression) {
		t.Fatalf("missing reason: %v", err)
	}
	if err = p.settings.update("acme", func(s *storedSettings) error { s.WorkspaceName = "Concurrent name"; return nil }); err != nil {
		t.Fatal(err)
	}
	if err = p.applySharedPromptChange(t.Context(), "acme", card.ID, "Accepted tradeoff"); err != nil {
		t.Fatal(err)
	}
	if got := p.Settings("acme"); got.Prompts != config || got.WorkspaceName != "Concurrent name" {
		t.Fatalf("settings changed incorrectly: %+v", got)
	}
	kept, err := p.Evals.Change("acme", BuiltinEntry, card.ID)
	if err != nil || kept.OverrideReason != "Accepted tradeoff" {
		t.Fatalf("override not retained: %+v %v", kept, err)
	}
	if err = p.applySharedPromptChange(t.Context(), "acme", card.ID, "Again"); !errors.Is(err, wire.ErrConflict) {
		t.Fatalf("stale config applied: %v", err)
	}
	broken, err := p.prepareSharedPromptChange(t.Context(), "acme", prompts.Config{Workspace: "cannot execute"})
	if err != nil {
		t.Fatal(err)
	}
	if broken.Evaluated {
		t.Fatal("unexecutable eval counted complete")
	}
	if err = p.applySharedPromptChange(t.Context(), "acme", broken.ID, "Override failure"); !errors.Is(err, evals.ErrRegression) {
		t.Fatalf("incomplete evaluation overridden: %v", err)
	}
	stale, err := p.prepareSharedPromptChange(t.Context(), "acme", prompts.Config{Workspace: "better instructions"})
	if err != nil {
		t.Fatal(err)
	}
	suite, err := p.Evals.Get("acme", "writer-proof")
	if err != nil {
		t.Fatal(err)
	}
	suite.Cases[5].Input = "changed unsampled case"
	if err = p.Evals.Put("acme", suite, true); err != nil {
		t.Fatal(err)
	}
	if err = p.applySharedPromptChange(t.Context(), "acme", stale.ID, "Reviewed"); !errors.Is(err, wire.ErrConflict) {
		t.Fatalf("changed full suite did not invalidate sampled card: %v", err)
	}
}
