package platform

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	presentation "github.com/urmzd/humagen/internal/presentation/github"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/clients"
	ghconnector "github.com/urmzd/humagen/pkg/integrations/github"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
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
		"  review  ", "\n\nreview\n", "review\t", "@humagen   review", "@humagen\nreview", "@humagen\n\nreview\n", "  @humagen review  ",
		// The slash command, alone or with what to look at after it.
		"/review", "/Review", "/REVIEW", "  /review  ", "/review focus on auth", "/review\nfocus on auth", "/review internal/retry/retry.go",
		"@humagen /review", "@humagen /review the retry loop",
		// A quoted line is what the person replies to.
		"> an earlier comment\n\n@humagen review", "> please review this\n\nreview", "> @humagen what changed?\n\n/review",
		"> one line\n> and another\n\nreview", "review\n\n> the comment it answers",
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
		// A line a quote runs on to is part of the quote.
		"> quote\nreview", "> quote\n/review", "> quote\n@humagen review", "> quote\n> more\nreview", "> quote\nreview\n\nthanks",
		// An indented block is code.
		"    review", "\treview", "    /review", "    @humagen review", "\n    review\n", "what about\n\n    review",
		// A command beside something shown is not the whole message.
		"review\n\n```\ncode\n```", "```\ncode\n```\n\nreview", "<!-- note -->\nreview", "review <!-- note -->", "`x` review", "/review `x`\n\n    code",
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

// startReview calls start_review in run and counts what it reports on the
// run's stream. Nothing it reports is kept: the run's record holds the
// switch.
func startReview(t *testing.T, p *Platform, run runs.Run, reported *int) (string, error) {
	t.Helper()
	return p.startReviewTool(runtime.Request{Tenant: run.Tenant, RunID: run.ID}, func(event.Action) error {
		*reported++
		return nil
	}).Execute(t.Context(), map[string]any{})
}

// shapeNow is the shape of a run as its record says it now.
func shapeNow(t *testing.T, p *Platform, run runs.Run) string {
	t.Helper()
	now, err := p.Runs.Get(run.Tenant, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	return runShape(now)
}

// asksNow reports whether a run would post a native review now.
func asksNow(t *testing.T, p *Platform, run runs.Run) bool {
	t.Helper()
	asked, err := p.asksReview(shapeClient(run.Tenant), shapeView(run))
	if err != nil {
		t.Fatal(err)
	}
	return asked
}

// formatChecked reports whether a run's answer would be checked as a
// review now.
func formatChecked(t *testing.T, p *Platform, run runs.Run) bool {
	t.Helper()
	now, err := p.Runs.Get(run.Tenant, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, ok := p.onlineFormat(now)
	return ok
}

// start_review turns the person's run on a pull request into a review, on
// the run's own record. Nothing of the switch needs to be in the journal:
// with no event of the run kept, the run still posts a native review and
// its answer is still checked as one.
func TestStartReviewTurnsTheAskersRunIntoAReview(t *testing.T) {
	p, _, _, _ := reviewFixture(t)
	pull := "github://acme/site/pull/7?head_sha=" + strings.Repeat("a", 40)
	run := shapeRun(t, p, "asked", pull)
	if shape := shapeNow(t, p, run); shape != wire.ShapeReply {
		t.Fatalf("the run starts as %q", shape)
	}
	if asksNow(t, p, run) || formatChecked(t, p, run) {
		t.Fatal("a reply posts a native review or is format-checked")
	}
	reported := 0
	if out, err := startReview(t, p, run, &reported); err != nil || out != startReviewDone {
		t.Fatalf("start_review: %q %v", out, err)
	}
	if reported != 1 {
		t.Fatalf("start_review reported %d actions on the stream, want one", reported)
	}
	if shape := shapeNow(t, p, run); shape != wire.ShapeReview {
		t.Fatalf("after start_review the run is %q", shape)
	}
	// The saved shape is unchanged: the switch is beside it on the record.
	if saved, _ := p.Runs.Get("t", run.ID); wire.ShapeOf(saved.Tag()) != wire.ShapeReply || !saved.StartedReview() {
		t.Fatalf("the record holds shape %q, started %v", wire.ShapeOf(saved.Tag()), saved.StartedReview())
	}
	// The journal holds nothing of this run, as after it has dropped the
	// run's events.
	if events, err := p.Journal.Events("t", []string{run.ID}); err != nil || len(events) != 0 {
		t.Fatalf("the journal holds %d events of the run, %v", len(events), err)
	}
	if !asksNow(t, p, run) {
		t.Fatal("a run that started a review posts no native review")
	}
	if !formatChecked(t, p, run) {
		t.Fatal("a started review is not format-checked")
	}
	if _, err := startReview(t, p, run, &reported); err == nil || err.Error() != startReviewAlready {
		t.Fatalf("a second start_review with nothing in the journal: %v", err)
	}

	// With no way to report on the stream, the switch is still saved.
	quiet := shapeRun(t, p, "quiet", pull)
	if out, err := p.startReviewTool(runtime.Request{Tenant: "t", RunID: quiet.ID}, nil).Execute(t.Context(), map[string]any{}); err != nil || out != startReviewDone {
		t.Fatalf("start_review with no stream: %q %v", out, err)
	}
	if shape := shapeNow(t, p, quiet); shape != wire.ShapeReview {
		t.Fatalf("with no stream the run is %q", shape)
	}
	// A stream that fails does not undo it either.
	closed := shapeRun(t, p, "closed", pull)
	if out, err := p.startReviewTool(runtime.Request{Tenant: "t", RunID: closed.ID}, func(event.Action) error { return errors.New("the stream is closed") }).Execute(t.Context(), map[string]any{}); err != nil || out != startReviewDone {
		t.Fatalf("start_review with a closed stream: %q %v", out, err)
	}
	if shape := shapeNow(t, p, closed); shape != wire.ShapeReview {
		t.Fatalf("with a closed stream the run is %q", shape)
	}

	other := shapeRun(t, p, "not-asked", pull)
	if asksNow(t, p, other) || formatChecked(t, p, other) {
		t.Fatal("a run that did not start a review posts a native review or is format-checked")
	}
}

// A run becomes a review once. A second call is refused and reports
// nothing; so is a call in a run the host started as a review.
func TestStartReviewIsRefusedASecondTime(t *testing.T) {
	p, _, _, _ := reviewFixture(t)
	pull := "github://acme/site/pull/7?head_sha=" + strings.Repeat("a", 40)
	run := shapeRun(t, p, "asked", pull)
	reported := 0
	if _, err := startReview(t, p, run, &reported); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if out, err := startReview(t, p, run, &reported); err == nil || out != "" || err.Error() != startReviewAlready {
			t.Fatalf("a second start_review: %q %v", out, err)
		}
	}
	if reported != 1 {
		t.Fatalf("start_review reported %d actions, want one", reported)
	}
	if shape := shapeNow(t, p, run); shape != wire.ShapeReview {
		t.Fatalf("the run is %q after a refused call", shape)
	}

	started := wire.NewEvent(wire.Raise{ID: "command", Tenant: "t", Agent: "reviewer", Origin: "github", Channel: pull, Text: "review", Actor: event.Actor{ID: "octocat"}, At: time.Now().Add(-time.Minute)})
	wire.SetShape(&started, wire.ShapeReview)
	command := shapeRecord(t, p, started)
	before := reported
	if _, err := startReview(t, p, command, &reported); err == nil || err.Error() != startReviewAlready || reported != before {
		t.Fatalf("start_review in a run that started as a review: %v, %d reported", err, reported-before)
	}
	if saved, _ := p.Runs.Get("t", command.ID); saved.StartedReview() {
		t.Fatal("a refused call in a review run changed its record")
	}
}

// The switch is on the run's record in the store, so a process started
// later reads it: the run is still a review there, it still posts a native
// review and is format-checked, and a second call there is refused.
func TestAStartedReviewOutlivesARestart(t *testing.T) {
	st := state.NewMemory()
	open := func() *Platform {
		t.Helper()
		p, err := New(Config{Workspace: t.TempDir(), Backends: Backends{State: st}})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	pull := "github://acme/site/pull/7?head_sha=" + strings.Repeat("a", 40)
	first := open()
	run := shapeRun(t, first, "asked", pull)
	reply := shapeRun(t, first, "not-asked", pull)
	if _, err := startReview(t, first, run, new(int)); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second := open()
	t.Cleanup(func() { _ = second.Close() })
	if shape := shapeNow(t, second, run); shape != wire.ShapeReview {
		t.Fatalf("after a restart the run is %q", shape)
	}
	if !asksNow(t, second, run) || !formatChecked(t, second, run) {
		t.Fatal("after a restart the started review posts no native review or is not format-checked")
	}
	reported := 0
	if out, err := startReview(t, second, run, &reported); err == nil || out != "" || err.Error() != startReviewAlready || reported != 0 {
		t.Fatalf("a second start_review after a restart: %q %v, %d reported", out, err, reported)
	}
	if shape := shapeNow(t, second, reply); shape != wire.ShapeReply {
		t.Fatalf("after a restart the reply is %q", shape)
	}
	if out, err := startReview(t, second, reply, &reported); err != nil || out != startReviewDone {
		t.Fatalf("the reply could not start a review after a restart: %q %v", out, err)
	}
}

// Of the calls two processes make at once in one run, exactly one starts
// the review and reports it; every other is refused.
func TestConcurrentStartReviewCallsStartOneReview(t *testing.T) {
	st := state.NewMemory()
	var ps [2]*Platform
	for i := range ps {
		p, err := New(Config{Workspace: t.TempDir(), Backends: Backends{State: st}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = p.Close() })
		ps[i] = p
	}
	pull := "github://acme/site/pull/7?head_sha=" + strings.Repeat("a", 40)
	for round := range 5 {
		run := shapeRun(t, ps[0], "asked-"+strconv.Itoa(round), pull)
		var done, refused, reported atomic.Int32
		var wg sync.WaitGroup
		begin := make(chan struct{})
		for i := range 8 {
			tool := ps[i%2].startReviewTool(runtime.Request{Tenant: "t", RunID: run.ID}, func(event.Action) error { reported.Add(1); return nil })
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-begin
				out, err := tool.Execute(context.Background(), map[string]any{})
				switch {
				case err == nil && out == startReviewDone:
					done.Add(1)
				case err != nil && err.Error() == startReviewAlready:
					refused.Add(1)
				default:
					t.Errorf("start_review: %q %v", out, err)
				}
			}()
		}
		close(begin)
		wg.Wait()
		if done.Load() != 1 || refused.Load() != 7 || reported.Load() != 1 {
			t.Fatalf("round %d: %d calls started the review, %d were refused, %d reported; want 1, 7, 1", round, done.Load(), refused.Load(), reported.Load())
		}
		for i, p := range ps {
			if shape := shapeNow(t, p, run); shape != wire.ShapeReview {
				t.Fatalf("round %d: process %d reads %q", round, i, shape)
			}
		}
	}
}

// A switch that cannot be saved fails the call, and the run stays a reply.
func TestStartReviewFailsWhenTheSwitchCannotBeSaved(t *testing.T) {
	st := &failingWrites{Store: state.NewMemory()}
	p, err := New(Config{Workspace: t.TempDir(), Backends: Backends{State: st}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	run := shapeRun(t, p, "asked", "github://acme/site/pull/7?head_sha="+strings.Repeat("a", 40))
	st.fail.Store(true)
	reported := 0
	out, err := startReview(t, p, run, &reported)
	st.fail.Store(false)
	if err == nil || out != "" || reported != 0 || !strings.Contains(err.Error(), "could not be saved") {
		t.Fatalf("start_review with a store that refuses writes: %q %v, %d reported", out, err, reported)
	}
	if shape := shapeNow(t, p, run); shape != wire.ShapeReply {
		t.Fatalf("a call that saved nothing left the run %q", shape)
	}
	if asksNow(t, p, run) {
		t.Fatal("a call that saved nothing lets the run post a native review")
	}
	if out, err := startReview(t, p, run, &reported); err != nil || out != startReviewDone {
		t.Fatalf("start_review once the store writes again: %q %v", out, err)
	}
}

// failingWrites is a store whose writes fail while fail is set.
type failingWrites struct {
	state.Store
	fail atomic.Bool
}

func (f *failingWrites) Write(ctx context.Context, namespace string, ops ...state.Op) error {
	if f.fail.Load() {
		return errors.New("the store is unavailable")
	}
	return f.Store.Write(ctx, namespace, ops...)
}

// start_review needs a pull request: on an issue, a repository or anywhere
// else it is refused, and the run stays a reply.
func TestStartReviewIsRefusedOffAPullRequest(t *testing.T) {
	p, _, _, _ := reviewFixture(t)
	for name, channel := range map[string]string{
		"an issue":       "github://acme/site/issues/7",
		"a repository":   "github://acme/site",
		"a Slack thread": "slack://T1/C1?thread_ts=1.2",
		"no channel":     "",
	} {
		run := shapeRun(t, p, "off-"+strings.ReplaceAll(name, " ", "-"), channel)
		reported := 0
		out, err := startReview(t, p, run, &reported)
		if err == nil || out != "" || reported != 0 {
			t.Errorf("start_review on %s: %q %v, %d reported", name, out, err, reported)
			continue
		}
		if !strings.Contains(err.Error(), "needs a pull request") {
			t.Errorf("start_review on %s was refused with %q", name, err)
		}
		if shape := shapeNow(t, p, run); shape != wire.ShapeReply {
			t.Errorf("start_review on %s left the run %q", name, shape)
		}
		if asksNow(t, p, run) {
			t.Errorf("a refused run on %s posts a native review", name)
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
		reported := 0
		out, err := startReview(t, p, run, &reported)
		if err == nil || out != "" || reported != 0 {
			t.Errorf("start_review in %s: %q %v, %d reported", name, out, err, reported)
			return
		}
		if !strings.Contains(err.Error(), why) {
			t.Errorf("start_review in %s was refused with %q, want %q", name, err, why)
		}
		if shape := shapeNow(t, p, run); shape == wire.ShapeReview {
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
	if shape := shapeNow(t, p, asked); shape != wire.ShapeReply {
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
		reported := 0
		if out, err := p.startReviewTool(req, func(event.Action) error { reported++; return nil }).Execute(t.Context(), map[string]any{}); err == nil || out != "" || reported != 0 {
			t.Errorf("start_review with %s: %q %v, %d reported", name, out, err, reported)
		}
	}
	if shape := shapeNow(t, p, asked); shape != wire.ShapeReply {
		t.Fatalf("a refused call made the asker's run %q", shape)
	}
	if _, err := startReview(t, p, asked, new(int)); err != nil {
		t.Fatalf("the asker's run could not start a review afterwards: %v", err)
	}
}

// shapeConnector is a GitHub connector on a fake API whose reviews are the
// platform's to allow, as a client's connector is wired.
func shapeConnector(t *testing.T, p *Platform, api *transport.Fake) *ghconnector.Connector {
	t.Helper()
	peer, err := ghconnector.New(ghconnector.Config{API: api, Owners: []string{"acme"}, Tenant: "t", Login: "humagen[bot]", AppID: 9001, AppLogin: appBotLogin, Interval: -1,
		AsksReview: func(v transport.View) (bool, error) { return p.asksReview(shapeClient("t"), v) },
		ReviewText: func(v transport.View) (string, error) {
			// As reviewView: only a report is read against the run's record.
			if !strings.Contains(v.Text, presentation.ReviewFormatV2) {
				return v.Text, nil
			}
			run, err := p.Runs.Get("t", v.RunID)
			if err != nil {
				return "", err
			}
			if err := p.reviewRecorded(run.Tenant, run.ID, 0); err != nil {
				return "", err
			}
			return p.reviewText(t.Context(), run, v.Text, nil, time.Time{}), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return peer
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
		peer := shapeConnector(t, p, api)
		run := shapeRun(t, p, "asked", target.String())
		if starts {
			if _, err := startReview(t, p, run, new(int)); err != nil {
				t.Fatal(err)
			}
		}
		done := reviewAppend(t, p, run, 1, at.Add(30*time.Second), event.Completed{Text: answer})
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

// A plain reply on a pull request is delivered from the run's record
// alone. With nothing of the run in the journal, not even its completion,
// the delivery succeeds at once: the answer is the run's comment, and no
// native review is posted.
func TestAPlainReplyIsDeliveredWithoutTheJournal(t *testing.T) {
	p, _, _, at := reviewFixture(t)
	sha := strings.Repeat("a", 40)
	target := address.MustParse("github://acme/site/pull/7?head_sha=" + sha)
	api := transport.NewFake()
	proof := transport.Context{}
	proof.Subject.Head.SHA = sha
	if err := api.SeedContext(transport.Ref{Owner: "acme", Repo: "site", Number: 7}, proof); err != nil {
		t.Fatal(err)
	}
	peer := shapeConnector(t, p, api)
	run := shapeRun(t, p, "asked", target.String())
	done := event.Envelope{ID: run.ID + ":1", Tenant: run.Tenant, Run: run.ID, Agent: run.Agent, Seq: 1, Time: at.Add(30 * time.Second), Type: event.TypeCompleted, Channel: target.String(), Data: event.Completed{Text: "The retry limit is three."}}
	if events, err := p.Journal.Events("t", []string{run.ID}); err != nil || len(events) != 0 {
		t.Fatalf("the journal holds %d events of the run, %v", len(events), err)
	}
	if asked, err := p.asksReview(shapeClient("t"), shapeView(run)); err != nil || asked {
		t.Fatalf("a reply with no completion in the journal: asks %v, %v", asked, err)
	}
	if err := peer.Deliver(t.Context(), target, done); err != nil {
		t.Fatalf("a plain reply was not delivered: %v", err)
	}
	reviews, comments := api.SubmittedReviews(), api.Comments()
	if len(reviews) != 0 || len(comments) != 1 || !strings.Contains(comments[0].Body, "The retry limit is three.") {
		t.Fatalf("a plain reply posted %d native reviews and comments %+v", len(reviews), comments)
	}
}

// A later attempt of a run that became a review reads answer_shape: review
// in its trigger context. A first attempt, a run that is still a reply and
// a request that names no shape read what the run started with.
func TestALaterAttemptOfAStartedReviewReadsReview(t *testing.T) {
	p, _, _, _ := reviewFixture(t)
	pull := "github://acme/site/pull/7?head_sha=" + strings.Repeat("a", 40)
	started, reply := shapeRun(t, p, "asked", pull), shapeRun(t, p, "not-asked", pull)
	if _, err := startReview(t, p, started, new(int)); err != nil {
		t.Fatal(err)
	}
	of := func(run runs.Run, attempt int, meta map[string]string) runtime.Request {
		return runtime.Request{Tenant: "t", RunID: run.ID, Attempt: attempt, Meta: meta}
	}
	saved := map[string]string{wire.MetaAnswerShape: wire.ShapeReply, "github_repo": "site"}
	for _, tc := range []struct {
		name string
		req  runtime.Request
		want string
	}{
		{"a later attempt of a started review", of(started, 2, saved), wire.ShapeReview},
		{"its first attempt", of(started, 1, saved), wire.ShapeReply},
		{"a later attempt of a reply", of(reply, 3, saved), wire.ShapeReply},
		{"a later attempt with no shape", of(started, 2, map[string]string{"github_repo": "site"}), ""},
		{"a later attempt of no recorded run", runtime.Request{Tenant: "t", RunID: "nowhere", Attempt: 2, Meta: saved}, wire.ShapeReply},
	} {
		got := p.withStartedReview(tc.req)
		if got.Meta[wire.MetaAnswerShape] != tc.want || got.Meta["github_repo"] != "site" {
			t.Errorf("%s reads %v, want shape %q", tc.name, got.Meta, tc.want)
		}
	}
	if saved[wire.MetaAnswerShape] != wire.ShapeReply {
		t.Fatal("the request's own context was changed")
	}
	if text := event.RequestText("have a look", p.withStartedReview(of(started, 2, saved)).Meta); !strings.Contains(text, "- answer_shape: review") {
		t.Fatalf("the later attempt's request reads %q", text)
	}
}

func shapeClient(tenant string) clients.Client { return clients.Client{Tenant: tenant} }

func shapeView(r runs.Run) transport.View { return transport.View{Tenant: r.Tenant, RunID: r.ID} }
