package journal

import (
	"github.com/urmzd/humagen/internal/wire"
	"testing"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/event"
)

var t0 = time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)

func ev(t *testing.T, tenant, run string, at int, body event.Body) event.Envelope {
	t.Helper()
	return event.Envelope{Seq: uint64(at), Run: run, Tenant: tenant, Agent: "lead", Version: wire.VersionOf(1), Type: body.Type(), Data: body, Time: t0.Add(time.Duration(at) * time.Second)}
}

func filled(t *testing.T) *Journal {
	j := New(0)
	for _, e := range []event.Envelope{
		ev(t, "acme", "r1", 0, event.Accepted{}),
		ev(t, "acme", "r1", 1, event.Started{Attempt: 1}),
		ev(t, "acme", "r1", 2, event.ToolCall{ID: "t1", Name: "github.comment", Phase: event.PhaseRequested}),
		ev(t, "acme", "r1", 5, event.ToolDone{ID: "t1", Name: "github.comment", Error: "rate limited"}),
		ev(t, "acme", "r1", 6, event.Text{Text: "Posted the comment."}),
		ev(t, "acme", "r1", 8, event.Text{Text: " Done."}),
		ev(t, "acme", "r1", 9, event.Completed{Text: "Posted the comment. Done."}),
		ev(t, "globex", "r9", 3, event.Text{Text: "secret"}),
	} {
		j.Append(e, "slack")
	}
	return j
}

func TestQueryIsNewestFirstAndConfinedToATenant(t *testing.T) {
	j := filled(t)
	got := j.Query(Query{Tenant: "acme"})
	if len(got) != 7 || got[0].Kind != "lifecycle.completed" || got[6].Kind != "lifecycle.accepted" {
		t.Fatalf("got %d entries, first %s", len(got), got[0].Kind)
	}
	for _, e := range got {
		if e.RunID == "r9" || e.Detail == "secret" {
			t.Fatalf("another tenant's event leaked: %+v", e)
		}
	}
	if got := j.Query(Query{}); len(got) != 0 {
		t.Fatalf("a query with no tenant returned %d entries", len(got))
	}
}

func TestQueryNarrows(t *testing.T) {
	j := filled(t)
	for name, tc := range map[string]struct {
		q    Query
		want int
	}{
		"family":   {Query{Tenant: "acme", Kind: "lifecycle"}, 3},
		"exact":    {Query{Tenant: "acme", Kind: "delta.tool.done"}, 1},
		"text":     {Query{Tenant: "acme", Contains: "RATE LIMITED"}, 1},
		"since":    {Query{Tenant: "acme", Since: t0.Add(6 * time.Second)}, 3},
		"until":    {Query{Tenant: "acme", Until: t0.Add(1 * time.Second)}, 2},
		"limit":    {Query{Tenant: "acme", Limit: 2}, 2},
		"no run":   {Query{Tenant: "acme", RunID: "r9"}, 0},
		"no agent": {Query{Tenant: "acme", Agent: "helper"}, 0},
	} {
		if got := j.Query(tc.q); len(got) != tc.want {
			t.Errorf("%s: got %d, want %d", name, len(got), tc.want)
		}
	}
}

func TestTraceRebuildsSpans(t *testing.T) {
	spans := filled(t).Trace("acme", []string{"r1", "r9"})
	by := map[SpanKind]Span{}
	for _, s := range spans {
		if s.RunID != "r1" {
			t.Fatalf("another tenant's run was traced: %+v", s)
		}
		by[s.Kind] = s
	}
	sec := func(s Span) (int, int) { return int(s.Start.Sub(t0).Seconds()), int(s.End.Sub(t0).Seconds()) }
	for kind, want := range map[SpanKind][2]int{
		SpanRun: {0, 9}, SpanAttempt: {1, 9}, SpanTool: {2, 5}, SpanOutput: {6, 8},
	} {
		if a, b := sec(by[kind]); a != want[0] || b != want[1] {
			t.Errorf("%s span = %d..%d, want %d..%d", kind, a, b, want[0], want[1])
		}
	}
	if by[SpanTool].Error != "rate limited" || by[SpanTool].Name != "github.comment" {
		t.Errorf("tool span = %+v", by[SpanTool])
	}
}

func TestARetriedFailureDoesNotEndTheRun(t *testing.T) {
	j := New(0)
	j.Append(ev(t, "acme", "r1", 0, event.Accepted{}), "slack")
	j.Append(ev(t, "acme", "r1", 1, event.Started{Attempt: 1}), "slack")
	j.Append(ev(t, "acme", "r1", 2, event.Failed{Error: "timeout"}), "slack")
	for _, s := range j.Trace("acme", []string{"r1"}) {
		if s.Kind == SpanRun && !s.End.IsZero() {
			t.Fatal("the run span closed on a failure that will be retried")
		}
		if s.Kind == SpanAttempt && s.Error != "timeout" {
			t.Fatalf("attempt = %+v", s)
		}
	}
}

func TestTheRingDropsTheOldest(t *testing.T) {
	j := New(3)
	for i := range 5 {
		j.Append(ev(t, "acme", "r1", i, event.Text{Text: "x"}), "slack")
	}
	got := j.Query(Query{Tenant: "acme"})
	if len(got) != 3 || got[0].Seq != 4 || got[2].Seq != 2 {
		t.Fatalf("got %+v", got)
	}
}

func TestDetailIsClippedOnARuneBoundary(t *testing.T) {
	long := ""
	for range 200 {
		long += "é"
	}
	j := New(0)
	j.Append(ev(t, "acme", "r1", 0, event.Text{Text: long}), "slack")
	d := j.Query(Query{Tenant: "acme"})[0].Detail
	if len(d) > maxDetail+4 || []rune(d)[len([]rune(d))-2] != 'é' {
		t.Fatalf("clipped badly: %d bytes", len(d))
	}
}
