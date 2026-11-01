package notice_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/notice"
)

type note struct {
	ID string `json:"id"`
}

// loopback stands in for a database: every hub listening hears every send,
// the sender's included, as Postgres NOTIFY does.
type loopback struct {
	mu        sync.Mutex
	listeners map[int]func(string, []byte)
	next      int
	fail      error
	block     chan struct{}
}

func (l *loopback) Send(ctx context.Context, topic, tenant string, payload []byte) error {
	if l.block != nil {
		select {
		case <-l.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.fail != nil {
		return l.fail
	}
	for _, fn := range l.listeners {
		fn(tenant, payload)
	}
	return nil
}

func (l *loopback) Listen(topic string, fn func(string, []byte)) func() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.listeners == nil {
		l.listeners = map[int]func(string, []byte){}
	}
	id := l.next
	l.next++
	l.listeners[id] = fn
	return func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		delete(l.listeners, id)
	}
}

func (l *loopback) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.listeners)
}

func relay(t *testing.T, h *notice.Hub[note], r notice.Relay, report func(error)) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = h.Relay(ctx, r, report) }()
	t.Cleanup(func() { cancel(); <-done })
}

func recv(t *testing.T, ch <-chan note) note {
	t.Helper()
	select {
	case n := <-ch:
		return n
	case <-time.After(2 * time.Second):
		t.Fatal("no notice")
		return note{}
	}
}

func quiet(t *testing.T, ch <-chan note) {
	t.Helper()
	select {
	case n := <-ch:
		t.Fatalf("unexpected notice %+v", n)
	case <-time.After(100 * time.Millisecond):
	}
}

func awaitListeners(t *testing.T, l *loopback, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for l.count() != n {
		if time.Now().After(deadline) {
			t.Fatalf("listeners = %d, want %d", l.count(), n)
		}
		time.Sleep(time.Millisecond)
	}
}

// Without a relay a notice reaches this process's watchers of its tenant
// only.
func TestWithoutARelayANoticeStaysInTheProcess(t *testing.T) {
	h := notice.New[note]("t")
	acme, stop := h.Watch("acme")
	defer stop()
	globex, stopG := h.Watch("globex")
	defer stopG()
	h.Tell("acme", note{ID: "1"})
	if got := recv(t, acme); got.ID != "1" {
		t.Fatalf("got %+v", got)
	}
	quiet(t, globex)
	if h.Dropped() != 0 {
		t.Fatalf("dropped %d without a relay", h.Dropped())
	}
}

// Two hubs on one relay: a notice told on one reaches the other's watchers
// of that tenant, and its own watchers once.
func TestANoticeCrossesTheRelayOnce(t *testing.T) {
	l := &loopback{}
	a, b := notice.New[note]("t"), notice.New[note]("t")
	relay(t, a, l, nil)
	relay(t, b, l, nil)
	awaitListeners(t, l, 2)

	onA, stopA := a.Watch("acme")
	defer stopA()
	onB, stopB := b.Watch("acme")
	defer stopB()
	otherTenant, stopO := b.Watch("globex")
	defer stopO()

	a.Tell("acme", note{ID: "1"})
	if got := recv(t, onB); got.ID != "1" {
		t.Fatalf("b got %+v", got)
	}
	if got := recv(t, onA); got.ID != "1" {
		t.Fatalf("a got %+v", got)
	}
	quiet(t, onA)
	quiet(t, otherTenant)
}

// A relay that fails drops the notice, reports once until it works again,
// and never blocks Tell.
func TestAFailingRelayDropsAndReportsOnce(t *testing.T) {
	l := &loopback{fail: errors.New("down")}
	h := notice.New[note]("t")
	var mu sync.Mutex
	var reports int
	relay(t, h, l, func(error) { mu.Lock(); reports++; mu.Unlock() })
	awaitListeners(t, l, 1)
	for range 3 {
		h.Tell("acme", note{ID: "x"})
	}
	deadline := time.Now().Add(2 * time.Second)
	for h.Dropped() < 3 {
		if time.Now().After(deadline) {
			t.Fatalf("dropped = %d, want 3", h.Dropped())
		}
		time.Sleep(time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if reports != 1 {
		t.Fatalf("reports = %d, want 1", reports)
	}
}

// A relay that cannot keep up holds at most MaxPending notices; past that
// Tell drops rather than waits.
func TestTheSendQueueIsBounded(t *testing.T) {
	l := &loopback{block: make(chan struct{})}
	h := notice.New[note]("t")
	relay(t, h, l, nil)
	awaitListeners(t, l, 1)
	extra := 10
	done := make(chan struct{})
	go func() {
		defer close(done)
		// One is taken by the sender, which blocks; MaxPending queue.
		for range notice.MaxPending + 1 + extra {
			h.Tell("acme", note{ID: "x"})
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Tell blocked on a relay that could not keep up")
	}
	if got := h.Dropped(); got < uint64(extra) {
		t.Fatalf("dropped = %d, want at least %d", got, extra)
	}
	close(l.block)
}
