package platform

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/urmzd/fiber/pkg/registry"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/projects"
)

// HelpSuggestion identifies work without starting it or widening its audience.
type HelpSuggestion struct {
	Project string `json:"project"`
	Title   string `json:"title"`
	Agent   string `json:"agent"`
	Reason  string `json:"reason"`
}

// FindHelp ranks only visible project/agent pairs. Hidden names, conversations
// and sources never enter scoring, explanations or counts.
func (p *Platform) FindHelp(ctx context.Context, who auth.Principal, description string) ([]HelpSuggestion, error) {
	description = strings.TrimSpace(description)
	if description == "" || utf8.RuneCountInString(description) > 200 {
		return nil, fmt.Errorf("%w: describe the work in 1 to 200 characters", projects.ErrInvalid)
	}
	out := []HelpSuggestion{}
	if !who.Can(auth.ProjectsRead) || !who.Can(auth.AgentsRead) {
		return out, nil
	}
	tokens := strings.FieldsFunc(strings.ToLower(description), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	tree, seer := p.ProjectTree(who.Tenant), p.Seer(who)
	ordinary := ReaderOf(who)
	ordinary.Govern = false
	visible := []projects.Project{}
	for _, pr := range append([]projects.Project{tree.Root()}, tree.Projects()...) {
		if pr.Archived || !tree.Resolve(pr.Name, ordinary).Role.AtLeast(projects.Viewer) || !seer.Project(pr.Name) {
			continue
		}
		visible = append(visible, pr)
	}
	type scored struct {
		HelpSuggestion
		score int
	}
	ranked := []scored{}
	page := registry.Page{Size: 100}
	for {
		agents, next, err := p.AgentDirectory(ctx, who.Tenant, page)
		if err != nil {
			return nil, err
		}
		for _, agent := range agents {
			if !seer.Agent(agent.Spec.Name) {
				continue
			}
			for _, pr := range visible {
				if !tree.IsAvailable(pr.Name, agent.Spec.Name) {
					continue
				}
				title := titleOf(pr, p.Settings(who.Tenant).WorkspaceName)
				if pr.IsRoot() && p.Settings(who.Tenant).WorkspaceName == "" {
					title = "Company"
				}
				score := helpScore(tokens, pr.Name+" "+pr.Title+" "+pr.Description+" "+agent.Spec.Name+" "+agent.Spec.Description)
				if score == 0 {
					continue
				}
				ranked = append(ranked, scored{HelpSuggestion{Project: projectAddress(pr.Name), Title: title, Agent: agent.Spec.Name, Reason: "Matches your description and is available in this project"}, score})
				slices.SortFunc(ranked, func(a, b scored) int {
					if n := cmp.Compare(b.score, a.score); n != 0 {
						return n
					}
					if n := strings.Compare(a.Project, b.Project); n != 0 {
						return n
					}
					return strings.Compare(a.Agent, b.Agent)
				})
				if len(ranked) > 8 {
					ranked = ranked[:8]
				}
			}
		}
		if next == "" {
			break
		}
		page.Token = next
	}
	for _, r := range ranked {
		out = append(out, r.HelpSuggestion)
	}
	return out, nil
}
func helpScore(tokens []string, text string) int {
	text = strings.ToLower(text)
	score := 0
	for _, token := range tokens {
		if len(token) > 1 && strings.Contains(text, token) {
			score++
		}
	}
	return score
}
