package join

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// The first word after any leading mentions forces the outcome, without a
// model: for a stop the rest is the reason, for a queue or a replace the
// rest is what runs.
func TestAVerbFirstForcesTheOutcome(t *testing.T) {
	never := FollowupFunc(func(context.Context, string, string) (Followup, error) {
		t.Fatal("a forced outcome asked the model")
		return Followup{}, nil
	})
	for _, tc := range []struct {
		text, outcome, reason, runs string
	}{
		{"queue also check the lint", FollowupQueue, "also check the lint", "also check the lint"},
		{"Queue: also check the lint", FollowupQueue, "also check the lint", "also check the lint"},
		{"replace fix the tests instead", FollowupReplace, "fix the tests instead", "fix the tests instead"},
		{"REPLACE:fix the tests instead", FollowupReplace, "fix the tests instead", "fix the tests instead"},
		{"stop", FollowupStop, "", ""},
		{"stop: wrong repository,\n  sorry", FollowupStop, "wrong repository, sorry", ""},
		{"Stop the deploy is cancelled", FollowupStop, "the deploy is cancelled", ""},
		{"@coder stop: not needed", FollowupStop, "not needed", ""},
		{"@coder @reviewer replace: review PR 12", FollowupReplace, "review PR 12", "@coder @reviewer review PR 12"},
		{"<@U0ANA|ana> queue then deploy", FollowupQueue, "then deploy", "<@U0ANA|ana> then deploy"},
		{"  @coder   queue:   then deploy", FollowupQueue, "then deploy", "@coder then deploy"},
	} {
		got := Classify(context.Background(), never, "build it", tc.text)
		if got.Outcome != tc.outcome || got.Reason != tc.reason || got.Text != tc.runs || !got.Forced {
			t.Errorf("%q = %+v, want %s %q runs %q", tc.text, got, tc.outcome, tc.reason, tc.runs)
		}
	}
}

// Words that are not a verb first are not one: a verb later in the text, a
// longer word, a verb with nothing to run, or a mention alone.
func TestOnlyAVerbFirstForcesAnything(t *testing.T) {
	for _, text := range []string{
		"please stop", "stopping is fine", "stop.", "queue", "replace:", "@coder replace", "@coder", "requeue it", "the stop: here",
	} {
		if f, ok := Forced(text); ok {
			t.Errorf("%q forced %+v", text, f)
		}
		if got := Classify(context.Background(), nil, "build it", text); got.Outcome != FollowupQueue || got.Forced || got.Text != text {
			t.Errorf("%q with no model = %+v", text, got)
		}
	}
}

// The model decides when no verb does, and anything but a clear answer
// queues: an error, an unknown outcome, no reason, a timeout or no model.
func TestTheModelDecidesAndAnythingUnclearQueues(t *testing.T) {
	answer := func(f Followup, err error) FollowupAsker {
		return FollowupFunc(func(context.Context, string, string) (Followup, error) { return f, err })
	}
	for _, tc := range []struct {
		name string
		a    FollowupAsker
		want string
	}{
		{"queue", answer(Followup{Outcome: FollowupQueue, Reason: "Adds a detail."}, nil), FollowupQueue},
		{"replace", answer(Followup{Outcome: FollowupReplace, Reason: "A new task."}, nil), FollowupReplace},
		{"stop", answer(Followup{Outcome: FollowupStop, Reason: "Asks to stop."}, nil), FollowupStop},
		{"unknown outcome", answer(Followup{Outcome: "maybe", Reason: "Unsure."}, nil), FollowupQueue},
		{"no reason", answer(Followup{Outcome: FollowupStop, Reason: "  "}, nil), FollowupQueue},
		{"error", answer(Followup{Outcome: FollowupStop, Reason: "Asks to stop."}, errors.New("offline")), FollowupQueue},
		{"nil asker", nil, FollowupQueue},
	} {
		got := Classify(context.Background(), tc.a, "build it", "never mind, it can wait")
		if got.Outcome != tc.want || got.Forced || got.Text != "never mind, it can wait" || got.Reason == "" {
			t.Errorf("%s = %+v, want %s", tc.name, got, tc.want)
		}
	}
}

// An asker that does not answer in time queues, whether or not it heeds its
// context.
func TestASlowModelQueues(t *testing.T) {
	was := followupTimeout
	followupTimeout = 20 * time.Millisecond
	t.Cleanup(func() { followupTimeout = was })
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	for name, a := range map[string]FollowupAsker{
		"heeds": FollowupFunc(func(ctx context.Context, _, _ string) (Followup, error) {
			<-ctx.Done()
			return Followup{Outcome: FollowupStop, Reason: "Too late."}, nil
		}),
		"ignores": FollowupFunc(func(context.Context, string, string) (Followup, error) {
			<-release
			return Followup{Outcome: FollowupStop, Reason: "Too late."}, nil
		}),
	} {
		start := time.Now()
		if got := Classify(context.Background(), a, "build it", "and the docs"); got.Outcome != FollowupQueue {
			t.Errorf("%s: %+v", name, got)
		}
		if time.Since(start) > 5*time.Second {
			t.Errorf("%s: waited %s", name, time.Since(start))
		}
	}
}

// The model's answer is read only when it is exactly one JSON object with an
// outcome and a reason, and both texts reach it clipped, as data.
func TestTheModelsAnswerIsStrictJSON(t *testing.T) {
	for _, tc := range []struct {
		reply, want string
	}{
		{`{"outcome":"replace","reason":"A new task."}`, FollowupReplace},
		{"\n {\"outcome\": \"stop\", \"reason\": \"Asks to stop.\"} \n", FollowupStop},
		{`{"outcome":"queue","reason":"Adds a detail."}`, FollowupQueue},
		{`stop`, FollowupQueue},
		{`{"outcome":"stop"}`, FollowupQueue},
		{`{"outcome":"stop","reason":"yes","extra":1}`, FollowupQueue},
		{`{"outcome":"stop","reason":"yes"} {}`, FollowupQueue},
		{`{"outcome":"stop","reason":"yes"`, FollowupQueue},
		{"Sure: {\"outcome\":\"stop\",\"reason\":\"Asks to stop.\"}", FollowupQueue},
		{"```json\n{\"outcome\":\"stop\",\"reason\":\"Asks to stop.\"}\n```", FollowupQueue},
		{`{"outcome":"halt","reason":"Asks to stop."}`, FollowupQueue},
	} {
		if got := Classify(context.Background(), Model{Provider: reply(tc.reply)}, "build it", "hm"); got.Outcome != tc.want {
			t.Errorf("%q = %+v, want %s", tc.reply, got, tc.want)
		}
	}
	if got := Classify(context.Background(), Model{}, "build it", "hm"); got.Outcome != FollowupQueue {
		t.Errorf("a model with no provider = %+v", got)
	}
	var sent string
	f := FollowupFunc(func(_ context.Context, current, message string) (Followup, error) {
		sent = current + "|" + message
		return Followup{Outcome: FollowupQueue, Reason: "Adds a detail."}, nil
	})
	Classify(context.Background(), f, strings.Repeat("a", 3*maxMessage), strings.Repeat("b", 3*maxMessage))
	if len(sent) > 2*maxMessage+16 {
		t.Errorf("the texts were sent unclipped: %d bytes", len(sent))
	}
	long := FollowupFunc(func(context.Context, string, string) (Followup, error) {
		return Followup{Outcome: FollowupStop, Reason: "a\x00b\n" + strings.Repeat("x", 2*maxReason)}, nil
	})
	if got := Classify(context.Background(), long, "build it", "hm"); len(got.Reason) > maxReason || strings.ContainsAny(got.Reason, "\x00\n") || !strings.HasPrefix(got.Reason, "a b x") {
		t.Errorf("a long reason = %q", got.Reason)
	}
}
