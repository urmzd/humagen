package connectionauth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/state"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}
func setup(t *testing.T) (*Manager, state.Store, *atomic.Int32) {
	t.Helper()
	store := state.NewMemory()
	calls := &atomic.Int32{}
	client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/.well-known/oauth-protected-resource/mcp":
			return response(`{"resource":"https://service.test/mcp","authorization_servers":["https://auth.test"]}`), nil
		case "/.well-known/oauth-authorization-server":
			return response(`{"issuer":"https://auth.test","authorization_endpoint":"https://auth.test/authorize","token_endpoint":"https://auth.test/token","registration_endpoint":"https://auth.test/register","code_challenge_methods_supported":["S256"]}`), nil
		case "/register":
			return response(`{"client_id":"registered","token_endpoint_auth_method":"none"}`), nil
		case "/token":
			calls.Add(1)
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.Form.Get("grant_type") == "authorization_code" {
				if r.Form.Get("code_verifier") == "" || r.Form.Get("resource") != "https://service.test/mcp" {
					t.Error("missing verifier/resource")
				}
				return response(`{"access_token":"secret-access","refresh_token":"secret-refresh","token_type":"Bearer","expires_in":1}`), nil
			}
			if r.Form.Get("refresh_token") != "secret-refresh" {
				t.Error("wrong refresh token")
			}
			return response(`{"access_token":"new-secret-access","refresh_token":"rotated-secret-refresh","token_type":"Bearer","expires_in":3600}`), nil
		}
		t.Fatalf("unexpected request %s", r.URL)
		return nil, nil
	})}
	m, err := New(store, Config{Key: []byte(strings.Repeat("k", 32)), RedirectURL: "https://humagen.test/connections/callback", Providers: []Provider{{Name: "notes", Resource: "https://service.test/mcp"}}, HTTP: client})
	if err != nil {
		t.Fatal(err)
	}
	return m, store, calls
}
func nonceOf(t *testing.T, a Authorization) string {
	t.Helper()
	u, err := url.Parse(a.URL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
		t.Fatal("missing PKCE")
	}
	return q.Get("state")
}
func TestAuthorizationIsBoundSingleUseAndEncrypted(t *testing.T) {
	m, st, calls := setup(t)
	ctx := context.Background()
	a, err := m.Begin(ctx, "tenant", "alice", "notes")
	if err != nil {
		t.Fatal(err)
	}
	nonce := nonceOf(t, a)
	for _, identity := range [][2]string{{"other", "alice"}, {"tenant", "bob"}} {
		if m.Complete(ctx, identity[0], identity[1], "notes", nonce, "code") == nil {
			t.Fatal("accepted another identity")
		}
	}
	if m.Complete(ctx, "tenant", "alice", "notes", "wrong-state", "code") == nil {
		t.Fatal("accepted wrong state")
	}
	if err = m.Complete(ctx, "tenant", "alice", "notes", nonce, "code"); err != nil {
		t.Fatal(err)
	}
	if m.Complete(ctx, "tenant", "alice", "notes", nonce, "code") == nil {
		t.Fatal("replayed callback")
	}
	if calls.Load() != 1 {
		t.Fatalf("exchanges=%d", calls.Load())
	}
	records, err := st.Load(ctx, "connection_oauth")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range records {
		for _, secret := range []string{"secret-access", "secret-refresh", "registered", "code"} {
			if strings.Contains(string(r.Value), secret) {
				t.Fatalf("plaintext %q in storage", secret)
			}
		}
	}
	if err = m.Disconnect("tenant", "alice", "notes"); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Token(ctx, "tenant", "alice", "notes"); !errors.Is(err, ErrReconnect) {
		t.Fatal(err)
	}
}
func TestTokenRefreshSurvivesRestartAndCiphertextCannotMoveTenants(t *testing.T) {
	m, st, calls := setup(t)
	ctx := context.Background()
	a, err := m.Begin(ctx, "tenant", "alice", "notes")
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Complete(ctx, "tenant", "alice", "notes", nonceOf(t, a), "code"); err != nil {
		t.Fatal(err)
	}
	again, err := New(st, m.cfg)
	if err != nil {
		t.Fatal(err)
	}
	value, err := again.Token(ctx, "tenant", "alice", "notes")
	if err != nil || value != "new-secret-access" {
		t.Fatalf("%s %v", value, err)
	}
	if _, err = again.Token(ctx, "tenant", "alice", "notes"); err != nil || calls.Load() != 2 {
		t.Fatalf("refreshes=%d err=%v", calls.Load(), err)
	}
	key := recordKey("account", "alice", "notes")
	item, err := m.records.Lookup("tenant", key)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.records.Put("other", key, item.Value); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Token(ctx, "other", "alice", "notes"); !errors.Is(err, ErrReconnect) {
		t.Fatal("moved ciphertext decrypted")
	}
}
func TestRestartedAuthorizationAndExpiredState(t *testing.T) {
	m, st, _ := setup(t)
	ctx := context.Background()
	a, err := m.Begin(ctx, "tenant", "alice", "notes")
	if err != nil {
		t.Fatal(err)
	}
	again, err := New(st, m.cfg)
	if err != nil {
		t.Fatal(err)
	}
	again.now = func() time.Time { return a.ExpiresAt.Add(time.Second) }
	if again.Complete(ctx, "tenant", "alice", "notes", nonceOf(t, a), "code") == nil {
		t.Fatal("accepted expired state")
	}
	again.now = time.Now
	b, err := again.Begin(ctx, "tenant", "alice", "notes")
	if err != nil {
		t.Fatal(err)
	}
	if m.Complete(ctx, "tenant", "alice", "notes", nonceOf(t, a), "code") == nil {
		t.Fatal("accepted replaced authorization")
	}
	if err = m.Complete(ctx, "tenant", "alice", "notes", nonceOf(t, b), "code"); err != nil {
		t.Fatal(err)
	}
}
func TestUnsafeAndMismatchedMetadataIsRefused(t *testing.T) {
	for _, raw := range []string{"http://example.com", "https://127.0.0.1", "https://169.254.169.254", "https://10.0.0.1", "https://localhost", "https://user:pass@service.test", "https://service.test/#fragment"} {
		if safeURL(raw) {
			t.Errorf("accepted %s", raw)
		}
	}
	for _, body := range []string{
		`{"issuer":"https://other.test","authorization_endpoint":"https://auth.test/authorize","token_endpoint":"https://auth.test/token","code_challenge_methods_supported":["S256"]}`,
		`{"issuer":"https://auth.test","authorization_endpoint":"https://auth.test/authorize","token_endpoint":"https://127.0.0.1/token","code_challenge_methods_supported":["S256"]}`,
		`{"issuer":"https://auth.test","authorization_endpoint":"https://auth.test/authorize","token_endpoint":"https://auth.test/token","code_challenge_methods_supported":["plain"]}`,
	} {
		m, _, _ := setup(t)
		next := m.client.Transport
		m.client.Transport = transport(func(r *http.Request) (*http.Response, error) {
			if strings.Contains(r.URL.Path, "oauth-authorization-server") {
				return response(body), nil
			}
			return next.RoundTrip(r)
		})
		if _, err := m.Begin(context.Background(), "tenant", "alice", "notes"); err == nil {
			t.Errorf("accepted %s", body)
		}
	}
}
func TestStatusNeverContainsCredentialMaterial(t *testing.T) {
	m, _, _ := setup(t)
	a, err := m.Begin(context.Background(), "tenant", "alice", "notes")
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Complete(context.Background(), "tenant", "alice", "notes", nonceOf(t, a), "code"); err != nil {
		t.Fatal(err)
	}
	status, err := m.Status("tenant", "alice", "notes")
	if err != nil || !status.Connected {
		t.Fatal(status, err)
	}
	raw, _ := json.Marshal(status)
	if strings.Contains(string(raw), "secret") {
		t.Fatal(string(raw))
	}
}

func TestCustomEndpointAndWorkspaceConsentStayBound(t *testing.T) {
	m, _, _ := setup(t)
	ctx := context.Background()
	resource := "https://service.test/mcp"
	a, err := m.BeginFor(ctx, "tenant", "alice", WorkspaceOwner, "other", resource)
	if err != nil {
		t.Fatal(err)
	}
	if a.Provider != "workspace-"+OtherProvider(resource) {
		t.Fatalf("provider=%s", a.Provider)
	}
	nonce := nonceOf(t, a)
	if m.CompleteFor(ctx, "tenant", "bob", WorkspaceOwner, a.Provider, nonce, "code") == nil {
		t.Fatal("another admin consumed consent")
	}
	if m.CompleteFor(ctx, "tenant", "alice", "alice", a.Provider, nonce, "code") == nil {
		t.Fatal("workspace consent moved to a personal account")
	}
	if err = m.CompleteFor(ctx, "tenant", "alice", WorkspaceOwner, a.Provider, nonce, "code"); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Token(ctx, "tenant", "alice", a.Provider); err == nil {
		t.Fatal("workspace token became personal")
	}
	held, err := m.Connections("tenant", WorkspaceOwner)
	if err != nil || len(held) != 1 || held[0].Resource != resource {
		t.Fatalf("connections=%+v err=%v", held, err)
	}
	if got, err := m.Resource("tenant", WorkspaceOwner, a.Provider); err != nil || got != resource {
		t.Fatal(got, err)
	}
	if _, err = m.Resource("other-tenant", WorkspaceOwner, a.Provider); err == nil {
		t.Fatal("cross-tenant resource read")
	}
	if _, err = m.BeginFor(ctx, "tenant", "alice", "alice", OtherProvider(resource), "https://different.test/mcp"); err == nil {
		t.Fatal("custom name rebound")
	}
	if err = m.Disconnect("tenant", WorkspaceOwner, a.Provider); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Token(ctx, "tenant", WorkspaceOwner, a.Provider); err == nil {
		t.Fatal("revoked shared token still usable")
	}
}

func TestResourceMetadataChallengeIsPublicAndEndpointBound(t *testing.T) {
	m, _, _ := setup(t)
	prior := m.client.Transport
	m.client.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if strings.HasPrefix(r.URL.Path, "/.well-known/oauth-protected-resource") {
			return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}}, nil
		}
		if r.URL.Path == "/mcp" {
			if r.Header.Get("Authorization") != "" {
				t.Fatal("discovery sent a credential")
			}
			return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{"Www-Authenticate": []string{`Bearer resource_metadata="https://service.test/metadata"`}}}, nil
		}
		if r.URL.Path == "/metadata" {
			return response(`{"resource":"https://service.test/mcp","authorization_servers":["https://auth.test"]}`), nil
		}
		return prior.RoundTrip(r)
	})
	if _, err := m.Begin(context.Background(), "tenant", "alice", "notes"); err != nil {
		t.Fatal(err)
	}
	m.client.Transport = transport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{"Www-Authenticate": []string{`Bearer resource_metadata="https://127.0.0.1/metadata"`}}}, nil
	})
	if _, err := m.Begin(context.Background(), "tenant", "alice", "notes"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unsafe challenge=%v", err)
	}
}
