package app

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/leader"
	"github.com/urmzd/humagen/pkg/spec"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// Two replicas on one Postgres run one Slack app: one Socket Mode
// connection at a time, and the other replica takes it over when the one
// running it stops.
func TestASlackAppRunsOnOneReplicaAndMovesWhenItStops(t *testing.T) {
	pool := statetest.Pool(t)
	app := &slackApp{tenant: "acme", choice: spec.Slack{Team: "T1"}}
	var running, most atomic.Int32
	type replica struct {
		held   atomic.Int32
		runs   atomic.Int32
		cancel context.CancelFunc
		done   chan error
	}
	start := func() *replica {
		r := &replica{done: make(chan error, 1)}
		lease, err := leader.NewPostgres(pool.Config().ConnConfig, leader.WithInterval(100*time.Millisecond), leader.WithKey(app.leaseKey()))
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		r.cancel = cancel
		go func() {
			r.done <- runLeased(ctx, lease, &r.held, slog.New(slog.DiscardHandler), app.leaseKey(), func(ctx context.Context) error {
				r.runs.Add(1)
				if n := running.Add(1); n > most.Load() {
					most.Store(n)
				}
				<-ctx.Done()
				running.Add(-1)
				return nil
			})
		}()
		t.Cleanup(func() { cancel(); <-r.done })
		return r
	}
	a, b := start(), start()
	waitFor(t, func() bool { return a.held.Load()+b.held.Load() == 1 })
	time.Sleep(time.Second)
	if most.Load() != 1 {
		t.Fatalf("%d connections at once", most.Load())
	}
	holder, other := a, b
	if b.held.Load() == 1 {
		holder, other = b, a
	}
	holder.cancel()
	if err := <-holder.done; err != nil {
		t.Fatal(err)
	}
	holder.done <- nil // for the cleanup
	waitFor(t, func() bool { return other.held.Load() == 1 })
	if most.Load() != 1 {
		t.Fatalf("%d connections at once", most.Load())
	}
}

// An app that cannot start at all ends its campaign with the reason, rather
// than taking and giving back its lease for ever.
func TestASlackAppThatCannotStartGivesUpItsLease(t *testing.T) {
	var held atomic.Int32
	want := errors.New("no node")
	err := runLeased(t.Context(), leader.Always{}, &held, slog.New(slog.DiscardHandler), "k", func(context.Context) error { return want })
	if !errors.Is(err, want) || held.Load() != 0 {
		t.Fatalf("err = %v, held = %d", err, held.Load())
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
