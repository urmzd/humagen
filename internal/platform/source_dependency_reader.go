package platform

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/knowledge"
)

// SourceDependencyReader is a trusted composition boundary for stores absent
// from the source layer. An implementation must independently check current
// skill/memory access, ordinary origin-run access and private provenance owner
// access. Supplied run IDs prove actual retrieval and ordinary human run/source access; they grant no skill or memory object access.
// Do not infer relationships from instruction, note or provenance text.
type SourceDependencyReader interface {
	ReadSourceDependencies(context.Context, auth.Principal, []string) (SourceExtraDependencies, error)
}

type SourceExtraDependencies struct {
	Skills []SourceDependency
	Memory []SourceDependency
	// Supported contains only "skills" or "memory" whose typed provenance the
	// implementation can authoritatively project, including empty result sets.
	Supported []string
}

// RegisterSourceDependencyReader installs one native projection. Configuration
// injection and registration are mutually exclusive; no silent replacement.
func (p *Platform) RegisterSourceDependencyReader(reader SourceDependencyReader) error {
	p.sourceDependencyMu.Lock()
	defer p.sourceDependencyMu.Unlock()
	if reader == nil || p.sourceDependencyReader != nil || p.cfg.SourceDependencies != nil {
		return fmt.Errorf("%w: source dependency reader already configured or missing", knowledge.ErrExists)
	}
	p.sourceDependencyReader = reader
	return nil
}

func (p *Platform) extraSourceDependencies(ctx context.Context, who auth.Principal, runIDs []string, out *SourceDependencies) error {
	p.sourceDependencyMu.RLock()
	reader := p.sourceDependencyReader
	if reader == nil {
		reader = p.cfg.SourceDependencies
	}
	p.sourceDependencyMu.RUnlock()
	if reader == nil {
		return nil
	}
	extra, err := reader.ReadSourceDependencies(ctx, who, slices.Clone(runIDs))
	if err != nil {
		return err
	}
	if len(extra.Skills) > 200 || len(extra.Memory) > 200 {
		return knowledge.ErrInvalid
	}
	proven := map[string]bool{}
	for _, id := range runIDs {
		proven[id] = true
	}
	project := func(rows []SourceDependency) []SourceDependency {
		kept := []SourceDependency{}
		seen := map[string]bool{}
		for _, row := range rows[:min(len(rows), 200)] {
			if !proven[row.OriginRun] || row.ID == "" || len(row.ID) > 256 || len(row.Title) > 512 || row.Relation != "recorded_origin_run" || len(row.Path) < 2 || !strings.HasPrefix(row.Path, "/") || strings.HasPrefix(row.Path, "//") || strings.ContainsAny(row.Path, "\r\n\\") || len(row.Path) > 2048 || seen[row.ID+row.Path] {
				continue
			}
			row.OriginRun = ""
			kept = append(kept, row)
			seen[row.ID+row.Path] = true
			if len(kept) == 50 {
				break
			}
		}
		return kept
	}
	if slices.Contains(extra.Supported, "skills") {
		out.Skills = project(extra.Skills)
		out.Unsupported = slices.DeleteFunc(out.Unsupported, func(kind string) bool { return kind == "skills" })
	}
	if slices.Contains(extra.Supported, "memory") {
		out.Memory = project(extra.Memory)
		out.Unsupported = slices.DeleteFunc(out.Unsupported, func(kind string) bool { return kind == "memory" })
	}
	return nil
}

// ordinarySourceDependencyRuns removes governance as an extra-store copy
// authority. It uses the same principal's own current roles, never fabricated
// groups, and requires an actual human for personal-origin graph projections.
func (p *Platform) ordinarySourceDependencyRuns(who auth.Principal, name string) []string {
	if who.Kind != auth.User || !who.Can(auth.RunsRead) || !who.Can(auth.KnowledgeRead) {
		return []string{}
	}
	ordinary := p.Seer(who)
	ordinary.govern = false
	ordinary.roles = ordinary.own
	src, err := p.Knowledge.Get(who.Tenant, name)
	if err != nil || (src.Lifecycle != nil && src.Lifecycle.Project != "" && !ordinary.Place(Place{Project: src.Lifecycle.Project})) {
		return []string{}
	}
	return p.SourceEvidence.RunReferences(who.Tenant, name, 200, func(id string, proof knowledge.CitationEvidence) bool {
		run, err := p.Runs.Get(who.Tenant, id)
		return err == nil && ordinary.Run(run) && (proof.Person == "" || proof.Person == who.Subject) && (proof.Project == "" || ordinary.Place(Place{Project: proof.Project}))
	})
}
