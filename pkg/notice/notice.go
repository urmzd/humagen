// Package notice tells a store's watchers that something changed, in every
// process on one database.
//
// A Hub keeps this process's watchers, by tenant. Tell reaches them at once
// and, once Relay runs, every other process's too: the notice is sent through
// a Relay (fiber's bus.Notices on Postgres) and each process hands what it
// hears to its own watchers. Without a relay, as in memory, a notice stays in
// the process.
//
// A notice is lossy by design. It says that something changed, carries no
// content, and is dropped rather than wait: by a watcher not keeping up, by a
// full send queue, or by a relay that is down. A watcher fetches what it
// shows again on the next notice.
package notice

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"
)

// Bounds. Every one is enforced here.
const (
	// WatchBuffer is how many notices a watcher may fall behind by before
	// it misses one.
	WatchBuffer = 32
	// MaxPending is how many notices wait to be sent to other processes.
	// One more is dropped, not queued.
	MaxPending = 256
	// SendTimeout bounds one send to the relay.
	SendTimeout = 2 * time.Second
)

// Relay carries a notice to every process on one database.
// fiber's bus.Notices is one.
type Relay interface {
	// Send carries payload, JSON, for tenant on topic to every process
	// listening, this one included.
	Send(ctx context.Context, topic, tenant string, payload []byte) error
	// Listen calls fn with every payload sent on topic until cancel is
	// called. fn must not block.
	Listen(topic string, fn func(tenant string, payload []byte)) (cancel func())
}

// Hub is one store's watchers in this process, and the way its notices reach
// other processes.
type Hub[T any] struct {
	topic  string
	origin string

	mu       sync.Mutex
	watchers map[string]map[int]chan T
	next     int
	out      chan relayed

	dropped atomic.Uint64
}

// relayed is a notice as it travels: the process it came from, so that
// process does not hear its own notice twice, and the notice.
type relayed struct {
	Origin string          `json:"o"`
	Notice json.RawMessage `json:"n"`
	tenant string
}

// New returns a hub whose notices travel on topic, which names the store.
func New[T any](topic string) *Hub[T] {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return &Hub[T]{topic: topic, origin: hex.EncodeToString(b[:]), watchers: map[string]map[int]chan T{}}
}

// Watch reports the notices about a tenant, made by any process the hub
// hears from, until cancel is called.
func (h *Hub[T]) Watch(tenant string) (<-chan T, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.watchers[tenant] == nil {
		h.watchers[tenant] = map[int]chan T{}
	}
	ch := make(chan T, WatchBuffer)
	id := h.next
	h.next++
	h.watchers[tenant][id] = ch
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			h.mu.Lock()
			defer h.mu.Unlock()
			delete(h.watchers[tenant], id)
			if len(h.watchers[tenant]) == 0 {
				delete(h.watchers, tenant)
			}
		})
	}
}

// Tell gives n to this process's watchers of tenant, and queues it for the
// other processes while Relay runs. It never blocks.
func (h *Hub[T]) Tell(tenant string, n T) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.local(tenant, n)
	if h.out == nil {
		return
	}
	body, err := json.Marshal(n)
	if err != nil {
		h.dropped.Add(1)
		return
	}
	select {
	case h.out <- relayed{Origin: h.origin, Notice: body, tenant: tenant}:
	default:
		h.dropped.Add(1)
	}
}

// local gives n to this process's watchers. The caller holds mu.
func (h *Hub[T]) local(tenant string, n T) {
	for _, ch := range h.watchers[tenant] {
		// A watcher that is not keeping up misses a notice and never
		// blocks a change. It fetches again on the next one.
		select {
		case ch <- n:
		default:
		}
	}
}

// Dropped is how many notices this hub could not send to other processes:
// queued past MaxPending, unencodable, or refused by the relay.
func (h *Hub[T]) Dropped() uint64 { return h.dropped.Load() }

// Relay sends this process's notices through r and gives this process's
// watchers what other processes send, until ctx ends. A send that fails is
// dropped; report, if not nil, hears of the first failure after a success.
// One Relay runs at a time.
func (h *Hub[T]) Relay(ctx context.Context, r Relay, report func(error)) error {
	out := make(chan relayed, MaxPending)
	h.mu.Lock()
	h.out = out
	h.mu.Unlock()
	stop := r.Listen(h.topic, h.heard)
	defer func() {
		stop()
		h.mu.Lock()
		h.out = nil
		h.mu.Unlock()
	}()
	failing := false
	for {
		select {
		case <-ctx.Done():
			return nil
		case m := <-out:
			body, err := json.Marshal(m)
			if err == nil {
				sctx, cancel := context.WithTimeout(ctx, SendTimeout)
				err = r.Send(sctx, h.topic, m.tenant, body)
				cancel()
			}
			if err != nil {
				h.dropped.Add(1)
				if !failing && report != nil && ctx.Err() == nil {
					report(err)
				}
				failing = true
				continue
			}
			failing = false
		}
	}
}

// heard gives this process's watchers a notice another process sent.
func (h *Hub[T]) heard(tenant string, payload []byte) {
	var m relayed
	if err := json.Unmarshal(payload, &m); err != nil || m.Origin == h.origin {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.watchers[tenant]) == 0 {
		return
	}
	var n T
	if err := json.Unmarshal(m.Notice, &n); err != nil {
		return
	}
	h.local(tenant, n)
}
