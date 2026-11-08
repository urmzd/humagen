package platform

import (
	"context"
	"testing"

	"github.com/urmzd/orchagent/flow/runtime"
	"github.com/urmzd/orchagent/flow/turn/native"

	"github.com/urmzd/humagen/internal/wire"
)

// An agent's effort reaches its own primary model when that model takes it,
// and is dropped when the run moves to another model or the model takes no
// such effort, so the run answers at the model's default instead of failing.
func TestEffortReachesOnlyAPrimaryThatTakesIt(t *testing.T) {
	p, _ := configured(t, Config{})
	ctx := context.Background()
	for _, kind := range []string{KindAnthropic, KindOpenAI} {
		if _, err := p.SetModelProvider(ctx, admin("acme"), kind, ModelProviderInput{Kind: kind, Key: "sk"}); err != nil {
			t.Fatal(err)
		}
	}
	effortOf := func(spec native.Spec) string {
		t.Helper()
		req, err := p.resolveModel(runtime.Request{Tenant: "acme", Definition: native.Revision{Name: spec.Name, Version: "1.0.0", Spec: spec}})
		if err != nil {
			t.Fatal(err)
		}
		return req.Definition.Spec.Effort
	}
	sonnet := native.Spec{Name: "thinker", Provider: KindAnthropic, Model: "claude-sonnet-5-5", Effort: "high"}
	if got := effortOf(sonnet); got != "high" {
		t.Fatalf("on a model that takes it, effort = %q", got)
	}
	unknown := sonnet
	unknown.Provider, unknown.Model = KindOpenAI, "claude-sonnet-5-5"
	if got := effortOf(unknown); got != "" {
		t.Fatalf("on a model the catalog does not know, effort = %q", got)
	}
	// Google is not enabled, so the chain is cut to its fallback, which the
	// effort was not set for.
	moved := native.Spec{Name: "thinker", Provider: KindGoogle, Model: "gemini-3-flash-preview", Effort: "high", Fallbacks: []native.ModelRef{{Provider: KindAnthropic, Model: "claude-sonnet-5-5"}}}
	if got := effortOf(moved); got != "" {
		t.Fatalf("on a fallback, effort = %q", got)
	}
}

// A turn runs with the effort its pinned revision carries.
func TestATurnTakesItsRevisionsEffort(t *testing.T) {
	p, _ := configured(t, Config{})
	ctx := context.Background()
	if _, err := p.SetModelProvider(ctx, admin("acme"), KindAnthropic, ModelProviderInput{Kind: KindAnthropic, Key: "sk"}); err != nil {
		t.Fatal(err)
	}
	rev, err := p.Agents.Create(ctx, "acme", wire.AgentSpec{Name: "thinker", Provider: KindAnthropic, Model: "claude-sonnet-5-5", SystemPrompt: "Think.", Effort: "medium"}, "ada")
	if err != nil {
		t.Fatal(err)
	}
	req := p.withEffort(runtime.Request{Tenant: "acme", Definition: native.Revision{Name: "thinker", Version: wire.VersionOf(rev.Rev), Spec: native.Spec{Name: "thinker", Provider: KindAnthropic, Model: "claude-sonnet-5-5"}}})
	if req.Definition.Spec.Effort != "medium" {
		t.Fatalf("effort = %q", req.Definition.Spec.Effort)
	}
}
