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
	if len(all) != 9 {
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
