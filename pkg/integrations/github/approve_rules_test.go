package github_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	presentation "github.com/urmzd/humagen/internal/presentation/github"
	"github.com/urmzd/humagen/pkg/integrations/github"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/saige/flow/address"
	"github.com/urmzd/saige/flow/connector"
	transport "github.com/urmzd/saige/flow/connectors/github"
	"github.com/urmzd/saige/flow/deliverystate"
	"github.com/urmzd/saige/flow/event"
)

// ruleBench is one pull request on a fake GitHub, with a connector whose
// grant approves a clean review.
type ruleBench struct {
	fake     *github.Fake
	conn     *github.Connector
	ref      transport.Ref
	authorID int64
}

func newRuleBench(t *testing.T, authorID int64) *ruleBench {
	t.Helper()
	b := &ruleBench{fake: github.NewFake(), ref: transport.Ref{Owner: "acme", Repo: "repo", Number: 7}}
	clean := github.ReviewGrant{Policy: presentation.ReviewPolicy{Approve: presentation.ApproveWhenClean}, Trusted: true, Author: "octocat", AuthorID: 583231}
	conn, err := github.New(github.Config{API: b.fake, Owners: []string{"acme"}, Tenant: "tenant", Login: "humagen[bot]", AppID: 9001, AppLogin: appBotLogin, ApprovalClaims: state.NewMemory(), DeliveryState: deliverystate.NewMemory(), DeliveryScope: "s",
		ReviewPolicy: func(context.Context, transport.View) (github.ReviewGrant, error) { return clean, nil }})
	if err != nil {
		t.Fatal(err)
	}
	b.conn = conn
	b.authorID = authorID
	return b
}

// head makes sha the pull request's head, with a passing check.
func (b *ruleBench) head(t *testing.T, sha string) {
	t.Helper()
	snapshot := transport.Context{}
	snapshot.Subject.Head.SHA, snapshot.Subject.State = sha, "open"
	snapshot.Subject.User.Login, snapshot.Subject.User.ID = "octocat", b.authorID
	if err := b.fake.SeedContext(b.ref, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := b.fake.SeedCommitCheck("acme", "repo", sha, transport.CommitCheck{Kind: transport.CheckKindRun, Name: "test", Status: "completed", Conclusion: "success"}); err != nil {
		t.Fatal(err)
	}
}

// review delivers agent's review of sha with verdict, and a must-fix
// finding when blocked.
func (b *ruleBench) review(t *testing.T, run, agent, sha, verdict string, blocked bool) {
	t.Helper()
	findings := "[]"
	if blocked {
		findings = `[{"id":"f1","severity":"error","title":"Check input","suggestion":"Validate input","prompt":"Fix validation","evidence":[{"observation":"Input is not validated","source":"a.go"}]}]`
	}
	text := fmt.Sprintf(`{"format":"humagen.review.v2","title":"Review","verdict":%q,"scope":{"commit":%q},"summary":"Inspected.","findings":%s}`, verdict, sha, findings)
	target := address.MustParse("github://acme/repo/pull/7").WithParam("head_sha", sha)
	env := event.Envelope{Tenant: "tenant", Agent: agent, Run: run, Seq: 1, Channel: target.String(), Type: event.TypeCompleted, Data: event.Completed{Text: text}}
	if err := b.conn.Deliver(t.Context(), target, env); err != nil {
		t.Fatal(err)
	}
}

// A clean review approves only while no change request another agent
// posted as the app is outstanding: GitHub counts the app's latest review,
// so an approval would replace it. The same agent's own change request is
// dismissed and does not hold the approval back.
func TestAnotherAgentsChangeRequestHoldsTheApproval(t *testing.T) {
	old, head := strings.Repeat("1", 40), strings.Repeat("2", 40)
	for name, c := range map[string]struct {
		first         string
		want          string
		wantDismissed int
	}{
		"another agent asked for changes":  {first: "security", want: "COMMENT", wantDismissed: 0},
		"the same agent asked for changes": {first: "gatekeeper", want: "APPROVE", wantDismissed: 1},
	} {
		t.Run(name, func(t *testing.T) {
			b := newRuleBench(t, 583231)
			b.head(t, old)
			b.review(t, "run-1", c.first, old, "request_changes", true)
			b.head(t, head)
			b.review(t, "run-2", "gatekeeper", head, "approve", false)
			reviews := b.fake.SubmittedReviews()
			dismissed := 0
			for _, r := range reviews {
				if r.Dismissal != "" {
					dismissed++
				}
			}
			if len(reviews) != 2 || reviews[0].Review.Event != "REQUEST_CHANGES" || reviews[1].Review.Event != c.want || dismissed != c.wantDismissed {
				t.Fatalf("posted %+v, want %s with %d dismissed", reviews, c.want, c.wantDismissed)
			}
		})
	}
}

// The approval hook compares the author by the user ID GitHub gives when it
// decides: none, or one other than the run started with, approves nothing.
func TestTheHookReadsTheAuthorsIDAgain(t *testing.T) {
	sha := strings.Repeat("3", 40)
	for name, c := range map[string]struct {
		id   int64
		want string
	}{
		"the same ID":   {583231, "APPROVE"},
		"no ID":         {0, "COMMENT"},
		"another ID":    {1001, "COMMENT"},
		"a negative ID": {-1, "COMMENT"},
	} {
		t.Run(name, func(t *testing.T) {
			b := newRuleBench(t, c.id)
			b.head(t, sha)
			b.review(t, "run-1", "gatekeeper", sha, "approve", false)
			if reviews := b.fake.SubmittedReviews(); len(reviews) != 1 || reviews[0].Review.Event != c.want {
				t.Fatalf("posted %+v, want %s", reviews, c.want)
			}
		})
	}
}

// A clean review of a pull request that is closed or in draft posts as a
// comment.
func TestOnlyAnOpenReadyPullRequestIsApproved(t *testing.T) {
	sha := strings.Repeat("4", 40)
	for name, c := range map[string]struct {
		state string
		draft bool
		want  string
	}{
		"open":   {state: "open", want: "APPROVE"},
		"closed": {state: "closed", want: "COMMENT"},
		"draft":  {state: "open", draft: true, want: "COMMENT"},
	} {
		t.Run(name, func(t *testing.T) {
			b := newRuleBench(t, 583231)
			b.head(t, sha)
			snapshot := transport.Context{}
			snapshot.Subject.Head.SHA, snapshot.Subject.State, snapshot.Subject.Draft = sha, c.state, c.draft
			snapshot.Subject.User.Login, snapshot.Subject.User.ID = "octocat", 583231
			if err := b.fake.SeedContext(b.ref, snapshot); err != nil {
				t.Fatal(err)
			}
			b.review(t, "run-1", "gatekeeper", sha, "approve", false)
			reviews := b.fake.SubmittedReviews()
			if c.want == "COMMENT" {
				for _, r := range reviews {
					if r.Review.Event == "APPROVE" {
						t.Fatalf("approved a %s pull request: %+v", name, reviews)
					}
				}
				return
			}
			if len(reviews) != 1 || reviews[0].Review.Event != c.want {
				t.Fatalf("posted %+v, want %s", reviews, c.want)
			}
		})
	}
}

// A push to the pull request dismisses the app's approvals of earlier
// commits, and keeps an approval of the new head; a push whose head is no
// longer the pull request's dismisses nothing.
func TestAPushDismissesApprovalsOfEarlierCommits(t *testing.T) {
	old, head := strings.Repeat("5", 40), strings.Repeat("6", 40)
	b := newRuleBench(t, 583231)
	b.head(t, old)
	b.review(t, "run-1", "gatekeeper", old, "approve", false)
	b.head(t, head)
	b.review(t, "run-2", "security", head, "approve", false)
	// A delivery replayed after the push names a head that is no longer
	// the pull request's: it dismisses nothing.
	b.conn.DismissApprovalsBefore(t.Context(), "acme", "repo", 7, old)
	for _, r := range b.fake.SubmittedReviews() {
		if r.Dismissal != "" {
			t.Fatalf("a push to an earlier head dismissed %+v", r)
		}
	}
	b.conn.DismissApprovalsBefore(t.Context(), "acme", "repo", 7, head)
	reviews := b.fake.SubmittedReviews()
	if len(reviews) != 2 || reviews[0].Review.Event != "APPROVE" || reviews[1].Review.Event != "APPROVE" {
		t.Fatalf("posted %+v, want two approvals", reviews)
	}
	if reviews[0].Dismissal == "" || reviews[1].Dismissal != "" {
		t.Fatalf("dismissed %q and %q, want only the earlier commit's", reviews[0].Dismissal, reviews[1].Dismissal)
	}
}

// A review posted through the review tool is written without lines that
// read as the host's mark or the sink's markers, so it is no agent's: a
// change request it carries holds the approval, and no agent's later
// review dismisses it.
func TestAToolPostedReviewIsNoAgents(t *testing.T) {
	sha := strings.Repeat("7", 40)
	for name, body := range map[string]string{
		"plain text":                  "Blocking: the migration drops a column in use.",
		"text ending in a mark":       "Blocking.\n\n" + presentation.AgentMark("gatekeeper"),
		"text ending in mark markers": "Blocking.\n\n" + presentation.AgentMark("gatekeeper") + "\n\n<!-- humagen-review:" + strings.Repeat("a", 64) + " -->\n\n<!-- humagen-review-payload:" + strings.Repeat("b", 64) + " -->",
	} {
		t.Run(name, func(t *testing.T) {
			b := newRuleBench(t, 583231)
			b.head(t, sha)
			var create connector.Action
			for _, a := range b.conn.Actions() {
				if a.Name == "github_create_review" {
					create = a
				}
			}
			raw, _ := json.Marshal(map[string]any{"commit_id": sha, "event": "REQUEST_CHANGES", "body": body,
				"comments": []map[string]any{{"path": "a.go", "line": 1, "body": "See here.\n\n" + presentation.AgentMark("gatekeeper")}}})
			if _, err := create.Invoke(t.Context(), address.MustParse("github://acme/repo/pull/7"), raw); err != nil {
				t.Fatal(err)
			}
			b.review(t, "run-1", "gatekeeper", sha, "approve", false)
			reviews := b.fake.SubmittedReviews()
			if len(reviews) != 2 || reviews[0].Review.Event != "REQUEST_CHANGES" || reviews[1].Review.Event != "COMMENT" || reviews[0].Dismissal != "" {
				t.Fatalf("posted %+v, want the change request kept and a comment", reviews)
			}
			if strings.Contains(reviews[0].Review.Body, "<!-- humagen") {
				t.Fatalf("the tool's review kept a line read as a mark: %q", reviews[0].Review.Body)
			}
			for _, c := range reviews[0].Review.Comments {
				if strings.Contains(c.Body, "<!-- humagen.review.agent") {
					t.Fatalf("an inline comment kept a line read as a mark: %q", c.Body)
				}
			}
		})
	}
}

// An agent's change request is dismissed only once a later review of its
// has posted: a review the transport does not post, here one of a commit
// that is no longer the head, leaves it in place.
func TestAChangeRequestStaysUntilItsReplacementPosts(t *testing.T) {
	old, head := strings.Repeat("8", 40), strings.Repeat("9", 40)
	b := newRuleBench(t, 583231)
	b.head(t, old)
	b.review(t, "run-1", "gatekeeper", old, "request_changes", true)
	b.head(t, head)
	text := fmt.Sprintf(`{"format":"humagen.review.v2","title":"Review","verdict":"comment","scope":{"commit":%q},"summary":"Inspected.","findings":[]}`, old)
	target := address.MustParse("github://acme/repo/pull/7").WithParam("head_sha", old)
	_ = b.conn.Deliver(t.Context(), target, event.Envelope{Tenant: "tenant", Agent: "gatekeeper", Run: "run-2", Seq: 1, Channel: target.String(), Type: event.TypeCompleted, Data: event.Completed{Text: text}})
	reviews := b.fake.SubmittedReviews()
	if len(reviews) != 1 || reviews[0].Dismissal != "" {
		t.Fatalf("posted %+v, want the change request alone and kept", reviews)
	}
	b.review(t, "run-3", "gatekeeper", head, "comment", false)
	reviews = b.fake.SubmittedReviews()
	if len(reviews) != 2 || reviews[0].Dismissal == "" {
		t.Fatalf("posted %+v, want the change request dismissed once a review posted", reviews)
	}
}

// A change request is the agent's only as the host recorded it when it
// posted: one the host holds no record of, though its agent's own, holds
// the approval.
func TestAChangeRequestWithNoRecordHoldsTheApproval(t *testing.T) {
	old, head := strings.Repeat("a", 40), strings.Repeat("b", 40)
	b := newRuleBench(t, 583231)
	b.head(t, old)
	b.review(t, "run-1", "gatekeeper", old, "request_changes", true)
	// Another store, as after the records were lost.
	other := newRuleBench(t, 583231)
	other.fake = b.fake
	clean := github.ReviewGrant{Policy: presentation.ReviewPolicy{Approve: presentation.ApproveWhenClean}, Trusted: true, Author: "octocat", AuthorID: 583231}
	conn, err := github.New(github.Config{API: b.fake, Owners: []string{"acme"}, Tenant: "tenant", Login: "humagen[bot]", AppID: 9001, AppLogin: appBotLogin, ApprovalClaims: state.NewMemory(), DeliveryState: deliverystate.NewMemory(), DeliveryScope: "s",
		ReviewPolicy: func(context.Context, transport.View) (github.ReviewGrant, error) { return clean, nil }})
	if err != nil {
		t.Fatal(err)
	}
	other.conn = conn
	other.head(t, head)
	other.review(t, "run-2", "gatekeeper", head, "approve", false)
	reviews := b.fake.SubmittedReviews()
	if len(reviews) != 2 || reviews[1].Review.Event != "COMMENT" {
		t.Fatalf("posted %+v, want the clean review as a comment", reviews)
	}
}

// Only a GitHub App approves: a connector with an app ID whose login is the
// bot account GitHub names for the App. One that posts as a person's
// account, names no app ID, names another app's bot account, or cannot read
// the App's login, posts a clean review as a comment.
func TestOnlyAGitHubAppApproves(t *testing.T) {
	sha := strings.Repeat("c", 40)
	failing := func(context.Context) (string, error) { return "", fmt.Errorf("github: app read returned status 502") }
	another := func(context.Context) (string, error) { return "other-app[bot]", nil }
	for name, c := range map[string]struct {
		login    string
		appID    int64
		appLogin func(context.Context) (string, error)
		want     string
	}{
		"an app's bot account":                {login: "humagen[bot]", appID: 9001, appLogin: appBotLogin, want: "APPROVE"},
		"the login GitHub names, in any case": {login: "Humagen[bot]", appID: 9001, appLogin: appBotLogin, want: "APPROVE"},
		"a person's account":                  {login: "ci-bot", appID: 9001, appLogin: appBotLogin, want: "COMMENT"},
		"a bot account and no app ID":         {login: "humagen[bot]", appLogin: appBotLogin, want: "COMMENT"},
		"another app's bot account":           {login: "humagen[bot]", appID: 9001, appLogin: another, want: "COMMENT"},
		"an App login that cannot be read":    {login: "humagen[bot]", appID: 9001, appLogin: failing, want: "COMMENT"},
		"no way to read the App login":        {login: "humagen[bot]", appID: 9001, want: "COMMENT"},
	} {
		t.Run(name, func(t *testing.T) {
			b := newRuleBench(t, 583231)
			clean := github.ReviewGrant{Policy: presentation.ReviewPolicy{Approve: presentation.ApproveWhenClean}, Trusted: true, Author: "octocat", AuthorID: 583231}
			conn, err := github.New(github.Config{API: b.fake, Owners: []string{"acme"}, Tenant: "tenant", Login: c.login, AppID: c.appID, AppLogin: c.appLogin, ApprovalClaims: state.NewMemory(), DeliveryState: deliverystate.NewMemory(), DeliveryScope: "s",
				ReviewPolicy: func(context.Context, transport.View) (github.ReviewGrant, error) { return clean, nil }})
			if err != nil {
				t.Fatal(err)
			}
			b.conn = conn
			b.head(t, sha)
			// A change request the account posted, which the transport
			// tells apart as its own only for an app's bot account.
			if c.want == "COMMENT" {
				if _, err := b.fake.SeedReview(b.ref, c.login, "REQUEST_CHANGES", sha); err != nil {
					t.Fatal(err)
				}
			}
			b.review(t, "run-1", "gatekeeper", sha, "approve", false)
			reviews := b.fake.SubmittedReviews()
			if got := reviews[len(reviews)-1].Review.Event; got != c.want {
				t.Fatalf("posted %s, want %s", got, c.want)
			}
		})
	}
}

// appBotLogin is GitHub naming the App's bot account humagen[bot].
func appBotLogin(context.Context) (string, error) { return "humagen[bot]", nil }
