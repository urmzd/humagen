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

	t.Run("versions", func(t *testing.T) {
		st := open(t)
		must(t, st.Write(ctx, "ns", state.Put("t", "k", []byte("1"))))
		r, err := st.Lookup(ctx, "ns", "t", "k")
		if err != nil || r.Version != 1 || string(r.Value) != "1" || r.Tenant != "t" || r.Key != "k" {
			t.Fatalf("Lookup = %+v, %v", r, err)
		}
		must(t, st.Write(ctx, "ns", state.Put("t", "k", []byte("2"))))
		if r, _ := st.Lookup(ctx, "ns", "t", "k"); r.Version != 2 {
			t.Fatalf("after a second put, version = %d", r.Version)
		}
		recs, err := st.Load(ctx, "ns")
		if err != nil || len(recs) != 1 || recs[0].Version != 2 {
			t.Fatalf("Load = %+v, %v", recs, err)
		}
		if _, err := st.Lookup(ctx, "ns", "t", "missing"); !errors.Is(err, state.ErrNotFound) {
			t.Fatalf("Lookup(missing) = %v", err)
		}
		// A deleted key starts again at 1.
		must(t, st.Write(ctx, "ns", state.Delete("t", "k"), state.Put("t", "k", []byte("3"))))
		if r, _ := st.Lookup(ctx, "ns", "t", "k"); r.Version != 1 {
			t.Fatalf("after delete and put, version = %d", r.Version)
		}
	})

	t.Run("conditional writes", func(t *testing.T) {
		st := open(t)
		must(t, st.Write(ctx, "ns", state.Create("t", "k", []byte("a"))))
		if err := st.Write(ctx, "ns", state.Create("t", "k", []byte("b"))); !errors.Is(err, state.ErrConflict) {
			t.Fatalf("creating a key that has a record = %v, want ErrConflict", err)
		}
		must(t, st.Write(ctx, "ns", state.PutAt("t", "k", []byte("b"), 1)))
		if err := st.Write(ctx, "ns", state.PutAt("t", "k", []byte("c"), 1)); !errors.Is(err, state.ErrConflict) {
			t.Fatalf("a put at a stale version = %v, want ErrConflict", err)
		}
		if err := st.Write(ctx, "ns", state.PutAt("t", "gone", []byte("c"), 1)); !errors.Is(err, state.ErrConflict) {
			t.Fatalf("a put at a version of a missing key = %v, want ErrConflict", err)
		}
		if err := st.Write(ctx, "ns", state.DeleteAt("t", "k", 1)); !errors.Is(err, state.ErrConflict) {
			t.Fatalf("a delete at a stale version = %v, want ErrConflict", err)
		}
		if v, _ := st.Get(ctx, "ns", "t", "k"); string(v) != "b" {
			t.Fatalf("after refused writes = %q", v)
		}
		// A failed condition applies nothing, before it or after it.
		err := st.Write(ctx, "ns", state.Put("t", "before", []byte("1")), state.PutAt("t", "k", []byte("x"), 9), state.Put("t", "after", []byte("1")))
		if !errors.Is(err, state.ErrConflict) {
			t.Fatalf("a write with a stale op = %v, want ErrConflict", err)
		}
		for _, k := range []string{"before", "after"} {
			if _, err := st.Get(ctx, "ns", "t", k); !errors.Is(err, state.ErrNotFound) {
				t.Fatalf("a conflicting write left %q behind: %v", k, err)
			}
		}
		// Conditions see the ops before them in the same write.
		must(t, st.Write(ctx, "ns", state.Create("t", "n", []byte("1")), state.PutAt("t", "n", []byte("2"), 1), state.DeleteAt("t", "k", 2)))
		if r, _ := st.Lookup(ctx, "ns", "t", "n"); r.Version != 2 || string(r.Value) != "2" {
			t.Fatalf("create then put at 1 = %+v", r)
		}
		if _, err := st.Get(ctx, "ns", "t", "k"); !errors.Is(err, state.ErrNotFound) {
			t.Fatalf("delete at version = %v", err)
		}
		// A condition names a version or Missing, and a delete a version.
		for _, op := range []state.Op{{Tenant: "t", Key: "k", Value: []byte("1"), If: -2}, {Tenant: "t", Key: "k", If: state.Missing}} {
			if err := st.Write(ctx, "ns", op); !errors.Is(err, state.ErrInvalid) {
				t.Fatalf("op %+v = %v, want ErrInvalid", op, err)
			}
		}
		// A condition is per tenant.
		must(t, st.Write(ctx, "ns", state.Create("u", "n", []byte("1"))))
	})

	t.Run("compare and set racers", func(t *testing.T) {
		st := open(t)
		must(t, st.Write(ctx, "ns", state.Create("t", "count", []byte("0"))))
		const racers, each = 8, 10
		var wg sync.WaitGroup
		for range racers {
			wg.Go(func() {
				for range each {
					for {
						r, err := st.Lookup(ctx, "ns", "t", "count")
						if err != nil {
							t.Error(err)
							return
						}
						var n int
						_, _ = fmt.Sscan(string(r.Value), &n)
						err = st.Write(ctx, "ns", state.PutAt("t", "count", fmt.Appendf(nil, "%d", n+1), r.Version))
						if errors.Is(err, state.ErrConflict) {
							continue
						}
						if err != nil {
							t.Error(err)
						}
						break
					}
				}
			})
		}
		wg.Wait()
		r, err := st.Lookup(ctx, "ns", "t", "count")
		if err != nil || string(r.Value) != fmt.Sprint(racers*each) || r.Version != racers*each+1 {
			t.Fatalf("count = %q at version %d, %v; want %d at %d", r.Value, r.Version, err, racers*each, racers*each+1)
		}
	})

	t.Run("list pages a tenant by prefix", func(t *testing.T) {
		st := open(t)
		for i := range 25 {
			must(t, st.Write(ctx, "ns", state.Put("t", "m:a:"+state.Seq(int64(i)), []byte{byte(i)})))
		}
		must(t, st.Write(ctx, "ns", state.Put("t", "m:b:"+state.Seq(1), []byte("b"))))
		must(t, st.Write(ctx, "ns", state.Put("t", "m:", []byte("bare"))))
		must(t, st.Write(ctx, "ns", state.Put("t", "m;", []byte("next"))))
		must(t, st.Write(ctx, "ns", state.Put("u", "m:a:"+state.Seq(3), []byte("other tenant"))))
		must(t, st.Write(ctx, "other", state.Put("t", "m:a:"+state.Seq(3), []byte("other namespace"))))

		var got []string
		after := ""
		for pages := 0; ; pages++ {
			if pages > 10 {
				t.Fatal("the pages do not end")
			}
			recs, next, err := st.List(ctx, "ns", "t", state.Query{Prefix: "m:a:", After: after, Limit: 10})
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range recs {
				if r.Tenant != "t" || r.Version != 1 || len(r.Value) != 1 {
					t.Fatalf("record %+v", r)
				}
				got = append(got, r.Key)
			}
			if next == "" {
				break
			}
			if next != recs[len(recs)-1].Key {
				t.Fatalf("next = %q, last = %q", next, recs[len(recs)-1].Key)
			}
			after = next
		}
		if len(got) != 25 || got[0] != "m:a:"+state.Seq(0) || got[24] != "m:a:"+state.Seq(24) {
			t.Fatalf("pages = %v", got)
		}
		// A page that ends exactly at the last record says none follow, or
		// gives a cursor whose page is empty.
		recs, next, err := st.List(ctx, "ns", "t", state.Query{Prefix: "m:a:", After: got[19], Limit: 5})
		if err != nil || len(recs) != 5 {
			t.Fatalf("last page = %d, %v", len(recs), err)
		}
		if next != "" {
			if more, _, _ := st.List(ctx, "ns", "t", state.Query{Prefix: "m:a:", After: next, Limit: 5}); len(more) != 0 {
				t.Fatalf("past the last page = %d", len(more))
			}
		}

		// Newest first.
		recs, next, err = st.List(ctx, "ns", "t", state.Query{Prefix: "m:a:", Limit: 3, Desc: true})
		if err != nil || len(recs) != 3 || recs[0].Key != "m:a:"+state.Seq(24) || recs[2].Key != "m:a:"+state.Seq(22) || next != recs[2].Key {
			t.Fatalf("desc = %v, %q, %v", keys(recs), next, err)
		}
		recs, _, err = st.List(ctx, "ns", "t", state.Query{Prefix: "m:a:", After: next, Limit: 2, Desc: true})
		if err != nil || len(recs) != 2 || recs[0].Key != "m:a:"+state.Seq(21) {
			t.Fatalf("desc after = %v, %v", keys(recs), err)
		}

		// No prefix is the whole tenant, in byte order.
		recs, next, err = st.List(ctx, "ns", "t", state.Query{Limit: state.MaxList + 5})
		if err != nil || len(recs) != 28 || next != "" || recs[0].Key != "m:" || recs[27].Key != "m;" {
			t.Fatalf("whole tenant = %d records %q..., %q, %v", len(recs), keys(recs[:1]), next, err)
		}
		if recs, _, err := st.List(ctx, "ns", "t", state.Query{Prefix: "m:b:", Limit: 10}); err != nil || len(recs) != 1 || string(recs[0].Value) != "b" {
			t.Fatalf("prefix m:b: = %v, %v", keys(recs), err)
		}
		if recs, _, err := st.List(ctx, "ns", "t", state.Query{Prefix: "z", Limit: 10}); err != nil || len(recs) != 0 {
			t.Fatalf("an empty prefix range = %v, %v", keys(recs), err)
		}
		if recs, _, err := st.List(ctx, "ns", "nobody", state.Query{Limit: 10}); err != nil || len(recs) != 0 {
			t.Fatalf("another tenant = %v, %v", keys(recs), err)
		}
		for _, q := range []state.Query{{}, {Limit: -1}, {Prefix: "\xff", Limit: 1}} {
			if q.Prefix != "" {
				q.Prefix = string([]byte{0xff})
			}
			if _, _, err := st.List(ctx, "ns", "t", q); !errors.Is(err, state.ErrInvalid) {
				t.Fatalf("List(%+v) = %v, want ErrInvalid", q, err)
			}
		}
	})

	t.Run("list prefixes past the ASCII range", func(t *testing.T) {
		st := open(t)
		for _, k := range []string{"é", "éa", "é\U0010FFFF", "ê", "\U0010FFFF", "\U0010FFFFa", "\uD7FF", "\uD7FFz", "\uE000"} {
			must(t, st.Write(ctx, "ns", state.Put("t", k, []byte("v"))))
		}
		for _, c := range []struct {
			prefix string
			want   int
		}{{"é", 3}, {"\U0010FFFF", 2}, {"\uD7FF", 2}, {"ê", 1}} {
			recs, _, err := st.List(ctx, "ns", "t", state.Query{Prefix: c.prefix, Limit: 100})
			if err != nil || len(recs) != c.want {
				t.Fatalf("prefix %q = %v, %v, want %d", c.prefix, keys(recs), err, c.want)
			}
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

func keys(recs []state.Record) []string {
	out := make([]string, len(recs))
	for i, r := range recs {
		out[i] = r.Key
	}
	return out
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
