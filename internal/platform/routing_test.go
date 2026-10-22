package platform_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/urmzd/warp/internal/platform"
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
