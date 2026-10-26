package runs

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/urmzd/fiber/pkg/agentrt/payload"
	"github.com/urmzd/fiber/pkg/envelope"
	"github.com/urmzd/fiber/pkg/runtime"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestUsageRetainsHumanAssistanceAfterResumeAndRestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		x, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		record(t, x, "acme", "alone", "writer", "api", 0, nil)
		end(t, x, "acme", "alone", time.Second, envelope.KindCompleted, payload.Lifecycle{})
		record(t, x, "acme", "asked", "writer", "api", 0, nil)
		end(t, x, "acme", "asked", time.Second, envelope.KindParked, payload.Lifecycle{Question: &payload.Question{}})
		end(t, x, "acme", "asked", 2*time.Second, envelope.KindResumed, payload.Lifecycle{})
		end(t, x, "acme", "asked", 3*time.Second, envelope.KindCompleted, payload.Lifecycle{})
		if err := x.Flush(); err != nil {
			t.Fatal(err)
		}
		x, err = Open(st)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(x.Summarize("acme", t0, t0.Add(time.Hour), time.Hour))
		var got struct{ Total map[string]any }
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		if got.Total["completed_alone"] != float64(1) || got.Total["needed_person"] != float64(1) || got.Total["avg_ms"] != float64(2000) {
			t.Fatalf("assistance history lost: %s", raw)
		}
	})
}

func TestCalledWorkHumanQuestionCountsForItsParentAfterRestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		x, _ := Open(st)
		root := envelope.Tag{ID: "parent", Tenant: "acme", Agent: "lead", Origin: OriginAPI, At: t0}
		id := runtime.RunID("acme", root.ID)
		if err := x.Record(id, root, 1); err != nil {
			t.Fatal(err)
		}
		child, err := call.New(root, call.Request{Key: "child", Agent: "writer", Text: "help", Mode: call.Handoff}, call.State{}, call.Limits{MaxDepth: 3, MaxFanOut: 3}, t0.Add(time.Second))
		if err != nil {
			t.Fatal(err)
		}
		childID := runtime.RunID("acme", child.ID)
		if err := x.Record(childID, child, 1); err != nil {
			t.Fatal(err)
		}
		end(t, x, "acme", childID, 2*time.Second, envelope.KindParked, payload.Lifecycle{Question: &payload.Question{}})
		end(t, x, "acme", childID, 3*time.Second, envelope.KindCompleted, payload.Lifecycle{})
		end(t, x, "acme", id, 4*time.Second, envelope.KindCompleted, payload.Lifecycle{})
		if err := x.Flush(); err != nil {
			t.Fatal(err)
		}
		x, err = Open(st)
		if err != nil {
			t.Fatal(err)
		}
		s := x.Summarize("acme", t0, t0.Add(time.Hour), time.Hour)
		if s.Total.NeededPerson != 2 || s.Total.CompletedAlone != 0 || s.Total.HandoffsOut != 1 {
			t.Fatalf("called work lost from truthful metrics: %+v", s.Total)
		}
		for _, g := range s.ByAgent {
			if g.Name == "lead" && (g.HandoffsOut != 1 || g.NeededPerson != 1) {
				t.Fatalf("parent metrics: %+v", g)
			}
		}
	})
}

func TestFailedAssistanceFlushNeverFabricatesCompletedAloneAfterRecovery(t *testing.T) {
	st := statetest.NewFaulty(state.NewMemory())
	x, _ := Open(st)
	record(t, x, "acme", "recover", "writer", "api", 0, nil)
	end(t, x, "acme", "recover", time.Second, envelope.KindParked, payload.Lifecycle{Question: &payload.Question{}})
	st.Fail(errors.New("write unavailable"))
	if x.Flush() == nil {
		t.Fatal("fault not exercised")
	}
	x, err := Open(st)
	if err != nil {
		t.Fatal(err)
	}
	end(t, x, "acme", "recover", 2*time.Second, envelope.KindCompleted, payload.Lifecycle{})
	s := x.Summarize("acme", t0, t0.Add(time.Hour), time.Hour)
	if s.Total.CompletedAlone != 0 || s.Total.HumanHelpUnknown != 1 {
		t.Fatalf("lost observation became a false claim: %+v", s.Total)
	}
}

func TestSummaryNeverExceedsItsBucketCap(t *testing.T) {
	x := NewIndex()
	for _, input := range []struct {
		since, until time.Time
		bucket       time.Duration
	}{
		{t0.Add(17 * time.Second), t0.Add(30 * 24 * time.Hour), time.Nanosecond},
		{t0.Add(59 * time.Minute), t0.Add(239*time.Hour + 89*time.Minute), time.Hour},
	} {
		s := x.Summarize("acme", input.since, input.until, input.bucket)
		if len(s.Buckets) > MaxBuckets {
			t.Fatalf("%d buckets exceeds %d", len(s.Buckets), MaxBuckets)
		}
	}
}

func TestUsageDoesNotCallRecoveredHistoryUnassisted(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		x, _ := Open(st)
		record(t, x, "acme", "recover", "writer", "api", 0, nil)
		x, _ = Open(st)
		end(t, x, "acme", "recover", time.Second, envelope.KindCompleted, payload.Lifecycle{})
		raw, _ := json.Marshal(x.Summarize("acme", t0, t0.Add(time.Hour), time.Hour))
		var got struct{ Total map[string]any }
		_ = json.Unmarshal(raw, &got)
		if got.Total["completed_alone"] != float64(0) || got.Total["human_help_unknown"] != float64(1) {
			t.Fatalf("recovered history fabricated: %s", raw)
		}
	})
}

func TestSummaryHonorsExactBoundsAndReadableHandoffParents(t *testing.T) {
	x := NewIndex()
	record(t, x, "acme", "before", "writer", "api", 0, nil)
	root := envelope.Tag{ID: "parent", Tenant: "acme", Agent: "lead", Origin: "api", At: t0.Add(time.Minute)}
	id := runtime.RunID("acme", root.ID)
	if err := x.Record(id, root, 1); err != nil {
		t.Fatal(err)
	}
	child, err := call.New(root, call.Request{Key: "child", Agent: "writer", Text: "help", Mode: call.Handoff}, call.State{}, call.Limits{MaxDepth: 3, MaxFanOut: 3}, t0.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := x.Record(runtime.RunID("acme", child.ID), child, 1); err != nil {
		t.Fatal(err)
	}
	s := x.Summarize("acme", t0.Add(time.Minute), t0.Add(3*time.Minute), time.Hour)
	if s.Total.Runs != 2 {
		t.Fatalf("bucket rounding included an earlier run: %d", s.Total.Runs)
	}
	s, err = x.SummarizeWhere("acme", SummaryQuery{Since: t0, Until: t0.Add(time.Hour), Bucket: time.Hour, AllProjects: true, Keep: func(r Run) bool { return r.ID != id }})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(s)
	var got struct{ Total map[string]any }
	_ = json.Unmarshal(raw, &got)
	if got.Total["handoffs_out"] != float64(0) {
		t.Fatalf("hidden parent handoff revealed: %s", raw)
	}
}
