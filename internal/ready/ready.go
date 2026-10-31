// Package ready answers whether this process should be sent traffic: its
// connections answer, it finished starting, and it is not draining. It is
// the readiness probe; /healthz, whether the process is alive, is separate.
package ready

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// Statuses a Report carries.
const (
	Ready       = "ready"
	Starting    = "starting"
	Draining    = "draining"
	Unavailable = "unavailable"
)

// checkTimeout bounds each check, below a probe's usual timeout.
const checkTimeout = 2 * time.Second

// Check is one connection the process needs, such as its database.
type Check struct {
	Name  string
	Probe func(ctx context.Context) error
}

// Result is one check's outcome. Why a check failed is logged, never
// answered: the probe is public.
type Result struct {
	Name string `json:"name"`
	OK   bool   `json:"ok"`
}

// Report is the answer to a readiness probe.
type Report struct {
	Status string   `json:"status"`
	Checks []Result `json:"checks"`
}

// Gate holds the process's readiness. The zero Gate is starting and has no
// checks.
type Gate struct {
	started  atomic.Bool
	draining atomic.Bool
	log      *slog.Logger

	mu     sync.Mutex
	checks []Check
}

// New returns a gate over checks, starting.
func New(log *slog.Logger, checks ...Check) *Gate {
	if log == nil {
		log = slog.Default()
	}
	return &Gate{log: log, checks: checks}
}

// Add adds a check.
func (g *Gate) Add(c Check) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.checks = append(g.checks, c)
}

// Started marks the process as having finished starting.
func (g *Gate) Started() { g.started.Store(true) }

// Drain marks the process as stopping: it is not ready from now on.
func (g *Gate) Drain() { g.draining.Store(true) }

// Draining reports whether Drain was called.
func (g *Gate) Draining() bool { return g.draining.Load() }

// Check runs every check, at once, and reports whether the process is ready.
// A draining or starting process is not ready whatever its checks say, and
// its checks still run, so the answer shows them.
func (g *Gate) Check(ctx context.Context) (Report, bool) {
	g.mu.Lock()
	checks := append([]Check(nil), g.checks...)
	g.mu.Unlock()
	results := make([]Result, len(checks))
	var wg sync.WaitGroup
	for i, c := range checks {
		wg.Go(func() {
			cctx, cancel := context.WithTimeout(ctx, checkTimeout)
			defer cancel()
			err := c.Probe(cctx)
			if err != nil {
				g.logger().Warn("ready: check failed", "check", c.Name, "error", err)
			}
			results[i] = Result{Name: c.Name, OK: err == nil}
		})
	}
	wg.Wait()
	r := Report{Status: Ready, Checks: results}
	for _, res := range results {
		if !res.OK {
			r.Status = Unavailable
		}
	}
	switch {
	case g.draining.Load():
		r.Status = Draining
	case !g.started.Load():
		r.Status = Starting
	}
	return r, r.Status == Ready
}

func (g *Gate) logger() *slog.Logger {
	if g.log == nil {
		return slog.Default()
	}
	return g.log
}
