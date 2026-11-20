package join

import (
	"context"
	"errors"
	"testing"
)

func TestFollowupOutcomesAndFallback(t *testing.T) {
	for _, outcome := range []string{"queue", "replace", "stop", "unsure", ""} {
		t.Run(outcome, func(t *testing.T) {
			got := Classify(context.Background(), FollowupFunc(func(context.Context, string, string) (Followup, error) {
				return Followup{Outcome: outcome, Reason: "reason"}, nil
			}), "old", "new")
			want := outcome
			if want == "unsure" || want == "" {
				want = "queue"
			}
			if got.Outcome != want {
				t.Fatal(got)
			}
		})
	}
	for _, a := range []FollowupAsker{nil, FollowupFunc(func(context.Context, string, string) (Followup, error) {
		return Followup{Outcome: "stop"}, errors.New("offline")
	})} {
		if got := Classify(context.Background(), a, "old", "new"); got.Outcome != "queue" {
			t.Fatal(got)
		}
	}
}

func TestFollowupStrictJSON(t *testing.T) {
	for _, text := range []string{`{"outcome":"replace","reason":"new task"}`, `{"outcome":"stop","reason":"stop"}`} {
		if got := Classify(context.Background(), Model{Provider: reply(text)}, "old", "new"); got.Outcome == "queue" {
			t.Fatal(got)
		}
	}
	for _, text := range []string{`stop`, `{"outcome":"stop"}`, `{"outcome":"stop","reason":"yes","extra":1}`, `{"outcome":"stop","reason":"yes"} {}`} {
		if got := Classify(context.Background(), Model{Provider: reply(text)}, "old", "new"); got.Outcome != "queue" {
			t.Fatal(got)
		}
	}
}
