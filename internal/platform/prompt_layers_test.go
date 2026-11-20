package platform

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/evals"
	"github.com/urmzd/humagen/pkg/prompts"
	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/runtime"
	"github.com/urmzd/saige/flow/turn"
)

func TestRunUsesItsPinnedPromptAfterWorkspaceChanges(t *testing.T) {
	p, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	s := p.Settings("acme")
	s.Prompts.Workspace = "Use the old terminology."
	if err := p.SetSettings(t.Context(), "acme", s); err != nil {
		t.Fatal(err)
	}
	tag, err := p.pinPromptLayers(wire.NewEvent(wire.Raise{ID: "r", Tenant: "acme", Agent: "writer", Text: "Hello"}), "")
	if err != nil {
		t.Fatal(err)
	}
	s.Prompts.Workspace = "Use the new terminology."
	if err := p.SetSettings(t.Context(), "acme", s); err != nil {
		t.Fatal(err)
	}
	req := runtime.Request{Meta: wire.Meta(tag), Definition: turn.Pinned{Spec: turn.Spec{SystemPrompt: "Write clearly."}}}
	got, err := withPromptLayers(req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Definition.Spec.SystemPrompt, "old terminology") || strings.Contains(got.Definition.Spec.SystemPrompt, "new terminology") || !strings.HasSuffix(got.Definition.Spec.SystemPrompt, "Write clearly.") {
		t.Fatalf("unpinned prompt: %s", got.Definition.Spec.SystemPrompt)
	}
	if got.Meta[prompts.MetaSnapshot] != "" || req.Meta[prompts.MetaSnapshot] == "" {
		t.Fatal("private pin leaked or original mutated")
	}
}

func TestBuilderRevisionGateChecksCandidateBeforePublishing(t *testing.T) {
	engine := turn.Func(func(_ context.Context, def turn.Pinned, _ turn.Input, _ func(context.Context, event.Body) error) (turn.Result, error) {
		if strings.Contains(def.Spec.SystemPrompt, "OMIT EVIDENCE") {
			return turn.Result{Text: "Unknown"}, nil
		}
		return turn.Result{Text: "Verified evidence"}, nil
	})
	p, err := New(Config{Workspace: t.TempDir(), Runtime: engine, TestBuiltinProvider: "offline", TestBuiltinModel: "offline"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	_, err = p.Agents.Create(t.Context(), "acme", wire.AgentSpec{Name: "writer", Provider: "offline", Model: "offline", SystemPrompt: "Include evidence."}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Evals.Put("acme", evals.Suite{Name: "writer-evidence", Target: evals.Agent, Subject: "writer", Cases: []evals.Case{{ID: "case1", Input: "What happened?", Checks: []evals.Check{{Kind: evals.Contains, Value: "evidence"}}}}}, false); err != nil {
		t.Fatal(err)
	}
	_, _, err = p.saveAgent(t.Context(), "acme", map[string]any{"name": "writer", "description": "Writer", "system_prompt": "OMIT EVIDENCE"})
	if !errors.Is(err, evals.ErrRegression) {
		t.Fatalf("regression saved: %v", err)
	}
	rev, err := p.Catalog().Get(t.Context(), "acme", "writer", 0)
	if err != nil || rev.Rev != 1 || rev.Spec.SystemPrompt != "Include evidence." {
		t.Fatalf("failed gate published a version: %+v %v", rev, err)
	}
	_, _, err = p.saveAgent(t.Context(), "acme", map[string]any{"name": "writer", "description": "Writer", "system_prompt": "Always cite evidence."})
	if err != nil {
		t.Fatalf("passing revision refused: %v", err)
	}
}
