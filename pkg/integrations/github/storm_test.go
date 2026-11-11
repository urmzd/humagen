package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/egressguard"
	"github.com/urmzd/orchagent/flow/address"
	"github.com/urmzd/orchagent/flow/deliverystate"
	"github.com/urmzd/orchagent/flow/event"
)

// refusingGitHub answers every write with a 403 for a missing permission and
// counts what it was sent.
type refusingGitHub struct {
	*httptest.Server
	mu     sync.Mutex
	writes map[string]int
}

func newRefusingGitHub(t *testing.T) *refusingGitHub {
	t.Helper()
	g := &refusingGitHub{writes: map[string]int{}}
	g.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		g.mu.Lock()
		g.writes[r.Method+" "+r.URL.Path]++
		g.mu.Unlock()
		w.Header().Set("X-GitHub-Request-Id", "ABCD:1234:5678")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"Resource not accessible by integration","documentation_url":"https://docs.github.com/rest/issues/comments#create-an-issue-comment","status":"403"}`))
	}))
	t.Cleanup(g.Close)
	return g
}

func (g *refusingGitHub) count(key string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.writes[key]
}

// The runtime's connector keeps a message pending until a write of it
// succeeds and writes every pending message on each Flush, whatever the
// refusal was. This pins that: it is the defect the egress guard contains,
// and the day the runtime stops a refused message itself this test says the
// guard's part of the work can go (docs/architecture/egress.md).
func TestTheRuntimeConnectorAloneRetriesARefusedWriteOnEveryFlush(t *testing.T) {
	fake := newRefusingGitHub(t)
	c, err := New(Config{API: NewHTTPAPIAt(fake.URL, "token", fake.Client()), Tenant: "t", Interval: -1,
		DeliveryState: deliverystate.NewMemory(), DeliveryScope: "client"})
	if err != nil {
		t.Fatal(err)
	}
	to := address.MustParse("github://org/repo/issues/42")
	e := event.Envelope{Tenant: "t", Run: "run", Agent: "reviewer", Channel: to.String(), Seq: 1, Type: event.TypeText, Data: event.Text{Text: "hello"}, Time: time.Now()}
	if err := c.Deliver(t.Context(), to, e); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("delivery of a refused write: %v", err)
	}
	const flushes = 10
	for range flushes {
		if err := c.Flush(t.Context()); err == nil {
			t.Fatal("a flush of a refused message reported success")
		}
	}
	if got := fake.count("POST /repos/org/repo/issues/42/comments"); got != 1+flushes {
		t.Fatalf("the unguarded connector sent %d creates, want one per delivery and flush (%d)", got, 1+flushes)
	}
}

// With the guard as its transport and around its delivery state, the same
// connector sends the refused write once.
func TestAGuardedConnectorSendsARefusedWriteOnce(t *testing.T) {
	fake := newRefusingGitHub(t)
	var stopped []egressguard.Refusal
	guard := egressguard.New(egressguard.Config{OnStopped: func(_ context.Context, _ deliverystate.Key, r egressguard.Refusal) { stopped = append(stopped, r) }})
	client := *fake.Client()
	client.Transport = guard.Transport(client.Transport, Classifier{})
	c, err := New(Config{API: NewHTTPAPIAt(fake.URL, "token", &client), Tenant: "t", Interval: -1,
		DeliveryState: guard.Store(deliverystate.NewMemory()), DeliveryScope: "client"})
	if err != nil {
		t.Fatal(err)
	}
	to := address.MustParse("github://org/repo/issues/42")
	for seq := uint64(1); seq <= 5; seq++ {
		ctx, attempt := guard.Begin(t.Context())
		e := event.Envelope{Tenant: "t", Run: "run", Agent: "reviewer", Channel: to.String(), Seq: seq, Type: event.TypeText, Data: event.Text{Text: "hello"}, Time: time.Now()}
		if err := c.Deliver(ctx, to, e); err != nil && !attempt.Covers(err) {
			t.Fatalf("event %d: %v", seq, err)
		}
	}
	for range 10 {
		ctx, attempt := guard.Begin(t.Context())
		if err := c.Flush(ctx); err != nil && !attempt.Covers(err) {
			t.Fatal(err)
		}
	}
	if got := fake.count("POST /repos/org/repo/issues/42/comments"); got != 1 {
		t.Fatalf("the guarded connector sent %d creates, want 1", got)
	}
	if len(stopped) != 1 || stopped[0].Status != 403 || stopped[0].Message != "Resource not accessible by integration" || stopped[0].RequestID != "ABCD:1234:5678" || stopped[0].Class != egressguard.Permanent {
		t.Fatalf("stopped deliveries: %+v", stopped)
	}
}
