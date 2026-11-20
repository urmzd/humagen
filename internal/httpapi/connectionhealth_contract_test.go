package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/connectionauth"
	"github.com/urmzd/humagen/pkg/state"
)

func exerciseConnectionHealthContract(t *testing.T, c *contract) {
	t.Helper()
	provider := platform.NativeConnectionProvider("drive")
	provider.ClientID = "registered"
	provider.ClientSecret = "secret"
	manager, err := connectionauth.New(state.NewMemory(), connectionauth.Config{Key: []byte(strings.Repeat("h", 32)), RedirectURL: "https://humagen.test/connections/callback", Providers: []connectionauth.Provider{provider}, HTTP: &http.Client{Transport: connectionContractTransport(func(r *http.Request) (*http.Response, error) {
		body := `{"access_token":"offline-health-token","token_type":"Bearer","expires_in":3600}`
		if r.URL.Host == "www.googleapis.com" {
			if r.Header.Get("Authorization") != "Bearer offline-health-token" {
				t.Fatal("health omitted credentials")
			}
			body = `{"kind":"drive#about"}`
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}})
	if err != nil {
		t.Fatal(err)
	}
	previous := c.f.p.ConnectionAuth
	c.f.p.ConnectionAuth = manager
	defer func() { c.f.p.ConnectionAuth = previous }()
	tenant := "connection-health"
	token := c.f.person(t, tenant, "alice", "Alice", "humagen-admins")
	a := c.call(token, "POST", "/api/connections/{provider}/authorize", "/api/connections/drive/authorize", `{"scope":"person"}`, 200)
	u, _ := url.Parse(a.str("url"))
	callback, _ := json.Marshal(ConnectionCallback{State: u.Query().Get("state"), Code: "offline-code"})
	c.call(token, "POST", "/api/connections/{provider}/callback", "/api/connections/drive/callback", string(callback), 204)
	c.call(token, "POST", "/api/connections/{provider}/check", "/api/connections/drive/check", "", 200)
	c.call(token, "POST", "/api/connections/{provider}/check", "/api/connections/unknown/check", "", 404)
	c.call(token, "POST", "/api/connections/{provider}/check", "/api/connections/other-missing/check", "", 409)
	c.f.p.ConnectionAuth = nil
	c.call(token, "POST", "/api/connections/{provider}/check", "/api/connections/drive/check", "", 503)
	c.f.p.ConnectionAuth = manager
	_, err = c.f.p.Clients.Create(tenant, clients.Client{ID: "unverified", Name: "Unverified Slack", Surface: clients.Slack, Identity: clients.Identity{Team: "T12345"}, Credentials: clients.Credentials{Token: clients.SecretRef{Set: true}, SigningSecret: clients.SecretRef{Set: true}}}, clients.DefaultCaps(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	c.call(token, "POST", "/api/clients/{id}/check", "/api/clients/unverified/check", "", 200)
	c.call(token, "POST", "/api/clients/{id}/check", "/api/clients/missing/check", "", 404)
}

func TestConnectionHealthHTTPContract(t *testing.T) {
	f := newFixture(t)
	c := &contract{t: t, f: f, spec: loadSpec(t), seen: map[string]bool{}}
	exerciseConnectionHealthContract(t, c)
}
