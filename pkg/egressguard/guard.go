package egressguard

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/urmzd/orchagent/flow/deliverystate"
)

// Policy is the guard's numbers. A zero field takes its default.
type Policy struct {
	// BreakerThreshold is how many refused writes in a row open the circuit.
	BreakerThreshold int
	// BreakerCooling is how long the circuit stays open the first time, and
	// BreakerMaxCooling the most it grows to as probes keep failing.
	BreakerCooling    time.Duration
	BreakerMaxCooling time.Duration
	// RateLimitFloor is the pause for a rate limit that names no time. It
	// doubles for each one in a row, up to RateLimitMax, which also bounds a
	// time the provider did name.
	RateLimitFloor time.Duration
	RateLimitMax   time.Duration
	// BackoffBase and BackoffMax bound the wait after a transient failure,
	// which doubles per failure in a row.
	BackoffBase time.Duration
	BackoffMax  time.Duration
	// MaxAttempts is how many transient failures in a row one message takes
	// before its delivery is stopped.
	MaxAttempts int
	// Jitter returns a number in [0, 1). Nil means a random one.
	Jitter func() float64
}

// Defaults follow GitHub's guidance for its secondary rate limits: wait at
// least a minute when no header says how long, and longer each time after.
const (
	DefaultBreakerThreshold  = 5
	DefaultBreakerCooling    = time.Minute
	DefaultBreakerMaxCooling = 15 * time.Minute
	DefaultRateLimitFloor    = time.Minute
	DefaultRateLimitMax      = time.Hour
	DefaultBackoffBase       = 2 * time.Second
	DefaultBackoffMax        = 5 * time.Minute
	DefaultMaxAttempts       = 20
)

func (p Policy) withDefaults() Policy {
	if p.BreakerThreshold <= 0 {
		p.BreakerThreshold = DefaultBreakerThreshold
	}
	if p.BreakerCooling <= 0 {
		p.BreakerCooling = DefaultBreakerCooling
	}
	if p.BreakerMaxCooling < p.BreakerCooling {
		p.BreakerMaxCooling = max(DefaultBreakerMaxCooling, p.BreakerCooling)
	}
	if p.RateLimitFloor <= 0 {
		p.RateLimitFloor = DefaultRateLimitFloor
	}
	if p.RateLimitMax < p.RateLimitFloor {
		p.RateLimitMax = max(DefaultRateLimitMax, p.RateLimitFloor)
	}
	if p.BackoffBase <= 0 {
		p.BackoffBase = DefaultBackoffBase
	}
	if p.BackoffMax < p.BackoffBase {
		p.BackoffMax = max(DefaultBackoffMax, p.BackoffBase)
	}
	if p.MaxAttempts <= 0 {
		p.MaxAttempts = DefaultMaxAttempts
	}
	if p.Jitter == nil {
		p.Jitter = rand.Float64
	}
	return p
}

// Failures keeps the messages whose delivery has stopped, where every replica
// and a restarted one find them.
type Failures interface {
	Load(deliverystate.Key) (Refusal, bool, error)
	// Save records a stopped message and reports whether it was new.
	Save(deliverystate.Key, Refusal) (bool, error)
}

// Config is what a guard is built from.
type Config struct {
	Policy Policy
	// Clock is the time source. Nil means time.Now.
	Clock func() time.Time
	// Log receives one line per decision, with Attrs on each.
	Log   *slog.Logger
	Attrs []any
	// Failures keeps stopped messages. Nil keeps them in this guard only.
	Failures Failures
	// OnStopped is called once when a message's delivery stops, after it was
	// recorded.
	OnStopped func(context.Context, deliverystate.Key, Refusal)
	// OnHealth is called when writes are held and when they resume.
	OnHealth func(Health)
}

// Guard holds one client's writes to one surface to the policy.
type Guard struct {
	cfg      Config
	policy   Policy
	now      func() time.Time
	log      *slog.Logger
	failures Failures

	mu sync.Mutex
	// pausedUntil is the end of a rate-limit pause; strikes counts the rate
	// limits since the last accepted write.
	pausedUntil time.Time
	strikes     int
	lastRate    *Refusal
	// consecutive counts refused writes since the last accepted one.
	consecutive int
	// open is set from the circuit opening until a write is accepted again.
	open      bool
	openUntil time.Time
	cooling   time.Duration
	probing   bool
	lastOpen  *Refusal
	// retryAt holds the client's writes after a transient failure.
	retryAt time.Time
	streak  int
	// messages is the per-message state, bounded.
	messages map[string]*message
}

type message struct {
	// stopped is known: true from a refusal or a record, false from a
	// lookup that found none, good until checked.
	stopped  bool
	checked  time.Time
	known    bool
	failures int
	// notBefore holds the message after a transient failure.
	notBefore time.Time
	last      *Refusal
	touched   time.Time
}

const (
	maxMessages = 8192
	// recheck is how long a "not stopped" answer from the record is believed,
	// so one replica learns what another recorded.
	recheck = 15 * time.Second
)

// New returns a guard.
func New(cfg Config) *Guard {
	g := &Guard{cfg: cfg, policy: cfg.Policy.withDefaults(), now: cfg.Clock, log: cfg.Log, failures: cfg.Failures, messages: map[string]*message{}}
	if g.now == nil {
		g.now = time.Now
	}
	if g.log == nil {
		g.log = slog.New(slog.DiscardHandler)
	}
	if g.failures == nil {
		g.failures = NewMemoryFailures()
	}
	return g
}

// Health reports whether the client's writes are being held.
func (g *Guard) Health() Health {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.health(g.now())
}

func (g *Guard) health(now time.Time) Health {
	switch {
	case now.Before(g.pausedUntil):
		return Health{State: "rate_limited", Until: g.pausedUntil, Consecutive: g.consecutive, Last: g.lastRate}
	case g.open:
		return Health{State: "circuit_open", Until: g.openUntil, Consecutive: g.consecutive, Last: g.lastOpen}
	}
	return Health{Healthy: true, State: "ok"}
}

// Held reports whether no write of the client would be sent now, and until
// when. A caller that only writes can skip its work.
func (g *Guard) Held() (time.Time, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	switch {
	case now.Before(g.pausedUntil):
		return g.pausedUntil, true
	case g.open && now.Before(g.openUntil):
		return g.openUntil, true
	case g.open && g.probing:
		return now.Add(time.Second), true
	case now.Before(g.retryAt):
		return g.retryAt, true
	}
	return time.Time{}, false
}

func keyID(k deliverystate.Key) string { b, _ := json.Marshal(k); return string(b) }

// entry returns a message's state, making room when the table is full. The
// caller holds g.mu.
func (g *Guard) entry(id string, now time.Time) *message {
	m := g.messages[id]
	if m == nil {
		if len(g.messages) >= maxMessages {
			g.evict()
		}
		m = &message{}
		g.messages[id] = m
	}
	m.touched = now
	return m
}

// evict drops the older half of the table. A dropped stopped message is read
// back from the record; a dropped backoff starts again from its base.
func (g *Guard) evict() {
	var oldest, newest time.Time
	for _, m := range g.messages {
		if oldest.IsZero() || m.touched.Before(oldest) {
			oldest = m.touched
		}
		if m.touched.After(newest) {
			newest = m.touched
		}
	}
	mid := oldest.Add(newest.Sub(oldest) / 2)
	for id, m := range g.messages {
		if !m.touched.After(mid) {
			delete(g.messages, id)
		}
	}
}

// admit decides whether a call is sent. probe is set when it is the one call
// let through an open circuit.
func (g *Guard) admit(provider string, write bool, key *deliverystate.Key) (blocked *Blocked, probe bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	if key != nil {
		m := g.entry(keyID(*key), now)
		if m.stopped {
			return &Blocked{Provider: provider, Reason: "delivery of this message has stopped", Last: m.last}, false
		}
		if now.Before(m.notBefore) {
			return &Blocked{Provider: provider, Reason: "this message is backing off", Until: m.notBefore, Last: m.last}, false
		}
	}
	if !write {
		return nil, false
	}
	switch {
	case now.Before(g.pausedUntil):
		return &Blocked{Provider: provider, Reason: "writes are paused by a rate limit", Until: g.pausedUntil, Last: g.lastRate}, false
	case g.open && now.Before(g.openUntil):
		return &Blocked{Provider: provider, Reason: "writes are paused by an open circuit", Until: g.openUntil, Last: g.lastOpen}, false
	case g.open && g.probing:
		return &Blocked{Provider: provider, Reason: "writes are paused while one probes an open circuit", Until: now.Add(time.Second), Last: g.lastOpen}, false
	case now.Before(g.retryAt):
		return &Blocked{Provider: provider, Reason: "writes are backing off", Until: g.retryAt}, false
	}
	if g.open {
		g.probing = true
		return nil, true
	}
	return nil, false
}

// backoff is base doubled n-1 times, capped, with the upper half jittered.
func (g *Guard) backoff(base, limit time.Duration, n int) time.Duration {
	d := base
	for i := 1; i < n && d < limit; i++ {
		d *= 2
	}
	d = min(d, limit)
	return d/2 + time.Duration(g.policy.Jitter()*float64(d/2))
}

// release gives back a probe whose call ended without an answer to judge.
func (g *Guard) release(probe bool) {
	if !probe {
		return
	}
	g.mu.Lock()
	g.probing = false
	g.mu.Unlock()
}

// record applies a call's outcome. It returns the refusal a message's
// delivery stopped with, when this call stopped it.
func (g *Guard) record(write bool, key *deliverystate.Key, probe bool, r *Refusal) (stopped *Refusal) {
	g.mu.Lock()
	now := g.now()
	var m *message
	if key != nil {
		m = g.entry(keyID(*key), now)
	}
	var lines []func()
	var changed *Health
	note := func(h Health) { changed = &h }
	if probe {
		g.probing = false
	}
	switch {
	case r == nil:
		if m != nil {
			m.failures, m.notBefore, m.last = 0, time.Time{}, nil
		}
		if write {
			wasHeld := g.open || g.strikes > 0
			g.consecutive, g.streak, g.strikes, g.retryAt = 0, 0, 0, time.Time{}
			g.open, g.cooling, g.lastOpen, g.lastRate = false, 0, nil, nil
			if wasHeld {
				lines = append(lines, func() { g.log.Info("egress: writes resumed", g.cfg.Attrs...) })
				note(g.health(now))
			}
		}
	case r.Class == RateLimited:
		g.strikes++
		until := r.Until
		if until.IsZero() {
			wait := g.policy.RateLimitFloor
			for i := 1; i < g.strikes && wait < g.policy.RateLimitMax; i++ {
				wait *= 2
			}
			until = now.Add(min(wait, g.policy.RateLimitMax))
		}
		if limit := now.Add(g.policy.RateLimitMax); until.After(limit) {
			until = limit
		}
		if floor := now.Add(time.Second); until.Before(floor) {
			until = floor
		}
		if until.After(g.pausedUntil) {
			g.pausedUntil = until
		}
		kept := *r
		g.lastRate = &kept
		if m != nil {
			m.last = &kept
		}
		paused := g.pausedUntil
		lines = append(lines, func() {
			g.log.Warn("egress: rate limited, every write of this client is paused", append(append([]any{}, g.cfg.Attrs...), append(kept.Attrs(), "until", paused.UTC().Format(time.RFC3339), "strikes", g.strikes)...)...)
		})
		note(g.health(now))
	default:
		kept := *r
		if write {
			g.consecutive++
		}
		if m != nil {
			m.last = &kept
		}
		// Only a refused write stops a message at once. A read the provider
		// refused may be answered differently next time, so it is retried
		// like a transient failure, and stopped when its attempts run out.
		if r.Class == Transient || !write {
			if write {
				g.streak++
				g.retryAt = now.Add(g.backoff(g.policy.BackoffBase/2, time.Minute, g.streak))
			}
			if m != nil {
				m.failures++
				if m.failures >= g.policy.MaxAttempts {
					kept.Class, kept.Attempts = Exhausted, m.failures
					m.stopped, m.known = true, true
					stopped = &kept
				} else {
					wait := g.backoff(g.policy.BackoffBase, g.policy.BackoffMax, m.failures)
					m.notBefore = now.Add(wait)
					attempt := m.failures
					lines = append(lines, func() {
						g.log.Warn("egress: write failed, backing off", append(append([]any{}, g.cfg.Attrs...), append(kept.Attrs(), "run", key.Run, "target", key.Target, "attempt", attempt, "retry_in", wait.String())...)...)
					})
				}
			}
		} else if m != nil {
			m.stopped, m.known = true, true
			stopped = &kept
		}
		if write && (probe || g.consecutive >= g.policy.BreakerThreshold) {
			if g.cooling == 0 {
				g.cooling = g.policy.BreakerCooling
			} else {
				g.cooling = min(g.cooling*2, g.policy.BreakerMaxCooling)
			}
			g.open, g.openUntil, g.lastOpen = true, now.Add(g.cooling), &kept
			n, until := g.consecutive, g.openUntil
			lines = append(lines, func() {
				g.log.Error("egress: circuit open, every write of this client is paused", append(append([]any{}, g.cfg.Attrs...), append(kept.Attrs(), "consecutive", n, "until", until.UTC().Format(time.RFC3339))...)...)
			})
			note(g.health(now))
		}
	}
	g.mu.Unlock()
	for _, line := range lines {
		line()
	}
	if changed != nil && g.cfg.OnHealth != nil {
		g.cfg.OnHealth(*changed)
	}
	return stopped
}

// stoppedKey reports whether a message's delivery has stopped, from this
// guard's memory or else the record.
func (g *Guard) stoppedKey(k deliverystate.Key) bool {
	id := keyID(k)
	g.mu.Lock()
	now := g.now()
	m := g.entry(id, now)
	if m.stopped || (m.known && now.Before(m.checked.Add(recheck))) {
		stopped := m.stopped
		g.mu.Unlock()
		return stopped
	}
	g.mu.Unlock()
	r, found, err := g.failures.Load(k)
	if err != nil {
		g.log.Warn("egress: stopped deliveries could not be read", append(append([]any{}, g.cfg.Attrs...), "error", err)...)
		return false
	}
	g.mu.Lock()
	m = g.entry(id, now)
	m.known, m.checked = true, now
	if found {
		m.stopped, m.last = true, &r
	}
	g.mu.Unlock()
	return found
}

// backingOff reports whether a message is waiting out a transient failure.
func (g *Guard) backingOff(k deliverystate.Key) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	m := g.messages[keyID(k)]
	return m != nil && g.now().Before(m.notBefore)
}

// stop records that a message's delivery has stopped and tells the host,
// once across replicas.
func (g *Guard) stop(ctx context.Context, k deliverystate.Key, r Refusal) {
	created, err := g.failures.Save(k, r)
	attrs := append(append([]any{}, g.cfg.Attrs...), append(r.Attrs(), "run", k.Run, "target", k.Target, "kind", messageKind(k.Scope), "refusal", r.String())...)
	if err != nil {
		g.log.Error("egress: a stopped delivery could not be recorded, so a restart tries it once more", append(attrs, "error", err)...)
		created = true
	}
	if !created {
		return
	}
	g.log.Error("egress: write refused, delivery of this message has stopped", attrs...)
	if g.cfg.OnStopped != nil {
		g.cfg.OnStopped(ctx, k, r)
	}
}

// messageKind is which of a run's messages a scope holds: "comments",
// "reactions", or "message" for a scope with no prefix.
func messageKind(scope string) string {
	if i := strings.IndexAny(scope, ":["); i > 0 && scope[i] == ':' {
		return scope[:i]
	}
	return "message"
}

// MessageKind is which of a run's messages a delivery key is for.
func MessageKind(k deliverystate.Key) string { return messageKind(k.Scope) }

type transport struct {
	g    *Guard
	base http.RoundTripper
	c    Classifier
}

// Transport returns the round tripper a client's provider calls go through.
// Every call is admitted, sent through base and judged by c.
func (g *Guard) Transport(base http.RoundTripper, c Classifier) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &transport{g: g, base: base, c: c}
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	att := attemptFrom(req.Context())
	key := att.current()
	write := t.c.Write(req)
	blocked, probe := t.g.admit(t.c.Provider(), write, key)
	if blocked != nil {
		att.touch(key)
		return nil, blocked
	}
	resp, err := t.base.RoundTrip(req)
	var refusal *Refusal
	switch {
	case err != nil && errors.Is(req.Context().Err(), context.Canceled):
		t.g.release(probe)
		return nil, err
	case err != nil:
		// The error's text is the transport's and may name anything; only
		// the fact of it is kept.
		refusal = &Refusal{Class: Transient, Provider: t.c.Provider(), Method: req.Method, Path: req.URL.Path, Message: "the request did not complete", At: t.g.now()}
	default:
		refusal = t.c.Classify(req, resp, t.g.now())
	}
	if refusal != nil {
		att.touch(key)
	}
	if stopped := t.g.record(write, key, probe, refusal); stopped != nil {
		att.stop(*key, *stopped)
	}
	return resp, err
}
