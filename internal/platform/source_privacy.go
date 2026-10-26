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
	return src.Lifecycle == nil || src.Lifecycle.Project == "" || p.Sees(who, Place{Project: src.Lifecycle.Project})
}

func (p *Platform) runtimeSourceAllowed(tenant, name, project string) bool {
	src, err := p.Knowledge.Get(tenant, name)
	if err != nil || src.Lifecycle == nil || src.Lifecycle.Project == "" {
		return true
	}
	return slices.ContainsFunc(p.Projects.Tree(tenant).Chain(project), func(pr projects.Project) bool { return pr.Name == src.Lifecycle.Project })
}

func (p *Platform) runtimeReaderSourceAllowed(tenant, name, project, person string) bool {
	if !p.runtimeSourceAllowed(tenant, name, project) {
		return false
	}
	src, err := p.Knowledge.Get(tenant, name)
	if err != nil || src.Lifecycle == nil || src.Lifecycle.Project == "" {
		return true
	}
	if person == "" {
		return false
	}
	return p.Sees(auth.Principal{Tenant: tenant, Subject: person, Kind: auth.User}, Place{Project: src.Lifecycle.Project})
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
	return src.Lifecycle == nil || src.Lifecycle.Project == "" || src.Lifecycle.Project == projects.NameOf(destinationProject)
}
