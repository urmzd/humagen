package platform

import (
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/sourcelinks"
)

// SourceLinkRule carries resource selectors, never tokens or live bindings.
type SourceLinkRule = sourcelinks.Rule

// SourceLinkRulesForFork snapshots only the selected project's explicit rules.
// Fork authorization is checked before this internal composition helper is used.
func (p *Platform) SourceLinkRulesForFork(tenant, from string) ([]SourceLinkRule, error) {
	rules, err := p.SourceLinks.List(tenant)
	if err != nil {
		return nil, err
	}
	out := []SourceLinkRule{}
	for _, rule := range rules {
		if projects.NameOf(rule.Project) == projects.NameOf(from) {
			out = append(out, rule)
		}
	}
	return out, nil
}

// RestoreSourceLinkRulesDisabled replays the recorded snapshot into the staged
// project. A fork never copies an active auto-link or any live binding.
func (p *Platform) RestoreSourceLinkRulesDisabled(tenant, to, operation string, rules []SourceLinkRule) error {
	if _, err := p.Projects.Get(tenant, projects.NameOf(to)); err != nil {
		return err
	}
	destination := projects.NameOf(to)
	if destination == "" {
		destination = "~"
	}
	return p.SourceLinks.RestoreDisabled(tenant, destination, operation, rules)
}

func (p *Platform) CopySourceLinkRulesDisabled(tenant, from, to, operation string) error {
	rules, err := p.SourceLinkRulesForFork(tenant, from)
	if err != nil {
		return err
	}
	return p.RestoreSourceLinkRulesDisabled(tenant, to, operation, rules)
}
