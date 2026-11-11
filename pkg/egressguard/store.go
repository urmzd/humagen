package egressguard

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/urmzd/orchagent/flow/deliverystate"
)

// Attempt is one call into a connector: a delivery of one event, or a flush.
// It is how the guard's transport learns which message a provider call is
// for, and how the caller learns which failures the guard has taken care of.
type Attempt struct {
	mu      sync.Mutex
	key     *deliverystate.Key
	touched map[string]bool
	stopped map[string]stoppedMessage
}

type stoppedMessage struct {
	key     deliverystate.Key
	refusal Refusal
}

type attemptKey struct{}

// Begin returns a context for one call into the client's connector.
func (g *Guard) Begin(ctx context.Context) (context.Context, *Attempt) {
	a := &Attempt{touched: map[string]bool{}, stopped: map[string]stoppedMessage{}}
	return context.WithValue(ctx, attemptKey{}, a), a
}

func attemptFrom(ctx context.Context) *Attempt {
	a, _ := ctx.Value(attemptKey{}).(*Attempt)
	return a
}

func (a *Attempt) current() *deliverystate.Key {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.key
}

func (a *Attempt) enter(k *deliverystate.Key) (previous *deliverystate.Key) {
	a.mu.Lock()
	defer a.mu.Unlock()
	previous, a.key = a.key, k
	return previous
}

func (a *Attempt) touch(k *deliverystate.Key) {
	if a == nil || k == nil {
		return
	}
	a.mu.Lock()
	a.touched[keyID(*k)] = true
	a.mu.Unlock()
}

func (a *Attempt) stop(k deliverystate.Key, r Refusal) {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.stopped[keyID(k)] = stoppedMessage{key: k, refusal: r}
	a.mu.Unlock()
}

func (a *Attempt) takeStopped(k deliverystate.Key) (Refusal, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s, ok := a.stopped[keyID(k)]
	delete(a.stopped, keyID(k))
	return s.refusal, ok
}

// Covers reports whether every failure in err is one the guard has taken
// care of: a message it stopped and recorded, or one it left pending to be
// written by a later flush under its backoff. The connector commits an event
// to a message's pending state before it writes, so such an error asks for no
// retry of the call itself. An error with a part the guard never saw (a
// store that failed, an address no client serves) is not covered.
func (a *Attempt) Covers(err error) bool {
	if a == nil || err == nil {
		return false
	}
	a.mu.Lock()
	n := len(a.touched)
	a.mu.Unlock()
	return n > 0 && leaves(err) <= n
}

// leaves counts the errors joined into err.
func leaves(err error) int {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		n := 0
		for _, e := range joined.Unwrap() {
			n += leaves(e)
		}
		return n
	}
	return 1
}

// bookkeeping scopes hold what a connector remembers about feedback, not a
// run's message, and are passed through untouched.
var bookkeeping = []string{"feedback:", "suggestion:", "resolution:"}

func tracked(scope string) bool {
	for _, p := range bookkeeping {
		if strings.HasPrefix(scope, p) {
			return false
		}
	}
	return true
}

type store struct {
	g     *Guard
	inner deliverystate.Store
}

// Store wraps the delivery state a client's connector keeps its messages in.
// The connector lists its pending messages from it and writes each one inside
// an Update, so this is where the guard withholds a stopped message, hides
// one that is backing off, and learns which message a provider call is for.
func (g *Guard) Store(inner deliverystate.Store) deliverystate.Store {
	return &store{g: g, inner: inner}
}

// Update implements deliverystate.Store. A stopped message takes no further
// event and is never written again.
func (s *store) Update(ctx context.Context, k deliverystate.Key, update func([]byte) ([]byte, bool, error)) error {
	if !tracked(k.Scope) {
		return s.inner.Update(ctx, k, update)
	}
	if s.g.stoppedKey(k) {
		return nil
	}
	att := attemptFrom(ctx)
	if att == nil {
		return s.inner.Update(ctx, k, update)
	}
	previous := att.enter(&k)
	err := s.inner.Update(ctx, k, update)
	att.enter(previous)
	if r, ok := att.takeStopped(k); ok {
		s.g.stop(ctx, k, r)
		s.settle(ctx, k)
	}
	return err
}

// settle takes a stopped message off the pending list, keeping its state.
func (s *store) settle(ctx context.Context, k deliverystate.Key) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	err := s.inner.Update(ctx, k, func(data []byte) ([]byte, bool, error) { return data, false, nil })
	if err != nil && !errors.Is(err, context.Canceled) {
		s.g.log.Warn("egress: a stopped message is still listed as pending and is skipped on each flush", append(append([]any{}, s.g.cfg.Attrs...), "run", k.Run, "error", err)...)
	}
}

// Pending implements deliverystate.Store. While the client's writes are held
// it lists nothing, so a flush takes no row lock and sends nothing.
func (s *store) Pending(ctx context.Context, scope string, limit int) ([]deliverystate.Key, error) {
	if _, held := s.g.Held(); held {
		return nil, nil
	}
	keys, err := s.inner.Pending(ctx, scope, limit)
	if err != nil || !tracked(scope) {
		return keys, err
	}
	out := keys[:0]
	for _, k := range keys {
		if s.g.stoppedKey(k) {
			s.settle(ctx, k)
			continue
		}
		if s.g.backingOff(k) {
			continue
		}
		out = append(out, k)
	}
	return out, nil
}

// MemoryFailures keeps stopped messages in one process.
type MemoryFailures struct {
	mu   sync.Mutex
	kept map[string]Refusal
}

// NewMemoryFailures returns an empty record.
func NewMemoryFailures() *MemoryFailures { return &MemoryFailures{kept: map[string]Refusal{}} }

// Load implements Failures.
func (m *MemoryFailures) Load(k deliverystate.Key) (Refusal, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.kept[keyID(k)]
	return r, ok, nil
}

// Save implements Failures.
func (m *MemoryFailures) Save(k deliverystate.Key, r Refusal) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.kept[keyID(k)]; ok {
		return false, nil
	}
	m.kept[keyID(k)] = r
	return true, nil
}
