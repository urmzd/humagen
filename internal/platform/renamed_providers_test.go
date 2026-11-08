package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/urmzd/orchagent/flow/runtime"
	"github.com/urmzd/orchagent/flow/turn/native"
	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/humagen/internal/wire"
)

// storePolicies writes policies into tenant's stored settings as an older
// server would have, without the checks a write makes now.
func storePolicies(t *testing.T, p *Platform, list ...ModelPolicy) {
	t.Helper()
	if err := p.settings.update("acme", func(v *storedSettings) error {
		v.Settings = p.Settings("acme")
		v.ModelPolicies = list
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

var geminiPolicy = ModelPolicy{Name: "default", Models: []wire.ModelRef{
	{Provider: KindAnthropic, Model: "claude-sonnet-5-5"},
	{Provider: "gemini", Model: "gemini-3-flash-preview"},
}}

// Data stored when Google's provider was named gemini reads as google: a
// workspace's model policies, so the write every start makes succeeds and
// keeps them as google, an agent's chain at run time, and a chain written
// back from the console.
func TestStoredGeminiReadsAsGoogle(t *testing.T) {
	p, _ := configured(t, Config{})
	ctx := context.Background()
	storePolicies(t, p, geminiPolicy)

	if got := p.Settings("acme").ModelPolicies[0].Models[1].Provider; got != KindGoogle {
		t.Fatalf("a stored policy reads gemini as %q", got)
	}
	set := p.Settings("acme")
	set.WorkspaceName = "Acme"
	if err := p.SetSettings(ctx, "acme", set); err != nil {
		t.Fatalf("the write a start makes = %v", err)
	}
	raw, err := p.settings.get("acme")
	if err != nil || raw.ModelPolicies[0].Models[1].Provider != KindGoogle {
		t.Fatalf("stored after the write = %+v, %v", raw.ModelPolicies, err)
	}

	if _, err := p.SetModelProvider(ctx, admin("acme"), KindGoogle, ModelProviderInput{Kind: KindGoogle, Key: "sk"}); err != nil {
		t.Fatal(err)
	}
	req, err := p.resolveModel(runtime.Request{Tenant: "acme", Definition: native.Revision{Name: "old", Version: "1.0.0", Spec: native.Spec{
		Name: "old", Provider: "gemini", Model: "gemini-3-flash-preview",
	}}})
	if err != nil {
		t.Fatalf("a run of an agent stored on gemini = %v", err)
	}
	if spec := req.Definition.Spec; spec.Model != "gemini-3-flash-preview" || spec.Provider != p.runName("acme", KindGoogle) {
		t.Fatalf("the run is on %s/%s", spec.Provider, spec.Model)
	}

	written, err := p.SpecWrittenBy(ctx, admin("acme"), nil, wire.AgentSpec{
		Name: "old", Provider: KindAnthropic, Model: "claude-sonnet-5-5", SystemPrompt: "Answer.",
		Fallbacks: []wire.ModelRef{{Provider: "gemini", Model: "gemini-3-flash-preview"}},
	})
	if err != nil || written.Fallbacks[0].Provider != KindGoogle {
		t.Fatalf("a chain written back = %+v, %v", written.Fallbacks, err)
	}
}

// A deployment that still declares gemini keeps it: nothing is renamed.
func TestADeclaredGeminiStaysGemini(t *testing.T) {
	gemini := func(native.Spec) (saigetypes.Provider, error) { return nil, errors.New("not called") }
	p, _ := configured(t, Config{Ext: Extensions{Providers: map[string]native.ProviderFunc{"gemini": gemini}}})
	storePolicies(t, p, geminiPolicy)
	if got := p.Settings("acme").ModelPolicies[0].Models[1].Provider; got != "gemini" {
		t.Fatalf("a declared gemini reads as %q", got)
	}
}

// A policy stored on a provider the deployment no longer has, and that was
// never renamed, does not fail a settings write either; changing it, or
// adding one like it, is checked as ever.
func TestAKeptPolicyOnAProviderSinceGoneDoesNotFailASettingsWrite(t *testing.T) {
	p, _ := configured(t, Config{})
	ctx := context.Background()
	gone := ModelPolicy{Name: "default", Models: []wire.ModelRef{{Provider: KindAnthropic, Model: "claude-sonnet-5-5"}, {Provider: "retired", Model: "old-model"}}}
	storePolicies(t, p, gone)
	set := p.Settings("acme")
	set.WorkspaceName = "Acme"
	if err := p.SetSettings(ctx, "acme", set); err != nil {
		t.Fatalf("a write keeping the policy = %v", err)
	}
	changed := p.Settings("acme")
	changed.ModelPolicies[0].Description = "Edited."
	if err := p.SetSettings(ctx, "acme", changed); !errors.Is(err, ErrModelPolicy) {
		t.Fatalf("changing the policy = %v", err)
	}
	added := p.Settings("acme")
	added.ModelPolicies = append(added.ModelPolicies, ModelPolicy{Name: "other", Models: gone.Models})
	if err := p.SetSettings(ctx, "acme", added); !errors.Is(err, ErrModelPolicy) {
		t.Fatalf("adding one like it = %v", err)
	}
}
