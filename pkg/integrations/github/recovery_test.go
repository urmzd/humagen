package github_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	presentation "github.com/urmzd/humagen/internal/presentation/github"
	"github.com/urmzd/humagen/pkg/integrations/github"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/saige/flow/address"
	"github.com/urmzd/saige/flow/connectors"
	transport "github.com/urmzd/saige/flow/connectors/github"
	"github.com/urmzd/saige/flow/deliverystate"
	"github.com/urmzd/saige/flow/event"
)

// With no review policy an approve verdict is posted as a comment, and a
// retried delivery recovers that review instead of posting another.
func TestNativeReviewRecoveryKeepsTheReviewAndCurrentEvidenceChecks(t *testing.T) {
	api := github.NewFake()
	ref := transport.Ref{Owner: "acme", Repo: "repo", Number: 7}
	sha := strings.Repeat("a", 40)
	snapshot := transport.Context{Files: []transport.ContextFile{{Filename: "a.go", Patch: "@@ -1 +1 @@\n-old\n+new"}}}
	snapshot.Subject.Head.SHA = sha
	snapshot.Subject.State = "open"
	snapshot.Subject.User.Login = "octocat"
	if err := api.SeedContext(ref, snapshot); err != nil {
		t.Fatal(err)
	}
	target := address.MustParse("github://acme/repo/pull/7").WithParam("head_sha", sha)
	text := fmt.Sprintf(`{"format":"humagen.review.v2","title":"Review","verdict":"approve","scope":{"commit":%q},"summary":"Validated behavior.","findings":[{"id":"f1","severity":"info","title":"Preserve behavior","suggestion":"Keep this result","evidence":[{"observation":"Observed the result","source":"a.go:1","step":"test-1"}],"prompt":"Preserve this behavior","location":{"path":"a.go","line":1}}]}`, sha)
	evidence := true
	findings := 0
	cfg := github.Config{API: api, Tenant: "tenant", Login: "humagen[bot]", ReviewText: func(v transport.View) (string, error) {
		if !evidence {
			return "", errors.New("source evidence unavailable")
		}
		return v.Text, nil
	}, ObserveDelivery: func(_ context.Context, d connectors.Delivery) error {
		if d.Finding == "f1" {
			findings++
		}
		return nil
	}}
	env := event.Envelope{Tenant: "tenant", Agent: "gatekeeper", Run: "run", Seq: 1, Channel: target.String(), Type: event.TypeCompleted, Data: event.Completed{Text: text}}
	// Each delivery is a connector whose receipt was lost: a fresh scope in
	// the same durable store, which keeps the review's layout record.
	cfg.DeliveryState = deliverystate.NewMemory()
	for i := range 2 {
		cfg.DeliveryScope = fmt.Sprint("lost-receipt-", i)
		c, err := github.New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.Deliver(t.Context(), target, env); err != nil {
			t.Fatal(err)
		}
		snapshot.Subject.Head.SHA = strings.Repeat("b", 40)
		snapshot.Files = nil
		if err := api.SeedContext(ref, snapshot); err != nil {
			t.Fatal(err)
		}
	}
	if reviews := api.SubmittedReviews(); len(reviews) != 1 || reviews[0].Review.Event != "COMMENT" || findings != 2 {
		t.Fatalf("recovery duplicated or changed review: %+v findings=%d", reviews, findings)
	}
	evidence = false
	cfg.DeliveryScope = "lost-receipt-again"
	c, err := github.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Deliver(t.Context(), target, env); err == nil {
		t.Fatal("recovery bypassed current host evidence check")
	}
	if len(api.SubmittedReviews()) != 1 || findings != 2 {
		t.Fatal("failed recovery changed native review")
	}
}

// An approval the host posted is recovered as an approval: a retried
// delivery matches it and posts nothing more.
func TestARecoveredApprovalStaysOneApproval(t *testing.T) {
	api := github.NewFake()
	ref := transport.Ref{Owner: "acme", Repo: "repo", Number: 7}
	sha := strings.Repeat("a", 40)
	snapshot := transport.Context{Files: []transport.ContextFile{{Filename: "a.go", Patch: "@@ -1 +1 @@\n-old\n+new"}}}
	snapshot.Subject.Head.SHA = sha
	snapshot.Subject.State = "open"
	snapshot.Subject.User.Login = "octocat"
	if err := api.SeedContext(ref, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := api.SeedCommitCheck("acme", "repo", sha, transport.CommitCheck{Kind: transport.CheckKindRun, Name: "test", Status: "completed", Conclusion: "success"}); err != nil {
		t.Fatal(err)
	}
	target := address.MustParse("github://acme/repo/pull/7").WithParam("head_sha", sha)
	text := fmt.Sprintf(`{"format":"humagen.review.v2","title":"Review","verdict":"approve","scope":{"commit":%q},"summary":"Validated behavior.","findings":[]}`, sha)
	cfg := github.Config{API: api, Tenant: "tenant", Login: "humagen[bot]", ApprovalClaims: state.NewMemory(), DeliveryState: deliverystate.NewMemory(),
		ReviewPolicy: func(context.Context, transport.View) (github.ReviewGrant, error) {
			return github.ReviewGrant{Policy: presentation.ReviewPolicy{Approve: presentation.ApproveWhenClean}, Trusted: true, Author: "octocat", AuthorID: 583231}, nil
		}}
	env := event.Envelope{Tenant: "tenant", Agent: "gatekeeper", Run: "run", Seq: 1, Channel: target.String(), Type: event.TypeCompleted, Data: event.Completed{Text: text}}
	for i := range 2 {
		cfg.DeliveryScope = fmt.Sprint("lost-receipt-", i)
		c, err := github.New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.Deliver(t.Context(), target, env); err != nil {
			t.Fatal(err)
		}
	}
	if reviews := api.SubmittedReviews(); len(reviews) != 1 || reviews[0].Review.Event != "APPROVE" {
		t.Fatalf("recovery duplicated or changed the approval: %+v", reviews)
	}
}
