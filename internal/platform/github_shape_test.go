package platform

import (
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/runs"
	transport "github.com/urmzd/saige/flow/connectors/github"
	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/runtime"
)

// The host reads only the exact command. Every other wording starts as a
// reply, whatever it says about reviews: the agent reads what it means.
func TestOnlyTheExactCommandIsAReviewAtTheStart(t *testing.T) {
	for _, text := range []string{
		"review", "Review.", "REVIEW!", "re-review", "rereview?", "@humagen review", "@humagen re-review",
		"/review", "/review focus on auth", "> an earlier comment\n\n@humagen review",
	} {
		if !isReviewCommand(text) {
			t.Errorf("%q is the review command", text)
		}
	}
	for _, text := range []string{
		// Requests in words: the agent starts the review.
		"please review", "Please review.", "please review this PR", "review this pull request", "review the change please",
		"@humagen can you re-review?", "Hey @humagen, could you review it?", "please give it another review",
		"would you mind reviewing the retry?", "@humagen review the retry loop", "review\n\nfocus on security",
		"review: focus on auth", "@humagen review for security", "please review internal/retry/retry.go",
		"can you re-review this after my last push?", "@humagen review, focus on the error handling",
		"review it again, the backoff changed", "Could you review the webhook client?", "review only the migration files",
		"@humagen review\n- check the token is never logged\n- check retries stop on cancel",
		"please review and also explain finding 2", "review pls", "review asap", "review once more",
		"review before merging", "review when you get a chance", "do a quick review", "give it a quick review",
		// Talk about a review.
		"review for this PR missed the nil case", "review on line 4 is wrong", "review in the last comment is wrong",
		"review from earlier was wrong", "review the bot left is wrong", "review this morning was useful, thanks",
		"this review is wrong about retries", "review comments are noise, can you explain finding 2?",
		"Review: why is this nil?", "Review: is this nil safe?", "the review said the loop is fine", "reviews are noisy today",
		"review the retry changes and also fix them", "why did the review flag line 42?", "don't review yet",
		"please do not review", "> review\nwhat does Deliver return?", "what does `review` return?", "```\nreview\n```",
		"the reviewer is wrong about retries", "what does Deliver return?", "fix the second finding", "/reviewer", "",
	} {
		if isReviewCommand(text) {
			t.Errorf("%q starts as a reply", text)
		}
	}
}

// The host chooses the shape a run starts with: a review only on a pull
// request, and only on a signal that cannot be misread.
func TestGitHubShapeIsTheHostsChoice(t *testing.T) {
	pull := "github://acme/site/pull/7?head_sha=" + strings.Repeat("a", 40)
	issue := "github://acme/site/issues/7"
	raise := func(channel, text string, ctx map[string]string) event.Envelope {
		return wire.NewEvent(wire.Raise{ID: "e", Tenant: "t", Origin: "github", Channel: channel, Text: text, Context: ctx})
	}
	for _, tc := range []struct {
		name string
		tag  event.Envelope
		want string
	}{
		{"the command on a pull request", raise(pull, "review", nil), wire.ShapeReview},
		{"a request in words", raise(pull, "please review this", nil), wire.ShapeReply},
		{"a question on a pull request", raise(pull, "what does this change?", nil), wire.ShapeReply},
		{"the command on an issue", raise(issue, "review", nil), wire.ShapeReply},
		{"an automatic review", raise(pull, "Review pull request #7.", map[string]string{wire.MetaAnswerShape: wire.ShapeReview}), wire.ShapeReview},
		{"a rule that replies on a pull request", raise(pull, "review", map[string]string{wire.MetaAnswerShape: wire.ShapeReply}), wire.ShapeReply},
		{"a requested review", event.Envelope{Channel: pull, Data: event.GitHubReviewRequested{}}, wire.ShapeReview},
	} {
		if got := githubShape(tc.tag); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

// shapeRun records a run a person started on channel, saved as a reply.
func shapeRun(t *testing.T, p *Platform, id, channel string) runs.Run {
	t.Helper()
	tag := wire.NewEvent(wire.Raise{ID: id, Tenant: "t", Agent: "reviewer", Origin: "github", Channel: channel, Text: "have a look before I merge", At: time.Now().Add(-time.Minute)})
	wire.SetShape(&tag, wire.ShapeReply)
	runID := runtime.RunID("t", tag.ID)
	if err := p.Runs.Record(runID, tag, 1); err != nil {
		t.Fatal(err)
	}
	run, _ := p.Runs.Get("t", runID)
	return run
}

// start_review turns the person's run on a pull request into a review,
// once, as an action on the run's record: a retry reads it, and the run
// posts a native review only after it. It is refused on an issue and for a
// run another agent started.
func TestStartReviewTurnsTheAskersRunIntoAReviewOnce(t *testing.T) {
	p, _, _, at := reviewFixture(t)
	pull := "github://acme/site/pull/7?head_sha=" + strings.Repeat("a", 40)
	run := shapeRun(t, p, "asked", pull)
	seq := uint64(0)
	calls := 0
	tool := p.startReviewTool(runtime.Request{Tenant: "t", RunID: run.ID}, func(a event.Action) error {
		calls++
		seq++
		reviewAppend(t, p, run, seq, at.Add(time.Duration(seq)*time.Second), a)
		return nil
	})
	asks := func(r runs.Run) bool {
		t.Helper()
		asked, err := p.asksReview(shapeClient("t"), shapeView(r))
		if err != nil {
			t.Fatal(err)
		}
		return asked
	}
	if shape, _ := p.runShape(run); shape != wire.ShapeReply {
		t.Fatalf("the run starts as %q", shape)
	}
	for range 2 {
		if out, err := tool.Execute(t.Context(), map[string]any{}); err != nil || out != startReviewDone {
			t.Fatalf("start_review: %q %v", out, err)
		}
	}
	if calls != 1 {
		t.Fatalf("start_review recorded %d switches, want one", calls)
	}
	if shape, _ := p.runShape(run); shape != wire.ShapeReview {
		t.Fatalf("after start_review the run is %q", shape)
	}
	seq++
	reviewAppend(t, p, run, seq, at.Add(time.Duration(seq)*time.Second), event.Completed{Text: "{}"})
	if !asks(run) {
		t.Fatal("a run that started a review posts no native review")
	}
	if v, ok := p.onlineFormat(run); !ok || v.Passed {
		t.Fatalf("a started review was not format-checked: %+v %v", v, ok)
	}

	other := shapeRun(t, p, "not-asked", pull)
	reviewAppend(t, p, other, 1, at.Add(time.Second), event.Completed{Text: "The retry limit is three."})
	if asks(other) {
		t.Fatal("a run that did not start a review posts a native review")
	}
	if _, ok := p.onlineFormat(other); ok {
		t.Fatal("a reply was format-checked")
	}

	onIssue := shapeRun(t, p, "on-issue", "github://acme/site/issues/7")
	if _, err := p.startReviewTool(runtime.Request{Tenant: "t", RunID: onIssue.ID}, func(event.Action) error { return nil }).Execute(t.Context(), map[string]any{}); err == nil {
		t.Fatal("start_review was allowed on an issue")
	}
	handoff, err := call.New(run.Tag(), call.Request{Key: "helper", Agent: "reviewer", Text: "Take over", Mode: call.Handoff}, call.State{}, call.Limits{MaxDepth: 2, MaxFanOut: 2}, at)
	if err != nil {
		t.Fatal(err)
	}
	wire.SetShape(&handoff, wire.ShapeReply)
	childID := runtime.RunID("t", handoff.ID)
	if err := p.Runs.Record(childID, handoff, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := p.startReviewTool(runtime.Request{Tenant: "t", RunID: childID}, func(event.Action) error { return nil }).Execute(t.Context(), map[string]any{}); err == nil {
		t.Fatal("start_review was allowed for a run another agent started")
	}
}

func shapeClient(tenant string) clients.Client { return clients.Client{Tenant: tenant} }

func shapeView(r runs.Run) transport.View { return transport.View{Tenant: r.Tenant, RunID: r.ID} }
