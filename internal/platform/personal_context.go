package platform

import (
	"strings"
	"unicode/utf8"

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
	prefs := p.Prefs.Get(tenant, subject).Personal
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
func (p *Platform) personalContext(tenant, project, subject, body string) string {
	layers := p.personalLayers(tenant, project, subject)
	if len(layers) == 0 {
		return body
	}
	parts := make([]string, 0, len(layers)+1)
	for _, layer := range layers {
		parts = append(parts, layer.Text)
		if layer.Kind == "personal_notes" {
			if err := p.Memory.UsedRendered(tenant, memory.PersonalOwner(subject), layer.Text, p.now()); err != nil {
				p.log.Warn("platform: personal memory use could not be recorded", "error", err)
			}
		}
	}
	return strings.Join(append(parts, body), "\n\n")
}
