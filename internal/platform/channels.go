package platform

import (
	"context"
	"strings"

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
}

// ChannelUpdate changes a channel's title, purpose and default, as a whole:
// a field left empty is cleared. Version is the one read; general reads 0
// until it is first written.
type ChannelUpdate struct {
	Version int    `json:"version"`
	Title   string `json:"title"`
	Purpose string `json:"purpose"`
	Default string `json:"default"`
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
	project = projects.NameOf(project)
	if !p.HasProject(tenant, project) {
		return projects.Channel{}, projects.ErrNotFound
	}
	if err := p.checkDefault(ctx, tenant, project, in.Default); err != nil {
		return projects.Channel{}, err
	}
	return p.Projects.CreateChannel(tenant, projects.Channel{
		Project: project, Name: in.Name, Title: strings.TrimSpace(in.Title), Purpose: strings.TrimSpace(in.Purpose), Default: in.Default,
	}, w, p.now())
}

// UpdateChannel changes a channel's title, purpose and default.
func (p *Platform) UpdateChannel(ctx context.Context, tenant, project, name string, w projects.Writer, in ChannelUpdate) (projects.Channel, error) {
	project = projects.NameOf(project)
	if !p.HasProject(tenant, project) {
		return projects.Channel{}, projects.ErrNotFound
	}
	if err := p.checkDefault(ctx, tenant, project, in.Default); err != nil {
		return projects.Channel{}, err
	}
	return p.Projects.UpdateChannel(tenant, project, name, in.Version, w, p.now(), func(c *projects.Channel) error {
		c.Title, c.Purpose, c.Default = strings.TrimSpace(in.Title), strings.TrimSpace(in.Purpose), in.Default
		return nil
	})
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
