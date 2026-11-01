package leader

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/urmzd/humagen/pkg/state/statetest"
)

const tick = 100 * time.Millisecond

// candidate is one process campaigning: whether it leads now, and how many
// terms it has begun.
type candidate struct {
	leading atomic.Bool
	terms   atomic.Int32
	stop    context.CancelFunc
	done    chan struct{}
}

func campaign(t *testing.T, e Elector, together *atomic.Int32, most *atomic.Int32) *candidate {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	c := &candidate{stop: cancel, done: make(chan struct{})}
	go func() {
		defer close(c.done)
		_ = e.Campaign(ctx, func(ctx context.Context) {
			c.terms.Add(1)
			c.leading.Store(true)
			if n := together.Add(1); n > most.Load() {
				most.Store(n)
			}
			<-ctx.Done()
			together.Add(-1)
			c.leading.Store(false)
		})
	}()
	t.Cleanup(func() { cancel(); <-c.done })
	return c
}

func elector(t *testing.T, pool *pgxpool.Pool) *Postgres {
	t.Helper()
	e, err := NewPostgres(pool.Config().ConnConfig, WithInterval(tick))
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAlwaysLeadsAtOnce(t *testing.T) {
	var together, most atomic.Int32
	c := campaign(t, Always{}, &together, &most)
	eventually(t, "the only process to lead", c.leading.Load)
	c.stop()
	<-c.done
	if c.leading.Load() {
		t.Fatal("still leading after the campaign ended")
	}
}

// Two processes on one database: exactly one leads, never both.
func TestOneLeaderOnOneDatabase(t *testing.T) {
	pool := statetest.Pool(t)
	var together, most atomic.Int32
	a := campaign(t, elector(t, pool), &together, &most)
	b := campaign(t, elector(t, pool), &together, &most)
	eventually(t, "a leader", func() bool { return a.leading.Load() || b.leading.Load() })
	time.Sleep(10 * tick)
	if a.leading.Load() == b.leading.Load() {
		t.Fatalf("leading: a=%v b=%v, want exactly one", a.leading.Load(), b.leading.Load())
	}
	if most.Load() != 1 {
		t.Fatalf("%d processes led at once", most.Load())
	}
}

// Another schema is another deployment, with its own leader.
func TestASchemaElectsItsOwnLeader(t *testing.T) {
	var together, most atomic.Int32
	a := campaign(t, elector(t, statetest.Pool(t)), &together, &most)
	b := campaign(t, elector(t, statetest.Pool(t)), &together, &most)
	eventually(t, "both deployments to lead", func() bool { return a.leading.Load() && b.leading.Load() })
}

// The leader stops cleanly: it lets the lock go, and the other takes over.
func TestALeaderThatStopsHandsOver(t *testing.T) {
	pool := statetest.Pool(t)
	var together, most atomic.Int32
	a := campaign(t, elector(t, pool), &together, &most)
	eventually(t, "a to lead", a.leading.Load)
	b := campaign(t, elector(t, pool), &together, &most)
	a.stop()
	<-a.done
	eventually(t, "b to take over", b.leading.Load)
	if most.Load() != 1 {
		t.Fatalf("%d processes led at once", most.Load())
	}
}

// The leader's connection is killed under it: its term ends, and a new term
// begins on whichever process wins the lock next, never beside the old one.
func TestFailoverWhenTheLeadersConnectionCloses(t *testing.T) {
	pool := statetest.Pool(t)
	var together, most atomic.Int32
	a := campaign(t, elector(t, pool), &together, &most)
	eventually(t, "a to lead", a.leading.Load)
	b := campaign(t, elector(t, pool), &together, &most)
	time.Sleep(5 * tick)
	if b.leading.Load() {
		t.Fatal("b leads while a holds the lock")
	}

	var killed int
	if err := pool.QueryRow(t.Context(), `
		SELECT count(pg_terminate_backend(pid)) FROM pg_locks
		WHERE locktype = 'advisory' AND classid = $1::oid AND objid = hashtext(current_schema())::oid AND granted`,
		int64(LockClass)).Scan(&killed); err != nil || killed != 1 {
		t.Fatalf("terminate the leader's session: killed %d, %v", killed, err)
	}
	eventually(t, "a's term to end", func() bool { return !a.leading.Load() || a.terms.Load() > 1 })
	// Either may win the free lock, a campaigning again or b; what holds is
	// that a new term begins and never beside another.
	eventually(t, "a new term", func() bool { return a.terms.Load()+b.terms.Load() >= 2 && (a.leading.Load() || b.leading.Load()) })
	if most.Load() != 1 {
		t.Fatalf("%d processes led at once", most.Load())
	}
}

// A term that returns gives leadership up, and the campaign goes on.
func TestATermThatReturnsCampaignsAgain(t *testing.T) {
	pool := statetest.Pool(t)
	e := elector(t, pool)
	var mu sync.Mutex
	terms := 0
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = e.Campaign(ctx, func(context.Context) {
			mu.Lock()
			terms++
			mu.Unlock()
		})
	}()
	eventually(t, "a second term", func() bool { mu.Lock(); defer mu.Unlock(); return terms >= 2 })
	cancel()
	<-done
}

func keyed(t *testing.T, pool *pgxpool.Pool, key string, opts ...Option) *Postgres {
	t.Helper()
	e, err := NewPostgres(pool.Config().ConnConfig, append([]Option{WithInterval(tick), WithKey(key)}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// A lease on a key is held by one process at a time, is taken over when
// its holder stops, and is apart from the deployment's leader.
func TestALeaseIsHeldOnceAndTakenOver(t *testing.T) {
	pool := statetest.Pool(t)
	var together, most, lt, lm atomic.Int32
	lead := campaign(t, elector(t, pool), &lt, &lm)
	a := campaign(t, keyed(t, pool, "slack-app:acme"), &together, &most)
	b := campaign(t, keyed(t, pool, "slack-app:acme"), &together, &most)
	eventually(t, "a holder", func() bool { return a.leading.Load() || b.leading.Load() })
	eventually(t, "the leader beside the lease", lead.leading.Load)
	time.Sleep(10 * tick)
	if most.Load() != 1 {
		t.Fatalf("%d processes held one lease at once", most.Load())
	}
	holder, other := a, b
	if b.leading.Load() {
		holder, other = b, a
	}
	holder.stop()
	<-holder.done
	eventually(t, "the other process to take the lease over", other.leading.Load)
	if most.Load() != 1 {
		t.Fatalf("%d processes held one lease at once", most.Load())
	}
}

// Two processes and two keys: a process that holds a lease waits longer
// for the next, so each process ends up holding one.
func TestLeasesSpreadAcrossProcesses(t *testing.T) {
	pool := statetest.Pool(t)
	type process struct{ held atomic.Int32 }
	var procs [2]process
	var cands [2][2]*candidate
	for i := range procs {
		delay := WithDelay(func() time.Duration { return time.Duration(procs[i].held.Load()) * 5 * tick })
		for k, key := range []string{"slack-app:acme", "slack-app:globex"} {
			// The first process starts first, so it would take both keys
			// without the delay.
			if i == 0 && k == 1 {
				eventually(t, "the first process to hold its first lease", func() bool { return procs[0].held.Load() >= 1 })
			}
			e := keyed(t, pool, key, delay)
			ctx, cancel := context.WithCancel(t.Context())
			c := &candidate{stop: cancel, done: make(chan struct{})}
			go func() {
				defer close(c.done)
				_ = e.Campaign(ctx, func(ctx context.Context) {
					procs[i].held.Add(1)
					c.leading.Store(true)
					<-ctx.Done()
					c.leading.Store(false)
					procs[i].held.Add(-1)
				})
			}()
			t.Cleanup(func() { cancel(); <-c.done })
			cands[i][k] = c
		}
	}
	eventually(t, "both keys held", func() bool {
		return (cands[0][0].leading.Load() || cands[1][0].leading.Load()) && (cands[0][1].leading.Load() || cands[1][1].leading.Load())
	})
	if procs[0].held.Load() != 1 || procs[1].held.Load() != 1 {
		t.Fatalf("held: %d and %d, want one each", procs[0].held.Load(), procs[1].held.Load())
	}
}
