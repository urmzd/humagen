package connectionauth

import (
	"context"
	"errors"
	"github.com/urmzd/humagen/pkg/state"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestNativeAuthorizationHasSeparateAudienceAndExplicitSourceBinding(t *testing.T) {
	ctx := context.Background()
	client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://native.test/token" {
			t.Fatalf("unexpected discovery: %s", r.URL)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("code_verifier") == "" || r.Form.Get("resource") != "" || r.Form.Get("client_secret") != "registered-secret" {
			t.Fatal("native exchange lost binding or used MCP audience")
		}
		return response(`{"access_token":"native-token","token_type":"Bearer","expires_in":3600,"refresh_token":"native-refresh"}`), nil
	})}
	m, err := New(state.NewMemory(), Config{Key: []byte(strings.Repeat("n", 32)), RedirectURL: "https://humagen.test/connections/callback", HTTP: client, Providers: []Provider{{Name: "drive", Resource: "https://native.test/drive/", Issuer: "https://native.test", ClientID: "registered", ClientSecret: "registered-secret", AuthorizationURL: "https://native.test/authorize", TokenURL: "https://native.test/token", Offline: true, Scopes: []string{"readonly"}}}})
	if err != nil {
		t.Fatal(err)
	}
	connect := func() {
		t.Helper()
		a, err := m.Begin(ctx, "tenant", "alice", "drive")
		if err != nil {
			t.Fatal(err)
		}
		u, _ := url.Parse(a.URL)
		if u.Query().Get("resource") != "" || u.Query().Get("access_type") != "offline" || u.Query().Get("prompt") != "consent" {
			t.Fatal("wrong native consent parameters")
		}
		if err = m.Complete(ctx, "tenant", "alice", "drive", nonceOf(t, a), "code"); err != nil {
			t.Fatal(err)
		}
	}
	connect()
	if _, err = m.BoundToken(ctx, "tenant", "alice", "source:folder-project", "drive"); !errors.Is(err, ErrReconnect) {
		t.Fatal("account consent silently exposed a source")
	}
	if err = m.Bind("tenant", "alice", "source:folder-project", "drive"); err != nil {
		t.Fatal(err)
	}
	if token, err := m.BoundToken(ctx, "tenant", "alice", "source:folder-project", "drive"); err != nil || token != "native-token" {
		t.Fatal("bound account unavailable")
	}
	for _, args := range [][3]string{{"tenant", "bob", "source:folder-project"}, {"other", "alice", "source:folder-project"}, {"tenant", "alice", "source:other-project"}} {
		if _, err = m.BoundToken(ctx, args[0], args[1], args[2], "drive"); !errors.Is(err, ErrReconnect) {
			t.Fatal("binding crossed person tenant or source")
		}
	}
	connect()
	if _, err = m.BoundToken(ctx, "tenant", "alice", "source:folder-project", "drive"); !errors.Is(err, ErrReconnect) {
		t.Fatal("reconnected account reused prior consent")
	}
	if err = m.Bind("tenant", "alice", "source:folder-project", "drive"); err != nil {
		t.Fatal(err)
	}
	if err = m.Disconnect("tenant", "alice", "drive"); err != nil {
		t.Fatal(err)
	}
	if _, err = m.BoundToken(ctx, "tenant", "alice", "source:folder-project", "drive"); !errors.Is(err, ErrReconnect) {
		t.Fatal("revoked account still usable")
	}
}

func TestNativeSlackRotationRetainsVerifiedInstallation(t *testing.T) {
	ctx := context.Background()
	exchanges := 0
	client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		exchanges++
		if r.Form.Get("grant_type") == "refresh_token" {
			return response(`{"ok":true,"access_token":"rotated-bot","token_type":"bot","refresh_token":"rotated-refresh","expires_in":3600}`), nil
		}
		return response(`{"ok":true,"access_token":"initial-bot","token_type":"bot","refresh_token":"initial-refresh","expires_in":1,"app_id":"A12345","team":{"id":"T12345"}}`), nil
	})}
	m, err := New(state.NewMemory(), Config{Key: []byte(strings.Repeat("s", 32)), RedirectURL: "https://humagen.test/connections/callback", HTTP: client, Providers: []Provider{{Name: "slack", Resource: "https://slack.com/api/", Issuer: "https://slack.com", ClientID: "app", ClientSecret: "secret", AuthorizationURL: "https://slack.com/oauth/v2/authorize", TokenURL: "https://slack.com/api/oauth.v2.access", Scopes: []string{"chat:write", "files:read"}}}})
	if err != nil {
		t.Fatal(err)
	}
	a, err := m.BeginFor(ctx, "tenant", "admin", WorkspaceOwner, "slack", "")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(a.URL)
	if u.Query().Get("scope") != "chat:write,files:read" {
		t.Fatal("wrong Slack scopes")
	}
	if err = m.CompleteFor(ctx, "tenant", "admin", WorkspaceOwner, "workspace-slack", nonceOf(t, a), "code"); err != nil {
		t.Fatal(err)
	}
	if err = m.Bind("tenant", WorkspaceOwner, "client:slack-t12345", "workspace-slack"); err != nil {
		t.Fatal(err)
	}
	token, err := m.BoundToken(ctx, "tenant", WorkspaceOwner, "client:slack-t12345", "workspace-slack")
	if err != nil || token != "rotated-bot" || exchanges != 2 {
		t.Fatal("Slack refresh failed", err, exchanges)
	}
	details, err := m.Details("tenant", WorkspaceOwner, "workspace-slack")
	if err != nil || details["team"] != "T12345" || details["app_id"] != "A12345" {
		t.Fatal("rotation replaced verified installation")
	}
}
