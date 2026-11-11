package platform

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/runtime"
	"github.com/urmzd/saige/flow/turn"
)

func TestBuilderEvalHistoryDoesNotExposeRemovedPrivateReplay(t *testing.T) {
	policy, err := auth.ParsePolicy("history", `role reader { allow read on * } grant reader to subject "bob" grant reader to subject "ana"`)
	if err != nil {
		t.Fatal(err)
	}
	p, err := New(Config{Workspace: t.TempDir(), Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	if _, err := p.Agents.Create(t.Context(), "acme", wire.AgentSpec{Name: "writer", Provider: "offline", Model: "offline", SystemPrompt: "Write"}, "test"); err != nil {
		t.Fatal(err)
	}
	privateEvalTraceRun(t, p, "original", "ana")
	tag := wire.NewEvent(wire.Raise{ID: "builder", Origin: runs.OriginAPI, Tenant: "acme", Agent: BuiltinBuilder, Actor: event.Actor{ID: "bob", User: "bob"}, At: time.Now()})
	if err := p.Runs.Record("builder", tag, 1); err != nil {
		t.Fatal(err)
	}
	req := runtime.Request{Tenant: "acme", RunID: "builder", Definition: turn.Pinned{Spec: turn.Spec{Name: BuiltinBuilder}}}
	suite := evalHistorySuite()
	suite.Subject = "writer"
	if err := p.builderReplaySuiteAllowed(req, suite); !errors.Is(err, ErrForbidden) {
		t.Fatalf("tool-only trace skipped original-run access: %v", err)
	}
	// Even the source's owner cannot publish its private tool bytes to the
	// builder's shared audience through the current suite definition.
	ownerTag := wire.NewEvent(wire.Raise{ID: "owner-builder", Origin: runs.OriginAPI, Tenant: "acme", Agent: BuiltinBuilder, Actor: event.Actor{ID: "ana", User: "ana"}, At: time.Now()})
	if err := p.Runs.Record("owner-builder", ownerTag, 1); err != nil {
		t.Fatal(err)
	}
	ownerReq := req
	ownerReq.RunID = "owner-builder"
	owner, err := p.builderPerson(ownerReq)
	if err != nil || !p.EvalSuiteSourcesVisible(owner, suite) {
		t.Fatalf("owner fixture lacks source authority: %+v %v", owner, err)
	}
	if err := p.builderReplaySuiteAllowed(ownerReq, suite); !errors.Is(err, ErrForbidden) {
		t.Fatalf("private tool bytes crossed the builder audience: %v", err)
	}
	if err := p.Evals.Put("acme", suite, false); err != nil {
		t.Fatal(err)
	}
	if _, err := p.builderReadEvalsTool(ownerReq).Execute(t.Context(), map[string]any{"name": "writer"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("current private suite was serialized to the builder: %v", err)
	}
	finishEvalHistory(t, p, suite, "PRIVATE TOOL RESULT")
	suite.Cases[0].Trace = nil
	if err := p.Evals.Put("acme", suite, true); err != nil {
		t.Fatal(err)
	}
	finishEvalHistory(t, p, suite, "public answer")
	answer, err := p.builderReadEvalsTool(req).Execute(t.Context(), map[string]any{"name": "writer"})
	if err != nil || strings.Contains(answer, "PRIVATE TOOL RESULT") || !strings.Contains(answer, "public answer") {
		t.Fatalf("builder history = %s, %v", answer, err)
	}
}
