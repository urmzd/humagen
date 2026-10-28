package platform

import (
	"context"
	"time"

	"github.com/urmzd/humagen/pkg/knowledge"
)

// refreshSourceOnce uses current standing subject policy, never remembered
// sign-in groups. One provider read at a time bounds external work.
func (p *Platform) refreshSourceOnce(ctx context.Context) {
	for _, candidate := range p.Knowledge.RefreshDueEligible(p.now(), 200, func(tenant string, src knowledge.Source) bool {
		cfg := src.Lifecycle.SourceConfig
		return p.mayScheduleSource(tenant, cfg.Owner, cfg.Project)
	}) {
		if ctx.Err() != nil {
			return
		}
		cfg := candidate.Source.Lifecycle.SourceConfig
		if !p.mayScheduleSource(candidate.Tenant, cfg.Owner, cfg.Project) {
			continue
		}
		who := p.byPolicy(candidate.Tenant, cfg.Owner)
		if _, err := p.syncSourceExpected(ctx, who, candidate.Source.Name, false, &cfg); err != nil {
			p.log.Warn("source scheduled refresh refused", "error", err)
		}
		return
	}
}
func (p *Platform) refreshSources(ctx context.Context) error {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		p.refreshSourceOnce(ctx)
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}
