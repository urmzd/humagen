package threads_test

import (
	"reflect"
	"testing"

	"github.com/urmzd/warp/pkg/projects"
	"github.com/urmzd/warp/pkg/threads"
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
		{"@auto is a tag", threads.Input{Mentions: []threads.Mention{agent("auto")}}, threads.RuleTagged, []string{"auto"}},
		{"a tag beats a reply", threads.Input{Mentions: []threads.Mention{agent("scribe")}, Reply: true}, threads.RuleTagged, []string{"scribe"}},
		{"a tag beats post only", threads.Input{Mentions: []threads.Mention{agent("scribe")}, PostOnly: true}, threads.RuleTagged, []string{"scribe"}},
		{"a refused agent sends nothing to the default", threads.Input{Mentions: []threads.Mention{refused(agent("scribe"))}, Default: ignite}, threads.RuleTagged, []string{}},
		{"people only", threads.Input{Mentions: []threads.Mention{person("okta|dana")}, Default: ignite}, threads.RulePeople, []string{}},
		{"experts only", threads.Input{Mentions: []threads.Mention{sme("billing")}, Default: ignite}, threads.RulePeople, []string{}},
		{"a tag that names nobody is still a person's", threads.Input{Mentions: []threads.Mention{{Kind: threads.Person, Name: "ignit", Refused: "no one goes by @ignit"}}, Default: ignite}, threads.RulePeople, []string{}},
		{"a reply", threads.Input{Reply: true, PostOnly: true, Default: ignite}, threads.RuleReply, []string{}},
		{"post only", threads.Input{PostOnly: true, Default: ignite}, threads.RulePostOnly, []string{}},
		{"the default", threads.Input{Default: ignite}, threads.RuleDefault, []string{"ignite"}},
		{"no default", threads.Input{}, threads.RuleDefault, []string{}},
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

func TestAtMostThreeAgentsAnswerOneMessage(t *testing.T) {
	in := threads.Input{Mentions: []threads.Mention{agent("a"), agent("b"), person("okta|dana"), agent("c"), agent("d"), agent("e")}}
	d := threads.Decide(in)
	if !reflect.DeepEqual(d.Agents, []string{"a", "b", "c"}) {
		t.Fatalf("agents = %v", d.Agents)
	}
	for i, m := range d.Mentions {
		if over := m.Name == "d" || m.Name == "e"; over != (m.Refused != "") {
			t.Fatalf("tag %d = %+v", i, m)
		}
	}
	if in.Mentions[3].Refused != "" || in.Mentions[4].Refused != "" {
		t.Fatal("Decide changed its input")
	}
}

func TestADefaultThatIsNoLongerAvailableFailsClosed(t *testing.T) {
	d := threads.Decide(threads.Input{Default: threads.Default{Agent: "ignite", From: threads.FromProject("Field")}, Unavailable: "it is not available in Globex"})
	if d.Rule != threads.RuleDefault || len(d.Agents) != 0 || d.Default {
		t.Fatalf("Decide = %+v; want no run", d)
	}
	if d.Reason != "@ignite answers untagged messages here, but it is not available in Globex. Tag an agent." {
		t.Fatalf("reason = %q", d.Reason)
	}
}

func TestTheDefaultIsTheNearestOneSet(t *testing.T) {
	chain := []threads.Default{{From: threads.FromProject("Globex")}, {Agent: "field-helper", From: threads.FromProject("Field")}, {Agent: "auto", From: threads.FromProject(projects.Root)}}
	renewals := projects.Channel{Project: "Globex", Name: "renewals", Default: "globex-helper"}
	general := projects.Channel{Project: "Globex", Name: projects.General}
	for _, c := range []struct {
		name   string
		thread threads.Thread
		client string
		ch     projects.Channel
		chain  []threads.Default
		want   threads.Default
	}{
		{"the thread's", threads.Thread{Default: "scribe"}, "slack-helper", renewals, chain, threads.Default{Agent: "scribe", From: threads.FromThread}},
		{"the client's", threads.Thread{}, "slack-helper", renewals, chain, threads.Default{Agent: "slack-helper", From: threads.FromClient}},
		{"the channel's", threads.Thread{}, "", renewals, chain, threads.Default{Agent: "globex-helper", From: threads.FromChannel}},
		{"the nearest project's", threads.Thread{}, "", general, chain, threads.Default{Agent: "field-helper", From: "project:Field"}},
		{"the root's", threads.Thread{}, "", general, chain[2:], threads.Default{Agent: "auto", From: "project:~"}},
		{"none", threads.Thread{}, "", general, nil, threads.Default{}},
	} {
		if got := threads.DefaultOf(c.thread, c.client, c.ch, c.chain); got != c.want {
			t.Errorf("%s: DefaultOf = %+v, want %+v", c.name, got, c.want)
		}
	}
}
