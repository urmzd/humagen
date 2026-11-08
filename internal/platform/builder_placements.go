package platform

import (
	"cmp"
	"slices"
	"strings"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/orchagent/flow/runtime"
)

type builderConnection struct {
	Name    string `json:"name"`
	Service string `json:"service"`
	Scope   string `json:"scope"`
	Status  string `json:"status"`
}

type builderListen struct {
	Connection string `json:"connection"`
	Place      string `json:"place"`
	Project    string `json:"project,omitempty"`
	Enabled    bool   `json:"enabled"`
}

// These are current configuration, not new grants or live health claims.
// Pointers distinguish an unreadable/unknown list from a known empty list.
type builderPlacements struct {
	Connections *[]builderConnection `json:"connections,omitempty"`
	Listens     *[]builderListen     `json:"listens,omitempty"`
}

// Client configuration requires clients:read, and may not be disclosed into a
// broader answer audience just because the person who asked can read it.
func (p *Platform) builderPlacementPreview(req runtime.Request, spec wire.AgentSpec) (builderPlacements, error) {
	unknown := builderPlacements{}
	who, err := p.builderPerson(req)
	if err != nil || !who.Can(auth.ClientsRead) {
		return unknown, nil
	}
	run, person, err := p.ownPrivateConversation(req)
	if err != nil || person != who.Subject || !p.Seer(who).Run(run) {
		return unknown, nil
	}
	all, err := p.Clients.List(req.Tenant)
	if err != nil {
		return unknown, err
	}
	connections, listens := []builderConnection{}, []builderListen{}
	seer := p.Seer(who)
	complete := true
	for _, held := range all {
		if !held.Routes(spec.Name) {
			continue
		}
		c := seer.Client(held)
		status := "configured"
		if !c.Enabled {
			status = "disabled"
		}
		connections = append(connections, builderConnection{Name: c.Name, Service: string(c.Surface), Scope: "workspace", Status: status})
		home := held.HomeOf().Project
		if !seer.Project(home) {
			complete = false
			continue
		}
		project := "Workspace"
		if home != projects.Root {
			project = home
		}
		if c.Surface == clients.Slack {
			inbound := c.AllowsEvent("app_mention") || c.AllowsEvent("message") || c.AllowsEvent("slash_command")
			// A binding supplies a human-readable channel title. ActionChannels
			// are write grants and must never be described as listeners.
			listens = append(listens, builderListen{Connection: c.Name, Place: "Slack mentions and replies", Project: project, Enabled: c.Enabled && inbound})
			bindings, err := p.Bindings(req.Tenant, c.ID)
			if err != nil {
				return unknown, err
			}
			for _, binding := range bindings {
				if !binding.Direction.AllowsInbound() {
					continue
				}
				if !seer.Project(binding.Project) {
					complete = false
					continue
				}
				channel, err := p.Projects.Channel(req.Tenant, binding.Project, binding.Channel)
				if err != nil {
					return unknown, err
				}
				label := channel.Title
				if label == "" {
					label = channel.Name
				}
				boundProject := "Workspace"
				if binding.Project != projects.Root {
					boundProject = binding.Project
				}
				listens = append(listens, builderListen{Connection: c.Name, Place: "Slack channel: " + label, Project: boundProject, Enabled: c.Enabled && inbound && !channel.Archived})
			}
		} else if c.Surface == clients.GitHub {
			inbound := c.AllowsEvent("issue_comment") || c.AllowsEvent("pull_request_review_comment") || c.AllowsEvent("pull_request_review")
			// Installation repository access is checked at use time. A read or
			// write repository grant is not evidence of an inbound subscription.
			listens = append(listens, builderListen{Connection: c.Name, Place: "GitHub mentions in installed repositories", Project: project, Enabled: c.Enabled && inbound})
			if c.GitHub != nil {
				for _, rule := range c.GitHub.Automations {
					if rule.Agent == spec.Name {
						listens = append(listens, builderListen{Connection: c.Name, Place: rule.Repository + ": " + rule.Event + "/" + rule.Action, Project: project, Enabled: c.Enabled && c.AllowsEvent(rule.Event) && !rule.Paused})
					}
				}
				for repo, labels := range c.GitHub.AutoLabels {
					for label, agent := range labels {
						if agent == spec.Name {
							listens = append(listens, builderListen{Connection: c.Name, Place: repo + ": label " + label, Project: project, Enabled: c.Enabled && c.AllowsEvent("issues")})
						}
					}
				}
			}
		}
	}
	if slices.Contains(spec.Tools, personToolsGrant) || slices.Contains(spec.Tools, "person_tools") {
		own, err := p.PersonContexts.Get(req.Tenant, who.Subject)
		if err != nil {
			return unknown, err
		}
		home, _ := p.ProjectTree(req.Tenant).Home(spec.Name)
		settings, err := p.resolveSettings(req.Tenant, home, nil, who.Subject)
		if err != nil {
			return unknown, err
		}
		for _, server := range own.ToolServers {
			status := "configured"
			if server.Tools == nil {
				status = "review_required"
			} else if !settings.Ceilings.AllowsToolServer(server.Host()) {
				status = "blocked_by_policy"
			}
			scope := "personal"
			if strings.HasPrefix(server.OAuth, "workspace-") {
				scope = "workspace"
			}
			connections = append(connections, builderConnection{Name: server.Name, Service: "tool_server", Scope: scope, Status: status})
		}
	}
	slices.SortFunc(connections, func(a, b builderConnection) int {
		return cmp.Or(cmp.Compare(a.Scope, b.Scope), cmp.Compare(a.Service, b.Service), cmp.Compare(a.Name, b.Name))
	})
	slices.SortFunc(listens, func(a, b builderListen) int {
		return cmp.Or(cmp.Compare(a.Connection, b.Connection), cmp.Compare(a.Project, b.Project), cmp.Compare(a.Place, b.Place))
	})
	out := builderPlacements{Connections: &connections}
	// Do not present a truncated or partly hidden list as the whole placement.
	if complete && len(listens) <= 256 {
		out.Listens = &listens
	}
	return out, nil
}
