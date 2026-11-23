package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/accounts"
	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/connectionauth"
	"github.com/urmzd/humagen/pkg/state"
)

type accountLinkTransport func(*http.Request) (*http.Response, error)

func (f accountLinkTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// An account link begins and completes only in the browser that asked: the
// consent is bound to an HttpOnly cookie, and a bearer token or a call an
// agent or the MCP dispatched is refused.
func TestAnAccountLinkIsBoundToTheBrowserThatBeganIt(t *testing.T) {
	gh := platform.NativeConnectionProvider(platform.GitHubAccount)
	gh.ClientID, gh.ClientSecret = "app-client", "app-secret"
	fake := accountLinkTransport(func(r *http.Request) (*http.Response, error) {
		body := `{}`
		switch r.URL.Host + r.URL.Path {
		case "github.com/login/oauth/access_token":
			body = `{"access_token":"user-token","token_type":"bearer"}`
		case "api.github.com/user":
			body = `{"id":42,"login":"octocat","type":"User"}`
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	p, err := platform.New(platform.Config{Workspace: t.TempDir(), ConnectionAuth: &connectionauth.Config{Key: []byte(strings.Repeat("k", 32)), RedirectURL: "https://humagen.test/connections/callback", Providers: []connectionauth.Provider{gh}, HTTP: &http.Client{Transport: fake}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	who := auth.Principal{Subject: "ana", Tenant: "acme", Kind: auth.User, Roles: []auth.Role{auth.Admin}}
	s := &Server{P: p, Authn: &Authn{Dev: &who}, Log: slog.New(slog.DiscardHandler)}
	h := s.Handler()
	call := func(method, path, body string, edit func(*http.Request)) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "http://example.com")
		if edit != nil {
			edit(r)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	const authorize, callback = "/api/connections/github-account/authorize", "/api/connections/github-account/callback"

	if w := call("POST", authorize, `{}`, func(r *http.Request) { r.Header.Set("Authorization", "Bearer token") }); w.Code != http.StatusForbidden {
		t.Fatalf("a bearer token began a link: %d %s", w.Code, w.Body)
	}
	if status, out := dispatch(t.Context(), h, who, "POST", authorize, nil, strings.NewReader(`{}`)); status != http.StatusForbidden {
		t.Fatalf("a dispatched call began a link: %d %s", status, out)
	}
	// A page on another site cannot start, finish or disconnect a link with
	// the person's cookies.
	crossSite := func(r *http.Request) {
		r.Header.Set("Origin", "https://elsewhere.test")
		r.Header.Set("Sec-Fetch-Site", "cross-site")
	}
	for _, c := range []struct{ method, path, body string }{
		{"POST", authorize, `{}`},
		{"POST", callback, `{"state":"s","code":"c"}`},
		{"DELETE", "/api/connections/github-account", ""},
		{"DELETE", "/api/clients/gh/accounts/42", ""},
	} {
		w := call(c.method, c.path, c.body, crossSite)
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s %s from another site: %d %s", c.method, c.path, w.Code, w.Body)
		}
		for _, ck := range w.Result().Cookies() {
			t.Fatalf("%s %s from another site set %s", c.method, c.path, ck.Name)
		}
	}

	w := call("POST", authorize, `{}`, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("authorize: %d %s", w.Code, w.Body)
	}
	var cookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "humagen_account_link_github_account" {
			cookie = c
		}
	}
	if cookie == nil || cookie.Value == "" || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("no browser secret: %+v", w.Result().Cookies())
	}
	var a connectionauth.Authorization
	if err := json.Unmarshal(w.Body.Bytes(), &a); err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(a.URL)
	if strings.Contains(a.URL, cookie.Value) {
		t.Fatal("the consent URL carries the browser secret")
	}
	done, _ := json.Marshal(ConnectionCallback{State: u.Query().Get("state"), Code: "code"})

	if w := call("POST", callback, string(done), nil); w.Code != http.StatusForbidden {
		t.Fatalf("a callback with no browser secret: %d %s", w.Code, w.Body)
	}
	if w := call("POST", callback, string(done), func(r *http.Request) { r.AddCookie(&http.Cookie{Name: cookie.Name, Value: "another-browser"}) }); w.Code != http.StatusBadRequest {
		t.Fatalf("a callback from another browser: %d %s", w.Code, w.Body)
	}
	if status, _ := dispatch(t.Context(), h, who, "POST", callback, nil, strings.NewReader(string(done))); status != http.StatusForbidden {
		t.Fatalf("a dispatched callback: %d", status)
	}
	if _, err := p.Accounts.Of("acme", accounts.GitHub, "ana"); err == nil {
		t.Fatal("linked before the right browser called back")
	}
	// Another person signed in to the same browser completes nothing, for
	// themselves or for the person who began it.
	who.Subject = "bob"
	if w := call("POST", callback, string(done), func(r *http.Request) { r.AddCookie(cookie) }); w.Code != http.StatusBadRequest {
		t.Fatalf("another person's callback: %d %s", w.Code, w.Body)
	}
	for _, subject := range []string{"ana", "bob"} {
		if _, err := p.Accounts.Of("acme", accounts.GitHub, subject); err == nil {
			t.Fatalf("%s was linked by another person's callback", subject)
		}
	}
	who.Subject = "ana"
	// Those callbacks ended the consent: the browser that began it completes
	// nothing with it, and begins again.
	if w := call("POST", callback, string(done), func(r *http.Request) { r.AddCookie(cookie) }); w.Code != http.StatusBadRequest {
		t.Fatalf("an ended consent: %d %s", w.Code, w.Body)
	}
	w = call("POST", authorize, `{}`, nil)
	for _, c := range w.Result().Cookies() {
		if c.Name == cookie.Name {
			cookie = c
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &a); err != nil {
		t.Fatal(err)
	}
	u, _ = url.Parse(a.URL)
	done, _ = json.Marshal(ConnectionCallback{State: u.Query().Get("state"), Code: "code"})
	w = call("POST", callback, string(done), func(r *http.Request) { r.AddCookie(cookie) })
	if w.Code != http.StatusNoContent {
		t.Fatalf("the browser that began it: %d %s", w.Code, w.Body)
	}
	if got, err := p.Accounts.Of("acme", accounts.GitHub, "ana"); err != nil || got.ID != "42" {
		t.Fatalf("linked %+v %v", got, err)
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == cookie.Name && c.MaxAge >= 0 {
			t.Fatalf("the browser secret was kept: %+v", c)
		}
	}
	// A second callback with the same state and secret is refused.
	if w := call("POST", callback, string(done), func(r *http.Request) { r.AddCookie(cookie) }); w.Code != http.StatusBadRequest {
		t.Fatalf("a repeated callback: %d %s", w.Code, w.Body)
	}

	// The person disconnects their own account in their browser, as they
	// connected it.
	const own = "/api/connections/github-account"
	if w := call("DELETE", own, "", func(r *http.Request) { r.Header.Set("Authorization", "Bearer token") }); w.Code != http.StatusForbidden {
		t.Fatalf("a bearer token disconnected an account: %d %s", w.Code, w.Body)
	}
	if status, out := dispatch(t.Context(), h, who, "DELETE", own, nil, nil); status != http.StatusForbidden {
		t.Fatalf("a dispatched call disconnected an account: %d %s", status, out)
	}
	if _, err := p.Accounts.Of("acme", accounts.GitHub, "ana"); err != nil {
		t.Fatalf("the account was disconnected: %v", err)
	}
	if w := call("DELETE", own, "", nil); w.Code != http.StatusNoContent {
		t.Fatalf("disconnect in the browser: %d %s", w.Code, w.Body)
	}
	if _, err := p.Accounts.Of("acme", accounts.GitHub, "ana"); err == nil {
		t.Fatal("still linked after Disconnect")
	}
}

// Each provider's link has its own browser secret, so beginning one leaves
// another's in place.
func TestEachAccountLinkHasItsOwnBrowserSecret(t *testing.T) {
	s := &Server{}
	gh, sl := s.linkCookieName(platform.GitHubAccount), s.linkCookieName(platform.SlackAccount)
	if gh == sl || gh == "" || sl == "" {
		t.Fatalf("cookie names %q and %q", gh, sl)
	}
	s.Authn = &Authn{Secure: true}
	if name := s.linkCookieName(platform.GitHubAccount); !strings.HasPrefix(name, "__Host-") {
		t.Fatalf("secure cookie name %q", name)
	}
}

// accountLinkFixture is a server with GitHub account linking against a
// stand-in GitHub that counts code exchanges, signed in as *who, and the
// server's log.
func accountLinkFixture(t *testing.T) (h http.Handler, p *platform.Platform, who *auth.Principal, exchanges *atomic.Int32, logs *strings.Builder) {
	t.Helper()
	gh := platform.NativeConnectionProvider(platform.GitHubAccount)
	gh.ClientID, gh.ClientSecret = "app-client", "app-secret"
	exchanges = &atomic.Int32{}
	fake := accountLinkTransport(func(r *http.Request) (*http.Response, error) {
		body := `{}`
		switch r.URL.Host + r.URL.Path {
		case "github.com/login/oauth/access_token":
			exchanges.Add(1)
			body = `{"access_token":"user-token","token_type":"bearer"}`
		case "api.github.com/user":
			body = `{"id":777,"login":"octocat","type":"User"}`
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	logs = &strings.Builder{}
	log := slog.New(slog.NewTextHandler(lockedWriter{&sync.Mutex{}, logs}, nil))
	p, err := platform.New(platform.Config{Workspace: t.TempDir(), Log: log, ConnectionAuth: &connectionauth.Config{Key: []byte(strings.Repeat("k", 32)), RedirectURL: "https://humagen.test/connections/callback", Providers: []connectionauth.Provider{gh}, HTTP: &http.Client{Transport: fake}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	who = &auth.Principal{Subject: "bob", Tenant: "acme", Kind: auth.User, Scopes: []auth.Permission{auth.RunsRead, auth.RunsWrite}}
	s := &Server{P: p, Authn: &Authn{Dev: who}, Log: log}
	return s.Handler(), p, who, exchanges, logs
}

type lockedWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (l lockedWriter) Write(b []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(b)
}

// A consent is completed only in the browser that began it, and a callback
// for it from anywhere else ends it: another person's browser presenting its
// state, with no link cookie or with a cookie of their own, or a caller that
// is not a browser. The person who began it then completes nothing with
// that state, and no code is exchanged.
func TestACallbackFromElsewhereEndsTheAccountLinkConsent(t *testing.T) {
	const authorize, callback = "/api/connections/github-account/authorize", "/api/connections/github-account/callback"
	elsewhere := []struct {
		why  string
		send func(h http.Handler, who *auth.Principal, body string) int
	}{
		{"another person's browser with no link cookie", func(h http.Handler, who *auth.Principal, body string) int {
			who.Subject = "ana"
			return accountLinkCall(h, "POST", callback, body, nil).Code
		}},
		{"another person's browser with a cookie of its own", func(h http.Handler, who *auth.Principal, body string) int {
			who.Subject = "ana"
			return accountLinkCall(h, "POST", callback, body, func(r *http.Request) {
				r.AddCookie(&http.Cookie{Name: "humagen_account_link_github_account", Value: "her-own"})
			}).Code
		}},
		{"the same person with no link cookie", func(h http.Handler, who *auth.Principal, body string) int {
			return accountLinkCall(h, "POST", callback, body, nil).Code
		}},
		{"a caller with a bearer token", func(h http.Handler, who *auth.Principal, body string) int {
			return accountLinkCall(h, "POST", callback, body, func(r *http.Request) { r.Header.Set("Authorization", "Bearer token") }).Code
		}},
		{"a call the MCP endpoint dispatched", func(h http.Handler, who *auth.Principal, body string) int {
			status, _ := dispatch(context.Background(), h, *who, "POST", callback, nil, strings.NewReader(body))
			return status
		}},
	}
	for _, c := range elsewhere {
		t.Run(c.why, func(t *testing.T) {
			h, p, who, exchanges, logs := accountLinkFixture(t)
			w := accountLinkCall(h, "POST", authorize, `{}`, nil)
			if w.Code != http.StatusOK {
				t.Fatalf("authorize: %d %s", w.Code, w.Body)
			}
			var cookie *http.Cookie
			for _, ck := range w.Result().Cookies() {
				if ck.Name == "humagen_account_link_github_account" {
					cookie = ck
				}
			}
			var a connectionauth.Authorization
			if err := json.Unmarshal(w.Body.Bytes(), &a); err != nil || cookie == nil {
				t.Fatalf("authorize: %v %v", err, cookie)
			}
			u, _ := url.Parse(a.URL)
			nonce := u.Query().Get("state")
			done, _ := json.Marshal(ConnectionCallback{State: nonce, Code: "the-code"})

			if status := c.send(h, who, string(done)); status != http.StatusForbidden && status != http.StatusBadRequest {
				t.Fatalf("the callback from elsewhere: %d", status)
			}
			who.Subject = "bob"
			if w := accountLinkCall(h, "POST", callback, string(done), func(r *http.Request) { r.AddCookie(cookie) }); w.Code != http.StatusBadRequest {
				t.Fatalf("the browser that began it, afterwards: %d %s", w.Code, w.Body)
			}
			if n := exchanges.Load(); n != 0 {
				t.Fatalf("%d code exchanges", n)
			}
			if all, err := p.Accounts.List("acme", accounts.GitHub, "", 10); err != nil || len(all) != 0 {
				t.Fatalf("linked %+v %v", all, err)
			}
			records, err := p.Audit.List("acme", audit.Query{Resource: "connection", From: time.Now().Add(-time.Hour), To: time.Now().Add(time.Hour), Limit: 50}, func(audit.Record) bool { return true })
			if err != nil {
				t.Fatal(err)
			}
			audited := false
			for _, r := range records {
				audited = audited || (r.Action == "connection.end" || r.Action == "connection.connect") && r.Outcome == audit.Failed && r.Change.ID == platform.GitHubAccount
			}
			if !audited {
				t.Fatalf("the callback from elsewhere is not in the audit: %+v", records)
			}
			out := logs.String()
			if !strings.Contains(out, "ended it") {
				t.Fatalf("the ending was not logged: %s", out)
			}
			for _, secret := range []string{nonce, "the-code", cookie.Value} {
				if strings.Contains(out, secret) {
					t.Fatal("the log carries the consent's state, code or browser secret")
				}
			}
		})
	}
}

// accountLinkCall sends a same-origin request to h.
func accountLinkCall(h http.Handler, method, path, body string, edit func(*http.Request)) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://example.com")
	if edit != nil {
		edit(r)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// GitHub is asked to show the account choice; Slack's OpenID Connect
// authorize documents no prompt, so it is sent none.
func TestTheAccountLinkConsentAsksForTheAccountChoice(t *testing.T) {
	gh, sl := platform.NativeConnectionProvider(platform.GitHubAccount), platform.NativeConnectionProvider(platform.SlackAccount)
	gh.ClientID, gh.ClientSecret, sl.ClientID, sl.ClientSecret = "gh-client", "gh-secret", "sl-client", "sl-secret"
	m, err := connectionauth.New(state.NewMemory(), connectionauth.Config{Key: []byte(strings.Repeat("k", 32)), RedirectURL: "https://humagen.test/connections/callback", Providers: []connectionauth.Provider{gh, sl}})
	if err != nil {
		t.Fatal(err)
	}
	for provider, prompt := range map[string]string{platform.GitHubAccount: "select_account", platform.SlackAccount: ""} {
		a, err := m.BeginBound(t.Context(), "acme", "bob", provider, "", "browser")
		if err != nil {
			t.Fatal(err)
		}
		u, _ := url.Parse(a.URL)
		if got := u.Query().Get("prompt"); got != prompt || u.Query().Has("prompt") != (prompt != "") {
			t.Fatalf("%s consent prompt %q, want %q: %s", provider, got, prompt, a.URL)
		}
	}
}
