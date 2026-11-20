package platform

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/connectionauth"
	"github.com/urmzd/humagen/pkg/personctx"
	"github.com/urmzd/humagen/pkg/threads"
)

func TestNativeSlackUseRejectsOnlyConfirmedCurrentConsent(t *testing.T) {
	for _, tc := range []struct {
		name, body                string
		status                    int
		fail, reconnect, rejected bool
	}{
		{name: "invalid", body: `{"ok":false,"error":"invalid_auth"}`, status: 200, rejected: true},
		{name: "revoked", body: `{"ok":false,"error":"token_revoked"}`, status: 200, rejected: true},
		{name: "unauthorized", body: `private diagnostic`, status: 401, rejected: true},
		{name: "scope", body: `{"ok":false,"error":"missing_scope"}`, status: 200},
		{name: "forbidden", body: `{"ok":false,"error":"invalid_auth"}`, status: 403},
		{name: "outage", body: `{"ok":false,"error":"invalid_auth"}`, status: 503},
		{name: "network", fail: true},
		{name: "old-consent", body: `{"ok":false,"error":"invalid_auth"}`, status: 200, reconnect: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := New(Config{Workspace: t.TempDir(), ConnectionAuth: nativeConfig("slack")})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = p.Close() })
			if _, err = p.Agents.Create(t.Context(), "tenant", wire.AgentSpec{Name: BuiltinEntry, Model: "offline", Provider: "offline", SystemPrompt: "Reconnect"}, "test"); err != nil {
				t.Fatal(err)
			}
			owner := auth.Principal{Tenant: "tenant", Subject: "alice", Kind: auth.User, Scopes: []auth.Permission{auth.ClientsWrite}}
			authorizeNative(t, p, owner, "slack", "workspace")
			c, err := p.Clients.Get("tenant", "slack-t12345")
			if err != nil {
				t.Fatal(err)
			}
			token, err := p.slackClientToken(c)
			if err != nil {
				t.Fatal(err)
			}
			client := p.slackOAuthClient(c, token)
			client.Transport.(*slackOAuthTransport).base = nativeHTTP(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("Authorization") != "Bearer bot-token" {
					t.Fatal("missing native credential")
				}
				if tc.reconnect {
					next := owner
					next.Subject = "bob"
					authorizeNative(t, p, next, "slack", "workspace")
				}
				if tc.fail {
					return nil, errors.New("private network detail")
				}
				response := nativeResponse(tc.body)
				response.StatusCode = tc.status
				return response, nil
			})
			response, err := client.Post("https://slack.com/api/chat.postMessage", "application/json", strings.NewReader(`{}`))
			if !tc.fail {
				if err != nil {
					t.Fatal(err)
				}
				raw, _ := io.ReadAll(response.Body)
				response.Body.Close()
				if string(raw) != tc.body {
					t.Fatal("observer changed API response")
				}
			}
			status, err := p.ConnectionAuth.Status("tenant", connectionauth.WorkspaceOwner, slackProvider(c))
			if err != nil || status.ReconnectRequired != tc.rejected {
				t.Fatal(status, err)
			}
			all, err := p.Alerts.Alerts("tenant")
			if err != nil || (len(all) == 1) != tc.rejected {
				t.Fatal(all, err)
			}
			if tc.rejected {
				th, err := p.Threads.Get("tenant", all[0].Session)
				if err != nil || th.CreatedBy != "alice" || th.Visibility != threads.Private || len(all[0].Who) != 1 || all[0].Who[0] != "alice" {
					t.Fatal("native rejection not owner-private", th, all[0], err)
				}
			}
		})
	}
}

func TestSlackRejectionInspectionKeepsOversizeBody(t *testing.T) {
	body := strings.Repeat("x", 128<<10)
	r := nativeResponse(body)
	if slackResponseRejected(r) {
		t.Fatal("classified unbounded response")
	}
	got, err := io.ReadAll(r.Body)
	if err != nil || string(got) != body {
		t.Fatal("observer truncated response")
	}
}

func TestConnectionOldHTTPClientCannotRejectReconnectedGrant(t *testing.T) {
	p, err := New(Config{Workspace: t.TempDir(), ConnectionAuth: nativeConfig("drive"), PersonalHTTP: &http.Client{Transport: nativeHTTP(func(*http.Request) (*http.Response, error) {
		r := nativeResponse(`{}`)
		r.StatusCode = 401
		return r, nil
	})}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	who := auth.Principal{Tenant: "tenant", Subject: "alice", Kind: auth.User}
	authorizeNative(t, p, who, "drive", "person")
	client := p.personOAuthHTTPClient(who.Tenant, who.Subject, personctx.ToolServer{OAuth: "drive"})
	authorizeNative(t, p, who, "drive", "person") // Same provider token, different grant.
	r, _ := http.NewRequestWithContext(context.Background(), "GET", "https://service.example/mcp", nil)
	r.Header.Set("Authorization", "Bearer drive-token")
	response, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	status, err := p.ConnectionAuth.Status(who.Tenant, who.Subject, "drive")
	if err != nil || status.ReconnectRequired {
		t.Fatal("late old client rejected current grant", status, err)
	}
}
