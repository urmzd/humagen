package platform

import (
	"context"
	"fmt"
	"strings"

	"github.com/urmzd/warp/pkg/auth"
	"github.com/urmzd/warp/pkg/clients"
	"github.com/urmzd/warp/pkg/projects"
)

// A project's channels and its experts. The project store keeps them and
// their history; the platform checks what the store cannot: that a default
// can answer where it is written.

// ChannelInput is a new channel.
type ChannelInput struct {
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Purpose string `json:"purpose,omitempty"`
	// Default answers untagged messages in the channel: an agent available
	// in the project, or auto. "" inherits from the project.
	Default string `json:"default,omitempty"`
	// Slack binds the channel to a Slack channel of a Slack client in the
	// workspace, whose bot is already in it.
	Slack *projects.SlackBinding `json:"slack,omitempty"`
}

// ChannelUpdate changes a channel's title, purpose and default, as a whole:
// a field left empty is cleared. Version is the one read; general reads 0
// until it is first written.
type ChannelUpdate struct {
	Version int                    `json:"version"`
	Title   string                 `json:"title"`
	Purpose string                 `json:"purpose"`
	Default string                 `json:"default"`
	Slack   *projects.SlackBinding `json:"slack"`
}

// Experts is the experts a project lists, at a version.
type Experts struct {
	Version int               `json:"version"`
	Experts []projects.Expert `json:"experts"`
}

// Channels returns a project's channels, general first. "~" is the root.
func (p *Platform) Channels(tenant, project string) ([]projects.Channel, error) {
	return p.Projects.Channels(tenant, projects.NameOf(project))
}

// CreateChannel adds a channel to a project.
func (p *Platform) CreateChannel(ctx context.Context, tenant, project string, w projects.Writer, in ChannelInput) (projects.Channel, error) {
	return p.createChannel(ctx, tenant, project, w, in, nil)
}

// CreateChannelAs is CreateChannel as who writes it: a default naming an agent
// they may not read is refused as one that does not exist.
func (p *Platform) CreateChannelAs(ctx context.Context, who auth.Principal, project string, w projects.Writer, in ChannelInput) (projects.Channel, error) {
	return p.createChannel(ctx, who.Tenant, project, w, in, p.Seer(who).AgentNamed)
}

func (p *Platform) createChannel(ctx context.Context, tenant, project string, w projects.Writer, in ChannelInput, sees func(string) bool) (projects.Channel, error) {
	project = projects.NameOf(project)
	if !p.HasProject(tenant, project) {
		return projects.Channel{}, projects.ErrNotFound
	}
	if err := p.checkDefault(ctx, tenant, project, in.Default, sees); err != nil {
		return projects.Channel{}, err
	}
	if err := p.checkBinding(tenant, in.Slack); err != nil {
		return projects.Channel{}, err
	}
	return p.Projects.CreateChannel(tenant, projects.Channel{
		Project: project, Name: in.Name, Title: strings.TrimSpace(in.Title), Purpose: strings.TrimSpace(in.Purpose), Default: in.Default, Slack: in.Slack,
	}, w, p.now())
}

// UpdateChannel changes a channel's title, purpose and default.
func (p *Platform) UpdateChannel(ctx context.Context, tenant, project, name string, w projects.Writer, in ChannelUpdate) (projects.Channel, error) {
	return p.updateChannel(ctx, tenant, project, name, w, in, nil)
}

// UpdateChannelAs is UpdateChannel as who writes it: a default naming an
// agent they may not read is refused as one that does not exist.
func (p *Platform) UpdateChannelAs(ctx context.Context, who auth.Principal, project, name string, w projects.Writer, in ChannelUpdate) (projects.Channel, error) {
	return p.updateChannel(ctx, who.Tenant, project, name, w, in, p.Seer(who).AgentNamed)
}

func (p *Platform) updateChannel(ctx context.Context, tenant, project, name string, w projects.Writer, in ChannelUpdate, sees func(string) bool) (projects.Channel, error) {
	project = projects.NameOf(project)
	if !p.HasProject(tenant, project) {
		return projects.Channel{}, projects.ErrNotFound
	}
	if err := p.checkDefault(ctx, tenant, project, in.Default, sees); err != nil {
		return projects.Channel{}, err
	}
	if err := p.checkBinding(tenant, in.Slack); err != nil {
		return projects.Channel{}, err
	}
	return p.Projects.UpdateChannel(tenant, project, name, in.Version, w, p.now(), func(c *projects.Channel) error {
		c.Title, c.Purpose, c.Default, c.Slack = strings.TrimSpace(in.Title), strings.TrimSpace(in.Purpose), in.Default, in.Slack
		return nil
	})
}

// MayBind holds a change to a channel's Slack binding to the client's side:
// which of a Slack client's channels a project answers in is decided by
// those who administer the client (clients:write), as binding one files
// every new thread there in the project. Keeping a channel's binding as it
// is, or removing it, takes nothing more than the route does. channel is ""
// for a new channel.
func (p *Platform) MayBind(who auth.Principal, project, channel string, b *projects.SlackBinding) error {
	if b == nil || who.Can(auth.ClientsWrite) {
		return nil
	}
	if channel != "" {
		if cur, err := p.Projects.Channel(who.Tenant, projects.NameOf(project), channel); err == nil && cur.Slack != nil && *cur.Slack == *b {
			return nil
		}
	}
	return fmt.Errorf("%w: binding a Slack channel takes clients:write, as every new thread there is filed in this project", ErrForbidden)
}

// checkBinding checks that a binding names a Slack client of the workspace.
// Whether its bot is in the Slack channel is Slack's to say: the platform
// never widens what the app may read.
func (p *Platform) checkBinding(tenant string, b *projects.SlackBinding) error {
	if b == nil {
		return nil
	}
	c, err := p.Clients.Get(tenant, b.Client)
	if err != nil {
		return fmt.Errorf("%w: no client %q in this workspace", projects.ErrInvalid, b.Client)
	}
	if c.Surface != clients.Slack {
		return fmt.Errorf("%w: %s is a %s client; a channel is bound only to Slack", projects.ErrInvalid, c.ID, c.Surface)
	}
	return nil
}

// Bindings returns the Slack channels a client is bound to. Another
// workspace's client has none here.
func (p *Platform) Bindings(tenant, client string) ([]projects.Binding, error) {
	if _, err := p.Clients.Get(tenant, client); err != nil {
		return nil, err
	}
	return p.Projects.Bindings(tenant, client), nil
}

// ArchiveChannel archives a channel: it and its threads stay, and it takes
// no new threads. General is never archived.
func (p *Platform) ArchiveChannel(tenant, project, name string, w projects.Writer) (projects.Channel, error) {
	return p.Projects.ArchiveChannel(tenant, projects.NameOf(project), name, w, p.now())
}

// ProjectExperts returns the experts a project lists.
func (p *Platform) ProjectExperts(tenant, project string) (Experts, error) {
	pr, err := p.Projects.Get(tenant, projects.NameOf(project))
	if err != nil {
		return Experts{}, err
	}
	if pr.Experts == nil {
		pr.Experts = []projects.Expert{}
	}
	return Experts{Version: pr.Version, Experts: pr.Experts}, nil
}

// SetProjectExperts replaces the experts a project lists.
func (p *Platform) SetProjectExperts(tenant, project string, w projects.Writer, in Experts) (Experts, error) {
	pr, err := p.Projects.SetExperts(tenant, projects.NameOf(project), in.Version, in.Experts, w, p.now())
	if err != nil {
		return Experts{}, err
	}
	return Experts{Version: pr.Version, Experts: pr.Experts}, nil
}
