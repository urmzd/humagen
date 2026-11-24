package github_test

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	presentation "github.com/urmzd/humagen/internal/presentation/github"
	"github.com/urmzd/humagen/pkg/integrations/github"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/saige/flow/address"
	transport "github.com/urmzd/saige/flow/connectors/github"
	"github.com/urmzd/saige/flow/deliverystate"
	"github.com/urmzd/saige/flow/event"
)

// checksThatTurn passes the checks on the first read and fails them on every
// read after, as a check that fails while the approval waits.
type checksThatTurn struct {
	*transport.Fake
	reads atomic.Int32
}

func (c *checksThatTurn) CommitChecks(ctx context.Context, owner, repo, sha string) ([]transport.CommitCheck, error) {
	if c.reads.Add(1) == 1 {
		return []transport.CommitCheck{{Kind: transport.CheckKindRun, Name: "test", Status: "completed", Conclusion: "success"}}, nil
	}
	return []transport.CommitCheck{{Kind: transport.CheckKindRun, Name: "test", Status: "completed", Conclusion: "failure"}}, nil
}

// The approval hook decides again with what is true when the transport asks:
// a policy or a check that changed after the review was planned posts a
// comment, and a grant that cannot be read posts nothing and is retried.
func TestTheApprovalHookDecidesAgain(t *testing.T) {
	sha := strings.Repeat("c", 40)
	text := fmt.Sprintf(`{"format":"humagen.review.v2","title":"Review","verdict":"approve","scope":{"commit":%q},"summary":"Validated behavior.","findings":[]}`, sha)
	clean := github.ReviewGrant{Policy: presentation.ReviewPolicy{Approve: presentation.ApproveWhenClean}, Trusted: true, Author: "octocat", AuthorID: 583231}
	for name, c := range map[string]struct {
		turn  bool
		grant func(call int) (github.ReviewGrant, error)
		want  string
	}{
		"nothing changed": {grant: func(int) (github.ReviewGrant, error) { return clean, nil }, want: "APPROVE"},
		"a check failed":  {turn: true, grant: func(int) (github.ReviewGrant, error) { return clean, nil }, want: "COMMENT"},
		"the policy became never": {grant: func(call int) (github.ReviewGrant, error) {
			if call > 1 {
				return github.ReviewGrant{Trusted: true, Author: "octocat", AuthorID: 583231}, nil
			}
			return clean, nil
		}, want: "COMMENT"},
		"the grant could not be read": {grant: func(call int) (github.ReviewGrant, error) {
			if call > 1 {
				return github.ReviewGrant{}, fmt.Errorf("unavailable")
			}
			return clean, nil
		}, want: ""},
	} {
		t.Run(name, func(t *testing.T) {
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
			var api transport.API = fake
			if c.turn {
				api = &checksThatTurn{Fake: fake}
			}
			var calls atomic.Int32
			conn, err := github.New(github.Config{API: api, Tenant: "tenant", Login: "humagen[bot]", AppID: 9001, ApprovalClaims: state.NewMemory(), DeliveryState: deliverystate.NewMemory(), DeliveryScope: "s",
				ReviewPolicy: func(context.Context, transport.View) (github.ReviewGrant, error) { return c.grant(int(calls.Add(1))) }})
			if err != nil {
				t.Fatal(err)
			}
			target := address.MustParse("github://acme/repo/pull/7").WithParam("head_sha", sha)
			env := event.Envelope{Tenant: "tenant", Agent: "gatekeeper", Run: "run", Seq: 1, Channel: target.String(), Type: event.TypeCompleted, Data: event.Completed{Text: text}}
			err = conn.Deliver(t.Context(), target, env)
			if c.want == "" {
				// An answer the hook cannot read posts nothing; the
				// delivery is retried.
				if err == nil || len(fake.SubmittedReviews()) != 0 {
					t.Fatalf("an unreadable grant posted %+v, %v", fake.SubmittedReviews(), err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if reviews := fake.SubmittedReviews(); len(reviews) != 1 || reviews[0].Review.Event != c.want {
				t.Fatalf("posted %+v, want one %s", reviews, c.want)
			}
		})
	}
}

// Without a review policy nothing approves, whatever the report and the
// checks say.
func TestNoReviewPolicyApprovesNothing(t *testing.T) {
	sha := strings.Repeat("d", 40)
	fake := github.NewFake()
	snapshot := transport.Context{}
	snapshot.Subject.Head.SHA = sha
	snapshot.Subject.State = "open"
	snapshot.Subject.User.Login, snapshot.Subject.User.ID = "octocat", 583231
	if err := fake.SeedContext(transport.Ref{Owner: "acme", Repo: "repo", Number: 7}, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := fake.SeedCommitCheck("acme", "repo", sha, transport.CommitCheck{Kind: transport.CheckKindRun, Name: "test", Status: "completed", Conclusion: "success"}); err != nil {
		t.Fatal(err)
	}
	conn, err := github.New(github.Config{API: fake, Tenant: "tenant", Login: "humagen[bot]", AppID: 9001, ApprovalClaims: state.NewMemory(), DeliveryState: deliverystate.NewMemory(), DeliveryScope: "s"})
	if err != nil {
		t.Fatal(err)
	}
	target := address.MustParse("github://acme/repo/pull/7").WithParam("head_sha", sha)
	text := fmt.Sprintf(`{"format":"humagen.review.v2","title":"Review","verdict":"approve","scope":{"commit":%q},"summary":"Validated behavior.","findings":[]}`, sha)
	if err := conn.Deliver(t.Context(), target, event.Envelope{Tenant: "tenant", Agent: "gatekeeper", Run: "run", Seq: 1, Channel: target.String(), Type: event.TypeCompleted, Data: event.Completed{Text: text}}); err != nil {
		t.Fatal(err)
	}
	if reviews := fake.SubmittedReviews(); len(reviews) != 1 || reviews[0].Review.Event != "COMMENT" {
		t.Fatalf("posted %+v, want one comment", reviews)
	}
}
