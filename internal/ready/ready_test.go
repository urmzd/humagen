package ready_test

import (
	"context"
	"errors"
	"testing"

	"github.com/urmzd/humagen/internal/ready"
)

func TestAGateIsReadyOnlyStartedHealthyAndNotDraining(t *testing.T) {
	var dbErr error
	g := ready.New(nil, ready.Check{Name: "database", Probe: func(context.Context) error { return dbErr }})
	ctx := t.Context()

	if r, ok := g.Check(ctx); ok || r.Status != ready.Starting {
		t.Fatalf("before start = %+v, %v", r, ok)
	}
	g.Started()
	if r, ok := g.Check(ctx); !ok || r.Status != ready.Ready || len(r.Checks) != 1 || !r.Checks[0].OK {
		t.Fatalf("started = %+v, %v", r, ok)
	}
	dbErr = errors.New("connection refused")
	if r, ok := g.Check(ctx); ok || r.Status != ready.Unavailable || r.Checks[0].OK || r.Checks[0].Name != "database" {
		t.Fatalf("database down = %+v, %v", r, ok)
	}
	dbErr = nil
	g.Drain()
	if !g.Draining() {
		t.Fatal("Draining() = false")
	}
	if r, ok := g.Check(ctx); ok || r.Status != ready.Draining {
		t.Fatalf("draining = %+v, %v", r, ok)
	}
}

// A check that hangs is cut off rather than holding the probe.
func TestAHangingCheckFails(t *testing.T) {
	g := ready.New(nil, ready.Check{Name: "bus", Probe: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }})
	g.Started()
	if r, ok := g.Check(t.Context()); ok || r.Status != ready.Unavailable {
		t.Fatalf("hanging check = %+v, %v", r, ok)
	}
}
