package platform

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/clients"
	ghconnector "github.com/urmzd/humagen/pkg/integrations/github"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/saige/flow/address"
	transport "github.com/urmzd/saige/flow/connectors/github"
	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/runtime"
)

// handles are the triggers of the app in these tests: "@humagen", and the
// login of an app with a name of its own.
var handles = []string{"humagen", "humagen-acme"}

// The host reads only the exact command: the word alone, or addressed by a
// trigger that opens the message, or a message that starts with "/review".
func TestTheExactCommandIsAReviewAtTheStart(t *testing.T) {
	for _, text := range []string{
		// The word alone, in any case, with closing punctuation.
		"review", "Review", "REVIEW", "Review.", "REVIEW!", "review?", "review!!", "re-review", "Re-Review.", "rereview?",
		// Addressed by the trigger that opens the message.
		"@humagen review", "@Humagen Review", "@HUMAGEN REVIEW", "@humagen re-review", "@humagen rereview",
		"@humagen, review", "@humagen: review", "@humagen review.", "@humagen review!", "@humagen-acme review",
		// Space around it does not count.
		"  review  ", "\n\nreview\n", "\treview\t", "@humagen   review", "@humagen\nreview", "@humagen\n\nreview\n", "  @humagen review  ",
		// The slash command, alone or with what to look at after it.
		"/review", "/Review", "/REVIEW", "  /review  ", "/review focus on auth", "/review\nfocus on auth", "/review internal/retry/retry.go",
		"@humagen /review", "@humagen /review the retry loop",
		// A quoted line is what the person replies to.
		"> an earlier comment\n\n@humagen review", "> please review this\n\nreview", "> @humagen what changed?\n/review",
	} {
		if !isReviewCommand(text, handles) {
			t.Errorf("%q is the review command", text)
		}
	}
}

// Every other wording starts as a reply, whatever it says about reviews:
// the agent reads what it means, and starts the review when one is asked.
func TestAnyOtherWordingStartsAsAReply(t *testing.T) {
	for _, text := range []string{
		// The command with anything after it.
		"@humagen review this function's naming?", "@humagen review the retry loop", "@humagen review for security",
		"@humagen review, focus on the error handling", "@humagen review this", "@humagen review it", "@humagen review please",
		"@humagen review again", "@humagen review now", "@humagen review #12", "@humagen review @octocat's change",
		"review\n\nfocus on security", "review: focus on auth", "review - the auth part", "review (again)",
		"@humagen review\n- check the token is never logged\n- check retries stop on cancel",
		// Punctuation that leaves the sentence open.
		"review,", "review:", "review;", "review...ish", "review?!x", "review / approve", "\"review\"", "'review'", "(review)", "*review*", "`review`",
		// A request in words.
		"@humagen can you review this?", "@humagen can you review", "@humagen can you re-review?", "@humagen could you review it?",
		"@humagen please review", "@humagen pls review", "@humagen review pls", "please review", "Please review.", "please review this PR",
		"review this pull request", "review the change please", "please give it another review", "would you mind reviewing the retry?",
		"please review internal/retry/retry.go", "can you re-review this after my last push?", "review it again, the backoff changed",
		"Could you review the webhook client?", "review only the migration files", "please review and also explain finding 2",
		"review asap", "review once more", "review before merging", "review when you get a chance", "do a quick review", "give it a quick review",
		"a review", "the review", "re review", "re-review please", "reviews", "reviewed", "reviewing", "reviewer", "preview", "code review",
		// The trigger inside a sentence is part of what was said.
		"Hey @humagen review", "hey @humagen, review", "Thanks. @humagen review", "could @humagen review?", "ok @humagen /review",
		"review @humagen", "review, @humagen", "I think @humagen should review this", "cc @humagen for a review",
		// A mention of anyone else is not the command's address.
		"@octocat review", "@humagen @octocat review", "@octocat @humagen review", "review @octocat", "@acme/reviewers review",
		"@humagen review cc @octocat", "@humagen-bot review", "@humagenx review",
		// A slash command of another name, or one inside a sentence.
		"/reviewer", "/reviews", "/review-bot is down", "/review.md is missing", "/review/latest", "/re-review", "/ review",
		"what does /review do?", "use /review", "try\n/review", "@humagen what does /review do?", "//review",
		// Talk about a review.
		"review for this PR missed the nil case", "review on line 4 is wrong", "review in the last comment is wrong",
		"review from earlier was wrong", "review the bot left is wrong", "review this morning was useful, thanks",
		"this review is wrong about retries", "review comments are noise, can you explain finding 2?",
		"Review: why is this nil?", "Review: is this nil safe?", "the review said the loop is fine", "reviews are noisy today",
		"review the retry changes and also fix them", "why did the review flag line 42?", "don't review yet", "do not review",
		"please do not review", "no review", "not review", "the reviewer is wrong about retries", "what does Deliver return?",
		"fix the second finding", "@humagen why did you review this?", "@humagen was that a review?",
		// Quoted or shown text is not said.
		"> review\nwhat does Deliver return?", "> @humagen review", "> /review", "what does `review` return?", "```\nreview\n```",
		"```\n/review\n```", "    review\nis indented code", "<!-- review -->", "<!-- @humagen review -->\nwhat changed?",
		// Nothing at all.
		"", " ", "\n", "@humagen", "@humagen ", ".", "?",
	} {
		if isReviewCommand(text, handles) {
			t.Errorf("%q starts as a reply", text)
		}
	}
}

// The host chooses the shape a run starts with: a review only on a pull
// request, and only on a signal that cannot be misread. The same words on
// an issue start a reply.
func TestGitHubShapeIsTheHostsChoice(t *testing.T) {
	pull := "github://acme/site/pull/7?head_sha=" + strings.Repeat("a", 40)
	thread := pull + "&review_comment=31"
	issue := "github://acme/site/issues/7"
	raise := func(channel, text string, ctx map[string]string) event.Envelope {
		return wire.NewEvent(wire.Raise{ID: "e", Tenant: "t", Origin: "github", Channel: channel, Text: text, Context: ctx})
	}
	review, reply := map[string]string{wire.MetaAnswerShape: wire.ShapeReview}, map[string]string{wire.MetaAnswerShape: wire.ShapeReply}
	for _, tc := range []struct {
		name string
		tag  event.Envelope
		want string
	}{
		{"the command on a pull request", raise(pull, "review", nil), wire.ShapeReview},
		{"the addressed command on a pull request", raise(pull, "@humagen review", nil), wire.ShapeReview},
		{"the addressed command in any case", raise(pull, "@Humagen Review.", nil), wire.ShapeReview},
		{"the slash command on a pull request", raise(pull, "/review", nil), wire.ShapeReview},
		{"the slash command with what to look at", raise(pull, "/review the retry loop", nil), wire.ShapeReview},
		{"the command in a line comment's thread", raise(thread, "@humagen review", nil), wire.ShapeReview},
		{"the command with more after it", raise(pull, "@humagen review this function's naming?", nil), wire.ShapeReply},
		{"a request in words", raise(pull, "please review this", nil), wire.ShapeReply},
		{"a question that asks for a review", raise(pull, "@humagen can you review this?", nil), wire.ShapeReply},
		{"the trigger inside a sentence", raise(pull, "hey @humagen review", nil), wire.ShapeReply},
		{"the command addressed to someone else", raise(pull, "@octocat review", nil), wire.ShapeReply},
		{"a question on a pull request", raise(pull, "what does this change?", nil), wire.ShapeReply},
		{"no text on a pull request", raise(pull, "", nil), wire.ShapeReply},
		{"the command on an issue", raise(issue, "review", nil), wire.ShapeReply},
		{"the addressed command on an issue", raise(issue, "@humagen review", nil), wire.ShapeReply},
		{"the slash command on an issue", raise(issue, "/review", nil), wire.ShapeReply},
		{"a request in words on an issue", raise(issue, "please review this", nil), wire.ShapeReply},
		{"the command on a repository", raise("github://acme/site", "review", nil), wire.ShapeReply},
		{"the command with no channel", raise("", "review", nil), wire.ShapeReply},
		{"the command in a Slack channel", raise("slack://T1/C1", "review", nil), wire.ShapeReply},
		{"an automatic review", raise(pull, "Review pull request #7.", review), wire.ShapeReview},
		{"a rule that reviews, whatever its instructions say", raise(pull, "Summarize the change.", review), wire.ShapeReview},
		{"a rule that replies on a pull request", raise(pull, "review", reply), wire.ShapeReply},
		{"a rule that reviews on an issue", raise(issue, "review", review), wire.ShapeReply},
		{"a shape that is neither", raise(pull, "what changed?", map[string]string{wire.MetaAnswerShape: "approve"}), wire.ShapeReply},
		{"a requested review", event.Envelope{Channel: pull, Data: event.GitHubReviewRequested{}}, wire.ShapeReview},
		{"a requested review with no pull request", event.Envelope{Channel: issue, Data: event.GitHubReviewRequested{}}, wire.ShapeReply},
	} {
		if got := githubShape(tc.tag, handles); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

// shapeRun records a run octocat started on channel, saved as a reply.
func shapeRun(t *testing.T, p *Platform, id, channel string) runs.Run {
	t.Helper()
	tag := wire.NewEvent(wire.Raise{ID: id, Tenant: "t", Agent: "reviewer", Origin: "github", Channel: channel, Text: "have a look before I merge", Actor: event.Actor{ID: "octocat"}, At: time.Now().Add(-time.Minute)})
	wire.SetShape(&tag, wire.ShapeReply)
	return shapeRecord(t, p, tag)
}

func shapeRecord(t *testing.T, p *Platform, tag event.Envelope) runs.Run {
	t.Helper()
	runID := runtime.RunID("t", tag.ID)
	if err := p.Runs.Record(runID, tag, 1); err != nil {
		t.Fatal(err)
	}
	run, err := p.Runs.Get("t", runID)
	if err != nil {
		t.Fatal(err)
	}
	return run
}

// startReview calls start_review in run, recording what it reports on the
// run's stream, and counts the switches recorded.
func startReview(t *testing.T, p *Platform, run runs.Run, recorded *int) (string, error) {
	t.Helper()
	return p.startReviewTool(runtime.Request{Tenant: run.Tenant, RunID: run.ID}, func(a event.Action) error {
		*recorded++
		reviewAppend(t, p, run, uint64(*recorded), time.Now().UTC().Add(-time.Second), a)
		return nil
	}).Execute(t.Context(), map[string]any{})
}

// start_review turns the person's run on a pull request into a review, as
// an action on the run's record: a retry reads it, the run posts a native
// review only after it, and its answer is checked as a review.
func TestStartReviewTurnsTheAskersRunIntoAReview(t *testing.T) {
	p, _, _, at := reviewFixture(t)
	pull := "github://acme/site/pull/7?head_sha=" + strings.Repeat("a", 40)
	run := shapeRun(t, p, "asked", pull)
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
	recorded := 0
	if out, err := startReview(t, p, run, &recorded); err != nil || out != startReviewDone {
		t.Fatalf("start_review: %q %v", out, err)
	}
	if recorded != 1 {
		t.Fatalf("start_review recorded %d switches, want one", recorded)
	}
	if shape, _ := p.runShape(run); shape != wire.ShapeReview {
		t.Fatalf("after start_review the run is %q", shape)
	}
	// The saved shape is unchanged: the switch is on the run's record.
	if saved, _ := p.Runs.Get("t", run.ID); wire.ShapeOf(saved.Tag()) != wire.ShapeReply {
		t.Fatalf("start_review rewrote the saved shape to %q", wire.ShapeOf(saved.Tag()))
	}
	reviewAppend(t, p, run, 2, at.Add(2*time.Second), event.Completed{Text: "{}"})
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
}

// A run becomes a review once. A second call is refused and records
// nothing, in the process that took the first and in one that reads the
// run's record; so is a call in a run the host started as a review.
func TestStartReviewIsRefusedASecondTime(t *testing.T) {
	p, _, _, _ := reviewFixture(t)
	pull := "github://acme/site/pull/7?head_sha=" + strings.Repeat("a", 40)
	run := shapeRun(t, p, "asked", pull)
	recorded := 0
	if _, err := startReview(t, p, run, &recorded); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if out, err := startReview(t, p, run, &recorded); err == nil || out != "" || err.Error() != startReviewAlready {
			t.Fatalf("a second start_review: %q %v", out, err)
		}
	}
	// Another process holds nothing in memory and reads the run's record.
	p.startedReviews.Clear()
	if _, err := startReview(t, p, run, &recorded); err == nil || err.Error() != startReviewAlready {
		t.Fatalf("a second start_review, read from the record: %v", err)
	}
	if recorded != 1 {
		t.Fatalf("start_review recorded %d switches, want one", recorded)
	}
	if shape, _ := p.runShape(run); shape != wire.ShapeReview {
		t.Fatalf("the run is %q after a refused call", shape)
	}

	// Before the record holds the first call, the second is refused too.
	lagging := shapeRun(t, p, "lagging", pull)
	tool := p.startReviewTool(runtime.Request{Tenant: "t", RunID: lagging.ID}, func(event.Action) error { recorded++; return nil })
	if _, err := tool.Execute(t.Context(), map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(t.Context(), map[string]any{}); err == nil || err.Error() != startReviewAlready {
		t.Fatalf("a second start_review before the first is recorded: %v", err)
	}
	if recorded != 2 {
		t.Fatalf("%d switches recorded over two runs, want two", recorded)
	}

	// A call whose record fails switches nothing, and may be made again.
	failed := shapeRun(t, p, "failed", pull)
	if _, err := p.startReviewTool(runtime.Request{Tenant: "t", RunID: failed.ID}, func(event.Action) error { return errors.New("the stream is closed") }).Execute(t.Context(), map[string]any{}); err == nil {
		t.Fatal("start_review succeeded without a record")
	}
	if shape, _ := p.runShape(failed); shape != wire.ShapeReply {
		t.Fatalf("a call that recorded nothing left the run %q", shape)
	}
	if _, err := startReview(t, p, failed, new(int)); err != nil {
		t.Fatalf("start_review after a failed record: %v", err)
	}

	started := wire.NewEvent(wire.Raise{ID: "command", Tenant: "t", Agent: "reviewer", Origin: "github", Channel: pull, Text: "review", Actor: event.Actor{ID: "octocat"}, At: time.Now().Add(-time.Minute)})
	wire.SetShape(&started, wire.ShapeReview)
	before := recorded
	if _, err := startReview(t, p, shapeRecord(t, p, started), &recorded); err == nil || err.Error() != startReviewAlready || recorded != before {
		t.Fatalf("start_review in a run that started as a review: %v, %d recorded", err, recorded-before)
	}
}

// start_review needs a pull request: on an issue, a repository or anywhere
// else it is refused, and the run stays a reply.
func TestStartReviewIsRefusedOffAPullRequest(t *testing.T) {
	p, _, _, at := reviewFixture(t)
	for name, channel := range map[string]string{
		"an issue":       "github://acme/site/issues/7",
		"a repository":   "github://acme/site",
		"a Slack thread": "slack://T1/C1?thread_ts=1.2",
		"no channel":     "",
	} {
		run := shapeRun(t, p, "off-"+strings.ReplaceAll(name, " ", "-"), channel)
		recorded := 0
		out, err := startReview(t, p, run, &recorded)
		if err == nil || out != "" || recorded != 0 {
			t.Errorf("start_review on %s: %q %v, %d recorded", name, out, err, recorded)
			continue
		}
		if !strings.Contains(err.Error(), "needs a pull request") {
			t.Errorf("start_review on %s was refused with %q", name, err)
		}
		if shape, _ := p.runShape(run); shape != wire.ShapeReply {
			t.Errorf("start_review on %s left the run %q", name, shape)
		}
		reviewAppend(t, p, run, 1, at.Add(time.Second), event.Completed{Text: "{}"})
		if asked, err := p.asksReview(shapeClient("t"), shapeView(run)); err != nil || asked {
			t.Errorf("a refused run on %s posts a native review: %v %v", name, asked, err)
		}
	}
}

// start_review is for the run the asker started. A run an agent started for
// another, by a handoff, a subagent call or a delegation, is refused, and so
// is one no person asked for: a rule's run, whose shape is the rule's, a run
// a bot started, and a call outside any recorded run.
func TestStartReviewIsRefusedForARunTheAskerDidNotStart(t *testing.T) {
	p, _, _, at := reviewFixture(t)
	pull := "github://acme/site/pull/7?head_sha=" + strings.Repeat("a", 40)
	asked := shapeRun(t, p, "asked", pull)
	refused := func(name string, run runs.Run, why string) {
		t.Helper()
		recorded := 0
		out, err := startReview(t, p, run, &recorded)
		if err == nil || out != "" || recorded != 0 {
			t.Errorf("start_review in %s: %q %v, %d recorded", name, out, err, recorded)
			return
		}
		if !strings.Contains(err.Error(), why) {
			t.Errorf("start_review in %s was refused with %q, want %q", name, err, why)
		}
		if shape, _ := p.runShape(run); shape == wire.ShapeReview {
			t.Errorf("start_review in %s made the run a review", name)
		}
	}
	for _, mode := range []call.Mode{call.Handoff, call.Subagent, call.Delegate} {
		child, err := call.New(asked.Tag(), call.Request{Key: "helper-" + string(mode), Agent: "reviewer", Text: "Take over", Mode: mode}, call.State{}, call.Limits{MaxDepth: 2, MaxFanOut: 4}, at)
		if err != nil {
			t.Fatal(err)
		}
		// Even one that answers on the pull request, saved as a reply.
		child.Channel = pull
		wire.SetShape(&child, wire.ShapeReply)
		refused("a "+string(mode)+" run", shapeRecord(t, p, child), "answers another agent")
	}
	// A call in a child run switches neither it nor the run that called it.
	if shape, _ := p.runShape(asked); shape != wire.ShapeReply {
		t.Fatalf("a child's call made the asker's run %q", shape)
	}

	rule := wire.NewEvent(wire.Raise{ID: "automation/gh/d1/summarize", Tenant: "t", Agent: "reviewer", Origin: "github", Channel: pull, Text: "Summarize the change.", At: time.Now().Add(-time.Minute),
		Context: map[string]string{wire.MetaGitHubEvent: "pull_request", wire.MetaAnswerShape: wire.ShapeReply}})
	refused("a rule's run", shapeRecord(t, p, rule), "no person asked")

	bot := wire.NewEvent(wire.Raise{ID: "from-a-bot", Tenant: "t", Agent: "reviewer", Origin: "github", Channel: pull, Text: "have a look", Actor: event.Actor{ID: "dependabot[bot]", Bot: true}, At: time.Now().Add(-time.Minute)})
	wire.SetShape(&bot, wire.ShapeReply)
	refused("a bot's run", shapeRecord(t, p, bot), "no person asked")

	for name, req := range map[string]runtime.Request{
		"no run":                     {Tenant: "t"},
		"a run that is not recorded": {Tenant: "t", RunID: runtime.RunID("t", "nowhere")},
		"another workspace's run":    {Tenant: "other", RunID: asked.ID},
	} {
		recorded := 0
		if out, err := p.startReviewTool(req, func(event.Action) error { recorded++; return nil }).Execute(t.Context(), map[string]any{}); err == nil || out != "" || recorded != 0 {
			t.Errorf("start_review with %s: %q %v, %d recorded", name, out, err, recorded)
		}
	}
	if shape, _ := p.runShape(asked); shape != wire.ShapeReply {
		t.Fatalf("a refused call made the asker's run %q", shape)
	}
	// With no way to record, the asker's own call switches nothing.
	if _, err := p.startReviewTool(runtime.Request{Tenant: "t", RunID: asked.ID}, nil).Execute(t.Context(), map[string]any{}); err == nil {
		t.Fatal("start_review succeeded with no record")
	}
	if _, err := startReview(t, p, asked, new(int)); err != nil {
		t.Fatalf("the asker's run could not start a review afterwards: %v", err)
	}
}

// A reply run that called start_review posts its report as a native pull
// request review, at the head the host pinned. The same report from a run
// that did not call it posts no review: it stays the run's comment, as any
// reply is. The transport keeps the run's comment in both.
func TestAStartedReviewIsPostedAsANativePullRequestReview(t *testing.T) {
	sha := strings.Repeat("a", 40)
	target := address.MustParse("github://acme/site/pull/7?head_sha=" + sha)
	answer := `{"format":"humagen.review.v2","title":"Review","verdict":"comment","summary":"Inspected the change.","findings":[]}`
	for _, starts := range []bool{true, false} {
		p, _, _, at := reviewFixture(t)
		api := transport.NewFake()
		proof := transport.Context{}
		proof.Subject.Head.SHA = sha
		if err := api.SeedContext(transport.Ref{Owner: "acme", Repo: "site", Number: 7}, proof); err != nil {
			t.Fatal(err)
		}
		peer, err := ghconnector.New(ghconnector.Config{API: api, Owners: []string{"acme"}, Tenant: "t", Login: "humagen[bot]", Interval: -1,
			AsksReview: func(v transport.View) (bool, error) { return p.asksReview(shapeClient("t"), v) },
			ReviewText: func(v transport.View) (string, error) {
				run, err := p.Runs.Get("t", v.RunID)
				if err != nil {
					return "", err
				}
				return p.reviewText(t.Context(), run, v.Text, nil, time.Time{}), nil
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		run := shapeRun(t, p, "asked", target.String())
		seq := 0
		if starts {
			if _, err := startReview(t, p, run, &seq); err != nil {
				t.Fatal(err)
			}
		}
		done := reviewAppend(t, p, run, uint64(seq+1), at.Add(30*time.Second), event.Completed{Text: answer})
		done.Channel = target.String()
		if err := peer.Deliver(t.Context(), target, done); err != nil {
			t.Fatal(err)
		}
		reviews, comments := api.SubmittedReviews(), api.Comments()
		if !starts {
			if len(reviews) != 0 || len(comments) != 1 {
				t.Fatalf("a reply posted %d native reviews and %d comments, want the run's comment only", len(reviews), len(comments))
			}
			continue
		}
		if len(reviews) != 1 || len(comments) != 1 {
			t.Fatalf("a started review posted %d native reviews and %d comments, want one review beside the run's comment", len(reviews), len(comments))
		}
		if got := reviews[0]; got.Ref.Number != 7 || got.Review.CommitID != sha || got.Review.Event != "COMMENT" || !strings.Contains(got.Review.Body, "**No problems found.**") {
			t.Fatalf("native review: %+v", got)
		}
	}
}

func shapeClient(tenant string) clients.Client { return clients.Client{Tenant: tenant} }

func shapeView(r runs.Run) transport.View { return transport.View{Tenant: r.Tenant, RunID: r.ID} }
