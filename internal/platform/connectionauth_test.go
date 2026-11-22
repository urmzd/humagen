package platform

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/connectionauth"
	"github.com/urmzd/humagen/pkg/personctx"
	runsettings "github.com/urmzd/humagen/pkg/settings"
)

func TestConnectionPresetsAndHostRefusalsAreActionable(t *testing.T) {
	p, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	who := auth.Principal{Tenant: "acme", Subject: "alice"}
	all, err := p.ConnectionPresets(who)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 18 {
		t.Fatalf("presets=%d", len(all))
	}
	for _, c := range all {
		if c.Kind == "oauth" && (c.Available || c.Reason == "" || c.Connected) {
			t.Fatalf("unconfigured service claimed ready: %+v", c)
		}
	}
	_, err = p.DiscoverPersonTools(context.Background(), who, personctx.ToolServer{Name: "notes", URL: "https://notes.example.com/mcp"})
	if !errors.Is(err, ErrForbidden) || !strings.Contains(err.Error(), "No MCP hosts are allowed") {
		t.Fatal(err)
	}
	if err = p.SetRunSettings("acme", runsettings.Layer{Ceilings: runsettings.Ceilings{ToolServers: []string{"allowed.example.com"}}}, nil); err != nil {
		t.Fatal(err)
	}
	_, err = p.DiscoverPersonTools(context.Background(), who, personctx.ToolServer{Name: "notes", URL: "https://notes.example.com/mcp"})
	if !errors.Is(err, ErrForbidden) || !strings.Contains(err.Error(), "allowed.example.com") {
		t.Fatal(err)
	}
}
func TestPersonalOAuthCannotSendItsTokenToAnotherEndpoint(t *testing.T) {
	p, err := New(Config{Workspace: t.TempDir(), ConnectionAuth: &connectionauth.Config{Key: []byte(strings.Repeat("k", 32)), RedirectURL: "https://humagen.example.com/connections/callback", Providers: DefaultConnectionProviders()}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	who := auth.Principal{Tenant: "acme", Subject: "alice"}
	for _, server := range []personctx.ToolServer{
		{Name: "notion", URL: "https://attacker.example.com/mcp", OAuth: "notion"},
		{Name: "notion", URL: "https://mcp.notion.com/mcp", OAuth: "unknown"},
		{Name: "notion", URL: "https://mcp.notion.com/mcp", OAuth: "notion", Credential: "env:HUMAGEN_SECRET"},
	} {
		if _, err = p.SetPersonContext(context.Background(), who, personctx.Input{ToolServers: []personctx.ToolServer{server}}); !errors.Is(err, personctx.ErrInvalid) {
			t.Fatalf("%+v: %v", server, err)
		}
	}
	if _, err = p.BeginConnection(context.Background(), who, "notion"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("host grant bypassed: %v", err)
	}
}

// The Google Workspace tool servers sign in through the deployment's one
// Google application: without it they are not offered and say what is
// missing, and with it each is a provider of its own, asked only for scopes
// its server supports.
func TestGoogleWorkspaceToolServersNeedTheGoogleClient(t *testing.T) {
	want := map[string]string{
		"gmail":           "https://gmailmcp.googleapis.com/mcp/v1",
		"google-calendar": "https://calendarmcp.googleapis.com/mcp/v1",
		"google-drive":    "https://drivemcp.googleapis.com/mcp/v1",
		"google-docs":     "https://docsmcp.googleapis.com/mcp/v1",
		"google-sheets":   "https://sheetsmcp.googleapis.com/mcp/v1",
		"google-slides":   "https://slidesmcp.googleapis.com/mcp/v1",
	}
	for _, d := range DefaultConnectionProviders() {
		if _, google := want[d.Name]; google {
			t.Errorf("%s is offered without a registered Google application", d.Name)
		}
	}
	providers := GoogleToolProviders()
	if len(providers) != len(want) {
		t.Fatalf("providers = %d, want %d", len(providers), len(want))
	}
	for _, g := range providers {
		if g.Resource != want[g.Name] || g.Issuer != "https://accounts.google.com" || g.AuthorizationURL == "" || g.TokenURL == "" || !g.Offline || len(g.Scopes) == 0 {
			t.Errorf("provider = %+v", g)
		}
		for _, scope := range g.Scopes {
			if !strings.HasPrefix(scope, "https://www.googleapis.com/auth/") {
				t.Errorf("%s asks for %q", g.Name, scope)
			}
		}
		if name, ok := connectionAlertName(g.Name); !ok || name == "" {
			t.Errorf("%s has no name to show", g.Name)
		}
	}
	if name, ok := connectionAlertName("atlassian"); !ok || name != "Atlassian" {
		t.Errorf("atlassian is shown as %q", name)
	}

	who := auth.Principal{Tenant: "acme", Subject: "alice"}
	reasons := func(cfg *connectionauth.Config) map[string]ConnectionPreset {
		p, err := New(Config{Workspace: t.TempDir(), ConnectionAuth: cfg})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = p.Close() })
		all, err := p.ConnectionPresets(who)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]ConnectionPreset{}
		for _, c := range all {
			out[c.ID] = c
		}
		return out
	}
	key := []byte(strings.Repeat("k", 32))
	without := reasons(&connectionauth.Config{Key: key, RedirectURL: "https://humagen.example.com/connections/callback", Providers: DefaultConnectionProviders()})
	for id := range want {
		if c := without[id]; c.Available || !strings.Contains(c.Reason, "connections.clients.google") {
			t.Errorf("%s without a Google application: %+v", id, c)
		}
	}
	registered := GoogleToolProviders()
	for i := range registered {
		registered[i].ClientID, registered[i].ClientSecret = "registered", "secret"
	}
	with := reasons(&connectionauth.Config{Key: key, RedirectURL: "https://humagen.example.com/connections/callback", Providers: append(DefaultConnectionProviders(), registered...)})
	for id := range want {
		// Registered, it is held back only by the workspace's host ceiling.
		if c := with[id]; c.Available || strings.Contains(c.Reason, "connections.clients.google") || c.Reason == "" {
			t.Errorf("%s with a Google application: %+v", id, c)
		}
	}
}
