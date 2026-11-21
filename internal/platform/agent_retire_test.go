package platform_test

import (
	"errors"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/saige/flow/connectors/cron"
)

// A retired agent's name stays taken, so a Slack app that answers as it, its
// schedules and the rest keyed by its name never pass to a new agent; its
// schedules are paused, and Run Now on one is refused, not failed.
func TestRetiringKeepsTheNameAndPausesItsSchedules(t *testing.T) {
	p := start(t, call.Limits{})
	ctx := t.Context()
	keeper := wire.AgentSpec{Name: "keeper", Model: "offline", Provider: "offline", SystemPrompt: "Keep."}
	if _, err := p.Agents.Create(ctx, tenant, keeper, "ada"); err != nil {
		t.Fatal(err)
	}
	if err := p.Schedules.Add(cron.Schedule{Name: "keeper-nightly", Expression: "0 3 * * *", Agent: "keeper", Text: "sum", Tenant: tenant}, platform.About{}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.CreateClient(ctx, tenant, "ada", platform.ClientInput{
		Client: clients.Client{ID: "keeper-app", Name: "keeper-app", Surface: clients.Slack, Enabled: true,
			Identity: clients.Identity{AppID: "A0KEEP", Team: "T01ACME"}, DefaultAgent: "keeper"},
		Token: clients.SecretInput{Value: "xoxb-keep"}, SigningSecret: clients.SecretInput{Value: "sig-keep"},
	}); err != nil {
		t.Fatal(err)
	}

	if err := p.RetireAgent(ctx, installer, "keeper", 1); err != nil {
		t.Fatalf("retire = %v", err)
	}
	if _, paused, err := p.Schedules.Get(tenant, "keeper-nightly"); err != nil || !paused {
		t.Fatalf("the retired agent's schedule paused = %v, %v", paused, err)
	}
	if err := p.ScheduleAgentRetired(ctx, tenant, "keeper"); !errors.Is(err, platform.ErrAgentRetired) {
		t.Fatalf("a retired agent's schedule may run: %v", err)
	}
	// Nobody takes the name, so neither the Slack app that answers as it nor
	// anything else keyed by it reaches another agent.
	if _, err := p.Agents.Create(ctx, tenant, keeper, "ada"); !errors.Is(err, wire.ErrRetired) {
		t.Fatalf("a new agent took a retired name: %v", err)
	}
	if err := p.ScheduleAgentRetired(ctx, tenant, "nobody"); err != nil {
		t.Fatalf("a schedule asking an agent that never was is not a retired one's: %v", err)
	}
	// Retiring it again is not found: it is gone.
	if err := p.RetireAgent(ctx, installer, "keeper", 0); err == nil {
		t.Fatal("a retired agent was retired again")
	}
}

// A person never retires the platform's own agents.
func TestRetireRefusesThePlatformsOwn(t *testing.T) {
	p := startWith(t, platform.Config{TestBuiltinProvider: "offline", TestBuiltinModel: "offline"})
	if err := p.EnsureBuiltins(t.Context(), tenant); err != nil {
		t.Fatal(err)
	}
	if err := p.RetireAgent(t.Context(), installer, platform.BuiltinApprover, 0); !errors.Is(err, platform.ErrAgentProtected) {
		t.Fatalf("retiring the approver = %v", err)
	}
}
