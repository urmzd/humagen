package platform

import (
	"context"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/library"
)

type SkillAgentUse struct {
	Agent         string `json:"agent"`
	AgentRevision int    `json:"agent_revision"`
	NeedsReview   bool   `json:"needs_review"`
}
type SkillUsage struct {
	Agents []SkillAgentUse `json:"agents"`
	More   bool            `json:"more"`
}

// SkillAgents lists actual exact default bindings, independently of test runs.
// Both the skill and every agent use their normal read rule before the cap.
func (p *Platform) SkillAgents(ctx context.Context, who auth.Principal, id string, version, limit int) (SkillUsage, error) {
	out := SkillUsage{Agents: []SkillAgentUse{}}
	if !who.Can(auth.ProjectsRead) {
		return out, library.ErrNotFound
	}
	latest, err := p.LibrarySkill(who, id)
	if err != nil {
		return out, library.ErrNotFound
	}
	exact, err := p.SkillLibrary.Get(who.Tenant, latest.Scope, latest.Name, version)
	if err != nil {
		return out, library.ErrNotFound
	}
	if limit < 1 || limit > 200 {
		return out, library.ErrInvalid
	}
	if !who.Can(auth.AgentsRead) || exact.PublishedAt.IsZero() {
		return out, nil
	}
	seer := p.Seer(who)
	for _, binding := range p.AgentSkillDefaults.Using(who.Tenant, library.SkillRef{ID: id, Version: version}) {
		if !seer.Agent(binding.Agent) {
			continue
		}
		revision, err := p.Agents.Get(ctx, who.Tenant, binding.Agent, 0)
		if err != nil {
			continue
		}
		if len(out.Agents) == limit {
			out.More = true
			break
		}
		home, homed := seer.tree.Home(binding.Agent)
		person := ""
		if project, ok := seer.tree.Get(home); ok {
			person = project.PersonalOwner
		}
		out.Agents = append(out.Agents, SkillAgentUse{Agent: revision.Spec.Name, AgentRevision: binding.AgentRevision, NeedsReview: binding.AgentRevision != revision.Rev || !homed || !skillScopeAt(seer.tree, exact.Scope, home, person)})
	}
	return out, nil
}
