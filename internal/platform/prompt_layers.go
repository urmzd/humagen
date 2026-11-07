package platform

import (
	"encoding/json"
	"maps"
	"unicode/utf8"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/prompts"
	"github.com/urmzd/orchagent/flow/event"
	"github.com/urmzd/orchagent/flow/runtime"
)

func (p *Platform) promptLayers(tenant, project string) (prompts.Snapshot, error) {
	settings, err := p.settings.get(tenant)
	if err != nil {
		return prompts.Snapshot{}, err
	}
	tree := p.Projects.Tree(tenant)
	var layers []prompts.Layer
	for _, l := range instructionLayers(tree.Chain(project), settings.WorkspaceName) {
		kind := "project"
		if l.From == "" {
			kind = "workspace"
		}
		layers = append(layers, prompts.NewLayer(kind, l.From, l.Text))
	}
	return prompts.Compose(settings.Prompts, layers)
}

// pinPromptLayers replaces caller-supplied pins at the admission boundary.
func (p *Platform) pinPromptLayers(tag event.Envelope, project string) (event.Envelope, error) {
	snapshot, err := p.promptLayers(tag.Tenant, project)
	if wire.Origin(tag) == OriginEval && wire.Meta(tag)[metaEvalPrefix] != "" {
		err = json.Unmarshal([]byte(wire.Meta(tag)[metaEvalPrefix]), &snapshot)
		if err == nil {
			err = snapshot.Validate()
		}
	}
	if err != nil {
		return tag, err
	}
	wire.EditMeta(&tag, func(meta map[string]string) { err = snapshot.Pin(meta); delete(meta, metaEvalPrefix) })
	return tag, err
}

func withPromptLayers(req runtime.Request) (runtime.Request, error) {
	snapshot, ok, err := prompts.Read(req.Meta)
	if err != nil || !ok {
		return req, err
	}
	req.Definition.Spec.SystemPrompt = snapshot.System(req.Definition.Spec.SystemPrompt)
	// Private pins are provenance, not additional text for the model.
	req.Meta = maps.Clone(req.Meta)
	delete(req.Meta, prompts.MetaSnapshot)
	return req, nil
}

func promptPreview(l prompts.Layer) ContextLayer {
	return ContextLayer{Kind: l.Kind + "_instructions", From: l.From, Text: l.Text, Version: l.Version, Budget: prompts.MaxLayerChars, Used: utf8.RuneCountInString(l.Text), Sent: true, Unit: "characters"}
}
