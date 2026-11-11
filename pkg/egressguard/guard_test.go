package egressguard_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/saige/flow/deliverystate"

	"github.com/urmzd/humagen/pkg/egressguard"
	"github.com/urmzd/humagen/pkg/integrations/github"
)

// provider answers each request with the next status of its script, and 201
// once the script is spent.
type provider struct {
	mu     sync.Mutex
	script []int
	sent   []string
	fail   error
}

func (p *provider) RoundTrip(r *http.Request) (*http.Response, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sent = append(p.sent, r.Method)
	if p.fail != nil {
		return nil, p.fail
	}
	status := http.StatusCreated
	if len(p.script) > 0 {
		status, p.script = p.script[0], p.script[1:]
	}
	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"message":"scripted"}`))}, nil
}

func (p *provider) requests() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.sent)
}

type clock struct{ at time.Time }

func (c *clock) now() time.Time { return c.at }

// harness is a guard between a connector-shaped caller and a provider: each
// write of a message happens inside the store's Update, as the runtime's
// connector does it.
type harness struct {
	t       *testing.T
	guard   *egressguard.Guard
	store   deliverystate.Store
	inner   *deliverystate.Memory
	http    *http.Client
	api     *provider
	clock   *clock
	stopped []egressguard.Refusal
	health  []egressguard.Health
}

func newHarness(t *testing.T, policy egressguard.Policy, script ...int) *harness {
	h := &harness{t: t, api: &provider{script: script}, clock: &clock{at: time.Unix(1_800_000_000, 0)}, inner: deliverystate.NewMemory()}
	if policy.Jitter == nil {
		policy.Jitter = func() float64 { return 1 }
	}
	h.guard = egressguard.New(egressguard.Config{Policy: policy, Clock: h.clock.now,
		OnStopped: func(_ context.Context, _ deliverystate.Key, r egressguard.Refusal) { h.stopped = append(h.stopped, r) },
		OnHealth:  func(s egressguard.Health) { h.health = append(h.health, s) },
	})
	h.store = h.guard.Store(h.inner)
	h.http = &http.Client{Transport: h.guard.Transport(h.api, github.Classifier{})}
	return h
}

func key(run string) deliverystate.Key {
	return deliverystate.Key{Tenant: "t", Scope: "comments:client", Run: run, Channel: "github://o/r/issues/1", Target: "github://o/r/issues/1"}
}

// write is one delivery of a message: the connector marks it pending, then
// writes it inside Update. It reports whether a request reached the provider.
func (h *harness) write(k deliverystate.Key, method string) (sent bool, covered bool, err error) {
	h.t.Helper()
	before := h.api.requests()
	ctx, attempt := h.guard.Begin(h.t.Context())
	var writeErr error
	err = h.store.Update(ctx, k, func(data []byte) ([]byte, bool, error) {
		req, _ := http.NewRequestWithContext(ctx, method, "https://api.github.com/repos/o/r/issues/1/comments", nil)
		resp, doErr := h.http.Do(req)
		if doErr != nil {
			writeErr = doErr
			return []byte("pending"), true, nil
		}
		_ = resp.Body.Close()
		if resp.StatusCode > 299 {
			writeErr = fmt.Errorf("status %d", resp.StatusCode)
			return []byte("pending"), true, nil
		}
		return []byte("written"), false, nil
	})
	if err == nil {
		err = writeErr
	}
	return h.api.requests() > before, attempt.Covers(err), err
}

func (h *harness) pending() []deliverystate.Key {
	h.t.Helper()
	keys, err := h.store.Pending(h.t.Context(), "comments:client", 256)
	if err != nil {
		h.t.Fatal(err)
	}
	return keys
}

// A message the provider refuses for good is stopped by its first write:
// it leaves the pending list, takes no further event and sends nothing more.
func TestAPermanentRefusalStopsItsMessage(t *testing.T) {
	h := newHarness(t, egressguard.Policy{}, 403)
	sent, covered, err := h.write(key("run"), http.MethodPost)
	if !sent || !covered || err == nil {
		t.Fatalf("the refused write: sent=%v covered=%v err=%v", sent, covered, err)
	}
	if len(h.stopped) != 1 || h.stopped[0].Class != egressguard.Permanent || h.stopped[0].Status != 403 {
		t.Fatalf("stopped: %+v", h.stopped)
	}
	if got := h.pending(); len(got) != 0 {
		t.Fatalf("a stopped message is still pending: %v", got)
	}
	if inner, _ := h.inner.Pending(t.Context(), "comments:client", 256); len(inner) != 0 {
		t.Fatalf("a stopped message is still pending in the store itself: %v", inner)
	}
	for range 10 {
		if sent, _, err := h.write(key("run"), http.MethodPost); sent || err != nil {
			t.Fatalf("a stopped message was written again: sent=%v err=%v", sent, err)
		}
	}
	if len(h.stopped) != 1 || h.api.requests() != 1 {
		t.Fatalf("stopped %d times with %d requests, want 1 and 1", len(h.stopped), h.api.requests())
	}
	// Another message of the same client is its own.
	if sent, _, err := h.write(key("other"), http.MethodPost); !sent || err != nil {
		t.Fatalf("another run's message: sent=%v err=%v", sent, err)
	}
}

// A guard that starts with nothing in memory, as after a restart, reads the
// stopped message from the record and leaves it alone.
func TestAStoppedMessageStaysStoppedForANewGuard(t *testing.T) {
	failures := egressguard.NewMemoryFailures()
	api := &provider{script: []int{403}}
	inner := deliverystate.NewMemory()
	for round := range 3 {
		guard := egressguard.New(egressguard.Config{Failures: failures})
		store := guard.Store(inner)
		client := &http.Client{Transport: guard.Transport(api, github.Classifier{})}
		ctx, _ := guard.Begin(t.Context())
		_ = store.Update(ctx, key("run"), func([]byte) ([]byte, bool, error) {
			req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.github.com/x", nil)
			if resp, err := client.Do(req); err == nil {
				_ = resp.Body.Close()
			}
			return []byte("pending"), true, nil
		})
		if api.requests() != 1 {
			t.Fatalf("round %d: %d requests, want the 1 that was refused", round, api.requests())
		}
	}
}

// Transient failures of one message wait twice as long each time, up to the
// cap, and stop the message when its attempts run out.
func TestATransientFailureBacksOffWithJitterAndACapThenGivesUp(t *testing.T) {
	policy := egressguard.Policy{BackoffBase: time.Second, BackoffMax: 8 * time.Second, MaxAttempts: 6, BreakerThreshold: 1000}
	h := newHarness(t, policy, 500, 502, 503, 500, 500, 500, 500)
	var waits []time.Duration
	last := h.clock.at
	for range 400 {
		sent, covered, err := h.write(key("run"), http.MethodPost)
		if err != nil && !covered {
			t.Fatalf("a failure the guard handled was not covered: %v", err)
		}
		if sent {
			waits = append(waits, h.clock.at.Sub(last))
			last = h.clock.at
		}
		h.clock.at = h.clock.at.Add(500 * time.Millisecond)
	}
	want := []time.Duration{0, time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 8 * time.Second}
	if fmt.Sprint(waits) != fmt.Sprint(want) {
		t.Fatalf("attempts were sent after %v, want %v", waits, want)
	}
	if len(h.stopped) != 1 || h.stopped[0].Class != egressguard.Exhausted || h.stopped[0].Attempts != 6 {
		t.Fatalf("stopped: %+v", h.stopped)
	}

	// Jitter spreads a wait over the upper half of its window.
	low := newHarness(t, egressguard.Policy{BackoffBase: 4 * time.Second, Jitter: func() float64 { return 0 }}, 500)
	_, _, _ = low.write(key("run"), http.MethodPost)
	low.clock.at = low.clock.at.Add(1999 * time.Millisecond)
	if sent, _, _ := low.write(key("run"), http.MethodPost); sent {
		t.Fatal("a retry was sent before half its backoff")
	}
	low.clock.at = low.clock.at.Add(time.Millisecond)
	if sent, _, _ := low.write(key("run"), http.MethodPost); !sent {
		t.Fatal("with no jitter a retry waits half its backoff, and was not sent then")
	}
}

// A network error is a transient failure too, and its text is not kept.
func TestANetworkErrorIsTransientAndItsTextIsNotKept(t *testing.T) {
	h := newHarness(t, egressguard.Policy{MaxAttempts: 1})
	h.api.fail = errors.New("dial tcp: private-network-detail Bearer ghs_secretsecretsecret")
	if sent, covered, _ := h.write(key("run"), http.MethodPost); !sent || !covered {
		t.Fatalf("sent=%v covered=%v", sent, covered)
	}
	if len(h.stopped) != 1 || strings.Contains(h.stopped[0].Error(), "private") || strings.Contains(h.stopped[0].Error(), "ghs_") {
		t.Fatalf("stopped: %+v", h.stopped)
	}
}

// A read the provider refuses does not stop a message at once, and counts
// for nothing against the client's writes.
func TestARefusedReadIsRetriedAndDoesNotOpenTheCircuit(t *testing.T) {
	h := newHarness(t, egressguard.Policy{BreakerThreshold: 2}, 404, 404, 404)
	for range 3 {
		if _, covered, err := h.write(key("run"), http.MethodGet); err == nil || !covered {
			t.Fatalf("covered=%v err=%v", covered, err)
		}
		h.clock.at = h.clock.at.Add(time.Hour)
	}
	if len(h.stopped) != 0 || !h.guard.Health().Healthy {
		t.Fatalf("stopped %+v, health %+v", h.stopped, h.guard.Health())
	}
}

// The circuit opens after the threshold, lets one probe through when its
// cooling ends, doubles the cooling when the probe is refused, up to its
// cap, and closes when a probe is accepted.
func TestTheCircuitOpensProbesAndCloses(t *testing.T) {
	policy := egressguard.Policy{BreakerThreshold: 3, BreakerCooling: time.Minute, BreakerMaxCooling: 4 * time.Minute}
	h := newHarness(t, policy, 403, 403, 403, 403, 403, 403, 403)
	run := 0
	next := func() (bool, error) {
		run++
		sent, _, err := h.write(key(fmt.Sprint("run-", run)), http.MethodPost)
		return sent, err
	}
	for range 3 {
		if sent, _ := next(); !sent {
			t.Fatal("a write before the threshold was held")
		}
	}
	for _, cooling := range []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 4 * time.Minute} {
		health := h.guard.Health()
		if health.Healthy || health.State != "circuit_open" || !health.Until.Equal(h.clock.at.Add(cooling)) {
			t.Fatalf("cooling %v: health %+v at %v", cooling, health, h.clock.at)
		}
		sent, err := next()
		var blocked *egressguard.Blocked
		if sent || !errors.As(err, &blocked) || !strings.Contains(blocked.Error(), "open circuit") {
			t.Fatalf("a write with the circuit open: sent=%v err=%v", sent, err)
		}
		if until, held := h.guard.Held(); !held || !until.Equal(h.clock.at.Add(cooling)) {
			t.Fatalf("held=%v until %v", held, until)
		}
		h.clock.at = h.clock.at.Add(cooling)
		if sent, _ := next(); !sent {
			t.Fatalf("no probe was sent after %v", cooling)
		}
	}
	// The script is spent: the next probe is accepted.
	h.clock.at = h.clock.at.Add(4 * time.Minute)
	if sent, err := next(); !sent || err != nil {
		t.Fatalf("the accepted probe: sent=%v err=%v", sent, err)
	}
	if !h.guard.Health().Healthy {
		t.Fatalf("health after an accepted probe: %+v", h.guard.Health())
	}
	if last := h.health[len(h.health)-1]; !last.Healthy {
		t.Fatalf("the host was not told writes resumed: %+v", h.health)
	}
	if requests := h.api.requests(); requests != 8 {
		t.Fatalf("%d requests, want 3 before the circuit opened, 4 refused probes and 1 accepted", requests)
	}
}

// While writes are held the store lists nothing pending, so a flush has
// nothing to lock and nothing to send; reads still go out.
func TestHeldWritesListNothingPendingAndLetReadsThrough(t *testing.T) {
	h := newHarness(t, egressguard.Policy{}, 429)
	if _, covered, _ := h.write(key("run"), http.MethodPost); !covered {
		t.Fatal("a rate-limited write was not covered")
	}
	if inner, _ := h.inner.Pending(t.Context(), "comments:client", 256); len(inner) != 1 {
		t.Fatalf("a rate-limited message must stay pending in the store: %v", inner)
	}
	if got := h.pending(); len(got) != 0 {
		t.Fatalf("pending while held: %v", got)
	}
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://api.github.com/repos/o/r", nil)
	resp, err := h.http.Do(req)
	if err != nil {
		t.Fatalf("a read while writes are held: %v", err)
	}
	_ = resp.Body.Close()
	h.clock.at = h.clock.at.Add(time.Minute)
	if got := h.pending(); len(got) != 1 {
		t.Fatalf("pending after the pause: %v", got)
	}
}

// A connector's bookkeeping in the same store is not a message and is
// passed through untouched.
func TestBookkeepingScopesAreNotGuarded(t *testing.T) {
	h := newHarness(t, egressguard.Policy{}, 403)
	k := key("run")
	k.Scope = "feedback:client"
	ctx, _ := h.guard.Begin(t.Context())
	for range 3 {
		calls := 0
		if err := h.store.Update(ctx, k, func([]byte) ([]byte, bool, error) { calls++; return nil, true, nil }); err != nil || calls != 1 {
			t.Fatalf("calls=%d err=%v", calls, err)
		}
	}
}

// An error with a part the guard never saw is not covered: the caller must
// still hear of it.
func TestAnErrorTheGuardNeverSawIsNotCovered(t *testing.T) {
	h := newHarness(t, egressguard.Policy{}, 403)
	_, attempt := h.guard.Begin(t.Context())
	if attempt.Covers(errors.New("store is down")) || attempt.Covers(nil) {
		t.Fatal("an attempt with no refusal covered an error")
	}
	_, covered, err := h.write(key("run"), http.MethodPost)
	if !covered {
		t.Fatal("the refusal alone was not covered")
	}
	ctx, attempt := h.guard.Begin(t.Context())
	_ = h.store.Update(ctx, key("second"), func([]byte) ([]byte, bool, error) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.github.com/x", nil)
		_, _ = h.http.Do(req)
		return nil, true, nil
	})
	if attempt.Covers(errors.Join(err, errors.New("store is down"))) {
		t.Fatal("a refusal joined with another failure was covered")
	}
}
