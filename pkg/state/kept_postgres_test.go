package state_test

import (
	"context"
	"errors"
	"testing"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// A row an older release wrote to warp_state is read, and a process of an
// older release holding the schema's lock keeps this one out.
func TestPostgresReadsWhatAnOlderReleaseWrote(t *testing.T) {
	pool := statetest.Pool(t)
	ctx := context.Background()
	st, err := state.OpenPostgres(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO warp_state (namespace, tenant, key, value) VALUES ('clients', 'acme', 'docs', '{"id":"docs"}')`); err != nil {
		t.Fatal(err)
	}
	if v, err := st.Get(ctx, "clients", "acme", "docs"); err != nil || string(v) != `{"id":"docs"}` {
		t.Fatalf("Get = %s, %v", v, err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	var held bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1, hashtext(current_schema()))`, int32(0x77617270)).Scan(&held); err != nil || !held {
		t.Fatalf("lock: %v, %v", held, err)
	}
	if _, err := state.OpenPostgres(ctx, pool); !errors.Is(err, state.ErrLocked) {
		t.Fatalf("opened beside an older release holding the lock: %v", err)
	}
}
