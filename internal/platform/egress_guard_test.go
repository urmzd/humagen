package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/saige/flow/address"
	"github.com/urmzd/saige/flow/deliverystate"
	"github.com/urmzd/saige/flow/event"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/egressguard"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// These tests hold a client's writes to what docs/architecture/egress.md
// says of them, against a fake provider that counts every request: a refused
// write is sent a fixed, small number of times, whatever the flush loop and a
// restart do afterwards.

// fakeProvider is a GitHub or Slack API that counts requests and answers
// writes as its script says.
type fakeProvider struct {
	*httptest.Server
	mu     sync.Mutex
	counts map[string]int
	ids    int
	// refuse, when it returns a status, is the answer to a write.
	refuse func(r *http.Request) (status int, header map[string]string, body string)
}

func newFakeProvider(t *testing.T) *fakeProvider {
	t.Helper()
	f := &fakeProvider{counts: map[string]int{}}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.counts[r.Method+" "+r.URL.Path]++
		f.ids++
		id, refuse := f.ids, f.refuse
		f.mu.Unlock()
		slack := strings.HasPrefix(r.URL.Path, "/api/")
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		if refuse != nil {
			if status, header, body := refuse(r); status != 0 {
				for k, v := range header {
					w.Header().Set(k, v)
				}
				w.WriteHeader(status)
				_, _ = w.Write([]byte(body))
				return
			}
		}
		switch {
		case slack:
			_, _ = fmt.Fprintf(w, `{"ok":true,"ts":"1700000000.%06d","channel":"C02CHANNEL"}`, id)
		case strings.HasSuffix(r.URL.Path, "/reactions"):
			w.WriteHeader(http.StatusCreated)
			_, _ = fmt.Fprintf(w, `{"id":%d,"content":"eyes"}`, id)
		default:
			w.WriteHeader(http.StatusCreated)
			_, _ = fmt.Fprintf(w, `{"id":%d,"html_url":"https://github.com/org/repo/issues/42#issuecomment-%d"}`, id, id)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeProvider) answer(refuse func(*http.Request) (int, map[string]string, string)) {
	f.mu.Lock()
	f.refuse = refuse
	f.mu.Unlock()
}

func (f *fakeProvider) count(key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.counts[key]
}

// writes is every request that was not a read.
func (f *fakeProvider) writes() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for key, c := range f.counts {
		if !strings.HasPrefix(key, "GET ") {
			n += c
		}
	}
	return n
}

// client is an HTTP client that sends every request to the fake, whatever
// host it named.
func (f *fakeProvider) client() *http.Client {
	return &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		to := r.Clone(r.Context())
		to.URL.Scheme, to.URL.Host = "http", strings.TrimPrefix(f.URL, "http://")
		to.Host = ""
		return http.DefaultTransport.RoundTrip(to)
	})}
}

// logBuffer collects the platform's log.
type logBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *logBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

// lines is the log lines that contain every one of parts.
func (l *logBuffer) lines(parts ...string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
next:
	for _, line := range strings.Split(l.b.String(), "\n") {
		for _, part := range parts {
			if !strings.Contains(line, part) {
				continue next
			}
		}
		out = append(out, line)
	}
	return out
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type testClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *testClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	c.at = c.at.Add(d)
	c.mu.Unlock()
}

const (
	permissionDenied = `{"message":"Resource not accessible by integration","documentation_url":"https://docs.github.com/rest/issues/comments#create-an-issue-comment","status":"403"}`
	secondaryLimit   = `{"message":"You have exceeded a secondary rate limit. Please wait a few minutes before you try again.","documentation_url":"https://docs.github.com/rest/overview/rate-limits-for-the-rest-api#about-secondary-rate-limits"}`

	createComment = "POST /repos/org/repo/issues/42/comments"
	addReaction   = "POST /repos/org/repo/issues/42/reactions"
)

var githubTarget = address.MustParse("github://org/repo/issues/42")

// guardedPlatform is a platform whose clients reach their provider through
// the fake, on its clock, with no jitter in its backoff.
func guardedPlatform(t *testing.T, b Backends, f *fakeProvider, clock *testClock, logs ...*logBuffer) *Platform {
	t.Helper()
	cfg := Config{
		Workspace: t.TempDir(), Backends: b, ClientInterval: -1, ClientHTTP: f.client(), Now: clock.now,
		EgressPolicy: egressguard.Policy{Jitter: func() float64 { return 1 }},
	}
	if len(logs) > 0 {
		cfg.Log = slog.New(slog.NewTextHandler(logs[0], nil))
	}
	p, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p
}

func githubClient(reactions bool) clients.Client {
	c := clients.Client{Tenant: "t", ID: "app", Surface: clients.GitHub, Managed: clients.ManagedStatic, Enabled: true,
		Identity: clients.Identity{Owners: []string{"org"}}}
	if reactions {
		c.GitHub = &clients.GitHubOptions{Login: "humagen[bot]"}
	}
	return c
}

// seatOf dials a client's connector, as a start or a reseat does, and records
// the run its events belong to.
func seatOf(t *testing.T, p *Platform, c clients.Client, to address.Address, runIDs ...string) *seat {
	t.Helper()
	for _, run := range runIDs {
		if err := p.Runs.Record(run, wire.NewEvent(wire.Raise{ID: "accepted-" + run, Tenant: c.Tenant, Origin: string(c.Surface), Channel: to.String(), Agent: "reviewer", At: time.Now()}), 1); err != nil {
			t.Fatal(err)
		}
	}
	st, err := p.dial(c, resolved{token: "token", signing: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if st.guard == nil {
		t.Fatal("a client that reaches its provider over HTTP has no egress guard")
	}
	return st
}

// runEvents is a short run as egress sees it: accepted, started, three
// pieces of text, completed.
func runEvents(run string, to address.Address) []event.Envelope {
	at := time.Now()
	e := func(seq uint64, body event.Body) event.Envelope {
		return event.Envelope{Tenant: "t", Run: run, Agent: "reviewer", Channel: to.String(), Seq: seq, Type: body.Type(), Data: body, Time: at}
	}
	return []event.Envelope{
		e(1, event.Accepted{}), e(2, event.Started{}),
		e(3, event.Text{Text: "one "}), e(4, event.Text{Text: "two "}), e(5, event.Text{Text: "three"}),
		e(6, event.Completed{Text: "one two three"}),
	}
}

func deliverAll(t *testing.T, st *seat, to address.Address, events []event.Envelope) {
	t.Helper()
	for _, e := range events {
		if err := st.deliver(t.Context(), to, e); err != nil {
			t.Fatalf("delivering %s: %v", e.Type, err)
		}
	}
}

func flushTimes(t *testing.T, st *seat, n int) {
	t.Helper()
	for range n {
		if err := st.flush(t.Context()); err != nil {
			t.Fatalf("flush: %v", err)
		}
	}
}

func eachBackend(t *testing.T, test func(t *testing.T, open func() Backends)) {
	t.Run("memory", func(t *testing.T) {
		// One set of in-memory stores stands for what a restart finds again.
		shared := Backends{State: state.NewMemory(), DeliveryState: deliverystate.NewMemory()}
		test(t, func() Backends { return shared })
	})
	t.Run("postgres", func(t *testing.T) {
		pools := statetest.Pools(t, 3)
		next := 0
		test(t, func() Backends {
			b, release, err := OpenPostgres(t.Context(), pools[next])
			next++
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = release() })
			return b
		})
	})
}

// The defect: an installation without write permission. Every write of the
// run is answered 403. The run's comment and its reaction are each tried
// once, the refusal is recorded and shown to people once, and nothing the
// flush loop, later events, a second replica or a restart does sends another.
func TestAPermissionRefusalIsSentOncePerMessageAndNeverAgain(t *testing.T) {
	eachBackend(t, func(t *testing.T, open func() Backends) {
		fake := newFakeProvider(t)
		fake.answer(func(*http.Request) (int, map[string]string, string) {
			return http.StatusForbidden, map[string]string{"X-GitHub-Request-Id": "ABCD:1234:5678"}, permissionDenied
		})
		clock := &testClock{at: time.Unix(1_800_000_000, 0)}
		c := githubClient(true)
		pinned := func(when string, wantComments int) {
			t.Helper()
			if comments, reactions, writes := fake.count(createComment), fake.count(addReaction), fake.writes(); comments != wantComments || reactions != 1 || writes != wantComments+1 {
				t.Fatalf("%s: %d comment creates, %d reaction creates, %d writes in all; want %d, 1 and %d", when, comments, reactions, writes, wantComments, wantComments+1)
			}
		}
		pin := func(when string) { t.Helper(); pinned(when, 1) }

		logs := &logBuffer{}
		p := guardedPlatform(t, open(), fake, clock, logs)
		st := seatOf(t, p, c, githubTarget, "run")
		events := runEvents("run", githubTarget)
		// Accepted has nothing to say in a comment yet: its reaction is the
		// run's first write, and the comment's follows with the first text.
		deliverAll(t, st, githubTarget, events[:2])
		pinned("after the run was accepted and started", 0)
		deliverAll(t, st, githubTarget, events[2:])
		pin("after every event of the run")
		// A redelivery of the whole run, as at-least-once delivery allows.
		deliverAll(t, st, githubTarget, events)
		pin("after a redelivery")

		// An hour of the flush loop, one flush a second.
		for range 3600 {
			clock.advance(time.Second)
			flushTimes(t, st, 1)
		}
		pin("after an hour of flushes")

		// The loop itself, on the wall clock, with the seat on its surface.
		p.surfaces["github"].put(st)
		ctx, cancel := context.WithTimeout(t.Context(), 2200*time.Millisecond)
		_ = p.flushConnections(ctx)
		cancel()
		pin("after the platform's own flush loop ran")

		refusals := p.DeliveryFailures("t", "run")
		if len(refusals) != 2 {
			t.Fatalf("recorded refusals: %+v", refusals)
		}
		for _, r := range refusals {
			if r.Status != 403 || r.Message != "Resource not accessible by integration" || r.RequestID != "ABCD:1234:5678" || r.Class != egressguard.Permanent ||
				r.DocumentationURL != "https://docs.github.com/rest/issues/comments#create-an-issue-comment" {
				t.Fatalf("a recorded refusal does not say what GitHub said: %+v", r)
			}
		}
		raised := p.Inbox.OpenOf("t", inbox.KindFailure)
		if len(raised) != 1 || raised[0].Source.Run != "run" || len(raised[0].Parts) != 1 || raised[0].Parts[0].Failure == nil ||
			!strings.Contains(raised[0].Parts[0].Failure.Error, "403 Forbidden: Resource not accessible by integration") {
			t.Fatalf("people were not told once: %+v", raised)
		}

		// The log says what was refused, once per message, in GitHub's words,
		// and nothing retried or gave up around it.
		refused := logs.lines("egress: write refused, delivery of this message has stopped", "client=app", "run=run", "status=403",
			`message="Resource not accessible by integration"`, "request_id=ABCD:1234:5678", "documentation_url=https://docs.github.com/rest/issues/comments")
		if len(refused) != 2 || len(logs.lines("kind=comments")) != 1 || len(logs.lines("kind=reactions")) != 1 {
			t.Fatalf("the log does not name each refused message once:\n%s", strings.Join(logs.lines("egress:"), "\n"))
		}
		if noise := logs.lines("connector flush"); len(noise) != 0 {
			t.Fatalf("the flush loop still logs the refusal: %d lines, first %s", len(noise), noise[0])
		}

		// A restart: a new process on the same stores, with nothing in memory.
		// The permission is still missing.
		restarted := guardedPlatform(t, open(), fake, clock, logs)
		again := seatOf(t, restarted, c, githubTarget)
		for range 60 {
			clock.advance(time.Second)
			flushTimes(t, again, 1)
		}
		pin("after a restart and a minute of flushes")
		deliverAll(t, again, githubTarget, events)
		late := event.Envelope{Tenant: "t", Run: "run", Agent: "reviewer", Channel: githubTarget.String(), Seq: 7, Type: event.TypeText, Data: event.Text{Text: " late"}, Time: time.Now()}
		deliverAll(t, again, githubTarget, []event.Envelope{late})
		flushTimes(t, again, 10)
		pin("after a restart, a redelivery and a late event")
		if raised := restarted.Inbox.OpenOf("t", inbox.KindFailure); len(raised) != 1 {
			t.Fatalf("a restart told people again: %d messages", len(raised))
		}
		if again := logs.lines("egress: write refused"); len(again) != 2 {
			t.Fatalf("a restart logged the refusal again: %d lines", len(again))
		}

		// The permission is granted. The stopped run stays stopped; the next
		// run is delivered.
		fake.answer(nil)
		next := seatOf(t, restarted, c, githubTarget, "next")
		deliverAll(t, next, githubTarget, runEvents("next", githubTarget))
		if got := fake.count(createComment); got != 2 {
			t.Fatalf("the next run's comment: %d creates in all, want 2", got)
		}
	})
}

// A secondary rate limit pauses every write of the client: for at least a
// minute when GitHub names no time, for as long as retry-after says when it
// does, and until x-ratelimit-reset when the quota is spent. Writes resume
// when the pause ends and the message is delivered.
func TestARateLimitPausesEveryWriteUntilTheTimeGitHubGives(t *testing.T) {
	for name, tc := range map[string]struct {
		header func(now time.Time) map[string]string
		status int
		pause  time.Duration
	}{
		"secondary limit without a header waits a minute": {func(time.Time) map[string]string { return nil }, 403, time.Minute},
		"retry-after": {func(time.Time) map[string]string { return map[string]string{"Retry-After": "90"} }, 403, 90 * time.Second},
		"429":         {func(time.Time) map[string]string { return map[string]string{"Retry-After": "30"} }, 429, 30 * time.Second},
		"primary reset": {func(now time.Time) map[string]string {
			return map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": strconv.FormatInt(now.Add(10*time.Minute).Unix(), 10)}
		}, 403, 10 * time.Minute},
	} {
		t.Run(name, func(t *testing.T) {
			fake := newFakeProvider(t)
			clock := &testClock{at: time.Unix(1_800_000_000, 0)}
			limited := true
			var mu sync.Mutex
			fake.answer(func(*http.Request) (int, map[string]string, string) {
				mu.Lock()
				defer mu.Unlock()
				if !limited {
					return 0, nil, ""
				}
				return tc.status, tc.header(clock.now()), secondaryLimit
			})
			logs := &logBuffer{}
			p := guardedPlatform(t, Backends{}, fake, clock, logs)
			st := seatOf(t, p, githubClient(false), githubTarget, "run", "other")
			deliverAll(t, st, githubTarget, runEvents("run", githubTarget))
			// A second run of the same client is held too: the limit is the
			// client's, not the message's.
			deliverAll(t, st, githubTarget, runEvents("other", githubTarget))
			if got := fake.writes(); got != 1 {
				t.Fatalf("writes while limited: %d, want the 1 that was refused", got)
			}
			if h := st.guard.Health(); h.Healthy || h.State != "rate_limited" || !h.Until.Equal(clock.now().Add(tc.pause)) || !strings.Contains(h.Message(), "secondary rate limit") {
				t.Fatalf("health while limited: %+v", h)
			}
			// GitHub would accept writes again, and still none is sent
			// before the pause ends.
			mu.Lock()
			limited = false
			mu.Unlock()
			for elapsed := time.Second; elapsed < tc.pause; elapsed += time.Second {
				clock.advance(time.Second)
				flushTimes(t, st, 1)
			}
			if got := fake.writes(); got != 1 {
				t.Fatalf("writes during the pause: %d, want 1", got)
			}
			clock.advance(time.Second)
			flushTimes(t, st, 1)
			if got, comments := fake.writes(), fake.count(createComment); got != 3 || comments != 3 {
				t.Fatalf("after the pause: %d writes, %d creates; want the refused one and one per run (3)", got, comments)
			}
			if h := st.guard.Health(); !h.Healthy {
				t.Fatalf("health after writes resumed: %+v", h)
			}
			if failures := p.DeliveryFailures("t", "run"); len(failures) != 0 {
				t.Fatalf("a rate limit stopped a delivery: %+v", failures)
			}
			paused := logs.lines("egress: rate limited, every write of this client is paused", "You have exceeded a secondary rate limit", "until="+clock.now().UTC().Format(time.RFC3339))
			if len(paused) != 1 || len(logs.lines("egress: writes resumed")) != 1 || len(logs.lines("egress: write refused")) != 0 {
				t.Fatalf("the log does not say the client was paused once and resumed:\n%s", strings.Join(logs.lines("egress:"), "\n"))
			}
			flushTimes(t, st, 5)
			if got := fake.writes(); got != 3 {
				t.Fatalf("flushes after delivery wrote again: %d", got)
			}
		})
	}
}

// A secondary limit that keeps answering without a time is waited out for
// longer each time: a minute, then two, then four.
func TestARepeatedSecondaryLimitBacksOffExponentially(t *testing.T) {
	fake := newFakeProvider(t)
	clock := &testClock{at: time.Unix(1_800_000_000, 0)}
	fake.answer(func(*http.Request) (int, map[string]string, string) { return 403, nil, secondaryLimit })
	p := guardedPlatform(t, Backends{}, fake, clock)
	st := seatOf(t, p, githubClient(false), githubTarget, "run")
	deliverAll(t, st, githubTarget, runEvents("run", githubTarget))
	want := 1
	for _, pause := range []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute} {
		if h := st.guard.Health(); !h.Until.Equal(clock.now().Add(pause)) {
			t.Fatalf("pause %v: writes are held until %v, now %v", pause, h.Until, clock.now())
		}
		for elapsed := time.Second; elapsed < pause; elapsed += time.Second {
			clock.advance(time.Second)
			flushTimes(t, st, 1)
		}
		if got := fake.writes(); got != want {
			t.Fatalf("during the %v pause: %d writes, want %d", pause, got, want)
		}
		clock.advance(time.Second)
		flushTimes(t, st, 1)
		want++
		if got := fake.writes(); got != want {
			t.Fatalf("after the %v pause: %d writes, want %d", pause, got, want)
		}
	}
}

// A 500 is retried, each time after twice as long, and delivered when the
// provider recovers. One request per attempt, however often the loop runs.
func TestATransientFailureBacksOffAndIsDeliveredOnRecovery(t *testing.T) {
	fake := newFakeProvider(t)
	clock := &testClock{at: time.Unix(1_800_000_000, 0)}
	fake.answer(func(*http.Request) (int, map[string]string, string) {
		return 500, nil, `<html>private-provider-body</html>`
	})
	p := guardedPlatform(t, Backends{}, fake, clock)
	st := seatOf(t, p, githubClient(false), githubTarget, "run")
	start := clock.now()
	deliverAll(t, st, githubTarget, runEvents("run", githubTarget))
	if got := fake.writes(); got != 1 {
		t.Fatalf("writes for a run whose first write failed: %d, want 1", got)
	}
	// With no jitter the message waits 2s, 4s, 8s after each failure; the
	// circuit is kept out of the way by a success-free count below its
	// threshold.
	sent := map[time.Duration]int{}
	for elapsed := time.Second; elapsed <= 14*time.Second; elapsed += time.Second {
		clock.advance(time.Second)
		before := fake.writes()
		flushTimes(t, st, 3)
		if n := fake.writes() - before; n > 0 {
			sent[clock.now().Sub(start)] = n
		}
	}
	if len(sent) != 3 || sent[2*time.Second] != 1 || sent[6*time.Second] != 1 || sent[14*time.Second] != 1 {
		t.Fatalf("retries were sent at %v, want one each at 2s, 6s and 14s", sent)
	}
	fake.answer(nil)
	clock.advance(16 * time.Second)
	flushTimes(t, st, 3)
	if writes, comments := fake.writes(), fake.count(createComment); writes != 5 || comments != 5 {
		t.Fatalf("after recovery: %d writes, want the 4 that failed and the 1 that was accepted", writes)
	}
	if failures := p.DeliveryFailures("t", "run"); len(failures) != 0 {
		t.Fatalf("a transient failure stopped a delivery: %+v", failures)
	}
	flushTimes(t, st, 5)
	if got := fake.writes(); got != 5 {
		t.Fatalf("flushes after delivery wrote again: %d", got)
	}
}

// Five refused writes in a row open the client's circuit: nothing is written
// for a minute, then one write probes, and its refusal keeps the circuit
// open for twice as long. The log and the client's health both say so.
func TestConsecutiveRefusalsOpenTheClientsCircuit(t *testing.T) {
	fake := newFakeProvider(t)
	clock := &testClock{at: time.Unix(1_800_000_000, 0)}
	fake.answer(func(*http.Request) (int, map[string]string, string) { return 403, nil, permissionDenied })
	logs := &logBuffer{}
	p := guardedPlatform(t, Backends{State: state.NewMemory()}, fake, clock, logs)
	c := githubClient(false)
	c.Name = "GitHub"
	created, err := p.Clients.Create("t", c, clients.DefaultCaps(), clock.now())
	if err != nil {
		t.Fatal(err)
	}
	runIDs := []string{"r1", "r2", "r3", "r4", "r5", "r6", "r7", "r8"}
	st := seatOf(t, p, created, githubTarget, runIDs...)
	for _, run := range runIDs {
		deliverAll(t, st, githubTarget, runEvents(run, githubTarget))
	}
	if got := fake.writes(); got != egressguard.DefaultBreakerThreshold {
		t.Fatalf("writes before the circuit opened: %d, want %d", got, egressguard.DefaultBreakerThreshold)
	}
	h := st.guard.Health()
	if h.Healthy || h.State != "circuit_open" || !h.Until.Equal(clock.now().Add(time.Minute)) || h.Consecutive != 5 {
		t.Fatalf("health with the circuit open: %+v", h)
	}
	who := auth.Principal{Tenant: "t", Subject: "owner", Kind: auth.User, Roles: []auth.Role{auth.Admin}}
	kept, err := p.lastConnectionHealth(who, created.ID, true)
	if err != nil || kept == nil || kept.Healthy || !strings.Contains(kept.Message, "refused 5 writes in a row") || !strings.Contains(kept.Message, "Resource not accessible by integration") {
		t.Fatalf("the client's health does not say its circuit is open: %+v, %v", kept, err)
	}
	if opened := logs.lines("egress: circuit open, every write of this client is paused", "consecutive=5", "Resource not accessible by integration"); len(opened) != 1 {
		t.Fatalf("the log does not say the circuit opened:\n%s", strings.Join(logs.lines("egress:"), "\n"))
	}
	for range 59 {
		clock.advance(time.Second)
		flushTimes(t, st, 1)
	}
	if got := fake.writes(); got != 5 {
		t.Fatalf("writes while the circuit was open: %d, want 5", got)
	}
	// The minute is up: one write probes, is refused, and the circuit stays
	// open for two minutes.
	clock.advance(time.Second)
	flushTimes(t, st, 5)
	if got := fake.writes(); got != 6 {
		t.Fatalf("writes after the first cooling period: %d, want one probe (6)", got)
	}
	if h := st.guard.Health(); h.State != "circuit_open" || !h.Until.Equal(clock.now().Add(2*time.Minute)) {
		t.Fatalf("health after a refused probe: %+v", h)
	}
	// The permission is granted. The next probe is accepted and the circuit
	// closes.
	fake.answer(nil)
	clock.advance(2 * time.Minute)
	flushTimes(t, st, 1)
	if h := st.guard.Health(); !h.Healthy {
		t.Fatalf("health after an accepted probe: %+v", h)
	}
	if kept, err := p.lastConnectionHealth(who, created.ID, true); err != nil || kept == nil || !kept.Healthy {
		t.Fatalf("the client's health after writes resumed: %+v, %v", kept, err)
	}
}

// Slack reports a refusal as HTTP 200 with ok:false. The same loop flushes
// its messages, and the same guard stops one Slack will never take.
func TestASlackRefusalIsSentOnceAndNeverAgain(t *testing.T) {
	eachBackend(t, func(t *testing.T, open func() Backends) {
		fake := newFakeProvider(t)
		fake.answer(func(*http.Request) (int, map[string]string, string) {
			return http.StatusOK, nil, `{"ok":false,"error":"not_in_channel","private":"private-provider-body"}`
		})
		clock := &testClock{at: time.Unix(1_800_000_000, 0)}
		c := clients.Client{Tenant: "t", ID: "app", Surface: clients.Slack, Managed: clients.ManagedStatic, Enabled: true, Identity: clients.Identity{Team: "T01ABCDEF"}}
		to := address.MustParse("slack://T01ABCDEF/C02CHANNEL?thread=1700000000.000100")
		p := guardedPlatform(t, open(), fake, clock)
		st := seatOf(t, p, c, to, "run")
		deliverAll(t, st, to, runEvents("run", to))
		for range 120 {
			clock.advance(time.Second)
			flushTimes(t, st, 1)
		}
		restarted := guardedPlatform(t, open(), fake, clock)
		again := seatOf(t, restarted, c, to)
		deliverAll(t, again, to, runEvents("run", to))
		flushTimes(t, again, 10)
		posts := fake.count("POST /api/chat.postMessage")
		if posts != 1 {
			t.Fatalf("chat.postMessage was sent %d times, want 1", posts)
		}
		refusals := restarted.DeliveryFailures("t", "run")
		if len(refusals) == 0 || refusals[0].Message != "not_in_channel" {
			t.Fatalf("recorded refusals: %+v", refusals)
		}
		raw, _ := json.Marshal(refusals)
		if strings.Contains(string(raw), "private-provider-body") {
			t.Fatalf("a refusal kept more of Slack's body than its code: %s", raw)
		}
		if raised := restarted.Inbox.OpenOf("t", inbox.KindFailure); len(raised) != 1 {
			t.Fatalf("people were told %d times, want 1", len(raised))
		}
	})
}
