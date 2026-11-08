package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/urmzd/humagen/pkg/connectionauth"
	"github.com/urmzd/humagen/pkg/settings"
	"github.com/urmzd/humagen/pkg/state"
)

type connectionContractTransport func(*http.Request) (*http.Response, error)

func (f connectionContractTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// All provider calls terminate at this offline boundary. This checks the public
// HTTP contract, not a live provider installation or consent grant.
func exerciseConnectionContract(t *testing.T, c *contract) {
	t.Helper()
	failToken := false
	client := &http.Client{Transport: connectionContractTransport(func(r *http.Request) (*http.Response, error) {
		var body string
		switch r.URL.Path {
		case "/.well-known/oauth-protected-resource/mcp":
			body = `{"resource":"https://mcp.notion.com/mcp","authorization_servers":["https://oauth.example.com"]}`
		case "/.well-known/oauth-authorization-server":
			body = `{"issuer":"https://oauth.example.com","authorization_endpoint":"https://oauth.example.com/authorize","token_endpoint":"https://oauth.example.com/token","registration_endpoint":"https://oauth.example.com/register","code_challenge_methods_supported":["S256"]}`
		case "/register":
			body = `{"client_id":"contract-client","token_endpoint_auth_method":"none"}`
		case "/token":
			if failToken {
				return &http.Response{StatusCode: 400, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":"invalid_grant"}`))}, nil
			}
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.Form.Get("code_verifier") == "" || r.Form.Get("resource") != "https://mcp.notion.com/mcp" {
				t.Fatal("token exchange omitted PKCE or audience")
			}
			body = `{"access_token":"contract-access","refresh_token":"contract-refresh","token_type":"Bearer","expires_in":3600}`
		default:
			t.Fatalf("unexpected provider request %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	manager, err := connectionauth.New(state.NewMemory(), connectionauth.Config{Key: []byte(strings.Repeat("c", 32)), RedirectURL: "https://humagen.example.com/connections/callback", Providers: []connectionauth.Provider{{Name: "notion", Resource: "https://mcp.notion.com/mcp"}}, HTTP: client})
	if err != nil {
		t.Fatal(err)
	}
	previous := c.f.p.ConnectionAuth
	c.f.p.ConnectionAuth = manager
	defer func() { c.f.p.ConnectionAuth = previous }()
	tenant := "connection-contract"
	if err = c.f.p.SetRunSettings(tenant, settings.Layer{Ceilings: settings.Ceilings{ToolServers: []string{"mcp.notion.com"}}}, nil); err != nil {
		t.Fatal(err)
	}
	token := c.f.person(t, tenant, "alice", "Alice", "humagen-admins")
	c.call(token, "GET", "/api/connections/presets", "/api/connections/presets", "", 200)
	auth := c.call(token, "POST", "/api/connections/{provider}/authorize", "/api/connections/notion/authorize", "", 200)
	target, err := url.Parse(auth.str("url"))
	if err != nil {
		t.Fatal(err)
	}
	if target.Query().Get("code_challenge_method") != "S256" {
		t.Fatal("authorization omitted PKCE")
	}
	callback := func(nonce string) string {
		b, _ := json.Marshal(ConnectionCallback{State: nonce, Code: "contract-code"})
		return string(b)
	}
	c.call(token, "POST", "/api/connections/{provider}/callback", "/api/connections/notion/callback", callback("wrong-state"), 400)
	c.call(token, "POST", "/api/connections/{provider}/callback", "/api/connections/notion/callback", callback(target.Query().Get("state")), 204)
	c.call(token, "GET", "/api/connections/{provider}", "/api/connections/notion", "", 200)
	c.call(token, "POST", "/api/connections/{provider}/callback", "/api/connections/notion/callback", callback(target.Query().Get("state")), 400)
	c.call(token, "DELETE", "/api/connections/{provider}", "/api/connections/notion", "", 204)
	c.call(token, "GET", "/api/connections/{provider}", "/api/connections/notion", "", 200)
	c.call(token, "GET", "/api/connections/{provider}", "/api/connections/unknown", "", 404)
	c.call(token, "POST", "/api/connections/{provider}/authorize", "/api/connections/unknown/authorize", "", 404)
	c.call(token, "DELETE", "/api/connections/{provider}", "/api/connections/unknown", "", 404)

	c.call(token, "POST", "/api/connections/{provider}/authorize", "/api/connections/other-unknown/authorize", "", 409)
	refused := c.call(token, "POST", "/api/connections/{provider}/authorize", "/api/connections/notion/authorize", "", 200)
	refusedURL, _ := url.Parse(refused.str("url"))
	failToken = true
	c.call(token, "POST", "/api/connections/{provider}/callback", "/api/connections/notion/callback", callback(refusedURL.Query().Get("state")), 409)
	failToken = false
	c.f.p.ConnectionAuth = nil
	c.call(token, "POST", "/api/connections/{provider}/authorize", "/api/connections/notion/authorize", "", 503)
	c.call(token, "POST", "/api/connections/{provider}/callback", "/api/connections/notion/callback", callback("unavailable"), 503)
	c.call(token, "DELETE", "/api/connections/{provider}", "/api/connections/notion", "", 503)
	c.call("", "GET", "/api/connections/presets", "/api/connections/presets", "", 401)
}

func TestConnectionHTTPContract(t *testing.T) {
	f := newFixture(t)
	c := &contract{t: t, f: f, spec: loadSpec(t), seen: map[string]bool{}}
	exerciseConnectionContract(t, c)
}
