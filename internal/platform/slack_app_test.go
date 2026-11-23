package platform_test

import (
	"bytes"
	"errors"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/state"
)

// The Slack app a server starts acts for the people its Platform document
// maps, through a record the server keeps: made at the first start, brought
// in line with the file at the next, and never at the cost of what an
// administrator chose on it.
func TestTheSlackAppsRecordFollowsTheFileAndKeepsWhatAnAdministratorChose(t *testing.T) {
	st := state.NewMemory()
	var logs bytes.Buffer
	open := func() *platform.Platform {
		t.Helper()
		p, err := platform.New(platform.Config{
			Workspace: t.TempDir(), Backends: platform.Backends{State: st}, ClientInterval: -1,
			Log: slog.New(slog.NewTextHandler(&logs, nil)),
		})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	p := open()
	const machine = "slack-app:T01ACME"
	app := platform.SlackApp{Team: "T01ACME", Machine: machine, People: map[string]string{"U0ANA": "tailscale:ana@github", "U0BEN": "tailscale:ben@github"}}
	c, err := p.ApplySlackApp(t.Context(), tenant, "file", app)
	if err != nil {
		t.Fatal(err)
	}
	id := platform.SlackAppClient("T01ACME")
	if id != "slack-app-t01acme" || c.ID != id || c.Managed != clients.ManagedApp || !c.Enabled || c.Version != 1 ||
		c.Identity.Team != "T01ACME" || c.Slack.Machine != machine || !maps.Equal(c.Slack.People, app.People) {
		t.Fatalf("the record made = %+v (slack %+v)", c, c.Slack)
	}
	// As with no client: no default of its own, and any agent may be named.
	if c.DefaultAgent != "" || !slices.Equal(c.Agents, []string{clients.AnyAgent}) {
		t.Fatalf("the record routes to default %q and %v", c.DefaultAgent, c.Agents)
	}
	// It holds no secret, and nothing Slack sends is offered to it.
	if c.Credentials.Token.Set || c.Credentials.SigningSecret.Set {
		t.Fatalf("the record names secrets: %+v", c.Credentials)
	}
	// The same file again changes nothing.
	if again, err := p.ApplySlackApp(t.Context(), tenant, "file", app); err != nil || again.Version != 1 {
		t.Fatalf("the same file again = version %d, %v", again.Version, err)
	}

	// Only the credential the record names acts for its people, and only for
	// those the map names.
	who := auth.Principal{Subject: machine, Tenant: tenant, Kind: auth.Machine}
	ana, err := p.ActingFor(who, platform.ActAs{Client: id, User: "U0ANA"})
	if err != nil || ana.Subject != "tailscale:ana@github" || ana.Kind != auth.User || ana.Tenant != tenant {
		t.Fatalf("the app acting for U0ANA = %+v, %v", ana, err)
	}
	if got, err := p.ActingFor(who, platform.ActAs{Client: id, User: "U0NOBODY"}); !errors.Is(err, platform.ErrForbidden) || got.Subject != "" {
		t.Fatalf("the app acting for an unmapped user = %+v, %v", got, err)
	}
	for name, other := range map[string]auth.Principal{
		"another machine":       {Subject: "slack-app:T02OTHER", Tenant: tenant, Kind: auth.Machine},
		"a person of that name": {Subject: machine, Tenant: tenant, Kind: auth.User},
		"another workspace":     {Subject: machine, Tenant: "globex", Kind: auth.Machine},
	} {
		if got, err := p.ActingFor(other, platform.ActAs{Client: id, User: "U0ANA"}); err == nil {
			t.Errorf("%s acted for U0ANA as %+v", name, got)
		}
	}

	// An administrator narrows what the app may reach. The file's own fields
	// are not theirs to change here, and its secrets are not there to write.
	narrowed := c
	narrowed.DefaultAgent, narrowed.Agents = "lead", []string{"helper"}
	narrowed.Identity.Team = "T09EVIL"
	narrowed.Slack = &clients.SlackOptions{Sessions: true, Machine: "someone-else", People: map[string]string{"U0EVE": "tailscale:ana@github"}}
	got, err := p.UpdateClient(t.Context(), tenant, id, "ada", platform.ClientInput{Client: narrowed})
	if err != nil {
		t.Fatal(err)
	}
	if got.DefaultAgent != "lead" || !slices.Equal(got.Agents, []string{"helper"}) || !got.Slack.Sessions {
		t.Fatalf("an administrator's change = %+v", got)
	}
	if got.Identity.Team != "T01ACME" || got.Slack.Machine != machine || !maps.Equal(got.Slack.People, app.People) || got.Managed != clients.ManagedApp {
		t.Fatalf("the API changed what the file owns: %+v (slack %+v)", got, got.Slack)
	}
	if _, err := p.UpdateClient(t.Context(), tenant, id, "ada", platform.ClientInput{Client: got, Token: clients.SecretInput{Value: "xoxb-1"}}); !errors.Is(err, clients.ErrManaged) {
		t.Fatalf("a token written to the record = %v, want ErrManaged", err)
	}
	if err := p.DeleteClient(t.Context(), tenant, id, "ada"); !errors.Is(err, clients.ErrManaged) {
		t.Fatalf("deleting the record = %v, want ErrManaged", err)
	}
	// Nobody makes such a record through the API.
	forged := platform.ClientInput{Client: clients.Client{ID: "forged", Name: "forged", Surface: clients.Slack, Enabled: true, Managed: clients.ManagedApp, Identity: clients.Identity{Team: "T03X"}}}
	if _, err := p.CreateClient(t.Context(), tenant, "ada", forged); !errors.Is(err, clients.ErrManaged) {
		t.Fatalf("a record made through CreateClient = %v, want ErrManaged", err)
	}
	// Another Slack app in the same team is a client of its own beside it.
	if _, err := p.CreateClient(t.Context(), tenant, "ada", slackClient("webhook", "", "T01ACME", "lead")); err != nil {
		t.Fatalf("a webhook client in the app's team: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}

	// The next start, with ben gone from the file and cy added.
	p = open()
	defer func() { _ = p.Close() }()
	if strings.Contains(logs.String(), "could not be started") {
		t.Fatalf("the record was dialed at restart: %s", logs.String())
	}
	app.People = map[string]string{"U0ANA": "tailscale:ana@github", "U0CY": "tailscale:cy@github"}
	next, err := p.ApplySlackApp(t.Context(), tenant, "file", app)
	if err != nil {
		t.Fatal(err)
	}
	if !maps.Equal(next.Slack.People, app.People) || next.Slack.Machine != machine || next.Version != got.Version+1 {
		t.Fatalf("the record after the file changed = %+v (slack %+v)", next, next.Slack)
	}
	if next.DefaultAgent != "lead" || !slices.Equal(next.Agents, []string{"helper"}) || !next.Slack.Sessions || !next.Enabled {
		t.Fatalf("the start took back what an administrator chose: %+v (slack %+v)", next, next.Slack)
	}
	if _, err := p.ActingFor(who, platform.ActAs{Client: id, User: "U0BEN"}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("ben, taken out of the file, = %v", err)
	}
	if cy, err := p.ActingFor(who, platform.ActAs{Client: id, User: "U0CY"}); err != nil || cy.Subject != "tailscale:cy@github" {
		t.Fatalf("cy, added to the file, = %+v, %v", cy, err)
	}

	// A file that maps nobody any more leaves the credential nobody to act for.
	if err := p.ClearSlackApps(t.Context(), tenant, "file", id); err != nil {
		t.Fatal(err)
	}
	if kept, _ := p.Clients.Get(tenant, id); len(kept.Slack.People) != 2 {
		t.Fatalf("the record the file still maps was cleared: %+v", kept.Slack)
	}
	if err := p.ClearSlackApps(t.Context(), tenant, "file", ""); err != nil {
		t.Fatal(err)
	}
	cleared, err := p.Clients.Get(tenant, id)
	if err != nil || cleared.Slack.Machine != "" || len(cleared.Slack.People) != 0 || cleared.DefaultAgent != "lead" {
		t.Fatalf("the cleared record = %+v (slack %+v), %v", cleared, cleared.Slack, err)
	}
	if got, err := p.ActingFor(who, platform.ActAs{Client: id, User: "U0ANA"}); err == nil {
		t.Fatalf("the app acted for U0ANA through a cleared record as %+v", got)
	}
	// The webhook client beside it was never the app's to clear.
	if other, err := p.Clients.Get(tenant, "webhook"); err != nil || other.Managed != "" || !other.Enabled {
		t.Fatalf("the webhook client = %+v, %v", other, err)
	}
}

// A client that already has the record's id is not the app's: it is left
// exactly as it is, and the app gets no record.
func TestTheSlackAppsRecordNeverTakesOverAnotherClient(t *testing.T) {
	f := &fakes{}
	p := startWith(t, f.config())
	id := platform.SlackAppClient("T01ACME")
	mine := create(t, p, tenant, mapsNobody(slackClient(id, "A0MINE", "T01ACME", "lead")))
	_, err := p.ApplySlackApp(t.Context(), tenant, "file", platform.SlackApp{Team: "T01ACME", Machine: "slack-app:T01ACME", People: map[string]string{"U0ANA": "ana"}})
	if !errors.Is(err, clients.ErrExists) {
		t.Fatalf("applying over another client = %v, want ErrExists", err)
	}
	if kept, err := p.Clients.Get(tenant, id); err != nil || kept.Version != mine.Version || kept.Slack.Machine != "" || len(kept.Slack.People) != 0 {
		t.Fatalf("the other client = %+v, %v", kept, err)
	}
	if err := p.ClearSlackApps(t.Context(), tenant, "file", ""); err != nil {
		t.Fatal(err)
	}
	if kept, _ := p.Clients.Get(tenant, id); kept.Version != mine.Version {
		t.Fatalf("clearing touched another client: %+v", kept)
	}
}

// A Slack app's credential is one a Slack client names as its machine, or one
// the server made for the app it starts, and it starts no work for nobody of
// its own accord. Any other credential, and any person, may.
func TestASlackAppsCredentialIsHeldToAskingForAPerson(t *testing.T) {
	p, err := platform.New(platform.Config{Workspace: t.TempDir(), Backends: platform.Backends{State: state.NewMemory()}, ClientInterval: -1})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	in := slackClient("webhook", "A01APP", "T01ACME", "lead")
	in.Slack.Machine = "svc-slack"
	if _, err := p.CreateClient(t.Context(), tenant, "ada", in); err != nil {
		t.Fatal(err)
	}
	for name, who := range map[string]auth.Principal{
		"a client's machine":     {Subject: "svc-slack", Tenant: tenant, Kind: auth.Machine},
		"the server's app's":     {Subject: platform.SlackAppMachine("T09NEW"), Tenant: tenant, Kind: auth.Machine},
		"the machine, elsewhere": {Subject: platform.SlackAppMachine("T01ACME"), Tenant: "globex", Kind: auth.Machine},
	} {
		if !p.SlackMachine(who) || !errors.Is(p.MayAskForNobody(who), platform.ErrForbidden) {
			t.Errorf("%s may ask for nobody", name)
		}
	}
	for name, who := range map[string]auth.Principal{
		"another machine":          {Subject: "ci-bot", Tenant: tenant, Kind: auth.Machine},
		"a person of that subject": {Subject: "svc-slack", Tenant: tenant, Kind: auth.User},
	} {
		if p.SlackMachine(who) || p.MayAskForNobody(who) != nil {
			t.Errorf("%s is held to asking for a person", name)
		}
	}
}
