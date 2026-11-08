package platform

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/connectionauth"
	"github.com/urmzd/humagen/pkg/githubinstall"
	"github.com/urmzd/humagen/pkg/state"
)

func (p *Platform) configureGitHubInstall(st state.Store, cfg *githubinstall.Config) error {
	if cfg == nil {
		return nil
	}
	m, err := githubinstall.New(st, *cfg)
	if err != nil {
		return err
	}
	p.GitHubInstall = m
	return nil
}

// BeginGitHubInstall requires explicit workspace administration by a person.
func (p *Platform) BeginGitHubInstall(ctx context.Context, who auth.Principal) (connectionauth.Authorization, error) {
	if who.Kind != auth.User || !who.Can(auth.ClientsWrite) {
		return connectionauth.Authorization{}, ErrForbidden
	}
	attempt, err := p.BeginAudit(who, audit.Change{Action: "connection.authorize", Resource: "connection", ID: "github", Scope: "workspace", Route: "POST /api/connections/{provider}/authorize", Rule: string(auth.ClientsWrite)})
	if err != nil {
		return connectionauth.Authorization{}, err
	}
	out, err := p.GitHubInstall.Begin(ctx, who.Tenant, who.Subject)
	if completion := attempt.Finish(err); completion != nil {
		p.LogAuditCompletionFailure(completion)
	}
	return out, err
}

// CompleteGitHubInstall verifies the installation before publishing a disabled
// client. Agent routes and connector capabilities require a separate choice.
func (p *Platform) CompleteGitHubInstall(ctx context.Context, who auth.Principal, nonce, code, installationID string) error {
	if who.Kind != auth.User || !who.Can(auth.ClientsWrite) {
		return ErrForbidden
	}
	attempt, err := p.BeginAudit(who, audit.Change{Action: "connection.connect", Resource: "connection", ID: "github", Scope: "workspace", Route: "POST /api/connections/{provider}/callback", Rule: string(auth.ClientsWrite)})
	if err != nil {
		return err
	}
	err = p.completeGitHubInstall(ctx, who, nonce, code, installationID)
	if completion := attempt.Finish(err); completion != nil {
		p.LogAuditCompletionFailure(completion)
	}
	return err
}

func (p *Platform) completeGitHubInstall(ctx context.Context, who auth.Principal, nonce, code, installationID string) error {
	verified, err := p.GitHubInstall.Complete(ctx, who.Tenant, who.Subject, nonce, code, installationID)
	if err != nil {
		return err
	}
	cfg := p.cfg.GitHubInstall
	if cfg == nil {
		return connectionauth.ErrUnavailable
	}
	in := ClientInput{
		Client: clients.Client{
			ID: "github-" + verified.ID, Name: "GitHub · " + verified.AccountLogin,
			Surface: clients.GitHub, Managed: clients.ManagedInstall, Enabled: false,
			Identity: clients.Identity{AppID: verified.AppID, Installation: verified.ID, Owners: []string{verified.AccountLogin}},
			Agents:   []string{}, Capabilities: &clients.Capabilities{Events: []string{}, Actions: []string{}},
			GitHub: &clients.GitHubOptions{Login: verified.AppSlug + "[bot]"},
		},
		Token: clients.SecretInput{Ref: cfg.PrivateKeyRef}, SigningSecret: clients.SecretInput{Ref: cfg.SigningSecretRef}, nativeGitHub: true,
	}
	_, err = p.CreateClient(ctx, who.Tenant, who.Subject, in)
	if errors.Is(err, clients.ErrExists) {
		// Two callbacks or replicas can publish the same verified installation.
		// Never overwrite an unrelated client or reset its chosen permissions.
		old, readErr := p.Clients.Get(who.Tenant, in.ID)
		if readErr != nil {
			return readErr
		}
		if old.Managed == clients.ManagedInstall && old.Surface == clients.GitHub && old.Identity.AppID == verified.AppID && old.Identity.Installation == verified.ID && slices.Equal(old.Identity.Owners, in.Identity.Owners) && old.Credentials.Token.Ref == cfg.PrivateKeyRef && old.Credentials.SigningSecret.Ref == cfg.SigningSecretRef {
			return nil
		}
		return fmt.Errorf("%w: installation client already exists with another identity", clients.ErrExists)
	}
	return err
}

// DisconnectGitHubInstall disables native installations in this workspace.
// Uninstalling the app itself remains an explicit action in GitHub settings.
func (p *Platform) DisconnectGitHubInstall(ctx context.Context, who auth.Principal) error {
	if who.Kind != auth.User || !who.Can(auth.ClientsWrite) {
		return ErrForbidden
	}
	attempt, err := p.BeginAudit(who, audit.Change{Action: "connection.disconnect", Resource: "connection", ID: "github", Scope: "workspace", Route: "DELETE /api/connections/{provider}", Rule: string(auth.ClientsWrite)})
	if err != nil {
		return err
	}
	err = p.disableGitHubInstall(ctx, who)
	if completion := attempt.Finish(err); completion != nil {
		p.LogAuditCompletionFailure(completion)
	}
	return err
}

func (p *Platform) disableGitHubInstall(ctx context.Context, who auth.Principal) error {
	if p.GitHubInstall != nil {
		if err := p.GitHubInstall.Cancel(ctx, who.Tenant, who.Subject); err != nil {
			return err
		}
	}
	p.clientsMu.Lock()
	defer p.clientsMu.Unlock()
	all, err := p.Clients.List(who.Tenant)
	if err != nil {
		return err
	}
	for _, c := range all {
		if c.Surface != clients.GitHub || c.Managed != clients.ManagedInstall || !c.Enabled {
			continue
		}
		c.Enabled = false
		saved, err := p.Clients.Update(who.Tenant, c, p.now())
		if err != nil {
			return err
		}
		p.seat(saved, nil)
		p.touchClients()
	}
	return nil
}
