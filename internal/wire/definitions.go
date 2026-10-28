package wire

import (
	"context"
	"errors"
	"fmt"
	"strings"

	sdef "github.com/urmzd/saige/agent/definition"

	"github.com/urmzd/humagen/fiber/pkg/definition"
)

// DefinitionCatalog is an AgentCatalog over saige definitions (pkg/agents,
// or any definition.Source). Revision N is the Nth version of a name, lowest
// first, so an agent moved from the registry keeps its revision numbers.
type DefinitionCatalog struct{ src definition.Source }

// NewDefinitionCatalog returns a catalog that reads src.
func NewDefinitionCatalog(src definition.Source) *DefinitionCatalog {
	return &DefinitionCatalog{src: src}
}

var _ AgentCatalog = (*DefinitionCatalog)(nil)

// Get implements AgentCatalog.
func (c *DefinitionCatalog) Get(ctx context.Context, tenant, name string, rev int) (Revision, error) {
	name = NormalizeName(name)
	if rev < 0 {
		return Revision{}, fmt.Errorf("%w: revision %d", ErrNotFound, rev)
	}
	if rev == 0 {
		if _, err := c.src.Latest(ctx, tenant, name); err != nil {
			return Revision{}, catalogErr(err)
		}
	}
	all, err := c.src.Versions(ctx, tenant, name)
	if err != nil {
		return Revision{}, catalogErr(err)
	}
	if rev == 0 {
		rev = len(all)
	}
	if rev > len(all) {
		return Revision{}, fmt.Errorf("%w: %s revision %d", ErrNotFound, name, rev)
	}
	return RevisionOf(all[rev-1], rev)
}

// List implements AgentCatalog.
func (c *DefinitionCatalog) List(ctx context.Context, tenant string, page Page) ([]Revision, string, error) {
	recs, next, err := c.src.List(ctx, tenant, definition.Page{Size: page.Size, Token: page.Token})
	if err != nil {
		return nil, "", catalogErr(err)
	}
	out := make([]Revision, 0, len(recs))
	for _, rec := range recs {
		all, err := c.src.Versions(ctx, tenant, rec.Name)
		if err != nil {
			return nil, "", catalogErr(err)
		}
		r, err := RevisionOf(rec, len(all))
		if err != nil {
			return nil, "", err
		}
		out = append(out, r)
	}
	return out, next, nil
}

// History implements AgentCatalog.
func (c *DefinitionCatalog) History(ctx context.Context, tenant, name string) ([]Revision, error) {
	all, err := c.src.Versions(ctx, tenant, NormalizeName(name))
	if err != nil {
		return nil, catalogErr(err)
	}
	out := make([]Revision, 0, len(all))
	for i, rec := range all {
		r, err := RevisionOf(rec, i+1)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

func catalogErr(err error) error {
	switch {
	case errors.Is(err, definition.ErrNotFound), errors.Is(err, definition.ErrRetired):
		return fmt.Errorf("%w: %w", ErrNotFound, err)
	case errors.Is(err, definition.ErrInvalid):
		return fmt.Errorf("%w: %w", ErrInvalidPage, err)
	}
	return err
}

// RevisionOf reads a stored definition as the registry's revision rev: the
// inverse of definition.FromAgentSpec. Its hash is the definition's digest.
func RevisionOf(rec definition.Record, rev int) (Revision, error) {
	d, err := sdef.Parse([]byte(rec.Content), "")
	if err != nil {
		return Revision{}, fmt.Errorf("%w: %s@%s: %w", ErrInvalidAgent, rec.Name, rec.Version, err)
	}
	p := rec.Policy
	spec := AgentSpec{
		Name:         d.Name,
		Description:  d.Description,
		SystemPrompt: strings.TrimSpace(d.Prompt),
		FallbackOn:   p.FallbackOn,
		Approvers:    p.Approvers,
		SelfApproval: p.SelfApproval,
		Answerers:    p.Answerers,
		TaggedOnly:   p.TaggedOnly,
		Access:       Access{WorkspaceAreas: p.WorkspaceAreas},
	}
	if d.Model != nil {
		spec.Provider, spec.Model, _ = strings.Cut(d.Model.Use, "/")
		for _, f := range d.Model.Fallback {
			provider, model, _ := strings.Cut(f, "/")
			spec.Fallbacks = append(spec.Fallbacks, ModelRef{Provider: provider, Model: model})
		}
	}
	if d.Tools != nil {
		spec.Tools = append(spec.Tools, d.Tools.Registry...)
		for _, m := range d.Tools.MCP {
			spec.Tools = append(spec.Tools, definition.MCPGrant{Provider: m.Server, Write: p.MayWrite(m.Server)}.String())
		}
	}
	for _, s := range d.Subagents {
		spec.Access.Spawn = append(spec.Access.Spawn, s.Ref)
	}
	if d.Approval != nil {
		spec.Approvals = d.Approval.Ask
	}
	for _, k := range p.Knowledge {
		spec.Sources = append(spec.Sources, Source{Name: k.Name, URI: k.URI, Options: k.Options})
	}
	return Revision{Spec: spec.Normalize(), Rev: rev, Hash: rec.Digest, CreatedAt: rec.CreatedAt, CreatedBy: rec.CreatedBy}, nil
}

// DefinitionPolicy is the sidecar an .agent.md file is read with: what saige's
// format does not say (approvers, answerers, workspace areas, MCP write
// grants, knowledge, tagged only).
type DefinitionPolicy = definition.Policy

// AgentFileSuffix marks an agent file written in saige's own format.
const AgentFileSuffix = ".agent.md"

// SpecOfAgentFile reads one .agent.md file and its policy as the spec the
// platform stores. Its version is not kept: the store numbers versions.
// saige's own validation applies here, and the store's when it is created.
func SpecOfAgentFile(content string, policy DefinitionPolicy) (AgentSpec, error) {
	if err := policy.Validate(); err != nil {
		return AgentSpec{}, fmt.Errorf("%w: %w", ErrInvalidAgent, err)
	}
	d, err := sdef.Parse([]byte(content), "")
	if err != nil {
		return AgentSpec{}, fmt.Errorf("%w: %w", ErrInvalidAgent, err)
	}
	r, err := RevisionOf(definition.Record{Name: d.Name, Version: d.Version, Content: content, Policy: policy.Normalize()}, 0)
	if err != nil {
		return AgentSpec{}, err
	}
	return r.Spec, nil
}
