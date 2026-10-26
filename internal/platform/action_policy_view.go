package platform

import (
	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/operations"
)

type ActionPolicySource struct {
	Kind  string           `json:"kind"`
	Name  string           `json:"name,omitempty"`
	Label string           `json:"label"`
	Level operations.Level `json:"level"`
}
type EffectiveActionRule struct {
	Level     operations.Level     `json:"level,omitempty"`
	Inherited operations.Level     `json:"inherited,omitempty"`
	Locked    bool                 `json:"locked"`
	Sources   []ActionPolicySource `json:"sources"`
}
type ActionPolicyView struct {
	operations.Policy
	Effective map[operations.Category]EffectiveActionRule `json:"effective"`
}

func policyRank(level operations.Level) int {
	switch level {
	case operations.Allow:
		return 1
	case operations.Ask:
		return 2
	case operations.Never:
		return 3
	}
	return 0
}

// ActionPolicy describes the known constraints at this scope. An agent or
// personal restriction can narrow a project further at actual run admission.
func (p *Platform) ActionPolicy(who auth.Principal, scope operations.Scope) ActionPolicyView {
	tree := p.Projects.Tree(who.Tenant)
	if scope.Kind == "agent" {
		scope.Name = legacy.BuiltinAgent(scope.Name)
	}
	scopes := []operations.Scope{{Kind: "workspace"}}
	project := ""
	switch scope.Kind {
	case "project":
		project = scope.Name
	case "agent":
		project, _ = tree.Home(scope.Name)
	}
	if scope.Kind == "project" || scope.Kind == "agent" {
		chain := tree.Chain(project)
		for i := len(chain) - 1; i >= 0; i-- {
			place := chain[i]
			if place.Name != "" {
				scopes = append(scopes, operations.Scope{Kind: "project", Name: place.Name})
			}
		}
	}
	if scope.Kind == "agent" || scope.Kind == "personal" {
		scopes = append(scopes, scope)
	}
	view := ActionPolicyView{Policy: p.Operations.Policy(who.Tenant, scope), Effective: map[operations.Category]EffectiveActionRule{}}
	seer := p.Seer(who)
	for _, category := range operations.Categories {
		rule := EffectiveActionRule{Sources: []ActionPolicySource{}}
		for i, at := range scopes {
			level := p.Operations.Policy(who.Tenant, at).Rules[category]
			if policyRank(level) > policyRank(rule.Level) {
				rule.Level = level
			}
			if i < len(scopes)-1 && policyRank(level) > policyRank(rule.Inherited) {
				rule.Inherited = level
			}
		}
		rule.Locked = policyRank(rule.Inherited) >= policyRank(operations.Ask)
		hidden := false
		for _, at := range scopes {
			level := p.Operations.Policy(who.Tenant, at).Rules[category]
			if level == operations.Unspecified || level != rule.Level {
				continue
			}
			source := ActionPolicySource{Kind: at.Kind, Name: at.Name, Label: at.Name, Level: level}
			if at.Kind == "personal" {
				source.Name, source.Label = "", ""
			}
			if at.Kind == "project" {
				if !seer.Project(at.Name) {
					hidden = true
					continue
				}
				if place, ok := tree.Get(at.Name); ok && place.Title != "" {
					source.Label = place.Title
				}
			}
			if len(rule.Sources) >= 63 {
				hidden = true
				continue
			}
			rule.Sources = append(rule.Sources, source)
		}
		if hidden {
			rule.Sources = append(rule.Sources, ActionPolicySource{Kind: "inherited", Level: rule.Level})
		}
		view.Effective[category] = rule
	}
	return view
}
