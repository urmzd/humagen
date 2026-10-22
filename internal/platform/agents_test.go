package platform_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/fiber/pkg/agentspec"
	"github.com/urmzd/fiber/pkg/connectors/cron"
	"github.com/urmzd/fiber/pkg/envelope"

	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/alerts"
	"github.com/urmzd/warp/pkg/auth"
	"github.com/urmzd/warp/pkg/call"
	"github.com/urmzd/warp/pkg/clients"
	"github.com/urmzd/warp/pkg/evals"
	"github.com/urmzd/warp/pkg/people"
	"github.com/urmzd/warp/pkg/pick"
	"github.com/urmzd/warp/pkg/policy"
	"github.com/urmzd/warp/pkg/projects"
	"github.com/urmzd/warp/pkg/runs"
	"github.com/urmzd/warp/pkg/state"
	"github.com/urmzd/warp/pkg/state/statetest"
)

// globexHelper adds an agent that lives in Globex, with Field's projects.
func globexHelper(t *testing.T, p *platform.Platform) {
	t.Helper()
	field(t, p)
	if _, err := p.Agents.Create(t.Context(), tenant, agentspec.AgentSpec{
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
	tg.Origin = runs.OriginSystem
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
		tg.Origin, tg.Meta["cron_schedule"] = "cron", "nightly"
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

func TestAutoPicksAmongTheAgentsAvailableWhereItIsAsked(t *testing.T) {
	p := start(t, call.Limits{})
	globexHelper(t, p)
	for _, spec := range []agentspec.AgentSpec{
		{Name: "biller", Model: "offline", Provider: "offline", SystemPrompt: "Bills.", Description: "billing and invoices"},
	} {
		if _, err := p.Agents.Create(t.Context(), tenant, spec, "test"); err != nil {
			t.Fatal(err)
		}
	}
	// In Globex, globex-helper lives there and matches; outside, it cannot
	// be picked, and biller can.
	got, err := p.Pick(t.Context(), tenant, "Globex", "the Globex renewals")
	if err != nil || got.Agent != "globex-helper" || !strings.HasPrefix(got.Reason, "Picked by @auto: matched") {
		t.Fatalf("in Globex = %+v, %v", got, err)
	}
	if got, err := p.Pick(t.Context(), tenant, "Initech", "the Globex renewals"); !errors.Is(err, pick.ErrNoAgent) {
		t.Fatalf("in Initech, a message only globex-helper matches = %+v, %v", got, err)
	}
	r, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "t1", Agent: "auto", Text: "an invoice question", Project: "Initech"})
	if err != nil || r.Agent != "biller" || !strings.Contains(r.Picked, "Picked by @auto: matched invoice") {
		t.Fatalf("@auto in Initech = %+v, %v", r, err)
	}
}

func TestAutoWithNoMatchHandsTheMessageToTheNearestNamedDefault(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	setAgents(t, p, "~", platform.AgentsInput{Enable: []string{"*"}, Default: "auto"})
	setAgents(t, p, "Field", platform.AgentsInput{Enable: []string{}, Default: "helper"})
	setAgents(t, p, "Globex", platform.AgentsInput{Enable: []string{}, Default: "auto"})
	r, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "t1", Text: "zzz", Project: "Renewals"})
	if err != nil || r.Agent != "helper" || !strings.Contains(r.Picked, "the default answered") {
		t.Fatalf("an untagged turn @auto cannot match = %+v, %v", r, err)
	}
	if _, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "t2", Text: "zzz"}); !errors.Is(err, pick.ErrNoAgent) {
		t.Fatalf("at the root, with no named default = %v, want pick.ErrNoAgent", err)
	}
}

func TestHomingAnAgentIsRefusedWhileSomethingElsewhereUsesIt(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	setAgents(t, p, "Initech", platform.AgentsInput{Enable: []string{}, Default: "helper"})
	if err := p.Schedules.Add(cron.Schedule{Name: "nightly", Expression: "0 3 * * *", Agent: "helper", Text: "sum", Tenant: tenant,
		Meta: map[string]string{runs.MetaProject: "Field"}}, platform.About{}); err != nil {
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
	if _, homed := p.ProjectTree(tenant).Home("helper"); homed {
		t.Fatal("a refused home was kept")
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
	if l := places["lead"]; l.Home != "" || len(l.EnabledIn) != 2 || l.EnabledIn[0] != "~" || l.EnabledIn[1] != "Field" {
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
		for tenantName, want := range map[string]string{"named": "lead", "unnamed": "auto"} {
			root := p.ProjectTree(tenantName).Root()
			if root.Agents.Default != want || len(root.Agents.Enable) != 1 || root.Agents.Enable[0] != "*" {
				t.Fatalf("the root of %s = %+v, want default %s", tenantName, root.Agents, want)
			}
			if _, held := storedSettings(t, st, tenantName)["baseline"]; held {
				t.Fatalf("the settings of %s still name a baseline", tenantName)
			}
			if h, _ := p.Projects.History(tenantName, projects.Root, 0, 0); len(h) == 0 || h[0].By != "migration" {
				t.Fatalf("the root of %s does not say the upgrade set it: %+v", tenantName, h)
			}
		}
		// A second start finds nothing to do.
		if err := p.Close(); err != nil {
			t.Fatal(err)
		}
		again, err := newOn(t, st)
		if err != nil {
			t.Fatal(err)
		}
		if h, _ := again.Projects.History("named", projects.Root, 0, 0); len(h) != 2 {
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
	if len(root.Agents.Enable) != 1 || root.Agents.Enable[0] != "*" || root.Agents.Default != "auto" {
		t.Fatalf("the root = %+v", root.Agents)
	}
}

func TestAWorkspaceNeverWrittenRunsAnyAgentAndPicksWithAuto(t *testing.T) {
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

func TestAClientTagForAutoIsPickedAmongWhatTheClientMayRoute(t *testing.T) {
	p := start(t, call.Limits{})
	c := clients.Client{Tenant: tenant, ID: "docs", Surface: clients.Slack, Enabled: true, DefaultAgent: "auto", Agents: []string{"helper"}}
	got, err := p.AutoFor(t.Context(), c, "Help.")
	if err != nil || got.Agent != "helper" {
		t.Fatalf("@auto for a client that routes to helper only = %+v, %v", got, err)
	}
	c.Agents = nil
	if _, err := p.AutoFor(t.Context(), c, "Help."); !errors.Is(err, pick.ErrNoAgent) {
		t.Fatalf("@auto for a client that routes to no agent = %v", err)
	}
}

func TestATurnCannotRunAutoItself(t *testing.T) {
	p := start(t, call.Limits{})
	if _, err := p.Invoke(t.Context(), envelope.Tag{ID: "a", Tenant: tenant, Agent: "auto", Origin: "api", Text: "hi"}); !errors.Is(err, platform.ErrNotAvailable) {
		t.Fatalf("a run of auto itself = %v", err)
	}
}

// @auto for a person a client's credential acts for picks only among the
// agents the client may route to, whether the turn asks for it or the
// client's default is auto, and never answers with an agent off its list.
func TestAutoThroughAClientPicksOnlyAmongItsAgents(t *testing.T) {
	f := &fakes{}
	p := startWith(t, f.config())
	ctx := t.Context()
	if _, err := p.Agents.Create(ctx, tenant, agentspec.AgentSpec{
		Name: "releaser", Model: "offline", Provider: "offline", SystemPrompt: "x", Description: "Cuts releases and tags versions.",
	}, "test"); err != nil {
		t.Fatal(err)
	}
	setAgents(t, p, "~", platform.AgentsInput{Enable: []string{"*"}, Default: "lead"})
	in := slackClient("app", "A0APP", team, "auto")
	in.Agents = []string{"helper"}
	create(t, p, tenant, in)
	if r, err := p.Converse(ctx, tenant, platform.Turn{ID: "t-free", Agent: "auto", Text: "tag the next release"}); err != nil || r.Agent != "releaser" {
		t.Fatalf("@auto with no client = %+v, %v", r, err)
	}
	for _, c := range []struct{ id, agent string }{{"t-asked", "auto"}, {"t-default", ""}} {
		r, err := p.Converse(ctx, tenant, platform.Turn{ID: c.id, Agent: c.agent, Text: "tag the next release", Client: "app"})
		if err == nil && r.Agent != "helper" {
			t.Fatalf("%s through the client was answered by %s, off its list", c.id, r.Agent)
		}
		if err != nil && !errors.Is(err, pick.ErrNoAgent) {
			t.Fatalf("%s through the client = %v", c.id, err)
		}
	}
	if _, err := p.Converse(ctx, tenant, platform.Turn{ID: "t-ghost", Agent: "auto", Text: "x", Client: "ghost"}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("a turn through a client that is not there = %v", err)
	}
}
