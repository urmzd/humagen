package platform_test

import (
	"net/http"
	"testing"

	saigetypes "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/runtime"

	"github.com/urmzd/humagen/pkg/runs"
)

// On Slack only the app's bot user is mentionable, so the agent a person
// names right after "@Humagen", with or without "@", is who answers; any
// other first word is the default's.
func TestTheAgentNamedAfterTheAppsMentionAnswersOnSlack(t *testing.T) {
	f := &fakes{}
	answers := map[string][][]saigetypes.Delta{}
	for _, name := range []string{"docs", "ops"} {
		for range 4 {
			answers[name] = append(answers[name], say(name+" here."))
		}
	}
	p := stage(t, &cast{says: answers}, f.config())
	in := slackClient("docs-app", "A0DOCS", team, "docs")
	in.Agents = []string{"ops"}
	create(t, p, tenant, in)
	ingress := p.IngressHandler()
	mention := func(event, text string) {
		t.Helper()
		body := `{"type":"event_callback","api_app_id":"A0DOCS","team_id":"` + team + `","event_id":"` + event + `","event":{"type":"app_mention",` +
			`"user":"U0HUMAN1","text":"` + text + `","ts":"1700000000.000` + event[2:] + `00","channel":"C01"}}`
		if code := signedSlack(ingress, "secret-docs-app", body); code != http.StatusOK {
			t.Fatalf("%s answered %d", event, code)
		}
	}
	for event, c := range map[string]struct{ text, agent string }{
		"Ev1": {"<@U08BOTID> ops deploy it", "ops"},
		"Ev2": {"<@U08BOTID> ask ops: roll it back", "ops"},
		"Ev3": {"<@U08BOTID> what changed?", "docs"},
		"Ev4": {"<@U08BOTID> operations are slow", "docs"},
	} {
		mention(event, c.text)
		run := await(t, p, runtime.RunID(tenant, event), runs.Completed)
		if run.Agent != c.agent {
			t.Errorf("%q was answered by %s, want %s", c.text, run.Agent, c.agent)
		}
	}
}
