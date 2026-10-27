package platform

import (
	"context"
	"github.com/urmzd/humagen/pkg/runs"
)

// MarkPrivateContext must succeed before a person-scoped tool executes. It
// shares admission with publication and never changes accepted tag identity.
func (p *Platform) MarkPrivateContext(tenant, run string) error {
	p.admission.Lock()
	defer p.admission.Unlock()
	return p.Runs.MarkPrivateContext(tenant, run)
}

func (p *Platform) runNeedsSummary(ctx context.Context, run runs.Run, destination string) bool {
	// A source name can be deleted and recreated with different authority.
	// Publication also checks retained query-time proofs so a later local
	// collection cannot relabel external or unknown historical evidence.
	for _, proof := range p.SourceEvidence.ForRun(run.Tenant, run.ID) {
		if proof.Provider != "manual" {
			return true
		}
	}
	if run.UsesPrivateContext() || run.Tag().Meta[MetaPersonalMemoryContext] == "true" || run.Tag().Meta[MetaPrivateLibraryContext] == "true" || p.ContextFlags.Has(run.Tenant, run.ID) || p.RunUsesPersonalSources(ctx, run) {
		return true
	}
	revision, err := p.Agents.Get(ctx, run.Tenant, run.Agent, run.Rev)
	if err != nil || p.grantsPersonScoped(ctx, run.Tenant, revision.Spec.Tools, run.CreatedAt) {
		return true
	}
	for _, source := range runs.SourcesOf(run.Tag().Meta) {
		if !p.SourceMayPublishIn(run.Tenant, source, destination) {
			return true
		}
	}
	return false
}

// RunRequiresReviewedSummary classifies the complete call tree for passive
// snapshots as well as live publication, including inherited private results.
func (p *Platform) RunRequiresReviewedSummary(ctx context.Context, run runs.Run, destination string) bool {
	tree, err := p.Runs.Tree(run.Tenant, run.ID)
	if err != nil {
		return true
	}
	for _, entry := range tree {
		if p.runNeedsSummary(ctx, entry, destination) {
			return true
		}
	}
	return false
}
