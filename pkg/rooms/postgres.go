package rooms

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresCounters keeps the gate's counts in a Postgres database every
// process shares, so a room's rate and a person's runs in flight hold across
// replicas. Each change is one transaction under an advisory lock on its key.
type PostgresCounters struct {
	pool *pgxpool.Pool
}

// roomsLock is the first half of the advisory lock a key's counts are
// changed under; the second is a hash of the schema and the key.
const roomsLock = 0x726f6f6d

// opTimeout bounds one change to the counts.
const opTimeout = 5 * time.Second

// sweepRows bounds the rate rows of other keys dropped by one Take, once
// they are an hour old, so the table holds no more than the live windows.
const sweepRows = 100

const roomsSchema = `
CREATE TABLE IF NOT EXISTS humagen_room_rate (
	key text        COLLATE "C" NOT NULL,
	at  timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS humagen_room_rate_key ON humagen_room_rate (key, at);
CREATE INDEX IF NOT EXISTS humagen_room_rate_at ON humagen_room_rate (at);
CREATE TABLE IF NOT EXISTS humagen_room_holds (
	key    text COLLATE "C" NOT NULL,
	holder text COLLATE "C" NOT NULL,
	PRIMARY KEY (key, holder)
);
`

// OpenPostgres creates the counters' tables if they are not there. The
// caller owns the pool.
func OpenPostgres(ctx context.Context, pool *pgxpool.Pool) (*PostgresCounters, error) {
	if pool == nil {
		return nil, errors.New("rooms: counters: no pool")
	}
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1, 0)`, int32(roomsLock)); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, roomsSchema)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("rooms: counters: migrate: %w", err)
	}
	return &PostgresCounters{pool: pool}, nil
}

// dbKey is a key as text can hold it: no NUL.
func dbKey(k string) string {
	b := []byte(k)
	for i := range b {
		if b[i] == 0 {
			b[i] = ':'
		}
	}
	return string(b)
}

func (c *PostgresCounters) locked(key string, fn func(ctx context.Context, tx pgx.Tx) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	return pgx.BeginFunc(ctx, c.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1, hashtext(current_schema() || ':' || $2))`, int32(roomsLock), key); err != nil {
			return err
		}
		return fn(ctx, tx)
	})
}

// Take implements Counters.
func (c *PostgresCounters) Take(key string, now time.Time, window time.Duration, max int) (ok bool, retry time.Duration, err error) {
	key = dbKey(key)
	err = c.locked(key, func(ctx context.Context, tx pgx.Tx) error {
		ok, retry = false, 0
		start := now.Add(-window)
		if _, err := tx.Exec(ctx, `DELETE FROM humagen_room_rate WHERE key = $1 AND at <= $2`, key, start); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM humagen_room_rate WHERE ctid IN (SELECT ctid FROM humagen_room_rate WHERE at <= $1 LIMIT $2)`, now.Add(-time.Hour), sweepRows); err != nil {
			return err
		}
		var n int
		var first *time.Time
		if err := tx.QueryRow(ctx, `SELECT count(*), min(at) FROM humagen_room_rate WHERE key = $1`, key).Scan(&n, &first); err != nil {
			return err
		}
		if n >= max {
			if first != nil {
				retry = first.Add(window).Sub(now)
			}
			return nil
		}
		ok = true
		_, err := tx.Exec(ctx, `INSERT INTO humagen_room_rate (key, at) VALUES ($1, $2)`, key, now)
		return err
	})
	return ok, retry, err
}

// Hold implements Counters.
func (c *PostgresCounters) Hold(key, holder string, max int, gone func(string) bool) (ok bool, err error) {
	key = dbKey(key)
	err = c.locked(key, func(ctx context.Context, tx pgx.Tx) error {
		ok = false
		var held bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM humagen_room_holds WHERE key = $1 AND holder = $2)`, key, holder).Scan(&held); err != nil {
			return err
		}
		if held {
			ok = true
			return nil
		}
		// At most max holders are read: a key never holds more.
		rows, err := tx.Query(ctx, `SELECT holder FROM humagen_room_holds WHERE key = $1 LIMIT $2`, key, max+1)
		if err != nil {
			return err
		}
		holders, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		n := len(holders)
		if n >= max && gone != nil {
			for _, h := range holders {
				if !gone(h) {
					continue
				}
				if _, err := tx.Exec(ctx, `DELETE FROM humagen_room_holds WHERE key = $1 AND holder = $2`, key, h); err != nil {
					return err
				}
				n--
			}
		}
		if n >= max {
			return nil
		}
		ok = true
		_, err = tx.Exec(ctx, `INSERT INTO humagen_room_holds (key, holder) VALUES ($1, $2)`, key, holder)
		return err
	})
	return ok, err
}

// Free implements Counters.
func (c *PostgresCounters) Free(key, holder string) error {
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	_, err := c.pool.Exec(ctx, `DELETE FROM humagen_room_holds WHERE key = $1 AND holder = $2`, dbKey(key), holder)
	return err
}
