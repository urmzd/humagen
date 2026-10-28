package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"

	"github.com/urmzd/fiber/pkg/envelope"
	"github.com/urmzd/fiber/pkg/runtime"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/library"
	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
)

type AgentSkillRef = library.SkillRef
type AgentSkillDefaults = library.AgentDefaults
type AgentSkillsView = library.AgentDefaults

const metaDefaultSkills = "_humagen.skills.defaults"
const metaDefaultSkillRevision = "_humagen.skills.agent_revision"

func (p *Platform) AgentSkills(tenant, agent string) (AgentSkillsView, error) {
	rev, err := p.Catalog().Get(context.Background(), tenant, agent, 0)
	if err != nil {
		return AgentSkillsView{}, library.ErrNotFound
	}
	v := p.AgentSkillDefaults.Get(tenant, rev.Spec.Name)
	if v.Version == 0 {
		v.AgentRevision = rev.Rev
	}
	return v, nil
}

// ValidateAgentSkills is read-only and may be used while admission is held.
func (p *Platform) ValidateAgentSkills(who auth.Principal, project string, refs []AgentSkillRef) ([]AgentSkillRef, error) {
	if !library.ValidRefs(refs) {
		return nil, library.ErrInvalid
	}
	if len(refs) > 0 && !who.Can(auth.ProjectsRead) {
		return nil, projects.ErrForbidden
	}
	project = projects.NameOf(project)
	tree := p.Projects.Tree(who.Tenant)
	if !p.Seer(who).Project(project) {
		return nil, library.ErrNotFound
	}
	for _, ref := range refs {
		latest, err := p.LibrarySkill(who, ref.ID)
		if err != nil {
			return nil, library.ErrNotFound
		}
		exact, err := p.SkillLibrary.Get(who.Tenant, latest.Scope, latest.Name, ref.Version)
		if err != nil {
			return nil, library.ErrNotFound
		}
		if exact.PublishedAt.IsZero() {
			return nil, fmt.Errorf("%w: select a published skill revision", library.ErrInvalid)
		}
		if !skillScopeAt(tree, exact.Scope, project, who.Subject) {
			return nil, fmt.Errorf("%w: skill is outside the agent home", library.ErrInvalid)
		}
	}
	return slices.Clone(refs), nil
}
func skillScopeAt(tree *projects.Tree, scope library.Scope, project, person string) bool {
	if !tree.Has(project) {
		return false
	}
	switch scope.Kind {
	case "workspace":
		return true
	case "project":
		return onChain(tree, project, scope.Project)
	case "personal":
		home, ok := tree.Get(project)
		return ok && person != "" && scope.Subject == person && home.PersonalOwner == person
	}
	return false
}
func (p *Platform) SetAgentSkills(who auth.Principal, agent string, agentRevision, expectedVersion int, refs []AgentSkillRef) (AgentSkillsView, error) {
	unlock, err := p.lockSetupRevision(who.Tenant, agent)
	if err != nil {
		return AgentSkillsView{}, err
	}
	defer unlock()
	if !p.agentReady(who.Tenant, agent) {
		return AgentSkillsView{}, library.ErrConflict
	}
	return p.setAgentSkills(who, agent, agentRevision, expectedVersion, refs)
}

// setAgentSkills is also used by the durable setup phase. It never takes the
// setup mutex; the caller must already own that pending setup capability.
func (p *Platform) setAgentSkills(who auth.Principal, agent string, agentRevision, expectedVersion int, refs []AgentSkillRef) (AgentSkillsView, error) {
	if !who.Can(auth.AgentsWrite) {
		return AgentSkillsView{}, projects.ErrForbidden
	}
	p.skillDefaultsMu.Lock()
	defer p.skillDefaultsMu.Unlock()
	p.admission.Lock()
	defer p.admission.Unlock()
	rev, err := p.Catalog().Get(context.Background(), who.Tenant, agent, 0)
	if err != nil || !p.Seer(who).Agent(agent) {
		return AgentSkillsView{}, library.ErrNotFound
	}
	if rev.Rev != agentRevision {
		return AgentSkillsView{}, library.ErrConflict
	}
	tree := p.Projects.Tree(who.Tenant)
	home, ok := tree.Home(agent)
	if !ok {
		return AgentSkillsView{}, library.ErrConflict
	}
	if err := p.ownsHome(tree, who, agent); err != nil {
		return AgentSkillsView{}, err
	}

	current := p.AgentSkillDefaults.Get(who.Tenant, rev.Spec.Name)
	for _, ref := range current.Skills {
		if !who.Can(auth.ProjectsRead) {
			return AgentSkillsView{}, projects.ErrForbidden
		}
		if _, err := p.LibrarySkill(who, ref.ID); err != nil {
			return AgentSkillsView{}, library.ErrNotFound
		}
	}
	refs, err = p.ValidateAgentSkills(who, home, refs)
	if err != nil {
		return AgentSkillsView{}, err
	}
	ch, err := p.Projects.Channel(who.Tenant, home, projects.General)
	if err != nil {
		return AgentSkillsView{}, err
	}
	change := audit.Change{Action: "agent.skills", Resource: "agent", ID: agent, Agent: agent, Project: home, Scope: "project", Rule: string(auth.AgentsWrite), After: map[string]string{"agent_revision": strconv.Itoa(agentRevision), "skill_count": strconv.Itoa(len(refs))}}
	if project, ok := tree.Get(home); ok && project.PersonalOwner != "" {
		change.Scope = "personal"
		change.Subject = project.PersonalOwner
	}
	attempt, err := p.BeginAudit(who, change)
	if err != nil {
		return AgentSkillsView{}, err
	}
	var saved AgentSkillsView
	err = p.Projects.GuardSnapshot(who.Tenant, tree, ch, func() error {
		var err error
		saved, err = p.AgentSkillDefaults.Set(who.Tenant, rev.Spec.Name, agentRevision, expectedVersion, refs)
		return err
	})
	if completion := attempt.Finish(err); completion != nil {
		p.LogAuditCompletionFailure(completion)
	}
	return saved, err
}
func (p *Platform) defaultSkillsFitHome(tenant, agent, project string, tree *projects.Tree) bool {
	for _, ref := range p.AgentSkillDefaults.Get(tenant, wire.NormalizeName(agent)).Skills {
		latest, err := p.SkillLibrary.Find(tenant, ref.ID)
		if err != nil {
			return false
		}
		exact, err := p.SkillLibrary.Get(tenant, latest.Scope, latest.Name, ref.Version)
		if err != nil {
			return false
		}
		person := ""
		if pr, ok := tree.Get(project); ok {
			person = pr.PersonalOwner
		}
		if !skillScopeAt(tree, exact.Scope, project, person) {
			return false
		}
	}
	return true
}
func (p *Platform) pinAgentSkillDefaults(ctx context.Context, tag envelope.Tag, edge call.Edge, project string) (envelope.Tag, error) {
	// Never believe a caller-supplied defaults marker.
	delete(tag.Meta, metaDefaultSkills)
	delete(tag.Meta, metaDefaultSkillRevision)
	defaults := p.AgentSkillDefaults.Get(tag.Tenant, wire.NormalizeName(tag.Agent))
	if defaults.Version == 0 || len(defaults.Skills) == 0 {
		return tag, nil
	}
	rev, err := p.Catalog().Get(ctx, tag.Tenant, tag.Agent, 0)
	if err != nil {
		return tag, err
	}
	if defaults.AgentRevision != rev.Rev {
		return tag, fmt.Errorf("%w: review skills for the current agent revision", library.ErrConflict)
	}
	tree := p.Projects.Tree(tag.Tenant)
	home, ok := tree.Home(tag.Agent)
	if !ok {
		return tag, library.ErrNotFound
	}
	person := p.operationTagPerson(tag, edge)
	var skills []pinnedSkill
	if raw := tag.Meta[runs.MetaSkills]; raw != "" {
		if json.Unmarshal([]byte(raw), &skills) != nil {
			return tag, library.ErrInvalid
		}
	}
	for _, ref := range defaults.Skills {
		latest, err := p.SkillLibrary.Find(tag.Tenant, ref.ID)
		if err != nil {
			return tag, library.ErrNotFound
		}
		exact, err := p.SkillLibrary.Get(tag.Tenant, latest.Scope, latest.Name, ref.Version)
		if err != nil || exact.PublishedAt.IsZero() {
			return tag, library.ErrNotFound
		}
		if !skillScopeAt(tree, exact.Scope, home, person) || !skillScopeAt(tree, exact.Scope, project, person) {
			return tag, library.ErrNotFound
		}
		pinned := pinRevision(exact)
		if !slices.ContainsFunc(skills, func(s pinnedSkill) bool {
			return s.Name == pinned.Name && s.From == pinned.From && s.Version == pinned.Version
		}) {
			skills = append(skills, pinned)
		}
	}
	if len(skills) > library.MaxDefaultSkills {
		return tag, fmt.Errorf("%w: at most four skills per turn", library.ErrInvalid)
	}
	oldTools := tag.Meta[operations.MetaTools]
	pinSkills(tag.Meta, skills)
	if oldTools != "" {
		var old, next []string
		if json.Unmarshal([]byte(oldTools), &old) != nil {
			return tag, library.ErrInvalid
		}
		if raw := tag.Meta[operations.MetaTools]; raw != "" {
			if json.Unmarshal([]byte(raw), &next) != nil {
				return tag, library.ErrInvalid
			}
			old = slices.DeleteFunc(old, func(v string) bool { return !slices.Contains(next, v) })
		}
		if old == nil {
			old = []string{}
		}
		raw, _ := json.Marshal(old)
		tag.Meta[operations.MetaTools] = string(raw)
	}
	raw, _ := json.Marshal(defaults.Skills)
	tag.Meta[metaDefaultSkills] = string(raw)
	tag.Meta[metaDefaultSkillRevision] = strconv.Itoa(rev.Rev)
	return tag, nil
}

// checkAgentSkillDefaults checks the pinned revision and current audience before
// any skill text reaches a model, including work resumed after a restart.
func (p *Platform) checkAgentSkillDefaults(req runtime.Request) error {
	raw := req.Meta[metaDefaultSkills]
	if raw == "" {
		return nil
	}
	revision, err := strconv.Atoi(req.Meta[metaDefaultSkillRevision])
	if err != nil || revision != req.Revision.Rev {
		return library.ErrConflict
	}
	var refs []AgentSkillRef
	if json.Unmarshal([]byte(raw), &refs) != nil || !library.ValidRefs(refs) {
		return library.ErrInvalid
	}
	project, person := p.operationPlace(req)
	tree := p.Projects.Tree(req.Tenant)
	home, ok := tree.Home(req.Agent())
	if !ok {
		return library.ErrNotFound
	}
	for _, ref := range refs {
		latest, err := p.SkillLibrary.Find(req.Tenant, ref.ID)
		if err != nil {
			return library.ErrNotFound
		}
		exact, err := p.SkillLibrary.Get(req.Tenant, latest.Scope, latest.Name, ref.Version)
		if err != nil || exact.PublishedAt.IsZero() || !skillScopeAt(tree, exact.Scope, home, person) || !skillScopeAt(tree, exact.Scope, project, person) {
			return library.ErrNotFound
		}
	}
	return nil
}

// defaultSkillPreview uses the admission compiler to report whether the exact
// defaults would be sent, then projects names through the reader's scope rule.
func (p *Platform) defaultSkillPreview(ctx context.Context, who auth.Principal, project, agent string) *ContextLayer {
	defaults := p.AgentSkillDefaults.Get(who.Tenant, wire.NormalizeName(agent))
	names := []string{}
	for _, ref := range defaults.Skills {
		latest, err := p.LibrarySkill(who, ref.ID)
		if err != nil {
			continue
		}
		exact, err := p.SkillLibrary.Get(who.Tenant, latest.Scope, latest.Name, ref.Version)
		if err != nil {
			continue
		}
		names = append(names, fmt.Sprintf("%s@%d", exact.Name, exact.Version))
	}
	if len(names) == 0 {
		return nil
	}
	_, err := p.pinAgentSkillDefaults(ctx, envelope.Tag{Tenant: who.Tenant, Agent: agent, Actor: ActorOf(who), Meta: map[string]string{}}, call.Edge{}, project)
	layer := nameLayer("agent_skills", agent, names, library.MaxDefaultSkills)
	if err != nil {
		layer.Sent = false
		layer.Omitted = layer.Used
		layer.Used = 0
	}
	return &layer
}
