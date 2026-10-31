package state

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// table holds every namespace's records. Its name is the one every release
// has used, so an older binary on the same schema reads the same rows.
const table = "warp_state"

// schema is the one table every namespace shares. Values are bytes, not
// jsonb: a store writes JSON, but jsonb refuses a NUL in a string, and an
// event's text is whatever a model or a person wrote.
//
// The names are COLLATE "C" so that Load orders keys as bytes, as Memory does,
// and a store that pads a sequence number into its keys reads them back in
// the order it wrote them. The primary key is also the index a List pages:
// one namespace, one tenant, a range of keys.
//
// version was added after the table: a row an older release wrote is at
// version 1. An older release does not move it, so an older release must not
// run beside this one: stop it before starting this one.
const schema = `
CREATE TABLE IF NOT EXISTS ` + table + ` (
	namespace  text        COLLATE "C" NOT NULL,
	tenant     text        COLLATE "C" NOT NULL,
	key        text        COLLATE "C" NOT NULL,
	value      bytea       NOT NULL,
	updated_at timestamptz NOT NULL DEFAULT now(),
	PRIMARY KEY (namespace, tenant, key)
);
ALTER TABLE ` + table + ` ADD COLUMN IF NOT EXISTS version bigint NOT NULL DEFAULT 1;
`

// lockClass is the first half of the advisory lock a migration takes, so two
// processes starting at once create the table once. It is the value every
// release has used, for the lock older releases also held for as long as
// they ran.
const lockClass = 0x77617270

// Postgres is a Store in a Postgres database. It does not own the pool: the
// caller opened it and closes it.
type Postgres struct {
	pool *pgxpool.Pool
}

var _ Store = (*Postgres)(nil)

// OpenPostgres creates the table if it is not there. It takes no lock: any
// number of processes may open one database, since every store reads and
// writes it per request (see the package doc).
func OpenPostgres(ctx context.Context, pool *pgxpool.Pool) (*Postgres, error) {
	if pool == nil {
		return nil, fmt.Errorf("%w: no pool", ErrInvalid)
	}
	if err := migrate(ctx, pool); err != nil {
		return nil, err
	}
	return &Postgres{pool: pool}, nil
}

// OpenPostgresShared is OpenPostgres. It was the open that took no lock while
// OpenPostgres took one.
func OpenPostgresShared(ctx context.Context, pool *pgxpool.Pool) (*Postgres, error) {
	return OpenPostgres(ctx, pool)
}

func migrate(ctx context.Context, pool *pgxpool.Pool) error {
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1, 0)`, int32(lockClass)); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, schema)
		return err
	})
	if err != nil {
		return fmt.Errorf("state: migrate: %w", err)
	}
	return nil
}

// Close does nothing; the caller closes the pool. It is kept for callers
// written when the store held a lock.
func (p *Postgres) Close() error { return nil }

// Get implements Store.
func (p *Postgres) Get(ctx context.Context, namespace, tenant, key string) ([]byte, error) {
	r, err := p.Lookup(ctx, namespace, tenant, key)
	return r.Value, err
}

// Lookup implements Store.
func (p *Postgres) Lookup(ctx context.Context, namespace, tenant, key string) (Record, error) {
	r := Record{Tenant: tenant, Key: key}
	err := p.pool.QueryRow(ctx, `SELECT value, version FROM `+table+` WHERE namespace = $1 AND tenant = $2 AND key = $3`, namespace, tenant, key).Scan(&r.Value, &r.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, fmt.Errorf("state: get: %w", err)
	}
	return r, nil
}

// List implements Store.
func (p *Postgres) List(ctx context.Context, namespace, tenant string, q Query) ([]Record, string, error) {
	limit, err := checkQuery(namespace, q)
	if err != nil {
		return nil, "", err
	}
	sql := `SELECT key, value, version FROM ` + table + ` WHERE namespace = $1 AND tenant = $2`
	args := []any{namespace, tenant}
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if q.Prefix != "" {
		sql += ` AND key >= ` + arg(q.Prefix)
		if end, ok := prefixEnd(q.Prefix); ok {
			sql += ` AND key < ` + arg(end)
		}
	}
	order := ` ORDER BY key`
	if q.After != "" {
		if q.Desc {
			sql += ` AND key < ` + arg(q.After)
		} else {
			sql += ` AND key > ` + arg(q.After)
		}
	}
	if q.Desc {
		order += ` DESC`
	}
	sql += order + ` LIMIT ` + arg(limit+1)
	rows, err := p.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, "", fmt.Errorf("state: list: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Record, error) {
		r := Record{Tenant: tenant}
		err := row.Scan(&r.Key, &r.Value, &r.Version)
		return r, err
	})
	if err != nil {
		return nil, "", fmt.Errorf("state: list: %w", err)
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		next = out[limit-1].Key
	}
	return out, next, nil
}

// Load implements Store.
func (p *Postgres) Load(ctx context.Context, namespace string) ([]Record, error) {
	rows, err := p.pool.Query(ctx, `SELECT tenant, key, value, version FROM `+table+` WHERE namespace = $1 ORDER BY tenant, key`, namespace)
	if err != nil {
		return nil, fmt.Errorf("state: load: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Record, error) {
		var r Record
		err := row.Scan(&r.Tenant, &r.Key, &r.Value, &r.Version)
		return r, err
	})
	if err != nil {
		return nil, fmt.Errorf("state: load: %w", err)
	}
	return out, nil
}

// Write implements Store.
func (p *Postgres) Write(ctx context.Context, namespace string, ops ...Op) error {
	if err := checkNames(namespace, ops); err != nil {
		return err
	}
	if len(ops) == 0 {
		return nil
	}
	err := pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		return WriteTx(ctx, tx, namespace, ops...)
	})
	if errors.Is(err, ErrConflict) {
		return ErrConflict
	}
	if err != nil {
		return fmt.Errorf("state: write: %w", err)
	}
	return nil
}

// WriteTx applies ops in a transaction the caller holds, as Write does in one
// of its own. It lets another table's change commit with the records it
// depends on: the usage ledger's shared counters commit with the entry that
// moved them. A conditional op that fails returns ErrConflict, and the caller
// must roll the transaction back.
func WriteTx(ctx context.Context, tx pgx.Tx, namespace string, ops ...Op) error {
	if err := checkNames(namespace, ops); err != nil {
		return err
	}
	if len(ops) == 0 {
		return nil
	}
	b := &pgx.Batch{}
	for _, op := range ops {
		switch {
		case op.Value == nil && op.If == 0:
			b.Queue(`DELETE FROM `+table+` WHERE namespace = $1 AND tenant = $2 AND key = $3`, namespace, op.Tenant, op.Key)
		case op.Value == nil:
			b.Queue(`DELETE FROM `+table+` WHERE namespace = $1 AND tenant = $2 AND key = $3 AND version = $4`, namespace, op.Tenant, op.Key, op.If)
		case op.If == Missing:
			b.Queue(`INSERT INTO `+table+` (namespace, tenant, key, value) VALUES ($1, $2, $3, $4) ON CONFLICT (namespace, tenant, key) DO NOTHING`,
				namespace, op.Tenant, op.Key, op.Value)
		case op.If > 0:
			b.Queue(`UPDATE `+table+` SET value = $4, version = version + 1, updated_at = now()
				WHERE namespace = $1 AND tenant = $2 AND key = $3 AND version = $5`, namespace, op.Tenant, op.Key, op.Value, op.If)
		default:
			b.Queue(`INSERT INTO `+table+` (namespace, tenant, key, value) VALUES ($1, $2, $3, $4)
				ON CONFLICT (namespace, tenant, key) DO UPDATE SET value = EXCLUDED.value, version = `+table+`.version + 1, updated_at = now()`,
				namespace, op.Tenant, op.Key, op.Value)
		}
	}
	br := tx.SendBatch(ctx, b)
	for _, op := range ops {
		tag, err := br.Exec()
		if err != nil {
			_ = br.Close()
			return err
		}
		if op.If != 0 && tag.RowsAffected() == 0 {
			_ = br.Close()
			return ErrConflict
		}
	}
	return br.Close()
}
