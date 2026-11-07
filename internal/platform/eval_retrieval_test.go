package platform

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/evals"
	"github.com/urmzd/orchagent/flow/event"
	"github.com/urmzd/orchagent/flow/turn"
	"github.com/urmzd/orchagent/flow/turn/native"
	"github.com/urmzd/saige/agent/agenttest"
	types "github.com/urmzd/saige/agent/types"
	ragtypes "github.com/urmzd/saige/rag/types"
)

type forbiddenEvalRetriever struct{ t *testing.T }

func (r forbiddenEvalRetriever) Retrieve(context.Context, string) (*ragtypes.AssembledContext, error) {
	r.t.Fatal("live retrieval was called")
	return nil, nil
}

func TestRecordedRetrievalReplaysExactPromptAndCitationsWithoutLiveReads(t *testing.T) {
	model := &agenttest.ScriptedProvider{Responses: [][]types.Delta{agenttest.TextResponse("recorded evidence")}}
	engine := turn.Func(func(context.Context, turn.Pinned, turn.Input, func(context.Context, event.Body) error) (turn.Result, error) {
		t.Fatal("host runtime must not handle recorded retrieval")
		return turn.Result{}, nil
	})
	p, err := New(Config{Workspace: t.TempDir(), Runtime: engine, AgentOptions: []native.Option{native.WithProvider("offline", func(native.Spec) (types.Provider, error) { return model, nil }), native.WithRetriever(forbiddenEvalRetriever{t})}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	snapshot := evals.Retrieval{Input: "Who signs?", Query: "Who signs?", Sources: []string{"handbook"}, Prompt: "EXACT RECORDED PREFIX\n[7] Signoff\nZora approves.", Blocks: []evals.ContextBlock{{Text: "Zora approves.", Citation: "[7]", Provenance: evals.Provenance{DocumentUUID: "original-document", DocumentTitle: "Signoff", SourceURI: "https://example.test/old"}}}}.Seal()
	c := evals.Case{ID: "case", Input: snapshot.Input, Trace: &evals.Trace{Run: "original", Tools: []evals.ToolResult{}, Retrieval: &snapshot}}
	spec := wire.AgentSpec{Name: "writer", Provider: "offline", Model: "offline", SystemPrompt: "Cite evidence.", Sources: []wire.Source{{Name: "handbook"}}}
	prefix, err := p.promptLayers("acme", "")
	if err != nil {
		t.Fatal(err)
	}
	got, err := p.evaluateCandidate(t.Context(), "acme", spec, prefix)(t.Context(), c)
	if err != nil || got != "recorded evidence" {
		t.Fatalf("replay %q %v", got, err)
	}
	calls := model.Requests()
	if len(calls) != 1 {
		t.Fatalf("calls %d", len(calls))
	}
	raw, _ := json.Marshal(calls[0].Messages)
	if !strings.Contains(string(raw), "EXACT RECORDED PREFIX") || !strings.Contains(string(raw), "Zora approves.") {
		t.Fatalf("recorded context missing: %s", raw)
	}
	r, _, err := replayRetrieval(c, []native.Source{{Name: "handbook"}})
	if err != nil {
		t.Fatal(err)
	}
	ac, err := r.Retrieve(t.Context(), c.Input)
	if err != nil {
		t.Fatal(err)
	}
	cites := native.Citations(ac)
	if len(cites) != 1 || cites[0].(event.Citation).Source.DocumentUUID != "original-document" || cites[0].(event.Citation).Label != "[7]" {
		t.Fatalf("citation provenance changed: %+v", cites)
	}
	c.Input = "changed query"
	if _, err = p.evaluateCandidate(t.Context(), "acme", spec, prefix)(t.Context(), c); err == nil {
		t.Fatal("changed input reused snapshot")
	}
	c.Input = snapshot.Input
	c.Trace.Retrieval.Prompt = "tampered"
	if _, err = p.evaluateCandidate(t.Context(), "acme", spec, prefix)(t.Context(), c); err == nil {
		t.Fatal("changed snapshot passed digest")
	}
}
