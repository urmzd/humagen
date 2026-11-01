package usage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
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
// Every change to the counts is one transaction that moves them and writes
// the ledger entry that moved them (state.WriteTx), so the entry and its
// counts commit together or not at all.
//
// # Striped admission
//
// The counts an admission moves, runs started and runs in flight, are kept
// in Stripes rows per scope, so admissions do not all write one row. A
// quota's limit is split into a share per stripe. An admission picks a
// stripe at random, locks it in each scope it has a quota in, and goes ahead
// on it when it has room for every quota, counting the other stripes at
// their share or their count, whichever is more: no other stripe can grow
// past that while this one is held, so the limit holds. When the stripe has
// no room, the admission locks every stripe of those scopes, in order, and
// goes ahead on any that has room if the scope's total does. So the limit
// is exact and an admission is refused only when the total has reached it;
// only near the limit do admissions wait on each other. An admission with
// no quota takes no lock.
//
// Spend is still counted under the tenant's advisory lock: a call's spend
// reads and rewrites its run's row.
//
// Entries are kept in the state table, under their tenant (see keys.go),
// at places from a sequence in the database, not a process's own count, so
// two processes appending never overwrite each other's entries.
type Counters struct {
	pool *pgxpool.Pool
}

// Stripes is how many rows an admission's counts are spread over, per scope.
const Stripes = 8

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
CREATE TABLE IF NOT EXISTS humagen_usage_stripes (
	tenant text COLLATE "C" NOT NULL,
	scope  text COLLATE "C" NOT NULL,
	stripe int  NOT NULL,
	PRIMARY KEY (tenant, scope, stripe)
);
CREATE TABLE IF NOT EXISTS humagen_usage_admits (
	tenant text   COLLATE "C" NOT NULL,
	scope  text   COLLATE "C" NOT NULL,
	day    bigint NOT NULL,
	stripe int    NOT NULL,
	runs   bigint NOT NULL DEFAULT 0,
	PRIMARY KEY (tenant, scope, day, stripe)
);
CREATE TABLE IF NOT EXISTS humagen_usage_slots (
	tenant text COLLATE "C" NOT NULL,
	scope  text COLLATE "C" NOT NULL,
	run_id text COLLATE "C" NOT NULL,
	stripe int  NOT NULL,
	PRIMARY KEY (tenant, scope, run_id)
);
CREATE INDEX IF NOT EXISTS humagen_usage_slots_run ON humagen_usage_slots (tenant, run_id);
`

// The tables a release before striped admission wrote are read as stripe 0:
// humagen_usage_days.runs, which nothing writes now, and the runs in
// humagen_usage_active, given slots once when the counters are opened.
// Neither grows, so a stripe's count is still bounded as the package says.

// OpenCounters creates the counters' tables if they are not there. The
// caller owns the pool. The counts are filled from an earlier release's
// entries the first time a ledger is opened on them (Open with
// WithCounters).
func OpenCounters(ctx context.Context, pool *pgxpool.Pool) (*Counters, error) {
	if pool == nil {
		return nil, errors.New("usage: counters: no pool")
	}
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1, 0)`, int32(countersLock)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, countersSchema); err != nil {
			return err
		}
		return slotActive(ctx, tx)
	})
	if err != nil {
		return nil, fmt.Errorf("usage: counters: migrate: %w", err)
	}
	return &Counters{pool: pool}, nil
}

// slotActive gives every run in flight that has no slots its slots, on
// stripe 0, once: the runs a release before striped admission let in.
func slotActive(ctx context.Context, tx pgx.Tx) error {
	var done bool
	err := tx.QueryRow(ctx, `SELECT built FROM humagen_usage_meta WHERE id = 2`).Scan(&done)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if done {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT tenant, run_id, project, within, agent, client FROM humagen_usage_active`)
	if err != nil {
		return err
	}
	type run struct {
		tenant string
		a      admission
	}
	runs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (run, error) {
		var r run
		err := row.Scan(&r.tenant, &r.a.runID, &r.a.project, &r.a.within, &r.a.agent, &r.a.client)
		return r, err
	})
	if err != nil {
		return err
	}
	for _, r := range runs {
		if err := addSlots(ctx, tx, r.tenant, r.a.runID, r.a.scopes(), nil); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO humagen_usage_meta (id, built) VALUES (2, true) ON CONFLICT (id) DO UPDATE SET built = true`)
	return err
}

// addSlots puts a run in flight in each scope, on the stripe stripe names
// for the scope, or 0.
func addSlots(ctx context.Context, tx pgx.Tx, tenant, runID string, scopes []string, stripe map[string]int) error {
	for _, k := range scopes {
		if _, err := tx.Exec(ctx, `INSERT INTO humagen_usage_slots (tenant, scope, run_id, stripe) VALUES ($1, $2, $3, $4)
			ON CONFLICT (tenant, scope, run_id) DO NOTHING`, tenant, dbScope(k), runID, stripe[k]); err != nil {
			return err
		}
	}
	return nil
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
	var admitted int64
	if err := q.QueryRow(ctx, `SELECT coalesce(sum(runs), 0) FROM humagen_usage_admits WHERE tenant = $1 AND scope = $2 AND day >= $3`,
		tenant, dbScope(scopeKey(scope)), policy.PeriodStart(policy.Day, since).Unix()).Scan(&admitted); err != nil {
		return u, err
	}
	u.Runs += int(admitted)
	u.Spend, u.Tokens, u.ModelCalls, u.UnpricedCalls, u.OtherCosts = sum.Micros, sum.Tokens, sum.ModelCalls, sum.Unpriced, sorted(sum.Other)
	return u, nil
}

// Usage reads what a scope has used since a time, which must be the start
// of a UTC day: the counts are kept by day.
func (c *Counters) Usage(ctx context.Context, tenant string, scope policy.Scope, since time.Time) (policy.Usage, error) {
	return usageFrom(ctx, c.pool, tenant, scope, since)
}

// runSpend reads what one run has spent.
func (c *Counters) runSpend(ctx context.Context, tenant, run string) (Spend, error) {
	var r runRec
	var live, settled []byte
	err := c.pool.QueryRow(ctx, `SELECT live, settled FROM humagen_usage_runs WHERE tenant = $1 AND run_id = $2`, tenant, run).Scan(&live, &settled)
	if errors.Is(err, pgx.ErrNoRows) {
		return Spend{}, nil
	}
	if err != nil {
		return Spend{}, err
	}
	if err := json.Unmarshal(live, &r.Live); err != nil {
		return Spend{}, err
	}
	if err := json.Unmarshal(settled, &r.Settled); err != nil {
		return Spend{}, err
	}
	return r.counted(), nil
}

// record applies an entry other than an admission with quotas to the shared
// counts and writes it under a new key, as one transaction. It reports the
// entry's key, what it added to the counts, and false when its Spent or
// Settled key was already counted.
func (c *Counters) record(ctx context.Context, e Entry) (n int64, added Spend, ok bool, err error) {
	if e.Kind == Admitted {
		n, ok, _, err = c.admit(ctx, e, nil)
		return n, Spend{}, ok, err
	}
	err = pgx.BeginFunc(ctx, c.pool, func(tx pgx.Tx) error {
		ok, added = false, Spend{}
		a := admissionOf(e)
		var counted *Spend
		switch e.Kind {
		case Ended:
			// Removing a run's rows needs no lock: nothing reads and
			// rewrites them. A run no longer in flight has ended already,
			// and its end is kept once.
			tag, err := tx.Exec(ctx, `DELETE FROM humagen_usage_active WHERE tenant = $1 AND run_id = $2`, e.Tenant, e.RunID)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `DELETE FROM humagen_usage_slots WHERE tenant = $1 AND run_id = $2`, e.Tenant, e.RunID); err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return nil
			}
		case Spent, Settled:
			if e.Key == "" || e.Spend == nil {
				return nil
			}
			if err := lockTenant(ctx, tx, e.Tenant); err != nil {
				return err
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
			counted = &added
			if err := addDay(ctx, tx, e.Tenant, a.scopes(), policy.PeriodStart(policy.Day, e.At).Unix(), added); err != nil {
				return err
			}
		}
		if n, err = writeEntry(ctx, tx, e, counted); err != nil {
			return err
		}
		ok = true
		return nil
	})
	if err != nil {
		return 0, Spend{}, false, fmt.Errorf("usage: counters: %w", err)
	}
	return n, added, ok, nil
}

// writeEntry writes an entry at the next place of the sequence.
func writeEntry(ctx context.Context, tx pgx.Tx, e Entry, added *Spend) (int64, error) {
	var n int64
	if err := tx.QueryRow(ctx, `SELECT nextval('humagen_usage_seq')`).Scan(&n); err != nil {
		return 0, err
	}
	return n, keepEntry(ctx, tx, e, added, n)
}

// keepEntry writes an entry's records at a place.
func keepEntry(ctx context.Context, tx pgx.Tx, e Entry, added *Spend, n int64) error {
	ops, err := entryOps(e, added, n)
	if err != nil {
		return err
	}
	return state.WriteTx(ctx, tx, namespace, ops...)
}

// share is a stripe's part of a limit: the limit split as evenly as it goes.
func share(max, stripe int) int64 {
	n := int64(max / Stripes)
	if stripe < max%Stripes {
		n++
	}
	return n
}

// quotaScopes is the scopes quotas hold, in the order their stripes are
// locked.
func quotaScopes(quotas []Quota) []string {
	var out []string
	for _, q := range quotas {
		k := dbScope(scopeKey(q.Scope))
		if !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}

// counted reads what each stripe of a quota's scope has counted.
func counted(ctx context.Context, tx pgx.Tx, tenant string, q Quota) ([Stripes]int64, error) {
	var by [Stripes]int64
	k := dbScope(scopeKey(q.Scope))
	var rows pgx.Rows
	var err error
	if q.Kind == QuotaConcurrent {
		rows, err = tx.Query(ctx, `SELECT stripe, count(*) FROM humagen_usage_slots WHERE tenant = $1 AND scope = $2 GROUP BY stripe`, tenant, k)
	} else {
		day := policy.PeriodStart(policy.Day, q.Since).Unix()
		rows, err = tx.Query(ctx, `SELECT stripe, sum(runs) FROM humagen_usage_admits WHERE tenant = $1 AND scope = $2 AND day >= $3 GROUP BY stripe
			UNION ALL SELECT 0, coalesce(sum(runs), 0) FROM humagen_usage_days WHERE tenant = $1 AND scope = $2 AND day >= $3`, tenant, k, day)
	}
	if err != nil {
		return by, err
	}
	defer rows.Close()
	for rows.Next() {
		var stripe int
		var n int64
		if err := rows.Scan(&stripe, &n); err != nil {
			return by, err
		}
		// A stripe past Stripes, from a release with more, counts in 0.
		if stripe < 0 || stripe >= Stripes {
			stripe = 0
		}
		by[stripe] += n
	}
	return by, rows.Err()
}

// errNoRoom ends a try that found no room on its stripe.
var errNoRoom = errors.New("usage: no room on this stripe")

// admit takes an admission's quotas and records it. It reports the entry's
// place, whether it was admitted, and the quota that had no room.
func (c *Counters) admit(ctx context.Context, e Entry, quotas []Quota) (n int64, ok bool, full *Quota, err error) {
	for _, q := range quotas {
		if q.Max <= 0 {
			return 0, false, &q, nil
		}
	}
	a := admissionOf(e)
	stripe := rand.IntN(Stripes)
	scopes := quotaScopes(quotas)

	// One stripe, held in each scope with a quota.
	err = pgx.BeginFunc(ctx, c.pool, func(tx pgx.Tx) error {
		for _, k := range scopes {
			if err := lockStripes(ctx, tx, e.Tenant, k, stripe, stripe+1); err != nil {
				return err
			}
		}
		on := map[[2]string]int{}
		for _, q := range quotas {
			by, err := counted(ctx, tx, e.Tenant, q)
			if err != nil {
				return err
			}
			// Every other stripe at most at its share, or its count when
			// that is more, and this one one more.
			total := by[stripe] + 1
			for i := range Stripes {
				if i != stripe {
					total += max(by[i], share(q.Max, i))
				}
			}
			if total > int64(q.Max) {
				return errNoRoom
			}
			on[[2]string{dbScope(scopeKey(q.Scope)), string(q.Kind)}] = stripe
		}
		if n, err = c.count(ctx, tx, e, a, stripe, on); err != nil {
			return err
		}
		ok = true
		return nil
	})
	if err == nil {
		return n, ok, nil, nil
	}
	if !errors.Is(err, errNoRoom) {
		return 0, false, nil, fmt.Errorf("usage: counters: %w", err)
	}

	// Every stripe, held in each scope with a quota: the totals are exact.
	err = pgx.BeginFunc(ctx, c.pool, func(tx pgx.Tx) error {
		for _, k := range scopes {
			if err := lockStripes(ctx, tx, e.Tenant, k, 0, Stripes); err != nil {
				return err
			}
		}
		on := map[[2]string]int{}
		for _, q := range quotas {
			by, err := counted(ctx, tx, e.Tenant, q)
			if err != nil {
				return err
			}
			var total int64
			best := 0
			for i := range Stripes {
				total += by[i]
				if share(q.Max, i)-by[i] > share(q.Max, best)-by[best] {
					best = i
				}
			}
			if total+1 > int64(q.Max) {
				full = &q
				return nil
			}
			on[[2]string{dbScope(scopeKey(q.Scope)), string(q.Kind)}] = best
		}
		if n, err = c.count(ctx, tx, e, a, stripe, on); err != nil {
			return err
		}
		ok = true
		return nil
	})
	if err != nil {
		return 0, false, nil, fmt.Errorf("usage: counters: %w", err)
	}
	return n, ok, full, nil
}

// lockStripes locks stripes [from, to) of a scope, making them first if
// they are not there.
func lockStripes(ctx context.Context, tx pgx.Tx, tenant, scope string, from, to int) error {
	if _, err := tx.Exec(ctx, `INSERT INTO humagen_usage_stripes (tenant, scope, stripe) SELECT $1, $2, s FROM generate_series($3::int, $4::int - 1) AS s
		ON CONFLICT DO NOTHING`, tenant, scope, from, to); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `SELECT 1 FROM humagen_usage_stripes WHERE tenant = $1 AND scope = $2 AND stripe >= $3 AND stripe < $4 ORDER BY stripe FOR UPDATE`,
		tenant, scope, from, to)
	return err
}

// count counts an admission: in flight, and started today, in every scope it
// falls in, on the stripe on names for the scope and kind, or stripe; and
// writes its entry.
func (c *Counters) count(ctx context.Context, tx pgx.Tx, e Entry, a admission, stripe int, on map[[2]string]int) (int64, error) {
	within := e.Within
	if within == nil {
		within = []string{}
	}
	b := &pgx.Batch{}
	b.Queue(`INSERT INTO humagen_usage_active (tenant, run_id, project, within, agent, client) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (tenant, run_id) DO NOTHING`, e.Tenant, e.RunID, e.Project, within, e.Agent, e.Client)
	day := policy.PeriodStart(policy.Day, e.At).Unix()
	for _, k := range a.scopes() {
		runs, ok := on[[2]string{dbScope(k), string(QuotaRuns)}]
		if !ok {
			runs = stripe
		}
		slot, ok := on[[2]string{dbScope(k), string(QuotaConcurrent)}]
		if !ok {
			slot = stripe
		}
		b.Queue(`INSERT INTO humagen_usage_admits (tenant, scope, day, stripe, runs) VALUES ($1, $2, $3, $4, 1)
			ON CONFLICT (tenant, scope, day, stripe) DO UPDATE SET runs = humagen_usage_admits.runs + 1`, e.Tenant, dbScope(k), day, runs)
		b.Queue(`INSERT INTO humagen_usage_slots (tenant, scope, run_id, stripe) VALUES ($1, $2, $3, $4)
			ON CONFLICT (tenant, scope, run_id) DO NOTHING`, e.Tenant, dbScope(k), e.RunID, slot)
	}
	b.Queue(`SELECT nextval('humagen_usage_seq')`)
	br := tx.SendBatch(ctx, b)
	for range b.Len() - 1 {
		if _, err := br.Exec(); err != nil {
			_ = br.Close()
			return 0, err
		}
	}
	var n int64
	if err := br.QueryRow().Scan(&n); err != nil {
		_ = br.Close()
		return 0, err
	}
	if err := br.Close(); err != nil {
		return 0, err
	}
	return n, keepEntry(ctx, tx, e, nil, n)
}

// spendRun folds a Spent or Settled entry into its run's row and returns what
// it adds to the counts, as apply does in memory.
func spendRun(ctx context.Context, tx pgx.Tx, e Entry) (Spend, error) {
	var r runRec
	var live, settled []byte
	err := tx.QueryRow(ctx, `SELECT live, settled, last FROM humagen_usage_runs WHERE tenant = $1 AND run_id = $2`, e.Tenant, e.RunID).Scan(&live, &settled, &r.Last)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return Spend{}, err
	default:
		if err := json.Unmarshal(live, &r.Live); err != nil {
			return Spend{}, err
		}
		if err := json.Unmarshal(settled, &r.Settled); err != nil {
			return Spend{}, err
		}
	}
	before := r.counted()
	if e.Kind == Spent {
		r.Live = r.Live.add(*e.Spend)
	} else {
		r.Settled = *e.Spend
	}
	if e.At.After(r.Last) {
		r.Last = e.At
	}
	added := r.counted().beyond(before)
	if live, err = json.Marshal(r.Live); err != nil {
		return Spend{}, err
	}
	if settled, err = json.Marshal(r.Settled); err != nil {
		return Spend{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO humagen_usage_runs (tenant, run_id, live, settled, last) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (tenant, run_id) DO UPDATE SET live = EXCLUDED.live, settled = EXCLUDED.settled, last = EXCLUDED.last`,
		e.Tenant, e.RunID, live, settled, r.Last)
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

// prune drops a tenant's counts no period can still need: those of days
// before cutoff. It is housekeeping, which the ledger runs once a day per
// tenant (Ledger.retain).
func (c *Counters) prune(ctx context.Context, tenant string, cutoff time.Time) error {
	return pgx.BeginFunc(ctx, c.pool, func(tx pgx.Tx) error {
		// The tenant's lock keeps this from deleting a run's spend row
		// while a call reads and rewrites it.
		if err := lockTenant(ctx, tx, tenant); err != nil {
			return err
		}
		b := &pgx.Batch{}
		b.Queue(`DELETE FROM humagen_usage_days WHERE tenant = $1 AND day < $2`, tenant, cutoff.Unix())
		b.Queue(`DELETE FROM humagen_usage_admits WHERE tenant = $1 AND day < $2`, tenant, cutoff.Unix())
		b.Queue(`DELETE FROM humagen_usage_keys WHERE tenant = $1 AND at < $2`, tenant, cutoff)
		b.Queue(`DELETE FROM humagen_usage_runs WHERE tenant = $1 AND last < $2`, tenant, cutoff)
		return tx.SendBatch(ctx, b).Close()
	})
}

// seed moves the sequence past every place an earlier release gave an
// entry and, the first time the counters are opened on a database, fills
// them from counts a ledger without counters kept in from (see keys.go): the
// entries of that release, recorded into a ledger in memory.
func (c *Counters) seed(ctx context.Context, from state.Store, last int64) error {
	return pgx.BeginFunc(ctx, c.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1, 1)`, int32(countersLock)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT setval('humagen_usage_seq', GREATEST((SELECT last_value FROM humagen_usage_seq), $1, 1))`, last); err != nil {
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
		if from != nil {
			recs, err := from.Load(ctx, namespace)
			if err != nil {
				return err
			}
			for _, r := range recs {
				if err := seedRecord(ctx, tx, r); err != nil {
					return fmt.Errorf("%s/%s: %w", r.Tenant, r.Key, err)
				}
			}
		}
		_, err = tx.Exec(ctx, `INSERT INTO humagen_usage_meta (id, built) VALUES (1, true) ON CONFLICT (id) DO UPDATE SET built = true`)
		return err
	})
}

// seedRecord fills the counters from one count a ledger without them kept.
func seedRecord(ctx context.Context, tx pgx.Tx, r state.Record) error {
	name := r.Tenant
	switch {
	case strings.HasPrefix(r.Key, "d:"):
		var d dayCount
		if err := json.Unmarshal(r.Value, &d); err != nil {
			return err
		}
		rest := strings.TrimPrefix(r.Key, "d:")
		i := strings.LastIndexByte(rest, ':')
		if i < 0 {
			return nil
		}
		scope, day := rest[:i], rest[i+1:]
		at, err := time.Parse("20060102", day)
		if err != nil {
			return nil
		}
		if _, err := tx.Exec(ctx, `INSERT INTO humagen_usage_days (tenant, scope, day, runs) VALUES ($1, $2, $3, $4)
			ON CONFLICT (tenant, scope, day) DO UPDATE SET runs = humagen_usage_days.runs + EXCLUDED.runs`, name, scope, at.Unix(), d.Runs); err != nil {
			return err
		}
		// addDay takes a scope key as the ledger writes it.
		return addDay(ctx, tx, name, []string{strings.Replace(scope, ":", "\x00", 1)}, at.Unix(), d.Spend)
	case strings.HasPrefix(r.Key, "a:"):
		var a activeRun
		if err := json.Unmarshal(r.Value, &a); err != nil {
			return err
		}
		id := strings.TrimPrefix(r.Key, "a:")
		within := a.Within
		if within == nil {
			within = []string{}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO humagen_usage_active (tenant, run_id, project, within, agent, client) VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (tenant, run_id) DO NOTHING`, name, id, a.Project, within, a.Agent, a.Client); err != nil {
			return err
		}
		ad := admission{project: a.Project, within: a.Within, agent: a.Agent, client: a.Client}
		return addSlots(ctx, tx, name, id, ad.scopes(), nil)
	case strings.HasPrefix(r.Key, "r:"):
		var run runRec
		if err := json.Unmarshal(r.Value, &run); err != nil {
			return err
		}
		live, err := json.Marshal(run.Live)
		if err != nil {
			return err
		}
		settled, err := json.Marshal(run.Settled)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO humagen_usage_runs (tenant, run_id, live, settled, last) VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (tenant, run_id) DO NOTHING`, name, strings.TrimPrefix(r.Key, "r:"), live, settled, run.Last)
		return err
	case strings.HasPrefix(r.Key, "k:"):
		var k keyRec
		if err := json.Unmarshal(r.Value, &k); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO humagen_usage_keys (tenant, key, at) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, name, strings.TrimPrefix(r.Key, "k:"), k.At)
		return err
	}
	return nil
}
