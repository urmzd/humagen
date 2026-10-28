package platform_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/saige/agent/agenttest"
	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/humagen/fiber/pkg/event"
	"github.com/urmzd/humagen/fiber/pkg/turn/native"
	"github.com/urmzd/humagen/internal/wire"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
)

func TestAnAgentsSourcesGroundItsTurnsInItsOwnTenant(t *testing.T) {
	model := &agenttest.ScriptedProvider{Responses: [][]saigetypes.Delta{
		agenttest.TextResponse("Refunds take five days [1]."),
		agenttest.TextResponse("I do not know."),
	}}
	p := startWith(t, platform.Config{Ext: platform.Extensions{Providers: map[string]native.ProviderFunc{
		"scripted": func(native.Spec) (saigetypes.Provider, error) { return model, nil },
	}}})
	ctx := t.Context()
	now := time.Now()
	for _, tn := range []string{tenant, "other"} {
		if _, err := p.Knowledge.Create(tn, "handbook", "", "test", now); err != nil {
			t.Fatal(err)
		}
		spec := wire.AgentSpec{Name: "support", Provider: "scripted", Model: "m", SystemPrompt: "Answer.", Sources: []wire.Source{{Name: "handbook", URI: "humagen://knowledge/handbook"}}}
		if _, err := p.Agents.Create(ctx, tn, spec, "test"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.Knowledge.Add(tenant, "handbook", "Refunds", "", "Refunds are paid back within five working days.", "test", now); err != nil {
		t.Fatal(err)
	}

	ask := func(tn, id string) runs.Run {
		r, err := p.Invoke(ctx, wire.NewEvent(wire.Raise{ID: id, Tenant: tn, Agent: "support", Origin: "api", Text: "How long do refunds take?"}))
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

func TestScopedSourcesGroundRealTurnsWithoutPersistingInternalHandles(t *testing.T) {
	model := &agenttest.ScriptedProvider{Responses: [][]saigetypes.Delta{agenttest.TextResponse("Use the release runbook [1].")}}
	p := startWith(t, platform.Config{Ext: platform.Extensions{Providers: map[string]native.ProviderFunc{"scripted": func(native.Spec) (saigetypes.Provider, error) { return model, nil }}}})
	ctx := t.Context()
	at := time.Now()
	if _, err := p.CreateProject(ctx, tenant, projects.Writer{By: "owner", Govern: true}, platform.ProjectInput{Name: "Secret", Members: []projects.Entry{{Who: "owner", Role: projects.Owner}}}); err != nil {
		t.Fatal(err)
	}
	_, _ = p.Knowledge.Create(tenant, "private", "", "owner", at)
	if _, err := p.Knowledge.Configure(tenant, "private", knowledge.SourceConfig{Mode: knowledge.Indexed, Provider: "manual", Owner: "owner", Project: "Secret"}, at); err != nil {
		t.Fatal(err)
	}
	_, _ = p.Knowledge.Add(tenant, "private", "Runbook", "", "release canary instructions", "owner", at)
	spec := wire.AgentSpec{Name: "worker", Provider: "scripted", Model: "m", SystemPrompt: "Answer.", Sources: []wire.Source{{Name: "private", URI: "knowledge://private"}}}
	if _, err := p.Agents.Create(ctx, tenant, spec, "owner"); err != nil {
		t.Fatal(err)
	}
	started, err := p.Invoke(ctx, wire.NewEvent(wire.Raise{ID: "private-grounding", Tenant: tenant, Agent: "worker", Origin: "api", Actor: event.Actor{ID: "owner", User: "owner"}, Text: "What are the release instructions?", Context: map[string]string{runs.MetaProject: "Secret"}}))
	if err != nil {
		t.Fatal(err)
	}
	completed := await(t, p, started.ID, runs.Completed)
	if len(model.Calls) != 1 {
		t.Fatal("model call missing")
	}
	request, _ := json.Marshal(model.Calls[0])
	if !strings.Contains(string(request), "release canary instructions") || strings.Contains(string(request), "sourcectx-") {
		t.Fatalf("model request did not receive only authorized source text: %s", request)
	}
	saved, err := p.Agents.Get(ctx, tenant, "worker", 0)
	if err != nil {
		t.Fatal(err)
	}
	revision, _ := json.Marshal(saved)
	tag, _ := json.Marshal(completed.Tag())
	record, _ := json.Marshal(completed)
	for _, raw := range [][]byte{revision, tag, record} {
		if strings.Contains(string(raw), "sourcectx-") {
			t.Fatal("internal grounding handle persisted")
		}
	}
}
