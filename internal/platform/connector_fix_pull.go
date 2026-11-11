package platform

import (
	"context"
	"fmt"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/saige/flow/connector"
	transport "github.com/urmzd/saige/flow/connectors/github"
)

// authorizeFixPull binds repository writes to the actual run's requester.
// Runtime operation gates independently require both file and external-write
// permission/approval, and the connector confines its explicit destination.
func (p *Platform) authorizeFixPull(c clients.Client, api transport.API) func(context.Context, transport.Ref) error {
	return func(ctx context.Context, ref transport.Ref) error {
		current, enabled := p.currentInstallation(c)
		if !enabled || current.GitHub == nil || !current.GitHub.FixPullRequests || !current.AllowsAction("github_create_fix_pr") {
			return fmt.Errorf("github: fix PR capability is unavailable")
		}
		run, err := p.Runs.Get(c.Tenant, connector.RunID(ctx))
		if err != nil {
			return err
		}
		if run.Tenant != c.Tenant || !current.Routes(run.Agent) {
			return fmt.Errorf("github: run is outside installation")
		}
		login := ""
		if run.Origin == "github" && run.Client == c.ID && !run.Actor.Bot {
			login = run.Actor.ID
		}
		if login == "" {
			person := runs.AskedBy(run)
			if person != "" {
				for name, subject := range current.GitHub.People {
					if subject == person {
						if login != "" {
							return fmt.Errorf("github: requester has ambiguous GitHub identity")
						}
						login = name
					}
				}
			}
		}
		if login == "" {
			return fmt.Errorf("github: requester needs a verified GitHub identity with repository write permission")
		}
		writer, ok := api.(transport.MutationAPI)
		if !ok {
			return fmt.Errorf("github: repository permissions cannot be checked")
		}
		allowed, err := writer.CanWrite(ctx, ref, login)
		if err != nil {
			return err
		}
		if !allowed {
			return fmt.Errorf("github: requester no longer has repository write permission")
		}
		return nil
	}
}
