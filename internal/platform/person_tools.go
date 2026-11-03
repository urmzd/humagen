package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/personctx"
	"github.com/urmzd/orchagent/flow/mcptools"
	"github.com/urmzd/orchagent/flow/runtime"
	"github.com/urmzd/orchagent/flow/toolprovider"
	"github.com/urmzd/orchagent/flow/turn/native"
	"github.com/urmzd/orchagent/flow/turn/native/saigetypes"
)

const personToolsGrant = "mcp:person_tools:write"

// A personal server's MCP client stays in the trusted worker. Credentials use
// the same resolver and person-scoped session option as workspace providers.
func (p *Platform) personToolResolver() mcptools.Resolver {
	if p.cfg.Ext.Resolver != nil {
		return p.cfg.Ext.Resolver
	}
	return mcptools.EnvResolver()
}

type personBearer struct {
	secret mcptools.Secret
	next   http.RoundTripper
}

func (t personBearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	if !t.secret.Empty() {
		r.Header.Set("Authorization", "Bearer "+t.secret.Reveal())
	}
	next := t.next
	if next == nil {
		next = http.DefaultTransport
	}
	return next.RoundTrip(r)
}

// DiscoverPersonTools previews a bounded tool list for the authenticated
// caller. Saving that list is the person's explicit approval.
func (p *Platform) DiscoverPersonTools(ctx context.Context, who auth.Principal, server personctx.ToolServer) ([]toolprovider.Tool, error) {
	if who.Kind == auth.Machine {
		return nil, ErrForbidden
	}
	if err := (personctx.Input{ToolServers: []personctx.ToolServer{server}}).Check(); err != nil {
		return nil, err
	}
	resolved, err := p.resolveSettings(who.Tenant, "", nil, who.Subject)
	if err != nil {
		return nil, err
	}
	if !resolved.Ceilings.AllowsToolServer(server.Host()) {
		return nil, fmt.Errorf("%w: this server's host is not allowed", ErrForbidden)
	}
	if server.Credential != "" && !p.refAllowed(who.Tenant, who.Subject, server.Credential) {
		return nil, fmt.Errorf("%w: %w", personctx.ErrInvalid, errPersonRef)
	}
	return p.discoverPersonTools(ctx, server)
}

func (p *Platform) discoverPersonTools(ctx context.Context, server personctx.ToolServer) ([]toolprovider.Tool, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var secret mcptools.Secret
	if server.Credential != "" {
		var err error
		secret, err = p.personToolResolver().Resolve(ctx, server.Credential)
		if err != nil {
			return nil, errors.New("personal tool credential could not be resolved")
		}
	}
	client := &http.Client{Transport: personBearer{secret: secret, next: p.personHTTPClient().Transport}, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "humagen", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL, HTTPClient: client, DisableStandaloneSSE: true, MaxRetries: -1, MaxEventSize: 1 << 20}, nil)
	if err != nil {
		return nil, errors.New("personal tool server could not be reached")
	}
	defer func() { _ = cs.Close() }()
	out := []toolprovider.Tool{}
	for tool, err := range cs.Tools(ctx, nil) {
		if err != nil {
			return nil, errors.New("personal tool list could not be read")
		}
		if len(out) >= 64 {
			return nil, fmt.Errorf("%w: at most 64 tools per server", personctx.ErrInvalid)
		}
		schema, err := json.Marshal(tool.InputSchema)
		if err != nil {
			return nil, personctx.ErrInvalid
		}
		t, err := toolprovider.NewTool(tool.Name, tool.Description, schema)
		if err != nil {
			return nil, personctx.ErrInvalid
		}
		t.Class = toolprovider.ClassWrite
		if tool.Annotations != nil && tool.Annotations.ReadOnlyHint {
			t.Class = toolprovider.ClassRead
		}
		out = append(out, t)
	}
	if raw, err := json.Marshal(out); err != nil || (!secret.Empty() && strings.Contains(string(raw), secret.Reveal())) {
		return nil, errors.New("personal tool list contains credential material")
	}
	slices.SortFunc(out, func(a, b toolprovider.Tool) int { return strings.Compare(a.Name, b.Name) })
	// Apply the same bound before the response is returned or pinned to a run.
	if err := (personctx.Input{ToolServers: []personctx.ToolServer{{Name: server.Name, URL: server.URL, Tools: out}}}).Check(); err != nil {
		return nil, err
	}
	return out, nil
}

func samePersonTools(a, b []toolprovider.Tool) bool {
	return slices.EqualFunc(a, b, func(a, b toolprovider.Tool) bool {
		return a.Name == b.Name && a.SchemaHash == b.SchemaHash && a.Description == b.Description && a.Class == b.Class
	})
}

func (p *Platform) personalToolGroup(ctx context.Context, req runtime.Request) (native.ToolGroup, error) {
	if !slices.Contains(req.Definition.Spec.Tools, personToolsGrant) {
		return native.ToolGroup{}, ErrForbidden
	}
	c, err := requestContext(req)
	if err != nil {
		return native.ToolGroup{}, err
	}
	if c.Person == "" {
		return native.ToolGroup{}, mcptools.ErrNoPersonCredential
	}
	out := []saigetypes.Tool{}
	for _, server := range c.ToolServers {
		if server.Tools == nil || !c.Settings.Ceilings.AllowsToolServer(server.Host()) {
			continue
		}
		// Revocation or a new approval after admission invalidates the old snapshot.
		own, err := p.PersonContexts.Get(req.Tenant, c.Person)
		if err != nil {
			return native.ToolGroup{}, err
		}
		if !slices.ContainsFunc(own.ToolServers, func(s personctx.ToolServer) bool {
			return s.Name == server.Name && s.URL == server.URL && s.Credential == server.Credential && samePersonTools(s.Tools, server.Tools)
		}) {
			continue
		}
		offered, err := p.discoverPersonTools(ctx, server)
		if err != nil {
			return native.ToolGroup{}, err
		}
		if !samePersonTools(server.Tools, offered) {
			return native.ToolGroup{}, fmt.Errorf("%w: %s tool list changed; approve it again in Customize", mcptools.ErrHeldBack, server.Name)
		}
		prov := toolprovider.Provider{Tenant: req.Tenant, Name: "person-" + server.Name, Transport: toolprovider.TransportHTTP, Endpoint: server.URL}
		opts := []mcptools.Option{mcptools.WithResolver(p.personToolResolver()), mcptools.WithHTTPClient(p.personHTTPClient())}
		if server.Credential != "" {
			prov.CredentialScope = toolprovider.ScopePerson
			opts = append(opts, mcptools.WithPersonCredential(mcptools.PersonCredential{Ref: server.Credential}))
		}
		session, err := mcptools.Connect(ctx, prov, opts...)
		if err != nil {
			return native.ToolGroup{}, err
		}
		tools, err := session.Toolset(ctx, toolprovider.Snapshot{Tenant: req.Tenant, Provider: prov.Name, Tools: server.Tools}, toolprovider.GrantWrite)
		if err != nil {
			_ = session.Close()
			return native.ToolGroup{}, err
		}
		go func() { <-ctx.Done(); _ = session.Close() }()
		for _, tool := range tools.All() {
			out = append(out, &personalTool{Tool: tool, p: p, req: req, server: server, person: c.Person})
		}
	}
	return native.ToolGroup{Name: personToolsGrant, Tools: out}, nil
}

type personalTool struct {
	saigetypes.Tool
	p      *Platform
	req    runtime.Request
	server personctx.ToolServer
	person string
}

func (t *personalTool) ActsForPerson() bool { return true }
func (t *personalTool) Approval(_ saigetypes.ToolCallPart) (native.ApprovalRequest, bool) {
	if t.Definition().Capability == saigetypes.ToolCapabilityRead {
		return native.ApprovalRequest{}, false
	}
	return native.ApprovalRequest{Prompt: fmt.Sprintf("Allow %s to use %s?", t.req.Agent(), t.Definition().Name), Approvers: []string{t.person}, SelfApproval: true, ExpiresIn: 24 * time.Hour}, true
}
func (t *personalTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	own, err := t.p.PersonContexts.Get(t.req.Tenant, t.person)
	if err != nil {
		return "", err
	}
	if !slices.ContainsFunc(own.ToolServers, func(s personctx.ToolServer) bool {
		return s.Name == t.server.Name && s.URL == t.server.URL && s.Credential == t.server.Credential && samePersonTools(s.Tools, t.server.Tools)
	}) {
		return "", mcptools.ErrHeldBack
	}
	offered, err := t.p.discoverPersonTools(ctx, t.server)
	if err != nil {
		return "", err
	}
	if !samePersonTools(offered, t.server.Tools) {
		return "", mcptools.ErrHeldBack
	}
	return t.Tool.Execute(ctx, args)
}

func (p *Platform) personHTTPClient() *http.Client {
	base := http.DefaultClient
	if p.personalHTTP != nil {
		base = p.personalHTTP
	}
	client := *base
	client.Timeout = 30 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &client
}
