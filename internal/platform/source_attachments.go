package platform

import (
	"fmt"
	"slices"

	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/sourcelinks"
)

type KnowledgeAttachment struct {
	Source  string `json:"source"`
	Project string `json:"project"`
	Channel string `json:"channel,omitempty"`
}

func canonicalSourceProject(project string) string {
	if projects.NameOf(project) == "" {
		return "~"
	}
	return project
}
func (p *Platform) SourceAttachmentsAs(who auth.Principal, name string) ([]KnowledgeAttachment, error) {
	if _, err := p.SourceAs(who, name); err != nil {
		return nil, err
	}
	rows := p.SourceSuggestions.List(who.Tenant, "", "linked", 200, func(row sourcelinks.Suggestion) bool {
		return row.Knowledge == name && p.sourceSuggestionVisible(who, row) && p.automaticSourceLinkActive(who.Tenant, row)
	})
	out := []KnowledgeAttachment{}
	for _, row := range rows {
		target := KnowledgeAttachment{name, row.Project, row.Channel}
		if !slices.Contains(out, target) {
			out = append(out, target)
		}
	}
	return out, nil
}
func (p *Platform) AttachSource(who auth.Principal, name, project, channel string) (result KnowledgeAttachment, resultErr error) {
	if !who.Can(auth.KnowledgeWrite) {
		return result, auth.ErrForbidden
	}
	src, err := p.SourceAs(who, name)
	if err != nil {
		return result, err
	}
	if project == "" {
		return result, knowledge.ErrInvalid
	}
	project = canonicalSourceProject(project)
	if err := p.sourceTargetOwner(who, project, channel); err != nil {
		return result, err
	}
	if !p.SourceMayPublishIn(who.Tenant, name, project) {
		return result, fmt.Errorf("%w: source scope cannot be widened by a context attachment", knowledge.ErrInvalid)
	}
	attempt, err := p.BeginAudit(who, audit.Change{Action: "source.attach", Resource: "source", ID: name, Scope: "project", Project: projects.NameOf(project), After: map[string]string{"project": projects.NameOf(project), "channel": channel}})
	if err != nil {
		return result, err
	}
	defer func() { p.finishSourceAudit(attempt, resultErr) }()
	_, err = p.SourceSuggestions.Attach(who.Tenant, name, sourceResource(src), project, channel, p.now())
	if err != nil {
		return result, err
	}
	return KnowledgeAttachment{name, project, channel}, nil
}
func (p *Platform) DetachSource(who auth.Principal, name, project, channel string) (resultErr error) {
	if !who.Can(auth.KnowledgeWrite) {
		return auth.ErrForbidden
	}
	if _, err := p.SourceAs(who, name); err != nil {
		return err
	}
	if project == "" {
		return knowledge.ErrInvalid
	}
	project = canonicalSourceProject(project)
	if err := p.sourceTargetOwner(who, project, channel); err != nil {
		return err
	}
	attempt, err := p.BeginAudit(who, audit.Change{Action: "source.detach", Resource: "source", ID: name, Scope: "project", Project: projects.NameOf(project), After: map[string]string{"project": projects.NameOf(project), "channel": channel}})
	if err != nil {
		return err
	}
	defer func() { p.finishSourceAudit(attempt, resultErr) }()
	return p.SourceSuggestions.Detach(who.Tenant, name, project, channel, p.now())
}

// knowledgeAtPlace is the one real selection used by pinning and preview.
// Channel attachments apply only there. Project attachments inherit along the
// same chain as project declarations. Explicit agent grants remain separate.
func (p *Platform) knowledgeAtPlace(tenant, project, channel string) []projects.KnowledgeSource {
	if channel == "" {
		channel = "general"
	}
	out := []projects.KnowledgeSource{}
	add := func(name, from string) {
		if len(out) >= projects.MaxRunKnowledge || slices.ContainsFunc(out, func(s projects.KnowledgeSource) bool { return s.Name == name }) {
			return
		}
		out = append(out, projects.KnowledgeSource{Name: name, From: from})
	}
	linked := func(pr, ch string) {
		for _, row := range p.SourceSuggestions.List(tenant, "", "linked", 200, func(row sourcelinks.Suggestion) bool {
			return row.Knowledge != "" && projects.NameOf(row.Project) == pr && row.Channel == ch && p.SourceMayPublishIn(tenant, row.Knowledge, row.Project)
		}) {
			if p.automaticSourceLinkActive(tenant, row) {
				add(row.Knowledge, pr)
			}
		}
	}
	linked(project, channel)
	tree := p.Projects.Tree(tenant)
	chain := tree.Chain(project)
	if len(chain) == 0 {
		chain = []projects.Project{tree.Root()}
	}
	for _, pr := range chain {
		linked(pr.Name, "")
		for _, name := range pr.Knowledge {
			add(name, pr.Name)
		}
	}
	return out
}
