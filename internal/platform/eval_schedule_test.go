package platform

import (
	"context"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/evals"
	"github.com/urmzd/saige/agent/agenttest"
	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/turn"
	"github.com/urmzd/saige/flow/turn/native"
)

func TestScheduledRecordedCasesRunOnceWithinTheirInterval(t *testing.T) {
	engine := turn.Func(func(context.Context, turn.Pinned, turn.Input, func(context.Context, event.Body) error) (turn.Result, error) {
		return turn.Result{Text: "evidence"}, nil
	})
	p, suite, _ := scheduledReplayFixture(t, engine)
	if err := p.Evals.Put("acme", suite, false); err != nil {
		t.Fatal(err)
	}
	if err := p.runDueEvals(t.Context()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		runs, err := p.Evals.Runs("acme", suite.Name)
		if err != nil {
			t.Fatal(err)
		}
		if len(runs) == 1 && runs[0].Status != evals.Running {
			if runs[0].Passed != 1 || runs[0].Rev != 1 {
				t.Fatalf("scheduled result: %+v", runs)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("scheduled evaluation did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	if err := p.runDueEvals(t.Context()); err != nil {
		t.Fatal(err)
	}
	runs, err := p.Evals.Runs("acme", suite.Name)
	if err != nil || len(runs) != 1 {
		t.Fatalf("schedule duplicated run: %+v %v", runs, err)
	}
}
func TestCandidateToolReplayUsesRecordedResultsWithoutLiveTools(t *testing.T) {
	p, err := New(Config{Workspace: t.TempDir(), AgentOptions: []native.Option{native.WithScript(agenttest.ToolCallResponse("search-1", "search", map[string]any{"q": "original"}), agenttest.TextResponse("evidence"))}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	prefix, err := p.promptLayers("acme", "")
	if err != nil {
		t.Fatal(err)
	}
	spec := wire.AgentSpec{Name: "writer", Provider: "offline", Model: "offline", SystemPrompt: "Use search", Tools: []string{"search"}}
	c := evals.Case{ID: "c", Input: "question", Trace: &evals.Trace{Run: "source", Tools: []evals.ToolResult{{Name: "search", Arguments: []byte(`{"q":"original"}`), Result: "recorded evidence"}}}}
	out, err := p.evaluateCandidate(t.Context(), "acme", spec, prefix)(t.Context(), c)
	if err != nil || out != "evidence" {
		t.Fatalf("isolated replay: %q %v", out, err)
	}
	sourced := spec
	sourced.Sources = []wire.Source{{Name: "private-source"}}
	if _, err = p.evaluateCandidate(t.Context(), "acme", sourced, prefix)(t.Context(), c); err == nil {
		t.Fatal("missing retrieval context passed")
	}
	c.Trace.Tools[0].Arguments = []byte(`{"q":"different"}`)
	if _, err = p.evaluateCandidate(t.Context(), "acme", spec, prefix)(t.Context(), c); err == nil {
		t.Fatal("changed tool arguments passed replay")
	}
}
