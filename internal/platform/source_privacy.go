package platform

import (
	"context"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"slices"
	"time"
)

const ToolSourceSearch = "source_search"

type SourceHealth struct {
	Name       string
	Cause      knowledge.Cause
	HardStale  bool
	LastGoodAt *time.Time
}

func (p *Platform) SourceHealth(tenant string, names []string, at time.Time) []SourceHealth {
	out := []SourceHealth{}
	for _, name := range names[:min(len(names), 50)] {
		src, err := p.Knowledge.Get(tenant, name)
		if err != nil || src.Lifecycle == nil {
			continue
		}
		stale := src.HardStale(at)
		if stale || src.Lifecycle.Status == knowledge.NeedsAttention {
			out = append(out, SourceHealth{Name: name, Cause: src.Lifecycle.Cause, HardStale: stale, LastGoodAt: src.Lifecycle.LastGoodAt})
		}
	}
	return out
}

func (p *Platform) SourceVisible(who auth.Principal, name string) bool {
	if !who.Can(auth.KnowledgeRead) {
		return false
	}
	src, err := p.Knowledge.Get(who.Tenant, name)
	if err != nil {
		return false
	}
	return p.sourceRecordVisible(who, src)
}

func (p *Platform) sourceRecordVisible(who auth.Principal, src knowledge.Source) bool {
	return who.Can(auth.KnowledgeRead) && (src.Lifecycle == nil || src.Lifecycle.Project == "" || p.Sees(who, Place{Project: src.Lifecycle.Project}))
}

// SourceAs authorizes the exact immutable record returned to the reader.
func (p *Platform) SourceAs(who auth.Principal, name string) (knowledge.Source, error) {
	src, err := p.Knowledge.Get(who.Tenant, name)
	if err != nil || !p.sourceRecordVisible(who, src) {
		return knowledge.Source{}, knowledge.ErrNotFound
	}
	return ShownSource(src.HealthAt(p.now())), nil
}

// ShownSource withholds derived external file inventory from public responses.
// Saved collection IDs remain configuration; variant exclusions come from
// retained provider inventory and cannot establish a reader's current access.
func ShownSource(src knowledge.Source) knowledge.Source {
	if src.RequiresUpstreamRead() {
		src.Variants = []knowledge.Variant{}
	}
	return src
}

func (p *Platform) runtimeSourceAllowed(tenant, name, project string) bool {
	src, err := p.Knowledge.Get(tenant, name)
	if err != nil {
		return false
	}
	if src.Lifecycle == nil || src.Lifecycle.Project == "" {
		return true
	}
	return slices.ContainsFunc(p.Projects.Tree(tenant).Chain(project), func(pr projects.Project) bool { return pr.Name == src.Lifecycle.Project })
}

func (p *Platform) runtimeReaderSourceAllowed(tenant, name, project, person string) bool {
	src, err := p.Knowledge.Get(tenant, name)
	return err == nil && p.runtimeSourceRecordAllowed(tenant, src, project, person)
}

// runtimeSourceRecordAllowed checks one exact source snapshot. The durable actor
// proves only its subject, so managed scoped copies require that exact owner
// and a current subject-based project role, without invented sign-in groups.
func (p *Platform) runtimeSourceRecordAllowed(tenant string, src knowledge.Source, project, person string) bool {
	if src.Lifecycle == nil || src.Lifecycle.Project == "" {
		return true
	}
	if !slices.ContainsFunc(p.Projects.Tree(tenant).Chain(project), func(pr projects.Project) bool { return pr.Name == src.Lifecycle.Project }) {
		return false
	}
	return person != "" && src.Lifecycle.Owner == person && p.Projects.Tree(tenant).Resolve(src.Lifecycle.Project, projects.Reader{Subject: person}).Role.AtLeast(projects.Viewer)
}

func (p *Platform) RunUsesPersonalSources(ctx context.Context, run runs.Run) bool {
	revision, err := p.Agents.Get(ctx, run.Tenant, run.Agent, run.Rev)
	return err != nil || slices.Contains(revision.Spec.Tools, ToolSourceSearch)
}

// SourceMayPublishIn is conservative about destination audiences. Being able
// to read a source yourself does not authorize publishing its text elsewhere.
// Source names with no retained metadata cannot prove publication is safe.
func (p *Platform) SourceMayPublishIn(tenant, name, destinationProject string) bool {
	src, err := p.Knowledge.Get(tenant, name)
	if err != nil {
		return false
	}
	if src.RequiresUpstreamRead() {
		return false
	}
	return src.Lifecycle == nil || src.Lifecycle.Project == "" || src.Lifecycle.Project == projects.NameOf(destinationProject)
}
