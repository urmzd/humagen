package platform

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/connectionauth"
	"github.com/urmzd/humagen/pkg/integrations/github"
	"github.com/urmzd/humagen/pkg/personctx"
)

type ConnectionHealth struct {
	Healthy   bool      `json:"healthy"`
	CheckedAt time.Time `json:"checked_at"`
	Message   string    `json:"message"`
}

func (p *Platform) healthResult(err error) ConnectionHealth {
	out := ConnectionHealth{Healthy: err == nil, CheckedAt: p.now(), Message: "Access verified. This check does not verify inbound webhooks or future requests."}
	if err != nil {
		out.Message = "Access could not be verified. Reconnect the account, check its permissions, then try again."
	}
	return out
}

func (p *Platform) CheckConnection(ctx context.Context, who auth.Principal, name string) (out ConnectionHealth, err error) {
	revision := p.connectionHealthRevision(who, name, false)
	defer func() {
		if err == nil {
			err = p.saveConnectionHealth(who, name, false, revision, out)
		}
	}()
	if who.Kind == auth.Machine {
		return ConnectionHealth{}, ErrForbidden
	}
	if name == "github" || name == "slack" || name == "workspace-slack" {
		if !who.Can(auth.ClientsWrite) {
			return ConnectionHealth{}, ErrForbidden
		}
		all, err := p.Clients.List(who.Tenant)
		if err != nil {
			return ConnectionHealth{}, err
		}
		checked := false
		for _, c := range all {
			if string(c.Surface) == strings.TrimPrefix(name, "workspace-") && c.Managed == clients.ManagedInstall {
				checked = true
				result, err := p.CheckClientConnection(ctx, who, c.ID)
				if err != nil || !result.Healthy {
					return result, err
				}
			}
		}
		if !checked {
			return p.healthResult(connectionauth.ErrReconnect), nil
		}
		return p.healthResult(nil), nil
	}
	base := strings.TrimPrefix(name, "workspace-")
	if base == "slack" {
		if !who.Can(auth.ClientsWrite) {
			return ConnectionHealth{}, ErrForbidden
		}
		name = "workspace-slack"
	}
	c, err := p.connectionAllowed(who, name)
	if err != nil {
		return ConnectionHealth{}, err
	}
	if c.Kind == "source_oauth" || c.Kind == "client_oauth" || c.Kind == "catalog_oauth" {
		return p.healthResult(p.ConnectionAuth.CheckNative(ctx, who.Tenant, connectionOwner(who, name), name)), nil
	}
	_, err = p.DiscoverPersonTools(ctx, who, personctx.ToolServer{Name: name, URL: c.Endpoint, OAuth: name})
	return p.healthResult(err), nil
}

func (p *Platform) CheckClientConnection(ctx context.Context, who auth.Principal, id string) (out ConnectionHealth, err error) {
	revision := p.connectionHealthRevision(who, id, true)
	defer func() {
		if err == nil {
			err = p.saveConnectionHealth(who, id, true, revision, out)
		}
	}()
	if who.Kind == auth.Machine || !who.Can(auth.ClientsWrite) {
		return ConnectionHealth{}, ErrForbidden
	}
	c, err := p.Clients.Get(who.Tenant, id)
	if err != nil {
		return ConnectionHealth{}, err
	}
	_, secrets, err := p.credentials(who.Tenant, id, c.Credentials, ClientInput{})
	if err != nil {
		return p.healthResult(err), nil
	}
	token := secrets.token
	if c.Surface == clients.Slack && c.Credentials.Token.Ref == slackOAuthRef {
		source, err := p.slackClientToken(c)
		if err != nil {
			return p.healthResult(err), nil
		}
		token, err = source(ctx)
		if err != nil {
			return p.healthResult(err), nil
		}
	}
	if c.Surface == clients.GitHub {
		if !github.IsPrivateKeyPEM(token) {
			out := p.healthResult(connectionauth.ErrReconnect)
			out.Message = "Use a GitHub App installation to verify its identity. A saved token alone does not verify this installation."
			return out, nil
		}
		source, err := github.NewAppTokenSource("", github.AppConfig{AppID: c.Identity.AppID, InstallationID: c.Identity.Installation, PrivateKey: token, HTTPClient: p.cfg.ConnectionCheckHTTP, Clock: p.now})
		if err != nil {
			return p.healthResult(err), nil
		}
		token, err = source.Token(ctx)
		if err != nil {
			return p.healthResult(err), nil
		}
	}
	err = connectionauth.ProbeNative(ctx, string(c.Surface), token, c.Identity.Team, p.cfg.ConnectionCheckHTTP)
	if errors.Is(err, connectionauth.ErrRejected) && c.Surface == clients.Slack && c.Credentials.Token.Ref == slackOAuthRef {
		provider, _, bindingErr := p.ConnectionAuth.Binding(who.Tenant, connectionauth.WorkspaceOwner, "client:"+id)
		if bindingErr == nil {
			p.ConnectionAuth.ReportRejected(ctx, who.Tenant, connectionauth.WorkspaceOwner, provider)
		}
	}
	return p.healthResult(err), nil
}
