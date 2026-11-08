package platform_test

import (
	"net/http"
	"testing"

	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
	saigetypes "github.com/urmzd/saige/agent/types"
)

// A Slack mention from a person the client maps reaches the root session of
// its thread, and the root is as readable as the conversation: shared in a
// public channel, private to its members in a private one. The audience is
// read from Slack, never guessed from the channel's id.
func TestASlackMentionReachesItsThreadsRootAsReadableAsTheConversation(t *testing.T) {
	f := &fakes{}
	cfg := f.config()
	cfg.Policy = operators(t, "ada")
	p := stage(t, &cast{says: map[string][][]saigetypes.Delta{"worker": {say("Public answer."), say("Private answer.")}}}, cfg)
	in := slackClient("app", "A0APP", team, "worker")
	in.Slack.Machine, in.Slack.People = "svc-slack", map[string]string{"U0HUMAN1": "ada"}
	create(t, p, tenant, in)
	for _, place := range []struct {
		channel, ts string
		want        threads.Visibility
	}{
		{"C0PUBLIC", "1700000000.000100", threads.Shared},
		{"G0PRIVATE", "1700000000.000200", threads.Private},
	} {
		if code := signedSlack(p.IngressHandler(), "secret-app", mentionOf("A0APP", team, "Ev"+place.channel, place.channel, place.ts)); code != http.StatusOK {
			t.Fatalf("the mention in %s answered %d", place.channel, code)
		}
		var run runs.Run
		eventually(t, "the mention in "+place.channel+" started a run", func() bool {
			for _, r := range p.Runs.List(tenant, 20) {
				if r.Agent == "worker" && r.Actor.ID == "U0HUMAN1" && r.Thread != "" {
					if th, err := p.Threads.Get(tenant, r.Thread); err == nil && th.Session != nil && th.Session.Parent == "slack://"+team+"/"+place.channel+"/"+place.ts {
						run = r
						return true
					}
				}
			}
			return false
		})
		th, err := p.Threads.Get(tenant, run.Thread)
		if err != nil {
			t.Fatal(err)
		}
		if th.Visibility != place.want || run.Client != "app" || run.Actor.User != "ada" {
			t.Fatalf("in %s the root is %s, the run %q for %q; want %s", place.channel, th.Visibility, run.Client, run.Actor.User, place.want)
		}
	}
}
