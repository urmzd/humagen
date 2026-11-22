package platform

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/accounts"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/connectionauth"
	"github.com/urmzd/saige/flow/event"
)

// fakeSurfaces is GitHub's and Slack's OAuth and identity endpoints.
type fakeSurfaces struct {
	mu sync.Mutex
	// githubUser and slackUser are what each identity endpoint answers;
	// an empty one answers 500.
	githubUser, slackUser string
	// bearer is the token the last identity request carried.
	bearer string
}

func (f *fakeSurfaces) RoundTrip(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	answer := func(status int, body string) (*http.Response, error) {
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	}
	switch r.URL.Host + r.URL.Path {
	case "github.com/login/oauth/access_token":
		return answer(200, `{"access_token":"gh-user-token","token_type":"bearer","expires_in":28800,"refresh_token":"gh-refresh"}`)
	case "slack.com/api/openid.connect.token":
		return answer(200, `{"ok":true,"access_token":"slack-user-token","token_type":"Bearer","id_token":"x.y.z"}`)
	case "api.github.com/user":
		f.bearer = r.Header.Get("Authorization")
		if f.githubUser == "" {
			return answer(500, `{}`)
		}
		return answer(200, f.githubUser)
	case "slack.com/api/openid.connect.userInfo":
		f.bearer = r.Header.Get("Authorization")
		if f.slackUser == "" {
			return answer(500, `{}`)
		}
		return answer(200, f.slackUser)
	}
	return answer(404, `{}`)
}

const (
	octocat  = `{"id":42,"login":"octocat","avatar_url":"https://avatars.githubusercontent.com/u/42","type":"User"}`
	slackAna = `{"ok":true,"sub":"U0ANA","https://slack.com/user_id":"U0ANA","https://slack.com/team_id":"T0TEAM","name":"Ana","picture":"https://avatars.slack-edge.com/ana.png"}`
)

func linkFixture(t *testing.T) (*Platform, *fakeSurfaces) {
	t.Helper()
	fake := &fakeSurfaces{githubUser: octocat, slackUser: slackAna}
	var providers []connectionauth.Provider
	for _, name := range []string{GitHubAccount, SlackAccount} {
		p := NativeConnectionProvider(name)
		p.ClientID, p.ClientSecret = "app-client", "app-secret"
		providers = append(providers, p)
	}
	p, err := New(Config{Workspace: t.TempDir(), ConnectionAuth: &connectionauth.Config{Key: []byte(strings.Repeat("k", 32)), RedirectURL: "https://humagen.test/connections/callback", Providers: providers, HTTP: &http.Client{Transport: fake}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p, fake
}

func person(tenant, subject string) auth.Principal {
	return auth.Principal{Tenant: tenant, Subject: subject, Kind: auth.User, Scopes: []auth.Permission{auth.RunsRead, auth.RunsWrite}}
}

// beginLink starts a link and returns the state the consent carries.
func beginLink(t *testing.T, p *Platform, who auth.Principal, name string) string {
	t.Helper()
	a, err := p.AuthorizeConnection(t.Context(), who, name, ConnectionAuthorizationInput{Scope: "person"})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(a.URL)
	if u.Query().Get("client_id") != "app-client" || u.Query().Get("code_challenge_method") != "S256" || u.Query().Get("redirect_uri") != "https://humagen.test/connections/callback" {
		t.Fatalf("consent URL %s", a.URL)
	}
	return u.Query().Get("state")
}

func TestGitHubAccountLinksByIDAndKeepsNoToken(t *testing.T) {
	p, fake := linkFixture(t)
	ana := person("t", "ana")
	state := beginLink(t, p, ana, GitHubAccount)
	if err := p.CompleteConnection(t.Context(), ana, GitHubAccount, state, "code"); err != nil {
		t.Fatal(err)
	}
	if fake.bearer != "Bearer gh-user-token" {
		t.Fatalf("GET /user carried %q", fake.bearer)
	}
	got, err := p.Accounts.Of("t", accounts.GitHub, "ana")
	if err != nil || got.ID != "42" || got.Login != "octocat" || got.Avatar == "" {
		t.Fatalf("linked %+v %v", got, err)
	}
	// The token was used for GET /user and is not kept anywhere.
	if st, err := p.ConnectionAuth.Status("t", "ana", GitHubAccount); err != nil || st.Connected {
		t.Fatalf("a token was stored: %+v %v", st, err)
	}
	if held, err := p.ConnectionAuth.Connections("t", "ana"); err != nil || len(held) != 0 {
		t.Fatalf("connections kept: %+v %v", held, err)
	}
	if _, err := p.ConnectionAuth.Token(t.Context(), "t", "ana", GitHubAccount); !errors.Is(err, connectionauth.ErrReconnect) {
		t.Fatalf("a token is usable: %v", err)
	}
	presets, err := p.ConnectionPresets(ana)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range presets {
		if c.ID == GitHubAccount && (!c.Connected || !c.Available || c.Account == nil || c.Account.Login != "octocat" || c.Health != nil) {
			t.Fatalf("preset %+v", c)
		}
	}
	// The state is spent.
	if err := p.CompleteConnection(t.Context(), ana, GitHubAccount, state, "code"); !errors.Is(err, connectionauth.ErrInvalid) {
		t.Fatalf("state replayed: %v", err)
	}
	if err := p.DisconnectConnection(ana, GitHubAccount); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Accounts.Of("t", accounts.GitHub, "ana"); !errors.Is(err, accounts.ErrNotFound) {
		t.Fatalf("still linked after Disconnect: %v", err)
	}
}

func TestAccountLinkRefusesAWrongState(t *testing.T) {
	p, _ := linkFixture(t)
	ana := person("t", "ana")
	state := beginLink(t, p, ana, GitHubAccount)
	if err := p.CompleteConnection(t.Context(), ana, GitHubAccount, state+"x", "code"); !errors.Is(err, connectionauth.ErrInvalid) {
		t.Fatalf("wrong state: %v", err)
	}
	if _, err := p.Accounts.Of("t", accounts.GitHub, "ana"); !errors.Is(err, accounts.ErrNotFound) {
		t.Fatal("a wrong state linked")
	}
	// The right one still works: a wrong guess does not spend it.
	if err := p.CompleteConnection(t.Context(), ana, GitHubAccount, state, "code"); err != nil {
		t.Fatal(err)
	}
}

func TestAccountLinkRefusesAnotherPersonsState(t *testing.T) {
	p, _ := linkFixture(t)
	ana, bob := person("t", "ana"), person("t", "bob")
	state := beginLink(t, p, ana, GitHubAccount)
	if err := p.CompleteConnection(t.Context(), bob, GitHubAccount, state, "code"); !errors.Is(err, connectionauth.ErrInvalid) {
		t.Fatalf("bob completed ana's consent: %v", err)
	}
	if _, err := p.Accounts.Of("t", accounts.GitHub, "bob"); !errors.Is(err, accounts.ErrNotFound) {
		t.Fatal("bob was linked through ana's state")
	}
	// Nor in another workspace.
	if err := p.CompleteConnection(t.Context(), person("t2", "ana"), GitHubAccount, state, "code"); !errors.Is(err, connectionauth.ErrInvalid) {
		t.Fatalf("another tenant completed it: %v", err)
	}
}

func TestAccountLinkFailsWhenTheSurfaceSaysNothing(t *testing.T) {
	p, fake := linkFixture(t)
	ana := person("t", "ana")
	for name, set := range map[string]func(string){
		GitHubAccount: func(v string) { fake.githubUser = v },
		SlackAccount:  func(v string) { fake.slackUser = v },
	} {
		set("")
		state := beginLink(t, p, ana, name)
		if err := p.CompleteConnection(t.Context(), ana, name, state, "code"); !errors.Is(err, connectionauth.ErrReconnect) {
			t.Fatalf("%s identity failure: %v", name, err)
		}
		set(`{"ok":false}`)
		state = beginLink(t, p, ana, name)
		if err := p.CompleteConnection(t.Context(), ana, name, state, "code"); !errors.Is(err, ErrAccountRefused) {
			t.Fatalf("%s empty identity: %v", name, err)
		}
	}
	for _, surface := range []string{accounts.GitHub, accounts.Slack} {
		if _, err := p.Accounts.Of("t", surface, "ana"); !errors.Is(err, accounts.ErrNotFound) {
			t.Fatalf("%s linked without an identity", surface)
		}
	}
}

func TestAccountLinkOnePersonPerUserUntilUnlinked(t *testing.T) {
	p, _ := linkFixture(t)
	ana, bob := person("t", "ana"), person("t", "bob")
	if err := p.CompleteConnection(t.Context(), ana, GitHubAccount, beginLink(t, p, ana, GitHubAccount), "code"); err != nil {
		t.Fatal(err)
	}
	if err := p.CompleteConnection(t.Context(), bob, GitHubAccount, beginLink(t, p, bob, GitHubAccount), "code"); !errors.Is(err, accounts.ErrClaimed) {
		t.Fatalf("second claim: %v", err)
	}
	// In another workspace the same GitHub user is free.
	carol := person("t2", "carol")
	if err := p.CompleteConnection(t.Context(), carol, GitHubAccount, beginLink(t, p, carol, GitHubAccount), "code"); err != nil {
		t.Fatalf("another tenant refused: %v", err)
	}
	if err := p.DisconnectConnection(ana, GitHubAccount); err != nil {
		t.Fatal(err)
	}
	if err := p.CompleteConnection(t.Context(), bob, GitHubAccount, beginLink(t, p, bob, GitHubAccount), "code"); err != nil {
		t.Fatalf("claim after unlink: %v", err)
	}
	if got, _ := p.Accounts.Get("t2", accounts.GitHub, "42"); got.Subject != "carol" {
		t.Fatalf("t2's link moved: %+v", got)
	}
}

func TestSlackAccountNeedsTheWorkspacesTeam(t *testing.T) {
	p, fake := linkFixture(t)
	ana := person("t", "ana")
	if err := p.CompleteConnection(t.Context(), ana, SlackAccount, beginLink(t, p, ana, SlackAccount), "code"); !errors.Is(err, ErrAccountRefused) {
		t.Fatalf("a team with no client: %v", err)
	}
	if _, err := p.Clients.Create("t", clients.Client{ID: "slack", Name: "Slack", Surface: clients.Slack, Identity: clients.Identity{Team: "T0TEAM"}, Slack: &clients.SlackOptions{}}, clients.DefaultCaps(), time.Now()); err != nil {
		t.Fatal(err)
	}
	// Another tenant's client on the team is not this workspace's.
	if _, err := p.Clients.Create("t2", clients.Client{ID: "slack", Name: "Slack", Surface: clients.Slack, Identity: clients.Identity{Team: "T0TEAM"}, Slack: &clients.SlackOptions{}}, clients.DefaultCaps(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := p.CompleteConnection(t.Context(), ana, SlackAccount, beginLink(t, p, ana, SlackAccount), "code"); err != nil {
		t.Fatal(err)
	}
	if fake.bearer != "Bearer slack-user-token" {
		t.Fatalf("userInfo carried %q", fake.bearer)
	}
	got, err := p.Accounts.Of("t", accounts.Slack, "ana")
	if err != nil || got.ID != "U0ANA" || got.Team != "T0TEAM" || got.Login != "Ana" {
		t.Fatalf("linked %+v %v", got, err)
	}
	if st, _ := p.ConnectionAuth.Status("t", "ana", SlackAccount); st.Connected {
		t.Fatal("a Slack token was stored")
	}
	bob := person("t", "bob")
	if err := p.CompleteConnection(t.Context(), bob, SlackAccount, beginLink(t, p, bob, SlackAccount), "code"); !errors.Is(err, accounts.ErrClaimed) {
		t.Fatalf("second Slack claim: %v", err)
	}
	fake.slackUser = strings.Replace(slackAna, "T0TEAM", "T0ELSE", 1)
	carol := person("t", "carol")
	if err := p.CompleteConnection(t.Context(), carol, SlackAccount, beginLink(t, p, carol, SlackAccount), "code"); !errors.Is(err, ErrAccountRefused) {
		t.Fatalf("a team none of the clients serves: %v", err)
	}
	listed, err := p.LinkedAccounts(person("t", "admin"), "slack")
	if err != nil || len(listed) != 1 || listed[0].Subject != "ana" {
		t.Fatalf("listed %+v %v", listed, err)
	}
	if listed, err := p.LinkedAccounts(person("t2", "admin"), "slack"); err != nil || len(listed) != 0 {
		t.Fatalf("t2 listed %+v %v", listed, err)
	}
	if _, err := p.LinkedAccounts(person("t3", "admin"), "slack"); !errors.Is(err, clients.ErrNotFound) {
		t.Fatalf("another tenant's client: %v", err)
	}
}

func TestAccountLinkRefusesMachinesAndSharedScope(t *testing.T) {
	p, _ := linkFixture(t)
	machine := person("t", "svc")
	machine.Kind = auth.Machine
	if _, err := p.AuthorizeConnection(t.Context(), machine, GitHubAccount, ConnectionAuthorizationInput{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("machine: %v", err)
	}
	if _, err := p.AuthorizeConnection(t.Context(), person("t", "ana"), GitHubAccount, ConnectionAuthorizationInput{Scope: "workspace"}); err == nil {
		t.Fatal("workspace scope accepted")
	}
	if _, err := p.AuthorizeConnection(t.Context(), person("t", "ana"), "workspace-"+GitHubAccount, ConnectionAuthorizationInput{}); err == nil {
		t.Fatal("workspace provider accepted")
	}
}

func TestLinkedAccountsResolveBeforeThePeopleMap(t *testing.T) {
	p, _ := linkFixture(t)
	gh := clients.Client{ID: "github", Tenant: "t", Surface: clients.GitHub, Enabled: true, GitHub: &clients.GitHubOptions{People: map[string]string{"octocat": "from-env", "hubot": "hubot-env"}}}
	tag := func(login string) event.Envelope { return wire.NewEvent(wire.Raise{Actor: event.Actor{ID: login}}) }
	delivery := func(id, login string) context.Context {
		return withGitHubSender(t.Context(), []byte(`{"sender":{"id":`+id+`,"login":"`+login+`"}}`))
	}
	if got := p.surfacePerson(delivery("42", "octocat"), gh, tag("octocat")); got != "from-env" {
		t.Fatalf("unlinked: %q", got)
	}
	if _, err := p.Accounts.Link("t", accounts.Account{Surface: accounts.GitHub, ID: "42", Login: "old-name", Subject: "ana"}); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		ctx   context.Context
		login string
		want  string
	}{
		{delivery("42", "octocat"), "octocat", "ana"},
		// A login renamed since linking still resolves by id.
		{delivery("42", "OctoCat"), "OctoCat", "ana"},
		// Without the delivery's sender, only the map.
		{t.Context(), "octocat", "from-env"},
		// A sender who is not the actor is not believed.
		{delivery("42", "hubot"), "octocat", "from-env"},
		{delivery("7", "hubot"), "hubot", "hubot-env"},
	} {
		if got := p.surfacePerson(test.ctx, gh, tag(test.login)); got != test.want {
			t.Fatalf("%s: %q want %q", test.login, got, test.want)
		}
	}
	// Another tenant's link is not this client's.
	other := gh
	other.Tenant = "t2"
	if got := p.surfacePerson(delivery("42", "octocat"), other, tag("octocat")); got != "from-env" {
		t.Fatalf("t2: %q", got)
	}

	sl := clients.Client{ID: "slack", Name: "Slack", Surface: clients.Slack, Enabled: true, Identity: clients.Identity{Team: "T0TEAM"}, Slack: &clients.SlackOptions{Machine: "m", People: map[string]string{"U0ANA": "from-file", "U0BOB": "bob-file"}}}
	created, err := p.Clients.Create("t", sl, clients.DefaultCaps(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got := p.surfacePerson(t.Context(), created, tag("U0ANA")); got != "from-file" {
		t.Fatalf("unlinked Slack: %q", got)
	}
	if _, err := p.Accounts.Link("t", accounts.Account{Surface: accounts.Slack, ID: "U0ANA", Team: "T0TEAM", Login: "Ana", Subject: "ana"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Accounts.Link("t", accounts.Account{Surface: accounts.Slack, ID: "U0BOB", Team: "T0ELSE", Login: "Bob", Subject: "bob"}); err != nil {
		t.Fatal(err)
	}
	if got := p.surfacePerson(t.Context(), created, tag("U0ANA")); got != "ana" {
		t.Fatalf("linked Slack: %q", got)
	}
	// A link from another team does not answer for this one.
	if got := p.surfacePerson(t.Context(), created, tag("U0BOB")); got != "bob-file" {
		t.Fatalf("other team's link: %q", got)
	}
	if people := p.slackPeople("t", created); people["U0ANA"] != "ana" || people["U0BOB"] != "bob-file" {
		t.Fatalf("slack people %v", people)
	}
}
