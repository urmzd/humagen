package github

import (
	"context"
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
	// Trusted says the run was an automatic review, a review request, or
	// started by a person the client maps.
	Trusted bool
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
	own := transport.ApprovalRequest{RunID: v.RunID, App: strings.TrimSuffix(strings.ToLower(strings.TrimSpace(cfg.Login)), "[bot]")}
	facts := presentation.ReviewFacts{Author: grant.Author, App: cfg.Login, Trusted: grant.Trusted, Own: own.OwnCheck}
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
	return &approvals{now: clock, pending: map[string]pendingApproval{}}
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
		facts.Author, facts.Draft, facts.Own = req.Author, req.Draft, req.OwnCheck
		return presentation.ReviewEvent(p.report, policy, facts) == "APPROVE", nil
	}
}
