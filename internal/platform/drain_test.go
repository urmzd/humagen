package platform_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/runs"
)

// A draining platform refuses a new turn, from a trigger or a conversation,
// and says it is draining; a run it finished before is untouched.
func TestADrainingPlatformRefusesNewTurns(t *testing.T) {
	p := start(t, call.Limits{})
	ctx := t.Context()
	r, err := p.Invoke(ctx, tag("before", "lead"))
	if err != nil {
		t.Fatal(err)
	}
	await(t, p, r.ID, runs.Completed)
	if err := p.WaitIdle(ctx); err != nil {
		t.Fatal(err)
	}

	p.Drain()
	p.Drain() // idempotent
	if !p.Draining() {
		t.Fatal("Draining() = false after Drain")
	}
	if _, err := p.Invoke(ctx, tag("after", "lead")); !errors.Is(err, platform.ErrDraining) {
		t.Errorf("Invoke while draining = %v, want ErrDraining", err)
	}
	if _, err := p.Converse(ctx, tenant, platform.Turn{ID: "t-after", Agent: "lead", Text: "Hello"}); !errors.Is(err, platform.ErrDraining) {
		t.Errorf("Converse while draining = %v, want ErrDraining", err)
	}
	if n := p.InFlight(); n != 0 {
		t.Errorf("in flight = %d, want 0", n)
	}
	// The run that joined before is still the same run.
	if again, err := p.Runs.Get(tenant, r.ID); err != nil || again.Status != runs.Completed {
		t.Errorf("earlier run = %+v, %v", again, err)
	}
}

// WaitIdle returns at once when nothing is held.
func TestWaitIdleReturnsAtOnceWhenNothingIsHeld(t *testing.T) {
	p := start(t, call.Limits{})
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := p.WaitIdle(ctx); err != nil {
		t.Fatalf("idle platform: %v", err)
	}
}
