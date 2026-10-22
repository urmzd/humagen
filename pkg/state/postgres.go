package state

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// schema is the one table every namespace shares. Values are bytes, not
// jsonb: a store writes JSON, but jsonb refuses a NUL in a string, and an
// event's text is whatever a model or a person wrote.
//
// The names are COLLATE "C" so that Load orders keys as bytes, as Memory does,
// and a store that pads a sequence number into its keys reads them back in
// the order it wrote them.
const schema = `
CREATE TABLE IF NOT EXISTS warp_state (
	namespace  text        COLLATE "C" NOT NULL,
	tenant     text        COLLATE "C" NOT NULL,
	key        text        COLLATE "C" NOT NULL,
	value      bytea       NOT NULL,
	updated_at timestamptz NOT NULL DEFAULT now(),
	PRIMARY KEY (namespace, tenant, key)
);
`

// lockClass is the first half of the advisory locks this package takes. The
// second half is the schema's hash, so two schemas in one database are two
// deployments and do not block each other.
// It is the value every release has used, so an older process and this one
// on one schema still exclude each other during an upgrade.
const lockClass = 0x77617270

// Postgres is a Store in a Postgres database. It does not own the pool: the
// caller opened it and closes it, after Close.
type Postgres struct {
	pool *pgxpool.Pool

	mu   sync.Mutex
	lock *pgxpool.Conn
}

var _ Store = (*Postgres)(nil)

// OpenPostgres creates the table if it is not there and takes the database
// for this process. A second process on the same schema gets ErrLocked until
// the first one closes or its connection ends.
func OpenPostgres(ctx context.Context, pool *pgxpool.Pool) (*Postgres, error) {
	if pool == nil {
		return nil, fmt.Errorf("%w: no pool", ErrInvalid)
	}
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1, 0)`, int32(lockClass)); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, schema)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("state: migrate: %w", err)
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("state: lock: %w", err)
	}
	var held bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1, hashtext(current_schema()))`, int32(lockClass)).Scan(&held); err != nil {
		conn.Release()
		return nil, fmt.Errorf("state: lock: %w", err)
	}
	if !held {
		conn.Release()
		return nil, ErrLocked
	}
	return &Postgres{pool: pool, lock: conn}, nil
}

// Close lets another process have the database. The store must not be used
// after it.
func (p *Postgres) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.lock == nil {
		return nil
	}
	_, err := p.lock.Exec(context.Background(), `SELECT pg_advisory_unlock($1, hashtext(current_schema()))`, int32(lockClass))
	p.lock.Release()
	p.lock = nil
	return err
}

// Get implements Store.
func (p *Postgres) Get(ctx context.Context, namespace, tenant, key string) ([]byte, error) {
	var v []byte
	err := p.pool.QueryRow(ctx, `SELECT value FROM warp_state WHERE namespace = $1 AND tenant = $2 AND key = $3`, namespace, tenant, key).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("state: get: %w", err)
	}
	return v, nil
}

// Load implements Store.
func (p *Postgres) Load(ctx context.Context, namespace string) ([]Record, error) {
	rows, err := p.pool.Query(ctx, `SELECT tenant, key, value FROM warp_state WHERE namespace = $1 ORDER BY tenant, key`, namespace)
	if err != nil {
		return nil, fmt.Errorf("state: load: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Record, error) {
		var r Record
		err := row.Scan(&r.Tenant, &r.Key, &r.Value)
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
		b := &pgx.Batch{}
		for _, op := range ops {
			if op.Value == nil {
				b.Queue(`DELETE FROM warp_state WHERE namespace = $1 AND tenant = $2 AND key = $3`, namespace, op.Tenant, op.Key)
				continue
			}
			b.Queue(`INSERT INTO warp_state (namespace, tenant, key, value) VALUES ($1, $2, $3, $4)
				ON CONFLICT (namespace, tenant, key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`,
				namespace, op.Tenant, op.Key, op.Value)
		}
		return tx.SendBatch(ctx, b).Close()
	})
	if err != nil {
		return fmt.Errorf("state: write: %w", err)
	}
	return nil
}
