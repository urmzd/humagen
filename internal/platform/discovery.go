package platform

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/urmzd/fiber/pkg/registry"
	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
)

// DiscoveryQuery bounds navigation rather than downloading workspace data.
type DiscoveryQuery struct {
	Q       string
	Limit   int
	Kinds   []string
	Project *string
}
type DiscoveryItem struct {
	Kind    string   `json:"kind"`
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Project string   `json:"project,omitempty"`
	Channel string   `json:"channel,omitempty"`
	Hue     *float64 `json:"hue,omitempty"`
}

func boundedTitle(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	chars := []rune(text)
	if len(chars) > 200 {
		text = string(chars[:197]) + "..."
	}
	return text
}

// Discover applies each entity's read permission and the same central Seer as
// its native route. There is no unfiltered count or all-workspace client dump.
func (p *Platform) Discover(ctx context.Context, who auth.Principal, q DiscoveryQuery) ([]DiscoveryItem, error) {
	q.Q = strings.TrimSpace(q.Q)
	if utf8.RuneCountInString(q.Q) > 200 || q.Limit < 0 || q.Limit > 50 || len(q.Kinds) > 3 {
		return nil, fmt.Errorf("%w: invalid bounded discovery query", projects.ErrInvalid)
	}
	if q.Limit == 0 {
		q.Limit = 20
	}
	for _, kind := range q.Kinds {
		if kind != "project" && kind != "agent" && kind != "conversation" {
			return nil, projects.ErrInvalid
		}
	}
	wants := func(kind string) bool { return len(q.Kinds) == 0 || slices.Contains(q.Kinds, kind) }
	canProjects := wants("project") && who.Can(auth.ProjectsRead)
	canAgents := wants("agent") && who.Can(auth.AgentsRead)
	canConversations := wants("conversation") && who.Can(auth.RunsRead)
	if !canProjects && !canAgents && !canConversations {
		return []DiscoveryItem{}, nil
	}
	seer := p.Seer(who)
	tree := p.Projects.Tree(who.Tenant)
	if q.Project != nil {
		name := projects.NameOf(*q.Project)
		q.Project = &name
		if !tree.Has(name) || !seer.Project(name) {
			return nil, projects.ErrNotFound
		}
	}
	needle := strings.ToLower(q.Q)
	matches := func(values ...string) bool {
		return needle == "" || slices.ContainsFunc(values, func(value string) bool { return strings.Contains(strings.ToLower(value), needle) })
	}
	out := []DiscoveryItem{}
	if canProjects {
		if q.Project == nil || *q.Project == projects.Root {
			workspace := p.Settings(who.Tenant).WorkspaceName
			if matches(workspace, "company") && seer.Project(projects.Root) {
				out = append(out, DiscoveryItem{Kind: "project", ID: "~", Title: boundedTitle(cmp.Or(workspace, "Company"))})
			}
		}
		rows, _, err := tree.Page(projects.Query{Q: q.Q, Limit: q.Limit, Keep: func(pr projects.Project) bool {
			return (q.Project == nil || pr.Name == *q.Project) && seer.Project(pr.Name)
		}})
		if err != nil {
			return nil, err
		}
		for _, pr := range rows {
			out = append(out, DiscoveryItem{Kind: "project", ID: pr.Name, Title: boundedTitle(cmp.Or(pr.Title, pr.Name)), Project: pr.Name})
		}
	}
	if canAgents {
		added := map[string]bool{}
		add := func(name, description string) {
			if legacy.IsBuiltinAgent(name) || added[name] || !matches(name, description) || !seer.Agent(name) || q.Project != nil && !tree.IsAvailable(*q.Project, name) {
				return
			}
			home, _ := tree.Home(name)
			out = append(out, DiscoveryItem{Kind: "agent", ID: name, Title: name, Project: home})
			added[name] = true
		}
		if projects.ValidAgent(q.Q) {
			if rev, err := p.Agents.Get(ctx, who.Tenant, q.Q, 0); err == nil {
				add(rev.Spec.Name, rev.Spec.Description)
			}
		}
		page := registry.Page{Size: 100}
		for scan := 0; scan < 1000 && len(added) < q.Limit; scan += 100 {
			rows, next, err := p.AgentDirectory(ctx, who.Tenant, page)
			if err != nil {
				return nil, err
			}
			for _, rev := range rows {
				add(rev.Spec.Name, rev.Spec.Description)
				if len(added) >= q.Limit {
					break
				}
			}
			if next == "" {
				break
			}
			page.Token = next
		}
	}
	if canConversations {
		query := runs.ThreadQuery{Limit: q.Limit}
		if q.Project != nil {
			query.Projects = []string{*q.Project}
		}
		rows, _, err := p.threadPage(who.Tenant, query, func(th ThreadSummary) bool { return matches(th.Title) && seer.Conversation(th.ID, th.Project) })
		if err != nil {
			return nil, err
		}
		for _, th := range rows {
			out = append(out, DiscoveryItem{Kind: "conversation", ID: th.ID, Title: boundedTitle(cmp.Or(th.Title, th.ID)), Project: th.Project, Channel: th.Channel})
		}
	}
	score := func(item DiscoveryItem) int {
		title, id := strings.ToLower(item.Title), strings.ToLower(item.ID)
		if needle != "" && (title == needle || id == needle) {
			return 0
		}
		if needle != "" && (strings.HasPrefix(title, needle) || strings.HasPrefix(id, needle)) {
			return 1
		}
		return 2
	}
	kindOrder := map[string]int{"project": 0, "conversation": 1, "agent": 2}
	slices.SortStableFunc(out, func(a, b DiscoveryItem) int {
		if c := cmp.Compare(score(a), score(b)); c != 0 {
			return c
		}
		if c := cmp.Compare(kindOrder[a.Kind], kindOrder[b.Kind]); c != 0 {
			return c
		}
		return strings.Compare(a.Title, b.Title)
	})
	if len(out) > q.Limit {
		out = out[:q.Limit]
	}
	names := []string{}
	for _, item := range out {
		if item.Kind == "agent" {
			names = append(names, item.ID)
		}
	}
	if len(names) > 0 {
		hues, err := p.AgentColors.Assign(who.Tenant, names)
		if err != nil {
			return nil, err
		}
		for i := range out {
			if out[i].Kind == "agent" {
				hue := hues[out[i].ID]
				out[i].Hue = &hue
			}
		}
	}
	return out, nil
}
