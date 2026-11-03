package platform

import (
	"context"
	"errors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	runsettings "github.com/urmzd/humagen/pkg/settings"
	"github.com/urmzd/orchagent/flow/mcptools"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/personctx"
	"github.com/urmzd/humagen/pkg/runctx"
	"github.com/urmzd/orchagent/flow/toolprovider"
	"github.com/urmzd/orchagent/flow/turn/native/saigetypes"
)

func TestPersonToolsRequireExplicitGrant(t *testing.T) {
	req := requestWith(t, runctx.Context{})
	out, err := boundRunContext(req)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range out.Definition.Spec.Tools {
		if name == personToolsGrant {
			t.Fatal("personal tools granted implicitly")
		}
	}
	req.Definition.Spec.Tools = []string{"person_tools"}
	out, err = boundRunContext(req)
	if err != nil || len(out.Definition.Spec.Tools) != 1 || out.Definition.Spec.Tools[0] != personToolsGrant {
		t.Fatalf("explicit grant: %+v %v", out.Definition.Spec.Tools, err)
	}
}

func TestPersonToolSnapshotsIncludeClasses(t *testing.T) {
	a, _ := toolprovider.NewTool("lookup", "Look up notes", nil)
	a.Class = toolprovider.ClassRead
	b := a
	b.Class = toolprovider.ClassWrite
	if samePersonTools([]toolprovider.Tool{a}, []toolprovider.Tool{b}) {
		t.Fatal("changed readOnlyHint kept approval")
	}
	if samePersonTools([]toolprovider.Tool{a}, nil) {
		t.Fatal("removed tool kept approval")
	}
	b = a
	b.Description = "New behavior"
	if samePersonTools([]toolprovider.Tool{a}, []toolprovider.Tool{b}) {
		t.Fatal("changed description kept approval")
	}
}

func TestPersonalToolDiscoveryDefaultDeny(t *testing.T) {
	p, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	server := personctx.ToolServer{Name: "notes", URL: "https://mcp.example.com"}
	for _, who := range []auth.Principal{{Tenant: "acme", Subject: "ana"}, {Tenant: "other", Subject: "ana"}, {Tenant: "acme", Subject: "machine", Kind: auth.Machine}} {
		if _, err := p.DiscoverPersonTools(context.Background(), who, server); !errors.Is(err, ErrForbidden) {
			t.Fatalf("%+v: %v", who, err)
		}
	}
}

func TestPersonalToolListChangeAndWriteApproval(t *testing.T) {
	// A hint is part of the person's approval even though upstream MCP schema
	// matching deliberately leaves server annotations out of schema hashes.
	read, _ := toolprovider.NewTool("lookup", "Read notes", nil)
	read.Class = toolprovider.ClassRead
	write := read
	write.Class = toolprovider.ClassWrite
	tool := &personalTool{Tool: &fixturePersonTool{capability: saigetypes.ToolCapabilityWrite}, person: "ana"}
	approval, needed := tool.Approval(saigetypes.ToolCallPart{})
	if !needed || len(approval.Approvers) != 1 || approval.Approvers[0] != "ana" || !approval.SelfApproval {
		t.Fatalf("write approval: %+v %v", approval, needed)
	}
	tool.Tool = &fixturePersonTool{capability: saigetypes.ToolCapabilityRead}
	if _, needed := tool.Approval(saigetypes.ToolCallPart{}); needed {
		t.Fatal("read hint requested write approval")
	}
	if samePersonTools([]toolprovider.Tool{read}, []toolprovider.Tool{write}) {
		t.Fatal("changed hint kept approval")
	}
}

type fixturePersonTool struct{ capability saigetypes.ToolCapability }

func (t *fixturePersonTool) Definition() saigetypes.ToolDef {
	return saigetypes.ToolDef{Name: "lookup", Capability: t.capability}
}
func (t *fixturePersonTool) Execute(context.Context, map[string]any) (string, error) { return "", nil }

func TestPersonalToolsTrustedWorkerSnapshotsAndRevocation(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "notes", Version: "1"}, nil)
	server.AddTool(&mcp.Tool{Name: "lookup", Description: "Read notes", InputSchema: map[string]any{"type": "object"}, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "a note"}}}, nil
	})
	httpServer := httptest.NewTLSServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil))
	defer httpServer.Close()

	p, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	p.personalHTTP = httpServer.Client()
	host := personctx.ToolServer{Name: "notes", URL: httpServer.URL}
	if err := p.SetRunSettings("acme", runsettings.Layer{Ceilings: runsettings.Ceilings{ToolServers: []string{host.Host()}}}, nil); err != nil {
		t.Fatal(err)
	}
	who := auth.Principal{Tenant: "acme", Subject: "ana"}
	tools, err := p.DiscoverPersonTools(context.Background(), who, host)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Class != toolprovider.ClassRead {
		t.Fatalf("tools: %+v", tools)
	}
	host.Tools = tools
	own, err := p.SetPersonContext(context.Background(), who, personctx.Input{ToolServers: []personctx.ToolServer{host}})
	if err != nil {
		t.Fatal(err)
	}
	c, err := p.newRunContext("acme", "", nil, "ana")
	if err != nil {
		t.Fatal(err)
	}
	req := requestWith(t, c)
	req.Tenant = "acme"
	req.Definition.Spec.Tools = []string{personToolsGrant}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	group, err := p.personalToolGroup(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if len(group.Tools) != 1 {
		t.Fatalf("group: %+v", group)
	}
	text, err := group.Tools[0].Execute(ctx, nil)
	if err != nil || !strings.Contains(text, "a note") {
		t.Fatalf("execute: %q %v", text, err)
	}
	// Adding a write tool invalidates the entire list, including held reads.
	server.AddTool(&mcp.Tool{Name: "write", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{}, nil
	})
	if _, err := group.Tools[0].Execute(ctx, nil); !errors.Is(err, mcptools.ErrHeldBack) {
		t.Fatalf("changed list: %v", err)
	}
	changed, err := p.DiscoverPersonTools(ctx, who, host)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 2 || changed[1].Class != toolprovider.ClassWrite {
		t.Fatalf("write defaults: %+v", changed)
	}
	_, err = p.SetPersonContext(ctx, who, personctx.Input{Version: own.Version})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := group.Tools[0].Execute(ctx, nil); !errors.Is(err, mcptools.ErrHeldBack) {
		t.Fatalf("revoked server: %v", err)
	}
}
