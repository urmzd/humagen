package usage

import (
	"context"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/state/statetest"
)

// A process that opens the counters while another is part way through
// counting a call neither waits for that call nor deadlocks with it. The
// call has written its key and goes on to write its run; an opener that
// took a lock on the runs table and then waited for one on the keys table
// would hold what the call needs next while waiting for what the call
// holds.
func TestOpeningTheCountersTakesNoLockACallBeingCountedNeeds(t *testing.T) {
	pools := statetest.Pools(t, 2)
	ctx := t.Context()
	if _, err := OpenCounters(ctx, pools[0]); err != nil {
		t.Fatal(err)
	}
	tx, err := pools[0].Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(ctx, `INSERT INTO humagen_usage_keys (tenant, key, at) VALUES ('acme', 'c1', now())`); err != nil {
		t.Fatal(err)
	}
	opened := make(chan error, 1)
	go func() {
		_, err := OpenCounters(context.Background(), pools[1])
		opened <- err
	}()
	// Postgres finds a deadlock after deadlock_timeout, a second by
	// default; an opener that waits is given longer than that to return.
	select {
	case err := <-opened:
		if err != nil {
			t.Fatalf("opening the counters beside a call being counted: %v", err)
		}
	case <-time.After(5 * time.Second):
		// The call goes on to its run, as record does, to show what the
		// wait turns into.
		_, werr := tx.Exec(ctx, `INSERT INTO humagen_usage_runs (tenant, run_id, live, settled, last) VALUES ('acme', 'r1', '{}', '{}', now())`)
		_ = tx.Rollback(context.Background())
		t.Fatalf("opening the counters waited for a call being counted; the call then got %v, the opener %v", werr, <-opened)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO humagen_usage_runs (tenant, run_id, live, settled, last) VALUES ('acme', 'r1', '{}', '{}', now())`); err != nil {
		t.Fatalf("the call's run: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	// Indexes that are missing, as on counters an earlier release made, are
	// created one transaction each: the opener waits for the call holding
	// no lock the call needs, so the call ends and the opener after it.
	for _, name := range []string{"humagen_usage_runs_last", "humagen_usage_keys_at"} {
		if _, err := pools[0].Exec(ctx, `DROP INDEX `+name); err != nil {
			t.Fatal(err)
		}
	}
	tx, err = pools[0].Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(ctx, `INSERT INTO humagen_usage_keys (tenant, key, at) VALUES ('acme', 'c2', now())`); err != nil {
		t.Fatal(err)
	}
	go func() {
		_, err := OpenCounters(context.Background(), pools[1])
		opened <- err
	}()
	if _, err := tx.Exec(ctx, `INSERT INTO humagen_usage_runs (tenant, run_id, live, settled, last) VALUES ('acme', 'r2', '{}', '{}', now())`); err != nil {
		t.Fatalf("the call's run, beside an opener creating indexes: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-opened; err != nil {
		t.Fatalf("opening counters whose indexes are missing: %v", err)
	}
	var made int
	if err := pools[0].QueryRow(ctx, `SELECT count(*) FROM pg_indexes WHERE schemaname = current_schema() AND indexname IN ('humagen_usage_runs_last', 'humagen_usage_keys_at', 'humagen_usage_slots_run')`).Scan(&made); err != nil || made != 3 {
		t.Fatalf("the counters have %d of their 3 indexes, %v", made, err)
	}
}
