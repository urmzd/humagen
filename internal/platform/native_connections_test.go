package platform

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/connectionauth"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/personctx"
	"github.com/urmzd/humagen/pkg/state"
)

type nativeHTTP func(*http.Request) (*http.Response, error)

func (f nativeHTTP) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func nativeResponse(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func nativeConfig(name string) *connectionauth.Config {
	p := NativeConnectionProvider(name)
	p.ClientID = "registered"
	p.ClientSecret = "client-secret"
	p.SigningSecret = "signing-secret"
	return &connectionauth.Config{Key: []byte(strings.Repeat("n", 32)), RedirectURL: "https://humagen.test/connections/callback", Providers: []connectionauth.Provider{p}, HTTP: &http.Client{Transport: nativeHTTP(func(r *http.Request) (*http.Response, error) {
		if name == "slack" {
			return nativeResponse(`{"ok":true,"access_token":"bot-token","refresh_token":"bot-refresh","expires_in":3600,"token_type":"bot","team":{"id":"T12345"},"app_id":"A12345"}`), nil
		}
		return nativeResponse(`{"access_token":"drive-token","refresh_token":"drive-refresh","expires_in":3600,"token_type":"Bearer"}`), nil
	})}}
}
func authorizeNative(t *testing.T, p *Platform, who auth.Principal, name, scope string) {
	t.Helper()
	a, err := p.AuthorizeConnection(t.Context(), who, name, ConnectionAuthorizationInput{Scope: scope})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(a.URL)
	if err = p.CompleteConnection(t.Context(), who, a.Provider, u.Query().Get("state"), "code"); err != nil {
		t.Fatal(err)
	}
}

func TestNativeSlackInstallationStartsDisabledAndPinsIdentity(t *testing.T) {
	p, err := New(Config{Workspace: t.TempDir(), ConnectionAuth: nativeConfig("slack")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	who := auth.Principal{Tenant: "tenant", Subject: "admin", Kind: auth.User, Scopes: []auth.Permission{auth.ClientsWrite}}
	if _, err = p.AuthorizeConnection(t.Context(), who, "slack", ConnectionAuthorizationInput{Scope: "person"}); !errors.Is(err, ErrForbidden) {
		t.Fatal("native install accepted personal scope")
	}
	authorizeNative(t, p, who, "slack", "workspace")
	c, err := p.Clients.Get("tenant", "slack-t12345")
	if err != nil {
		t.Fatal(err)
	}
	if c.Enabled || c.DefaultAgent != "" || c.Managed != clients.ManagedInstall || c.Capabilities == nil || len(c.Capabilities.Events) != 0 || len(c.Capabilities.Actions) != 0 {
		t.Fatal("install gained routing or grants")
	}
	caps, err := p.ClientCapabilities("tenant", c.ID)
	if err != nil || caps.Managed || len(caps.Actions) == 0 {
		t.Fatal("disabled installation cannot choose capabilities", err)
	}
	changed := c
	changed.Identity.Team = "T99999"
	changed.Enabled = true
	saved, err := p.UpdateClient(t.Context(), who.Tenant, c.ID, who.Subject, ClientInput{Client: changed})
	if err != nil || saved.Identity.Team != "T12345" {
		t.Fatal("installation identity was changed", err)
	}
	if _, err = p.UpdateClient(t.Context(), who.Tenant, c.ID, who.Subject, ClientInput{Client: saved, Token: clients.SecretInput{Value: "replacement"}}); !errors.Is(err, clients.ErrManaged) {
		t.Fatal("native token replaced through generic input")
	}
	if err = p.DisconnectConnection(who, "slack"); err != nil {
		t.Fatal(err)
	}
	saved, err = p.Clients.Get(who.Tenant, c.ID)
	if err != nil || saved.Enabled {
		t.Fatal("disconnect left client enabled")
	}
	if _, err = p.ConnectionAuth.BoundToken(t.Context(), who.Tenant, connectionauth.WorkspaceOwner, "client:"+c.ID, slackProvider(c)); !errors.Is(err, connectionauth.ErrReconnect) {
		t.Fatal("disconnected installation token usable")
	}
}

func TestNativeDriveSourceRequiresExactConsentAndRevocationDropsResults(t *testing.T) {
	provider := &selectedQueryProvider{docs: []knowledge.ImportDocument{{ID: "selected", Title: "Selected", Text: "release source"}}}
	f := newSourceQueryFixture(t, knowledge.Hybrid, provider)
	f.p.PersonContexts = personctx.NewStore()
	f.p.Audit, _ = audit.Open(nil)
	f.p.cfg.Ext.SourceProviders["drive"] = provider
	cfg := f.src.Lifecycle.SourceConfig
	cfg.Provider = "drive"
	cfg.Resources = []string{"selected"}
	if _, err := f.p.Knowledge.Create("tenant", "drive-docs", "", "owner", f.p.now()); err != nil {
		t.Fatal(err)
	}
	src, err := f.p.Knowledge.Configure("tenant", "drive-docs", cfg, f.p.now())
	if err != nil {
		t.Fatal(err)
	}
	f.src = src
	f.p.ConnectionAuth, err = connectionauth.New(state.NewMemory(), *nativeConfig("drive"))
	if err != nil {
		t.Fatal(err)
	}
	authorizeNative(t, f.p, f.who, "drive", "person")
	if _, err = f.p.sourceSecret(context.Background(), f.who, src); knowledge.FailureCause(err) != knowledge.AuthExpired {
		t.Fatal("source used unbound account")
	}
	if err = f.p.ConnectSourceOAuth(f.who, src.Name, "drive"); err != nil {
		t.Fatal(err)
	}
	if _, err = f.p.sourceSecret(context.Background(), f.who, src); err != nil {
		t.Fatal(err)
	}
	other := f.who
	other.Subject = "other"
	if _, err = f.p.sourceSecret(context.Background(), other, src); knowledge.FailureCause(err) != knowledge.AuthExpired {
		t.Fatal("source borrowed another person's account")
	}
	provider.afterFetch = func() {
		if err := f.p.ConnectionAuth.Disconnect("tenant", "owner", "drive"); err != nil {
			t.Fatal(err)
		}
	}
	if rows, err := f.p.searchSourceLive(t.Context(), f.who, src, "release"); len(rows) != 0 || knowledge.FailureCause(err) != knowledge.AuthExpired {
		t.Fatal("revoked source content released", err)
	}
}
