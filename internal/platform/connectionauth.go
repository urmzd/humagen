package platform

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/urmzd/humagen/internal/mcptools"
	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/connectionauth"
	"github.com/urmzd/humagen/pkg/personctx"
)

// ConnectionPreset lists a service and the next available connection action.
// Availability never implies that credentials have been verified.
type ConnectionPreset struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	Kind              string            `json:"kind"`
	Endpoint          string            `json:"endpoint,omitempty"`
	Available         bool              `json:"available"`
	ReconnectRequired bool              `json:"reconnect_required,omitempty"`
	Connected         bool              `json:"connected"`
	Reason            string            `json:"reason,omitempty"`
	Scope             string            `json:"scope"`
	GrantedScopes     *[]string         `json:"granted_scopes,omitempty"`
	Health            *ConnectionHealth `json:"health,omitempty"`
}

var connectionPresets = []ConnectionPreset{
	{ID: "slack", Name: "Slack", Kind: "client_oauth", Endpoint: "https://slack.com/api/", Scope: "workspace"},
	{ID: "github", Name: "GitHub", Kind: "github_install", Scope: "workspace"},
	{ID: "notion", Name: "Notion", Kind: "oauth", Endpoint: "https://mcp.notion.com/mcp", Scope: "person"},
	{ID: "linear", Name: "Linear", Kind: "oauth", Endpoint: "https://mcp.linear.app/mcp", Scope: "person"},
	{ID: "granola", Name: "Granola", Kind: "oauth", Endpoint: "https://mcp.granola.ai/mcp", Scope: "person"},
	{ID: "drive", Name: "Google Drive", Kind: "source_oauth", Endpoint: "https://www.googleapis.com/drive/v3/", Scope: "person"},
	{ID: "bigquery", Name: "BigQuery", Kind: "catalog_oauth", Endpoint: "https://bigquery.googleapis.com/bigquery/v2/", Scope: "person"},
	{ID: "other", Name: "Other MCP", Kind: "tools", Scope: "person"},
}

// DefaultConnectionProviders supplies stable service endpoints. Configuration
// can add pre-registered OAuth clients; a preset never broadens host grants.
func DefaultConnectionProviders() []connectionauth.Provider {
	out := []connectionauth.Provider{}
	for _, p := range connectionPresets {
		if p.Kind == "oauth" {
			out = append(out, connectionauth.Provider{Name: p.ID, Resource: p.Endpoint})
		}
	}
	return out
}
func preset(name string) (ConnectionPreset, bool) {
	for _, p := range connectionPresets {
		if p.ID == name {
			return p, true
		}
	}
	return ConnectionPreset{}, false
}
func (p *Platform) ConnectionPresets(who auth.Principal) ([]ConnectionPreset, error) {
	resolved, err := p.resolveSettings(who.Tenant, "", nil, who.Subject)
	if err != nil {
		return nil, err
	}
	out := slices.Clone(connectionPresets)
	for i := range out {
		c := &out[i]
		if c.ID == "github" {
			c.Available = p.GitHubInstall.Available() && who.Kind != auth.Machine
			if !c.Available {
				c.Reason = "An administrator must configure the GitHub App before installation."
			}
			if who.Can(auth.ClientsRead) {
				held, err := p.Clients.List(who.Tenant)
				if err != nil {
					return nil, err
				}
				for _, client := range held {
					if client.Surface == clients.GitHub && client.Managed == clients.ManagedInstall {
						c.Connected = true
					}
				}
			}
			continue
		}
		c.Available = true
		if c.ID == "other" && p.ConnectionAuth == nil {
			c.Available = false
			c.Reason = "An administrator must enable secure account connections before using OAuth. Credential-based tool connections remain available below."
		}
		if c.Kind != "oauth" && c.Kind != "source_oauth" && c.Kind != "client_oauth" && c.Kind != "catalog_oauth" {
			continue
		}
		c.Available = false
		if who.Kind == auth.Machine {
			c.Reason = "Sign in as a person to connect your account."
			continue
		}
		if !p.ConnectionAuth.Available(c.ID) {
			c.Reason = "An administrator must enable secure account connections for this workspace."
			continue
		}
		if c.Kind == "oauth" && !resolved.Ceilings.AllowsToolServer((personctx.ToolServer{URL: c.Endpoint}).Host()) {
			c.Reason = p.toolHostRefusal(resolved.Ceilings.ToolServers)
			continue
		}
		owner, provider := who.Subject, c.ID
		if c.Kind == "client_oauth" {
			owner, provider = connectionauth.WorkspaceOwner, "workspace-"+c.ID
		}
		status, err := p.ConnectionAuth.Status(who.Tenant, owner, provider)
		if err != nil {
			return nil, err
		}
		c.Available = true
		if c.Kind == "catalog_oauth" {
			c.Reason = "Read-only metadata sync. This Google consent does not enable SQL queries."
		}
		c.Connected = status.Connected
		c.ReconnectRequired = status.ReconnectRequired
		c.GrantedScopes = status.GrantedScopes
		if c.ID == "slack" {
			held, err := p.ConnectionAuth.Connections(who.Tenant, connectionauth.WorkspaceOwner)
			if err != nil {
				return nil, err
			}
			scopes := []string{}
			known := true
			for _, item := range held {
				if strings.HasPrefix(item.Provider, "workspace-slack-") {
					c.ReconnectRequired = c.ReconnectRequired || item.ReconnectRequired
					if !item.Connected {
						continue
					}
					c.Connected = true
					if item.GrantedScopes == nil {
						known = false
					} else {
						for _, scope := range *item.GrantedScopes {
							if !slices.Contains(scopes, scope) {
								scopes = append(scopes, scope)
							}
						}
					}
				}
			}
			if known && c.Connected {
				c.GrantedScopes = &scopes
			}
		}
	}
	if p.ConnectionAuth != nil && who.Kind != auth.Machine {
		for _, owner := range []string{who.Subject, connectionauth.WorkspaceOwner} {
			held, err := p.ConnectionAuth.Connections(who.Tenant, owner)
			if err != nil {
				return nil, err
			}
			for _, item := range held {
				if strings.HasPrefix(item.Provider, "workspace-slack") {
					continue
				}
				shared := owner == connectionauth.WorkspaceOwner
				if !shared && !strings.HasPrefix(item.Provider, "other-") {
					continue
				}
				base := strings.TrimPrefix(item.Provider, "workspace-")
				name := "Other MCP"
				if preset, ok := preset(base); ok {
					name = preset.Name
				}
				scope := "person"
				if shared {
					scope = "workspace"
				}
				allowed := resolved.Ceilings.AllowsToolServer((personctx.ToolServer{URL: item.Resource}).Host())
				reason := ""
				if !allowed {
					reason = p.toolHostRefusal(resolved.Ceilings.ToolServers)
				}
				out = append(out, ConnectionPreset{ID: item.Provider, Name: name, Kind: "oauth", Endpoint: item.Resource, Available: allowed, Connected: item.Connected, Reason: reason, Scope: scope, GrantedScopes: item.GrantedScopes, ReconnectRequired: item.ReconnectRequired})
			}
		}
	}
	for i := range out {
		if out[i].Connected || out[i].ReconnectRequired {
			out[i].Health, err = p.lastConnectionHealth(who, out[i].ID, false)
			if err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}
func (p *Platform) toolHostRefusal(hosts []string) string {
	if len(hosts) == 0 {
		return "No MCP hosts are allowed yet. Ask a workspace administrator to allow this service in Limits."
	}
	return "Allowed MCP hosts: " + strings.Join(hosts, ", ") + ". Ask an administrator to allow another host."
}

// ConnectionAuthorizationInput describes consent requested by the person. The
// scope is explicit; the platform supplies and authorizes the credential owner.
type ConnectionAuthorizationInput struct {
	Endpoint     string                       `json:"endpoint,omitempty"`
	Scope        string                       `json:"scope,omitempty"`
	Registration *connectionauth.Registration `json:"registration,omitempty"`
}

func connectionOwner(who auth.Principal, name string) string {
	if strings.HasPrefix(name, "workspace-") {
		return connectionauth.WorkspaceOwner
	}
	return who.Subject
}
func (p *Platform) connectionAllowed(who auth.Principal, name string) (ConnectionPreset, error) {
	if who.Kind == auth.Machine {
		return ConnectionPreset{}, ErrForbidden
	}
	base := strings.TrimPrefix(name, "workspace-")
	c, ok := preset(base)
	if strings.HasPrefix(base, "other-") {
		endpoint, err := p.ConnectionAuth.Resource(who.Tenant, connectionOwner(who, name), name)
		if err != nil {
			return c, err
		}
		c = ConnectionPreset{ID: name, Name: "Other MCP", Kind: "oauth", Endpoint: endpoint, Scope: "person"}
		ok = true
	}
	if !ok || (c.Kind != "oauth" && c.Kind != "source_oauth" && c.Kind != "client_oauth" && c.Kind != "catalog_oauth") {
		return c, ErrProviderNotFound
	}
	resolved, err := p.resolveSettings(who.Tenant, "", nil, who.Subject)
	if err != nil {
		return c, err
	}
	if c.Kind == "oauth" && !resolved.Ceilings.AllowsToolServer((personctx.ToolServer{URL: c.Endpoint}).Host()) {
		return c, fmt.Errorf("%w: %s", ErrForbidden, p.toolHostRefusal(resolved.Ceilings.ToolServers))
	}
	if !p.ConnectionAuth.Available(name) {
		return c, connectionauth.ErrUnavailable
	}
	return c, nil
}
func (p *Platform) BeginConnection(ctx context.Context, who auth.Principal, name string) (connectionauth.Authorization, error) {
	return p.AuthorizeConnection(ctx, who, name, ConnectionAuthorizationInput{})
}
func (p *Platform) AuthorizeConnection(ctx context.Context, who auth.Principal, name string, in ConnectionAuthorizationInput) (connectionauth.Authorization, error) {
	if who.Kind == auth.Machine {
		return connectionauth.Authorization{}, ErrForbidden
	}
	if in.Scope != "" && in.Scope != "person" && in.Scope != "workspace" {
		return connectionauth.Authorization{}, personctx.ErrInvalid
	}
	shared := in.Scope == "workspace" || strings.HasPrefix(name, "workspace-")
	if shared && !who.Can(auth.ClientsWrite) {
		return connectionauth.Authorization{}, ErrForbidden
	}
	base := strings.TrimPrefix(name, "workspace-")
	if in.Registration != nil && base != "other" && !strings.HasPrefix(base, "other-") {
		return connectionauth.Authorization{}, personctx.ErrInvalid
	}
	if base == "github" {
		if name != "github" || !shared || in.Endpoint != "" {
			return connectionauth.Authorization{}, personctx.ErrInvalid
		}
		return p.BeginGitHubInstall(ctx, who)
	}
	if base == "slack" && (!shared || !who.Can(auth.ClientsWrite)) {
		return connectionauth.Authorization{}, ErrForbidden
	}
	if (base == "drive" || base == "bigquery") && shared {
		return connectionauth.Authorization{}, personctx.ErrInvalid
	}
	resource := in.Endpoint
	if base == "other" || strings.HasPrefix(base, "other-") {
		if resource == "" {
			var err error
			resource, err = p.ConnectionAuth.Resource(who.Tenant, connectionOwner(who, name), name)
			if err != nil {
				return connectionauth.Authorization{}, err
			}
		}
		if base != "other" && base != connectionauth.OtherProvider(resource) {
			return connectionauth.Authorization{}, personctx.ErrInvalid
		}
		resolved, err := p.resolveSettings(who.Tenant, "", nil, who.Subject)
		if err != nil {
			return connectionauth.Authorization{}, err
		}
		if !resolved.Ceilings.AllowsToolServer((personctx.ToolServer{URL: resource}).Host()) {
			return connectionauth.Authorization{}, fmt.Errorf("%w: %s", ErrForbidden, p.toolHostRefusal(resolved.Ceilings.ToolServers))
		}
	} else {
		c, err := p.connectionAllowed(who, base)
		if err != nil {
			return connectionauth.Authorization{}, err
		}
		if resource != "" && resource != c.Endpoint {
			return connectionauth.Authorization{}, personctx.ErrInvalid
		}
		resource = c.Endpoint
	}
	owner := who.Subject
	if shared {
		owner = connectionauth.WorkspaceOwner
	}
	attempt, err := p.BeginAudit(who, audit.Change{Action: "connection.authorize", Resource: "connection", ID: name, Subject: who.Subject, Route: "POST /api/connections/{provider}/authorize", Rule: string(auth.RunsWrite), After: map[string]string{"scope": in.Scope, "endpoint": resource}})
	if err != nil {
		return connectionauth.Authorization{}, err
	}
	out, err := p.ConnectionAuth.BeginRegistered(ctx, who.Tenant, who.Subject, owner, base, resource, in.Registration)
	if completion := attempt.Finish(err); completion != nil {
		p.LogAuditCompletionFailure(completion)
	}
	return out, err
}
func (p *Platform) CompleteConnection(ctx context.Context, who auth.Principal, name, nonce, code string) error {
	if who.Kind == auth.Machine {
		return ErrForbidden
	}
	base := strings.TrimPrefix(name, "workspace-")
	c, known := preset(base)
	if !known && !strings.HasPrefix(base, "other-") {
		return ErrProviderNotFound
	}
	if (base == "drive" || base == "bigquery") && strings.HasPrefix(name, "workspace-") {
		return personctx.ErrInvalid
	}
	owner := connectionOwner(who, name)
	if owner == connectionauth.WorkspaceOwner && !who.Can(auth.ClientsWrite) {
		return ErrForbidden
	}
	endpoint, err := p.ConnectionAuth.PendingResource(who.Tenant, who.Subject, owner, name, nonce)
	if err != nil {
		return err
	}
	resolved, err := p.resolveSettings(who.Tenant, "", nil, who.Subject)
	if err != nil {
		return err
	}
	if c.Kind != "source_oauth" && c.Kind != "client_oauth" && c.Kind != "catalog_oauth" && !resolved.Ceilings.AllowsToolServer((personctx.ToolServer{URL: endpoint}).Host()) {
		return fmt.Errorf("%w: %s", ErrForbidden, p.toolHostRefusal(resolved.Ceilings.ToolServers))
	}
	attempt, err := p.BeginAudit(who, audit.Change{Action: "connection.connect", Resource: "connection", ID: name, Subject: who.Subject, Route: "POST /api/connections/{provider}/callback", Rule: string(auth.RunsWrite)})
	if err != nil {
		return err
	}
	err = p.ConnectionAuth.CompleteFor(ctx, who.Tenant, who.Subject, owner, name, nonce, code)
	if err == nil && base == "slack" {
		err = p.finishSlackInstall(ctx, who)
	}
	if completion := attempt.Finish(err); completion != nil {
		p.LogAuditCompletionFailure(completion)
	}
	return err
}
func (p *Platform) DisconnectConnection(who auth.Principal, name string) error {
	if who.Kind == auth.Machine {
		return ErrForbidden
	}
	if name == "github" {
		return p.DisconnectGitHubInstall(context.Background(), who)
	}
	owner := connectionOwner(who, name)
	if name == "slack" {
		name = "workspace-slack"
		owner = connectionauth.WorkspaceOwner
	}
	if owner == connectionauth.WorkspaceOwner && !who.Can(auth.ClientsWrite) {
		return ErrForbidden
	}
	base := strings.TrimPrefix(name, "workspace-")
	if _, ok := preset(base); !ok && !strings.HasPrefix(base, "other-") {
		return ErrProviderNotFound
	}
	attempt, err := p.BeginAudit(who, audit.Change{Action: "connection.disconnect", Resource: "connection", ID: name, Subject: who.Subject, Route: "DELETE /api/connections/{provider}", Rule: string(auth.RunsWrite)})
	if err != nil {
		return err
	}
	if base == "slack" {
		err = p.disableSlackInstall(who.Tenant)
	}
	if err == nil {
		err = p.ConnectionAuth.Disconnect(who.Tenant, owner, name)
	}
	if completion := attempt.Finish(err); completion != nil {
		p.LogAuditCompletionFailure(completion)
	}
	return err
}
func (p *Platform) oauthResource(tenant, person, name string) (string, error) {
	owner := person
	if strings.HasPrefix(name, "workspace-") {
		owner = connectionauth.WorkspaceOwner
	}
	if strings.HasPrefix(strings.TrimPrefix(name, "workspace-"), "other-") {
		return p.ConnectionAuth.Resource(tenant, owner, name)
	}
	c, ok := preset(strings.TrimPrefix(name, "workspace-"))
	if !ok || c.Kind != "oauth" {
		return "", personctx.ErrInvalid
	}
	return c.Endpoint, nil
}
func (p *Platform) personalOAuth(ctx context.Context, tenant, person string, server personctx.ToolServer) (mcptools.Secret, error) {
	endpoint, err := p.oauthResource(tenant, person, server.OAuth)
	if err != nil || server.URL != endpoint || server.Credential != "" {
		return mcptools.Secret{}, fmt.Errorf("%w: OAuth connection does not match this server", personctx.ErrInvalid)
	}
	owner := person
	if strings.HasPrefix(server.OAuth, "workspace-") {
		owner = connectionauth.WorkspaceOwner
	}
	value, err := p.ConnectionAuth.Token(ctx, tenant, owner, server.OAuth)
	if err != nil {
		return mcptools.Secret{}, err
	}
	return mcptools.NewSecret(value), nil
}
