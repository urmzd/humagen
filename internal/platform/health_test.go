package platform_test

import (
	"context"
	"errors"
	"testing"

	"github.com/urmzd/humagen/fiber/pkg/turn/native"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/call"
	saigetypes "github.com/urmzd/saige/agent/types"
)

func TestHealthSaysAnOutageWithNoModelProvider(t *testing.T) {
	h := start(t, call.Limits{}).Health(t.Context())
	if h.Status != platform.HealthDown {
		t.Fatalf("status = %s", h.Status)
	}
	last := h.Components[len(h.Components)-1]
	if last.ID != "provider:none" || last.Status != platform.HealthDown || last.Detail == "" {
		t.Fatalf("no provider = %+v", last)
	}
}

func TestHealthChecksEachProviderAndKeepsItsHistory(t *testing.T) {
	build := func(native.Spec) (saigetypes.Provider, error) { return nil, errors.New("unused") }
	p := startWith(t, platform.Config{Ext: platform.Extensions{
		Providers: map[string]native.ProviderFunc{"good": build, "bad": build},
		Probes: map[string]platform.Probe{
			"good": func(context.Context) error { return nil },
			"bad":  func(context.Context) error { return errors.New("the key was refused (401)") },
		},
	}})
	h := p.Health(t.Context())
	byID := map[string]platform.HealthComponent{}
	for _, c := range h.Components {
		byID[c.ID] = c
	}
	if byID["provider:good"].Status != platform.HealthOperational {
		t.Errorf("good = %+v", byID["provider:good"])
	}
	if c := byID["provider:bad"]; c.Status != platform.HealthDown || c.Detail != "the key was refused (401)" {
		t.Errorf("bad = %+v", c)
	}
	if h.Status != platform.HealthDown || len(byID["api"].History) != 1 {
		t.Fatalf("health = %+v", h)
	}
	// A second request within the interval reuses the report.
	if again := p.Health(t.Context()); !again.CheckedAt.Equal(h.CheckedAt) {
		t.Fatal("checked again within the interval")
	}
}
