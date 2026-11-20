package platform_test

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/join"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
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

// A refusal names the agent and where the run is, and never where the agent
// lives: it reaches whoever reads where it was made, such as a run's events
// or a message's tags, and a home in a project they may not read would leak.
// An allow list refuses an agent, and so does a place read by more than its
// private agent's readers.
func TestARefusalNamesWhereTheRunIsAndNeverWhereTheAgentLives(t *testing.T) {
	p := start(t, call.Limits{})
	project(t, p, "Field")
	vault(t, p)
	if err := p.Available(tenant, "~", "helper"); !errors.Is(err, platform.ErrNotAvailable) || !strings.Contains(err.Error(), "helper is private") || strings.Contains(err.Error(), "Vault") {
		t.Errorf("helper at the workspace, homed in Vault = %v", err)
	}
	if err := p.Available(tenant, "Vault", "helper"); err != nil {
		t.Errorf("helper in Vault, its home = %v", err)
	}
	setAgents(t, p, "Field", platform.AgentsInput{Enable: []string{}, Allow: []string{"lead"}})
	if err := p.Available(tenant, "Field", "helper"); !errors.Is(err, platform.ErrNotAvailable) || !strings.Contains(err.Error(), "helper is not allowed in Field") || strings.Contains(err.Error(), "Vault") {
		t.Errorf("helper in Field, which allows only lead = %v", err)
	}
}

// A turn asked through a client whose default lives in a project its person
// may not read is answered as through a client with no default: the default
// is passed over for them, so they are never told the agent's name; nor does
// @auto fall back to it. A person who reads that project is told it is not
// available, as a private agent answers only where its readers alone read.
func TestATurnNamesNoDefaultItsPersonMayNotRead(t *testing.T) {
	f := &fakes{}
	p := startWith(t, f.config())
	ctx := t.Context()
	vault(t, p)
	create(t, p, tenant, slackClient("app", "A0APP", team, "helper"))
	olu := auth.Principal{Subject: "olu", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Operator}}

	if _, err := p.Converse(ctx, tenant, platform.Turn{ID: "t-olu", Text: "Hello", Client: "app", Sees: p.Seer(olu).Agent}); !errors.Is(err, platform.ErrNotAvailable) {
		t.Fatalf("a turn through app for olu, who reads Vault = %v, want ErrNotAvailable", err)
	}
	plain := slackClient("plain", "A0PLN", "T0PLN", "")
	plain.Agents = []string{"helper"}
	create(t, p, tenant, plain)
	for _, asked := range []string{"", "auto"} {
		_, err := p.Converse(ctx, tenant, platform.Turn{ID: "t-ana-" + asked, Agent: asked, Text: "Hello", Client: "app", Sees: p.Seer(ana).Agent})
		_, without := p.Converse(ctx, tenant, platform.Turn{ID: "t-plain-" + asked, Agent: asked, Text: "Hello", Client: "plain", Sees: p.Seer(ana).Agent})
		if err == nil || strings.Contains(err.Error(), "helper") || without == nil || err.Error() != without.Error() {
			t.Errorf("a turn asking %q through app for ana = %v, and through a client with no default %v", asked, err, without)
		}
	}
}

// The call tool offers a run only the agents available where its calls are
// filed: one an allow list there leaves out would be refused, and is never
// named.
func TestTheCallToolOffersOnlyAgentsAvailableWhereTheRunIs(t *testing.T) {
	p := start(t, call.Limits{})
	run, _ := turn(t, p, "api-1", "")
	if got := p.CallTargets(tenant, run.ID, "lead", []string{"helper"}); !slices.Equal(got, []string{"helper"}) {
		t.Fatalf("targets of a run at the workspace = %v", got)
	}
	setAgents(t, p, "~", platform.AgentsInput{Enable: []string{"*"}, Allow: []string{"lead"}})
	if got := p.CallTargets(tenant, run.ID, "lead", []string{"helper"}); len(got) != 0 {
		t.Errorf("targets once the workspace allows only lead = %v", got)
	}
}

// An agent homed where a run's readers may not read is never offered to the
// run's call tool, nor asked to take an untagged message there, though it is
// allowed to answer there: agents are independent of projects, but who sees
// one is not. In its home it is offered and asked as any agent is.
func TestAHiddenAgentIsNeverOfferedOrAskedWhereItsReadersCannotSeeIt(t *testing.T) {
	p := start(t, call.Limits{})
	vault(t, p)
	run, _ := turn(t, p, "api-1", "")
	if got := p.CallTargets(tenant, run.ID, "lead", []string{"helper"}); len(got) != 0 {
		t.Fatalf("targets of a run at the workspace, helper homed in Vault = %v", got)
	}
	ds, err := p.Route(t.Context(), tenant, "", "Help, please.")
	if err != nil || slices.ContainsFunc(ds, func(d join.Decision) bool { return d.Agent == "helper" }) {
		t.Fatalf("helper was asked at the workspace: %+v, %v", ds, err)
	}
	ds, err = p.Route(t.Context(), tenant, "Vault", "Help, please.")
	if err != nil || !slices.ContainsFunc(ds, func(d join.Decision) bool { return d.Agent == "helper" }) {
		t.Fatalf("helper was not asked in its home: %+v, %v", ds, err)
	}
}

// The analyst's summaries name the agent that ran each run they count, as
// the runs do, even one that came to live in a members-only project since;
// the builder names, of the agents another's spec may call, only the ones
// everyone its answer reaches may read, as a spec is configuration.
func TestTheAnalystNamesWhoRanAndTheBuilderOnlyAgentsEveryoneTheyReachMayRead(t *testing.T) {
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
	if got := ask(map[string]any{}); got.Total.Runs != 2 || len(got.ByAgent) != 2 || len(got.Slowest) != 2 {
		t.Errorf("the workspace's runs = %+v", got)
	}
	if got := ask(map[string]any{"agent": "helper"}); got.Total.Runs != 1 || len(got.ByAgent) != 1 || got.ByAgent[0].Name != "helper" || len(got.Slowest) != 1 {
		t.Errorf("helper's runs = %+v", got)
	}
	out, err := use(t, p, as(asking, platform.BuiltinBuilder), platform.ToolReadAgent, map[string]any{"name": "lead"})
	if err != nil || strings.Contains(out, "helper") || !strings.Contains(out, "Lead.") {
		t.Errorf("lead, read for the workspace = %s, %v", out, err)
	}
}
