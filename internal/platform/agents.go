package platform

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/urmzd/orchagent/flow/event"

	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/alerts"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/join"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
)

// Agents in projects: which agents may run where, who answers a message that
// tags none, and where an agent lives (docs/architecture/projects.md, "Agents in
// projects"). The project tree decides availability; the platform asks it
// once, at admission, and again sooner wherever an answer helps a person:
// writing a schedule or a default, and previewing who would join a message.

var (
	// ErrNotAvailable reports an agent that may not run where a run is
	// filed: nothing on the project's chain offers it, or an allow list on
	// the chain leaves it out.
	ErrNotAvailable = errors.New("platform: not available")
	// ErrNoDefault reports a message that tags no agent, in a place where
	// no agent may answer it.
	ErrNoDefault = errors.New("platform: tag an agent")
	// ErrNoTakers reports a message no agent took. A message that names no
	// agent now always lands on one (land), so the platform no longer
	// returns it.
	ErrNoTakers = errors.New("platform: no agent took it")
)

// Open is the default that names no agent: each agent available where an
// untagged message is posted decides for itself whether to take it.
const Open = projects.OpenDefault

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
	if p.answersIn(tenant, tree, project, agent) {
		return nil
	}
	if !tree.Has(project) {
		return fmt.Errorf("%w: %q", ErrProjectNotFound, project)
	}
	return p.notAvailable(tenant, tree, project, agent)
}

// answersIn reports whether an agent may answer in a project: every allow
// list on the project's chain permits it (projects.Tree.IsAvailable), and
// everyone who reads the project may see it. Agents are independent of
// projects, so an agent with no home, or homed where everyone reads, answers
// anywhere; a private one, homed in a project kept to its members (a
// Personal project), answers only where its readers alone read, so what it
// says never reaches someone who may not see it.
func (p *Platform) answersIn(tenant string, tree *projects.Tree, project, agent string) bool {
	return p.answerersIn(tenant, tree, project)(agent)
}

// answerersIn is answersIn for many agents in one project, reading who reads
// the project once.
func (p *Platform) answerersIn(tenant string, tree *projects.Tree, project string) func(agent string) bool {
	seen := p.agentsEveryoneReads(tenant, project)
	return func(agent string) bool { return tree.IsAvailable(project, agent) && seen(agent) }
}

// notAvailable says why an agent may not run in a project the tree holds:
// an allow list on its chain leaves it out, or it is private to readers the
// project has more of. It never says where the agent's home is: the refusal
// reaches whoever reads the place it was made in, and they may not read it.
func (p *Platform) notAvailable(tenant string, tree *projects.Tree, project, agent string) error {
	where := p.whereOf(tenant, tree, project)
	if agent == Open {
		return fmt.Errorf("%w: open is no agent: the agents decide whether to take a message before anything runs", ErrNotAvailable)
	}
	if !tree.IsAvailable(project, agent) {
		return fmt.Errorf("%w: %s is not allowed in %s", ErrNotAvailable, agent, where)
	}
	return fmt.Errorf("%w: %s is private, and not everyone who reads %s may see it", ErrNotAvailable, agent, where)
}

// everyoneIn reports whether everyone in the workspace reads a project: no
// project on its chain is members only.
func everyoneIn(tree *projects.Tree, project string) bool {
	chain := tree.Chain(project)
	return chain != nil && !slices.ContainsFunc(chain, projects.MembersOnly)
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
func (p *Platform) admitAgent(tag event.Envelope, project string) error {
	if wire.Origin(tag) == runs.OriginSystem {
		return nil
	}
	// One of the store's agents the workspace has not installed is refused
	// saying which package to install, wherever the run came from.
	if why := p.NotInstalled(context.Background(), tag.Tenant, tag.Agent); why != "" {
		err := fmt.Errorf("%w: %s", ErrNotAvailable, why)
		p.log.Warn("platform: run refused: agent not installed", "tenant", tag.Tenant, "agent", tag.Agent, "project", project, "origin", wire.Origin(tag), "why", why)
		if schedule := wire.SurfaceOf(tag).CronSchedule; schedule != "" && wire.Origin(tag) == "cron" {
			p.unavailableAlert(tag.Tenant, schedule, tag.Agent, project, err)
		}
		return err
	}
	tree := p.Projects.Tree(tag.Tenant)
	at := project
	if p.answersIn(tag.Tenant, tree, at, tag.Agent) {
		return nil
	}
	if !tree.Has(at) {
		// Deleted between placeOf and here: refused as placeOf refuses it.
		return projectDeleted(at)
	}
	err := p.notAvailable(tag.Tenant, tree, at, tag.Agent)
	p.log.Warn("platform: run refused: agent not available", "tenant", tag.Tenant, "agent", tag.Agent, "project", at, "origin", wire.Origin(tag))
	// A person running a schedule now is told at once; the clock has nobody
	// to tell.
	if schedule := wire.SurfaceOf(tag).CronSchedule; schedule != "" && wire.Origin(tag) == "cron" {
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

// candidates are the agents available in a project that keep passes and
// that take untagged messages, as join asks them (answersIn: a private agent
// is asked only where its readers alone read). keep nil keeps every one. An
// agent whose spec is tagged_only is never asked.
func (p *Platform) candidates(ctx context.Context, tenant string, tree *projects.Tree, project string, keep func(string) bool) ([]join.Candidate, error) {
	answers := p.answerersIn(tenant, tree, project)
	var out []join.Candidate
	asks := p.goodAsks(tenant)
	page := wire.Page{Size: 100}
	for {
		revs, next, err := p.Catalog().List(ctx, tenant, page)
		if err != nil {
			return nil, err
		}
		for _, r := range revs {
			if wire.PolicyOf(r.Spec).TaggedOnly || !answers(r.Spec.Name) || (keep != nil && !keep(r.Spec.Name)) {
				continue
			}
			// A handle from before a rename is the same agent under its old
			// name: where the current one is offered too, only it is. Where
			// only the old handle is allowed, it still answers.
			if current := legacy.BuiltinAgent(r.Spec.Name); current != r.Spec.Name && answers(current) && (keep == nil || keep(current)) {
				continue
			}
			out = append(out, join.Candidate{Name: r.Spec.Name, Description: r.Spec.Description, Prompt: r.Spec.SystemPrompt, Asks: asks[r.Spec.Name]})
			if len(out) == join.MaxCandidates {
				return out, nil
			}
		}
		if next == "" {
			return out, nil
		}
		page.Token = next
	}
}

// asker is what asks a workspace's agents whether to join: a program's own
// (Config.Router), else nobody, so no agent takes a message on its own.
// Routing moves to the entry agent, which picks on its own chain; until then
// only a program, or a test, routes.
func (p *Platform) asker(tenant string) join.Asker {
	if p.cfg.Router != nil {
		return p.cfg.Router(tenant)
	}
	return nil
}

// Route is what would happen to a message posted in a project ("" or "~"
// is the root), without starting anything: each agent available there that
// takes untagged messages is asked whether it would, and its answer is
// returned.
func (p *Platform) Route(ctx context.Context, tenant, project, message string) ([]join.Decision, error) {
	project = projects.NameOf(project)
	tree := p.Projects.Tree(tenant)
	if !tree.Has(project) {
		return nil, fmt.Errorf("%w: %q", projects.ErrNotFound, project)
	}
	cs, err := p.candidates(ctx, tenant, tree, project, nil)
	if err != nil {
		return nil, err
	}
	return routePreview(ctx, p.asker(tenant), cs, message), nil
}

// land is who answers a message that names no agent where nothing names one
// to answer it (no default, or an open one): the entry agent, which answers
// it or calls whoever fits, as it does for a console root. Without an entry
// agent there that keep allows, the agents that take it answer; with none
// taking it, the best match search found; with no match, the workspace's
// named default, then the first agent available. A message never waits for
// a person because no agent took it. keep is who may answer: the agents the
// client the message came through may route to, that its poster may read.
// Only a place with no agent keep allows is refused, as ErrNoDefault.
func (p *Platform) land(ctx context.Context, tenant, project, message string, keep func(string) bool) ([]Answerer, error) {
	project = projects.NameOf(project)
	if entry := p.entryFor(ctx, tenant, project, keep); entry != "" {
		return []Answerer{{Agent: entry}}, nil
	}
	tree := p.Projects.Tree(tenant)
	cs, err := p.candidates(ctx, tenant, tree, project, keep)
	if err != nil {
		return nil, err
	}
	if found := join.Search(cs, message, join.Shortlist); len(found) > 0 {
		joined := join.Joined(join.Route(ctx, p.asker(tenant), join.Candidates(found), message))
		if len(joined) == 0 {
			return []Answerer{{Agent: found[0].Name, Picked: "Picked by search: matched " + strings.Join(found[0].Terms, ", ") + "."}}, nil
		}
		out := make([]Answerer, 0, min(len(joined), maxJoined))
		for _, d := range joined[:min(len(joined), maxJoined)] {
			out = append(out, Answerer{Agent: d.Agent, Picked: joinedReason(d)})
		}
		return out, nil
	}
	if d := tree.Root().Agents.Default; d != "" && d != Open && (keep == nil || keep(d)) && tree.IsAvailable(project, d) {
		return []Answerer{{Agent: d, Picked: "No agent matched it, so the workspace default answered."}}, nil
	}
	if len(cs) > 0 {
		return []Answerer{{Agent: cs[0].Name, Picked: "No agent matched it, so the first agent available here answered."}}, nil
	}
	return nil, fmt.Errorf("%w: no agent may answer it in %s", ErrNoDefault, p.whereOf(tenant, tree, project))
}

// maxJoined bounds how many agents take one message.
const maxJoined = 5

// joinedReason is what a run started by joining says about why.
func joinedReason(d join.Decision) string {
	if d.Reason == "" {
		return "Joined on its own."
	}
	return "Joined on its own: " + d.Reason
}

// Nearer is a default set nearer a message than its project: a thread's, a
// client's or a channel's, and where it is set ("thread", "client",
// "channel").
type Nearer struct {
	Agent string `json:"agent"`
	From  string `json:"from"`
}

// Place is where a message is posted, for deciding who answers it, and
// where anything else lives, for deciding who may read it (Sees).
type Place struct {
	// Project is where the message's conversation is filed. "" is the root.
	Project string
	// Nearer are the defaults set nearer than the project, nearest first.
	Nearer []Nearer
	// Thread is the conversation it belongs to, or "".
	Thread string
}

// Answerer is who answers a message, and how they were chosen.
type Answerer struct {
	Agent string
	// Picked is why the agent joined, when it took the message on its own.
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
// set: the first set of the place's nearer defaults (the thread's, then the
// client's), then the workspace's. A project chooses no default: any agent
// answers in any project. It is the zero Nearer when none is set.
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

// resolveAgents decides who answers a message in a place. asked is the
// agent the message tags, Open, or "" for none.
//
// A tagged agent answers alone. With none tagged, the default the place
// inherits answers. With none set, a default of open, and Open asked, the
// message lands (land): the entry agent answers it, and without one the
// agents that take it, else the best match. A named default that has
// stopped being available is not passed over for the next one along: it is
// ErrNotAvailable, saying why. Whether a tagged agent is available is
// admission's to say. routes, when set, is the client's list a surface
// sends the message through: only agents on it answer.
func (p *Platform) resolveAgents(ctx context.Context, tenant string, at Place, asked, text string, routes func(string) bool) ([]Answerer, error) {
	tree := p.Projects.Tree(tenant)
	out := Answerer{Agent: asked}
	if asked == "" {
		d := p.DefaultAt(tenant, at)
		out.Agent, out.From = d.Agent, d.From
	}
	if out.Agent == "" || out.Agent == Open {
		answer, err := p.land(ctx, tenant, at.Project, text, routes)
		if err != nil {
			return nil, err
		}
		for i := range answer {
			answer[i].From = out.From
		}
		return answer, nil
	}
	if out.From != "" && !p.answersIn(tenant, tree, at.Project, out.Agent) {
		return nil, fmt.Errorf("%w: @%s answers untagged messages in %s, but it is not available there; tag an agent", ErrNotAvailable, out.Agent, p.whereOf(tenant, tree, at.Project))
	}
	return []Answerer{out}, nil
}

// landFor is land for a message a client raised, where the client files its
// run (its bound channel, or its home): only agents the client may route to
// answer.
func (p *Platform) landFor(ctx context.Context, c clients.Client, at Place, text string) ([]Answerer, error) {
	return p.land(ctx, c.Tenant, at.Project, text, c.Routes)
}

// migrateBaseline replaces each workspace's baseline with its root's default,
// and enables every platform agent at a root written before projects had
// agents, so every agent stays available everywhere, as it was. A workspace
// whose settings named a baseline gets that agent as its default, which is
// what the console sent when nobody chose one; one whose settings named none
// gets everyone ("*"), which lets agents decide for callers that name no agent. Then its
// settings are written again without the baseline. A workspace whose settings
// named none and that has no tree is given none: its root already reads as
// every agent enabled and auto, so the upgrade changes nothing there.
//
// The root is written first. Stopping between the two writes is repaired at
// the next start: the settings still name the baseline, the root already has
// it, so only the settings are written. It runs after migrateProjects, which
// writes the root of a workspace that kept projects in its settings.
func (p *Platform) migrateBaseline() error {
	_, baselines, done, err := p.settings.leftovers()
	if err != nil || done {
		return err
	}
	tenants := slices.Collect(maps.Keys(baselines))
	err = p.Projects.EachTenant(func(t string) error {
		tree, err := p.Projects.Read(t)
		if err != nil {
			return err
		}
		if tree.Root().Agents.Enable == nil && !slices.Contains(tenants, t) {
			tenants = append(tenants, t)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("platform: read the workspaces' roots: %w", err)
	}
	slices.Sort(tenants)
	w := projects.Writer{By: migration, Govern: true}
	for _, tenant := range tenants {
		baseline, had := baselines[tenant]
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
			next.Default = Open
		}
		_, err := p.Projects.SetAgents(tenant, projects.Root, root.Version, next, w, p.now())
		if errors.Is(err, projects.ErrInvalid) && next.Default != Open {
			// A baseline the tree cannot hold as a default still leaves the
			// workspace with an answer: everyone decides, as if none had been set.
			p.log.Warn("platform: the baseline cannot be the workspace default; agents decide instead", "tenant", tenant, "baseline", baseline, "error", err)
			next.Default = Open
			_, err = p.Projects.SetAgents(tenant, projects.Root, root.Version, next, w, p.now())
		}
		if err != nil {
			// A process starting at the same time may have written the root
			// first: then it is done here.
			if now := p.Projects.Tree(tenant).Root(); now.Version == root.Version || now.Agents.Enable == nil {
				return fmt.Errorf("platform: make the baseline of %q the workspace default: %w", tenant, err)
			}
		} else {
			p.log.Info("platform: the baseline is now the workspace default", "tenant", tenant, "default", next.Default)
		}
		if !had {
			continue
		}
		err = p.settings.update(tenant, func(v *storedSettings) error {
			if v.LegacyBaseline == nil {
				return state.ErrUnchanged
			}
			v.LegacyBaseline = nil
			return nil
		})
		if err != nil {
			return fmt.Errorf("platform: clear the baseline of %q from its settings: %w", tenant, err)
		}
	}
	return p.settings.migrated()
}

// AgentsInput is what a person sets on a project's agents, as a whole: an
// entry left out is cleared. Version is the project's, as they read it.
type AgentsInput struct {
	Version int      `json:"version"`
	Enable  []string `json:"enable"`
	Allow   []string `json:"allow,omitempty"`
	Default string   `json:"default,omitempty"`
}

// Offer was one agent a project offered, and where it came from. Projects
// no longer offer agents, so an answer lists none; the type is kept for the
// API's shape until the console stops reading it.
type Offer struct {
	Agent string `json:"agent"`
	// From is the project, by address: "~" is the root.
	From string `json:"from"`
	// Home was set when the agent lived in From.
	Home bool `json:"home,omitempty"`
}

// ProjectAgents is a project's agents as a person reads them: what the
// project sets, what is available there as a result, and who answers a
// message that tags no agent.
type ProjectAgents struct {
	Version int `json:"version"`
	// Enable, Allow and Default are what the project sets (projects.Agents).
	Enable  []string `json:"enable"`
	Allow   []string `json:"allow,omitzero"`
	Default string   `json:"default,omitempty"`
	// Available is what may run here: every agent the allow lists permit.
	// Offers is always empty: projects no longer offer agents.
	Available projects.Availability `json:"available"`
	Offers    []Offer               `json:"offers"`
	// Homes is the agents shared with this project's readers.
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
		Available: tree.Available(name), Homes: CurrentAgentNames(tree.Homes(name)), Offers: []Offer{},
	}
	out.Available.Names = CurrentAgentNames(out.Available.Names)
	if out.Enable == nil {
		out.Enable = []string{}
	}
	if out.Homes == nil {
		out.Homes = []string{}
	}
	if d := p.DefaultAt(tenant, Place{Project: name}); d.Agent != "" {
		out.DefaultResolved = &d
	}
	return out, nil
}

// SetProjectAgents replaces a project's agent policy: its allow list, and on
// the root the workspace default. Every name must be an agent in the
// workspace, and the workspace default must be allowed at the root. An
// enable list and a project's own default are kept as written and not read.
// The root's policy and a company project's need projects:govern.
func (p *Platform) SetProjectAgents(ctx context.Context, tenant, name string, w projects.Writer, in AgentsInput) (ProjectAgents, error) {
	name = projects.NameOf(name)
	if err := p.EnsureBuiltins(ctx, tenant); err != nil {
		p.log.Warn("platform: built-in agents", "error", err)
	}
	for _, n := range slices.Concat(in.Enable, in.Allow, []string{in.Default}) {
		if n == "" || n == projects.Everyone || n == Open {
			continue
		}
		if _, err := p.Catalog().Get(ctx, tenant, n, 0); err != nil {
			return ProjectAgents{}, fmt.Errorf("%w: there is no agent called %q", projects.ErrInvalid, n)
		}
	}
	if _, err := p.Projects.SetAgents(tenant, name, in.Version, projects.Agents{Enable: in.Enable, Allow: in.Allow, Default: strings.TrimSpace(in.Default)}, w, p.now()); err != nil {
		return ProjectAgents{}, err
	}
	return p.ProjectAgentsOf(tenant, name)
}

// AgentPlace is who sees one agent, as its home.
type AgentPlace struct {
	// Home is the project whose readers see it, by address: "~" for the
	// whole workspace. Empty for a platform agent.
	Home string `json:"home,omitempty"`
	// EnabledIn is always empty: projects no longer enable agents. It is
	// kept for the API's shape until the console stops reading it.
	EnabledIn []string `json:"enabled_in"`
}

// AgentPlaces says, for each agent named, who sees it.
func (p *Platform) AgentPlaces(tenant string, agents []string) map[string]AgentPlace {
	tree := p.Projects.Tree(tenant)
	out := make(map[string]AgentPlace, len(agents))
	for _, a := range agents {
		place := AgentPlace{EnabledIn: []string{}}
		if home, homed := tree.Home(a); homed {
			place.Home = projectAddress(home)
		}
		out[a] = place
	}
	return out
}

// SetAgentHome sets who sees an agent, as its home: a Personal project keeps
// it to that person, another project to that project's readers, and "" or
// "~" to the whole workspace. A home decides nothing about where the agent
// answers: any agent answers in any project its allow lists permit. It is
// refused (projects.ErrInUse) while the agent's default skills would not fit
// the new home. The platform's own agents are the workspace's.
func (p *Platform) SetAgentHome(ctx context.Context, tenant, agent, project string, w projects.Writer) error {
	unlock, err := p.lockSetupRevision(tenant, agent)
	if err != nil {
		return err
	}
	defer unlock()
	return p.setAgentHome(ctx, tenant, agent, project, w)
}

// setAgentHome is also used by the setup flow while it holds the same lock.
func (p *Platform) setAgentHome(ctx context.Context, tenant, agent, project string, w projects.Writer) error {
	p.skillDefaultsMu.Lock()
	defer p.skillDefaultsMu.Unlock()
	if IsBuiltin(agent) && projects.NameOf(project) != projects.Root {
		return fmt.Errorf("%w: built-in agents are seen by the whole workspace", projects.ErrInvalid)
	}
	if _, err := p.Catalog().Get(ctx, tenant, agent, 0); err != nil {
		return err
	}
	return p.Projects.SetHome(tenant, agent, projects.NameOf(project), w, p.now(), func(before, after *projects.Tree) []string {
		return p.homeUsers(tenant, agent, before, after)
	})
}

// homeUsers names what a home change would leave out of place: the agent's
// default skills, which are scoped to who sees the agent. Schedules, alert
// rules and clients that use the agent hold no change back, since where the
// agent answers does not change.
func (p *Platform) homeUsers(tenant, agent string, _, after *projects.Tree) []string {
	if home, ok := after.Home(agent); ok && !p.defaultSkillsFitHome(tenant, agent, home, after) {
		return []string{"the agent default skills"}
	}
	return nil
}

// Catalog is where the platform reads agent definitions.
func (p *Platform) Catalog() wire.AgentCatalog { return p.Agents }
