package usage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/urmzd/humagen/pkg/policy"
	"github.com/urmzd/humagen/pkg/state"
)

// Counters are the ledger's counts in a Postgres database every process
// shares: runs admitted and spend per scope and UTC day, the runs in flight,
// what each run has spent, and the keys already counted.
//
// Every change to a tenant's counts is one transaction that takes the
// tenant's advisory lock, moves the counts and writes the ledger entry that
// moved them (state.WriteTx), so the entry and its counts commit together or
// not at all. An admission judges the counts read in that transaction, so two
// processes cannot both admit the last run a limit allows, and a call counted
// by one process is seen by the next admission in any other.
//
// Entries are kept under keys from a sequence in the database, not a
// process's own count, so two processes appending never overwrite each
// other's entries.
type Counters struct {
	pool *pgxpool.Pool
}

// Judge decides, from the counts read, whether an admission goes ahead.
type Judge func(read Reader) (bool, error)

// Reader reads what a scope has used since a time.
type Reader func(scope policy.Scope, since time.Time) (policy.Usage, error)

// countersLock is the first half of the advisory lock a tenant's counts are
// changed under; the second is a hash of the schema and the tenant.
const countersLock = 0x75736167

const countersSchema = `
CREATE SEQUENCE IF NOT EXISTS humagen_usage_seq;
CREATE TABLE IF NOT EXISTS humagen_usage_meta (
	id    int     PRIMARY KEY,
	built boolean NOT NULL
);
CREATE TABLE IF NOT EXISTS humagen_usage_days (
	tenant      text   COLLATE "C" NOT NULL,
	scope       text   COLLATE "C" NOT NULL,
	day         bigint NOT NULL,
	runs        bigint NOT NULL DEFAULT 0,
	micros      bigint NOT NULL DEFAULT 0,
	model_calls bigint NOT NULL DEFAULT 0,
	tokens      bigint NOT NULL DEFAULT 0,
	unpriced    bigint NOT NULL DEFAULT 0,
	other       bytea,
	PRIMARY KEY (tenant, scope, day)
);
CREATE TABLE IF NOT EXISTS humagen_usage_active (
	tenant  text   COLLATE "C" NOT NULL,
	run_id  text   COLLATE "C" NOT NULL,
	project text   NOT NULL,
	within  text[] NOT NULL,
	agent   text   NOT NULL,
	client  text   NOT NULL,
	PRIMARY KEY (tenant, run_id)
);
CREATE TABLE IF NOT EXISTS humagen_usage_runs (
	tenant  text        COLLATE "C" NOT NULL,
	run_id  text        COLLATE "C" NOT NULL,
	live    bytea       NOT NULL,
	settled bytea       NOT NULL,
	last    timestamptz NOT NULL,
	PRIMARY KEY (tenant, run_id)
);
CREATE INDEX IF NOT EXISTS humagen_usage_runs_last ON humagen_usage_runs (tenant, last);
CREATE TABLE IF NOT EXISTS humagen_usage_keys (
	tenant text        COLLATE "C" NOT NULL,
	key    text        COLLATE "C" NOT NULL,
	at     timestamptz NOT NULL,
	PRIMARY KEY (tenant, key)
);
CREATE INDEX IF NOT EXISTS humagen_usage_keys_at ON humagen_usage_keys (tenant, at);
`

// OpenCounters creates the counters' tables if they are not there. The
// caller owns the pool. The counts are filled from the ledger the first
// time a ledger is opened on them (Open with WithCounters).
func OpenCounters(ctx context.Context, pool *pgxpool.Pool) (*Counters, error) {
	if pool == nil {
		return nil, errors.New("usage: counters: no pool")
	}
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1, 0)`, int32(countersLock)); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, countersSchema)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("usage: counters: migrate: %w", err)
	}
	return &Counters{pool: pool}, nil
}

// dbScope is a scope key as the database keeps it: text cannot hold a NUL.
func dbScope(k string) string { return strings.Replace(k, "\x00", ":", 1) }

func lockTenant(ctx context.Context, tx pgx.Tx, tenant string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1, hashtext(current_schema() || ':' || $2))`, int32(countersLock), tenant)
	return err
}

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// usage reads a scope's counts since a day boundary.
func usageFrom(ctx context.Context, q querier, tenant string, scope policy.Scope, since time.Time) (policy.Usage, error) {
	var u policy.Usage
	var sum Spend
	rows, err := q.Query(ctx, `SELECT runs, micros, model_calls, tokens, unpriced, other FROM humagen_usage_days
		WHERE tenant = $1 AND scope = $2 AND day >= $3`, tenant, dbScope(scopeKey(scope)), policy.PeriodStart(policy.Day, since).Unix())
	if err != nil {
		return u, err
	}
	for rows.Next() {
		var runs, micros, calls, tokens, unpriced int64
		var other []byte
		if err := rows.Scan(&runs, &micros, &calls, &tokens, &unpriced, &other); err != nil {
			rows.Close()
			return u, err
		}
		s := Spend{Micros: micros, ModelCalls: int(calls), Tokens: tokens, Unpriced: int(unpriced)}
		if len(other) > 0 {
			if err := json.Unmarshal(other, &s.Other); err != nil {
				rows.Close()
				return u, err
			}
		}
		u.Runs += int(runs)
		sum = sum.add(s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return u, err
	}
	cond := ""
	args := []any{tenant}
	switch scope.Kind {
	case policy.Project:
		cond, args = ` AND (project = $2 OR $2 = ANY(within))`, append(args, scope.Name)
	case policy.Agent:
		cond, args = ` AND agent = $2`, append(args, scope.Name)
	case policy.Client:
		cond, args = ` AND client = $2`, append(args, scope.Name)
	}
	if err := q.QueryRow(ctx, `SELECT count(*) FROM humagen_usage_active WHERE tenant = $1`+cond, args...).Scan(&u.Concurrent); err != nil {
		return u, err
	}
	u.Spend, u.Tokens, u.ModelCalls, u.UnpricedCalls, u.OtherCosts = sum.Micros, sum.Tokens, sum.ModelCalls, sum.Unpriced, sorted(sum.Other)
	return u, nil
}

// Usage reads what a scope has used since a time, which must be the start
// of a UTC day: the counts are kept by day.
func (c *Counters) Usage(ctx context.Context, tenant string, scope policy.Scope, since time.Time) (policy.Usage, error) {
	return usageFrom(ctx, c.pool, tenant, scope, since)
}

// record applies e to the shared counts and writes it under a new key, as one
// transaction. A judge, for an admission, decides from the counts read in it.
// It reports the entry's key, what it added to the counts, and false when
// the judge refused it or its Spent or Settled key was already counted.
func (c *Counters) record(ctx context.Context, e Entry, judge Judge) (n int64, added Spend, ok bool, err error) {
	raw, err := json.Marshal(e)
	if err != nil {
		return 0, Spend{}, false, err
	}
	err = pgx.BeginFunc(ctx, c.pool, func(tx pgx.Tx) error {
		ok, added = false, Spend{}
		if err := lockTenant(ctx, tx, e.Tenant); err != nil {
			return err
		}
		a := admission{project: e.Project, agent: e.Agent, client: e.Client, runID: e.RunID, within: e.Within, at: e.At}
		switch e.Kind {
		case Admitted:
			if judge != nil {
				in, err := judge(func(scope policy.Scope, since time.Time) (policy.Usage, error) {
					return usageFrom(ctx, tx, e.Tenant, scope, since)
				})
				if err != nil || !in {
					return err
				}
			}
			within := e.Within
			if within == nil {
				within = []string{}
			}
			if _, err := tx.Exec(ctx, `INSERT INTO humagen_usage_active (tenant, run_id, project, within, agent, client) VALUES ($1, $2, $3, $4, $5, $6)
				ON CONFLICT (tenant, run_id) DO NOTHING`, e.Tenant, e.RunID, e.Project, within, e.Agent, e.Client); err != nil {
				return err
			}
			day := policy.PeriodStart(policy.Day, e.At).Unix()
			for _, k := range a.scopes() {
				if _, err := tx.Exec(ctx, `INSERT INTO humagen_usage_days (tenant, scope, day, runs) VALUES ($1, $2, $3, 1)
					ON CONFLICT (tenant, scope, day) DO UPDATE SET runs = humagen_usage_days.runs + 1`, e.Tenant, dbScope(k), day); err != nil {
					return err
				}
			}
			if err := retain(ctx, tx, e.Tenant, e.At); err != nil {
				return err
			}
		case Ended:
			if _, err := tx.Exec(ctx, `DELETE FROM humagen_usage_active WHERE tenant = $1 AND run_id = $2`, e.Tenant, e.RunID); err != nil {
				return err
			}
		case Spent, Settled:
			if e.Key == "" || e.Spend == nil {
				return nil
			}
			tag, err := tx.Exec(ctx, `INSERT INTO humagen_usage_keys (tenant, key, at) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, e.Tenant, e.Key, e.At)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return nil
			}
			if added, err = spendRun(ctx, tx, e); err != nil {
				return err
			}
			if err := addDay(ctx, tx, e.Tenant, a.scopes(), policy.PeriodStart(policy.Day, e.At).Unix(), added); err != nil {
				return err
			}
		}
		if err := tx.QueryRow(ctx, `SELECT nextval('humagen_usage_seq')`).Scan(&n); err != nil {
			return err
		}
		ok = true
		return state.WriteTx(ctx, tx, "usage", state.Put("", state.Seq(n), raw))
	})
	if err != nil {
		return 0, Spend{}, false, fmt.Errorf("usage: counters: %w", err)
	}
	return n, added, ok, nil
}

// spendRun folds a Spent or Settled entry into its run's row and returns what
// it adds to the counts, as apply does in memory.
func spendRun(ctx context.Context, tx pgx.Tx, e Entry) (Spend, error) {
	var r runSpend
	var live, settled []byte
	err := tx.QueryRow(ctx, `SELECT live, settled, last FROM humagen_usage_runs WHERE tenant = $1 AND run_id = $2`, e.Tenant, e.RunID).Scan(&live, &settled, &r.last)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return Spend{}, err
	default:
		if err := json.Unmarshal(live, &r.live); err != nil {
			return Spend{}, err
		}
		if err := json.Unmarshal(settled, &r.settled); err != nil {
			return Spend{}, err
		}
	}
	before := r.counted()
	if e.Kind == Spent {
		r.live = r.live.add(*e.Spend)
	} else {
		r.settled = *e.Spend
	}
	if e.At.After(r.last) {
		r.last = e.At
	}
	added := r.counted().beyond(before)
	if live, err = json.Marshal(r.live); err != nil {
		return Spend{}, err
	}
	if settled, err = json.Marshal(r.settled); err != nil {
		return Spend{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO humagen_usage_runs (tenant, run_id, live, settled, last) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (tenant, run_id) DO UPDATE SET live = EXCLUDED.live, settled = EXCLUDED.settled, last = EXCLUDED.last`,
		e.Tenant, e.RunID, live, settled, r.last)
	return added, err
}

// addDay adds spend to each scope's count for a day. The caller holds the
// tenant's lock, which is what makes reading and rewriting other currencies
// safe.
func addDay(ctx context.Context, tx pgx.Tx, tenant string, scopes []string, day int64, s Spend) error {
	if s.IsZero() {
		return nil
	}
	for _, k := range scopes {
		if _, err := tx.Exec(ctx, `INSERT INTO humagen_usage_days (tenant, scope, day, micros, model_calls, tokens, unpriced) VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (tenant, scope, day) DO UPDATE SET micros = humagen_usage_days.micros + EXCLUDED.micros,
				model_calls = humagen_usage_days.model_calls + EXCLUDED.model_calls,
				tokens = humagen_usage_days.tokens + EXCLUDED.tokens, unpriced = humagen_usage_days.unpriced + EXCLUDED.unpriced`,
			tenant, dbScope(k), day, s.Micros, s.ModelCalls, s.Tokens, s.Unpriced); err != nil {
			return err
		}
		if len(s.Other) == 0 {
			continue
		}
		var raw []byte
		if err := tx.QueryRow(ctx, `SELECT other FROM humagen_usage_days WHERE tenant = $1 AND scope = $2 AND day = $3`, tenant, dbScope(k), day).Scan(&raw); err != nil {
			return err
		}
		var other []policy.Cost
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &other); err != nil {
				return err
			}
		}
		merged, err := json.Marshal(addCosts(other, s.Other, 1))
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE humagen_usage_days SET other = $4 WHERE tenant = $1 AND scope = $2 AND day = $3`, tenant, dbScope(k), day, merged); err != nil {
			return err
		}
	}
	return nil
}

// retain drops a tenant's counts no period can still need, as prune does in
// memory.
func retain(ctx context.Context, tx pgx.Tx, tenant string, now time.Time) error {
	cutoff := policy.PeriodStart(policy.Day, now.Add(-keepAdmissions))
	b := &pgx.Batch{}
	b.Queue(`DELETE FROM humagen_usage_days WHERE tenant = $1 AND day < $2`, tenant, cutoff.Unix())
	b.Queue(`DELETE FROM humagen_usage_keys WHERE tenant = $1 AND at < $2`, tenant, cutoff)
	b.Queue(`DELETE FROM humagen_usage_runs WHERE tenant = $1 AND last < $2`, tenant, cutoff)
	return tx.SendBatch(ctx, b).Close()
}

// seed moves the sequence past every key the ledger loaded and, the first
// time the counters are opened on a database, fills them from the ledger's
// counts. The caller holds l.mu.
func (c *Counters) seed(ctx context.Context, l *Ledger) error {
	return pgx.BeginFunc(ctx, c.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1, 1)`, int32(countersLock)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT setval('humagen_usage_seq', GREATEST((SELECT last_value FROM humagen_usage_seq), $1, 1))`, l.seq); err != nil {
			return err
		}
		var built bool
		err := tx.QueryRow(ctx, `SELECT built FROM humagen_usage_meta WHERE id = 1`).Scan(&built)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if built {
			return nil
		}
		for name, t := range l.tenants {
			for _, d := range t.days {
				for k, n := range d.runs {
					if _, err := tx.Exec(ctx, `INSERT INTO humagen_usage_days (tenant, scope, day, runs) VALUES ($1, $2, $3, $4)
						ON CONFLICT (tenant, scope, day) DO UPDATE SET runs = humagen_usage_days.runs + EXCLUDED.runs`, name, dbScope(k), d.start.Unix(), n); err != nil {
						return err
					}
				}
				for k, s := range d.spend {
					if err := addDay(ctx, tx, name, []string{k}, d.start.Unix(), s); err != nil {
						return err
					}
				}
				for _, s := range d.spends {
					if _, err := tx.Exec(ctx, `INSERT INTO humagen_usage_keys (tenant, key, at) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, name, s.key, s.at); err != nil {
						return err
					}
				}
			}
			for id, a := range t.active {
				within := a.within
				if within == nil {
					within = []string{}
				}
				if _, err := tx.Exec(ctx, `INSERT INTO humagen_usage_active (tenant, run_id, project, within, agent, client) VALUES ($1, $2, $3, $4, $5, $6)
					ON CONFLICT (tenant, run_id) DO NOTHING`, name, id, a.project, within, a.agent, a.client); err != nil {
					return err
				}
			}
			for id, r := range t.runs {
				live, err := json.Marshal(r.live)
				if err != nil {
					return err
				}
				settled, err := json.Marshal(r.settled)
				if err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, `INSERT INTO humagen_usage_runs (tenant, run_id, live, settled, last) VALUES ($1, $2, $3, $4, $5)
					ON CONFLICT (tenant, run_id) DO NOTHING`, name, id, live, settled, r.last); err != nil {
					return err
				}
			}
		}
		_, err = tx.Exec(ctx, `INSERT INTO humagen_usage_meta (id, built) VALUES (1, true) ON CONFLICT (id) DO UPDATE SET built = true`)
		return err
	})
}
