package platform

import (
	"fmt"
	"github.com/urmzd/fiber/pkg/envelope"
	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/sourcelinks"
	"slices"
	"strings"
)

func (p *Platform) sourceSuggestionVisible(who auth.Principal, row sourcelinks.Suggestion) bool {
	if row.Knowledge != "" && !p.SourceVisible(who, row.Knowledge) {
		return false
	}
	project := projects.NameOf(row.Project)
	if !p.Projects.Tree(who.Tenant).Has(project) || !p.Sees(who, Place{Project: project}) {
		return false
	}
	if row.Client != "" {
		if _, err := p.Clients.Get(who.Tenant, row.Client); err != nil {
			return false
		}
	}
	return true
}

type SourceSuggestionView struct {
	sourcelinks.Suggestion
	Active bool `json:"active"`
}

func (p *Platform) SourceSuggestionsAs(who auth.Principal, client, source, status string, limit int) []SourceSuggestionView {
	if !who.Can(auth.ClientsRead) {
		return []SourceSuggestionView{}
	}
	rows := p.SourceSuggestions.List(who.Tenant, client, status, limit, func(row sourcelinks.Suggestion) bool {
		return (source == "" || row.Knowledge == source) && p.sourceSuggestionVisible(who, row)
	})
	out := []SourceSuggestionView{}
	for _, row := range rows {
		out = append(out, SourceSuggestionView{row, row.State == "linked" && p.automaticSourceLinkActive(who.Tenant, row)})
	}
	return out
}
func (p *Platform) SourceSuggestionAs(who auth.Principal, id string) (sourcelinks.Suggestion, error) {
	row, err := p.SourceSuggestions.Get(who.Tenant, id)
	if err != nil || !p.sourceSuggestionVisible(who, row) {
		return sourcelinks.Suggestion{}, sourcelinks.ErrNotFound
	}
	return row, nil
}
func (p *Platform) DecideSourceSuggestion(who auth.Principal, id, action string, version int) (result sourcelinks.Suggestion, resultErr error) {
	if !who.Can(auth.ClientsWrite) {
		return result, auth.ErrForbidden
	}
	row, err := p.SourceSuggestionAs(who, id)
	if err != nil {
		return result, err
	}
	if err := p.sourceRuleOwner(who, row.Project, row.Channel); err != nil {
		return result, err
	}
	if version < 1 {
		return result, sourcelinks.ErrInvalid
	}
	if row.Version != version {
		return result, sourcelinks.ErrConflict
	}
	if action != "link" && action != "ignore" {
		return result, sourcelinks.ErrInvalid
	}
	if action == "link" && row.Knowledge != "" && !p.SourceMayPublishIn(who.Tenant, row.Knowledge, row.Project) {
		return result, fmt.Errorf("%w: source scope cannot be widened by a context link", sourcelinks.ErrInvalid)
	}
	attempt, err := p.BeginAudit(who, audit.Change{Action: "source_link_suggestion.decide", Resource: "source_link_suggestion", ID: id, Scope: "project", Project: projects.NameOf(row.Project), After: map[string]string{"action": action, "knowledge": row.Knowledge, "client": row.Client}})
	if err != nil {
		return result, err
	}
	defer func() { p.finishSourceAudit(attempt, resultErr) }()
	return p.SourceSuggestions.Decide(who.Tenant, id, action, version, p.now())
}
func sourceResource(src knowledge.Source) sourcelinks.Resource {
	provider, origin, attribute := "manual", src.Name, "source"
	if src.Lifecycle != nil {
		provider = src.Lifecycle.Provider
		if src.Lifecycle.Origin != "" {
			origin = src.Lifecycle.Origin
		}
		switch provider {
		case "drive":
			attribute = "folder"
		case "notion":
			attribute = "parent"
		}
	}
	return sourcelinks.Resource{Provider: provider, ID: origin, Attributes: map[string][]string{attribute: {origin}}}
}
func (p *Platform) observeKnowledgeSource(tenant string, src knowledge.Source) error {
	matches, err := p.SourceLinks.Match(tenant, sourceResource(src))
	if err != nil {
		return err
	}
	matches = slices.DeleteFunc(matches, func(m sourcelinks.Match) bool { return !p.SourceMayPublishIn(tenant, src.Name, m.Project) })
	_, err = p.SourceSuggestions.Observe(tenant, "", src.Name, sourceResource(src), matches, p.now())
	return err
}
func clientResource(c clients.Client, tag envelope.Tag) (sourcelinks.Resource, bool) {
	if c.Surface == clients.Slack {
		return slackResource(tag.Meta["slack_channel"])
	}
	if c.Surface != clients.GitHub {
		return sourcelinks.Resource{}, false
	}
	owner, repo := tag.Meta["github_owner"], tag.Meta["github_repo"]
	if owner == "" || repo == "" || len(owner)+len(repo) > 256 {
		return sourcelinks.Resource{}, false
	}
	id := owner + "/" + repo
	attrs := map[string][]string{"repository": {id}}
	if labels := tag.Meta["github_labels"]; labels != "" {
		values := strings.Split(labels, ",")
		if len(values) <= 50 {
			attrs["label"] = values
		}
	}
	if author := tag.Meta["github_author"]; author != "" {
		attrs["contributor"] = []string{author}
	}
	return sourcelinks.Resource{Provider: "github", ID: id, Attributes: attrs}, true
}
func slackResource(channel string) (sourcelinks.Resource, bool) {
	if channel == "" || directMessage(channel) || len(channel) > 128 {
		return sourcelinks.Resource{}, false
	}
	return sourcelinks.Resource{Provider: "slack", ID: channel, Attributes: map[string][]string{"channel": {channel}}}, true
}
func (p *Platform) observeClientResource(c clients.Client, resource sourcelinks.Resource) error {
	matches, err := p.SourceLinks.Match(c.Tenant, resource)
	if err != nil {
		return err
	}
	_, err = p.SourceSuggestions.Observe(c.Tenant, c.ID, "", resource, matches, p.now())
	return err
}
func (p *Platform) automaticSourceLinkActive(tenant string, row sourcelinks.Suggestion) bool {
	if !row.Automatic {
		return true
	}
	if row.Knowledge != "" {
		src, err := p.Knowledge.Get(tenant, row.Knowledge)
		if err != nil {
			return false
		}
		current := sourceResource(src)
		if current.Provider != row.Resource.Provider || current.ID != row.Resource.ID {
			return false
		}
	}
	matches, err := p.SourceLinks.Match(tenant, row.Resource)
	if err != nil {
		return false
	}
	return len(matches) == 1 && matches[0].Mode == sourcelinks.Auto && matches[0].Project == row.Project && matches[0].Channel == row.Channel
}
func (p *Platform) linkedClientResource(c clients.Client, resource sourcelinks.Resource) (project, channel string, ok bool) {
	rows := p.SourceSuggestions.List(c.Tenant, c.ID, "linked", 200, func(row sourcelinks.Suggestion) bool {
		return row.Knowledge == "" && row.Resource.Provider == resource.Provider && row.Resource.ID == resource.ID
	})
	destinations := map[[2]string]bool{}
	for _, row := range rows {
		row.Resource = resource
		if !p.automaticSourceLinkActive(c.Tenant, row) {
			continue
		}
		name := projects.NameOf(row.Project)
		ch := row.Channel
		if ch == "" {
			ch = "general"
		}
		channelRecord, err := p.Projects.Channel(c.Tenant, name, ch)
		if err != nil || channelRecord.Archived {
			continue
		}
		destinations[[2]string{name, ch}] = true
	}
	if len(destinations) != 1 {
		return "", "", false
	}
	for dest := range destinations {
		return dest[0], dest[1], true
	}
	return "", "", false
}
