package platform

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/urmzd/humagen/pkg/leader"
)

// ProcessRole is which parts of the platform a process runs, so the API and the
// execution of turns scale on their own (RFC 0004, a million people in one
// room).
type ProcessRole string

const (
	// RoleAll runs everything in one process: the API and the turns. It is
	// the default, and what a deployment of one process runs.
	RoleAll ProcessRole = "all"
	// RoleAPI serves the API and admits runs onto the shared queue, and
	// executes none of them.
	RoleAPI ProcessRole = "api"
	// RoleWorker executes the turns on the shared queue and serves no API.
	RoleWorker ProcessRole = "worker"
)

// ProcessRoles are the roles a process may take, in the order they are offered.
var ProcessRoles = []ProcessRole{RoleAll, RoleAPI, RoleWorker}

// ParseProcessRole reads a role by name. Empty is RoleAll.
func ParseProcessRole(s string) (ProcessRole, error) {
	if s == "" {
		return RoleAll, nil
	}
	for _, r := range ProcessRoles {
		if string(r) == s {
			return r, nil
		}
	}
	return "", fmt.Errorf("platform: role %q is not all, api or worker", s)
}

// Serves reports whether the role answers the API.
func (r ProcessRole) Serves() bool { return r != RoleWorker }

// Executes reports whether the role runs the queue's turns.
func (r ProcessRole) Executes() bool { return r != RoleAPI }

// Process is what this process is in its deployment: its role, and whether
// it leads, running the work done once per deployment. A readiness probe
// reads it.
type Process struct {
	Role ProcessRole `json:"role"`
	// Leader is whether this process runs the deployment's singleton work
	// now: the reconciler, schedules, inbox retention, email notices,
	// knowledge refresh and catalog crawls.
	Leader bool `json:"leader"`
	// LeaderSince is when this process last became the leader. Zero when it
	// never has.
	LeaderSince time.Time `json:"leader_since,omitzero"`
}

// leadership is what Process reports of the elector.
type leadership struct {
	leading atomic.Bool
	since   atomic.Int64
}

// Process reports this process's role and leadership.
func (p *Platform) Process() Process {
	out := Process{Role: p.role, Leader: p.lead.leading.Load()}
	if ns := p.lead.since.Load(); ns != 0 {
		out.LeaderSince = time.Unix(0, ns).UTC()
	}
	return out
}

// checkRole refuses a role that would leave runs with nobody to execute
// them: a split across processes needs the run ledger and queue on a
// database every process shares.
func checkRole(r ProcessRole, b Backends) error {
	if r != RoleAll && b.Queue == nil {
		return fmt.Errorf("platform: role %s shares the run queue with other processes, so it needs the ledger and queue on Postgres", r)
	}
	return nil
}

// elector is the configured elector, or one that always leads.
func (p *Platform) elector() leader.Elector {
	if p.cfg.Leader != nil {
		return p.cfg.Leader
	}
	return leader.Always{}
}

// campaign runs the singleton loops while this process leads. Each term
// starts every loop and waits for all of them to stop before the next term
// can start, so a loop never runs twice in one process. A loop that fails
// stops the platform through fail, as every other loop does.
func (p *Platform) campaign(ctx context.Context, loops map[string]func(context.Context) error, fail func(name string, err error)) error {
	return p.elector().Campaign(ctx, func(term context.Context) {
		p.lead.since.Store(p.now().UnixNano())
		p.lead.leading.Store(true)
		p.log.Info("platform: leading", "role", p.role)
		defer func() {
			p.lead.leading.Store(false)
			if ctx.Err() == nil {
				p.log.Warn("platform: no longer leading", "role", p.role)
			}
		}()
		done := make(chan struct{}, len(loops))
		for name, fn := range loops {
			go func() {
				defer func() { done <- struct{}{} }()
				if err := fn(term); err != nil && term.Err() == nil {
					fail(name, err)
				}
			}()
		}
		for range loops {
			<-done
		}
	})
}
