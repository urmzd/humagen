package runs

import (
	"context"
	"fmt"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/urmzd/saige/flow/event"

	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// Reading several conversations at once answers what reading each alone
// does, on both backends, and never reads another tenant's.
func TestSeveralConversationsReadAtOnceAreEachOnesOwn(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		x, err := Open(open(t))
		if err != nil {
			t.Fatal(err)
		}
		threads := []string{"th-a", "th-b", "th-c"}
		for i := range 9 {
			id := fmt.Sprintf("r%d", i)
			record(t, x, "acme", id, "lead", OriginAPI, time.Duration(i)*time.Second, map[string]string{MetaThread: threads[i%3]})
			if i%2 == 0 {
				end(t, x, "acme", id, time.Duration(i)*time.Second+time.Millisecond, event.TypeCompleted, wire.Lifecycle{Text: "done"})
			}
		}
		record(t, x, "globex", "other", "lead", OriginAPI, time.Minute, map[string]string{MetaThread: "th-a"})

		ask := append([]string{"th-none"}, threads...)
		turns, err := x.Conversations("acme", ask)
		if err != nil {
			t.Fatal(err)
		}
		sums, err := x.ConversationSummaries("acme", ask)
		if err != nil {
			t.Fatal(err)
		}
		if len(turns) != 3 || len(sums) != 3 {
			t.Fatalf("read %d conversations and %d summaries, want 3 of each", len(turns), len(sums))
		}
		for _, th := range threads {
			one, err := x.Conversation("acme", th)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(turns[th], one) {
				t.Errorf("%s: read at once = %+v, alone = %+v", th, turns[th], one)
			}
			sum, err := x.ConversationSummary("acme", th)
			if err != nil || !reflect.DeepEqual(sums[th], sum) {
				t.Errorf("%s: summary at once = %+v, alone = %+v (%v)", th, sums[th], sum, err)
			}
			for _, r := range turns[th] {
				if r.Tenant != "acme" {
					t.Errorf("%s holds %s of tenant %q", th, r.ID, r.Tenant)
				}
			}
		}
		if other, _ := x.Conversations("globex", []string{"th-a"}); len(other["th-a"]) != 1 || other["th-a"][0].ID != "other" {
			t.Fatalf("the other tenant's conversation = %+v", other)
		}
	})
}

// A list of a known few asks the store for that many runs, not a full page.
func TestListReadsOnlyAsManyRunsAsItReturns(t *testing.T) {
	st := &pages{Store: state.NewMemory()}
	x, err := Open(st)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 30 {
		record(t, x, "acme", fmt.Sprintf("r%d", i), "lead", OriginAPI, time.Duration(i)*time.Second, nil)
	}
	st.limits = nil
	if got := x.List("acme", 10); len(got) != 10 || got[0].ID != "r29" {
		t.Fatalf("list = %d runs from %q", len(got), got[0].ID)
	}
	if !reflect.DeepEqual(st.limits, []int{10}) {
		t.Fatalf("the list asked for pages of %v runs, want one of 10", st.limits)
	}
}

// pages records the limit of each page of runs a store is asked for.
type pages struct {
	state.Store
	limits []int
}

func (p *pages) List(ctx context.Context, namespace, tenant string, q state.Query) ([]state.Record, string, error) {
	if q.Prefix == "o:" {
		p.limits = append(p.limits, q.Limit)
	}
	return p.Store.List(ctx, namespace, tenant, q)
}

// BenchmarkConversations reads the turns of fifty conversations of twenty
// turns each, one at a time and all at once, on each backend. On Postgres a
// statement is a round trip, so it reports statements a read besides time.
func BenchmarkConversations(b *testing.B) {
	for _, backend := range []string{"memory", "postgres"} {
		for _, mode := range []string{"each", "at-once"} {
			b.Run(backend+"/"+mode, func(b *testing.B) {
				st := &counted{Store: benchStore(b, backend)}
				x, err := Open(st)
				if err != nil {
					b.Fatal(err)
				}
				threads := make([]string, 50)
				for i := range threads {
					threads[i] = fmt.Sprintf("th-%d", i)
				}
				for i := range 1000 {
					tag := wire.NewEvent(wire.Raise{ID: fmt.Sprintf("r%d", i), Tenant: "acme", Agent: "lead", Origin: OriginAPI, Text: "go", Context: map[string]string{MetaThread: threads[i%50]}, At: t0.Add(time.Duration(i) * time.Second)})
					if err := x.Record(tag.ID, tag, 1); err != nil {
						b.Fatal(err)
					}
				}
				st.n.Store(0)
				b.ResetTimer()
				for range b.N {
					if mode == "each" {
						for _, th := range threads {
							if rr, err := x.Conversation("acme", th); err != nil || len(rr) != 20 {
								b.Fatalf("%s: %d turns, %v", th, len(rr), err)
							}
						}
						continue
					}
					if got, err := x.Conversations("acme", threads); err != nil || len(got) != 50 {
						b.Fatalf("%d conversations, %v", len(got), err)
					}
				}
				b.ReportMetric(float64(st.n.Load())/float64(b.N), "statements/op")
			})
		}
	}
}

// counted counts the statements a store is asked for.
type counted struct {
	state.Store
	n atomic.Int64
}

func (c *counted) Lookup(ctx context.Context, namespace, tenant, key string) (state.Record, error) {
	c.n.Add(1)
	return c.Store.Lookup(ctx, namespace, tenant, key)
}

func (c *counted) LookupMany(ctx context.Context, namespace, tenant string, keys []string) ([]state.Record, error) {
	c.n.Add(1)
	return state.LookupMany(ctx, c.Store, namespace, tenant, keys)
}

func (c *counted) List(ctx context.Context, namespace, tenant string, q state.Query) ([]state.Record, string, error) {
	c.n.Add(1)
	return c.Store.List(ctx, namespace, tenant, q)
}

var benchSchemas atomic.Uint64

// benchStore is a store on a backend: memory, or Postgres on a schema of its
// own in the database statetest.URLEnv names, without which it skips.
func benchStore(b *testing.B, backend string) state.Store {
	b.Helper()
	if backend == "memory" {
		return state.NewMemory()
	}
	url := legacy.Getenv(statetest.URLEnv)
	if url == "" {
		b.Skip("set " + statetest.URLEnv)
	}
	ctx := context.Background()
	schema := fmt.Sprintf("humagenbench_%d_%d", time.Now().UnixNano(), benchSchemas.Add(1))
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		b.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		b.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		pool.Close()
	})
	st, err := state.OpenPostgres(ctx, pool)
	if err != nil {
		b.Fatal(err)
	}
	return st
}
