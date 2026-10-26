package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/urmzd/fiber/pkg/agentspec"
	"github.com/urmzd/fiber/pkg/registry"
	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/memory"
	"github.com/urmzd/humagen/pkg/projectforks"
	"github.com/urmzd/humagen/pkg/projects"
)

// ProjectForkInput selects the destination and explicitly confirmed context.
type ProjectForkInput struct {
	Name    string   `json:"name"`
	Parent  string   `json:"parent,omitempty"`
	Title   string   `json:"title,omitempty"`
	Memory  []string `json:"memory,omitempty"`
	Sources []string `json:"sources,omitempty"`
}

type ProjectForkOperation struct {
	ID          string `json:"id"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	State       string `json:"state"`
	Error       string `json:"error,omitempty"`
	Projects    int    `json:"projects"`
	Agents      int    `json:"agents"`
	Memory      int    `json:"memory"`
}

func (p *Platform) projectForkView(tenant string, record projectforks.Record) ProjectForkOperation {
	state := "preparing"
	if record.Error != "" {
		state = "failed"
	}
	pr, err := p.Projects.Get(tenant, record.Plan.Destination)
	if err == nil && pr.Fork != nil && pr.Fork.Operation == record.ID && pr.Fork.Ready {
		state = "ready"
	}
	view := ProjectForkOperation{ID: record.ID, Source: record.Plan.Source, Destination: record.Plan.Destination, State: state, Projects: len(record.Plan.Projects), Agents: len(record.Plan.Agents), Memory: len(record.Plan.Notes)}
	if state != "ready" {
		view.Error = record.Error
	}
	return view
}

// ProjectFork is an exact-person read, including before staged resources exist.
func (p *Platform) ProjectFork(who auth.Principal, destination string) (ProjectForkOperation, error) {
	if who.Kind != auth.User {
		return ProjectForkOperation{}, projectforks.ErrNotFound
	}
	record, err := p.ProjectForks.Get(who.Tenant, destination, who.Subject)
	if err != nil {
		return ProjectForkOperation{}, err
	}
	view := p.projectForkView(who.Tenant, record)
	if !p.Seer(who).Project(record.Plan.Source) {
		view.Source = ""
	}
	return view, nil
}

// ForkProject resumes a saved plan, or captures a new plan only after source
// and destination access checks. A stopped copy remains hidden and unrunnable.
func (p *Platform) ForkProject(ctx context.Context, who auth.Principal, source string, in ProjectForkInput) (view ProjectForkOperation, resultErr error) {
	var attempt *AuditAttempt
	var observed error
	defer func() {
		if attempt != nil {
			outcome := resultErr
			if observed != nil {
				outcome = observed
			}
			_ = attempt.Finish(outcome)
		}
	}()
	begin := func() error {
		var err error
		attempt, err = p.BeginAudit(who, audit.Change{Action: "project.fork", Resource: "project_fork", ID: in.Name, Scope: "personal", Subject: who.Subject, Project: in.Name})
		return err
	}
	if _, _, err := p.ProjectRole(who, source, projects.Viewer); err != nil {
		return ProjectForkOperation{}, err
	}
	if who.Kind != auth.User || !who.Can(auth.ProjectsWrite) {
		return ProjectForkOperation{}, ErrForbidden
	}
	seenMemory := map[string]bool{}
	for _, id := range in.Memory {
		if seenMemory[id] {
			return ProjectForkOperation{}, projects.ErrInvalid
		}
		seenMemory[id] = true
	}
	if source == "" || source == "~" || !projects.ValidName(in.Name) || source == in.Name || len(in.Memory) > projectforks.MaxNotes || len(in.Sources) > projects.MaxRunKnowledge {
		return ProjectForkOperation{}, projects.ErrInvalid
	}
	if _, _, err := p.ProjectRole(who, source, projects.Viewer); err != nil {
		return ProjectForkOperation{}, err
	}
	in.Parent = projects.NameOf(in.Parent)
	if err := p.MayFileIn(who, in.Parent); err != nil {
		return ProjectForkOperation{}, err
	}
	mu := p.moves.of(who.Tenant, "project-fork:"+in.Name)
	mu.Lock()
	defer mu.Unlock()
	record, err := p.ProjectForks.Get(who.Tenant, in.Name, who.Subject)
	if err != nil {
		if !errors.Is(err, projectforks.ErrNotFound) {
			return ProjectForkOperation{}, err
		}
		plan, err := p.planProjectFork(ctx, who, source, in)
		if err != nil {
			return ProjectForkOperation{}, err
		}
		if err := begin(); err != nil {
			return ProjectForkOperation{}, err
		}
		record, err = p.ProjectForks.Start(who.Tenant, who.Subject, plan, p.now())
		if err != nil {
			return ProjectForkOperation{}, err
		}
	} else if record.Plan.Source != source || record.Plan.Parent != in.Parent {
		return ProjectForkOperation{}, projectforks.ErrNotFound
	}
	if p.projectForkView(who.Tenant, record).State == "ready" {
		return p.projectForkView(who.Tenant, record), nil
	}
	// Revalidate the saved plan after revocation, without replacing its snapshot.
	if len(record.Plan.Agents) > 0 && (!who.Can(auth.AgentsRead) || !who.Can(auth.AgentsWrite)) {
		return ProjectForkOperation{}, ErrForbidden
	}
	seer := p.Seer(who)
	for _, copy := range record.Plan.Projects {
		if !seer.Project(copy.Source) {
			return ProjectForkOperation{}, projects.ErrNotFound
		}
	}
	for _, copy := range record.Plan.Projects {
		if len(seer.Sources(copy.Config.Knowledge)) != len(copy.Config.Knowledge) {
			return ProjectForkOperation{}, projects.ErrNotFound
		}
	}
	if attempt == nil {
		if err := begin(); err != nil {
			return ProjectForkOperation{}, err
		}
	}
	err = p.copyProjectFork(ctx, who, record)
	observed = err
	if err != nil {
		p.log.Error("platform: project fork stopped before publication", "operation", record.ID, "error", err)
	}
	// A failure marker never exposes a partially copied resource. The ready
	// flags in the project store are the single durable publication decision.
	if save := p.ProjectForks.Failure(who.Tenant, in.Name, who.Subject, err, p.now()); save != nil && err == nil {
		if p.projectForkView(who.Tenant, record).State != "ready" {
			return ProjectForkOperation{}, save
		}
	}
	if err != nil {
		record.Error = "Copy stopped before publication. Retry to continue."
	}
	return p.projectForkView(who.Tenant, record), nil
}

func (p *Platform) planProjectFork(ctx context.Context, who auth.Principal, source string, in ProjectForkInput) (projectforks.Plan, error) {
	plan := projectforks.Plan{Source: source, Destination: in.Name, Parent: in.Parent, Projects: []projectforks.ProjectCopy{}, Agents: []projectforks.AgentCopy{}, Notes: []projectforks.NoteCopy{}}
	tree := p.Projects.Tree(who.Tenant)
	if tree.Has(in.Name) {
		return plan, projects.ErrExists
	}
	names := tree.Subtree(source)
	seer := p.Seer(who)
	names = slices.DeleteFunc(names, func(name string) bool { return !seer.Project(name) })
	if len(names) == 0 || len(names) > projectforks.MaxProjects {
		return plan, projects.ErrLimit
	}
	if len(seer.Sources(in.Sources)) != len(in.Sources) {
		return plan, projects.ErrNotFound
	}
	known := map[string]bool{}
	for _, src := range p.Knowledge.List(who.Tenant) {
		known[src.Name] = true
	}
	for _, name := range in.Sources {
		if !known[name] {
			return plan, projects.ErrNotFound
		}
	}
	mapped := map[string]string{source: in.Name}
	for _, name := range names {
		if name != source {
			mapped[name] = in.Name + "-" + name
		}
	}
	parentDepth := len(tree.Chain(in.Parent))
	sourceDepth := len(tree.Chain(source))
	for _, name := range names {
		if !projects.ValidName(mapped[name]) || parentDepth+len(tree.Chain(name))-sourceDepth > projects.DefaultCaps().Depth {
			return plan, projects.ErrInvalid
		}
		if tree.Has(mapped[name]) {
			return plan, projects.ErrExists
		}
	}
	agentNames := map[string]string{}
	for _, name := range names {
		for _, agent := range tree.Homes(name) {
			if !seer.Agent(agent) {
				continue
			}
			if !who.Can(auth.AgentsRead) || !who.Can(auth.AgentsWrite) {
				return plan, ErrForbidden
			}
			dest := projectForkAgentName(in.Name, agent)
			if !projects.ValidAgent(dest) {
				return plan, projects.ErrInvalid
			}
			if _, err := p.Agents.Get(ctx, who.Tenant, dest, 0); !errors.Is(err, registry.ErrNotFound) {
				if err != nil {
					return plan, err
				}
				return plan, projects.ErrExists
			}
			agentNames[agent] = dest
			rev, err := p.Agents.Get(ctx, who.Tenant, agent, 0)
			if err != nil {
				return plan, err
			}
			plan.Agents = append(plan.Agents, projectforks.AgentCopy{Source: agent, Name: dest, Project: mapped[name], Spec: rev.Spec})
		}
	}
	if len(plan.Agents) > projectforks.MaxAgents {
		return plan, projects.ErrLimit
	}
	for i := range plan.Agents {
		cp := &plan.Agents[i]
		cp.Spec.Name = cp.Name
		cp.Spec.Access.Spawn = slices.Clone(cp.Spec.Access.Spawn)
		for j, name := range cp.Spec.Access.Spawn {
			if to, ok := agentNames[name]; ok {
				cp.Spec.Access.Spawn[j] = to
			}
		}
		cp.Spec.Approvals = slices.Clone(cp.Spec.Approvals)
		for j, name := range cp.Spec.Approvals {
			if to, ok := agentNames[strings.TrimPrefix(name, "tag_")]; ok && strings.HasPrefix(name, "tag_") {
				cp.Spec.Approvals[j] = "tag_" + to
			}
		}
		// Connector options can contain credential material. Only explicitly
		// selected knowledge references cross; credentials are reconnected later.
		cp.Spec.Sources = nil
		for _, name := range in.Sources {
			cp.Spec.Sources = append(cp.Spec.Sources, agentspec.Source{Name: name, URI: sourceURI(name)})
		}
		cp.Spec = cp.Spec.Normalize()
	}
	for _, name := range names {
		config, _ := tree.Get(name)
		if config.PersonalOwner != "" {
			return plan, ErrForbidden
		}
		channels, err := p.Projects.Channels(who.Tenant, name)
		if err != nil {
			return plan, err
		}
		for i := range channels {
			channels[i].Project = mapped[name]
			channels[i].Slack = nil
			channels[i].BoundAt = time.Time{}
			channels[i].Default = remapForkAgent(channels[i].Default, agentNames)
		}
		config.Knowledge = slices.Clone(in.Sources)
		config.Agents.Default = remapForkAgent(config.Agents.Default, agentNames)
		config.Agents.Enable = slices.DeleteFunc(slices.Clone(config.Agents.Enable), func(agent string) bool { _, copied := agentNames[agent]; return copied })
		config.Agents.Allow = slices.Clone(config.Agents.Allow)
		for i, agent := range config.Agents.Allow {
			config.Agents.Allow[i] = remapForkAgent(agent, agentNames)
		}
		parent := mapped[config.Parent]
		if name == source {
			parent = in.Parent
			if in.Title != "" {
				config.Title = in.Title
			}
		}
		plan.Projects = append(plan.Projects, projectforks.ProjectCopy{Source: name, Name: mapped[name], Parent: parent, Config: config, Channels: channels})
	}
	notes := p.Memory.List(who.Tenant, memory.ProjectOwner(source))
	for _, id := range in.Memory {
		found := false
		for _, note := range notes {
			if note.ID == id && note.Status == memory.Approved {
				plan.Notes = append(plan.Notes, projectforks.NoteCopy{Project: in.Name, SourceID: id, Text: note.Text})
				found = true
				break
			}
		}
		if !found {
			return plan, projects.ErrNotFound
		}
	}
	return plan, nil
}
func remapForkAgent(name string, names map[string]string) string {
	if to, ok := names[name]; ok {
		return to
	}
	return name
}

func (p *Platform) copyProjectFork(ctx context.Context, who auth.Principal, record projectforks.Record) error {
	w := WriterOf(who)
	for _, copy := range record.Plan.Projects {
		if current, err := p.Projects.Get(who.Tenant, copy.Name); err == nil {
			if current.Fork == nil || current.Fork.Operation != record.ID {
				return projects.ErrExists
			}
			continue
		} else if !errors.Is(err, projects.ErrNotFound) {
			return err
		}
		config := copy.Config
		config.Name, config.Parent = copy.Name, copy.Parent
		if config.Kind == projects.Company {
			config.Kind = projects.Team
		}
		config.PersonalOwner = ""
		config.Archived = false
		config.Managed = ""
		config.FileOwns = nil
		config.Members = []projects.Entry{{Who: who.Subject, Role: projects.Owner}}
		config.Experts = nil
		config.Agents = projects.Agents{}
		config.Fork = &projects.ForkOrigin{Operation: record.ID, Source: copy.Source, By: who.Subject, At: record.At}
		if _, err := p.Projects.Create(who.Tenant, config, w, p.now()); err != nil {
			return err
		}
	}
	for _, copy := range record.Plan.Agents {
		// Reservation is durable before registry creation, including on restart.
		if err := p.Projects.SetHome(who.Tenant, copy.Name, copy.Project, w, p.now(), nil); err != nil {
			return err
		}
		current, err := p.Agents.Get(ctx, who.Tenant, copy.Name, 0)
		if err == nil {
			if !reflect.DeepEqual(current.Spec.Normalize(), copy.Spec.Normalize()) {
				return registry.ErrConflict
			}
			continue
		}
		if !errors.Is(err, registry.ErrNotFound) {
			return err
		}
		if _, err := p.Agents.Create(ctx, who.Tenant, copy.Spec, who.Subject); err != nil {
			return err
		}
	}
	for _, copy := range record.Plan.Projects {
		for _, channel := range copy.Channels {
			current, err := p.Projects.Channel(who.Tenant, copy.Name, channel.Name)
			if err == nil && (channel.Name != projects.General || current.Version > 0) {
				continue
			}
			channel.Managed = ""
			channel.Archived = false
			channel.Slack = nil
			if channel.Name == projects.General {
				_, err = p.Projects.UpdateChannel(who.Tenant, copy.Name, channel.Name, current.Version, w, p.now(), func(c *projects.Channel) error {
					c.Title = channel.Title
					c.Purpose = channel.Purpose
					c.Default = channel.Default
					return nil
				})
			} else {
				_, err = p.Projects.CreateChannel(who.Tenant, channel, w, p.now())
			}
			if err != nil {
				return err
			}
		}
		current, err := p.Projects.Get(who.Tenant, copy.Name)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(current.Agents, copy.Config.Agents) {
			if _, err := p.Projects.SetAgents(who.Tenant, copy.Name, current.Version, copy.Config.Agents, w, p.now()); err != nil {
				return err
			}
		}
		current, err = p.Projects.Get(who.Tenant, copy.Name)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(current.Experts, copy.Config.Experts) {
			if _, err = p.Projects.SetExperts(who.Tenant, copy.Name, current.Version, copy.Config.Experts, w, p.now()); err != nil {
				return err
			}
		}
	}
	for _, note := range record.Plan.Notes {
		if _, err := p.Memory.Add(who.Tenant, memory.ProjectOwner(note.Project), note.Text, memory.Person, memory.Approved, who.Subject, p.now()); err != nil {
			return err
		}
	}
	for _, copy := range record.Plan.Projects {
		if !p.Seer(who).Project(copy.Source) {
			return projects.ErrNotFound
		}
		if len(p.Seer(who).Sources(copy.Config.Knowledge)) != len(copy.Config.Knowledge) {
			return projects.ErrNotFound
		}
	}
	names := make([]string, 0, len(record.Plan.Projects))
	for _, copy := range record.Plan.Projects {
		names = append(names, copy.Name)
	}
	return p.Projects.CompleteFork(who.Tenant, record.ID, names, w, p.now())
}

func projectForkAgentName(project, agent string) string {
	raw := strings.ToLower(project + "-" + agent)
	slug := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, raw)
	if len(slug) > 64 || slug != raw {
		sum := sha256.Sum256([]byte(raw))
		if len(slug) > 55 {
			slug = slug[:55]
		}
		slug += "-" + hex.EncodeToString(sum[:4])
	}
	return slug
}
