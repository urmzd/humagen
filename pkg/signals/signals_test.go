package signals_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/event"

	"github.com/urmzd/humagen/pkg/feedback"
	"github.com/urmzd/humagen/pkg/signals"
)

var t0 = time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

// ev is one event of run, at t0 plus sec seconds.
func ev(run string, seq uint64, sec int, body event.Body) event.Envelope {
	return event.Envelope{
		ID: fmt.Sprintf("%s-%d", run, seq), Run: run, Seq: seq,
		Type: body.Type(), Time: t0.Add(time.Duration(sec) * time.Second), Data: body,
	}
}

func usd(m int64) *event.Cost { return &event.Cost{Micros: m, Currency: "USD"} }

func TestFoldSumsARunThatRetriedAndWaited(t *testing.T) {
	events := []event.Envelope{
		ev("r1", 1, 0, event.Accepted{Attempt: 1}),
		ev("r1", 2, 1, event.Started{Attempt: 1}),
		ev("r1", 3, 2, event.ToolDone{ID: "a", Name: "search"}),
		ev("r1", 4, 3, event.ToolDone{ID: "b", Name: "shell", Error: "exit 1"}),
		ev("r1", 5, 4, event.Failed{Attempt: 1, Error: "provider unavailable"}),
		ev("r1", 6, 5, event.Started{Attempt: 2}),
		ev("r1", 7, 6, event.Parked{Attempt: 2, Reason: "approval"}),
		ev("r1", 8, 36, event.Resumed{Attempt: 2}),
		ev("r1", 9, 40, event.Completed{Attempt: 2, Text: "done"}),
	}
	got := signals.Fold("r1", events, nil)
	want := signals.Record{
		Run: "r1", LatencyMS: 40_000, WaitingMS: 30_000,
		ToolCalls: 2, ToolErrors: 1, Attempts: 2, Retries: 1,
	}
	if got != want {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestFoldIgnoresOrderOtherRunsAndRedeliveries(t *testing.T) {
	events := []event.Envelope{
		ev("r1", 3, 10, event.Completed{Attempt: 1}),
		ev("r1", 2, 5, event.ToolDone{ID: "a", Error: "boom"}),
		ev("r1", 2, 5, event.ToolDone{ID: "a", Error: "boom"}), // redelivered
		ev("r2", 1, 0, event.ToolDone{ID: "z"}),
		ev("r1", 1, 0, event.Accepted{Attempt: 1}),
	}
	got := signals.Fold("r1", events, nil)
	if got.ToolCalls != 1 || got.ToolErrors != 1 || got.LatencyMS != 10_000 || got.Retries != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestFoldDoesNotInferAnEndingFromSilence(t *testing.T) {
	events := []event.Envelope{
		ev("r1", 1, 0, event.Accepted{Attempt: 1}),
		ev("r1", 2, 1, event.Started{Attempt: 1}),
		ev("r1", 3, 2, event.Parked{Attempt: 1}),
		ev("r1", 4, 9, event.Failed{Attempt: 1, Error: "retrying"}),
	}
	got := signals.Fold("r1", events, nil)
	if got.LatencyMS != 0 || got.WaitingMS != 0 {
		t.Fatalf("a run with no ending event has no latency: %+v", got)
	}
	events = append(events, ev("r1", 5, 12, event.Failed{Attempt: 1, Error: "gave up", Terminal: true}))
	got = signals.Fold("r1", events, nil)
	if got.LatencyMS != 12_000 || got.WaitingMS != 10_000 {
		t.Fatalf("a terminal failure ends the run: %+v", got)
	}
}

func TestFoldPrefersTheRuntimesUsageToTheTally(t *testing.T) {
	calls := []event.Envelope{
		ev("r1", 1, 0, event.UsageCall{AccountingID: "c1", PromptTokens: 100, CompletionTokens: 20, Cost: usd(300)}),
		ev("r1", 2, 1, event.UsageCall{AccountingID: "c1", PromptTokens: 100, CompletionTokens: 20, Cost: usd(300)}),
		ev("r1", 3, 2, event.UsageCall{AccountingID: "c2", PromptTokens: 50, CompletionTokens: 5}),
		ev("r1", 4, 3, event.UsageCall{AccountingID: "c3", Depth: 1, PromptTokens: 999, Cost: usd(999)}),
	}
	got := signals.Fold("r1", calls, nil)
	if got.ModelCalls != 2 || got.Tokens != 175 || got.Spend != 300 || got.UnpricedCalls != 1 {
		t.Fatalf("tallied own calls once each, children left out: %+v", got)
	}

	own := event.Usage{ModelCalls: 3, PromptTokens: 400, CompletionTokens: 40, Cost: usd(1200)}
	done := ev("r1", 5, 4, event.Completed{Attempt: 1, Usage: &event.RunUsage{Own: own, Total: own}})
	got = signals.Fold("r1", append(calls, done), nil)
	if got.ModelCalls != 3 || got.Tokens != 440 || got.Spend != 1200 || got.UnpricedCalls != 0 {
		t.Fatalf("the runtime's record stands: %+v", got)
	}
}

func TestFoldRatings(t *testing.T) {
	rate := func(ratings ...string) []feedback.Record {
		var out []feedback.Record
		for i, r := range ratings {
			out = append(out, feedback.Record{Run: "r1", Subject: fmt.Sprint(i), Rating: r})
		}
		return append(out, feedback.Record{Run: "other", Subject: "x", Rating: "down"})
	}
	for _, tc := range []struct {
		ratings  []string
		up, down int
		want     string
	}{
		{nil, 0, 0, ""},
		{[]string{"up"}, 1, 0, signals.Up},
		{[]string{"down", "down"}, 0, 2, signals.Down},
		{[]string{"up", "down", "up"}, 2, 1, signals.Mixed},
	} {
		got := signals.Fold("r1", nil, rate(tc.ratings...))
		if got.Up != tc.up || got.Down != tc.down || got.Rating != tc.want {
			t.Errorf("%v: got %+v", tc.ratings, got)
		}
	}
}
