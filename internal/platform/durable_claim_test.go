package platform_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/urmzd/fiber/pkg/agentrt"
	"github.com/urmzd/fiber/pkg/agentspec"
	"github.com/urmzd/fiber/pkg/durable"
	"github.com/urmzd/saige/agent/agenttest"
	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// lateClaims stands for a database that gets to the run queue's claims late,
// as a loaded server can: a claim is on the wire when the worker stops, and
// the server carries it out only after the worker has handed back the run it
// was working on. While it holds, a claim written to the database waits at the
// gate, and whatever its connection writes after the claim waits behind it.
// The claim's connection still reads with its deadlines, so a worker that
// stops waiting for the answer is not kept waiting.
type lateClaims struct {
	mu    sync.Mutex
	hold  bool
	count int
	gate  chan struct{}
	once  sync.Once
}

func newLateClaims() *lateClaims { return &lateClaims{gate: make(chan struct{})} }

// holdClaims holds every claim written from now until open.
func (l *lateClaims) holdClaims() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.hold = true
}

// open lets the held claims through, and every claim after them.
func (l *lateClaims) open() {
	l.once.Do(func() {
		l.mu.Lock()
		l.hold = false
		l.mu.Unlock()
		close(l.gate)
	})
}

// held is how many claims have been held.
func (l *lateClaims) held() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.count
}

func (l *lateClaims) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	c, err := new(net.Dialer).DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	return &lateConn{Conn: c, l: l}, nil
}

// claimText is in the queue's claim statement and in no other statement the
// platform makes.
var claimText = []byte("SKIP LOCKED")

// claims reports whether b, what a connection writes to the database, carries
// out a claim: it holds the claim statement and executes it. Describing the
// statement, which the connection does once before it first claims, claims
// nothing.
func claims(b []byte) bool {
	if !bytes.Contains(b, claimText) {
		return false
	}
	for len(b) >= 5 {
		kind, n := b[0], int(binary.BigEndian.Uint32(b[1:5]))
		if n < 4 || len(b) < 1+n {
			return false
		}
		if kind == 'E' {
			return true
		}
		b = b[1+n:]
	}
	return false
}

type lateConn struct {
	net.Conn
	l  *lateClaims
	mu sync.Mutex
	// tail is closed once everything written so far has gone out.
	tail chan struct{}
}

func (c *lateConn) Write(b []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.l.mu.Lock()
	hold := c.l.hold && claims(b)
	if hold {
		c.l.count++
	}
	c.l.mu.Unlock()
	if !hold && c.tail == nil {
		return c.Conn.Write(b)
	}
	prev, done := c.tail, make(chan struct{})
	c.tail = done
	b = bytes.Clone(b)
	go func() {
		if prev != nil {
			<-prev
		}
		if hold {
			<-c.l.gate
		}
		_, _ = c.Conn.Write(b)
		c.mu.Lock()
		if c.tail == done {
			c.tail = nil
		}
		c.mu.Unlock()
		close(done)
	}()
	return len(b), nil
}

// A deadline bounds waiting for the answer. What has been written is on the
// wire, and reaches the database whatever the deadline.
func (c *lateConn) SetDeadline(t time.Time) error { return c.SetReadDeadline(t) }

func (c *lateConn) SetWriteDeadline(time.Time) error { return nil }

func (c *lateConn) Close() error {
	c.mu.Lock()
	tail := c.tail
	c.mu.Unlock()
	if tail != nil {
		<-tail
	}
	return c.Conn.Close()
}

// A worker's idle slots go on claiming while a run works, and one of their
// claims can be on its way to the database when the platform stops. The
// database can carry it out after the worker has handed back the run it was
// working on, and lease that run to the claim. The worker had stopped waiting
// for the answer, so nobody held the lease: the next platform could not resume
// the run until the lease ran out, 30 seconds later, and the run was charged a
// failure for it. A claim the database answers as the platform stops comes
// back to the worker, which hands the run back, and the next platform resumes
// it at once.
func TestARunClaimedAsThePlatformStopsIsResumedAfterTheRestart(t *testing.T) {
	ctx := t.Context()
	pool := statetest.Pool(t)
	late := newLateClaims()
	cfg := pool.Config()
	cfg.ConnConfig.DialFunc = late.dial
	// Every statement carries its text, so a claim is known on the wire.
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeCacheDescribe
	slow, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(slow.Close)
	// A claim still held when the test ends goes through, so its connection
	// can close.
	t.Cleanup(late.open)
	first, release, err := platform.OpenPostgres(ctx, slow)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = release() })

	model := func(m saigetypes.Provider) []agentrt.Option {
		return []agentrt.Option{agentrt.WithProvider(agentrt.ProviderOffline, func(agentrt.Spec) (saigetypes.Provider, error) { return m, nil })}
	}
	held := &holding{called: make(chan struct{})}
	p, stop := runningWith(t, platform.Config{Backends: first, AgentOptions: model(held)})
	if _, err := p.Agents.Create(ctx, tenant, agentspec.AgentSpec{Name: "lead", Model: "offline", Provider: "offline", SystemPrompt: "Lead."}, "test"); err != nil {
		t.Fatal(err)
	}
	r, err := p.Invoke(ctx, tag("claimed-at-stop", "lead"))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-held.called:
	case <-time.After(10 * time.Second):
		t.Fatal("the run never called its model")
	}

	// Where the run stands in the queue, read past the held claims.
	type place struct {
		ready, leased bool
		attempt       int
	}
	queued := func() (place, error) {
		var at place
		err := pool.QueryRow(ctx, "SELECT ready_at IS NOT NULL, lease_until IS NOT NULL, attempt FROM "+durable.QueueTable+" WHERE run_id = $1", r.ID).Scan(&at.ready, &at.leased, &at.attempt)
		return at, err
	}
	late.holdClaims()
	eventually(t, "an idle slot's claim is on its way to the database", func() bool { return late.held() > 0 })
	// The database gets to the held claims once the stopping worker has
	// handed the run back.
	go func() {
		for ctx.Err() == nil {
			if at, err := queued(); err == nil && at.ready && !at.leased {
				late.open()
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	stop()
	eventually(t, "a held claim reached the database", func() bool {
		at, err := queued()
		return err == nil && at.attempt > 1
	})
	if at, err := queued(); err != nil || at.leased {
		t.Fatalf("after the platform stopped the run is %+v (%v): leased to a claim no worker holds", at, err)
	}

	if err := release(); err != nil {
		t.Fatal(err)
	}
	again, releaseAgain, err := platform.OpenPostgres(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = releaseAgain() })
	const answer = "Answered after the restart."
	q, _ := runningWith(t, platform.Config{
		Backends:     again,
		AgentOptions: model(&agenttest.ScriptedProvider{Responses: [][]saigetypes.Delta{say(answer)}}),
	})
	if got := await(t, q, r.ID, runs.Completed); got.Answer != answer {
		t.Fatalf("the run answered %q, want %q", got.Answer, answer)
	}
}
