package platform_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/saige/agent/agenttest"
	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/fiber/pkg/agentrt"
	"github.com/urmzd/fiber/pkg/agentspec"
	"github.com/urmzd/fiber/pkg/envelope"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/runs"
)

func TestAnAgentsSourcesGroundItsTurnsInItsOwnTenant(t *testing.T) {
	model := &agenttest.ScriptedProvider{Responses: [][]saigetypes.Delta{
		agenttest.TextResponse("Refunds take five days [1]."),
		agenttest.TextResponse("I do not know."),
	}}
	p := startWith(t, platform.Config{Ext: platform.Extensions{Providers: map[string]agentrt.ProviderFunc{
		"scripted": func(agentrt.Spec) (saigetypes.Provider, error) { return model, nil },
	}}})
	ctx := t.Context()
	now := time.Now()
	for _, tn := range []string{tenant, "other"} {
		if _, err := p.Knowledge.Create(tn, "handbook", "", "test", now); err != nil {
			t.Fatal(err)
		}
		spec := agentspec.AgentSpec{Name: "support", Provider: "scripted", Model: "m", SystemPrompt: "Answer.", Sources: []agentspec.Source{{Name: "handbook", URI: "humagen://knowledge/handbook"}}}
		if _, err := p.Agents.Create(ctx, tn, spec, "test"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.Knowledge.Add(tenant, "handbook", "Refunds", "", "Refunds are paid back within five working days.", "test", now); err != nil {
		t.Fatal(err)
	}

	ask := func(tn, id string) runs.Run {
		r, err := p.Invoke(ctx, envelope.Tag{ID: id, Tenant: tn, Agent: "support", Origin: "api", Text: "How long do refunds take?"})
		if err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(10 * time.Second)
		for {
			got, err := p.Runs.Get(tn, r.ID)
			if err == nil && got.Status == runs.Completed {
				return got
			}
			if time.Now().After(deadline) {
				t.Fatalf("run %s: %+v %v", id, got.Status, err)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	ask(tenant, "grounded")
	ask("other", "elsewhere")
	if len(model.Calls) != 2 {
		t.Fatalf("model calls = %d", len(model.Calls))
	}
	first, _ := json.Marshal(model.Calls[0].Messages)
	if !strings.Contains(string(first), "within five working days") {
		t.Errorf("the turn was not given the passage: %s", first)
	}
	second, _ := json.Marshal(model.Calls[1].Messages)
	if strings.Contains(string(second), "five working days") {
		t.Errorf("another tenant's turn saw this tenant's knowledge: %s", second)
	}
}
