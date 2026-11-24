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
	transport "github.com/urmzd/saige/flow/connectors/github"
	"github.com/urmzd/saige/flow/deliverystate"
	"github.com/urmzd/saige/flow/event"
)

const replicaSHA = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"

func vseed(t *testing.T, fake *transport.Fake, sha string) {
	t.Helper()
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
}

// vconn is one replica's connector. Replicas share claims, the store every
// replica of a deployment shares.
func vconn(t *testing.T, api any, claims ...state.Store) *github.Connector {
	t.Helper()
	store := state.Store(state.NewMemory())
	if len(claims) > 0 {
		store = claims[0]
	}
	clean := github.ReviewGrant{Policy: presentation.ReviewPolicy{Approve: presentation.ApproveWhenClean}, Trusted: true, Author: "octocat", AuthorID: 583231}
	conn, err := github.New(github.Config{API: api.(transport.API), Owners: []string{"acme"}, Tenant: "tenant", Login: "humagen[bot]", AppID: 9001, AppLogin: appBotLogin, ApprovalClaims: store, DeliveryState: deliverystate.NewMemory(), DeliveryScope: "s",
		ReviewPolicy: func(context.Context, transport.View) (github.ReviewGrant, error) { return clean, nil }})
	if err != nil {
		t.Fatal(err)
	}
	return conn
}

func venv(run, sha, verdict, sev string) (address.Address, event.Envelope) {
	return venvBy("gatekeeper", run, sha, verdict, sev)
}

func venvBy(agent, run, sha, verdict, sev string) (address.Address, event.Envelope) {
	target := address.MustParse("github://acme/repo/pull/7").WithParam("head_sha", sha)
	findings := "[]"
	if sev != "" {
		findings = `[{"id":"f1","severity":"` + sev + `","title":"Check input","suggestion":"Validate input","prompt":"Fix validation","evidence":[{"observation":"Input is not validated","source":"a.go"}]}]`
	}
	text := fmt.Sprintf(`{"format":"humagen.review.v2","title":"Review","verdict":%q,"scope":{"commit":%q},"summary":"Validated behavior.","findings":%s}`, verdict, sha, findings)
	return target, event.Envelope{Tenant: "tenant", Agent: agent, Run: run, Seq: 1, Channel: target.String(), Type: event.TypeCompleted, Data: event.Completed{Text: text}}
}

// interleave is one GitHub shared by two replicas. When replica A posts its
// approval, replica B delivers its own review first: B's hook runs after A's
// OwnReviews read and before A's approval is listed.
type interleave struct {
	*transport.Fake
	other func()
	fired bool
}

func (i *interleave) CreateReview(ctx context.Context, ref transport.Ref, r transport.Review) (transport.ReviewResult, error) {
	if r.Event == "APPROVE" && !i.fired && i.other != nil {
		i.fired = true
		i.other()
	}
	return i.Fake.CreateReview(ctx, ref, r)
}

// Two replicas reviewing one commit at once approve it once: the claim is
// in the store they share, and the replica that loses posts a comment.
func TestTwoReplicasApproveOneCommitOnce(t *testing.T) {
	fake := transport.NewFake()
	vseed(t, fake, replicaSHA)
	shared := &interleave{Fake: fake}
	claims := state.NewMemory()
	a, b := vconn(t, shared, claims), vconn(t, fake, claims)
	shared.other = func() {
		target, env := venv("run-b", replicaSHA, "approve", "")
		if err := b.Deliver(t.Context(), target, env); err != nil {
			t.Error(err)
		}
	}
	target, env := venv("run-a", replicaSHA, "approve", "")
	if err := a.Deliver(t.Context(), target, env); err != nil {
		t.Fatal(err)
	}
	approvals := 0
	for _, r := range fake.SubmittedReviews() {
		if r.Review.Event == "APPROVE" {
			approvals++
		}
	}
	if reviews := fake.SubmittedReviews(); approvals != 1 || len(reviews) != 2 {
		t.Fatalf("two replicas approved one commit %d times: %+v", approvals, reviews)
	}
}

// failDismiss fails every dismissal.
type failDismiss struct{ *transport.Fake }

func (failDismiss) DismissReview(context.Context, transport.Ref, int64, string) error {
	return errors.New("github: 502 on dismissal")
}

// A dismissal that fails is logged and never stops the new review.
func TestADismissalThatFailsDoesNotStopTheReview(t *testing.T) {
	old, head := strings.Repeat("1", 40), strings.Repeat("2", 40)
	fake := transport.NewFake()
	conn := vconn(t, failDismiss{fake})
	vseed(t, fake, old)
	target, env := venv("run-0", old, "request_changes", "error")
	if err := conn.Deliver(t.Context(), target, env); err != nil {
		t.Fatal(err)
	}
	vseed(t, fake, head)
	target, env = venv("run-1", head, "comment", "")
	err := conn.Deliver(t.Context(), target, env)
	if n := len(fake.SubmittedReviews()); n != 2 || err != nil {
		t.Fatalf("dismissal failure blocked the new review: posted %d reviews, err %v", n, err)
	}
}

// A person's review, and the app's review of another pull request, are
// never dismissed, and a new approval leaves the earlier one.
func TestDismissalKeepsToTheAgentsOwnStaleReviews(t *testing.T) {
	old, head := strings.Repeat("1", 40), strings.Repeat("2", 40)
	fake := transport.NewFake()
	conn := vconn(t, fake)
	ref := transport.Ref{Owner: "acme", Repo: "repo", Number: 7}
	other := transport.Ref{Owner: "acme", Repo: "repo", Number: 8}
	if _, err := fake.SeedReview(ref, "ada", "REQUEST_CHANGES", old); err != nil {
		t.Fatal(err)
	}
	if _, err := fake.SeedReview(ref, "ada", "APPROVE", old); err != nil {
		t.Fatal(err)
	}
	if _, err := fake.SeedReview(other, "humagen[bot]", "REQUEST_CHANGES", old); err != nil {
		t.Fatal(err)
	}
	vseed(t, fake, old)
	target, env := venv("run-0", old, "approve", "")
	if err := conn.Deliver(t.Context(), target, env); err != nil {
		t.Fatal(err)
	}
	vseed(t, fake, head)
	target, env = venv("run-1", head, "approve", "")
	if err := conn.Deliver(t.Context(), target, env); err != nil {
		t.Fatal(err)
	}
	for _, r := range fake.SubmittedReviews() {
		if r.Dismissal != "" {
			t.Errorf("dismissed %+v (author %q, pr %d)", r.Review.Event, r.Author, r.Ref.Number)
		}
	}
}

// One agent's review never dismisses another agent's.
func TestAnotherAgentsReviewIsNeverDismissed(t *testing.T) {
	old, head := strings.Repeat("1", 40), strings.Repeat("2", 40)
	fake := transport.NewFake()
	conn := vconn(t, fake)
	vseed(t, fake, old)
	target, env := venvBy("security-reviewer", "run-0", old, "request_changes", "error")
	if err := conn.Deliver(t.Context(), target, env); err != nil {
		t.Fatal(err)
	}
	vseed(t, fake, head)
	target, env = venvBy("gatekeeper", "run-1", head, "comment", "")
	if err := conn.Deliver(t.Context(), target, env); err != nil {
		t.Fatal(err)
	}
	for _, r := range fake.SubmittedReviews() {
		if r.Dismissal != "" {
			t.Fatalf("another agent's review was dismissed: %+v", r)
		}
	}
}

// An earlier change request is dismissed only by a review where nothing
// blocks: a finding at block_on, or a request_changes verdict, keeps it.
func TestAChangeRequestIsDismissedOnlyWhenNothingBlocks(t *testing.T) {
	for name, c := range map[string]struct {
		verdict, sev string
		want         int
	}{
		"info below block_on":               {verdict: "comment", sev: "info", want: 1},
		"error at block_on":                 {verdict: "comment", sev: "error", want: 0},
		"verdict request_changes, no block": {verdict: "request_changes", sev: "", want: 0},
	} {
		t.Run(name, func(t *testing.T) {
			old, head := strings.Repeat("1", 40), strings.Repeat("2", 40)
			fake := transport.NewFake()
			conn := vconn(t, fake)
			vseed(t, fake, old)
			target, env := venv("run-0", old, "request_changes", "error")
			if err := conn.Deliver(t.Context(), target, env); err != nil {
				t.Fatal(err)
			}
			vseed(t, fake, head)
			target, env = venv("run-1", head, c.verdict, c.sev)
			if err := conn.Deliver(t.Context(), target, env); err != nil {
				t.Fatal(err)
			}
			n := 0
			for _, r := range fake.SubmittedReviews() {
				if r.Dismissal != "" {
					n++
				}
			}
			if n != c.want {
				t.Fatalf("dismissed %d, want %d", n, c.want)
			}
		})
	}
}
