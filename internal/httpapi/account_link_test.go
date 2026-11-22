package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/accounts"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/connectionauth"
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

	w := call("POST", authorize, `{}`, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("authorize: %d %s", w.Code, w.Body)
	}
	var cookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "humagen_account_link" {
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
}
