package platform

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/urmzd/fiber/pkg/envelope"
	"github.com/urmzd/fiber/pkg/registry"

	"github.com/urmzd/warp/pkg/alerts"
	"github.com/urmzd/warp/pkg/clients"
	"github.com/urmzd/warp/pkg/pick"
	"github.com/urmzd/warp/pkg/projects"
	"github.com/urmzd/warp/pkg/runs"
)

// Agents in projects: which agents may run where, who answers a message that
// tags none, and where an agent lives (docs/projects.md, "Agents in
// projects"). The project tree decides availability; the platform asks it
// once, at admission, and again sooner wherever an answer helps a person:
// writing a schedule or a default, and previewing @auto.

var (
	// ErrNotAvailable reports an agent that may not run where a run is
	// filed: nothing on the project's chain offers it, or an allow list on
	// the chain leaves it out.
	ErrNotAvailable = errors.New("platform: not available")
	// ErrNoDefault reports a message that tags no agent, in a place where
	// nothing answers untagged messages.
	ErrNoDefault = errors.New("platform: tag an agent")
)

// Auto is the built-in router's reserved name: @auto picks the agent.
const Auto = projects.Auto

// whereOf is how a project is named in what a person reads: its title, or the
// workspace's name for the root.
func (p *Platform) whereOf(tenant string, tree *projects.Tree, project string) string {
	if pr, ok := tree.Get(project); ok {
		return titleOf(pr, p.Settings(tenant).WorkspaceName)
	}
	return project
}

// Available reports whether an agent may run in a project ("" is the root,
// and "~" names it too): nil when it may, and ErrNotAvailable saying why when
// it may not. It is the one check: admission makes it for every run, and the
// places that write an agent into a project make it first, so a person hears
// the same answer sooner. Whether the agent exists is the registry's to say.
// A project the tree does not hold is ErrProjectNotFound, as it is to the
// caller that looked it up just before a delete won.
func (p *Platform) Available(tenant, project, agent string) error {
	project = projects.NameOf(project)
	tree := p.Projects.Tree(tenant)
	if tree.IsAvailable(project, agent) {
		return nil
	}
	if !tree.Has(project) {
		return fmt.Errorf("%w: %q", ErrProjectNotFound, project)
	}
	return p.notAvailable(tenant, tree, project, agent)
}

// notAvailable says why an agent may not run in a project the tree holds.
func (p *Platform) notAvailable(tenant string, tree *projects.Tree, project, agent string) error {
	where := p.whereOf(tenant, tree, project)
	if agent == Auto {
		return fmt.Errorf("%w: @auto picks an agent before anything runs, so it never runs itself", ErrNotAvailable)
	}
	if home, ok := tree.Home(agent); ok && !onChain(tree, project, home) {
		return fmt.Errorf("%w: %s is not available in %s; it lives in %s and is available only there and below", ErrNotAvailable, agent, where, p.whereOf(tenant, tree, home))
	}
	return fmt.Errorf("%w: %s is not available in %s", ErrNotAvailable, agent, where)
}

// onChain reports whether a project other than the root is the project
// itself or one of its ancestors.
func onChain(tree *projects.Tree, project, name string) bool {
	return project == name || slices.Contains(tree.Ancestors(project), name)
}

// admitAgent is admission's availability check. A run filed in a project,
// the root included, must use an agent available there. Housekeeping is
// exempt, as it is from admission. An eval is filed where its agent lives
// (evalCase), so it is checked as any run there is. A scheduled run refused
// here keeps firing and is refused each time, and the refusal is raised
// once, as an alert, until someone acknowledges it.
func (p *Platform) admitAgent(tag envelope.Tag, project string) error {
	if tag.Origin == runs.OriginSystem {
		return nil
	}
	tree := p.Projects.Tree(tag.Tenant)
	at := project
	if tree.IsAvailable(at, tag.Agent) {
		return nil
	}
	if !tree.Has(at) {
		// Deleted between placeOf and here: refused as placeOf refuses it.
		return projectDeleted(at)
	}
	err := p.notAvailable(tag.Tenant, tree, at, tag.Agent)
	p.log.Warn("platform: run refused: agent not available", "tenant", tag.Tenant, "agent", tag.Agent, "project", at, "origin", tag.Origin)
	// A person running a schedule now is told at once; the clock has nobody
	// to tell.
	if schedule := tag.Meta["cron_schedule"]; schedule != "" && tag.Origin == "cron" {
		p.unavailableAlert(tag.Tenant, schedule, tag.Agent, project, err)
	}
	return err
}

// unavailableAlert raises, once while it stays open, that a schedule's agent
// was refused where the schedule is filed.
func (p *Platform) unavailableAlert(tenant, schedule, agent, project string, err error) {
	key := "unavailable: schedule " + schedule
	a := alerts.Alert{
		Kind: alerts.RunFailed, Severity: alerts.Critical, Agent: agent, Project: project, At: p.now(),
		Title: fmt.Sprintf("The schedule %s could not start: %s", schedule, strings.TrimPrefix(err.Error(), ErrNotAvailable.Error()+": ")),
	}
	raised, ok, aerr := p.Alerts.Raise(tenant, key, a)
	if aerr != nil {
		p.log.Error("platform: alert could not be kept", "rule", key, "error", aerr)
	}
	if ok {
		p.inboxAlert(tenant, raised)
	}
}

// candidates are the agents available in a project that keep passes, as
// @auto weighs them. keep nil keeps every one.
func (p *Platform) candidates(ctx context.Context, tenant string, tree *projects.Tree, project string, keep func(string) bool) ([]pick.Candidate, error) {
	var out []pick.Candidate
	page := registry.Page{Size: 100}
	for {
		revs, next, err := p.Agents.List(ctx, tenant, page)
		if err != nil {
			return nil, err
		}
		for _, r := range revs {
			if !tree.IsAvailable(project, r.Spec.Name) || (keep != nil && !keep(r.Spec.Name)) {
				continue
			}
			out = append(out, pick.Candidate{Name: r.Spec.Name, Description: r.Spec.Description, Prompt: r.Spec.SystemPrompt})
		}
		if next == "" {
			return out, nil
		}
		page.Token = next
	}
}

// Pick is what @auto does with a message in a project ("" or "~" is the
// root), without starting anything: it chooses among the agents available
// there. With no match it falls back to the nearest default on the project's
// chain that names an agent, and with none it is pick.ErrNoAgent.
func (p *Platform) Pick(ctx context.Context, tenant, project, message string) (pick.Choice, error) {
	project = projects.NameOf(project)
	tree := p.Projects.Tree(tenant)
	if !tree.Has(project) {
		return pick.Choice{}, fmt.Errorf("%w: %q", projects.ErrNotFound, project)
	}
	fallback, _ := tree.NamedDefault(project)
	return p.auto(ctx, tenant, tree, project, message, fallback, nil)
}

func (p *Platform) auto(ctx context.Context, tenant string, tree *projects.Tree, project, message, fallback string, keep func(string) bool) (pick.Choice, error) {
	cs, err := p.candidates(ctx, tenant, tree, project, keep)
	if err != nil {
		return pick.Choice{}, err
	}
	return pick.Agent(message, cs, fallback)
}

// Nearer is a default set nearer a message than its project: a thread's, a
// client's or a channel's, and where it is set ("thread", "client",
// "channel"). Channels and threads, and a client's home, fill these in; until
// they do, a message has none.
type Nearer struct {
	Agent string `json:"agent"`
	From  string `json:"from"`
}

// Place is where a message is posted, for deciding who answers it.
type Place struct {
	// Project is where the message's conversation is filed. "" is the root.
	Project string
	// Nearer are the defaults set nearer than the project, nearest first.
	Nearer []Nearer
}

// Answerer is who answers a message, and how they were chosen.
type Answerer struct {
	Agent string
	// Picked is @auto's reason, when @auto chose.
	Picked string
	// From is where the default that answered is set: "thread", "client",
	// "channel", or "project:<name>" ("project:~" for the root). It is empty
	// for an agent the message tagged.
	From string
}

// fromProject names a default set on a project, as a default's From reads.
func fromProject(name string) string {
	if name == projects.Root {
		return "project:" + projects.RootAddress
	}
	return "project:" + name
}

// DefaultAt is the default a message in a place inherits, and where it is
// set: the first set of the place's nearer defaults, then its project's, each
// ancestor's nearest first, and the root's. It is the zero Nearer when none
// is set.
func (p *Platform) DefaultAt(tenant string, at Place) Nearer {
	for _, n := range at.Nearer {
		if n.Agent != "" {
			return n
		}
	}
	if agent, from := p.Projects.Tree(tenant).DefaultOf(at.Project); agent != "" {
		return Nearer{Agent: agent, From: fromProject(from)}
	}
	return Nearer{}
}

// namedDefault is the nearest default on a place's chain that names an
// agent, where @auto hands a message nothing matched.
func namedDefault(tree *projects.Tree, at Place) string {
	for _, n := range at.Nearer {
		if n.Agent != "" && n.Agent != Auto {
			return n.Agent
		}
	}
	agent, _ := tree.NamedDefault(at.Project)
	return agent
}

// resolveAgent decides who answers a message in a place. asked is the agent
// the message tags: an agent, Auto for @auto, or "" for none.
//
// Nothing routes on its own: with no agent asked, the default the place
// inherits answers, and with none set it is ErrNoDefault. A default of auto,
// and @auto itself, pick among the agents available in the place
// (pick.ErrNoAgent when nothing fits). A named default that has stopped being
// available is not passed over for the next one along: it is ErrNotAvailable,
// saying why. Whether a tagged agent is available is admission's to say.
// routes, when set, is the client's list a surface sends the message through:
// @auto picks only among the agents on it, and falls back only to one on it.
func (p *Platform) resolveAgent(ctx context.Context, tenant string, at Place, asked, text string, routes func(string) bool) (Answerer, error) {
	tree := p.Projects.Tree(tenant)
	out := Answerer{Agent: asked}
	if asked == "" {
		d := p.DefaultAt(tenant, at)
		if d.Agent == "" {
			return Answerer{}, fmt.Errorf("%w: nobody answers untagged messages in %s; tag an agent", ErrNoDefault, p.whereOf(tenant, tree, at.Project))
		}
		out.Agent, out.From = d.Agent, d.From
	}
	if out.Agent == Auto {
		fallback := namedDefault(tree, at)
		if routes != nil && !routes(fallback) {
			fallback = ""
		}
		choice, err := p.auto(ctx, tenant, tree, at.Project, text, fallback, routes)
		if err != nil {
			return Answerer{}, err
		}
		out.Agent, out.Picked = choice.Agent, choice.Reason
		return out, nil
	}
	if out.From != "" && !tree.IsAvailable(at.Project, out.Agent) {
		return Answerer{}, fmt.Errorf("%w: @%s answers untagged messages in %s, but it is not available there; tag an agent", ErrNotAvailable, out.Agent, p.whereOf(tenant, tree, at.Project))
	}
	return out, nil
}

// autoFor is @auto for a message a client raised: it picks among the agents
// available where the client files its runs that the client may route to.
// Until clients have a home, that is the root.
func (p *Platform) autoFor(ctx context.Context, c clients.Client, text string) (pick.Choice, error) {
	tree := p.Projects.Tree(c.Tenant)
	at := Place{Project: projects.Root}
	fallback := namedDefault(tree, at)
	if !c.Routes(fallback) {
		fallback = ""
	}
	return p.auto(ctx, c.Tenant, tree, at.Project, text, fallback, c.Routes)
}

// migrateBaseline replaces each workspace's baseline with its root's default,
// and enables every platform agent at a root written before projects had
// agents, so every agent stays available everywhere, as it was. A workspace
// whose settings named a baseline gets that agent as its default, which is
// what the console sent when nobody chose one; one whose settings named none
// gets auto, which keeps the picker for callers that name no agent. Then its
// settings are written again without the baseline. A workspace whose settings
// named none and that has no tree is given none: its root already reads as
// every agent enabled and auto, so the upgrade changes nothing there.
//
// The root is written first. Stopping between the two writes is repaired at
// the next start: the settings still name the baseline, the root already has
// it, so only the settings are written. It runs after migrateProjects, which
// writes the root of a workspace that kept projects in its settings.
func (p *Platform) migrateBaseline() error {
	p.settings.mu.Lock()
	defer p.settings.mu.Unlock()
	tenants := slices.Collect(maps.Keys(p.settings.baselines))
	for _, t := range p.Projects.Tenants() {
		if p.Projects.Tree(t).Root().Agents.Enable == nil && !slices.Contains(tenants, t) {
			tenants = append(tenants, t)
		}
	}
	slices.Sort(tenants)
	w := projects.Writer{By: migration, Govern: true}
	for _, tenant := range tenants {
		baseline, had := p.settings.baselines[tenant]
		// The root is written even when the upgrade leaves it as it reads,
		// so its history says the upgrade set the workspace default. An
		// empty baseline was the picker, which auto still is.
		if had && baseline != "" {
			if err := p.Projects.EnsureRoot(tenant, migration, p.now()); err != nil {
				return fmt.Errorf("platform: write the root of %q: %w", tenant, err)
			}
		}
		root := p.Projects.Tree(tenant).Root()
		next := root.Agents
		if next.Enable == nil {
			next.Enable = []string{projects.Everyone}
		}
		switch {
		case had && baseline != "":
			next.Default = baseline
		case next.Default == "":
			next.Default = Auto
		}
		_, err := p.Projects.SetAgents(tenant, projects.Root, root.Version, next, w, p.now())
		if errors.Is(err, projects.ErrInvalid) && next.Default != Auto {
			// A baseline the tree cannot hold as a default still leaves the
			// workspace with an answer: @auto, as if none had been set.
			p.log.Warn("platform: the baseline cannot be the workspace default; @auto answers instead", "tenant", tenant, "baseline", baseline, "error", err)
			next.Default = Auto
			_, err = p.Projects.SetAgents(tenant, projects.Root, root.Version, next, w, p.now())
		}
		if err != nil {
			return fmt.Errorf("platform: make the baseline of %q the workspace default: %w", tenant, err)
		}
		p.log.Info("platform: the baseline is now the workspace default", "tenant", tenant, "default", next.Default)
		if !had {
			continue
		}
		delete(p.settings.baselines, tenant)
		if err := p.settings.put(tenant, p.settings.by[tenant]); err != nil {
			p.settings.baselines[tenant] = baseline
			return fmt.Errorf("platform: clear the baseline of %q from its settings: %w", tenant, err)
		}
	}
	return nil
}

// AgentsInput is what a person sets on a project's agents, as a whole: an
// entry left out is cleared. Version is the project's, as they read it.
type AgentsInput struct {
	Version int      `json:"version"`
	Enable  []string `json:"enable"`
	Allow   []string `json:"allow,omitempty"`
	Default string   `json:"default,omitempty"`
}

// Offer is one agent available in a project, and where it comes from: the
// nearest project on the chain that enables it, or the project it lives in.
// Agent "*" is every platform agent.
type Offer struct {
	Agent string `json:"agent"`
	// From is the project, by address: "~" is the root.
	From string `json:"from"`
	// Home is set when the agent lives in From.
	Home bool `json:"home,omitempty"`
}

// ProjectAgents is a project's agents as a person reads them: what the
// project sets, what is available there as a result, and who answers a
// message that tags no agent.
type ProjectAgents struct {
	Version int `json:"version"`
	// Enable, Allow and Default are what the project sets (projects.Agents).
	Enable  []string `json:"enable"`
	Allow   []string `json:"allow,omitempty"`
	Default string   `json:"default,omitempty"`
	// Available is what may run here, with Offers saying where each comes
	// from, in the order of Available's names.
	Available projects.Availability `json:"available"`
	Offers    []Offer               `json:"offers"`
	// Homes is the agents that live here.
	Homes []string `json:"homes"`
	// DefaultResolved is who answers an untagged message here, and where
	// that default is set. Nil when nothing does.
	DefaultResolved *Nearer `json:"default_resolved"`
}

// projectAddress is how a project is named in an answer: "~" for the root.
func projectAddress(name string) string {
	if name == projects.Root {
		return projects.RootAddress
	}
	return name
}

// ProjectAgentsOf reads a project's agents. "~" is the root.
func (p *Platform) ProjectAgentsOf(tenant, name string) (ProjectAgents, error) {
	name = projects.NameOf(name)
	tree := p.Projects.Tree(tenant)
	pr, ok := tree.Get(name)
	if !ok {
		return ProjectAgents{}, fmt.Errorf("%w: %q", projects.ErrNotFound, name)
	}
	out := ProjectAgents{
		Version: pr.Version, Enable: pr.Agents.Enable, Allow: pr.Agents.Allow, Default: pr.Agents.Default,
		Available: tree.Available(name), Homes: tree.Homes(name), Offers: []Offer{},
	}
	if out.Enable == nil {
		out.Enable = []string{}
	}
	if out.Homes == nil {
		out.Homes = []string{}
	}
	chain := tree.Chain(name)
	offer := func(agent string) Offer {
		if home, ok := tree.Home(agent); ok {
			return Offer{Agent: agent, From: projectAddress(home), Home: true}
		}
		for _, q := range chain {
			if slices.Contains(q.Agents.Enable, agent) || slices.Contains(q.Agents.Enable, projects.Everyone) {
				return Offer{Agent: agent, From: projectAddress(q.Name)}
			}
		}
		return Offer{Agent: agent, From: projectAddress(name)}
	}
	if out.Available.All {
		o := offer(projects.Everyone)
		o.Agent = projects.Everyone
		out.Offers = append(out.Offers, o)
	}
	for _, a := range out.Available.Names {
		out.Offers = append(out.Offers, offer(a))
	}
	if d := p.DefaultAt(tenant, Place{Project: name}); d.Agent != "" {
		out.DefaultResolved = &d
	}
	return out, nil
}

// SetProjectAgents replaces what a project turns on. Every name must be an
// agent in the workspace, a named default must be available in the project
// under the new lists, and an agent that lives in a project is not enabled,
// since that adds nothing. The root's lists, the workspace default among
// them, and a company project's need projects:govern.
func (p *Platform) SetProjectAgents(ctx context.Context, tenant, name string, w projects.Writer, in AgentsInput) (ProjectAgents, error) {
	name = projects.NameOf(name)
	if err := p.EnsureBuiltins(ctx, tenant); err != nil {
		p.log.Warn("platform: built-in agents", "error", err)
	}
	for _, n := range slices.Concat(in.Enable, in.Allow, []string{in.Default}) {
		if n == "" || n == projects.Everyone || n == Auto {
			continue
		}
		if _, err := p.Agents.Get(ctx, tenant, n, 0); err != nil {
			return ProjectAgents{}, fmt.Errorf("%w: there is no agent called %q", projects.ErrInvalid, n)
		}
	}
	if _, err := p.Projects.SetAgents(tenant, name, in.Version, projects.Agents{Enable: in.Enable, Allow: in.Allow, Default: strings.TrimSpace(in.Default)}, w, p.now()); err != nil {
		return ProjectAgents{}, err
	}
	return p.ProjectAgentsOf(tenant, name)
}

// AgentPlace is where one agent lives and which projects enable it.
type AgentPlace struct {
	// Home is the project it lives in, by address. Empty for a platform
	// agent.
	Home string `json:"home,omitempty"`
	// EnabledIn is the projects, by address, whose enable list names it, or
	// holds "*" for a platform agent, in tree order.
	EnabledIn []string `json:"enabled_in"`
}

// AgentPlaces says, for each agent named, where it lives and which projects
// enable it.
func (p *Platform) AgentPlaces(tenant string, agents []string) map[string]AgentPlace {
	tree := p.Projects.Tree(tenant)
	all := append([]projects.Project{tree.Root()}, tree.Projects()...)
	out := make(map[string]AgentPlace, len(agents))
	for _, a := range agents {
		place := AgentPlace{EnabledIn: []string{}}
		home, homed := tree.Home(a)
		if homed {
			place.Home = projectAddress(home)
		}
		for _, q := range all {
			if slices.Contains(q.Agents.Enable, a) || (!homed && slices.Contains(q.Agents.Enable, projects.Everyone)) {
				place.EnabledIn = append(place.EnabledIn, projectAddress(q.Name))
			}
		}
		out[a] = place
	}
	return out
}

// SetAgentHome makes an agent live in a project: available there and below,
// and nowhere else. "" or "~" makes it a platform agent again, available
// wherever it is enabled. It is refused (projects.ErrInUse, naming them)
// while something outside the new home's subtree would lose the agent: a
// default, an enable or allow list, a schedule filed elsewhere, an alert rule
// watching it elsewhere, or a client that routes to it, since a client files
// its runs at the root. The platform's own agents live nowhere.
func (p *Platform) SetAgentHome(ctx context.Context, tenant, agent, project string, w projects.Writer) error {
	if IsBuiltin(agent) {
		return fmt.Errorf("%w: %s is one of the platform's own agents, which live nowhere and are available wherever they are enabled", projects.ErrInvalid, agent)
	}
	if _, err := p.Agents.Get(ctx, tenant, agent, 0); err != nil {
		return err
	}
	return p.Projects.SetHome(tenant, agent, projects.NameOf(project), w, p.now(), func(before, after *projects.Tree) []string {
		return p.homeUsers(tenant, agent, before, after)
	})
}

// homeUsers names what outside the tree would lose an agent when it moves
// from before to after.
func (p *Platform) homeUsers(tenant, agent string, before, after *projects.Tree) []string {
	lost := func(project string) bool {
		return before.IsAvailable(project, agent) && !after.IsAvailable(project, agent)
	}
	var out []string
	for _, sc := range p.Schedules.List(tenant) {
		if sc.Agent == agent && lost(sc.Meta[runs.MetaProject]) {
			out = append(out, "the schedule "+sc.Name)
		}
	}
	for _, r := range p.Alerts.Rules(tenant) {
		if r.Agent == agent && r.Project != "" && lost(r.Project) {
			out = append(out, "the alert rule "+r.Name)
		}
	}
	for _, surface := range clients.Surfaces() {
		for _, c := range p.Clients.On(surface) {
			if c.Tenant == tenant && (c.DefaultAgent == agent || slices.Contains(c.Agents, agent)) && lost(projects.Root) {
				out = append(out, "the client "+c.ID)
			}
		}
	}
	return out
}
