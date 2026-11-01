package platform

import (
	"context"
	"errors"
	"testing"
	"time"

	dqueue "github.com/urmzd/anchor/pkg/queue"
)

func claimWithin(t *testing.T, q dqueue.Queue, d time.Duration) (dqueue.Item, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), d)
	defer cancel()
	it, _, err := q.Claim(ctx, time.Minute)
	return it, err
}

// Before a drain the gate passes claims through and counts what is held;
// settling or releasing lets go of it.
func TestTheGateCountsWhatIsHeld(t *testing.T) {
	d := newDrain()
	q := d.wrap(dqueue.NewMemory())
	ctx := t.Context()
	for _, id := range []string{"a", "b"} {
		if err := q.Enqueue(ctx, id, 0); err != nil {
			t.Fatal(err)
		}
	}
	a, err := claimWithin(t, q, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	b, err := claimWithin(t, q, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if n := d.inFlight(); n != 2 {
		t.Fatalf("in flight = %d, want 2", n)
	}
	if err := q.Settle(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := q.Release(ctx, b, 0, false); err != nil {
		t.Fatal(err)
	}
	if n := d.inFlight(); n != 0 {
		t.Fatalf("in flight = %d after settle and release, want 0", n)
	}
}

// A drain cuts off a claim that is waiting for work, and no claim after it
// takes a run: what is queued stays queued for another replica.
func TestADrainStopsClaims(t *testing.T) {
	d := newDrain()
	inner := dqueue.NewMemory()
	q := d.wrap(inner)
	waiting := make(chan error, 1)
	go func() {
		_, err := claimWithin(t, q, 2*time.Second)
		waiting <- err
	}()
	time.Sleep(50 * time.Millisecond)
	d.stop()
	if err := q.Enqueue(t.Context(), "late", 0); err != nil {
		t.Fatal(err)
	}
	if err := <-waiting; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting claim = %v, want it to wait out its context", err)
	}
	if _, err := claimWithin(t, q, 100*time.Millisecond); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("claim while draining = %v, want none", err)
	}
	// The run is still there for another worker.
	if it, err := claimWithin(t, inner, time.Second); err != nil || it.RunID != "late" {
		t.Fatalf("queue after drain = %+v, %v", it, err)
	}
}

// While draining, a run that a held run called is still claimed, so a turn
// waiting on its subagent can finish, and any other run is handed back
// without being charged a failure.
func TestADrainStillClaimsWhatHeldWorkCalled(t *testing.T) {
	d := newDrain()
	inner := dqueue.NewMemory()
	q := d.wrap(inner)
	ctx := t.Context()
	if err := q.Enqueue(ctx, "parent", 0); err != nil {
		t.Fatal(err)
	}
	parent, err := claimWithin(t, q, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	d.called(parent.RunID, "child")
	d.called("not-held", "stranger")
	d.stop()
	if err := q.Enqueue(ctx, "foreign", 0); err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(ctx, "child", 0); err != nil {
		t.Fatal(err)
	}
	child, err := claimWithin(t, q, 5*time.Second)
	if err != nil || child.RunID != "child" {
		t.Fatalf("claim while draining = %+v, %v; want the child", child, err)
	}
	if err := q.Settle(ctx, child); err != nil {
		t.Fatal(err)
	}
	if err := q.Settle(ctx, parent); err != nil {
		t.Fatal(err)
	}
	if n := d.inFlight(); n != 0 {
		t.Fatalf("in flight = %d, want 0", n)
	}
	// Nothing is left to wait for, so the gate claims nothing more.
	if _, err := claimWithin(t, q, 100*time.Millisecond); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("claim with nothing held = %v, want none", err)
	}
	foreign, err := claimWithin(t, inner, time.Second)
	if err != nil || foreign.RunID != "foreign" {
		t.Fatalf("foreign run = %+v, %v", foreign, err)
	}
	if foreign.Failures != 0 {
		t.Errorf("handing a run back charged it %d failures", foreign.Failures)
	}
}
