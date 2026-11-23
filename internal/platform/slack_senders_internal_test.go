package platform

import (
	"errors"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/sourcelinks"
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
		ID: "app", Name: "App", Surface: clients.Slack, Identity: clients.Identity{AppID: "A01", Team: "T01"}, Enabled: true, Agents: []string{"a"},
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
	// The team the sender belongs to, where the connector wrote it, is the
	// client's too.
	own, other := tag("slack://T01/C01?thread=1"), tag("slack://T01/C01?thread=1")
	wire.SetMeta(&own, wire.MetaSlackSenderTeam, "T01")
	wire.SetMeta(&other, wire.MetaSlackSenderTeam, "T02")
	if !slackTeamOf(c, own) || slackTeamOf(c, other) {
		t.Error("the sender's own team was not held to the client's")
	}
	c.Identity.Team = ""
	if slackTeamOf(c, tag("slack://T01/C01?thread=1")) {
		t.Error("a client that names no team took an event")
	}
}

// What a Slack client's connector raised for someone its team maps to
// nobody, or for a sender of another team, goes no further and nothing is
// kept about the channel it came from. For a person the team maps the
// channel is noted as before.
func TestARefusedSlackEventLeavesNothingAboutItsChannel(t *testing.T) {
	p, c := slackRule(t)
	if _, err := p.Projects.Create("t", projects.Project{Name: "Vault", Members: []projects.Entry{{Who: "ana", Role: projects.Owner}}}, projects.Writer{By: "ana", Govern: true}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := p.SourceLinks.Put("t", sourcelinks.Rule{ID: "deals", Provider: "slack", Attribute: "channel", Pattern: "C01", Project: "Vault", Mode: sourcelinks.Suggest}); err != nil {
		t.Fatal(err)
	}
	s, st := p.surfaces["slack"], &seat{client: c}
	raise := func(id, user, senderTeam string) []event.Envelope {
		tag := wire.NewEvent(wire.Raise{ID: id, Tenant: "t", Origin: "slack", Channel: "slack://T01/C01?thread=1", At: time.Now(),
			Actor: event.Actor{ID: user}, Context: map[string]string{wire.MetaSlackChannel: "C01", wire.MetaSlackEvent: "app_mention"}})
		if senderTeam != "" {
			wire.SetMeta(&tag, wire.MetaSlackSenderTeam, senderTeam)
		}
		return s.stamp(t.Context(), st, tag)
	}
	noted := func() int {
		rows, err := p.SourceSuggestions.List("t", "", "", 10, nil)
		if err != nil {
			t.Fatal(err)
		}
		return len(rows)
	}
	if out := raise("e1", "U0EVE", "T01"); len(out) != 0 || noted() != 0 {
		t.Fatalf("someone unmapped: %d events went on and %d notes were kept", len(out), noted())
	}
	if out := raise("e2", "U0ANA", "T02"); len(out) != 0 || noted() != 0 {
		t.Fatalf("a mapped id of another team: %d events went on and %d notes were kept", len(out), noted())
	}
	raise("e3", "U0ANA", "T01")
	if noted() == 0 {
		t.Fatal("a mapped person's channel was not noted")
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
		if got := p.slackMayStop(c.ID, tc.run, tc.who); got != tc.want {
			t.Errorf("%s: may stop = %v, want %v", tc.name, got, tc.want)
		}
	}

	// A refusal is ErrForbidden from the platform, and final for a reaction:
	// the handler answers nil so the surface does not deliver it again, and
	// the run is not asked to stop.
	if err := p.stopFromSurface(t.Context(), "t", c.ID, "slack", "asked", "U0EVE"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("a stop by someone unmapped who did not ask: %v", err)
	}
	// Who pressed is decided before whose run it is: a run another client
	// raised, or none raised, is refused to someone unmapped and is nothing
	// to stop for a mapped person. Neither is reported as stopped.
	if err := p.stopFromSurface(t.Context(), "t", "other-app", "slack", "asked", "U0EVE"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("someone unmapped, through a client that did not raise the run: %v", err)
	}
	if err := p.stopFromSurface(t.Context(), "t", "other-app", "slack", "asked", "U0BEN"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("whoever asked, through a client that did not raise the run: %v", err)
	}
	other, err := p.Clients.Create("t", clients.Client{ID: "second", Name: "Second", Surface: clients.Slack, Identity: clients.Identity{AppID: "A02", Team: "T01"}, Enabled: true, Agents: []string{"a"}}, clients.DefaultCaps(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := p.stopFromSurface(t.Context(), "t", other.ID, "slack", "asked", "U0ANA"); !errors.Is(err, errNothingToStop) {
		t.Fatalf("a mapped person, through a client that did not raise the run: %v", err)
	}
	if err := p.stopFromSurface(t.Context(), "t", c.ID, "slack", "no-such-run", "U0ANA"); !errors.Is(err, errNothingToStop) {
		t.Fatalf("a run that does not exist: %v", err)
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
	if p.slackMayStop(c.ID, asked, "U0ANA") || !p.slackMayStop(c.ID, asked, "U0BEN") {
		t.Error("with the client disabled, a mapped person stopped the run or whoever asked could not")
	}
}
