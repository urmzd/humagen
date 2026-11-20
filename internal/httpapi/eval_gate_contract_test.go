package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/evals"
	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/turn"
)

func TestEvaluationDeniedRevisionReturnsConflictWithoutPrivateDetails(t *testing.T) {
	engine := turn.Func(func(_ context.Context, d turn.Pinned, _ turn.Input, _ func(context.Context, event.Body) error) (turn.Result, error) {
		if strings.Contains(d.Spec.SystemPrompt, "INCOMPLETE REVISION") {
			return turn.Result{}, errors.New("PRIVATE_PROVIDER_DETAIL")
		}
		if strings.Contains(d.Spec.SystemPrompt, "REGRESSED REVISION") {
			return turn.Result{Text: "missing"}, nil
		}
		return turn.Result{Text: "PRIVATE_EXPECTED_EVIDENCE"}, nil
	})
	f := newFixtureWith(t, platform.Config{Runtime: engine})
	token := f.machine(t, "acme", everything...)
	spec := wire.AgentSpec{Name: "guarded", Provider: "offline", Model: "offline", SystemPrompt: "Keep evidence"}
	before, err := f.p.Agents.Create(t.Context(), "acme", spec, "test")
	if err != nil {
		t.Fatal(err)
	}
	if err = f.p.Evals.Put("acme", evals.Suite{Name: "private-suite-marker", Target: evals.Agent, Subject: spec.Name, Cases: []evals.Case{{ID: "private-case-marker", Input: "PRIVATE_INPUT_DETAIL", Checks: []evals.Check{{Kind: evals.Contains, Value: "PRIVATE_EXPECTED_EVIDENCE"}}}}}, false); err != nil {
		t.Fatal(err)
	}
	for _, prompt := range []string{"REGRESSED REVISION", "INCOMPLETE REVISION"} {
		spec.SystemPrompt = prompt
		body, _ := json.Marshal(map[string]any{"spec": spec, "expected_rev": before.Rev})
		res, out := f.do(t, nil, "PUT", "/api/agents/guarded", token, string(body))
		if res.StatusCode != http.StatusConflict {
			t.Fatalf("denied revision = %d %v", res.StatusCode, out)
		}
		message, _ := out["error"].(string)
		if !strings.Contains(message, "evaluation") || strings.Contains(message, "PRIVATE_") || strings.Contains(message, "private-") {
			t.Fatalf("unsafe or unactionable rejection: %q", message)
		}
		held, err := f.p.Catalog().Get(t.Context(), "acme", spec.Name, 0)
		if err != nil || held.Rev != before.Rev || held.Hash != before.Hash {
			t.Fatalf("denied revision was persisted: %+v %v", held, err)
		}
	}
}

// A separate tenant prevents the gate fixture from affecting later contract
// mutations. Its tool-bearing case must fail closed without an isolated replay.
func exerciseEvaluationPolicyContract(t *testing.T, c *contract) {
	t.Helper()
	tenant := "eval-gate-contract"
	token := c.f.machine(t, tenant, everything...)
	spec := wire.AgentSpec{Name: "guarded", Provider: "offline", Model: "offline", SystemPrompt: "Keep evidence", Tools: []string{platform.ToolFileRead}}
	if _, err := c.f.p.Agents.Create(t.Context(), tenant, spec, "test"); err != nil {
		t.Fatal(err)
	}
	if err := c.f.p.Evals.Put(tenant, evals.Suite{Name: "sample", Target: evals.Agent, Subject: spec.Name, Cases: []evals.Case{{ID: "one", Input: "question", Checks: []evals.Check{{Kind: evals.Contains, Value: "evidence"}}}}}, false); err != nil {
		t.Fatal(err)
	}
	settings := c.f.p.Settings(tenant)
	settings.Prompts.Workspace = "Require stronger evidence"
	body, _ := json.Marshal(settings)
	c.call(token, "PUT", "/api/settings", "/api/settings", string(body), http.StatusConflict)
}
