package platform_test

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/auth"
	"github.com/urmzd/warp/pkg/call"
	"github.com/urmzd/warp/pkg/projects"
	"github.com/urmzd/warp/pkg/runs"
)

// vault makes Vault, an account only olu reads, and homes helper there. Ana
// reads the workspace, and not Vault.
func vault(t *testing.T, p *platform.Platform) {
	t.Helper()
	if _, err := p.CreateProject(t.Context(), tenant, projects.Writer{By: "olu"}, platform.ProjectInput{Name: "Vault", Kind: projects.Account}); err != nil {
		t.Fatal(err)
	}
	if err := p.SetAgentHome(t.Context(), tenant, "helper", "Vault", govern); err != nil {
		t.Fatal(err)
	}
}

// A refusal says where an agent lives only when everyone in the workspace
// reads that project: it reaches whoever reads where it was made, such as a
// run's events or a message's tags.
func TestARefusalNamesWhereAnAgentLivesOnlyWhereEveryoneReadsIt(t *testing.T) {
	p := start(t, call.Limits{})
	project(t, p, "Field")
	if err := p.SetAgentHome(t.Context(), tenant, "lead", "Field", govern); err != nil {
		t.Fatal(err)
	}
	vault(t, p)
	if err := p.Available(tenant, "~", "lead"); !errors.Is(err, platform.ErrNotAvailable) || !strings.Contains(err.Error(), "it lives in Field") {
		t.Errorf("lead at the workspace = %v", err)
	}
	if err := p.Available(tenant, "~", "helper"); !errors.Is(err, platform.ErrNotAvailable) || strings.Contains(err.Error(), "Vault") {
		t.Errorf("helper at the workspace = %v", err)
	}
}

// A turn asked through a client whose default lives in a project its person
// may not read is told nobody answers there, as is so, and is never told
// the agent's name; nor does @auto fall back to it.
func TestATurnNamesNoDefaultItsPersonMayNotRead(t *testing.T) {
	f := &fakes{}
	p := startWith(t, f.config())
	ctx := t.Context()
	vault(t, p)
	create(t, p, tenant, slackClient("app", "A0APP", team, "helper"))
	olu := auth.Principal{Subject: "olu", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Operator}}

	if _, err := p.Converse(ctx, tenant, platform.Turn{ID: "t-olu", Text: "Hello", Client: "app", Sees: p.Seer(olu).Agent}); !errors.Is(err, platform.ErrNotAvailable) || !strings.Contains(err.Error(), "helper") {
		t.Fatalf("a turn through app for olu, who reads Vault = %v", err)
	}
	for _, asked := range []string{"", "auto"} {
		_, err := p.Converse(ctx, tenant, platform.Turn{ID: "t-ana-" + asked, Agent: asked, Text: "Hello", Client: "app", Sees: p.Seer(ana).Agent})
		if err == nil || strings.Contains(err.Error(), "helper") {
			t.Errorf("a turn asking %q through app for ana = %v", asked, err)
		}
		if asked == "" && !errors.Is(err, platform.ErrNoDefault) {
			t.Errorf("an untagged turn through app for ana = %v, want nobody answers", err)
		}
	}
}

// The call tool offers a run only the agents available where its calls are
// filed: one living in a project the run's readers may not read would be
// refused, and is never named.
func TestTheCallToolOffersOnlyAgentsAvailableWhereTheRunIs(t *testing.T) {
	p := start(t, call.Limits{})
	run, _ := turn(t, p, "api-1", "")
	if got := p.CallTargets(tenant, run.ID, "lead", []string{"helper"}); !slices.Equal(got, []string{"helper"}) {
		t.Fatalf("targets of a run at the workspace = %v", got)
	}
	vault(t, p)
	if got := p.CallTargets(tenant, run.ID, "lead", []string{"helper"}); len(got) != 0 {
		t.Errorf("targets once helper lives in Vault = %v", got)
	}
}

// What the analyst and the builder say of agents names only the ones
// everyone their answer reaches may read: an agent that came to live in a
// members-only project is not listed among a summary's agents, has no runs
// when asked about, though its runs here are counted, and is not among the
// agents another's spec may call.
func TestTheAnalystAndTheBuilderNameOnlyAgentsEveryoneTheyReachMayRead(t *testing.T) {
	p := start(t, call.Limits{})
	r, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "api-h1", Agent: "helper", Text: "Help with the plan"})
	if err != nil {
		t.Fatal(err)
	}
	await(t, p, r.ID, runs.Completed)
	vault(t, p)
	asking, _ := turn(t, p, "api-1", "")

	type summary struct {
		Total   struct{ Runs int }      `json:"total"`
		ByAgent []struct{ Name string } `json:"by_agent"`
		Slowest []struct{ Agent string }
		Note    string
	}
	ask := func(args map[string]any) summary {
		t.Helper()
		out, err := use(t, p, as(asking, platform.BuiltinAnalyst), platform.ToolRunsSummary, args)
		if err != nil {
			t.Fatal(err)
		}
		var got summary
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		return got
	}
	if got := ask(map[string]any{}); got.Total.Runs != 2 || len(got.ByAgent) != 1 || got.ByAgent[0].Name != "lead" {
		t.Errorf("the workspace's runs = %+v", got)
	}
	if got := ask(map[string]any{"agent": "helper"}); got.Note != "No runs of helper in this window." || len(got.Slowest) != 0 {
		t.Errorf("helper's runs = %+v", got)
	}
	out, err := use(t, p, as(asking, platform.BuiltinBuilder), platform.ToolReadAgent, map[string]any{"name": "lead"})
	if err != nil || strings.Contains(out, "helper") || !strings.Contains(out, "Lead.") {
		t.Errorf("lead, read for the workspace = %s, %v", out, err)
	}
}
