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
	facts := presentation.ReviewFacts{Author: grant.Author, App: cfg.Login, Trusted: grant.Trusted}
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

// approvals holds each approval Review planned until the transport's sink
// asks MayApprove about it. The hook is told only the repository and the
// review, so this is how it finds the run the review answers for, and
// decides again with what is true when it asks. Two runs planning an
// approval of the same commit at once approve neither.
type approvals struct {
	mu      sync.Mutex
	now     func() time.Time
	pending map[string]pendingApproval
}

type pendingApproval struct {
	view      transport.View
	report    presentation.ReviewReport
	at        time.Time
	ambiguous bool
}

func newApprovals(clock func() time.Time) *approvals {
	if clock == nil {
		clock = time.Now
	}
	return &approvals{now: clock, pending: map[string]pendingApproval{}}
}

func approvalKey(owner, repo string, number int, commit string) string {
	return strings.ToLower(owner + "/" + repo + "#" + strconv.Itoa(number) + "@" + commit)
}

// plan records that Review planned an approval for v's run.
func (a *approvals) plan(v transport.View, report presentation.ReviewReport) {
	owner, repo, number, ok := pullOf(v.Channel)
	if !ok || report.Scope == nil {
		return
	}
	key := approvalKey(owner, repo, number, report.Scope.Commit)
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	for k, p := range a.pending {
		if now.Sub(p.at) > approvalTTL {
			delete(a.pending, k)
		}
	}
	if p, ok := a.pending[key]; ok && p.view.RunID != v.RunID {
		p.ambiguous, p.at = true, now
		a.pending[key] = p
		return
	}
	if len(a.pending) >= maxApprovalWait {
		return
	}
	a.pending[key] = pendingApproval{view: v, report: report, at: now}
}

// take removes and returns the approval planned for this commit, if exactly
// one run planned it lately.
func (a *approvals) take(owner, repo string, number int, commit string) (pendingApproval, bool) {
	key := approvalKey(owner, repo, number, commit)
	a.mu.Lock()
	defer a.mu.Unlock()
	p, ok := a.pending[key]
	delete(a.pending, key)
	if !ok || p.ambiguous || a.now().Sub(p.at) > approvalTTL {
		return pendingApproval{}, false
	}
	return p, true
}

// mayApprove is the transport's MayApprove: it decides the planned
// approval again, reading the policy and the checks as they are now, with
// the pull request's author and draft state as the transport read them from
// GitHub, and says yes only when ReviewEvent still approves. An approval
// nothing planned is no. A grant that cannot be read is an error, which
// posts nothing and is retried.
func (a *approvals) mayApprove(cfg Config) func(context.Context, transport.ApprovalRequest) (bool, error) {
	return func(ctx context.Context, req transport.ApprovalRequest) (bool, error) {
		if req.Review.Event != "APPROVE" || req.Review.CommitID != req.Commit {
			return false, nil
		}
		p, ok := a.take(req.Owner, req.Repo, req.Number, req.Commit)
		if !ok || p.report.Scope == nil || p.report.Scope.Commit != req.Commit {
			return false, nil
		}
		policy, facts, err := reviewDecision(ctx, cfg, p.view, p.report)
		if err != nil {
			return false, err
		}
		facts.Author, facts.Draft = req.Author, req.Draft
		return presentation.ReviewEvent(p.report, policy, facts) == "APPROVE", nil
	}
}
