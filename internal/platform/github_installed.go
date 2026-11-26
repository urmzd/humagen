package platform

import (
	"time"

	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/integrations/github"
)

// installedRepos keeps the repositories a GitHub client's installation
// covers, or is nil when the client is no installation. A client that signs
// in as a GitHub App has one; so does a client whose API the embedding
// program supplied, when that API can list them. A personal token has none:
// GitHub answers GET /installation/repositories only to an installation.
func (p *Platform) installedRepos(c clients.Client, api github.API, app bool) *github.InstalledRepos {
	list, ok := api.(github.InstallationLister)
	if !ok || (!app && p.cfg.GitHubAPI == nil) {
		return nil
	}
	return github.NewInstalledRepos(list, func() time.Time { return p.now() }, func(err error) {
		p.log.Warn("platform: the repositories of a GitHub installation could not be listed, so its configured ones are used",
			"tenant", c.Tenant, "client", c.ID, "error", err)
	})
}
