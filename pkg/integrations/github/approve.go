package github

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	presentation "github.com/urmzd/humagen/internal/presentation/github"
	"github.com/urmzd/saige/flow/address"
	transport "github.com/urmzd/saige/flow/connectors/github"
)

// ReviewGrant is what the host says of the run a review answers for. The
// zero value approves nothing.
type ReviewGrant struct {
	// Policy is the answering agent's review policy, narrowed by the
	// repository's settings.
	Policy presentation.ReviewPolicy
	// Trusted says the run was an automatic review, or asked by a person
	// the client maps: by a comment, a review request, or resuming a paused
	// automatic review.
	Trusted bool
	// Asker is the login whose ask started or resumed the run, "" for an
	// automatic review nobody resumed. An asker who wrote the pull request
	// approves nothing.
	Asker string
	// Author is the pull request's author, as the run's signed event
	// carried it. "" is unknown, and approves nothing.
	Author string
}

// reviewDecision is the policy and the facts one review is decided on.
// Checks are read only when an approval is possible, so a review that
// cannot approve costs no API call.
func reviewDecision(ctx context.Context, cfg Config, v transport.View, report presentation.ReviewReport) (presentation.ReviewPolicy, presentation.ReviewFacts, error) {
	var grant ReviewGrant
	if cfg.ReviewPolicy != nil {
		var err error
		if grant, err = cfg.ReviewPolicy(ctx, v); err != nil {
			return presentation.ReviewPolicy{}, presentation.ReviewFacts{}, err
		}
	}
	facts := presentation.ReviewFacts{Author: grant.Author, App: cfg.Login, Trusted: grant.Trusted, Asker: grant.Asker}
	if grant.Policy.Approve != presentation.ApproveWhenClean || report.Verdict != "approve" || report.Scope == nil {
		return grant.Policy, facts, nil
	}
	owner, repo, ok := ownerRepo(v.Channel)
	api, readable := cfg.API.(transport.CommitChecksAPI)
	if !ok || !readable {
		return grant.Policy, facts, nil
	}
	// A read that fails, or reaches the bound, leaves the checks unread:
	// the review is then not approved.
	if checks, err := api.CommitChecks(ctx, owner, repo, report.Scope.Commit); err == nil {
		facts.ChecksRead, facts.Checks = true, checks
	}
	return grant.Policy, facts, nil
}

func ownerRepo(channel string) (owner, repo string, ok bool) {
	owner, repo, _, ok = pullOf(channel)
	return owner, repo, ok
}

// pullOf is the pull request a run's channel is on.
func pullOf(channel string) (owner, repo string, number int, ok bool) {
	a, err := address.Parse(channel)
	if err != nil || a.Connector != "github" || a.Workspace == "" || len(a.Path) < 3 || a.Path[1] != "pull" {
		return "", "", 0, false
	}
	number, err = strconv.Atoi(a.Path[2])
	if err != nil || number <= 0 {
		return "", "", 0, false
	}
	return a.Workspace, a.Path[0], number, true
}

// The bounds on approvals waiting for the transport's MayApprove hook. The
// sink asks the hook right after Review planned the approval, in the same
// delivery, so an entry older than this was never asked for.
const (
	approvalTTL     = 2 * time.Minute
	maxApprovalWait = 256
)

// approvals holds the report of each approval Review planned, by run, until
// the transport's sink asks MayApprove about that run. The hook is told the
// run but not its report, which the decision reads again.
type approvals struct {
	mu      sync.Mutex
	now     func() time.Time
	pending map[string]pendingApproval
	// granted are the commits the hook said yes to lately, so a second
	// run in this process cannot approve one again before GitHub lists
	// the first approval.
	granted map[string]time.Time
	// host is the connector, set once it is built.
	host func() reviewHost
}

type pendingApproval struct {
	view   transport.View
	report presentation.ReviewReport
	at     time.Time
}

func newApprovals(clock func() time.Time) *approvals {
	if clock == nil {
		clock = time.Now
	}
	return &approvals{now: clock, pending: map[string]pendingApproval{}, granted: map[string]time.Time{}}
}

func approvalKey(run, commit string) string { return run + "@" + strings.ToLower(commit) }

// plan records that Review planned an approval for v's run.
func (a *approvals) plan(v transport.View, report presentation.ReviewReport) {
	if v.RunID == "" || report.Scope == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	for k, p := range a.pending {
		if now.Sub(p.at) > approvalTTL {
			delete(a.pending, k)
		}
	}
	if len(a.pending) >= maxApprovalWait {
		return
	}
	a.pending[approvalKey(v.RunID, report.Scope.Commit)] = pendingApproval{view: v, report: report, at: now}
}

// take removes and returns the approval run planned for commit lately.
func (a *approvals) take(run, commit string) (pendingApproval, bool) {
	key := approvalKey(run, commit)
	a.mu.Lock()
	defer a.mu.Unlock()
	p, ok := a.pending[key]
	delete(a.pending, key)
	if !ok || a.now().Sub(p.at) > approvalTTL {
		return pendingApproval{}, false
	}
	return p, true
}

// mayApprove is the transport's MayApprove: it finds the run by the
// request's RunID, decides its planned approval again, reading the policy
// and the checks as they are now, with the pull request's author and draft
// state as the transport read them from GitHub and the run's own check
// skipped, and says yes only when ReviewEvent still approves. A request for
// another pull request, or one nothing planned, is no. A grant that cannot be
// read is an error, which posts nothing and is retried.
func (a *approvals) mayApprove(cfg Config) func(context.Context, transport.ApprovalRequest) (bool, error) {
	return func(ctx context.Context, req transport.ApprovalRequest) (bool, error) {
		if req.RunID == "" || req.Review.Event != "APPROVE" || req.Review.CommitID != req.Commit {
			return false, nil
		}
		p, ok := a.take(req.RunID, req.Commit)
		if !ok || p.report.Scope == nil || p.report.Scope.Commit != req.Commit {
			return false, nil
		}
		owner, repo, number, ok := pullOf(p.view.Channel)
		if !ok || !strings.EqualFold(owner, req.Owner) || !strings.EqualFold(repo, req.Repo) || number != req.Number {
			return false, nil
		}
		policy, facts, err := reviewDecision(ctx, cfg, p.view, p.report)
		if err != nil {
			return false, err
		}
		facts.Author, facts.Draft = req.Author, req.Draft
		if presentation.ReviewEvent(p.report, policy, facts) != "APPROVE" {
			return false, dismissStale(ctx, cfg, a.host(), refOf(req), req.Commit, "COMMENT", false)
		}
		// One approval for each commit, whichever run posts it.
		approved, err := a.approvedAlready(ctx, req)
		if err != nil || approved {
			return false, err
		}
		return true, nil
	}
}

// approvedAlready reports whether the app approved req's commit already:
// in GitHub's list of the pull request's reviews, or by a yes this hook
// gave lately, which GitHub may not list yet. The commit is claimed before
// the list is read, so two runs at once approve once.
func (a *approvals) approvedAlready(ctx context.Context, req transport.ApprovalRequest) (bool, error) {
	key := strings.ToLower(req.Owner + "/" + req.Repo + "@" + req.Commit)
	a.mu.Lock()
	now := a.now()
	for k, at := range a.granted {
		if now.Sub(at) > approvalTTL {
			delete(a.granted, k)
		}
	}
	if _, ok := a.granted[key]; ok {
		a.mu.Unlock()
		return true, nil
	}
	a.granted[key] = now
	a.mu.Unlock()
	rows, err := a.host().OwnReviews(ctx, req.Owner, req.Repo, req.Number)
	if err != nil {
		a.mu.Lock()
		delete(a.granted, key)
		a.mu.Unlock()
		return false, err
	}
	for _, row := range rows {
		if row.State == "APPROVED" && strings.EqualFold(row.CommitID, req.Commit) {
			return true, nil
		}
	}
	return false, nil
}

// staleReviewMessage is what a dismissed review says it was dismissed for.
const staleReviewMessage = "A later review of this pull request replaces this one."

// staleReviews are the app's earlier reviews a new review at commit, posted
// as event, makes stale: an approval of another commit when the new review
// does not approve, and a change request when the new review has no finding
// that blocks.
func staleReviews(own []transport.ReviewRecord, commit, event string, blocked bool) []int64 {
	var out []int64
	for _, row := range own {
		switch {
		case row.State == "APPROVED" && event != "APPROVE" && !strings.EqualFold(row.CommitID, commit):
			out = append(out, row.ID)
		case row.State == "CHANGES_REQUESTED" && !blocked:
			out = append(out, row.ID)
		}
	}
	return out
}

// reviewHost is what the hook and the review sink use of the transport's
// host-only review calls: the app's own reviews on a pull request, and the
// dismissal of one of them.
type reviewHost interface {
	OwnReviews(ctx context.Context, owner, repo string, number int) ([]transport.ReviewRecord, error)
	DismissReview(ctx context.Context, owner, repo string, number int, reviewID int64, message string) error
}

// dismissStale dismisses the reviews staleReviews names. An API that cannot
// dismiss dismisses nothing.
func dismissStale(ctx context.Context, cfg Config, host reviewHost, ref transport.Ref, commit, event string, blocked bool) error {
	if _, ok := cfg.API.(transport.ReviewDismissAPI); !ok || host == nil {
		return nil
	}
	own, err := host.OwnReviews(ctx, ref.Owner, ref.Repo, ref.Number)
	if err != nil {
		return err
	}
	for _, id := range staleReviews(own, commit, event, blocked) {
		if err := host.DismissReview(ctx, ref.Owner, ref.Repo, ref.Number, id, staleReviewMessage); err != nil && !errors.Is(err, transport.ErrNotOwnReview) {
			return err
		}
	}
	return nil
}

func refOf(req transport.ApprovalRequest) transport.Ref {
	return transport.Ref{Owner: req.Owner, Repo: req.Repo, Number: req.Number}
}
