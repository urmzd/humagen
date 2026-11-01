package state_test

import (
	"context"
	"testing"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// A row an older release wrote to warp_state is read.
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
}

// A row written before records had versions is at version 1, and moves on
// from there.
func TestPostgresVersionsRowsFromBeforeVersions(t *testing.T) {
	pool := statetest.Pool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `CREATE TABLE warp_state (
		namespace  text        COLLATE "C" NOT NULL,
		tenant     text        COLLATE "C" NOT NULL,
		key        text        COLLATE "C" NOT NULL,
		value      bytea       NOT NULL,
		updated_at timestamptz NOT NULL DEFAULT now(),
		PRIMARY KEY (namespace, tenant, key));
		INSERT INTO warp_state (namespace, tenant, key, value) VALUES ('ns', 't', 'k', 'old')`); err != nil {
		t.Fatal(err)
	}
	st, err := state.OpenPostgres(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	r, err := st.Lookup(ctx, "ns", "t", "k")
	if err != nil || r.Version != 1 || string(r.Value) != "old" {
		t.Fatalf("Lookup = %+v, %v", r, err)
	}
	if err := st.Write(ctx, "ns", state.PutAt("t", "k", []byte("new"), 1)); err != nil {
		t.Fatal(err)
	}
	if r, _ := st.Lookup(ctx, "ns", "t", "k"); r.Version != 2 {
		t.Fatalf("version = %d", r.Version)
	}
}
