package platform

import (
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

// browser is the secret a person's browser holds for their link.
func browser(who auth.Principal) string { return "browser-" + who.Tenant + "-" + who.Subject }

// beginLink starts a link and returns the state the consent carries.
func beginLink(t *testing.T, p *Platform, who auth.Principal, name string) string {
	t.Helper()
	a, err := p.AuthorizeAccount(t.Context(), who, name, ConnectionAuthorizationInput{Scope: "person"}, browser(who))
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
	if err := p.CompleteAccount(t.Context(), ana, GitHubAccount, state, "code", browser(ana)); err != nil {
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
	if err := p.CompleteAccount(t.Context(), ana, GitHubAccount, state, "code", browser(ana)); !errors.Is(err, connectionauth.ErrInvalid) {
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
	if err := p.CompleteAccount(t.Context(), ana, GitHubAccount, state+"x", "code", browser(ana)); !errors.Is(err, connectionauth.ErrInvalid) {
		t.Fatalf("wrong state: %v", err)
	}
	if _, err := p.Accounts.Of("t", accounts.GitHub, "ana"); !errors.Is(err, accounts.ErrNotFound) {
		t.Fatal("a wrong state linked")
	}
	// The right one still works: a wrong guess does not spend it.
	if err := p.CompleteAccount(t.Context(), ana, GitHubAccount, state, "code", browser(ana)); err != nil {
		t.Fatal(err)
	}
}

func TestAccountLinkRefusesAnotherPersonsState(t *testing.T) {
	p, _ := linkFixture(t)
	ana, bob := person("t", "ana"), person("t", "bob")
	state := beginLink(t, p, ana, GitHubAccount)
	if err := p.CompleteAccount(t.Context(), bob, GitHubAccount, state, "code", browser(bob)); !errors.Is(err, connectionauth.ErrInvalid) {
		t.Fatalf("bob completed ana's consent: %v", err)
	}
	if _, err := p.Accounts.Of("t", accounts.GitHub, "bob"); !errors.Is(err, accounts.ErrNotFound) {
		t.Fatal("bob was linked through ana's state")
	}
	// Nor in another workspace.
	if err := p.CompleteAccount(t.Context(), person("t2", "ana"), GitHubAccount, state, "code", browser(person("t2", "ana"))); !errors.Is(err, connectionauth.ErrInvalid) {
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
		if err := p.CompleteAccount(t.Context(), ana, name, state, "code", browser(ana)); !errors.Is(err, connectionauth.ErrReconnect) {
			t.Fatalf("%s identity failure: %v", name, err)
		}
		set(`{"ok":false}`)
		state = beginLink(t, p, ana, name)
		if err := p.CompleteAccount(t.Context(), ana, name, state, "code", browser(ana)); !errors.Is(err, ErrAccountRefused) {
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
	if err := p.CompleteAccount(t.Context(), ana, GitHubAccount, beginLink(t, p, ana, GitHubAccount), "code", browser(ana)); err != nil {
		t.Fatal(err)
	}
	if err := p.CompleteAccount(t.Context(), bob, GitHubAccount, beginLink(t, p, bob, GitHubAccount), "code", browser(bob)); !errors.Is(err, accounts.ErrClaimed) {
		t.Fatalf("second claim: %v", err)
	}
	// In another workspace the same GitHub user is free.
	carol := person("t2", "carol")
	if err := p.CompleteAccount(t.Context(), carol, GitHubAccount, beginLink(t, p, carol, GitHubAccount), "code", browser(carol)); err != nil {
		t.Fatalf("another tenant refused: %v", err)
	}
	if err := p.DisconnectConnection(ana, GitHubAccount); err != nil {
		t.Fatal(err)
	}
	if err := p.CompleteAccount(t.Context(), bob, GitHubAccount, beginLink(t, p, bob, GitHubAccount), "code", browser(bob)); err != nil {
		t.Fatalf("claim after unlink: %v", err)
	}
	if got, _ := p.Accounts.Get("t2", accounts.GitHub, "", "42"); got.Subject != "carol" {
		t.Fatalf("t2's link moved: %+v", got)
	}
}

func TestSlackAccountNeedsTheWorkspacesTeam(t *testing.T) {
	p, fake := linkFixture(t)
	ana := person("t", "ana")
	if err := p.CompleteAccount(t.Context(), ana, SlackAccount, beginLink(t, p, ana, SlackAccount), "code", browser(ana)); !errors.Is(err, ErrAccountRefused) {
		t.Fatalf("a team with no client: %v", err)
	}
	if _, err := p.Clients.Create("t", clients.Client{ID: "slack", Name: "Slack", Surface: clients.Slack, Enabled: true, Identity: clients.Identity{Team: "T0TEAM"}, Slack: &clients.SlackOptions{}}, clients.DefaultCaps(), time.Now()); err != nil {
		t.Fatal(err)
	}
	// Another tenant's client on the team is not this workspace's.
	if _, err := p.Clients.Create("t2", clients.Client{ID: "slack", Name: "Slack", Surface: clients.Slack, Enabled: true, Identity: clients.Identity{Team: "T0TEAM"}, Slack: &clients.SlackOptions{}}, clients.DefaultCaps(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := p.CompleteAccount(t.Context(), ana, SlackAccount, beginLink(t, p, ana, SlackAccount), "code", browser(ana)); err != nil {
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
	if err := p.CompleteAccount(t.Context(), bob, SlackAccount, beginLink(t, p, bob, SlackAccount), "code", browser(bob)); !errors.Is(err, accounts.ErrClaimed) {
		t.Fatalf("second Slack claim: %v", err)
	}
	fake.slackUser = strings.Replace(slackAna, "T0TEAM", "T0ELSE", 1)
	carol := person("t", "carol")
	if err := p.CompleteAccount(t.Context(), carol, SlackAccount, beginLink(t, p, carol, SlackAccount), "code", browser(carol)); !errors.Is(err, ErrAccountRefused) {
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
	if _, err := p.AuthorizeAccount(t.Context(), machine, GitHubAccount, ConnectionAuthorizationInput{}, browser(machine)); !errors.Is(err, ErrForbidden) {
		t.Fatalf("machine: %v", err)
	}
	if _, err := p.AuthorizeAccount(t.Context(), person("t", "ana"), GitHubAccount, ConnectionAuthorizationInput{Scope: "workspace"}, "b"); err == nil {
		t.Fatal("workspace scope accepted")
	}
	if _, err := p.AuthorizeConnection(t.Context(), person("t", "ana"), "workspace-"+GitHubAccount, ConnectionAuthorizationInput{}); err == nil {
		t.Fatal("workspace provider accepted")
	}
}

// Linking records who a person is on a surface and nothing else: whom a
// client's connector acts for is still its people map alone, with or without
// a link.
func TestALinkedAccountLeavesWhoASurfaceUserIsUnchanged(t *testing.T) {
	p, _ := linkFixture(t)
	gh := clients.Client{ID: "github", Tenant: "t", Surface: clients.GitHub, Enabled: true, GitHub: &clients.GitHubOptions{People: map[string]string{"hubot": "from-map"}}}
	sl, err := p.Clients.Create("t", clients.Client{ID: "slack", Name: "Slack", Surface: clients.Slack, Enabled: true, Identity: clients.Identity{Team: "T0TEAM"}, Slack: &clients.SlackOptions{Machine: "m", People: map[string]string{"U0BOB": "from-map"}}}, clients.DefaultCaps(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	tag := func(id string) event.Envelope { return wire.NewEvent(wire.Raise{Actor: event.Actor{ID: id}}) }
	type answer struct {
		octocat, hubot, ana, bob string
		sender, acts             bool
	}
	read := func() answer {
		_, acts := sl.ActsFor("m", "U0ANA")
		return answer{
			octocat: p.surfacePerson(gh, tag("octocat")), hubot: p.surfacePerson(gh, tag("hubot")),
			ana: p.surfacePerson(sl, tag("U0ANA")), bob: p.surfacePerson(sl, tag("U0BOB")),
			sender: p.slackSender(sl, "T0TEAM", "U0ANA"), acts: acts,
		}
	}
	before := read()
	if before != (answer{hubot: "from-map", bob: "from-map"}) {
		t.Fatalf("before linking: %+v", before)
	}
	ana := person("t", "ana")
	if err := p.CompleteAccount(t.Context(), ana, GitHubAccount, beginLink(t, p, ana, GitHubAccount), "code", browser(ana)); err != nil {
		t.Fatal(err)
	}
	if err := p.CompleteAccount(t.Context(), ana, SlackAccount, beginLink(t, p, ana, SlackAccount), "code", browser(ana)); err != nil {
		t.Fatal(err)
	}
	if after := read(); after != before {
		t.Fatalf("linking changed who a surface user is: %+v, was %+v", after, before)
	}
}

// An account link begins and ends only in the person's own browser: an agent's
// tool, an MCP client or a token reaches it through the generic connection
// calls, which refuse it, and a callback from another browser is refused.
func TestAccountLinkIsBoundToTheBrowserThatBeganIt(t *testing.T) {
	p, _ := linkFixture(t)
	ana := person("t", "ana")
	for _, name := range []string{GitHubAccount, SlackAccount} {
		if _, err := p.AuthorizeConnection(t.Context(), ana, name, ConnectionAuthorizationInput{Scope: "person"}); !errors.Is(err, ErrAccountInConsole) {
			t.Fatalf("%s through the generic connection call: %v", name, err)
		}
		// The builder's connect_connection tool calls BeginConnection.
		if _, err := p.BeginConnection(t.Context(), ana, name); !errors.Is(err, ErrAccountInConsole) {
			t.Fatalf("%s through an agent's tool: %v", name, err)
		}
		if err := p.CompleteConnection(t.Context(), ana, name, "state", "code"); !errors.Is(err, ErrAccountInConsole) {
			t.Fatalf("%s completed through the generic call: %v", name, err)
		}
		if _, err := p.AuthorizeAccount(t.Context(), ana, name, ConnectionAuthorizationInput{}, ""); !errors.Is(err, ErrAccountInConsole) {
			t.Fatalf("%s begun with no browser secret: %v", name, err)
		}
	}
	state := beginLink(t, p, ana, GitHubAccount)
	// Ana's own state and code, carried to another browser.
	if err := p.CompleteAccount(t.Context(), ana, GitHubAccount, state, "code", "another-browser"); !errors.Is(err, connectionauth.ErrInvalid) {
		t.Fatalf("another browser: %v", err)
	}
	if err := p.CompleteAccount(t.Context(), ana, GitHubAccount, state, "code", ""); !errors.Is(err, ErrAccountInConsole) {
		t.Fatalf("no browser: %v", err)
	}
	if _, err := p.Accounts.Of("t", accounts.GitHub, "ana"); !errors.Is(err, accounts.ErrNotFound) {
		t.Fatal("linked from another browser")
	}
	if err := p.CompleteAccount(t.Context(), ana, GitHubAccount, state, "code", browser(ana)); err != nil {
		t.Fatalf("her own browser: %v", err)
	}
}

func TestAnAdministratorUnlinksAnAccount(t *testing.T) {
	p, _ := linkFixture(t)
	admin := person("t", "admin")
	admin.Scopes = append(admin.Scopes, auth.ClientsWrite)
	for _, tenant := range []string{"t", "t2"} {
		if _, err := p.Clients.Create(tenant, clients.Client{ID: "gh", Name: "GitHub", Surface: clients.GitHub, Enabled: true, Identity: clients.Identity{Owners: []string{"acme"}}, GitHub: &clients.GitHubOptions{}}, clients.DefaultCaps(), time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.Accounts.Link("t", accounts.Account{Surface: accounts.GitHub, ID: "42", Login: "octocat", Subject: "ana"}); err != nil {
		t.Fatal(err)
	}
	if err := p.UnlinkAccount(person("t", "viewer"), "gh", "42"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("without clients:write: %v", err)
	}
	other := admin
	other.Tenant = "t3"
	if err := p.UnlinkAccount(other, "gh", "42"); !errors.Is(err, clients.ErrNotFound) {
		t.Fatalf("another tenant's client: %v", err)
	}
	t2 := admin
	t2.Tenant = "t2"
	if err := p.UnlinkAccount(t2, "gh", "42"); !errors.Is(err, accounts.ErrNotFound) {
		t.Fatalf("an account another tenant linked: %v", err)
	}
	if err := p.UnlinkAccount(admin, "gh", "42"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Accounts.Of("t", accounts.GitHub, "ana"); !errors.Is(err, accounts.ErrNotFound) {
		t.Fatal("still linked")
	}
	// The claim is free for someone else.
	bob := person("t", "bob")
	if err := p.CompleteAccount(t.Context(), bob, GitHubAccount, beginLink(t, p, bob, GitHubAccount), "code", browser(bob)); err != nil {
		t.Fatalf("claim after an administrator unlinked: %v", err)
	}
}

func TestADisabledSlackClientServesNoTeam(t *testing.T) {
	p, _ := linkFixture(t)
	if _, err := p.Clients.Create("t", clients.Client{ID: "slack", Name: "Slack", Surface: clients.Slack, Identity: clients.Identity{Team: "T0TEAM"}, Slack: &clients.SlackOptions{}}, clients.DefaultCaps(), time.Now()); err != nil {
		t.Fatal(err)
	}
	ana := person("t", "ana")
	if err := p.CompleteAccount(t.Context(), ana, SlackAccount, beginLink(t, p, ana, SlackAccount), "code", browser(ana)); !errors.Is(err, ErrAccountRefused) {
		t.Fatalf("a disabled client's team: %v", err)
	}
}
