// Package statetest holds every state.Store to one contract, and gives the
// stores built on one a way to run their own tests on every backend.
//
// The Postgres backend runs when HUMAGEN_POSTGRES_TEST_URL names a database.
// Each test gets its own schema, dropped when it ends.
package statetest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/urmzd/humagen/pkg/state"

	"github.com/urmzd/humagen/internal/legacy"
)

// URLEnv names the database the Postgres backend is tested against.
const URLEnv = "HUMAGEN_POSTGRES_TEST_URL"

// Backend opens a fresh, empty store.
type Backend struct {
	Name string
	Open func(t *testing.T) state.Store
}

// Backends are Memory always, and Postgres when URLEnv is set.
func Backends() []Backend {
	return []Backend{
		{Name: "memory", Open: func(*testing.T) state.Store { return state.NewMemory() }},
		{Name: "postgres", Open: func(t *testing.T) state.Store {
			t.Helper()
			st, _ := OpenPostgres(t)
			return st
		}},
	}
}

// Each runs fn once per backend, as a subtest named for it.
func Each(t *testing.T, fn func(t *testing.T, open func(t *testing.T) state.Store)) {
	t.Helper()
	for _, b := range Backends() {
		t.Run(b.Name, func(t *testing.T) { fn(t, b.Open) })
	}
}

var schemas atomic.Uint64

// Pool returns a pool on a new schema of the test database, dropped at the
// end of the test, or skips the test when URLEnv is unset.
func Pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := legacy.Getenv(URLEnv) // or its old name, WARP_POSTGRES_TEST_URL
	if url == "" {
		t.Skip("set " + URLEnv + " to run against Postgres")
	}
	ctx := context.Background()
	schema := fmt.Sprintf("humagentest_%d_%d", time.Now().UnixNano(), schemas.Add(1))
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = 8
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	ident := pgx.Identifier{schema}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE SCHEMA "+ident); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DROP SCHEMA "+ident+" CASCADE")
		pool.Close()
	})
	return pool
}

// OpenPostgres returns a Postgres store on a new schema, and its pool.
func OpenPostgres(t *testing.T) (*state.Postgres, *pgxpool.Pool) {
	t.Helper()
	pool := Pool(t)
	st, err := state.OpenPostgres(context.Background(), pool)
	if err != nil {
		t.Fatal(err)
	}
	// Registered after the pool's cleanup, so it runs first.
	t.Cleanup(func() { _ = st.Close() })
	return st, pool
}

// Faulty wraps a store so a test can make its writes fail.
type Faulty struct {
	state.Store
	mu  sync.Mutex
	err error
}

// NewFaulty wraps st.
func NewFaulty(st state.Store) *Faulty { return &Faulty{Store: st} }

// Fail makes every write fail with err until it is called with nil.
func (f *Faulty) Fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

// Write implements state.Store.
func (f *Faulty) Write(ctx context.Context, namespace string, ops ...state.Op) error {
	f.mu.Lock()
	err := f.err
	f.mu.Unlock()
	if err != nil {
		return err
	}
	return f.Store.Write(ctx, namespace, ops...)
}

// ErrInjected is the error a test makes a Faulty store fail with.
var ErrInjected = errors.New("statetest: injected write failure")

// Run holds a store to the contract. open must return an empty store.
func Run(t *testing.T, open func(t *testing.T) state.Store) {
	ctx := context.Background()

	t.Run("get missing", func(t *testing.T) {
		st := open(t)
		if _, err := st.Get(ctx, "ns", "t", "k"); !errors.Is(err, state.ErrNotFound) {
			t.Fatalf("Get = %v, want ErrNotFound", err)
		}
	})

	t.Run("put get replace delete", func(t *testing.T) {
		st := open(t)
		if err := st.Write(ctx, "ns", state.Put("t", "k", []byte(`{"a":1}`))); err != nil {
			t.Fatal(err)
		}
		if v, err := st.Get(ctx, "ns", "t", "k"); err != nil || string(v) != `{"a":1}` {
			t.Fatalf("Get = %q, %v", v, err)
		}
		if err := st.Write(ctx, "ns", state.Put("t", "k", []byte(`{"a":2}`))); err != nil {
			t.Fatal(err)
		}
		if v, _ := st.Get(ctx, "ns", "t", "k"); string(v) != `{"a":2}` {
			t.Fatalf("after replace = %q", v)
		}
		if err := st.Write(ctx, "ns", state.Delete("t", "k")); err != nil {
			t.Fatal(err)
		}
		if _, err := st.Get(ctx, "ns", "t", "k"); !errors.Is(err, state.ErrNotFound) {
			t.Fatalf("after delete = %v", err)
		}
		if err := st.Write(ctx, "ns", state.Delete("t", "missing")); err != nil {
			t.Fatalf("deleting a missing key = %v", err)
		}
	})

	t.Run("namespaces and tenants are apart", func(t *testing.T) {
		st := open(t)
		must(t, st.Write(ctx, "a", state.Put("t1", "k", []byte("1"))))
		must(t, st.Write(ctx, "a", state.Put("t2", "k", []byte("2"))))
		must(t, st.Write(ctx, "b", state.Put("t1", "k", []byte("3"))))
		for _, c := range []struct{ ns, tenant, want string }{{"a", "t1", "1"}, {"a", "t2", "2"}, {"b", "t1", "3"}} {
			if v, err := st.Get(ctx, c.ns, c.tenant, "k"); err != nil || string(v) != c.want {
				t.Fatalf("%s/%s = %q, %v, want %q", c.ns, c.tenant, v, err, c.want)
			}
		}
		recs, err := st.Load(ctx, "a")
		if err != nil || len(recs) != 2 {
			t.Fatalf("Load(a) = %v, %v", recs, err)
		}
	})

	t.Run("load orders by tenant then key as bytes", func(t *testing.T) {
		st := open(t)
		keys := []string{state.Seq(10), state.Seq(2), "B", "a", "Z", "_"}
		for _, k := range keys {
			must(t, st.Write(ctx, "ns", state.Put("t2", k, []byte(k))))
			must(t, st.Write(ctx, "ns", state.Put("t1", k, []byte(k))))
		}
		recs, err := st.Load(ctx, "ns")
		if err != nil {
			t.Fatal(err)
		}
		want := []string{state.Seq(2), state.Seq(10), "B", "Z", "_", "a"}
		if len(recs) != 2*len(want) {
			t.Fatalf("Load = %d records", len(recs))
		}
		for i, r := range recs {
			tenant := "t1"
			if i >= len(want) {
				tenant = "t2"
			}
			if r.Tenant != tenant || r.Key != want[i%len(want)] || string(r.Value) != r.Key {
				t.Fatalf("record %d = %s/%s %q, want %s/%s", i, r.Tenant, r.Key, r.Value, tenant, want[i%len(want)])
			}
		}
	})

	t.Run("write is all or none", func(t *testing.T) {
		st := open(t)
		must(t, st.Write(ctx, "ns", state.Put("t", "keep", []byte("1"))))
		err := st.Write(ctx, "ns", state.Put("t", "new", []byte("2")), state.Delete("t", "keep"), state.Op{Tenant: "t"})
		if err == nil {
			t.Fatal("a write with an empty key was accepted")
		}
		if _, err := st.Get(ctx, "ns", "t", "new"); !errors.Is(err, state.ErrNotFound) {
			t.Fatalf("a refused write left %q behind: %v", "new", err)
		}
		if _, err := st.Get(ctx, "ns", "t", "keep"); err != nil {
			t.Fatalf("a refused write deleted %q: %v", "keep", err)
		}
		if err := st.Write(ctx, "ns", state.Put("t", "a\x00b", []byte("1"))); !errors.Is(err, state.ErrInvalid) {
			t.Fatalf("a key with a NUL = %v, want ErrInvalid", err)
		}
		// Ops apply in order: a put then a delete of one key leaves nothing.
		must(t, st.Write(ctx, "ns", state.Put("t", "x", []byte("1")), state.Delete("t", "x")))
		if _, err := st.Get(ctx, "ns", "t", "x"); !errors.Is(err, state.ErrNotFound) {
			t.Fatalf("put then delete = %v", err)
		}
	})

	t.Run("values are kept byte for byte", func(t *testing.T) {
		st := open(t)
		raw := []byte("{\"text\":\"nul \\u0000 and é\", \"n\":  1}\x00\xff")
		must(t, st.Write(ctx, "ns", state.Put("", "k", raw)))
		v, err := st.Get(ctx, "ns", "", "k")
		if err != nil || !bytes.Equal(v, raw) {
			t.Fatalf("Get = %q, %v", v, err)
		}
		must(t, st.Write(ctx, "ns", state.Put("", "empty", nil)))
		if v, err := st.Get(ctx, "ns", "", "empty"); err != nil || len(v) != 0 {
			t.Fatalf("empty value = %q, %v", v, err)
		}
		// The caller's slice is not retained.
		raw[0] = 'X'
		if v, _ := st.Get(ctx, "ns", "", "k"); v[0] != '{' {
			t.Fatal("the store kept the caller's slice")
		}
	})

	t.Run("concurrent writers", func(t *testing.T) {
		st := open(t)
		var wg sync.WaitGroup
		for i := range 16 {
			wg.Go(func() {
				for j := range 10 {
					if err := st.Write(ctx, "ns", state.Put("t", fmt.Sprintf("%02d-%02d", i, j), []byte("v"))); err != nil {
						t.Error(err)
					}
				}
			})
		}
		wg.Wait()
		recs, err := st.Load(ctx, "ns")
		if err != nil || len(recs) != 160 {
			t.Fatalf("Load = %d, %v", len(recs), err)
		}
	})
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
