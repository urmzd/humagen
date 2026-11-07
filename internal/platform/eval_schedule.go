package platform

import (
	"context"
	"errors"
	"github.com/urmzd/humagen/pkg/evals"
	"time"
)

// runEvalSchedule is a leader loop. Begin's stored suite guard also prevents
// duplicate execution while a previous leader's bounded work is finishing.
func (p *Platform) runEvalSchedule(ctx context.Context) error {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		if err := p.runDueEvals(ctx); err != nil && ctx.Err() == nil {
			p.log.Error("platform: scheduled eval scan failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}
func (p *Platform) runDueEvals(ctx context.Context) error {
	return p.Evals.EachTenant(func(tenant string) error {
		suites, err := p.Evals.List(tenant)
		if err != nil {
			return err
		}
		for _, suite := range suites {
			if err := ctx.Err(); err != nil {
				return err
			}
			if suite.EverySeconds == 0 {
				continue
			}
			prior, err := p.Evals.Runs(tenant, suite.Name)
			if err != nil {
				return err
			}
			var last time.Time
			busy := false
			for _, r := range prior {
				if r.StartedAt.After(last) {
					last = r.StartedAt
				}
				busy = busy || r.Status == evals.Running
			}
			if busy || !last.IsZero() && p.now().Sub(last) < time.Duration(suite.EverySeconds)*time.Second {
				continue
			}
			if _, err = p.StartEval(ctx, tenant, suite.Name, "system:schedule"); err != nil && !errors.Is(err, evals.ErrBusy) {
				p.log.Warn("platform: scheduled eval could not start", "tenant", tenant, "suite", suite.Name, "error", err)
			}
		}
		return nil
	})
}
