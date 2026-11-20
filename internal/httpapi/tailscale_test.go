package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/urmzd/humagen/pkg/auth"
)

const serveOrigin = "https://humagen.example.ts.net"

func tailnetRequest(path, login string) *http.Request {
	r := httptest.NewRequest("GET", serveOrigin+path, nil)
	r.RemoteAddr = "127.0.0.1:12345"
	if login != "" {
		r.Header.Set("Tailscale-User-Login", login)
	}
	return r
}

func TestTailscaleIdentityAndPolicy(t *testing.T) {
	pol, err := auth.ParsePolicy("tailnet", `
role viewer { allow read on * }
role admin { allow * on * }
grant viewer to subject "tailscale:alice@example.com"
grant admin to group "humagen-admins"
grant admin to email domain "example.com"
`)
	if err != nil {
		t.Fatal(err)
	}
	a, err := NewTailscaleAuthn("127.0.0.1:8080", serveOrigin, "acme", pol)
	if err != nil {
		t.Fatal(err)
	}
	r := tailnetRequest("/api/me", "alice@example.com")
	r.Header.Set("Tailscale-User-Name", "=?utf-8?q?Alice_B=C3=BCller?=")
	r.Header.Set("Tailscale-User-Groups", "humagen-admins")
	r.Header.Set("X-Tenant", "evil")
	who, err := a.Authenticate(r)
	if err != nil || who.Subject != "tailscale:alice@example.com" || who.Tenant != "acme" || who.Name != "Alice Büller" || !who.Can(auth.RunsRead) || who.Can(auth.RunsWrite) {
		t.Fatalf("identity: %+v, %v", who, err)
	}
	if d := who.Policy().Explain(who, auth.RunsRead); !d.Allowed || d.Policy.Grant == nil {
		t.Fatalf("missing grant explanation: %v", d)
	}
	stranger, err := a.Authenticate(tailnetRequest("/api/me", "stranger@example.com"))
	if err != nil || len(stranger.Granted()) != 0 {
		t.Fatalf("unmapped user: %+v, %v", stranger, err)
	}
}

func TestTailscaleRefusesUntrustedRequests(t *testing.T) {
	a, err := NewTailscaleAuthn("127.0.0.1:8080", serveOrigin, "acme", nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*http.Request){
		"missing identity":   func(r *http.Request) { r.Header.Del("Tailscale-User-Login") },
		"remote peer":        func(r *http.Request) { r.RemoteAddr = "100.64.0.1:1234" },
		"forwarded peer":     func(r *http.Request) { r.RemoteAddr = "192.168.1.1:1234"; r.Header.Set("X-Forwarded-For", "127.0.0.1") },
		"malformed peer":     func(r *http.Request) { r.RemoteAddr = "localhost" },
		"wrong host":         func(r *http.Request) { r.Host = "attacker.example" },
		"bearer":             func(r *http.Request) { r.Header.Set("Authorization", "Bearer fake") },
		"duplicate identity": func(r *http.Request) { r.Header.Add("Tailscale-User-Login", "admin@example.com") },
		"combined identity":  func(r *http.Request) { r.Header.Set("Tailscale-User-Login", "alice@example.com,admin@example.com") },
	} {
		t.Run(name, func(t *testing.T) {
			r := tailnetRequest("/api/me", "alice@example.com")
			mutate(r)
			if _, err := a.Authenticate(r); !errors.Is(err, auth.ErrUnauthenticated) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestTailscaleRoutes(t *testing.T) {
	pol, err := auth.ParsePolicy("tailnet", "role viewer { allow read on * }\ngrant viewer to subject \"tailscale:alice@example.com\"")
	if err != nil {
		t.Fatal(err)
	}
	a, err := NewTailscaleAuthn("127.0.0.1:8080", serveOrigin, "acme", pol)
	if err != nil {
		t.Fatal(err)
	}
	f := newFixture(t)
	s := &Server{P: f.p, Authn: a, Log: slog.New(slog.DiscardHandler)}
	h := s.Handler()
	for _, tc := range []struct {
		path, login string
		status      int
		location    string
	}{
		{"/api/me", "alice@example.com", 200, ""},
		{"/api/me", "", 401, ""},
		{"/api/runs", "stranger@example.com", 403, ""},
		{"/auth/login?return_to=/runs", "alice@example.com", 302, "/runs"},
		{"/auth/login?return_to=//evil.example", "alice@example.com", 302, "/"},
		{"/auth/login", "", 401, ""},
		{"/auth/callback?code=fake", "alice@example.com", 400, ""},
	} {
		t.Run(tc.path+tc.login, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, tailnetRequest(tc.path, tc.login))
			if w.Code != tc.status || w.Header().Get("Location") != tc.location {
				t.Fatalf("status %d location %q: %s", w.Code, w.Header().Get("Location"), w.Body.String())
			}
		})
	}
}

func TestTailscaleConfiguration(t *testing.T) {
	for _, tc := range []struct{ addr, origin, tenant string }{
		{"0.0.0.0:8080", serveOrigin, "acme"},
		{"100.64.0.1:8080", serveOrigin, "acme"},
		{"127.0.0.1:8080", "http://humagen.example.ts.net", "acme"},
		{"127.0.0.1:8080", serveOrigin + "/path", "acme"},
		{"127.0.0.1:8080", serveOrigin, ""},
	} {
		if _, err := NewTailscaleAuthn(tc.addr, tc.origin, tc.tenant, nil); err == nil {
			t.Errorf("accepted %+v", tc)
		}
	}
	if _, err := NewTailscaleAuthn("[::1]:8080", serveOrigin, "acme", nil); err != nil {
		t.Fatal(err)
	}
}

// The Slack app the server starts cannot come through Serve, so it presents
// the token the server made for it, from loopback, at the loopback address.
func childRequest(path, token string) *http.Request {
	r := httptest.NewRequest("GET", "http://127.0.0.1:8080"+path, nil)
	r.RemoteAddr = "127.0.0.1:23456"
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	return r
}

func TestTailscaleMintedMachine(t *testing.T) {
	pol, err := auth.ParsePolicy("tailnet", `
role admin { allow * on * }
grant admin to anyone
`)
	if err != nil {
		t.Fatal(err)
	}
	a, err := NewTailscaleAuthn("127.0.0.1:8080", serveOrigin, "acme", pol)
	if err != nil {
		t.Fatal(err)
	}
	scopes := []auth.Permission{auth.RunsRead, auth.RunsWrite}
	acme, err := a.MintMachine("acme", "slack-app:T1", "Slack app", scopes)
	if err != nil {
		t.Fatal(err)
	}
	other, err := a.MintMachine("other", "slack-app:T2", "Slack app", scopes)
	if err != nil {
		t.Fatal(err)
	}
	if acme == other || len(acme) < 43 {
		t.Fatalf("tokens are not each their own 32 random bytes: %d and %d characters", len(acme), len(other))
	}
	if _, err := a.MintMachine("acme", "slack-app:T1", "Slack app", scopes); err == nil {
		t.Error("one machine was given two credentials")
	}
	for _, bad := range [][2]string{{"", "slack-app:T1"}, {"acme", ""}} {
		if _, err := a.MintMachine(bad[0], bad[1], "", scopes); err == nil {
			t.Errorf("minted for tenant %q subject %q", bad[0], bad[1])
		}
	}

	// A machine: its own tenant and subject, exactly its scopes, and no role,
	// though the policy grants admin to anyone.
	who, err := a.Authenticate(childRequest("/api/me", acme))
	if err != nil || who.Kind != auth.Machine || who.Tenant != "acme" || who.Subject != "slack-app:T1" || len(who.Roles) != 0 {
		t.Fatalf("machine: %+v, %v", who, err)
	}
	if got := who.Granted(); len(got) != 2 || !who.Can(auth.RunsRead) || !who.Can(auth.RunsWrite) || who.Can(auth.AgentsWrite) {
		t.Fatalf("machine may %v, want exactly its scopes", got)
	}
	if who.Policy() != pol {
		t.Error("the machine does not carry the policy its mapped people are decided by")
	}
	who, err = a.Authenticate(childRequest("/api/me", other))
	if err != nil || who.Tenant != "other" || who.Subject != "slack-app:T2" {
		t.Fatalf("second machine: %+v, %v", who, err)
	}
	// The child may call any loopback name, with or without a port.
	for _, host := range []string{"localhost:8080", "[::1]:8080", "127.0.0.1"} {
		r := childRequest("/api/me", acme)
		r.Host = host
		if _, err := a.Authenticate(r); err != nil {
			t.Errorf("host %s: %v", host, err)
		}
	}

	for name, mutate := range map[string]func(*http.Request){
		"wrong token":           func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+acme[:len(acme)-1]+"!") },
		"empty token":           func(r *http.Request) { r.Header.Set("Authorization", "Bearer ") },
		"another scheme":        func(r *http.Request) { r.Header.Set("Authorization", "Basic "+acme) },
		"lowercase scheme":      func(r *http.Request) { r.Header.Set("Authorization", "bearer "+acme) },
		"two credentials":       func(r *http.Request) { r.Header.Add("Authorization", "Bearer "+other) },
		"remote peer":           func(r *http.Request) { r.RemoteAddr = "100.64.0.1:1234" },
		"forwarded peer":        func(r *http.Request) { r.RemoteAddr = "192.168.1.1:1234"; r.Header.Set("X-Forwarded-For", "127.0.0.1") },
		"malformed peer":        func(r *http.Request) { r.RemoteAddr = "localhost" },
		"with a login":          func(r *http.Request) { r.Header.Set("Tailscale-User-Login", "alice@example.com") },
		"with a name":           func(r *http.Request) { r.Header.Set("Tailscale-User-Name", "Alice") },
		"with an empty login":   func(r *http.Request) { r.Header.Set("Tailscale-User-Login", "") },
		"with any Serve header": func(r *http.Request) { r.Header.Set("Tailscale-User-Profile-Pic", "https://example.com/a.png") },
		"through Serve":         func(r *http.Request) { r.Host = "humagen.example.ts.net" },
		"another host":          func(r *http.Request) { r.Host = "attacker.example" },
	} {
		t.Run(name, func(t *testing.T) {
			r := childRequest("/api/me", acme)
			mutate(r)
			if _, err := a.Authenticate(r); !errors.Is(err, auth.ErrUnauthenticated) {
				t.Fatalf("got %v", err)
			}
		})
	}

	// A person through Serve is who they were, and a token does not help them.
	alice, err := a.Authenticate(tailnetRequest("/api/me", "alice@example.com"))
	if err != nil || alice.Kind != auth.User || alice.Subject != "tailscale:alice@example.com" {
		t.Fatalf("person: %+v, %v", alice, err)
	}
	r := tailnetRequest("/api/me", "alice@example.com")
	r.Header.Set("Authorization", "Bearer "+acme)
	if _, err := a.Authenticate(r); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("a token with an identity: %v", err)
	}
	// The header path keeps its Host check: a loopback caller naming the
	// loopback address with an identity header and no token is nobody.
	r = childRequest("/api/me", "")
	r.Header.Set("Tailscale-User-Login", "alice@example.com")
	if _, err := a.Authenticate(r); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("an identity at the loopback address: %v", err)
	}

	// Through the routes: the token is a 200 with the machine's permissions,
	// a wrong one a 401, and a permission it lacks a 403.
	f := newFixture(t)
	h := (&Server{P: f.p, Authn: a, Log: slog.New(slog.DiscardHandler)}).Handler()
	for _, tc := range []struct {
		path, token string
		status      int
	}{
		{"/api/me", acme, 200},
		{"/api/me", "wrong", 401},
		{"/api/me", "", 401},
		{"/api/audit", acme, 403},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, childRequest(tc.path, tc.token))
		if w.Code != tc.status {
			t.Errorf("%s with %q: %d %s", tc.path, tc.token, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), acme) {
			t.Errorf("%s: the answer holds the token", tc.path)
		}
	}
}

// Only Tailscale sign-in mints: every other mode has a credential to give.
func TestOnlyTailscaleMintsAMachine(t *testing.T) {
	dev := auth.Principal{Subject: "dev", Tenant: "acme", Kind: auth.User}
	for name, a := range map[string]*Authn{"dev": {Dev: &dev}, "oidc": {}} {
		if tok, err := a.MintMachine("acme", "slack-app:T1", "", nil); err == nil || tok != "" {
			t.Errorf("%s minted a credential", name)
		}
	}
}
