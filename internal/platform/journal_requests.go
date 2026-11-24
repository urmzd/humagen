package platform

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	drun "github.com/urmzd/saige/durable/run"
	"github.com/urmzd/saige/flow/runtime"
	"github.com/urmzd/saige/flow/thread"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/internal/wire/sessionstate"
	"github.com/urmzd/humagen/pkg/runs"
)

// pendingPage is how many runs that have not ended are read from the ledger
// at once.
const pendingPage = 200

// JournalMigration rewrites each request a thread journal holds to the
// request as it was asked (sessionstate.Journal.MigrateRequests), one tenant
// at a time. It reads the runs the ledger has not ended once, for every
// tenant, and again just before each entry it writes.
type JournalMigration struct {
	p     *Platform
	turns *pendingTurns
	// first is the threads that held a run that had not ended when the
	// migration started, and unreadable the runs that had not ended whose
	// input could not be read.
	first      map[thread.Key]string
	unreadable []string
	// holders are the tenants the run index holds each unreadable run in.
	holders map[string][]string
}

// StartJournalMigration reads the runs the ledger has not ended, for every
// tenant.
func (p *Platform) StartJournalMigration(ctx context.Context) (*JournalMigration, error) {
	m := &JournalMigration{p: p, turns: &pendingTurns{p: p, threads: map[string]pendingTurn{}}, holders: map[string][]string{}}
	busy, unreadable, err := m.turns.scan(ctx)
	if err != nil {
		return nil, err
	}
	m.first, m.unreadable = busy, unreadable
	return m, nil
}

// Migrate rewrites tenant's journaled requests. It leaves every thread that
// holds a run the ledger has not ended, so a turn that is running is shown
// the history it was shown when it began, and checks again just before each
// entry it writes. A run that has not ended and whose input cannot be read
// has no thread to leave: when it may be tenant's (the run index holds it in
// tenant, or in no tenant), tenant is left whole and the report lists it as
// sessionstate.TenantLeft. With apply false it writes nothing and reports
// what it would write; with replace, a request that is not matched to its
// run is set to sessionstate.UnkeptRequest.
func (m *JournalMigration) Migrate(ctx context.Context, tenant string, apply, replace bool) (sessionstate.RequestReport, error) {
	run, err := m.unsafeFor(tenant, m.unreadable)
	if err != nil {
		return sessionstate.RequestReport{Tenant: tenant, Apply: apply}, err
	}
	if run != "" {
		rep := sessionstate.RequestReport{Tenant: tenant, Apply: apply, Counts: map[string]int{sessionstate.TenantLeft: 1}}
		rep.Lines = []sessionstate.RequestLine{{Kind: sessionstate.TenantLeft, Key: tenant, Run: run, Detail: "its input cannot be read"}}
		return rep, nil
	}
	return sessionstate.NewJournal(m.p.sharedState()).MigrateRequests(ctx, sessionstate.RequestMigration{
		Tenant: tenant, Apply: apply, Replace: replace, Busy: m.first,
		Request: m.p.askedRequest,
		Recheck: func(ctx context.Context) (map[thread.Key]string, string, error) {
			busy, unreadable, err := m.turns.scan(ctx)
			if err != nil {
				return nil, "", err
			}
			run, err := m.unsafeFor(tenant, unreadable)
			return busy, run, err
		},
	})
}

// unsafeFor is the first of the unreadable runs that may be tenant's: one the
// run index holds in tenant, or in no tenant at all.
func (m *JournalMigration) unsafeFor(tenant string, unreadable []string) (string, error) {
	for _, id := range unreadable {
		holders, ok := m.holders[id]
		if !ok {
			holders = []string{}
			err := m.p.Runs.EachTenant(func(t string) error {
				_, err := m.p.Runs.Get(t, id)
				if err == nil {
					holders = append(holders, t)
					return nil
				}
				if errors.Is(err, runs.ErrNotFound) {
					return nil
				}
				return err
			})
			if err != nil {
				return "", fmt.Errorf("platform: run %s: %w", id, err)
			}
			m.holders[id] = holders
		}
		if len(holders) == 0 || slices.Contains(holders, tenant) {
			return id, nil
		}
	}
	return "", nil
}

// askedRequest reads one run's request from the run ledger.
func (p *Platform) askedRequest(ctx context.Context, runID string) (sessionstate.AskedRequest, error) {
	r, err := p.ledger.GetRun(ctx, runID)
	if errors.Is(err, drun.ErrNotFound) {
		return sessionstate.AskedRequest{Missing: "the ledger holds no such run"}, nil
	}
	if err != nil {
		return sessionstate.AskedRequest{}, err
	}
	if r.Name != runtime.ActivityTurn {
		return sessionstate.AskedRequest{Missing: "the run is not a turn"}, nil
	}
	req, err := wire.RunRequestOf(r.Input)
	if err != nil {
		return sessionstate.AskedRequest{Missing: "the run's input cannot be read"}, nil
	}
	return sessionstate.AskedRequest{Thread: req.Thread, Asked: req.Asked, WithPassages: req.WithPassages}, nil
}

// pendingTurn is what pendingTurns learned of one run: the thread of a turn,
// or that its input cannot be read, or that it is not a turn that has not
// ended (skip).
type pendingTurn struct {
	thread     thread.Key
	unreadable bool
	skip       bool
}

// pendingTurns reads the runs the ledger has not ended. Each run is read and
// decoded once: a run's thread does not change, and a run that has ended or
// is not a turn stays so.
type pendingTurns struct {
	p       *Platform
	threads map[string]pendingTurn
}

// scan lists the runs the ledger has not ended, a page at a time, and
// returns the threads that hold one, each to one such run, and the runs
// whose input cannot be read, whatever their tenant.
func (s *pendingTurns) scan(ctx context.Context) (map[thread.Key]string, []string, error) {
	busy := map[thread.Key]string{}
	var unreadable []string
	before := time.Now().Add(time.Minute)
	var cursor drun.PendingCursor
	for {
		rows, err := s.p.ledger.ListPending(ctx, before, cursor, pendingPage)
		if err != nil {
			return nil, nil, fmt.Errorf("platform: runs not ended: %w", err)
		}
		for _, row := range rows {
			t, ok, err := s.turn(ctx, row.ID)
			if err != nil {
				return nil, nil, err
			}
			switch {
			case !ok || t.skip:
			case t.unreadable:
				if !slices.Contains(unreadable, row.ID) {
					unreadable = append(unreadable, row.ID)
				}
			default:
				if _, held := busy[t.thread]; !held {
					busy[t.thread] = row.ID
				}
			}
		}
		if len(rows) < pendingPage {
			return busy, unreadable, nil
		}
		last := rows[len(rows)-1]
		cursor = drun.PendingCursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
}

// turn is what the ledger says of run id, read once. ok is false for a run
// the ledger no longer holds, which is read again on the next scan.
func (s *pendingTurns) turn(ctx context.Context, id string) (pendingTurn, bool, error) {
	if t, ok := s.threads[id]; ok {
		return t, true, nil
	}
	r, err := s.p.ledger.GetRun(ctx, id)
	if errors.Is(err, drun.ErrNotFound) {
		return pendingTurn{}, false, nil
	}
	if err != nil {
		return pendingTurn{}, false, fmt.Errorf("platform: run %s: %w", id, err)
	}
	var t pendingTurn
	if r.Status.Terminal() || r.Name != runtime.ActivityTurn {
		t.skip = true
	} else if req, err := wire.RunRequestOf(r.Input); err != nil {
		t.unreadable = true
	} else {
		t.thread = req.Thread
	}
	s.threads[id] = t
	return t, true, nil
}
