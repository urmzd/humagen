package platform

import (
	"context"
	"errors"
	"github.com/urmzd/humagen/pkg/evals"
	"time"
)

// EvalNextRun is when the schedule next starts a suite, as runDueEvals
// decides it, and whether the suite has a schedule. A scheduled suite that is
// due starts within a minute, so its next run is no earlier than now.
func (p *Platform) EvalNextRun(tenant string, suite evals.Suite) (time.Time, bool, error) {
	if suite.EverySeconds <= 0 {
		return time.Time{}, false, nil
	}
	prior, err := p.Evals.Runs(tenant, suite.Name)
	if err != nil {
		return time.Time{}, false, err
	}
	next, _ := evals.NextRun(suite, prior)
	if now := p.now(); next.Before(now) {
		next = now
	}
	return next, true, nil
}

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
			busy := false
			for _, r := range prior {
				busy = busy || r.Status == evals.Running
			}
			// The console shows the same next run (EvalNextRun).
			if next, _ := evals.NextRun(suite, prior); busy || p.now().Before(next) {
				continue
			}
			if _, err = p.StartEval(ctx, tenant, suite.Name, "system:schedule"); err != nil && !errors.Is(err, evals.ErrBusy) {
				p.log.Warn("platform: scheduled eval could not start", "tenant", tenant, "suite", suite.Name, "error", err)
			}
		}
		return nil
	})
}
