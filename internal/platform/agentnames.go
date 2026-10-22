package platform

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/urmzd/warp/pkg/auth"
	"github.com/urmzd/warp/pkg/clients"
	"github.com/urmzd/warp/pkg/projects"
)

// The agents a record names, as a reader is shown them.
//
// Besides the agents routes, three records name agents: a client (its
// default, its list, its bots and its auto-labels), a project's lists of
// agents, and the workspace's settings (its title and memory agents). A
// reader is shown a name in them only when they may read the agent
// (Seer.Agent): one that lives in a project they may not read is left out,
// as if it were not there. A change they send keeps what they were not
// shown, and a name they may not read is refused exactly as one that names
// no agent, so writing learns nothing either.

// shownNames is names, each one the reader may read, in order. "*" is no
// agent, so it stays.
func (s *Seer) shownNames(names []string) []string {
	if names == nil {
		return nil
	}
	return slices.DeleteFunc(slices.Clone(names), func(n string) bool { return n != clients.AnyAgent && !s.Agent(n) })
}

// agentsNamed is every agent a client names: its default, its list without
// "*", its bots and its auto-labels, in that order, the maps by key.
func agentsNamed(c clients.Client) []string {
	var out []string
	if c.DefaultAgent != "" {
		out = append(out, c.DefaultAgent)
	}
	for _, a := range c.Agents {
		if a != clients.AnyAgent {
			out = append(out, a)
		}
	}
	for _, ext := range slices.Sorted(maps.Keys(c.Bots)) {
		out = append(out, c.Bots[ext])
	}
	if c.GitHub != nil {
		for _, repo := range slices.Sorted(maps.Keys(c.GitHub.AutoLabels)) {
			labels := c.GitHub.AutoLabels[repo]
			for _, label := range slices.Sorted(maps.Keys(labels)) {
				out = append(out, labels[label])
			}
		}
	}
	return out
}

// Client is a client as the reader is shown it: its home left out when it is
// in a project they may not read, and every agent it names that they may not
// read left out of its default, its list, its bots and its auto-labels. The
// machine that hosts the client's surface is shown every agent it names, as
// it routes by them, just as it reads every thread on that surface.
func (s *Seer) Client(c clients.Client) clients.Client {
	if c.Home != nil && !s.Project(c.HomeOf().Project) {
		c.Home = nil
	}
	if machineOf(c, s.who) {
		return c
	}
	if c.DefaultAgent != "" && !s.Agent(c.DefaultAgent) {
		c.DefaultAgent = ""
	}
	c.Agents = s.shownNames(c.Agents)
	c.Bots = s.shownMap(c.Bots)
	if c.GitHub != nil {
		g := *c.GitHub
		g.AutoLabels = nil
		for repo, labels := range c.GitHub.AutoLabels {
			if shown := s.shownMap(labels); len(shown) > 0 {
				if g.AutoLabels == nil {
					g.AutoLabels = map[string]map[string]string{}
				}
				g.AutoLabels[repo] = shown
			}
		}
		c.GitHub = &g
	}
	return c
}

// shownMap is a map to agents, keeping only the entries whose agent the
// reader may read.
func (s *Seer) shownMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, a := range m {
		if s.Agent(a) {
			out[k] = a
		}
	}
	return out
}

// noAgent refuses a name that is no agent the writer may read, in the same
// words whether it lives in a project they may not read or does not exist.
func noAgent(kind error, name string) error {
	return fmt.Errorf("%w: there is no agent called %q", kind, name)
}

// ClientWrittenBy settles the agents a client names as who writes it,
// against old, the record it replaces, nil for a new client.
//
// Every agent it names must be one that exists and that who may read, or a
// name old already holds that who is shown, or auto; any other is refused with
// clients.ErrInvalid, as no agent, whether it lives in a project who may not
// read or does not exist. What old names that who is not shown is kept: its
// default, unless who names another, the agents of its list, unless who
// writes "*", which reaches them too, and its bots and auto-labels, unless
// who maps the same bot or label, and for a repository the client still
// serves. An agent who is not shown that the client routed to stays one it
// routes to, so a bot or a label kept for it still stands.
func (p *Platform) ClientWrittenBy(ctx context.Context, who auth.Principal, old *clients.Client, c clients.Client) (clients.Client, error) {
	seer := p.Seer(who)
	shown := seer.Agent
	if old != nil && machineOf(*old, who) {
		shown = func(string) bool { return true }
	}
	held := map[string]bool{}
	var hidden []string
	if old != nil {
		for _, a := range agentsNamed(*old) {
			switch {
			case shown(a):
				held[a] = true
			case !slices.Contains(hidden, a):
				hidden = append(hidden, a)
			}
		}
	}
	if err := p.EnsureBuiltins(ctx, who.Tenant); err != nil {
		p.log.Warn("platform: built-in agents", "error", err)
	}
	for _, a := range agentsNamed(c) {
		// auto is no agent: it picks one. Each name is looked up once.
		if held[a] || a == Auto {
			continue
		}
		rev, err := p.Agents.Get(ctx, who.Tenant, a, 0)
		if err != nil || rev.Spec.Name != a || !seer.Agent(a) {
			return clients.Client{}, noAgent(clients.ErrInvalid, a)
		}
		held[a] = true
	}
	if len(hidden) == 0 {
		return c, nil
	}

	c.Agents = slices.Clone(c.Agents)
	if c.DefaultAgent == "" && old.DefaultAgent != "" && !shown(old.DefaultAgent) {
		c.DefaultAgent = old.DefaultAgent
	}
	if !slices.Contains(c.Agents, clients.AnyAgent) {
		for _, a := range old.Agents {
			if a != clients.AnyAgent && !shown(a) && !slices.Contains(c.Agents, a) {
				c.Agents = append(c.Agents, a)
			}
		}
	}
	for ext, a := range old.Bots {
		if _, mapped := c.Bots[ext]; !mapped && !shown(a) {
			if c.Bots == nil {
				c.Bots = map[string]string{}
			}
			c.Bots[ext] = a
		}
	}
	if old.GitHub != nil {
		for repo, labels := range old.GitHub.AutoLabels {
			if owner, _, _ := strings.Cut(repo, "/"); !c.Serves(owner) {
				continue
			}
			for label, a := range labels {
				if shown(a) {
					continue
				}
				if c.GitHub == nil {
					c.GitHub = &clients.GitHubOptions{}
				}
				if c.GitHub.AutoLabels == nil {
					c.GitHub.AutoLabels = map[string]map[string]string{}
				}
				if c.GitHub.AutoLabels[repo] == nil {
					c.GitHub.AutoLabels[repo] = map[string]string{}
				}
				if _, mapped := c.GitHub.AutoLabels[repo][label]; !mapped {
					c.GitHub.AutoLabels[repo][label] = a
				}
			}
		}
	}
	for _, a := range hidden {
		if !c.Routes(a) {
			c.Agents = append(c.Agents, a)
		}
	}
	return c, nil
}

// ProjectAgents is a project's lists as the reader is shown them, each name
// of an agent they may not read left out. An allow list stays a list, so one
// whose every name is left out reads as an empty one, which lets nothing the
// reader is shown run, as it does.
func (s *Seer) ProjectAgents(a projects.Agents) projects.Agents {
	out := projects.Agents{Enable: s.shownNames(a.Enable), Default: a.Default}
	if a.Allow != nil {
		out.Allow = s.shownNames(a.Allow)
	}
	if out.Default != "" && out.Default != Auto && !s.Agent(out.Default) {
		out.Default = ""
	}
	return out
}

// AgentsView is a project's agents as the reader is shown them: its lists
// as ProjectAgents leaves them, and no resolved default naming an agent they
// may not read. What is available in a project the reader may read is
// always an agent they may read, since a project agent is available only in
// its home and below, which they may read too.
func (s *Seer) AgentsView(v ProjectAgents) ProjectAgents {
	shown := s.ProjectAgents(projects.Agents{Enable: v.Enable, Allow: v.Allow, Default: v.Default})
	v.Enable, v.Allow, v.Default = shown.Enable, shown.Allow, shown.Default
	if v.Enable == nil {
		v.Enable = []string{}
	}
	if d := v.DefaultResolved; d != nil && d.Agent != Auto && !s.Agent(d.Agent) {
		v.DefaultResolved = nil
	}
	return v
}

// Change is a project's change as the reader is shown it: its record before
// and after with their lists as ProjectAgents and Sources leave them, and the
// agent a change of home is about left out when they may not read it.
func (s *Seer) Change(c projects.Change) projects.Change {
	for _, pr := range []**projects.Project{&c.Before, &c.After} {
		if *pr != nil {
			shown := **pr
			shown.Agents = s.ProjectAgents(shown.Agents)
			shown.Knowledge = s.Sources(shown.Knowledge)
			*pr = &shown
		}
	}
	if c.Agent != "" && !s.Agent(c.Agent) {
		c.Agent = ""
	}
	return c
}

// ProjectAgentsWrittenBy settles the lists who writes to a project against
// what the project holds. Every name in them must be an agent that exists
// and that who may read; any other is refused as no agent, in the words
// SetProjectAgents uses for one that does not exist. The names in its allow
// list that who is not shown are kept, as a change keeps what its writer was
// not shown. They let nothing run here or below while those agents live
// where they do: only an owner writes the lists, an owner here owns every
// project below, and so every agent that lives below is one who reads. They
// would if one came to live below. An enable entry or a default never names
// an agent who may not read: an agent that lives in a project is never
// enabled (projects' checkAgents), and a default is available where it is
// set, so its agent lives on this project's chain, which who reads.
func (p *Platform) ProjectAgentsWrittenBy(ctx context.Context, who auth.Principal, project string, in AgentsInput) (AgentsInput, error) {
	seer := p.Seer(who)
	if err := p.EnsureBuiltins(ctx, who.Tenant); err != nil {
		p.log.Warn("platform: built-in agents", "error", err)
	}
	for _, n := range slices.Concat(in.Enable, in.Allow, []string{in.Default}) {
		if n == "" || n == projects.Everyone || n == Auto {
			continue
		}
		if _, err := p.Agents.Get(ctx, who.Tenant, n, 0); err != nil || !seer.Agent(n) {
			return AgentsInput{}, noAgent(projects.ErrInvalid, n)
		}
	}
	pr, ok := p.Projects.Tree(who.Tenant).Get(projects.NameOf(project))
	if !ok || in.Allow == nil {
		return in, nil
	}
	in.Allow = slices.Clone(in.Allow)
	for _, a := range pr.Agents.Allow {
		if _, err := p.Agents.Get(ctx, who.Tenant, a, 0); err == nil && !seer.Agent(a) {
			in.Allow = append(in.Allow, a)
		}
	}
	return in, nil
}

// Settings is the workspace's settings as the reader is shown them: a title
// or memory agent they may not read is left out, as if none were set.
func (s *Seer) Settings(st Settings) Settings {
	if st.TitleAgent != "" && !s.Agent(st.TitleAgent) {
		st.TitleAgent = ""
	}
	if st.MemoryAgent != "" && !s.Agent(st.MemoryAgent) {
		st.MemoryAgent = ""
	}
	return st
}

// SetSettingsBy replaces a workspace's settings as who writes them. A title
// or memory agent who may not read is refused as one that does not exist,
// and one they are not shown is kept when they name none in its place. A
// model policy an agent they may not read runs on is not removed, and the
// refusal does not name it.
func (p *Platform) SetSettingsBy(ctx context.Context, who auth.Principal, next Settings) error {
	seer := p.Seer(who)
	for _, name := range []string{next.TitleAgent, next.MemoryAgent} {
		if name == "" {
			continue
		}
		if _, err := p.Agents.Get(ctx, who.Tenant, name, 0); err != nil || !seer.Agent(name) {
			return fmt.Errorf("%w: %q", ErrNoBaseline, name)
		}
	}
	stored := p.Settings(who.Tenant)
	if next.TitleAgent == "" && !seer.Agent(stored.TitleAgent) {
		next.TitleAgent = stored.TitleAgent
	}
	if next.MemoryAgent == "" && !seer.Agent(stored.MemoryAgent) {
		next.MemoryAgent = stored.MemoryAgent
	}
	return p.setSettings(ctx, who.Tenant, next, seer.Agent)
}
