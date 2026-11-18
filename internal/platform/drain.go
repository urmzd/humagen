package platform

import (
	"context"
	"errors"
	"sync"
	"time"

	dqueue "github.com/urmzd/saige/durable/queue"
	drun "github.com/urmzd/saige/durable/run"

	"github.com/urmzd/humagen/pkg/call"
)

// ErrDraining reports that this process is stopping: it starts no new turn,
// so the caller should try again, where a load balancer sends it to another
// replica.
var ErrDraining = errors.New("platform: this server is stopping and starts no new turns; try again")

// drainRetry is how long a run that is not part of work this process holds
// waits, once handed back while draining, before it may be claimed again.
const drainRetry = 250 * time.Millisecond

// heldRetry is how long a run delivered again to this process, while this
// process still executes it, waits before the queue offers it once more.
const heldRetry = time.Second

// heldGrace is how long an attempt whose heartbeat was told its lease is gone
// has to let go of its run before the gate stops waiting for it. The worker
// cancels such an attempt at once, and what is left is to stop the model
// call, record what the attempt spent and say it failed: well under a second
// in the tests here, so five leaves room for a slow database. It is far below
// the 30 second lease, so a run whose attempt never lets go is taken again
// long before it could be mistaken for lost a second time.
const heldGrace = 5 * time.Second

// drainIdlePoll is how often WaitIdle looks at what this process holds.
const drainIdlePoll = 50 * time.Millisecond

// drain is how the platform stops taking work while it lets work in flight
// finish. It stands between anchor's worker and the queue: once draining,
// a claim slot takes only runs that a run this process holds called (so a
// turn waiting on its subagent can still finish), and hands any other run
// straight back for another replica. When the grace is over the platform's
// context is cancelled and the worker releases what it still holds, without
// charging the runs a failure.
type drain struct {
	ctx  context.Context // done once draining
	stop context.CancelFunc

	mu sync.Mutex
	// held is every claim this process holds, by run id.
	held map[string]held
	// now is the clock the holds are timed by.
	now func() time.Time
	// children are runs a held run called, by the caller's run id.
	children map[string]map[string]bool
}

func newDrain() *drain {
	ctx, stop := context.WithCancel(context.Background())
	return &drain{ctx: ctx, stop: stop, held: map[string]held{}, children: map[string]map[string]bool{}, now: time.Now}
}

// held is one claim this process holds.
type held struct {
	// attempt is the claim's fence.
	attempt int
	// again is when the queue first delivered the run again under this
	// claim, which means the claim's lease had lapsed. Zero until then.
	again time.Time
	// lost is when the claim's own heartbeat was told the lease is gone, at
	// which the worker cancels the attempt. Zero until then.
	lost time.Time
}

func (d *drain) draining() bool { return d.ctx.Err() != nil }

func (d *drain) hold(it dqueue.Item) {
	d.mu.Lock()
	d.held[it.RunID] = held{attempt: it.Attempt}
	d.mu.Unlock()
}

// let goes of a claim once the worker settles or releases it, whatever the
// queue answered: the worker holds it no longer either way.
func (d *drain) let(it dqueue.Item) {
	d.mu.Lock()
	if h, ok := d.held[it.RunID]; ok && h.attempt == it.Attempt {
		delete(d.held, it.RunID)
		delete(d.children, it.RunID)
	}
	d.mu.Unlock()
}

// lost notes that a held claim's heartbeat was told its lease is gone.
func (d *drain) lost(it dqueue.Item) {
	d.mu.Lock()
	if h, ok := d.held[it.RunID]; ok && h.attempt == it.Attempt && h.lost.IsZero() {
		h.lost = d.now()
		d.held[it.RunID] = h
	}
	d.mu.Unlock()
}

// forget drops what a run called, if the run is no longer held.
func (d *drain) forget(runID string) {
	d.mu.Lock()
	if _, ok := d.held[runID]; !ok {
		delete(d.children, runID)
	}
	d.mu.Unlock()
}

// holding reports whether a run just delivered, on a lease of ttl, is one
// this process still executes under an earlier claim, so that the delivery is
// to be handed back.
//
// The earlier claim is waited for only while there is reason to think its
// attempt is at work and will let go. Once its heartbeat has been told the
// lease is gone the attempt is cancelled, and it has heldGrace to let go.
// And a claim whose heartbeat has said nothing for a whole lease since the
// run first came back has no heartbeat: a worker renews well inside its
// lease, so by the queue's own rule that attempt is dead. Past either bound
// the hold is dropped and holding reports false, so the gate can never keep
// a run from being executed for good, whatever the worker does with a claim
// it lost.
func (d *drain) holding(it dqueue.Item, ttl time.Duration) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	h, ok := d.held[it.RunID]
	if !ok || h.attempt == it.Attempt {
		return false
	}
	now := d.now()
	if h.again.IsZero() {
		h.again = now
		d.held[it.RunID] = h
	}
	if (!h.lost.IsZero() && now.Sub(h.lost) >= heldGrace) || now.Sub(h.again) >= ttl {
		delete(d.held, it.RunID)
		return false
	}
	return true
}

// called notes a run a run this process holds called.
func (d *drain) called(parent, child string) {
	if parent == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.held[parent]; !ok {
		return
	}
	if d.children[parent] == nil {
		d.children[parent] = map[string]bool{}
	}
	d.children[parent][child] = true
}

// ours reports whether a run is one a held run called.
func (d *drain) ours(runID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, kids := range d.children {
		if kids[runID] {
			return true
		}
	}
	return false
}

// waiting reports whether a held run called a run that is not held yet.
func (d *drain) waiting() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, kids := range d.children {
		for kid := range kids {
			if _, ok := d.held[kid]; !ok {
				return true
			}
		}
	}
	return false
}

func (d *drain) inFlight() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.held)
}

// wrap puts the gate in front of q, keeping the capabilities anchor
// discovers on it by type assertion.
func (d *drain) wrap(q dqueue.Queue) dqueue.Queue {
	g := &gatedQueue{Queue: q, d: d}
	if log, ok := q.(dqueue.DeltaLog); ok {
		return gatedDeltaQueue{gatedQueue: g, DeltaLog: log}
	}
	return g
}

// gatedQueue is a queue whose claims stop when the platform drains.
type gatedQueue struct {
	dqueue.Queue
	d *drain
}

type gatedDeltaQueue struct {
	*gatedQueue
	dqueue.DeltaLog
}

// Claim takes a run, or, once draining, only a run this process's work
// called. A claim slot with nothing of ours to wait for parks until the
// platform stops.
//
// A run this process is still executing is never taken a second time. The
// queue delivers it again when its lease lapses under the attempt at work (a
// heartbeat that came too late, or the database's clock stepping forward past
// the lease). Two attempts of one run cannot execute in one process, so the
// worker would fail the second, and each one after it, until the run had
// spent every attempt it is allowed while the first was still answering. The
// second delivery is handed back instead, charged nothing, and comes back
// every heldRetry until the first attempt lets go: when it ends, or when its
// next heartbeat learns the lease is no longer its own and cancels it. Each
// hand-back is one claim and one release. The lapse itself has already cost
// the run one failure, which the queue charges as it delivers the run again.
//
// The wait is bounded (see holding): heldGrace after the first attempt's
// heartbeat was told its lease is gone, or one lease after the run first
// came back if that heartbeat never speaks. After that the delivery is taken
// whether or not the first attempt let go. If it is somehow still executing,
// the worker fails this delivery and charges it, as it did before the gate
// handed anything back, which is better than a run nobody executes.
func (q *gatedQueue) Claim(ctx context.Context, ttl time.Duration) (dqueue.Item, time.Time, error) {
	for {
		if q.d.draining() && !q.d.waiting() {
			<-ctx.Done()
			return dqueue.Item{}, time.Time{}, ctx.Err()
		}
		claim, cancel := context.WithCancel(ctx)
		// A claim waiting for work stops waiting when the drain starts, so
		// it cannot take a run after the gate closed. While draining, it
		// waits as long as a held run waits on one it called.
		unhook := func() bool { return true }
		if !q.d.draining() {
			unhook = context.AfterFunc(q.d.ctx, cancel)
		}
		it, deadline, err := q.Queue.Claim(claim, ttl)
		unhook()
		cancel()
		if err != nil {
			if ctx.Err() == nil && q.d.draining() {
				continue
			}
			return it, deadline, err
		}
		if q.d.holding(it, ttl) {
			// Handed back on the queue itself: the gate's own Release would
			// let go of nothing here, but this claim was never the gate's.
			_ = q.Queue.Release(context.WithoutCancel(ctx), it, heldRetry, false)
			continue
		}
		if !q.d.draining() || q.d.ours(it.RunID) {
			q.d.hold(it)
			return it, deadline, nil
		}
		// Another replica's work, or work that arrived as the drain began:
		// hand it back unexecuted, charging it nothing.
		// It comes back after a moment, so this slot's next claim can reach
		// the run it waits for rather than take the same one again.
		q.d.forget(it.RunID)
		_ = q.Queue.Release(context.WithoutCancel(ctx), it, drainRetry, false)
	}
}

// Heartbeat implements dqueue.Queue. An attempt told its lease is gone is one
// the gate stops waiting for after heldGrace.
func (q *gatedQueue) Heartbeat(ctx context.Context, it dqueue.Item, ttl time.Duration) (time.Time, error) {
	deadline, err := q.Queue.Heartbeat(ctx, it, ttl)
	if errors.Is(err, drun.ErrSuperseded) {
		q.d.lost(it)
	}
	return deadline, err
}

// Release implements dqueue.Queue.
func (q *gatedQueue) Release(ctx context.Context, it dqueue.Item, delay time.Duration, failed bool) error {
	defer q.d.let(it)
	return q.Queue.Release(ctx, it, delay, failed)
}

// Settle implements dqueue.Queue.
func (q *gatedQueue) Settle(ctx context.Context, it dqueue.Item) error {
	defer q.d.let(it)
	return q.Queue.Settle(ctx, it)
}

// Drain stops this process taking new work: a new turn is refused with
// ErrDraining and the worker claims only runs that work it holds called.
// Runs in flight go on. Cancelling Run's context afterwards releases what is
// still held, so another replica resumes it. Drain is idempotent.
func (p *Platform) Drain() { p.drain.stop() }

// Draining reports whether Drain was called.
func (p *Platform) Draining() bool { return p.drain.draining() }

// InFlight is how many runs this process holds a claim on.
func (p *Platform) InFlight() int { return p.drain.inFlight() }

// WaitIdle returns once this process holds no run, or with ctx's error.
func (p *Platform) WaitIdle(ctx context.Context) error {
	tick := time.NewTicker(drainIdlePoll)
	defer tick.Stop()
	for p.drain.inFlight() > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
	return nil
}

// refuseWhileDraining refuses a new turn, one no run called, while draining.
func (p *Platform) refuseWhileDraining(edge call.Edge) error {
	if p.drain.draining() && edge.IsRoot() {
		return ErrDraining
	}
	return nil
}
