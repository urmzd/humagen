package platform

import (
	"context"
	"time"

	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/githubinstall"
	github "github.com/urmzd/humagen/pkg/integrations/github"
)

// A client creator is not necessarily the current consenting person. Only the
// private verified installation receipt may identify a reconnect recipient.
func (p *Platform) githubAuthReceipt(ctx context.Context, c clients.Client) (githubinstall.OwnerReceipt, bool) {
	if p.GitHubInstall == nil || c.Managed != clients.ManagedInstall || c.Surface != clients.GitHub {
		return githubinstall.OwnerReceipt{}, false
	}
	current, err := p.Clients.Get(c.Tenant, c.ID)
	if err != nil || current.Surface != c.Surface || current.Managed != c.Managed || current.Enabled != c.Enabled || current.Identity.AppID != c.Identity.AppID || current.Identity.Installation != c.Identity.Installation || current.Credentials.Token.Ref != c.Credentials.Token.Ref || current.Credentials.SigningSecret.Ref != c.Credentials.SigningSecret.Ref {
		return githubinstall.OwnerReceipt{}, false
	}
	receipt, err := p.GitHubInstall.Receipt(ctx, c.Tenant, c.Identity.AppID, c.Identity.Installation)
	return receipt, err == nil
}

func (p *Platform) observeGitHubAppAuth(c clients.Client, cfg github.AppConfig) github.AppConfig {
	cfg.AuthRevision = func(ctx context.Context) string {
		receipt, ok := p.githubAuthReceipt(ctx, c)
		if !ok {
			return ""
		}
		return receipt.Generation
	}
	cfg.OnAuthRejected = func(ctx context.Context, generation string) {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		receipt, ok := p.githubAuthReceipt(ctx, c)
		if !ok || generation == "" || receipt.Generation != generation {
			return
		}
		_ = p.RecordConnectionAuthFailure(ctx, c.Tenant, receipt.Person, "github")
	}
	return cfg
}
