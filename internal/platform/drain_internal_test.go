package platform

import (
	"context"
	"errors"
	"testing"
	"time"

	dqueue "github.com/urmzd/saige/durable/queue"
	drun "github.com/urmzd/saige/durable/run"
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

// A run whose lease lapses under the attempt at work is delivered again, to
// the process still executing it. The gate does not take it twice: the second
// delivery goes back to the queue charged nothing, the first attempt's hold
// stands, and once the first attempt lets go the run is claimed again. The
// lapse itself costs the run the one failure the queue charges for it.
func TestTheGateHandsBackARunItStillHolds(t *testing.T) {
	d := newDrain()
	inner := dqueue.NewMemory()
	q := d.wrap(inner)
	ctx := t.Context()
	if err := q.Enqueue(ctx, "run", 0); err != nil {
		t.Fatal(err)
	}
	// A lease short enough to lapse while the first attempt still works.
	first, _, err := q.Claim(ctx, 20*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	// Long enough for the lease to lapse and the run to come back more than
	// once: no claim takes it while the first attempt holds it.
	if again, err := claimWithin(t, q, 2*heldRetry+heldRetry/2); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("claim of a run still held = %+v, %v; want none", again, err)
	}
	if n := d.inFlight(); n != 1 {
		t.Fatalf("in flight = %d while the first attempt works, want 1", n)
	}
	// The first attempt ends. The queue refuses its settle, since the lease is
	// no longer its own, and the gate lets go of the run all the same.
	if err := q.Settle(ctx, first); !errors.Is(err, drun.ErrSuperseded) {
		t.Fatalf("settle of the superseded attempt = %v, want %v", err, drun.ErrSuperseded)
	}
	if n := d.inFlight(); n != 0 {
		t.Fatalf("in flight = %d after the first attempt let go, want 0", n)
	}
	next, err := claimWithin(t, q, 2*heldRetry)
	if err != nil || next.RunID != "run" {
		t.Fatalf("claim after the first attempt let go = %+v, %v; want the run", next, err)
	}
	if next.Failures != 1 {
		t.Errorf("the run was charged %d failures, want 1: the lapsed lease and nothing for being handed back", next.Failures)
	}
	if n := d.inFlight(); n != 1 {
		t.Fatalf("in flight = %d, want 1", n)
	}
	if err := q.Settle(ctx, next); err != nil {
		t.Fatal(err)
	}
}

// scripted is a queue that delivers what a test gives it, records what is
// handed back, and answers every heartbeat as superseded. Nothing behind it
// ever settles or releases a claim: it stands for a worker that drops a claim
// it lost without a word.
type scripted struct {
	dqueue.Queue
	deliver  chan dqueue.Item
	released []dqueue.Item
}

func (s *scripted) Claim(ctx context.Context, _ time.Duration) (dqueue.Item, time.Time, error) {
	select {
	case it := <-s.deliver:
		return it, time.Time{}, nil
	case <-ctx.Done():
		return dqueue.Item{}, time.Time{}, ctx.Err()
	}
}

func (s *scripted) Heartbeat(context.Context, dqueue.Item, time.Duration) (time.Time, error) {
	return time.Time{}, drun.ErrSuperseded
}

func (s *scripted) Release(_ context.Context, it dqueue.Item, _ time.Duration, failed bool) error {
	if failed {
		return errors.New("a run handed back was charged a failure")
	}
	s.released = append(s.released, it)
	return nil
}

func (s *scripted) Settle(context.Context, dqueue.Item) error { return drun.ErrSuperseded }

// gateOn is a gate over a scripted queue, on a clock the test moves.
func gateOn(t *testing.T) (d *drain, s *scripted, q dqueue.Queue, advance func(time.Duration)) {
	t.Helper()
	d = newDrain()
	now := time.Unix(1_700_000_000, 0)
	d.now = func() time.Time { return now }
	s = &scripted{deliver: make(chan dqueue.Item, 1)}
	return d, s, d.wrap(s), func(by time.Duration) { now = now.Add(by) }
}

// handedBack delivers a run again and checks the gate hands it back and
// goes on holding the first attempt.
func handedBack(t *testing.T, d *drain, s *scripted, q dqueue.Queue, it dqueue.Item) {
	t.Helper()
	s.deliver <- it
	if got, err := claimWithin(t, q, 50*time.Millisecond); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("claim of attempt %d = %+v, %v; want it handed back", it.Attempt, got, err)
	}
	if n := len(s.released); n == 0 || s.released[n-1] != it {
		t.Fatalf("handed back %+v, want attempt %d last", s.released, it.Attempt)
	}
	if n := d.inFlight(); n != 1 {
		t.Fatalf("in flight = %d, want 1", n)
	}
}

// taken delivers a run again and checks the gate takes it this time, holds
// it as the one claim on the run, and is not let go of by the first attempt
// ending late.
func taken(t *testing.T, d *drain, s *scripted, q dqueue.Queue, first, it dqueue.Item) {
	t.Helper()
	handed := len(s.released)
	s.deliver <- it
	got, err := claimWithin(t, q, time.Second)
	if err != nil || got != it {
		t.Fatalf("claim of attempt %d = %+v, %v; want it taken", it.Attempt, got, err)
	}
	if len(s.released) != handed {
		t.Fatalf("attempt %d was handed back: %+v", it.Attempt, s.released)
	}
	if n := d.inFlight(); n != 1 {
		t.Fatalf("in flight = %d, want 1", n)
	}
	// The first attempt ends at last. Its claim is not the one held now.
	_ = q.Settle(t.Context(), first)
	if n := d.inFlight(); n != 1 {
		t.Fatalf("in flight = %d after the first attempt ended late, want 1: it let go of the new attempt's hold", n)
	}
	_ = q.Settle(t.Context(), it)
	if n := d.inFlight(); n != 0 {
		t.Fatalf("in flight = %d after the new attempt ended, want 0", n)
	}
}

// The gate waits for an attempt that lost its lease only so long. Once the
// attempt's heartbeat has been told the lease is gone, it has heldGrace to
// let go; a worker that never releases or settles such a claim does not keep
// the run from being executed. Until then each delivery is handed back, and
// after it the next one is taken.
func TestTheGateStopsWaitingForAnAttemptToldItsLeaseIsGone(t *testing.T) {
	d, s, q, advance := gateOn(t)
	first := dqueue.Item{RunID: "run", Attempt: 1}
	s.deliver <- first
	if got, err := claimWithin(t, q, time.Second); err != nil || got != first {
		t.Fatalf("first claim = %+v, %v", got, err)
	}
	// Not told yet: handed back however long it has been, short of a lease.
	handedBack(t, d, s, q, dqueue.Item{RunID: "run", Attempt: 2, Failures: 1})
	advance(10 * time.Second)
	handedBack(t, d, s, q, dqueue.Item{RunID: "run", Attempt: 3, Failures: 1})

	if _, err := q.Heartbeat(t.Context(), first, time.Minute); !errors.Is(err, drun.ErrSuperseded) {
		t.Fatalf("heartbeat = %v, want %v", err, drun.ErrSuperseded)
	}
	advance(heldGrace - time.Millisecond)
	handedBack(t, d, s, q, dqueue.Item{RunID: "run", Attempt: 4, Failures: 1})
	// A second heartbeat does not start the grace again.
	_, _ = q.Heartbeat(t.Context(), first, time.Minute)
	advance(time.Millisecond)
	taken(t, d, s, q, first, dqueue.Item{RunID: "run", Attempt: 5, Failures: 1})
}

// An attempt whose heartbeat never says anything is waited for no longer
// than one lease from when its run first came back: a worker renews inside
// its lease, so an attempt silent for a whole one is dead.
func TestTheGateStopsWaitingForAnAttemptWithNoHeartbeat(t *testing.T) {
	d, s, q, advance := gateOn(t)
	first := dqueue.Item{RunID: "run", Attempt: 1}
	s.deliver <- first
	if got, err := claimWithin(t, q, time.Second); err != nil || got != first {
		t.Fatalf("first claim = %+v, %v", got, err)
	}
	// However long the attempt worked before its lease lapsed counts for
	// nothing: the lease is counted from the first delivery after it.
	advance(time.Hour)
	handedBack(t, d, s, q, dqueue.Item{RunID: "run", Attempt: 2, Failures: 1})
	// claimWithin claims on a lease of a minute.
	advance(time.Minute - time.Millisecond)
	handedBack(t, d, s, q, dqueue.Item{RunID: "run", Attempt: 3, Failures: 1})
	advance(time.Millisecond)
	taken(t, d, s, q, first, dqueue.Item{RunID: "run", Attempt: 4, Failures: 1})
}
