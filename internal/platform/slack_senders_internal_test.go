package platform

import (
	"errors"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/saige/flow/event"
)

func slackRule(t *testing.T) (*Platform, clients.Client) {
	t.Helper()
	p, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	c, err := p.Clients.Create("t", clients.Client{
		ID: "app", Name: "App", Surface: clients.Slack, Identity: clients.Identity{Team: "T01"}, Enabled: true, Agents: []string{"a"},
		Slack: &clients.SlackOptions{Machine: "svc-app", People: map[string]string{"U0ANA": "ana"}},
	}, clients.DefaultCaps(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return p, c
}

// A Slack user asks through a client when the client is enabled, names a
// team, the user writes from that team and the team maps them.
func TestWhoAsksThroughASlackClient(t *testing.T) {
	p, c := slackRule(t)
	for _, tc := range []struct {
		name, team, user string
		want             bool
	}{
		{"a mapped person in the client's team", "T01", "U0ANA", true},
		{"a mapped person, id padded", "T01", " U0ANA ", true},
		{"someone unmapped", "T01", "U0EVE", false},
		{"a mapped id from another team", "T02", "U0ANA", false},
		{"no team", "", "U0ANA", false},
		{"no user", "T01", "", false},
	} {
		if got := p.slackSender(c, tc.team, tc.user); got != tc.want {
			t.Errorf("%s: asks = %v, want %v", tc.name, got, tc.want)
		}
	}

	// A client that names no team asks for nobody, whatever the delivery says.
	teamless := c
	teamless.Managed, teamless.Identity.Team = clients.ManagedStatic, ""
	if p.slackSender(teamless, "", "U0ANA") || p.slackSender(teamless, "T01", "U0ANA") {
		t.Error("a client that names no team asked for someone")
	}
	off := c
	off.Enabled = false
	if _, err := p.Clients.Update("t", off, time.Now()); err != nil {
		t.Fatal(err)
	}
	if p.slackSender(c, "T01", "U0ANA") {
		t.Error("a disabled client asked for someone")
	}
}

// What a Slack client's connector raised is the client's only when it came
// from the client's own team.
func TestARaisedSlackEventIsHeldToTheClientsTeam(t *testing.T) {
	_, c := slackRule(t)
	tag := func(channel string) event.Envelope {
		return wire.NewEvent(wire.Raise{ID: "e", Tenant: "t", Origin: "slack", Channel: channel, At: time.Now()})
	}
	if !slackTeamOf(c, tag("slack://T01/C01?thread=1")) {
		t.Error("an event from the client's team was not its own")
	}
	if slackTeamOf(c, tag("slack://T02/C01?thread=1")) {
		t.Error("an event from another team was the client's")
	}
	c.Identity.Team = ""
	if slackTeamOf(c, tag("slack://T01/C01?thread=1")) {
		t.Error("a client that names no team took an event")
	}
}

// A run is stopped from Slack by the Slack user who asked for it, or for a
// run it called by whoever asked for its root, and by a person the client's
// team maps. Nobody else stops it.
func TestWhoMayStopARunFromSlack(t *testing.T) {
	p, c := slackRule(t)
	record := func(id, origin string, actor event.Actor, root string) runs.Run {
		t.Helper()
		tag := wire.NewEvent(wire.Raise{ID: "tag-" + id, Tenant: "t", Agent: "a", Origin: origin, Channel: "slack://T01/C01?thread=1", At: time.Now(), Actor: actor, Context: map[string]string{runs.MetaClient: c.ID}})
		if err := p.Runs.Record(id, tag, 1); err != nil {
			t.Fatal(err)
		}
		r, err := p.Runs.Get("t", id)
		if err != nil {
			t.Fatal(err)
		}
		if root != "" {
			r.Root = root
		}
		return r
	}
	asked := record("asked", runs.OriginSlack, event.Actor{ID: "U0BEN"}, "")
	called := record("called", runs.OriginAPI, event.Actor{ID: "svc"}, "asked")
	bot := record("bot", runs.OriginSlack, event.Actor{ID: "U0BOT", Bot: true}, "")
	api := record("api", runs.OriginAPI, event.Actor{ID: "U0BEN"}, "")
	for _, tc := range []struct {
		name string
		run  runs.Run
		who  string
		want bool
	}{
		{"whoever asked, though unmapped", asked, "U0BEN", true},
		{"whoever asked for the root of a called run", called, "U0BEN", true},
		{"a mapped person who did not ask", asked, "U0ANA", true},
		{"a mapped person, on a called run", called, "U0ANA", true},
		{"someone unmapped who did not ask", asked, "U0EVE", false},
		{"someone unmapped, on a called run", called, "U0EVE", false},
		{"no user", asked, "", false},
		{"a bot's id on a run a bot raised", bot, "U0BOT", false},
		{"an id that matches a run asked for elsewhere", api, "U0BEN", false},
	} {
		if got := p.slackMayStop(tc.run, tc.who); got != tc.want {
			t.Errorf("%s: may stop = %v, want %v", tc.name, got, tc.want)
		}
	}

	// A refusal is ErrForbidden from the platform, and final for a reaction:
	// the handler answers nil so the surface does not deliver it again, and
	// the run is not asked to stop.
	if err := p.stopFromSurface(t.Context(), "t", c.ID, "slack", "asked", "U0EVE"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("a stop by someone unmapped who did not ask: %v", err)
	}
	if err := p.feedbackFor(c).Stop(t.Context(), "t", "asked", event.Actor{ID: "U0EVE"}); err != nil {
		t.Fatalf("a refused reaction answered %v, want nil", err)
	}
	if r, _ := p.Runs.Get("t", "asked"); r.Stopping || r.Status.Terminal() {
		t.Fatalf("a refused stop changed the run: %s, stopping %v", r.Status, r.Stopping)
	}

	// A disabled client's map names nobody; whoever asked still may.
	c.Enabled = false
	if _, err := p.Clients.Update("t", c, time.Now()); err != nil {
		t.Fatal(err)
	}
	if p.slackMayStop(asked, "U0ANA") || !p.slackMayStop(asked, "U0BEN") {
		t.Error("with the client disabled, a mapped person stopped the run or whoever asked could not")
	}
}
