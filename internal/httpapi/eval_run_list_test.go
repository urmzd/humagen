package httpapi

import (
	"net/http"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/saige/flow/event"
)

func TestRunListExcludesSetupAndEvaluationActivity(t *testing.T) {
	f := newFixture(t)
	token := f.machineAs(t, "ana", "acme", everything...)
	if res, out := f.do(t, nil, "POST", "/api/agents", token, helperSpec); res.StatusCode != http.StatusCreated {
		t.Fatalf("create agent: %d %v", res.StatusCode, out)
	}
	actor := event.Actor{ID: "ana", User: "ana"}
	normal, err := f.p.Converse(t.Context(), "acme", platform.Turn{ID: "ordinary-work", Agent: "helper", Text: "Help me", Actor: actor})
	if err != nil {
		t.Fatal(err)
	}
	dry, err := f.p.Converse(t.Context(), "acme", platform.Turn{ID: "setup-check", Agent: "helper", Text: "Check setup", Actor: actor, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	eval, err := f.p.Invoke(t.Context(), wire.NewEvent(wire.Raise{ID: "evaluation-check", Tenant: "acme", Agent: "helper", Origin: platform.OriginEval, Text: "Evaluate", Actor: actor}))
	if err != nil {
		t.Fatal(err)
	}
	res, out := f.do(t, nil, "GET", "/api/runs", token, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("list runs: %d %v", res.StatusCode, out)
	}
	found := false
	for _, item := range out["runs"].([]any) {
		id := item.(map[string]any)["id"]
		if id == dry.ID || id == eval.ID {
			t.Fatalf("setup/evaluation leaked into ordinary history: %v", out)
		}
		found = found || id == normal.ID
	}
	if !found {
		t.Fatalf("ordinary work disappeared: %v", out)
	}
}
