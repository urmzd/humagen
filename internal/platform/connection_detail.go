package platform

import (
	"context"
	"slices"
	"strings"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
)

type ConnectionMapping struct {
	Kind     string            `json:"kind"`
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Agents   []string          `json:"agents"`
	Projects []string          `json:"projects"`
	Places   []string          `json:"places"`
	Tools    []string          `json:"tools"`
	Health   *ConnectionHealth `json:"health,omitempty"`
}
type ConnectionDetail struct {
	Connection ConnectionPreset    `json:"connection"`
	Mappings   []ConnectionMapping `json:"mappings"`
	Note       string              `json:"note"`
	Truncated  bool                `json:"truncated"`
}

// GetConnection projects current authorized configuration, never another
// person's consent, source contents, credential references or runtime claims.
func (p *Platform) GetConnection(ctx context.Context, who auth.Principal, name string) (ConnectionDetail, error) {
	out := ConnectionDetail{Mappings: []ConnectionMapping{}, Note: "These are configured uses visible to you. They do not prove that an agent has used the service. Disconnect stops Humagen access; remove provider consent in the service's own settings."}
	if who.Kind == auth.Machine {
		return out, ErrForbidden
	}
	all, err := p.ConnectionPresets(who)
	if err != nil {
		return out, err
	}
	found := false
	for _, c := range all {
		if c.ID == name {
			out.Connection = c
			found = true
			break
		}
	}
	if !found {
		return out, ErrProviderNotFound
	}
	agents := []wire.AgentSpec{}
	if who.Can(auth.AgentsRead) && p.Agents != nil {
		page := wire.Page{Size: 100}
		for range 2 {
			rows, next, err := p.Catalog().List(ctx, who.Tenant, page)
			if err != nil {
				return out, err
			}
			for _, row := range rows {
				if p.Seer(who).Agent(row.Spec.Name) {
					agents = append(agents, row.Spec)
				}
			}
			if next == "" {
				break
			}
			page.Token = next
			out.Truncated = true
		}
	}
	mapping := func(kind, id, title string) ConnectionMapping {
		return ConnectionMapping{Kind: kind, ID: id, Name: title, Agents: []string{}, Projects: []string{}, Places: []string{}, Tools: []string{}}
	}
	addProject := func(m *ConnectionMapping, project string) {
		if who.Can(auth.ProjectsRead) && p.Seer(who).Project(project) && !slices.Contains(m.Projects, project) {
			m.Projects = append(m.Projects, project)
		}
	}
	if out.Connection.Kind == "oauth" && p.PersonContexts != nil {
		own, err := p.PersonContexts.Get(who.Tenant, who.Subject)
		if err != nil {
			return out, err
		}
		for _, server := range own.ToolServers {
			if server.OAuth != name {
				continue
			}
			m := mapping("tool_server", server.Name, server.Name)
			for _, tool := range server.Tools {
				m.Tools = append(m.Tools, tool.Name)
			}
			for _, a := range agents {
				if (!slices.Contains(a.Tools, personToolsGrant) && !slices.Contains(a.Tools, "person_tools")) || server.Tools == nil {
					continue
				}
				home, _ := p.ProjectTree(who.Tenant).Home(a.Name)
				settings, err := p.resolveSettings(who.Tenant, home, nil, who.Subject)
				if err != nil {
					return out, err
				}
				if settings.Ceilings.AllowsToolServer(server.Host()) {
					m.Agents = append(m.Agents, a.Name)
					addProject(&m, home)
				}
			}
			out.Mappings = append(out.Mappings, m)
		}
	}
	if name == "drive" && who.Can(auth.KnowledgeRead) && p.Knowledge != nil {
		sources, err := p.SourcesAs(who)
		if err != nil {
			return out, err
		}
		for _, src := range sources {
			if src.Lifecycle == nil {
				continue
			}
			provider, marker, err := p.ConnectionAuth.Binding(who.Tenant, who.Subject, sourceCredentialID(src))
			if err != nil || provider != name || marker == "" {
				continue
			}
			m := mapping("source", src.Name, src.Name)
			if src.Lifecycle != nil {
				addProject(&m, src.Lifecycle.Project)
			}
			for _, a := range agents {
				if slices.ContainsFunc(a.Sources, func(s wire.Source) bool { return s.Name == src.Name }) {
					m.Agents = append(m.Agents, a.Name)
				}
			}
			out.Mappings = append(out.Mappings, m)
		}
	}
	if (name == "github" || name == "slack" || name == "workspace-slack") && who.Can(auth.ClientsRead) && p.Clients != nil {
		list, err := p.Clients.List(who.Tenant)
		if err != nil {
			return out, err
		}
		for _, held := range list {
			if held.Managed != clients.ManagedInstall || string(held.Surface) != strings.TrimPrefix(name, "workspace-") {
				continue
			}
			c := p.Seer(who).Client(held)
			m := mapping("client", c.ID, c.Name)
			m.Health, err = p.lastConnectionHealth(who, c.ID, true)
			if err != nil {
				return out, err
			}
			for _, a := range agents {
				if c.Routes(a.Name) {
					m.Agents = append(m.Agents, a.Name)
				}
			}
			addProject(&m, held.HomeOf().Project)
			bindings, err := p.Bindings(who.Tenant, c.ID)
			if err != nil {
				return out, err
			}
			for _, b := range bindings {
				if !who.Can(auth.ProjectsRead) || !p.Seer(who).Project(b.Project) {
					continue
				}
				addProject(&m, b.Project)
				channel, err := p.Projects.Channel(who.Tenant, b.Project, b.Channel)
				if err != nil {
					return out, err
				}
				title := channel.Title
				if title == "" {
					title = channel.Name
				}
				m.Places = append(m.Places, "Slack channel "+b.SlackChannel+" ("+title+")")
			}
			if c.Slack != nil {
				for _, channel := range c.Slack.ActionChannels {
					m.Places = append(m.Places, "Slack action destination "+channel)
				}
			}
			if c.GitHub != nil {
				repos := append([]string{}, c.GitHub.ActionRepositories...)
				for _, rule := range c.GitHub.Automations {
					if !slices.Contains(repos, rule.Repository) {
						repos = append(repos, rule.Repository)
					}
				}
				for repo := range c.GitHub.AutoLabels {
					if !slices.Contains(repos, repo) {
						repos = append(repos, repo)
					}
				}
				slices.Sort(repos)
				for _, repo := range repos {
					m.Places = append(m.Places, "GitHub "+repo)
				}
			}
			out.Mappings = append(out.Mappings, m)
			if len(out.Mappings) == 200 {
				out.Truncated = true
				break
			}
		}
	}
	if name == "bigquery" && who.Can(auth.KnowledgeRead) && p.CatalogStore != nil {
		page, err := p.CatalogAs(who, "", 1)
		if err != nil {
			return out, err
		}
		for _, c := range page.Connectors {
			if !c.Mine || c.System != "bigquery" {
				continue
			}
			_, definition, err := p.managedBigQueryDefinition(who.Tenant, c.Name)
			if err != nil || definition.Input.Credential.Kind != "oauth" {
				continue
			}
			provider, marker, bindingErr := p.ConnectionAuth.Binding(who.Tenant, who.Subject, definition.Binding)
			if bindingErr != nil || marker == "" || provider != "bigquery" {
				continue
			}
			m := mapping("catalog", c.Name, c.Name)
			addProject(&m, c.Project)
			out.Mappings = append(out.Mappings, m)
			if len(out.Mappings) == 200 {
				out.Truncated = true
				break
			}
		}
	}
	return out, nil
}
