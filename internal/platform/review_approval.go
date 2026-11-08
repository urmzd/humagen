package platform

import (
	"strings"

	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/sourcelinks"
	"github.com/urmzd/orchagent/flow/address"
	transport "github.com/urmzd/orchagent/flow/connectors/github"
)

// A cross-repository action grant permits the action, not the destination
// project's approval policy. Resolve that policy from current placement.
func (p *Platform) reviewApproval(c clients.Client, v transport.View) bool {
	current, enabled := p.currentInstallation(c)
	run, err := p.Runs.Get(c.Tenant, v.RunID)
	if !enabled || err != nil || run.Client != c.ID || run.Tenant != c.Tenant || v.Tenant != c.Tenant || !current.Routes(run.Agent) {
		return false
	}
	a, err := address.Parse(v.Channel)
	if err != nil || a.Connector != "github" || len(a.Path) != 3 || a.Path[1] != "pull" || !current.Serves(a.Workspace) {
		return false
	}
	repository := strings.ToLower(a.Workspace + "/" + a.Path[0])
	rows, err := p.SourceSuggestions.List(c.Tenant, c.ID, "linked", 200, func(row sourcelinks.Suggestion) bool {
		return row.Knowledge == "" && row.Resource.Provider == "github" && strings.EqualFold(row.Resource.ID, repository)
	})
	if err != nil || len(rows) >= 200 {
		return false
	}
	home := current.HomeOf()
	project, channel := home.Project, home.Channel
	if len(rows) > 0 {
		destinations := map[[2]string]bool{}
		for _, row := range rows {
			if !p.automaticSourceLinkActive(c.Tenant, row) {
				return false
			}
			ch := row.Channel
			if ch == "" {
				ch = "general"
			}
			destinations[[2]string{projects.NameOf(row.Project), ch}] = true
		}
		if len(destinations) != 1 {
			return false
		}
		for destination := range destinations {
			project, channel = destination[0], destination[1]
		}
	}
	ch, err := p.Projects.Channel(c.Tenant, project, channel)
	if err != nil || ch.Archived {
		return false
	}
	pr, ok := p.Projects.Tree(c.Tenant).Get(project)
	return ok && pr.ApproveReviews
}
