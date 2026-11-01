package threads_test

import (
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/threads"
)

func TestTheFirstRuleThatAppliesDecides(t *testing.T) {
	ignite := threads.Default{Agent: "ignite", From: threads.FromChannel}
	refused := func(m threads.Mention) threads.Mention { m.Refused = "not available in Globex"; return m }
	for _, c := range []struct {
		name   string
		in     threads.Input
		rule   threads.Rule
		agents []string
	}{
		{"tagged agents answer, not the default", threads.Input{Mentions: []threads.Mention{agent("scribe"), person("okta|dana")}, Default: ignite}, threads.RuleTagged, []string{"scribe"}},
		{"open asked is a tag", threads.Input{Mentions: []threads.Mention{agent(threads.OpenName)}}, threads.RuleTagged, []string{threads.OpenName}},
		{"a tag beats a reply", threads.Input{Mentions: []threads.Mention{agent("scribe")}, Reply: true}, threads.RuleTagged, []string{"scribe"}},
		{"a tag beats post only", threads.Input{Mentions: []threads.Mention{agent("scribe")}, PostOnly: true}, threads.RuleTagged, []string{"scribe"}},
		{"a refused agent sends nothing to the default", threads.Input{Mentions: []threads.Mention{refused(agent("scribe"))}, Default: ignite}, threads.RuleTagged, []string{}},
		{"people only", threads.Input{Mentions: []threads.Mention{person("okta|dana")}, Default: ignite}, threads.RulePeople, []string{}},
		{"experts only", threads.Input{Mentions: []threads.Mention{sme("billing")}, Default: ignite}, threads.RulePeople, []string{}},
		{"a role only", threads.Input{Mentions: []threads.Mention{role("sre")}, Default: ignite}, threads.RulePeople, []string{}},
		{"a skill with open added", threads.Input{Mentions: threads.WithSkillAgent([]threads.Mention{skillOf("review")}), Default: ignite}, threads.RuleTagged, []string{threads.OpenName}},
		{"a tag that names nobody is still a person's", threads.Input{Mentions: []threads.Mention{{Kind: threads.Person, Name: "ignit", Refused: "no one goes by @ignit"}}, Default: ignite}, threads.RulePeople, []string{}},
		{"a reply", threads.Input{Reply: true, PostOnly: true, Default: ignite}, threads.RuleReply, []string{}},
		{"post only", threads.Input{PostOnly: true, Default: ignite}, threads.RulePostOnly, []string{}},
		{"the default", threads.Input{Default: ignite}, threads.RuleDefault, []string{"ignite"}},
		{"no default", threads.Input{}, threads.RuleDefault, []string{}},
		{"the console runs only what it tags", threads.Input{TagsOnly: true, Default: ignite}, threads.RuleUntagged, []string{}},
		{"a tag answers in the console", threads.Input{TagsOnly: true, Mentions: []threads.Mention{agent("scribe")}, Default: ignite}, threads.RuleTagged, []string{"scribe"}},
		{"people only in the console", threads.Input{TagsOnly: true, Mentions: []threads.Mention{person("okta|dana")}, Default: ignite}, threads.RulePeople, []string{}},
		{"post only in the console", threads.Input{TagsOnly: true, PostOnly: true, Default: ignite}, threads.RulePostOnly, []string{}},
		{"a skill in the console goes to the agents that join", threads.Input{TagsOnly: true, Mentions: threads.WithSkillAgent([]threads.Mention{skillOf("triage"), person("okta|dana")}), Default: ignite}, threads.RuleTagged, []string{threads.OpenName}},
		{"a skill with an agent goes to that agent", threads.Input{TagsOnly: true, Mentions: threads.WithSkillAgent([]threads.Mention{skillOf("triage"), agent("scribe")}), Default: ignite}, threads.RuleTagged, []string{"scribe"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			d := threads.Decide(c.in)
			if d.Rule != c.rule || !reflect.DeepEqual(d.Agents, c.agents) {
				t.Fatalf("Decide = %+v; want %s answering %v", d, c.rule, c.agents)
			}
			if (len(d.Agents) == 0) != (d.Reason != "") {
				t.Fatalf("a decision with %v answering says %q", d.Agents, d.Reason)
			}
			if d.Default != (c.rule == threads.RuleDefault && len(c.agents) > 0) {
				t.Fatalf("Default = %v", d.Default)
			}
		})
	}
}

func TestEveryAgentAMessageTagsAnswers(t *testing.T) {
	in := threads.Input{Mentions: []threads.Mention{agent("a"), agent("b"), person("okta|dana"), agent("c"), agent("d"), agent("e"), agent("f")}, TagsOnly: true, Routed: []string{"g", "h"}, Mode: threads.ModeAll}
	d := threads.Decide(in)
	if d.Rule != threads.RuleTagged || !reflect.DeepEqual(d.Agents, []string{"a", "b", "c", "d", "e", "f"}) {
		t.Fatalf("Decide = %+v; want every tagged agent", d)
	}
	for i, m := range d.Mentions {
		if m.Refused != "" {
			t.Fatalf("tag %d refused: %+v", i, m)
		}
	}
}

func TestADefaultThatIsNoLongerAvailableFailsClosed(t *testing.T) {
	d := threads.Decide(threads.Input{Default: threads.Default{Agent: "ignite", From: "project:Field"}, Unavailable: "it is not available in Globex"})
	if d.Rule != threads.RuleDefault || len(d.Agents) != 0 || d.Default {
		t.Fatalf("Decide = %+v; want no run", d)
	}
	if d.Reason != "@ignite answers untagged messages here, but it is not available in Globex. Tag an agent." {
		t.Fatalf("reason = %q", d.Reason)
	}
}

func TestTheNearerDefaultsAreTheThreadsThenTheClientsThenTheChannels(t *testing.T) {
	renewals := projects.Channel{Project: "Globex", Name: "renewals", Default: "globex-helper"}
	got := threads.Nearer(threads.Thread{Default: "scribe"}, "slack-helper", renewals)
	want := []threads.Default{{Agent: "scribe", From: threads.FromThread}, {Agent: "slack-helper", From: threads.FromClient}, {Agent: "globex-helper", From: threads.FromChannel}}
	if !slices.Equal(got, want) {
		t.Fatalf("Nearer = %+v, want %+v", got, want)
	}
	if got := threads.Nearer(threads.Thread{}, "", projects.Channel{Name: projects.General}); slices.ContainsFunc(got, func(d threads.Default) bool { return d.Agent != "" }) {
		t.Fatalf("Nearer with none set = %+v", got)
	}
}

// A thread's selected agents answer its untagged messages: the best fit
// each of them that joins on its own, or each of them. A tag wins over them, and a
// message to people starts nothing unless it names a skill.
func TestSelectedAgentsAnswerUntaggedMessages(t *testing.T) {
	sel := []string{"scribe", "ignite"}
	for _, c := range []struct {
		name   string
		in     threads.Input
		rule   threads.Rule
		agents []string
		among  []string
	}{
		{"join", threads.Input{TagsOnly: true, Routed: sel, Mode: threads.ModeJoin}, threads.RuleRouted, []string{threads.OpenName}, sel},
		{"one selected", threads.Input{TagsOnly: true, Routed: []string{"scribe"}}, threads.RuleRouted, []string{"scribe"}, nil},
		{"all", threads.Input{TagsOnly: true, Routed: sel, Mode: threads.ModeAll}, threads.RuleRouted, sel, nil},
		{"a tag wins", threads.Input{TagsOnly: true, Routed: sel, Mode: threads.ModeAll, Mentions: []threads.Mention{agent("lead")}}, threads.RuleTagged, []string{"lead"}, nil},
		{"people only", threads.Input{TagsOnly: true, Routed: sel, Mentions: []threads.Mention{person("okta|dana")}}, threads.RulePeople, []string{}, nil},
		{"a skill to people", threads.Input{TagsOnly: true, Routed: sel, Mode: threads.ModeAll, Mentions: []threads.Mention{skillOf("triage"), person("okta|dana")}}, threads.RuleRouted, sel, nil},
		{"post only", threads.Input{TagsOnly: true, Routed: sel, PostOnly: true}, threads.RulePostOnly, []string{}, nil},
		{"a reply", threads.Input{TagsOnly: true, Routed: sel, Reply: true}, threads.RuleReply, []string{}, nil},
		{"none selected", threads.Input{TagsOnly: true}, threads.RuleUntagged, []string{}, nil},
		{"a surface keeps its default", threads.Input{Routed: sel, Mode: threads.ModeAll, Default: threads.Default{Agent: "lead", From: threads.FromChannel}}, threads.RuleDefault, []string{"lead"}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			d := threads.Decide(c.in)
			if d.Rule != c.rule || !reflect.DeepEqual(d.Agents, c.agents) || !reflect.DeepEqual(d.Among, c.among) {
				t.Fatalf("Decide = %+v; want %s answering %v among %v", d, c.rule, c.agents, c.among)
			}
		})
	}
}

func TestRoutingIsNormalizedAndBounded(t *testing.T) {
	if a, m, err := threads.Routing([]string{"a", "b", "a"}, ""); err != nil || !reflect.DeepEqual(a, []string{"a", "b"}) || m != threads.ModeJoin {
		t.Fatalf("Routing = %v %q %v", a, m, err)
	}
	if a, m, err := threads.Routing([]string{"a"}, threads.ModeAll); err != nil || m != "" || len(a) != 1 {
		t.Fatalf("one agent = %v %q %v", a, m, err)
	}
	for _, c := range []struct {
		agents []string
		mode   string
	}{
		{[]string{"a", "b", "c", "d", "e", "f"}, ""},
		{[]string{"a", "b", "c", "d"}, threads.ModeAll},
		{[]string{"a", "b"}, "every"},
		{[]string{"auto"}, ""},
	} {
		if _, _, err := threads.Routing(c.agents, c.mode); !errors.Is(err, threads.ErrInvalid) {
			t.Errorf("Routing(%v, %q) = %v", c.agents, c.mode, err)
		}
	}
}
