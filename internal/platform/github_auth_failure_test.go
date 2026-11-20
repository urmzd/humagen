package platform

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	github "github.com/urmzd/humagen/pkg/integrations/github"
	"github.com/urmzd/humagen/pkg/threads"
)

// Uses the real verified-install fixture, then drives native token minting
// through a fake transport. No external provider is contacted.
func assertGitHubAuthFailureOwnership(t *testing.T, p, replica *Platform, creator auth.Principal, c clients.Client, key string) {
	t.Helper()
	if _, err := p.Agents.Create(t.Context(), creator.Tenant, wire.AgentSpec{Name: BuiltinEntry, Model: "offline", Provider: "offline", SystemPrompt: "Reconnect"}, "test"); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	source, err := github.NewAppTokenSource("https://api.github.com", p.observeGitHubAppAuth(c, github.AppConfig{
		AppID: c.Identity.AppID, InstallationID: c.Identity.Installation, PrivateKey: key,
		HTTPClient: &http.Client{Transport: nativeHTTP(func(*http.Request) (*http.Response, error) {
			close(entered)
			<-release
			r := nativeResponse(`{"message":"private provider detail"}`)
			r.StatusCode = 401
			return r, nil
		})},
	}))
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { _, err := source.Token(t.Context()); finished <- err }()
	<-entered
	currentOwner := creator
	currentOwner.Subject = "bob"
	a, err := replica.BeginGitHubInstall(t.Context(), currentOwner)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(a.URL)
	if err = replica.CompleteGitHubInstall(t.Context(), currentOwner, u.Query().Get("state"), "one-use-code", c.Identity.Installation); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err = <-finished; err == nil {
		t.Fatal("mint rejection lost")
	}
	all, err := p.Alerts.Alerts(creator.Tenant)
	if err != nil || len(all) != 0 {
		t.Fatal("old mint failure alerted reconnected owner", all, err)
	}
	saved, err := replica.Clients.Get(c.Tenant, c.ID)
	if err != nil || saved.CreatedBy != creator.Subject {
		t.Fatal("reconnect changed immutable client creator", saved, err)
	}
	for _, status := range []int{403, 500, 401} {
		source, err = github.NewAppTokenSource("https://api.github.com", p.observeGitHubAppAuth(c, github.AppConfig{
			AppID: c.Identity.AppID, InstallationID: c.Identity.Installation, PrivateKey: key,
			HTTPClient: &http.Client{Transport: nativeHTTP(func(*http.Request) (*http.Response, error) {
				r := nativeResponse(`{}`)
				r.StatusCode = status
				return r, nil
			})},
		}))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = source.Token(t.Context()); err == nil {
			t.Fatal("provider failure lost")
		}
		all, err = p.Alerts.Alerts(creator.Tenant)
		if err != nil || (len(all) == 1) != (status == 401) {
			t.Fatal("wrong failure classification", status, all, err)
		}
	}
	attention := all[0]
	th, err := p.Threads.Get(c.Tenant, attention.Session)
	if err != nil || th.CreatedBy != "bob" || th.Visibility != threads.Private || len(attention.Who) != 1 || attention.Who[0] != "bob" {
		t.Fatal("GitHub attention not current-owner private", th, attention, err)
	}
	formerReader, ownerReader := creator, currentOwner
	formerReader.Roles, ownerReader.Roles = []auth.Role{auth.Operator}, []auth.Role{auth.Operator}
	if p.Seer(formerReader).Alert(attention) || !p.Seer(ownerReader).Alert(attention) {
		t.Fatal("GitHub attention leaked to former consenting actor")
	}
	foreign := c
	foreign.Tenant = "other"
	if receipt, ok := p.githubAuthReceipt(t.Context(), foreign); ok {
		t.Fatal("cross-tenant receipt", receipt)
	}
}
