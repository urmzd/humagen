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
	"github.com/urmzd/humagen/internal/mcptools"
	"github.com/urmzd/humagen/internal/toolprovider"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/personctx"
	saigetypes "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/runtime"
	"github.com/urmzd/saige/flow/turn/native"
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
		return nil, fmt.Errorf("%w: %s", ErrForbidden, p.toolHostRefusal(resolved.Ceilings.ToolServers))
	}
	if server.Credential != "" && !p.refAllowed(who.Tenant, who.Subject, server.Credential) {
		return nil, fmt.Errorf("%w: %w", personctx.ErrInvalid, errPersonRef)
	}
	return p.discoverPersonToolsFor(ctx, who.Tenant, who.Subject, server)
}

func (p *Platform) discoverPersonToolsFor(ctx context.Context, tenant, person string, server personctx.ToolServer) ([]toolprovider.Tool, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	// Snapshot consent before resolving the bearer used by discovery.
	oauthClient := p.personOAuthHTTPClient(tenant, person, server)
	var secret mcptools.Secret
	if server.OAuth != "" {
		var err error
		secret, err = p.personalOAuth(ctx, tenant, person, server)
		if err != nil {
			return nil, err
		}
	}
	if server.Credential != "" {
		var err error
		secret, err = p.personToolResolver().Resolve(ctx, server.Credential)
		if err != nil {
			return nil, errors.New("personal tool credential could not be resolved")
		}
	}
	client := &http.Client{Transport: personBearer{secret: secret, next: oauthClient.Transport}, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "humagen", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL, HTTPClient: client, DisableStandaloneSSE: true, MaxRetries: -1, MaxEventSize: 1 << 20}, nil)
	if err != nil {
		return nil, errors.New("your MCP could not be reached")
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
			return s.Name == server.Name && s.URL == server.URL && s.Credential == server.Credential && s.OAuth == server.OAuth && samePersonTools(s.Tools, server.Tools)
		}) {
			continue
		}
		offered, err := p.discoverPersonToolsFor(ctx, req.Tenant, c.Person, server)
		if err != nil {
			return native.ToolGroup{}, err
		}
		if !samePersonTools(server.Tools, offered) {
			return native.ToolGroup{}, fmt.Errorf("%w: %s tool list changed; approve it again in Customize", mcptools.ErrHeldBack, server.Name)
		}
		prov := toolprovider.Provider{Tenant: req.Tenant, Name: "person-" + server.Name, Transport: toolprovider.TransportHTTP, Endpoint: server.URL}
		opts := []mcptools.Option{mcptools.WithResolver(p.personToolResolver()), mcptools.WithHTTPClient(p.personOAuthHTTPClient(req.Tenant, c.Person, server))}
		if server.OAuth != "" {
			prov.CredentialScope = toolprovider.ScopePerson
			source := mcptools.NewTokenSource(func(ctx context.Context) (mcptools.Secret, error) {
				return p.personalOAuth(ctx, req.Tenant, c.Person, server)
			})
			opts = append(opts, mcptools.WithPersonCredential(mcptools.PersonCredential{Source: source}))
		}
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
			out = append(out, &personServerTool{Tool: tool, p: p, req: req, server: server, person: c.Person})
		}
	}
	return native.ToolGroup{Name: personToolsGrant, Tools: out}, nil
}

// personServerTool is one tool of a server a person connected, checked
// against what the person approved each time it runs. What it declares
// (declaration) is read by the one wrapper every run tool passes through.
type personServerTool struct {
	saigetypes.Tool
	p      *Platform
	req    runtime.Request
	server personctx.ToolServer
	person string
}

// declaration is what a personal tool declares, as a registry tool would: it
// acts with the person's own account, and a call that is not a read waits
// for that person, who may be the one asking, for a day.
func (t *personServerTool) declaration() SystemTool {
	d := SystemTool{Name: t.Definition().Name, ActsForPerson: true}
	if t.Definition().Capability != saigetypes.ToolCapabilityRead {
		d.Approval = ToolApproval{Mode: ApprovalAlways, Approvers: []string{t.person}, SelfApproval: true, Expires: 24 * time.Hour, Prompt: allowPrompt}
	}
	return d
}
func (t *personServerTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	own, err := t.p.PersonContexts.Get(t.req.Tenant, t.person)
	if err != nil {
		return "", err
	}
	if !slices.ContainsFunc(own.ToolServers, func(s personctx.ToolServer) bool {
		return s.Name == t.server.Name && s.URL == t.server.URL && s.Credential == t.server.Credential && s.OAuth == t.server.OAuth && samePersonTools(s.Tools, t.server.Tools)
	}) {
		return "", mcptools.ErrHeldBack
	}
	offered, err := t.p.discoverPersonToolsFor(ctx, t.req.Tenant, t.person, t.server)
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
	if p.cfg.PersonalHTTP != nil {
		base = p.cfg.PersonalHTTP
	}
	client := *base
	client.Timeout = 30 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &client
}
