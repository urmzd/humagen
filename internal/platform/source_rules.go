package platform

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/sourcelinks"
)

func (p *Platform) SourceRulesAs(who auth.Principal) ([]sourcelinks.Rule, error) {
	out := []sourcelinks.Rule{}
	if !who.Can(auth.ClientsRead) {
		return out, nil
	}
	rules, err := p.SourceLinks.List(who.Tenant)
	if err != nil {
		return nil, err
	}
	for _, rule := range rules {
		if p.Sees(who, Place{Project: projects.NameOf(rule.Project)}) {
			out = append(out, rule)
		}
	}
	return out, nil
}
func (p *Platform) sourceRuleOwner(who auth.Principal, project, channel string) error {
	if !who.Can(auth.ClientsWrite) {
		return auth.ErrForbidden
	}
	return p.sourceTargetOwner(who, project, channel)
}
func (p *Platform) sourceTargetOwner(who auth.Principal, project, channel string) error {
	name := projects.NameOf(project)
	tree := p.Projects.Tree(who.Tenant)
	pr, _, err := p.projectRole(tree, who, name, projects.Owner)
	if err != nil {
		return err
	}
	if governedNotes(pr) && !who.Can(auth.ProjectsGovern) {
		return projects.ErrForbidden
	}
	if channel != "" {
		if _, err := p.Projects.Channel(who.Tenant, name, channel); err != nil {
			return err
		}
	}
	return nil
}
func (p *Platform) SaveSourceRule(who auth.Principal, id string, rule sourcelinks.Rule) (result sourcelinks.Rule, resultErr error) {
	var expected *sourcelinks.Rule
	if id != "" {
		old, err := p.SourceLinks.Get(who.Tenant, id)
		if err != nil {
			return result, err
		}
		if !p.Sees(who, Place{Project: projects.NameOf(old.Project)}) {
			return result, sourcelinks.ErrNotFound
		}
		if err := p.sourceRuleOwner(who, old.Project, old.Channel); err != nil {
			return result, err
		}
		expected = &old
	} else {
		var nonce [16]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return result, err
		}
		id = "rule-" + hex.EncodeToString(nonce[:])
	}
	if err := p.sourceRuleOwner(who, rule.Project, rule.Channel); err != nil {
		return result, err
	}
	rule.ID = id
	if expected != nil {
		rule.CopiedFrom = expected.CopiedFrom
	} else if rule.CopiedFrom != "" {
		return result, sourcelinks.ErrInvalid
	}
	attempt, err := p.BeginAudit(who, audit.Change{Action: "source_link_rule.save", Resource: "source_link_rule", ID: id, Scope: "project", Project: projects.NameOf(rule.Project), After: map[string]string{"provider": rule.Provider, "attribute": rule.Attribute, "mode": string(rule.Mode), "project": projects.NameOf(rule.Project)}})
	if err != nil {
		return result, err
	}
	defer func() { p.finishSourceAudit(attempt, resultErr) }()
	return p.SourceLinks.PutIfCurrent(who.Tenant, rule, expected)
}
func (p *Platform) DeleteSourceRule(who auth.Principal, id string) (resultErr error) {
	old, err := p.SourceLinks.Get(who.Tenant, id)
	if err != nil {
		return err
	}
	if !p.Sees(who, Place{Project: projects.NameOf(old.Project)}) {
		return sourcelinks.ErrNotFound
	}
	if err := p.sourceRuleOwner(who, old.Project, old.Channel); err != nil {
		return err
	}
	attempt, err := p.BeginAudit(who, audit.Change{Action: "source_link_rule.delete", Resource: "source_link_rule", ID: id, Scope: "project", Project: projects.NameOf(old.Project)})
	if err != nil {
		return err
	}
	defer func() { p.finishSourceAudit(attempt, resultErr) }()
	return p.SourceLinks.DeleteIfCurrent(who.Tenant, id, old)
}
func (p *Platform) PreviewSourceRules(who auth.Principal, resource sourcelinks.Resource) ([]sourcelinks.Match, error) {
	return p.PreviewSourceRuleCandidate(who, resource, nil)
}
func (p *Platform) PreviewSourceRuleCandidate(who auth.Principal, resource sourcelinks.Resource, candidate *sourcelinks.Rule) ([]sourcelinks.Match, error) {
	if !who.Can(auth.ClientsRead) {
		return nil, auth.ErrForbidden
	}
	if candidate != nil && (candidate.ID != "" || candidate.CopiedFrom != "" || !p.Sees(who, Place{Project: projects.NameOf(candidate.Project)})) {
		return nil, sourcelinks.ErrInvalid
	}
	all, err := p.SourceLinks.MatchCandidate(who.Tenant, resource, candidate)
	if err != nil {
		return nil, err
	}
	out := []sourcelinks.Match{}
	for _, m := range all {
		if p.Sees(who, Place{Project: projects.NameOf(m.Project)}) {
			m.Mode = sourcelinks.Suggest
			out = append(out, m)
		}
	}
	return out, nil
}
