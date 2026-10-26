package platform

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/memory"
	"github.com/urmzd/humagen/pkg/projects"
)

// ContextLayer describes the text actually composed, and its content budget.
// Omitted content uses the same unit as Used.
// Knowledge and skills count names. A zero budget means no extra composer cap.
type ContextLayer struct {
	Kind           string `json:"kind"`
	From           string `json:"from"`
	Text           string `json:"text"`
	Budget         int    `json:"budget"`
	Used           int    `json:"used"`
	Omitted        int    `json:"omitted"`
	PreviewOmitted int    `json:"preview_omitted"`
	Sent           bool   `json:"sent"`
	Unit           string `json:"unit"`
}

// ProjectContext is a bounded view of the live composition before a message.
type ProjectContext struct {
	Layers []ContextLayer `json:"layers"`
}

// turnLayers is the one composition used by both contextIn and its preview.
// Conversation text and the separately supplied system prompt belong to runtime.
func (p *Platform) turnLayers(tenant string, tree *projects.Tree, chain []projects.Project, project, channel, agent string) []ContextLayer {
	workspace := p.Settings(tenant).WorkspaceName
	out := instructionLayers(chain, workspace)
	if channel != "" {
		if c, err := p.Projects.Channel(tenant, project, channel); err == nil && strings.TrimSpace(c.Purpose) != "" {
			text := fmt.Sprintf("This conversation is in #%s. Its purpose: %s", c.Name, strings.TrimSpace(c.Purpose))
			out = append(out, ContextLayer{Kind: "channel", From: project, Text: text, Budget: projects.MaxPurpose, Sent: true, Unit: "characters", Used: utf8.RuneCountInString(c.Purpose)})
		}
	}
	if text := projects.ExpertsLine(tree.ExpertTopics(project)); text != "" {
		out = append(out, ContextLayer{Kind: "experts", From: project, Text: text, Sent: true, Unit: "items", Budget: projects.MaxExpertTopics, Used: len(tree.ExpertTopics(project))})
	}
	out = append(out, p.noteLayers(tenant, chain, workspace)...)
	if text := p.Memory.Prompt(tenant, agent); text != "" {
		out = append(out, ContextLayer{Kind: "agent_notes", From: agent, Text: text, Budget: memory.MaxNotes * memory.MaxChars, Used: p.noteCharacters(tenant, agent), Sent: true, Unit: "characters"})
	}
	return out
}

// ProjectContextAs previews the same layers a real turn composes, then removes
// everything the reader cannot see. Source names are never prompt text.
func (p *Platform) ProjectContextAs(ctx context.Context, who auth.Principal, project, channel, agent string) (ProjectContext, error) {
	out := ProjectContext{Layers: []ContextLayer{}}
	project = projects.NameOf(project)
	seer := p.Seer(who)
	tree := seer.tree
	chain := tree.Chain(project)
	missing := func() (ProjectContext, error) { return out, projects.ErrNotFound }
	if len(chain) == 0 || !seer.Project(project) || !seer.Agent(agent) {
		return missing()
	}
	if len(agent) == 0 || len(agent) > 63 || len(channel) > 63 {
		return out, fmt.Errorf("%w: choose an agent", projects.ErrInvalid)
	}
	rev, err := p.Agents.Get(ctx, who.Tenant, agent, 0)
	if err != nil {
		return missing()
	}
	if !tree.IsAvailable(project, agent) {
		return missing()
	}
	if channel != "" {
		if _, err := p.Projects.Channel(who.Tenant, project, channel); err != nil {
			return missing()
		}
	}
	system := ContextLayer{Kind: "agent_instructions", From: agent, Text: rev.Spec.SystemPrompt, Sent: true, Unit: "characters", Used: utf8.RuneCountInString(rev.Spec.SystemPrompt)}
	// The runtime owns the model's token window. There is no platform
	// character cap on the system prompt; only this preview is shortened.
	if system.Used > 32000 {
		system.Text = string([]rune(system.Text)[:32000])
		system.PreviewOmitted = system.Used - 32000
	}
	out.Layers = append(out.Layers, system)
	allVisible := true
	for _, pr := range chain {
		allVisible = allVisible && seer.Project(pr.Name)
	}
	for _, l := range p.turnLayers(who.Tenant, tree, chain, project, channel, agent) {
		if l.Kind == "agent_notes" {
			if !seer.Agent(l.From) {
				continue
			}
		} else if !seer.Project(l.From) || l.Kind == "experts" && !allVisible {
			continue
		}
		out.Layers = append(out.Layers, l)
	}
	if seer.Knowledge() {
		agentNames := []string{}
		for _, src := range rev.Spec.Sources {
			if p.SourceVisible(who, src.Name) {
				agentNames = append(agentNames, src.Name)
			}
		}
		out.Layers = append(out.Layers, nameLayer("agent_knowledge", agent, agentNames, 0))
		names := []string{}
		// Tree.Knowledge is the same bounded selection sourcesFor pins.
		for _, src := range tree.Knowledge(project) {
			if seer.Project(src.From) && p.SourceVisible(who, src.Name) && !slices.Contains(agentNames, src.Name) {
				names = append(names, src.Name)
			}
		}
		layer := nameLayer("knowledge", project, names, projects.MaxRunKnowledge)
		// Count only visible declarations left out by the real selection.
		seen := append(slices.Clone(agentNames), names...)
		for _, pr := range chain {
			if !seer.Project(pr.Name) {
				continue
			}
			for _, name := range pr.Knowledge {
				if p.SourceVisible(who, name) && !slices.Contains(seen, name) {
					layer.Omitted++
					seen = append(seen, name)
				}
			}
		}
		out.Layers = append(out.Layers, layer)
	}
	names := []string{}
	for _, s := range tree.Skills(project) {
		if seer.Project(s.From) {
			names = append(names, s.Name)
		}
	}
	layer := nameLayer("skills", project, names, projects.MaxChainSkills)
	seenSkills := slices.Clone(names)
	for _, pr := range chain {
		if !seer.Project(pr.Name) {
			continue
		}
		for _, skill := range pr.Skills {
			if !slices.Contains(seenSkills, skill.Name) {
				layer.Omitted++
				seenSkills = append(seenSkills, skill.Name)
			}
		}
	}
	layer.Sent = false
	out.Layers = append(out.Layers, layer)
	return out, nil
}

func (p *Platform) noteCharacters(tenant, owner string) int {
	used := 0
	for _, n := range p.Memory.List(tenant, owner) {
		if n.Status == memory.Approved {
			used += utf8.RuneCountInString(n.Text)
		}
	}
	return used
}

// nameLayer bounds a preview's output independently of agent spec sizes.
func nameLayer(kind, from string, names []string, budget int) ContextLayer {
	l := ContextLayer{Kind: kind, From: from, Budget: budget, Unit: "items", Sent: true}
	var kept []string
	chars := 0
	for _, name := range names {
		n := utf8.RuneCountInString(name)
		if len(kept) == 128 || chars+n > 16000 {
			l.PreviewOmitted++
			continue
		}
		kept = append(kept, name)
		chars += n
	}
	l.Text = strings.Join(kept, "\n")
	l.Used = len(names)
	return l
}
