package platform

import (
	"context"
	"errors"
	"fmt"
	"time"

	drun "github.com/urmzd/saige/durable/run"
	"github.com/urmzd/saige/flow/runtime"
	"github.com/urmzd/saige/flow/thread"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/internal/wire/sessionstate"
)

// pendingPage is how many runs that have not ended are read from the ledger
// at once.
const pendingPage = 200

// MigrateJournalRequests rewrites each request tenant's thread journal holds
// to the request as it was asked (sessionstate.Journal.MigrateRequests). It
// reads each entry's run from the run ledger, and leaves every thread that
// holds a run the ledger has not ended, so a turn that is running is shown
// the history it was shown when it began. With apply false it writes nothing
// and reports what it would write; with replace, a request that is not
// matched to its run is set to sessionstate.UnkeptRequest.
func (p *Platform) MigrateJournalRequests(ctx context.Context, tenant string, apply, replace bool) (sessionstate.RequestReport, error) {
	busy, err := p.threadsWithRunsNotEnded(ctx, tenant)
	if err != nil {
		return sessionstate.RequestReport{Tenant: tenant, Apply: apply}, err
	}
	return sessionstate.NewJournal(p.sharedState()).MigrateRequests(ctx, sessionstate.RequestMigration{
		Tenant: tenant, Apply: apply, Replace: replace, Busy: busy,
		Request: p.askedRequest,
	})
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

// threadsWithRunsNotEnded are tenant's threads that hold a run the ledger
// has not ended, each to one such run, read a page at a time. A run whose
// input cannot be read is an error: its thread cannot be told.
func (p *Platform) threadsWithRunsNotEnded(ctx context.Context, tenant string) (map[thread.Key]string, error) {
	busy := map[thread.Key]string{}
	before := time.Now().Add(time.Minute)
	var cursor drun.PendingCursor
	for {
		rows, err := p.ledger.ListPending(ctx, before, cursor, pendingPage)
		if err != nil {
			return nil, fmt.Errorf("platform: runs not ended: %w", err)
		}
		for _, row := range rows {
			r, err := p.ledger.GetRun(ctx, row.ID)
			if errors.Is(err, drun.ErrNotFound) {
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("platform: run %s: %w", row.ID, err)
			}
			if r.Status.Terminal() || r.Name != runtime.ActivityTurn {
				continue
			}
			req, err := wire.RunRequestOf(r.Input)
			if err != nil {
				return nil, fmt.Errorf("platform: run %s: %w", row.ID, err)
			}
			if req.Thread.Tenant != tenant {
				continue
			}
			if _, ok := busy[req.Thread]; !ok {
				busy[req.Thread] = row.ID
			}
		}
		if len(rows) < pendingPage {
			return busy, nil
		}
		last := rows[len(rows)-1]
		cursor = drun.PendingCursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
}
