package github_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	presentation "github.com/urmzd/humagen/internal/presentation/github"
	"github.com/urmzd/humagen/pkg/integrations/github"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/saige/flow/address"
	transport "github.com/urmzd/saige/flow/connectors/github"
	"github.com/urmzd/saige/flow/deliverystate"
	"github.com/urmzd/saige/flow/event"
)

// The app approves one commit once: a second run's clean review of the same
// commit is posted as a comment.
func TestTwoRunsOnOneCommitApproveOnce(t *testing.T) {
	sha := strings.Repeat("e", 40)
	fake := github.NewFake()
	ref := transport.Ref{Owner: "acme", Repo: "repo", Number: 7}
	snapshot := transport.Context{}
	snapshot.Subject.Head.SHA = sha
	snapshot.Subject.State = "open"
	snapshot.Subject.User.Login, snapshot.Subject.User.ID = "octocat", 583231
	if err := fake.SeedContext(ref, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := fake.SeedCommitCheck("acme", "repo", sha, transport.CommitCheck{Kind: transport.CheckKindRun, Name: "test", Status: "completed", Conclusion: "success"}); err != nil {
		t.Fatal(err)
	}
	clean := github.ReviewGrant{Policy: presentation.ReviewPolicy{Approve: presentation.ApproveWhenClean}, Trusted: true, Author: "octocat", AuthorID: 583231}
	conn, err := github.New(github.Config{API: fake, Tenant: "tenant", Login: "humagen[bot]", ApprovalClaims: state.NewMemory(), DeliveryState: deliverystate.NewMemory(), DeliveryScope: "s",
		ReviewPolicy: func(context.Context, transport.View) (github.ReviewGrant, error) { return clean, nil }})
	if err != nil {
		t.Fatal(err)
	}
	target := address.MustParse("github://acme/repo/pull/7").WithParam("head_sha", sha)
	text := fmt.Sprintf(`{"format":"humagen.review.v2","title":"Review","verdict":"approve","scope":{"commit":%q},"summary":"Validated behavior.","findings":[]}`, sha)
	for _, run := range []string{"run-a", "run-b"} {
		env := event.Envelope{Tenant: "tenant", Agent: "gatekeeper", Run: run, Seq: 1, Channel: target.String(), Type: event.TypeCompleted, Data: event.Completed{Text: text}}
		if err := conn.Deliver(t.Context(), target, env); err != nil {
			t.Fatal(err)
		}
	}
	approvals := 0
	for _, r := range fake.SubmittedReviews() {
		if r.Review.Event == "APPROVE" {
			approvals++
		}
	}
	if reviews := fake.SubmittedReviews(); approvals != 1 || len(reviews) != 2 || reviews[1].Review.Event != "COMMENT" {
		t.Fatalf("two runs on one commit posted %d approvals: %+v", approvals, reviews)
	}
}

// A review on a new head dismisses the app's earlier reviews it replaces: an
// approval of another head when it does not approve, and a change request
// when nothing in it blocks.
func TestALaterReviewDismissesTheAppsStaleReviews(t *testing.T) {
	old, head := strings.Repeat("1", 40), strings.Repeat("2", 40)
	report := func(commit, verdict, severity string) string {
		findings := "[]"
		if severity != "" {
			findings = `[{"id":"f1","severity":"` + severity + `","title":"Check input","suggestion":"Validate input","prompt":"Fix validation","evidence":[{"observation":"Input is not validated","source":"a.go"}]}]`
		}
		return fmt.Sprintf(`{"format":"humagen.review.v2","title":"Review","verdict":%q,"scope":{"commit":%q},"summary":"Inspected.","findings":%s}`, verdict, commit, findings)
	}
	for name, c := range map[string]struct {
		first, then string // the first review's verdict and finding severity, then the second's
		firstSev    string
		thenSev     string
		want        int // reviews dismissed by the second
	}{
		"an approval, then a comment":           {first: "approve", then: "comment", thenSev: "warning", want: 1},
		"an approval, then a change request":    {first: "approve", then: "request_changes", want: 1},
		"a change request, then a clean review": {first: "request_changes", firstSev: "error", then: "comment", want: 1},
		"a change request, then another":        {first: "request_changes", firstSev: "error", then: "comment", thenSev: "error", want: 0},
		"a comment, then a comment":             {first: "comment", then: "comment", want: 0},
	} {
		t.Run(name, func(t *testing.T) {
			fake := github.NewFake()
			ref := transport.Ref{Owner: "acme", Repo: "repo", Number: 7}
			seed := func(sha string) {
				snapshot := transport.Context{}
				snapshot.Subject.Head.SHA, snapshot.Subject.State = sha, "open"
				snapshot.Subject.User.Login, snapshot.Subject.User.ID = "octocat", 583231
				if err := fake.SeedContext(ref, snapshot); err != nil {
					t.Fatal(err)
				}
				if err := fake.SeedCommitCheck("acme", "repo", sha, transport.CommitCheck{Kind: transport.CheckKindRun, Name: "test", Status: "completed", Conclusion: "success"}); err != nil {
					t.Fatal(err)
				}
			}
			clean := github.ReviewGrant{Policy: presentation.ReviewPolicy{Approve: presentation.ApproveWhenClean}, Trusted: true, Author: "octocat", AuthorID: 583231}
			conn, err := github.New(github.Config{API: fake, Owners: []string{"acme"}, Tenant: "tenant", Login: "humagen[bot]", ApprovalClaims: state.NewMemory(), DeliveryState: deliverystate.NewMemory(), DeliveryScope: "s",
				ReviewPolicy: func(context.Context, transport.View) (github.ReviewGrant, error) { return clean, nil }})
			if err != nil {
				t.Fatal(err)
			}
			for i, step := range []struct{ sha, verdict, sev string }{{old, c.first, c.firstSev}, {head, c.then, c.thenSev}} {
				seed(step.sha)
				target := address.MustParse("github://acme/repo/pull/7").WithParam("head_sha", step.sha)
				env := event.Envelope{Tenant: "tenant", Agent: "gatekeeper", Run: fmt.Sprint("run-", i), Seq: 1, Channel: target.String(), Type: event.TypeCompleted, Data: event.Completed{Text: report(step.sha, step.verdict, step.sev)}}
				if err := conn.Deliver(t.Context(), target, env); err != nil {
					t.Fatal(err)
				}
			}
			reviews := fake.SubmittedReviews()
			dismissed := 0
			for _, r := range reviews {
				if r.Dismissal != "" {
					dismissed++
				}
			}
			if len(reviews) != 2 || dismissed != c.want || reviews[1].Dismissal != "" {
				t.Fatalf("dismissed %d, want %d of the first: %+v", dismissed, c.want, reviews)
			}
		})
	}
}
