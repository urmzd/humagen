package platform_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/connectors/cron"
	"github.com/urmzd/humagen/internal/wire"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/alerts"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/evals"
	"github.com/urmzd/humagen/pkg/join"
	"github.com/urmzd/humagen/pkg/people"
	"github.com/urmzd/humagen/pkg/policy"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// globexHelper adds an agent that lives in Globex, with Field's projects.
func globexHelper(t *testing.T, p *platform.Platform) {
	t.Helper()
	field(t, p)
	if _, err := p.Agents.Create(t.Context(), tenant, wire.AgentSpec{
		Name: "globex-helper", Model: "offline", Provider: "offline", SystemPrompt: "You know the Globex account.",
		Description: "Globex renewals and billing",
	}, "test"); err != nil {
		t.Fatal(err)
	}
	if err := p.SetAgentHome(t.Context(), tenant, "globex-helper", "Globex", ada); err != nil {
		t.Fatal(err)
	}
}

func setAgents(t *testing.T, p *platform.Platform, project string, in platform.AgentsInput) {
	t.Helper()
	cur, err := p.ProjectAgentsOf(tenant, project)
	if err != nil {
		t.Fatal(err)
	}
	in.Version = cur.Version
	if _, err := p.SetProjectAgents(t.Context(), tenant, project, govern, in); err != nil {
		t.Fatal(err)
	}
}

func TestAnAgentThatLivesInAProjectRunsOnlyThereAndBelow(t *testing.T) {
	p := start(t, call.Limits{})
	globexHelper(t, p)
	for _, c := range []struct {
		project string
		ok      bool
	}{
		{"Globex", true}, {"Renewals", true}, {"Initech", false}, {"Field", false}, {"", false},
	} {
		_, err := p.Invoke(t.Context(), inProject("in-"+c.project, "globex-helper", c.project))
		switch {
		case c.ok && err != nil:
			t.Fatalf("in %q: %v", c.project, err)
		case !c.ok && !errors.Is(err, platform.ErrNotAvailable):
			t.Fatalf("in %q = %v, want ErrNotAvailable", c.project, err)
		}
	}
	_, err := p.Invoke(t.Context(), inProject("initech", "globex-helper", "Initech"))
	if want := "globex-helper is not available in Initech"; err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("the refusal says %v, want it to say %q", err, want)
	}
}

func TestARefusedRunIsNeitherCountedNorRecorded(t *testing.T) {
	p := start(t, call.Limits{})
	globexHelper(t, p)
	if _, err := p.Invoke(t.Context(), inProject("x", "globex-helper", "Initech")); !errors.Is(err, platform.ErrNotAvailable) {
		t.Fatal(err)
	}
	if _, err := p.Runs.Get(tenant, "x"); err == nil {
		t.Fatal("a refused run was recorded")
	}
	for _, e := range p.Usage.Entries() {
		if e.Tenant == tenant {
			t.Fatalf("a refused run left a usage entry: %+v", e)
		}
	}
}

func TestAnAllowListNarrowsEveryProjectBelowIt(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	setAgents(t, p, "Field", platform.AgentsInput{Enable: []string{}, Allow: []string{"lead"}})
	if _, err := p.Invoke(t.Context(), inProject("a", "helper", "Globex")); !errors.Is(err, platform.ErrNotAvailable) {
		t.Fatalf("helper below Field's allow list = %v", err)
	}
	if _, err := p.Invoke(t.Context(), inProject("b", "lead", "Globex")); err != nil {
		t.Fatalf("lead below Field's allow list = %v", err)
	}
	if _, err := p.Invoke(t.Context(), inProject("c", "helper", "")); err != nil {
		t.Fatalf("helper at the root = %v", err)
	}
}

func TestACallReachesOnlyAgentsAvailableWhereItsRootIsFiled(t *testing.T) {
	p := start(t, call.Limits{MaxDepth: 2, MaxFanOut: 2})
	field(t, p)
	setAgents(t, p, "Globex", platform.AgentsInput{Enable: []string{}, Allow: []string{"lead"}})
	root, err := p.Invoke(t.Context(), inProject("root", "lead", "Renewals"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Call(t.Context(), tenant, root.ID, call.Request{Key: "k1", Agent: "helper", Text: "help", Mode: call.Delegate})
	if errors.Is(err, call.ErrInvalid) {
		t.Skip("the root finished before the call was made")
	}
	if !errors.Is(err, platform.ErrNotAvailable) {
		t.Fatalf("a call to helper from Renewals = %v, want ErrNotAvailable", err)
	}
}

func TestHousekeepingIsNotAskedWhereItRuns(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	setAgents(t, p, "Field", platform.AgentsInput{Enable: []string{}, Allow: []string{"lead"}})
	tg := inProject("hk", "helper", "Globex")
	tg.Source = wire.SourceOf(runs.OriginSystem, tg.Tenant)
	if _, err := p.Invoke(t.Context(), tg); err != nil {
		t.Fatalf("housekeeping was refused: %v", err)
	}
}

func TestAScheduleWhoseAgentStoppedBeingAvailableIsRefusedAndRaisedOnce(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	setAgents(t, p, "Field", platform.AgentsInput{Enable: []string{}, Allow: []string{"lead"}})
	for i, id := range []string{"tick-1", "tick-2"} {
		tg := inProject(id, "helper", "Initech")
		tg.Source = wire.SourceOf("cron", tg.Tenant)
		wire.SetMeta(&tg, "cron_schedule", "nightly")
		if _, err := p.Invoke(t.Context(), tg); !errors.Is(err, platform.ErrNotAvailable) {
			t.Fatalf("occurrence %d = %v", i, err)
		}
	}
	var open []alerts.Alert
	for _, a := range p.Alerts.Alerts(tenant) {
		if a.Open() {
			open = append(open, a)
		}
	}
	if len(open) != 1 || !strings.Contains(open[0].Title, "nightly") || !strings.Contains(open[0].Title, "helper is not available in Initech") {
		t.Fatalf("open alerts = %+v, want one about nightly", open)
	}
}

// Asked about a project the tree does not hold, as a schedule is when its
// project's delete wins between being looked up and being checked, the answer
// is that the project is not found, not that the agent is refused there.
func TestAvailabilityInAProjectThatIsGoneIsNotFound(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	if err := p.Available(tenant, "Nowhere", "helper"); !errors.Is(err, platform.ErrProjectNotFound) || errors.Is(err, platform.ErrNotAvailable) {
		t.Fatalf("helper in a project that is gone = %v, want ErrProjectNotFound", err)
	}
	if err := p.Available(tenant, "Globex", "helper"); err != nil {
		t.Fatalf("helper in Globex = %v", err)
	}
}

// evalOf runs a suite of n cases against an agent and returns the cases'
// results once it has finished.
func evalOf(t *testing.T, p *platform.Platform, agent string, n int) []evals.Result {
	t.Helper()
	suite := evals.Suite{Name: "of-" + agent, Target: evals.Agent, Subject: agent}
	for i := range n {
		suite.Cases = append(suite.Cases, evals.Case{ID: fmt.Sprintf("c%d", i), Input: "hello", Checks: []evals.Check{{Kind: evals.Contains, Value: "offline"}}})
	}
	if err := p.Evals.Put(tenant, suite, false); err != nil {
		t.Fatal(err)
	}
	run, err := p.StartEval(t.Context(), tenant, suite.Name, "ada")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		all, err := p.Evals.Runs(tenant, suite.Name)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range all {
			if r.ID == run.ID && r.Status != evals.Running {
				return r.Results
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the eval of %s did not finish", agent)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// An eval tries an agent where it lives, as any run of it would be: a
// project agent's cases are filed in its home, so the home's limits hold
// them and the runs they call are checked there. A platform agent's are
// filed at the workspace.
func TestAnEvalTriesAnAgentWhereItLives(t *testing.T) {
	p := start(t, call.Limits{})
	globexHelper(t, p)
	for _, c := range []struct {
		agent, project string
		within         []string
	}{{"globex-helper", "Globex", []string{"Field"}}, {"helper", "", nil}} {
		results := evalOf(t, p, c.agent, 1)
		if len(results) != 1 || results[0].RunID == "" {
			t.Fatalf("the eval of %s = %+v", c.agent, results)
		}
		r, err := p.Runs.Get(tenant, results[0].RunID)
		if err != nil {
			t.Fatal(err)
		}
		if r.Project != c.project || !slices.Equal(r.Within, c.within) {
			t.Fatalf("an eval of %s was filed in %q within %v, want %q within %v", c.agent, r.Project, r.Within, c.project, c.within)
		}
	}
}

// An eval's cases count toward the limits of the project its agent lives in,
// so evaluating a project agent never gets around them.
func TestAnEvalIsHeldByTheLimitsWhereItsAgentLives(t *testing.T) {
	p := start(t, call.Limits{})
	globexHelper(t, p)
	if _, err := p.Policies.Put(tenant, policy.Policy{Scope: policy.Scope{Kind: policy.Project, Name: "Globex"}, Period: policy.Day, MaxRuns: 1, Action: policy.Block}, "test", time.Now()); err != nil {
		t.Fatal(err)
	}
	results := evalOf(t, p, "globex-helper", 2)
	if len(results) != 2 || results[0].Error != "" || results[1].Error == "" {
		t.Fatalf("two cases under Globex's limit of one run = %+v, want the second refused", results)
	}
	if _, err := p.Invoke(t.Context(), inProject("after", "globex-helper", "Globex")); err == nil {
		t.Fatal("a run in Globex was let in after the eval used its one run")
	}
}

func TestATurnThatTagsNoAgentGoesToTheDefaultItInherits(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	setAgents(t, p, "~", platform.AgentsInput{Enable: []string{"*"}, Default: "lead"})
	setAgents(t, p, "Globex", platform.AgentsInput{Enable: []string{}, Default: "helper"})
	for _, c := range []struct{ project, want string }{{"", "lead"}, {"Initech", "lead"}, {"Globex", "helper"}, {"Renewals", "helper"}} {
		r, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "t-" + c.project, Text: "hello", Project: c.project})
		if err != nil {
			t.Fatalf("in %q: %v", c.project, err)
		}
		if r.Agent != c.want || r.Picked != "" {
			t.Fatalf("in %q, %s answered (picked %q), want %s", c.project, r.Agent, r.Picked, c.want)
		}
	}
	if d := p.DefaultAt(tenant, platform.Place{Project: "Renewals"}); d != (platform.Nearer{Agent: "helper", From: "project:Globex"}) {
		t.Fatalf("the default in Renewals = %+v", d)
	}
	// A nearer default, a thread's or a channel's, comes first.
	if d := p.DefaultAt(tenant, platform.Place{Project: "Renewals", Nearer: []platform.Nearer{{}, {Agent: "lead", From: "channel"}}}); d.Agent != "lead" || d.From != "channel" {
		t.Fatalf("with a channel's default = %+v", d)
	}
}

func TestATurnThatTagsNoAgentWhereNothingAnswersStartsNothing(t *testing.T) {
	p := start(t, call.Limits{})
	setAgents(t, p, "~", platform.AgentsInput{Enable: []string{"*"}})
	_, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "t1", Text: "hello"})
	if !errors.Is(err, platform.ErrNoDefault) || !strings.Contains(err.Error(), "tag an agent") {
		t.Fatalf("an untagged turn with no default = %v", err)
	}
	if n := len(p.Runs.List(tenant, 10)); n != 0 {
		t.Fatalf("%d runs started", n)
	}
}

func TestADefaultThatStoppedBeingAvailableIsNotPassedOver(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	setAgents(t, p, "~", platform.AgentsInput{Enable: []string{"*"}, Default: "lead"})
	setAgents(t, p, "Field", platform.AgentsInput{Enable: []string{}, Default: "helper"})
	setAgents(t, p, "Globex", platform.AgentsInput{Enable: []string{}, Allow: []string{"lead"}})
	_, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "t1", Text: "hello", Project: "Globex"})
	if !errors.Is(err, platform.ErrNotAvailable) || !strings.Contains(err.Error(), "@helper answers untagged messages in Globex") {
		t.Fatalf("a default that stopped being available = %v", err)
	}
}

func TestOnlyAgentsAvailableWhereAMessageIsPostedAreAskedToJoin(t *testing.T) {
	p := start(t, call.Limits{})
	globexHelper(t, p)
	for _, spec := range []wire.AgentSpec{
		{Name: "biller", Model: "offline", Provider: "offline", SystemPrompt: "Bills.", Description: "billing and invoices"},
	} {
		if _, err := p.Agents.Create(t.Context(), tenant, spec, "test"); err != nil {
			t.Fatal(err)
		}
	}
	// In Globex, globex-helper lives there and joins; outside, it is never
	// asked, and biller is.
	ds, err := p.Route(t.Context(), tenant, "Globex", "the Globex renewals")
	if err != nil || len(join.Joined(ds)) != 1 || join.Joined(ds)[0].Agent != "globex-helper" {
		t.Fatalf("in Globex = %+v, %v", ds, err)
	}
	ds, _ = p.Route(t.Context(), tenant, "Initech", "the Globex renewals")
	if slices.ContainsFunc(ds, func(d join.Decision) bool { return d.Agent == "globex-helper" }) || len(join.Joined(ds)) != 0 {
		t.Fatalf("in Initech, globex-helper was asked or someone joined: %+v", ds)
	}
	r, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "t1", Agent: "*", Text: "an invoice question", Project: "Initech"})
	if err != nil || r.Agent != "biller" || !strings.Contains(r.Picked, "Joined on its own: matched invoice") {
		t.Fatalf("open in Initech = %+v, %v", r, err)
	}
}

// Several agents can take one message, each in a run of its own; when none
// does, nothing starts and it waits, and no named default is fallen back on.
func TestEveryAgentThatJoinsAnswersAndNoTakersStartsNothing(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	setAgents(t, p, "~", platform.AgentsInput{Enable: []string{"*"}, Default: "*"})
	setAgents(t, p, "Field", platform.AgentsInput{Enable: []string{}, Default: "helper"})
	setAgents(t, p, "Globex", platform.AgentsInput{Enable: []string{}, Default: "*"})
	r, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "t1", Text: "Lead and Help, please.", Project: "Renewals"})
	if err != nil || r.Picked == "" {
		t.Fatalf("an untagged turn both agents fit = %+v, %v", r, err)
	}
	thread := r.Thread
	got := map[string]bool{}
	for _, run := range p.Runs.List(tenant, 50) {
		if run.Thread == thread {
			got[run.Agent] = true
		}
	}
	if !got["lead"] || !got["helper"] {
		t.Fatalf("the runs in the conversation are for %v, want lead and helper", got)
	}
	if _, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "t2", Text: "zzz", Project: "Renewals"}); !errors.Is(err, platform.ErrNoTakers) {
		t.Fatalf("a turn no agent fits = %v, want ErrNoTakers", err)
	}
}

// An agent whose spec is tagged_only is never asked.
func TestATaggedOnlyAgentIsNeverAskedToJoin(t *testing.T) {
	p := start(t, call.Limits{})
	if _, err := p.Agents.Create(t.Context(), tenant, wire.AgentSpec{
		Name: "quiet", Model: "offline", Provider: "offline", SystemPrompt: "Quiet.", TaggedOnly: true,
	}, "test"); err != nil {
		t.Fatal(err)
	}
	ds, err := p.Route(t.Context(), tenant, "", "Quiet, please.")
	if err != nil || slices.ContainsFunc(ds, func(d join.Decision) bool { return d.Agent == "quiet" }) {
		t.Fatalf("a tagged-only agent was asked: %+v, %v", ds, err)
	}
}

func TestHomingAnAgentIsRefusedWhileSomethingElsewhereUsesIt(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	setAgents(t, p, "Initech", platform.AgentsInput{Enable: []string{}, Default: "helper"})
	if err := p.Schedules.Add(cron.Schedule{Name: "nightly", Expression: "0 3 * * *", Agent: "helper", Text: "sum", Tenant: tenant,
		Context: map[string]string{runs.MetaProject: "Field"}}, platform.About{}); err != nil {
		t.Fatal(err)
	}
	if err := p.Alerts.AddRule(tenant, alerts.Rule{Name: "slow-helper", Kind: alerts.RunFailed, Severity: alerts.Warning, Agent: "helper", Project: "Initech", CreatedBy: "ada"}); err != nil {
		t.Fatal(err)
	}
	err := p.SetAgentHome(t.Context(), tenant, "helper", "Globex", ada)
	if !errors.Is(err, projects.ErrInUse) {
		t.Fatalf("homing helper in Globex = %v, want ErrInUse", err)
	}
	for _, want := range []string{"the default of Initech", "the schedule nightly", "the alert rule slow-helper"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal %q does not name %s", err, want)
		}
	}
	if home, homed := p.ProjectTree(tenant).Home("helper"); !homed || home != projects.Root {
		t.Fatal("a refused move changed the Company home")
	}
	// Everything that uses it is in Field's subtree, so nothing is lost.
	if err := p.SetAgentHome(t.Context(), tenant, "helper", "Field", ada); err != nil {
		t.Fatalf("homing helper in Field, where everything that uses it is: %v", err)
	}
}

func TestAClientThatRoutesToAnAgentKeepsItFromLeavingTheRoot(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	if _, err := p.CreateClient(t.Context(), tenant, "ada", platform.ClientInput{
		Client: clients.Client{ID: "docs", Name: "docs", Surface: clients.Slack, Enabled: true,
			Identity: clients.Identity{AppID: "A0DOCS", Team: "T01ACME"}, DefaultAgent: "helper"},
		Token: clients.SecretInput{Value: "xoxb-docs"}, SigningSecret: clients.SecretInput{Value: "sig-docs"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := p.SetAgentHome(t.Context(), tenant, "helper", "Globex", ada); !errors.Is(err, projects.ErrInUse) || !strings.Contains(err.Error(), "the client docs") {
		t.Fatalf("homing an agent a client defaults to = %v", err)
	}
}

// An agent is one agent under every spelling of its name, as the registry
// resolves it: owning its home is asked under each, a platform agent is the
// platform's under each, and none is available under any but its own.
func TestAnAgentIsTheSameAgentUnderEverySpellingOfItsName(t *testing.T) {
	p := start(t, call.Limits{})
	globexHelper(t, p)
	bo := auth.Principal{Subject: "bo", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Manager}}
	for _, name := range []string{"globex-helper", "Globex-Helper", "GLOBEX-HELPER", " globex-helper", "globex-helper\t"} {
		if err := p.OwnsHome(bo, name); !errors.Is(err, projects.ErrForbidden) {
			t.Errorf("OwnsHome(bo, %q) = %v, want forbidden", name, err)
		}
	}
	for _, name := range []string{platform.BuiltinWriter, "Warp-Writer", " WARP-WRITER "} {
		if !platform.IsBuiltin(name) {
			t.Errorf("%q is not the platform's", name)
		}
	}
	if _, err := p.Invoke(t.Context(), inProject("spelled", "Globex-Helper", "Globex")); !errors.Is(err, platform.ErrNotAvailable) {
		t.Fatalf("a run of Globex-Helper in Globex = %v, want ErrNotAvailable", err)
	}
}

func TestThePlatformsOwnAgentsLiveNowhere(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	if err := p.SetAgentHome(t.Context(), tenant, platform.BuiltinWriter, "Field", ada); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("homing %s = %v", platform.BuiltinWriter, err)
	}
}

func TestAProjectsAgentsNameAgentsTheWorkspaceHas(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	cur, _ := p.ProjectAgentsOf(tenant, "Field")
	if _, err := p.SetProjectAgents(t.Context(), tenant, "Field", ada, platform.AgentsInput{Version: cur.Version, Enable: []string{"ghost"}}); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("enabling an agent that does not exist = %v", err)
	}
	// The root's lists are the workspace default, which needs govern.
	root, _ := p.ProjectAgentsOf(tenant, "~")
	if _, err := p.SetProjectAgents(t.Context(), tenant, "~", ada, platform.AgentsInput{Version: root.Version, Enable: []string{"*"}, Default: "lead"}); !errors.Is(err, projects.ErrForbidden) {
		t.Fatalf("setting the workspace default without govern = %v", err)
	}
}

func TestAProjectsAgentsSayWhereEachComesFrom(t *testing.T) {
	p := start(t, call.Limits{})
	globexHelper(t, p)
	setAgents(t, p, "Field", platform.AgentsInput{Enable: []string{"lead"}, Allow: []string{"lead", "globex-helper"}, Default: "lead"})
	got, err := p.ProjectAgentsOf(tenant, "Renewals")
	if err != nil {
		t.Fatal(err)
	}
	want := []platform.Offer{{Agent: "globex-helper", From: "Globex", Home: true}, {Agent: "lead", From: "Field"}}
	if got.Available.All || len(got.Offers) != len(want) || got.Offers[0] != want[0] || got.Offers[1] != want[1] {
		t.Fatalf("offers in Renewals = %+v (available %+v)", got.Offers, got.Available)
	}
	if got.DefaultResolved == nil || *got.DefaultResolved != (platform.Nearer{Agent: "lead", From: "project:Field"}) {
		t.Fatalf("the default in Renewals = %+v", got.DefaultResolved)
	}
	places := p.AgentPlaces(tenant, []string{"lead", "globex-helper", "helper"})
	if g := places["globex-helper"]; g.Home != "Globex" || len(g.EnabledIn) != 0 {
		t.Fatalf("globex-helper's place = %+v", g)
	}
	if l := places["lead"]; l.Home != "~" || len(l.EnabledIn) != 2 || l.EnabledIn[0] != "~" || l.EnabledIn[1] != "Field" {
		t.Fatalf("lead's place = %+v", l)
	}
}

// settingsWith writes settings as an earlier version kept them, with a
// baseline.
func settingsWith(t *testing.T, st state.Store, tenantName string, baseline any) {
	t.Helper()
	fields := map[string]any{"history": "recent", "history_limit": 12000, "workspace_name": "Acme"}
	if baseline != nil {
		fields["baseline"] = baseline
	}
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Write(t.Context(), "settings", state.Put(tenantName, "settings", raw)); err != nil {
		t.Fatal(err)
	}
}

func TestTheBaselineBecomesTheWorkspaceDefault(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		settingsWith(t, st, "named", "lead")
		settingsWith(t, st, "unnamed", "")
		p, err := newOn(t, st)
		if err != nil {
			t.Fatal(err)
		}
		for tenantName, want := range map[string]string{"named": "lead", "unnamed": "*"} {
			root := p.ProjectTree(tenantName).Root()
			if root.Agents.Default != want || len(root.Agents.Enable) != 1 || root.Agents.Enable[0] != "*" {
				t.Fatalf("the root of %s = %+v, want default %s", tenantName, root.Agents, want)
			}
			if _, held := storedSettings(t, st, tenantName)["baseline"]; held {
				t.Fatalf("the settings of %s still name a baseline", tenantName)
			}
		}
		if h, _, _ := p.Projects.History("named", projects.Root, 0, 0); len(h) == 0 || h[0].By != "migration" {
			t.Fatalf("the root of named does not say the upgrade set it: %+v", h)
		}
		// An empty baseline was the picker, which open is now: the
		// workspace reads as it did, and is given no tree.
		if ok, _ := p.Projects.Stored("unnamed"); ok {
			t.Fatal("settings that named no baseline, with no tree, were given one")
		}
		// A second start finds nothing to do.
		if err := p.Close(); err != nil {
			t.Fatal(err)
		}
		again, err := newOn(t, st)
		if err != nil {
			t.Fatal(err)
		}
		if h, _, _ := again.Projects.History("named", projects.Root, 0, 0); len(h) != 2 {
			t.Fatalf("a second start wrote the root again: %+v", h)
		}
	})
}

func TestTheBaselineMigrationIsRepairedAfterAStopBetweenItsWrites(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		f := &faultyIn{Store: open(t)}
		settingsWith(t, f, tenant, "lead")
		f.fail("settings")
		if _, err := newOn(t, f); !errors.Is(err, statetest.ErrInjected) {
			t.Fatalf("a start that could not clear the baseline = %v", err)
		}
		if storedSettings(t, f, tenant)["baseline"] != "lead" {
			t.Fatal("the baseline was lost although the settings were not written")
		}
		f.fail("")
		p, err := newOn(t, f)
		if err != nil {
			t.Fatal(err)
		}
		if d, _ := p.ProjectTree(tenant).DefaultOf(projects.Root); d != "lead" {
			t.Fatalf("after the repair the default is %q", d)
		}
		if _, held := storedSettings(t, f, tenant)["baseline"]; held {
			t.Fatal("the repair did not clear the baseline")
		}
	})
}

// passing lets the next n writes to one namespace through and fails the
// rest, so a test can stop a start between two writes to the same store.
type passing struct {
	state.Store
	mu        sync.Mutex
	namespace string
	left      int
}

func (f *passing) Write(ctx context.Context, namespace string, ops ...state.Op) error {
	f.mu.Lock()
	failing := f.namespace == namespace && f.left == 0
	if f.namespace == namespace && f.left > 0 {
		f.left--
	}
	f.mu.Unlock()
	if failing {
		return statetest.ErrInjected
	}
	return f.Store.Write(ctx, namespace, ops...)
}

func TestOneStartMovesTheProjectsAndTheBaselineWhereverItStops(t *testing.T) {
	// Each stop leaves a record both migrations must finish from.
	stops := map[string]func(st state.Store) state.Store{
		// The import fails: nothing is written.
		"before the import": func(st state.Store) state.Store { return &passing{Store: st, namespace: "projects"} },
		// The import is written, and the settings without the projects are not.
		"after the import": func(st state.Store) state.Store { return &passing{Store: st, namespace: "settings"} },
		// The settings are written without the projects, still naming the
		// baseline, and the root's default is not.
		"after the projects leave the settings": func(st state.Store) state.Store {
			return &passing{Store: st, namespace: "projects", left: 1}
		},
		// The root's default is written, and the settings without the
		// baseline are not.
		"after the default is set": func(st state.Store) state.Store {
			return &passing{Store: st, namespace: "settings", left: 1}
		},
	}
	for name, stop := range stops {
		t.Run(name, func(t *testing.T) {
			statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
				st := open(t)
				raw, _ := json.Marshal(map[string]any{
					"baseline": "lead", "history": "recent", "history_limit": 12000, "workspace_name": "Acme",
					"projects": []map[string]string{{"name": "Launch"}, {"name": "Ops"}},
				})
				if err := st.Write(t.Context(), "settings", state.Put(tenant, "settings", raw)); err != nil {
					t.Fatal(err)
				}
				if _, err := newOn(t, stop(st)); !errors.Is(err, statetest.ErrInjected) {
					t.Fatalf("a start that stopped %s = %v", name, err)
				}
				if b, held := storedSettings(t, st, tenant)["baseline"]; !held || b != "lead" {
					t.Fatalf("stopped %s, the settings lost the baseline: %v", name, b)
				}
				p, err := newOn(t, st)
				if err != nil {
					t.Fatal(err)
				}
				var got []string
				for _, pr := range p.Projects.List(tenant) {
					got = append(got, pr.Name)
				}
				if !slices.Equal(got, []string{"Launch", "Ops"}) {
					t.Fatalf("after a stop %s the tree holds %q", name, got)
				}
				if d, _ := p.ProjectTree(tenant).DefaultOf(projects.Root); d != "lead" {
					t.Fatalf("after a stop %s the default is %q", name, d)
				}
				held := storedSettings(t, st, tenant)
				for _, key := range []string{"projects", "baseline"} {
					if _, ok := held[key]; ok {
						t.Fatalf("after a stop %s the settings still hold %s", name, key)
					}
				}
			})
		})
	}
}

func TestARootWrittenBeforeProjectsHadAgentsEnablesEveryAgent(t *testing.T) {
	st := state.NewMemory()
	// A root as the project store wrote it before Agents existed.
	raw, _ := json.Marshal(map[string]any{"name": "", "kind": "company", "version": 1})
	if err := st.Write(t.Context(), "projects", state.Put(tenant, "p:", raw)); err != nil {
		t.Fatal(err)
	}
	p, err := newOn(t, st)
	if err != nil {
		t.Fatal(err)
	}
	root := p.ProjectTree(tenant).Root()
	if len(root.Agents.Enable) != 1 || root.Agents.Enable[0] != "*" || root.Agents.Default != "*" {
		t.Fatalf("the root = %+v", root.Agents)
	}
}

func TestAWorkspaceNeverWrittenRunsAnyAgentThatJoins(t *testing.T) {
	p := start(t, call.Limits{})
	r, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "t1", Text: "Lead."})
	if err != nil || r.Agent != "lead" || r.Picked == "" {
		t.Fatalf("an untagged turn in a new workspace = %+v, %v", r, err)
	}
}

func TestSignInsRecordPeopleAndAgentNamesAreNotHandles(t *testing.T) {
	p := start(t, call.Limits{})
	p.SeePerson(auth.Principal{Subject: "okta|dana", Tenant: tenant, Kind: auth.User, Name: "Dana Scully", Roles: []auth.Role{auth.Operator}})
	p.SeePerson(auth.Principal{Subject: "ci", Tenant: tenant, Kind: auth.Machine, Name: "CI"})
	got, err := p.People.Get(tenant, "okta|dana")
	if err != nil || got.Handle != "dana-scully" || len(got.Roles) != 1 || got.Roles[0] != "operator" {
		t.Fatalf("Dana = %+v, %v", got, err)
	}
	if _, err := p.People.Get(tenant, "ci"); !errors.Is(err, people.ErrNotFound) {
		t.Fatalf("a machine was recorded: %v", err)
	}
	// A person named like an agent is given another handle.
	p.SeePerson(auth.Principal{Subject: "okta|lead", Tenant: tenant, Kind: auth.User, Name: "Lead"})
	if l, _ := p.People.Get(tenant, "okta|lead"); l.Handle == "lead" {
		t.Fatalf("a person was given an agent's name as a handle: %+v", l)
	}
	for _, name := range []string{"dana-scully", "auto", "sme"} {
		if err := p.CheckAgentName(tenant, name); !errors.Is(err, platform.ErrNameTaken) {
			t.Fatalf("an agent named %s = %v", name, err)
		}
	}
	if err := p.CheckAgentName(tenant, "dana-scully"); !strings.Contains(err.Error(), "a person goes by @dana-scully") {
		t.Fatalf("the refusal says %v", err)
	}
	if err := p.CheckAgentName(tenant, "reviewer"); err != nil {
		t.Fatal(err)
	}
}

func TestAClientsPeopleAndThePolicysSubjectsAreRecorded(t *testing.T) {
	p := start(t, call.Limits{})
	if _, err := p.CreateClient(t.Context(), tenant, "ada", platform.ClientInput{
		Client: clients.Client{ID: "docs", Name: "docs", Surface: clients.Slack, Enabled: true, DefaultAgent: "lead",
			Identity: clients.Identity{AppID: "A0DOCS", Team: "T01ACME"},
			Slack:    &clients.SlackOptions{Machine: "slack-app", People: map[string]string{"U01": "okta|fox", "U02": "okta|fox"}}},
		Token: clients.SecretInput{Value: "xoxb-docs"}, SigningSecret: clients.SecretInput{Value: "sig-docs"},
	}); err != nil {
		t.Fatal(err)
	}
	if got, err := p.People.Get(tenant, "okta|fox"); err != nil || len(got.Sources) != 1 || got.Sources[0] != people.Slack {
		t.Fatalf("a mapped person = %+v, %v", got, err)
	}
	if err := p.SeeGranted(tenant, []platform.Grant{{Subject: "okta|skinner", Roles: []string{"admin"}}}); err != nil {
		t.Fatal(err)
	}
	if got, err := p.People.Get(tenant, "okta|skinner"); err != nil || len(got.Roles) != 1 || got.Roles[0] != "admin" {
		t.Fatalf("a granted subject = %+v, %v", got, err)
	}
}

func TestRacingSignInsKeepOnePerson(t *testing.T) {
	p := start(t, call.Limits{})
	var wg sync.WaitGroup
	for range 40 {
		wg.Go(func() {
			p.SeePerson(auth.Principal{Subject: "okta|dana", Tenant: tenant, Kind: auth.User, Name: "Dana"})
		})
	}
	wg.Wait()
	if n := p.People.Len(tenant); n != 1 {
		t.Fatalf("%d people after 40 sign-ins of one", n)
	}
}

func TestAClientsMessageIsOfferedOnlyToWhatTheClientMayRoute(t *testing.T) {
	p := start(t, call.Limits{})
	c := clients.Client{Tenant: tenant, ID: "docs", Surface: clients.Slack, Enabled: true, DefaultAgent: "*", Agents: []string{"helper"}}
	got, err := p.JoinFor(t.Context(), c, "Help and Lead.")
	if err != nil || len(got) != 1 || got[0].Agent != "helper" {
		t.Fatalf("joining for a client that routes to helper only = %+v, %v", got, err)
	}
	c.Agents = nil
	if _, err := p.JoinFor(t.Context(), c, "Help."); !errors.Is(err, platform.ErrNoTakers) {
		t.Fatalf("joining for a client that routes to no agent = %v", err)
	}
}

func TestATurnCannotRunOpenItself(t *testing.T) {
	p := start(t, call.Limits{})
	if _, err := p.Invoke(t.Context(), wire.NewEvent(wire.Raise{ID: "a", Tenant: tenant, Agent: "*", Origin: "api", Text: "hi"})); !errors.Is(err, platform.ErrNotAvailable) {
		t.Fatalf("a run of open itself = %v", err)
	}
}

// For a person a client's credential acts for, only the agents the client
// may route to are asked, whether the turn asks for open or the client's
// default is open (or auto, as written before), and no agent off its list
// ever answers.
func TestJoiningThroughAClientAsksOnlyItsAgents(t *testing.T) {
	f := &fakes{}
	p := startWith(t, f.config())
	ctx := t.Context()
	if _, err := p.Agents.Create(ctx, tenant, wire.AgentSpec{
		Name: "releaser", Model: "offline", Provider: "offline", SystemPrompt: "x", Description: "Cuts releases and tags versions.",
	}, "test"); err != nil {
		t.Fatal(err)
	}
	setAgents(t, p, "~", platform.AgentsInput{Enable: []string{"*"}, Default: "lead"})
	in := slackClient("app", "A0APP", team, "auto")
	in.Agents = []string{"helper"}
	create(t, p, tenant, in)
	if r, err := p.Converse(ctx, tenant, platform.Turn{ID: "t-free", Agent: "*", Text: "tag the next release"}); err != nil || r.Agent != "releaser" {
		t.Fatalf("open with no client = %+v, %v", r, err)
	}
	for _, c := range []struct{ id, agent string }{{"t-asked", "*"}, {"t-default", ""}} {
		r, err := p.Converse(ctx, tenant, platform.Turn{ID: c.id, Agent: c.agent, Text: "tag the next release", Client: "app"})
		if err == nil && r.Agent != "helper" {
			t.Fatalf("%s through the client was answered by %s, off its list", c.id, r.Agent)
		}
		if err != nil && !errors.Is(err, platform.ErrNoTakers) {
			t.Fatalf("%s through the client = %v", c.id, err)
		}
	}
	if _, err := p.Converse(ctx, tenant, platform.Turn{ID: "t-ghost", Agent: "*", Text: "x", Client: "ghost"}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("a turn through a client that is not there = %v", err)
	}
}

// A move never leaves what the projects it moves run without its agent: a
// channel's default or a thread's, which would answer nothing, an agent that
// lives there, which would be available nowhere, or a schedule filed there,
// whose every occurrence admission would refuse. Each is named, and the move
// is refused until none is left.
func TestAMoveNeverLeavesWhatItsProjectsRunWithoutTheirAgent(t *testing.T) {
	p := start(t, call.Limits{})
	ctx := t.Context()
	w := projects.Writer{By: "ada", Govern: true}
	if _, err := p.Agents.Create(ctx, tenant, wire.AgentSpec{Name: "scout", Model: "offline", Provider: "offline", SystemPrompt: "Scout."}, "test"); err != nil {
		t.Fatal(err)
	}
	for _, in := range []platform.ProjectInput{{Name: "A"}, {Name: "B"}, {Name: "X", Parent: "A"}} {
		if _, err := p.CreateProject(ctx, tenant, w, in); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := p.Projects.Get(tenant, "B")
	if _, err := p.SetProjectAgents(ctx, tenant, "B", w, platform.AgentsInput{Version: b.Version, Enable: []string{}, Allow: []string{"helper"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.CreateChannel(ctx, tenant, "X", w, platform.ChannelInput{Name: "c", Default: "lead"}); err != nil {
		t.Fatal(err)
	}
	if err := p.SetAgentHome(ctx, tenant, "scout", "X", w); err != nil {
		t.Fatal(err)
	}
	th := startThread(t, p, admin, platform.ThreadInput{Project: "X", Title: "Renewal", Default: "lead", Text: "hello", PostOnly: true}).Thread
	if err := p.Schedules.Add(cron.Schedule{Name: "nightly", Expression: "0 3 * * *", Agent: "lead", Text: "sum", Tenant: tenant,
		Context: map[string]string{runs.MetaProject: "X"}}, platform.About{}); err != nil {
		t.Fatal(err)
	}
	move := func(want ...string) {
		t.Helper()
		x, _ := p.Projects.Get(tenant, "X")
		_, err := p.MoveProject(ctx, admin, "X", x.Version, "B")
		if len(want) == 0 {
			if err != nil {
				t.Fatalf("the move = %v", err)
			}
			return
		}
		if !errors.Is(err, projects.ErrInUse) {
			t.Fatalf("the move = %v, want in use", err)
		}
		for _, s := range want {
			if !strings.Contains(err.Error(), s) {
				t.Fatalf("the move = %v, want it to name %q", err, s)
			}
		}
		if x, _ := p.Projects.Get(tenant, "X"); x.Parent != "A" {
			t.Fatalf("a refused move put X under %q", x.Parent)
		}
	}

	move("the default of #c in X, lead, would not be available", "scout, which lives in X, would not be available there")
	if _, err := p.ArchiveChannel(tenant, "X", "c", w); err != nil {
		t.Fatal(err)
	}
	if err := p.SetAgentHome(ctx, tenant, "scout", "", w); err != nil {
		t.Fatal(err)
	}
	move("the schedule nightly, which runs lead in X, would be refused every time",
		fmt.Sprintf(`the default of the thread "Renewal" (%s) in X, lead, would not be available`, th.ID))
	if err := p.Schedules.Remove(tenant, "nightly"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpdateThread(ctx, admin, th.ID, platform.ThreadUpdate{Version: th.Version, Title: th.Title, Visibility: th.Visibility}); err != nil {
		t.Fatal(err)
	}
	move()
	if err := p.Available(tenant, "X", "lead"); !errors.Is(err, platform.ErrNotAvailable) {
		t.Fatalf("lead in X after the move = %v", err)
	}
}

// A client's message no agent takes is not handed to a named default: it
// waits.
func TestAClientsMessageNoAgentTakesWaits(t *testing.T) {
	p := start(t, call.Limits{})
	c := clients.Client{Tenant: tenant, ID: "docs", Surface: clients.Slack, Enabled: true, DefaultAgent: "lead", Agents: []string{clients.AnyAgent}}
	if got, err := p.JoinFor(t.Context(), c, "qqqq zzzz"); !errors.Is(err, platform.ErrNoTakers) {
		t.Fatalf("joining with nothing fitting = %+v, %v; want ErrNoTakers", got, err)
	}
}
