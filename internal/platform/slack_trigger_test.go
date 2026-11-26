package platform_test

import (
	"net/http"
	"strings"
	"testing"

	saigetypes "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/runtime"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/runs"
)

// On Slack only the app's bot user is mentionable, so the agent a person
// names right after an opening "@Humagen" is who answers: "ask ops", "ops:"
// or "@ops". A bare first word is part of the question, and so is the first
// word of a direct message, a thread reply, or a message that opens with
// someone else.
func TestTheAgentNamedAfterTheAppsMentionAnswersOnSlack(t *testing.T) {
	f := &fakes{}
	answers := map[string][][]saigetypes.Delta{}
	for _, name := range []string{"docs", "ops"} {
		for range 10 {
			answers[name] = append(answers[name], say(name+" here."))
		}
	}
	p := stage(t, &cast{says: answers}, f.config())
	in := slackClient("docs-app", "A0DOCS", team, "docs")
	in.Agents = []string{"ops"}
	create(t, p, tenant, in)
	ingress := p.IngressHandler()
	deliver := func(event, inner string) {
		t.Helper()
		body := `{"type":"event_callback","api_app_id":"A0DOCS","team_id":"` + team + `","event_id":"` + event + `",` +
			`"authorizations":[{"team_id":"` + team + `","user_id":"U08BOTID","is_bot":true}],"event":` + inner + `}`
		if code := signedSlack(ingress, "secret-docs-app", body); code != http.StatusOK {
			t.Fatalf("%s answered %d", event, code)
		}
	}
	mention := func(event, text string) {
		t.Helper()
		deliver(event, `{"type":"app_mention","user":"U0HUMAN1","text":"`+text+`","ts":"1700000000.000`+event[2:]+`00","channel":"C01"}`)
	}
	for _, c := range []struct{ event, text, agent string }{
		{"Ev1", "<@U08BOTID> ops: deploy it", "ops"},
		{"Ev2", "<@U08BOTID> ask ops roll it back", "ops"},
		{"Ev3", "<@U08BOTID> @ops look", "ops"},
		{"Ev4", "<@U08BOTID> what changed?", "docs"},
		{"Ev5", "<@U08BOTID> ops broke it", "docs"},
		// Someone else opens the message: the first word after them is words.
		{"Ev6", "<@U0ALICE> ops: deploy it <@U08BOTID>", "docs"},
	} {
		mention(c.event, c.text)
		run := await(t, p, runtime.RunID(tenant, c.event), runs.Completed)
		if run.Agent != c.agent {
			t.Errorf("%q was answered by %s, want %s", c.text, run.Agent, c.agent)
		}
	}
	// A direct message is addressed to the app, but names no agent by its first word.
	deliver("Ev7", `{"type":"message","channel_type":"im","user":"U0HUMAN1","text":"ops: deploy it","ts":"1700000000.000700","channel":"D01"}`)
	if run := runWith(t, p, "ops: deploy it", "D01"); run.Agent != "docs" {
		t.Errorf("a direct message was answered by %s, want docs", run.Agent)
	}
	// A reply in a thread that asks the app at its end reads its first word
	// as words too.
	deliver("Ev8", `{"type":"app_mention","user":"U0HUMAN1","text":"ops: broke it <@U08BOTID>","ts":"1700000000.000800","thread_ts":"1700000000.000400","channel":"C01"}`)
	if run := runWith(t, p, "ops: broke it", "C01"); run.Agent != "docs" {
		t.Errorf("a thread reply was answered by %s, want docs", run.Agent)
	}
}

// runWith waits for the run raised with text in a Slack channel.
func runWith(t *testing.T, p *platform.Platform, text, channel string) runs.Run {
	t.Helper()
	var found runs.Run
	eventually(t, "a run for "+text, func() bool {
		for _, r := range p.Runs.List(tenant, 50) {
			if strings.Contains(r.Text, text) && strings.Contains(r.Channel+" "+wire.Meta(r.Tag())[wire.MetaSlackChannel], channel) {
				found = r
				return true
			}
		}
		return false
	})
	return found
}
