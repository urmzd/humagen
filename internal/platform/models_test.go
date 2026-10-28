package platform

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/urmzd/humagen/fiber/pkg/runtime"
	"github.com/urmzd/humagen/fiber/pkg/turn/native"
	"github.com/urmzd/humagen/internal/wire"
)

func TestResolveModelPutsThePolicyInPlace(t *testing.T) {
	p, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	set := p.Settings("acme")
	set.ModelPolicies = []ModelPolicy{{
		Name:       "steady",
		Models:     []wire.ModelRef{{Provider: "anthropic", Model: "claude-sonnet-5-5"}, {Provider: "ollama", Model: "llama3.2"}},
		FallbackOn: []string{"rate_limit", "unavailable"},
	}}
	if err := p.SetSettings(ctx, "acme", set); err != nil {
		t.Fatal(err)
	}

	req := runtime.Request{Tenant: "acme", Definition: native.Revision{Spec: native.Spec{Name: "a", Provider: PolicyProvider, Model: "steady"}}}
	got, err := p.resolveModel(req)
	if err != nil {
		t.Fatal(err)
	}
	s := got.Definition.Spec
	if s.Provider != "anthropic" || s.Model != "claude-sonnet-5-5" || len(s.Fallbacks) != 1 || s.Fallbacks[0].Model != "llama3.2" ||
		!slices.Equal(s.FallbackOn, []string{"rate_limit", "unavailable"}) {
		t.Fatalf("resolved = %+v", s)
	}
	if req.Definition.Spec.Provider != PolicyProvider {
		t.Fatal("the caller's request was changed in place")
	}

	// An agent that names its own model is left alone; another tenant's policy is not found.
	own := runtime.Request{Tenant: "acme", Definition: native.Revision{Spec: native.Spec{Name: "b", Provider: "offline", Model: "offline"}}}
	if got, _ := p.resolveModel(own); got.Definition.Spec.Provider != "offline" {
		t.Fatalf("own model changed: %+v", got.Definition.Spec)
	}
	other := runtime.Request{Tenant: "globex", Definition: req.Definition}
	if _, err := p.resolveModel(other); !errors.Is(err, ErrModelPolicy) {
		t.Fatalf("another tenant's policy = %v", err)
	}
}

func TestCheckModelPolicies(t *testing.T) {
	ok := ModelPolicy{Name: "fast", Models: []wire.ModelRef{{Provider: "offline", Model: "offline"}}}
	if err := checkModelPolicies([]ModelPolicy{ok}); err != nil {
		t.Fatal(err)
	}
	for name, list := range map[string][]ModelPolicy{
		"bad name":        {{Name: "Fast!", Models: ok.Models}},
		"duplicate":       {ok, ok},
		"no models":       {{Name: "empty"}},
		"names a policy":  {{Name: "loop", Models: []wire.ModelRef{{Provider: PolicyProvider, Model: "fast"}}}},
		"unknown when":    {{Name: "w", Models: []wire.ModelRef{{Provider: "offline", Model: "offline"}, {Provider: "ollama", Model: "m"}}, FallbackOn: []string{"sometimes"}}},
		"repeats a model": {{Name: "r", Models: []wire.ModelRef{{Provider: "offline", Model: "offline"}, {Provider: "offline", Model: "offline"}}}},
	} {
		if err := checkModelPolicies(list); !errors.Is(err, ErrModelPolicy) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
