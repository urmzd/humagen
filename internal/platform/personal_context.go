package platform

import (
	"strings"
	"unicode/utf8"

	"github.com/urmzd/humagen/fiber/pkg/event"
	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/memory"
)

// Personal layers belong to the effective human actor, never the machine or
// another participant. A private memory crosses into shared work only by the
// person's explicit preference. Preview uses these exact same layers.
func (p *Platform) personalLayers(tenant, project, subject string) []ContextLayer {
	out := []ContextLayer{}
	if subject == "" {
		return out
	}
	prefs := p.PreferencesOf(tenant, subject).Personal
	if prefs.Briefing != "" {
		out = append(out, ContextLayer{Kind: "personal_briefing", From: subject, Text: "How the person asking prefers to be briefed:\n" + prefs.Briefing, Budget: 500, Used: utf8.RuneCountInString(prefs.Briefing), Sent: true, Unit: "characters"})
	}
	pr, ok := p.Projects.Tree(tenant).Get(project)
	own := ok && pr.PersonalOwner == subject
	if !own && !prefs.UsePersonalMemoryInProjects {
		return out
	}
	owner := memory.PersonalOwner(subject)
	if text := p.Memory.PromptAt(tenant, owner, "What the person asking has chosen to remember:", p.now()); text != "" {
		out = append(out, ContextLayer{Kind: "personal_notes", From: subject, Text: text, Budget: memory.MaxNotes * memory.MaxChars, Used: p.noteCharacters(tenant, owner), Sent: true, Unit: "characters"})
	}
	return out
}
func (p *Platform) personalContext(tenant, project, subject, body string, meta map[string]string) string {
	layers := p.personalLayers(tenant, project, subject)
	if len(layers) == 0 {
		return body
	}
	parts := make([]string, 0, len(layers)+1)
	for _, layer := range layers {
		parts = append(parts, layer.Text)
		if layer.Kind == "personal_notes" {
			meta["_humagen.context.personal_memory"] = "true"

		}
	}
	return strings.Join(append(parts, body), "\n\n")
}

// recordContextUse runs only after durable admission. Replayed IDs join before
// composition, so they never count the same accepted turn twice.
func (p *Platform) recordContextUse(tag event.Envelope, project string) {
	if wire.Origin(tag) != runs.OriginAPI && wire.Origin(tag) != runs.OriginManual && wire.Origin(tag) != "cron" {
		return
	}
	owners := []string{tag.Agent}
	for _, pr := range p.Projects.Tree(tag.Tenant).Chain(project) {
		owners = append(owners, memory.ProjectOwner(pr.Name))
	}
	if subject := personOf(wire.ActorOf(tag)); subject != "" && wire.Meta(tag)["_humagen.context.personal_memory"] == "true" {
		owners = append(owners, memory.PersonalOwner(subject))
	}
	for _, owner := range owners {
		if err := p.Memory.UsedRendered(tag.Tenant, owner, wire.TextOf(tag), p.now()); err != nil {
			p.log.Warn("platform: memory use could not be recorded", "error", err)
		}
	}
}

// turnContext applies the effective read policy before any remembered content
// reaches a model. Ask is an explicit tool approval path, never an automatic read.
func (p *Platform) turnContext(tenant, project, channel, agent, subject, body string, meta map[string]string) string {
	decision := p.ActionDecision(tenant, project, agent, subject, operations.ReadContext)
	withheld := decision == operations.Ask || decision == operations.Never || meta[operations.MetaForceAsk] == "true"
	if !withheld {
		return p.personalContext(tenant, project, subject, p.contextIn(tenant, project, channel, agent, body), meta)
	}
	tree := p.Projects.Tree(tenant)
	chain := tree.Chain(project)
	if chain == nil {
		chain = []projects.Project{{Name: project}, tree.Root()}
	}
	layers := []ContextLayer{}
	for _, layer := range p.turnLayers(tenant, tree, chain, project, channel, agent) {
		if layer.Kind != "agent_notes" && layer.Kind != "notes" && layer.Kind != "project_notes" && layer.Kind != "ancestor_notes" {
			layers = append(layers, layer)
		}
	}
	for _, layer := range p.personalLayers(tenant, project, subject) {
		if layer.Kind == "personal_briefing" {
			layers = append(layers, layer)
		}
	}
	return renderTurnLayers(layers, body)
}
