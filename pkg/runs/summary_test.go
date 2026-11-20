package runs

import (
	"strings"
	"testing"
	"time"

	"github.com/urmzd/saige/flow/event"

	"github.com/urmzd/humagen/internal/wire"
)

func threadOf(t *testing.T, x *Index, tenant, id string) ThreadSummary {
	t.Helper()
	for _, s := range x.Threads(tenant, 0) {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("no thread %q", id)
	return ThreadSummary{}
}

// A run parked on a person is counted as waiting until it resumes, and the
// latest thing said is the turn's text until its answer replaces it.
func TestAThreadSaysWhatWaitsAndWhatWasSaidLast(t *testing.T) {
	x := NewIndex()
	record(t, x, "acme", "r1", "lead", "api", 0, nil)
	if s := threadOf(t, x, "acme", "r1"); s.Waiting != 0 || s.Last != "go" || s.LastAgent {
		t.Fatalf("started: %+v", s)
	}
	end(t, x, "acme", "r1", time.Second, event.TypeParked, wire.Lifecycle{Question: &event.Question{}})
	if s := threadOf(t, x, "acme", "r1"); s.Waiting != 1 || s.Active != 1 {
		t.Fatalf("parked: waiting %d, active %d", s.Waiting, s.Active)
	}
	end(t, x, "acme", "r1", 2*time.Second, event.TypeResumed, wire.Lifecycle{})
	if s := threadOf(t, x, "acme", "r1"); s.Waiting != 0 || s.Active != 1 {
		t.Fatalf("resumed: waiting %d, active %d", s.Waiting, s.Active)
	}
	end(t, x, "acme", "r1", 3*time.Second, event.TypeCompleted, wire.Lifecycle{Text: "\n  Renew the lease.\nThen sign."})
	s := threadOf(t, x, "acme", "r1")
	if s.Waiting != 0 || s.Active != 0 || s.Last != "Renew the lease." || s.LastBy != "lead" || !s.LastAgent {
		t.Fatalf("answered: %+v", s)
	}
	// A run parked and then stopped stops waiting.
	record(t, x, "acme", "r2", "lead", "api", 4*time.Second, nil)
	end(t, x, "acme", "r2", 5*time.Second, event.TypeParked, wire.Lifecycle{Question: &event.Question{}})
	end(t, x, "acme", "r2", 6*time.Second, event.TypeFailed, wire.Lifecycle{Error: "x", Terminal: true})
	if s := threadOf(t, x, "acme", "r2"); s.Waiting != 0 || s.Failed != 1 {
		t.Fatalf("failed while parked: %+v", s)
	}
}

func TestFirstLineIsBounded(t *testing.T) {
	long := strings.Repeat("é", MaxLast)
	got := firstLine("\n\n" + long + "\nsecond")
	if len(got) > MaxLast+len("…") || !strings.HasSuffix(got, "…") || !strings.HasPrefix(got, "é") {
		t.Fatalf("got %d bytes: %q", len(got), got)
	}
	if firstLine("  \n\t\n") != "" {
		t.Fatal("blank text has a line")
	}
}
