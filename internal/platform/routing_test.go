package platform_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/auth"
	"github.com/urmzd/warp/pkg/call"
	"github.com/urmzd/warp/pkg/projects"
	"github.com/urmzd/warp/pkg/runs"
	"github.com/urmzd/warp/pkg/threads"
)

func agentsOf(rs []runs.Run) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Agent)
	}
	slices.Sort(out)
	return out
}

// A conversation's selected agents answer its untagged messages: with all,
// each of them side by side; with best_fit, the one that fits, picked as
// @auto picks among them alone, which the run says. A tag wins over them,
// and with none selected an untagged message starts nothing. Anyone who may
// post in it selects them; an agent that does not exist, or cannot answer
// there, is refused.
func TestSelectedAgentsAnswerAConversationsUntaggedMessages(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	workIn(t, p, "Initech", operator, bo)
	ctx := t.Context()

	got, err := p.StartThread(ctx, operator, platform.ThreadInput{Project: "Initech", Visibility: threads.Shared, Agents: []string{"helper", "lead"}, Mode: threads.ModeAll, Text: "what do you both think?"})
	if err != nil {
		t.Fatal(err)
	}
	th := got.Thread
	if got.Rule != threads.RuleRouted || !slices.Equal(agentsOf(got.Runs), []string{"helper", "lead"}) || th.Mode != threads.ModeAll {
		t.Fatalf("an untagged message with all selected started %+v (%s)", agentsOf(got.Runs), got.Rule)
	}
	for _, r := range got.Runs {
		await(t, p, r.ID, runs.Completed)
	}
	tagged := post(t, p, operator, th.ID, platform.MessageInput{Text: "@lead only you"})
	if tagged.Rule != threads.RuleTagged || !slices.Equal(agentsOf(tagged.Runs), []string{"lead"}) {
		t.Fatalf("a tag with agents selected started %+v (%s)", agentsOf(tagged.Runs), tagged.Rule)
	}
	await(t, p, tagged.Runs[0].ID, runs.Completed)

	// bo, a member who did not start it, changes only which agents answer.
	cur, _ := p.Threads.Get(tenant, th.ID)
	upd, err := p.UpdateThread(ctx, bo, th.ID, platform.ThreadUpdate{Version: cur.Version, Visibility: threads.Shared, Agents: []string{"helper", "lead"}, Mode: threads.ModeBestFit})
	if err != nil || upd.Mode != threads.ModeBestFit {
		t.Fatalf("a member selecting agents = %+v, %v", upd, err)
	}
	if _, err := p.UpdateThread(ctx, bo, th.ID, platform.ThreadUpdate{Version: upd.Version, Title: "renamed", Visibility: threads.Shared, Agents: upd.Agents, Mode: upd.Mode}); !errors.Is(err, projects.ErrForbidden) {
		t.Fatalf("a member who did not start it renamed it: %v", err)
	}
	best := post(t, p, bo, th.ID, platform.MessageInput{Text: "who should take this?"})
	if best.Rule != threads.RuleRouted || len(best.Runs) != 1 || !slices.Contains([]string{"helper", "lead"}, best.Runs[0].Agent) || best.Runs[0].Picked == "" {
		t.Fatalf("an untagged message with best_fit started %+v (%s)", best.Runs, best.Rule)
	}
	await(t, p, best.Runs[0].ID, runs.Completed)

	for _, c := range []struct {
		agents []string
		mode   string
		want   string
	}{
		{[]string{"ghost"}, "", "there is no agent called ghost"},
		{[]string{"helper", "lead", "auto"}, "", "no agent can be called"},
		{[]string{"a1", "a2", "a3", "a4", "a5", "a6"}, "", "at most 5"},
	} {
		if _, err := p.UpdateThread(ctx, bo, th.ID, platform.ThreadUpdate{Version: upd.Version, Visibility: threads.Shared, Agents: c.agents, Mode: c.mode}); !errors.Is(err, projects.ErrInvalid) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("selecting %v = %v", c.agents, err)
		}
	}
	// Initech stops allowing helper: selecting it is refused, as a default
	// that cannot answer there is.
	setAgents(t, p, "Initech", platform.AgentsInput{Enable: []string{}, Allow: []string{"lead"}})
	if _, err := p.UpdateThread(ctx, bo, th.ID, platform.ThreadUpdate{Version: upd.Version, Visibility: threads.Shared, Agents: []string{"helper"}}); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("selecting an agent not available there = %v", err)
	}
	// Selecting none: an untagged message starts nothing again.
	none, err := p.UpdateThread(ctx, bo, th.ID, platform.ThreadUpdate{Version: upd.Version, Visibility: threads.Shared})
	if err != nil || len(none.Agents) != 0 || none.Mode != "" {
		t.Fatalf("selecting none = %+v, %v", none, err)
	}
	if quiet := post(t, p, bo, th.ID, platform.MessageInput{Text: "anyone?"}); quiet.Rule != threads.RuleUntagged || len(quiet.Runs) != 0 {
		t.Fatalf("an untagged message with none selected started %+v (%s)", quiet.Runs, quiet.Rule)
	}
}

// A skill named in a conversation with selected agents goes to them, and
// they are told to use it; with none selected it goes to @auto.
func TestASkillGoesToTheSelectedAgents(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	workIn(t, p, "Initech", operator)
	declare(t, p, "Initech", "triage")
	got, err := p.StartThread(t.Context(), operator, platform.ThreadInput{Project: "Initech", Agents: []string{"lead"}, Text: "$triage this one"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Rule != threads.RuleRouted || len(got.Runs) != 1 || got.Runs[0].Agent != "lead" {
		t.Fatalf("a skill with lead selected started %+v (%s)", agentsOf(got.Runs), got.Rule)
	}
	if !strings.Contains(strings.Join(pinned(t, got.Runs[0]), ","), "triage") {
		t.Fatalf("lead was not told to use the skill: %v", pinned(t, got.Runs[0]))
	}
	for _, mn := range got.Message.Mentions {
		if mn.Kind == threads.Agent {
			t.Fatalf("a skill with agents selected was written as a tag of %s", mn.Name)
		}
	}
}

// A selected agent the poster may not read is no oracle: naming it answers
// as naming an agent that does not exist, whether or not the thread selects
// it, and a default they were not shown is changed only by its owners.
// Rewriting the selection drops it once it cannot answer there, and a skill
// goes to the agents the poster may read.
func TestAHiddenSelectedAgentIsNoOracle(t *testing.T) {
	p := start(t, call.Limits{})
	ctx := t.Context()
	// Vault is read by its members alone. helper is selected in a Field
	// conversation and then homed in Vault, which bo cannot read.
	field(t, p)
	workIn(t, p, "Field", operator, bo)
	if _, err := p.CreateProject(ctx, tenant, ada, platform.ProjectInput{Name: "Vault"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Projects.Update(tenant, "Vault", 1, ada, time.Now(), func(pr *projects.Project) error {
		pr.Members = []projects.Entry{{Who: "opal", Role: projects.Member}, {Who: "ada", Role: projects.Owner}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	declare(t, p, "Field", "triage")
	got, err := p.StartThread(ctx, operator, platform.ThreadInput{Project: "Field", Visibility: threads.Shared, Agents: []string{"helper", "lead"}, Default: "helper", Text: "x", PostOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	th := got.Thread
	if err := p.SetAgentHome(ctx, tenant, "helper", "Vault", govern); err != nil {
		t.Fatal(err)
	}
	if p.Seer(bo).Agent("helper") || !p.Seer(bo).Project("Field") {
		t.Fatal("the test needs bo to read Field and not helper")
	}
	if shown := p.Seer(bo).Thread(th); !slices.Equal(shown.Agents, []string{"lead"}) || shown.Default != "" || shown.Mode != "" {
		t.Fatalf("bo is shown %+v", shown)
	}
	refusal := func(agent string) string {
		_, err := p.UpdateThread(ctx, bo, th.ID, platform.ThreadUpdate{Version: th.Version, Visibility: threads.Shared, Agents: []string{agent}})
		if err == nil {
			return "accepted"
		}
		return strings.ReplaceAll(err.Error(), agent, "X")
	}
	if selected, ghost := refusal("helper"), refusal("ghost"); selected != ghost {
		t.Fatalf("naming the hidden selected agent = %q, an unknown one = %q", selected, ghost)
	}
	ownerOnly := func(def string) string {
		_, err := p.UpdateThread(ctx, bo, th.ID, platform.ThreadUpdate{Version: th.Version, Visibility: threads.Shared, Default: def, Agents: []string{"lead"}})
		return strings.ReplaceAll(fmt.Sprint(err), def, "X")
	}
	if hidden, ghost := ownerOnly("helper"), ownerOnly("ghost"); hidden != ghost {
		t.Fatalf("naming the hidden default = %q, an unknown one = %q", hidden, ghost)
	}
	// helper cannot answer in Field now, so bo's rewrite drops it, saying
	// nothing; the default is the owners' to change.
	kept, err := p.UpdateThread(ctx, bo, th.ID, platform.ThreadUpdate{Version: th.Version, Visibility: threads.Shared, Agents: []string{"lead"}})
	if err != nil || !slices.Equal(kept.Agents, []string{"lead"}) || kept.Default != "helper" {
		t.Fatalf("bo rewriting the selection = %+v, %v", kept, err)
	}
	skill := post(t, p, bo, th.ID, platform.MessageInput{Text: "$triage this"})
	if skill.Rule != threads.RuleRouted || len(skill.Runs) != 1 || skill.Runs[0].Agent != "lead" {
		t.Fatalf("bo's skill started %+v (%s)", agentsOf(skill.Runs), skill.Rule)
	}
}

// In a Slack thread whose client lists the agents it routes to, the best fit
// is picked among the selected agents it may route to, and the client is held
// to the agent picked, not to @auto.
func TestTheBestFitInASlackThreadIsOneTheClientRoutesTo(t *testing.T) {
	p, machine := slackRoom(t, func(in *platform.ClientInput) { in.Agents = []string{"helper", "lead"} })
	ana := auth.Principal{Subject: "ana", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Operator}}
	workIn(t, p, "Field", ana)
	got, err := p.StartThread(t.Context(), machine, fromSlack("C01RENEW", "1700000000.000100", "", "<@UAPP> @lead hello"))
	if err != nil {
		t.Fatal(err)
	}
	await(t, p, got.Runs[0].ID, runs.Completed)
	th, _ := p.Threads.Get(tenant, got.Thread.ID)
	if _, err := p.UpdateThread(t.Context(), ana, th.ID, platform.ThreadUpdate{Version: th.Version, Visibility: th.Visibility, Agents: []string{"helper", "lead"}}); err != nil {
		t.Fatal(err)
	}
	best := post(t, p, ana, th.ID, platform.MessageInput{Text: "which of you?"})
	if best.Rule != threads.RuleRouted || len(best.Runs) != 1 {
		t.Fatalf("the best fit in a Slack thread started %+v (%s: %s)", best.Runs, best.Rule, best.Reason)
	}
}

// A private conversation moves to another project or channel through an
// update, staying private, while no run has been filed in it.
func TestAPrivateConversationMovesWhileItHasNoRuns(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	workIn(t, p, "Initech", operator, bo)
	workIn(t, p, "Field", operator)
	ctx := t.Context()
	got, err := p.StartThread(ctx, operator, platform.ThreadInput{Project: "Initech", Text: "a draft"})
	if err != nil {
		t.Fatal(err)
	}
	th := got.Thread
	moved, err := p.UpdateThread(ctx, operator, th.ID, platform.ThreadUpdate{Version: th.Version, Project: "Field", Agents: []string{"lead"}})
	if err != nil || moved.Project != "Field" || moved.Channel != projects.General || moved.Visibility != threads.Private || !slices.Equal(moved.Agents, []string{"lead"}) {
		t.Fatalf("moving it = %+v, %v", moved, err)
	}
	if _, err := p.UpdateThread(ctx, operator, th.ID, platform.ThreadUpdate{Version: moved.Version, Project: "Renewals"}); !errors.Is(err, projects.ErrForbidden) && !errors.Is(err, projects.ErrNotFound) {
		t.Fatalf("moving it where its starter is not a member = %v", err)
	}
	asked := post(t, p, operator, th.ID, platform.MessageInput{Text: "go on"})
	if len(asked.Runs) != 1 || asked.Runs[0].Project != "Field" {
		t.Fatalf("the routed message started %+v", asked.Runs)
	}
	await(t, p, asked.Runs[0].ID, runs.Completed)
	cur, _ := p.Threads.Get(tenant, th.ID)
	if _, err := p.UpdateThread(ctx, operator, th.ID, platform.ThreadUpdate{Version: cur.Version, Project: "Initech", Agents: cur.Agents}); !errors.Is(err, platform.ErrStaysInProject) {
		t.Fatalf("moving a conversation that has runs = %v", err)
	}
	shared, err := p.StartThread(ctx, operator, platform.ThreadInput{Project: "Field", Visibility: threads.Shared, Text: "open"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpdateThread(ctx, operator, shared.Thread.ID, platform.ThreadUpdate{Version: shared.Thread.Version, Visibility: threads.Shared, Project: "Initech"}); !errors.Is(err, threads.ErrInvalid) {
		t.Fatalf("moving a shared thread through an update = %v", err)
	}
}
