package state_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// A write that loses its condition to another writer still holds the lock it
// waited for until it ends, and still runs the rest of its ops. It takes its
// locks in key order, as every write does, so a writer that names the same
// keys in another order is never left waiting on it in a circle: the write
// that may apply applies, and the one that lost reports a conflict.
func TestPostgresWritesLockKeysInOneOrder(t *testing.T) {
	pool := statetest.Pool(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	st, err := state.OpenPostgres(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Write(ctx, "ns", state.Put("t", "a", []byte("1")), state.Put("t", "next", []byte("1"))); err != nil {
		t.Fatal(err)
	}

	// The late writer has a pool of its own, named so that its wait can be
	// seen from outside.
	cfg := pool.Config()
	name := "late-" + cfg.ConnConfig.RuntimeParams["search_path"]
	cfg.ConnConfig.RuntimeParams["application_name"] = name
	latePool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(latePool.Close)
	late, err := state.OpenPostgres(ctx, latePool)
	if err != nil {
		t.Fatal(err)
	}

	// One writer holds "next", another holds "a".
	first, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Rollback(context.Background()) }()
	if err := state.WriteTx(ctx, first, "ns", state.PutAt("t", "next", []byte("2"), 1)); err != nil {
		t.Fatal(err)
	}
	second, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.Rollback(context.Background()) }()
	if err := state.WriteTx(ctx, second, "ns", state.PutAt("t", "a", []byte("2"), 1)); err != nil {
		t.Fatal(err)
	}

	// The late writer read both before either moved, and names "next" first.
	done := make(chan error, 1)
	go func() {
		done <- late.Write(ctx, "ns", state.PutAt("t", "next", []byte("3"), 1), state.PutAt("t", "a", []byte("3"), 1))
	}()
	for waiting := 0; waiting == 0; {
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE application_name = $1 AND wait_event_type = 'Lock'`, name).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting == 0 {
			time.Sleep(5 * time.Millisecond)
		}
	}

	// "next" moves on. The late writer has lost, and what it does next must
	// not stand in the way of the writer that holds "a" and now wants
	// "next". The pause lets the late writer's own deadlock check pass, so a
	// circle would be broken by failing the writer that may apply.
	if err := first.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var check int
	if err := pool.QueryRow(ctx, `SELECT setting::int FROM pg_settings WHERE name = 'deadlock_timeout'`).Scan(&check); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Duration(check) * time.Millisecond * 3 / 2)
	if err := state.WriteTx(ctx, second, "ns", state.PutAt("t", "next", []byte("4"), 2)); err != nil {
		t.Fatalf("the writer that may apply: %v", err)
	}
	if err := second.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, state.ErrConflict) {
		t.Fatalf("the late writer = %v, want a conflict", err)
	}
	for key, want := range map[string]string{"a": "2", "next": "4"} {
		if v, err := st.Get(ctx, "ns", "t", key); err != nil || string(v) != want {
			t.Errorf("%s = %s, %v; want %s", key, v, err, want)
		}
	}
}
