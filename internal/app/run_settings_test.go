package app

import (
	"context"
	"testing"

	"github.com/urmzd/humagen/fiber/pkg/event"
	"github.com/urmzd/humagen/internal/wire"

	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/settings"
	"github.com/urmzd/humagen/pkg/spec"
)

// The file's run settings reach a run filed in a project: the workspace's
// and the project's, the strictest ceiling and the nearest preference.
func TestTheFilesRunSettingsReachARun(t *testing.T) {
	p := newProjectsPlatform(t)
	const tenant = "acme"
	two, one := 2, 1
	w := spec.Workspace{
		Agents: &spec.Agents{Enable: []string{spec.EveryAgent}},
		Run:    &settings.Layer{Ceilings: settings.Ceilings{CallDepth: &two}, Preferences: settings.Preferences{Model: "offline/a"}},
		Projects: []spec.Project{{Name: "Field", Run: &settings.Layer{
			Ceilings:    settings.Ceilings{CallDepth: &one},
			Preferences: settings.Preferences{Model: "offline/b"},
		}}},
	}
	if err := applyRunSettings(p, tenant, w); err != nil {
		t.Fatal(err)
	}
	if err := applyProjects(p, tenant, w); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Agents.Create(context.Background(), tenant, wire.AgentSpec{Name: "lead", Model: "offline", Provider: "offline", SystemPrompt: "Lead."}, "test"); err != nil {
		t.Fatal(err)
	}
	run, err := p.Invoke(context.Background(), wire.NewEvent(wire.Raise{ID: "r-1", Tenant: tenant, Agent: "lead", Origin: "api", Text: "hi",
		Actor: event.Actor{ID: "ada", User: "ada"}, Context: map[string]string{runs.MetaProject: "Field"}}))
	if err != nil {
		t.Fatal(err)
	}
	c, ok, err := p.RunContext(tenant, run.ID)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if c.Settings.Ceilings.Depth(5) != 1 || c.Settings.Preferences.Model != "offline/b" {
		t.Fatalf("the run's context = %+v", c.Settings)
	}
}
