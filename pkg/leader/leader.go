// Package leader elects one process to run the work a deployment must do
// once, however many replicas it runs: housekeeping on a ticker, schedules,
// reminders. Work that each replica does for its own share, such as a
// tenant's delivery, is not elected here.
//
// An Elector calls a term function each time this process becomes the
// leader, with a context that ends when it stops being one. A term may be
// cut short at any moment, by a lost connection or a shutdown, so the work
// in it must be safe to stop and to repeat on another process.
package leader

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
)

// Term is the work done while this process leads. It returns when ctx ends;
// returning early gives up leadership.
type Term func(ctx context.Context)

// Elector campaigns for leadership.
type Elector interface {
	// Campaign blocks until ctx is done. Each time this process wins it
	// calls term, and it campaigns again once the term ends.
	Campaign(ctx context.Context, term Term) error
}

// Always is the elector of a deployment of one process: it leads at once
// and for as long as it runs.
type Always struct{}

// Campaign implements Elector.
func (Always) Campaign(ctx context.Context, term Term) error {
	term(ctx)
	<-ctx.Done()
	return nil
}

// LockClass is the first half of the advisory lock a Postgres elector
// holds. The second half is the schema's hash, so two deployments in two
// schemas of one database each elect their own leader.
const LockClass = 0x6c656164 // "lead"

// DefaultInterval is how often a follower asks for the lock and the leader
// checks the connection that holds it.
const DefaultInterval = 2 * time.Second

// A leader's check of its connection may take up to pingFor intervals, and
// a process that wins the lock waits graceFor intervals before its term
// starts. The lock is free the moment the old leader's session ends, but the
// old leader learns it only at its next failed check; the grace outlasts
// that check, so the old term has ended before the new one begins.
const (
	pingFor  = 2
	graceFor = pingFor + 1
)

// Postgres elects a leader with a session advisory lock held on a
// connection of its own. The process whose connection holds the lock leads;
// when that connection ends, for any reason, Postgres releases the lock and
// another process takes it at its next try.
type Postgres struct {
	connect  func(context.Context) (*pgx.Conn, error)
	interval time.Duration
	log      *slog.Logger
}

// Option configures a Postgres elector.
type Option func(*Postgres)

// WithInterval sets how often a follower asks for the lock and the leader
// checks its connection. A lost leader stops within about three intervals,
// and a new one starts about three intervals after it wins the lock.
func WithInterval(d time.Duration) Option {
	return func(e *Postgres) {
		if d > 0 {
			e.interval = d
		}
	}
}

// WithLogger sets where elections are logged.
func WithLogger(l *slog.Logger) Option {
	return func(e *Postgres) {
		if l != nil {
			e.log = l
		}
	}
}

// NewPostgres returns an elector that opens its own connection from cfg,
// apart from any pool, so the lock lives exactly as long as that session.
// cfg's search_path decides the schema, and so the deployment, it elects in.
func NewPostgres(cfg *pgx.ConnConfig, opts ...Option) (*Postgres, error) {
	if cfg == nil {
		return nil, errors.New("leader: no connection config")
	}
	cfg = cfg.Copy()
	e := &Postgres{
		connect:  func(ctx context.Context) (*pgx.Conn, error) { return pgx.ConnectConfig(ctx, cfg) },
		interval: DefaultInterval,
		log:      slog.New(slog.DiscardHandler),
	}
	for _, o := range opts {
		o(e)
	}
	return e, nil
}

// Campaign implements Elector.
func (e *Postgres) Campaign(ctx context.Context, term Term) error {
	for {
		conn, err := e.try(ctx)
		switch {
		case err != nil && ctx.Err() == nil:
			e.log.Warn("leader: could not ask for the lock", "error", err)
		case conn != nil:
			e.lead(ctx, conn, term)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(e.interval):
		}
	}
}

// try opens a connection and asks for the lock on it. It returns the
// connection when the lock is held, and closes it otherwise.
func (e *Postgres) try(ctx context.Context) (*pgx.Conn, error) {
	ask, cancel := context.WithTimeout(ctx, e.interval)
	defer cancel()
	conn, err := e.connect(ask)
	if err != nil {
		return nil, err
	}
	var held bool
	if err := conn.QueryRow(ask, `SELECT pg_try_advisory_lock($1, hashtext(current_schema()))`, int32(LockClass)).Scan(&held); err != nil {
		_ = conn.Close(context.Background())
		return nil, fmt.Errorf("leader: lock: %w", err)
	}
	if !held {
		_ = conn.Close(context.Background())
		return nil, nil
	}
	return conn, nil
}

// grace waits out the old leader's last check, and reports whether conn
// still holds the lock after it.
func (e *Postgres) grace(ctx context.Context, conn *pgx.Conn) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(graceFor * e.interval):
	}
	ping, stop := context.WithTimeout(ctx, pingFor*e.interval)
	defer stop()
	return conn.Ping(ping) == nil
}

// lead runs one term on conn, which holds the lock, and ends it when ctx
// ends, the term returns, or the connection stops answering. The term has
// returned by the time lead does, so two terms never overlap in one process.
func (e *Postgres) lead(ctx context.Context, conn *pgx.Conn, term Term) {
	if !e.grace(ctx, conn) {
		bye, stop := context.WithTimeout(context.Background(), e.interval)
		defer stop()
		_, _ = conn.Exec(bye, `SELECT pg_advisory_unlock($1, hashtext(current_schema()))`, int32(LockClass))
		_ = conn.Close(bye)
		return
	}
	tctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		term(tctx)
	}()
	tick := time.NewTicker(e.interval)
	defer tick.Stop()
	end := func(release bool) {
		cancel()
		<-done
		bye, stop := context.WithTimeout(context.Background(), e.interval)
		defer stop()
		if release {
			_, _ = conn.Exec(bye, `SELECT pg_advisory_unlock($1, hashtext(current_schema()))`, int32(LockClass))
		}
		_ = conn.Close(bye)
	}
	for {
		select {
		case <-ctx.Done():
			end(true)
			return
		case <-done:
			end(true)
			return
		case <-tick.C:
			// While the session is alive it holds the lock: a session lock
			// is released only by its owner or when the session ends.
			ping, stop := context.WithTimeout(ctx, pingFor*e.interval)
			err := conn.Ping(ping)
			stop()
			if err != nil && ctx.Err() == nil {
				e.log.Warn("leader: lost the connection that holds the lock", "error", err)
				end(false)
				return
			}
		}
	}
}
