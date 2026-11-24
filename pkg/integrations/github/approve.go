package github

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	presentation "github.com/urmzd/humagen/internal/presentation/github"
	"github.com/urmzd/humagen/pkg/state"
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
	// automatic review nobody resumed, and AskerID their GitHub user ID, 0
	// when the signed event did not carry it. An asker approves only when
	// known by ID, not the author, and a writer of none of the commits.
	Asker   string
	AskerID int64
	// Author is the pull request's author and AuthorID their GitHub user
	// ID, as the signed event carried them. An author unknown by ID
	// approves nothing.
	Author   string
	AuthorID int64
}

// PullCommit is who wrote one commit of a pull request, by GitHub user ID:
// 0 for a commit GitHub matched to no account.
type PullCommit struct {
	AuthorID, CommitterID int64
}

// PullCommitsAPI reads who wrote each commit of a pull request, every one
// of them or an error. An API without it leaves the commits unread, and a
// review a person asked for then approves nothing.
type PullCommitsAPI interface {
	PullCommits(ctx context.Context, ref transport.Ref) ([]PullCommit, error)
}

// reviewDecision is the policy and the facts one review is decided on.
// Checks and commits are read only when an approval is possible, so a
// review that cannot approve costs no API call.
func reviewDecision(ctx context.Context, cfg Config, v transport.View, report presentation.ReviewReport) (presentation.ReviewPolicy, presentation.ReviewFacts, error) {
	var grant ReviewGrant
	if cfg.ReviewPolicy != nil {
		var err error
		if grant, err = cfg.ReviewPolicy(ctx, v); err != nil {
			return presentation.ReviewPolicy{}, presentation.ReviewFacts{}, err
		}
	}
	facts := presentation.ReviewFacts{Author: grant.Author, AuthorID: grant.AuthorID, App: cfg.Login, Trusted: grant.Trusted, Asker: grant.Asker, AskerID: grant.AskerID}
	if grant.Policy.Approve != presentation.ApproveWhenClean || report.Verdict != "approve" || report.Scope == nil {
		return grant.Policy, facts, nil
	}
	owner, repo, number, ok := pullOf(v.Channel)
	if !ok {
		return grant.Policy, facts, nil
	}
	// A read that fails, or reaches the bound, leaves the checks unread:
	// the review is then not approved.
	if api, readable := cfg.API.(transport.CommitChecksAPI); readable {
		if checks, err := api.CommitChecks(ctx, owner, repo, report.Scope.Commit); err == nil {
			facts.ChecksRead, facts.Checks = true, checks
		}
	}
	if grant.AskerID > 0 {
		if api, readable := cfg.API.(PullCommitsAPI); readable {
			if commits, err := api.PullCommits(ctx, transport.Ref{Owner: owner, Repo: repo, Number: number}); err == nil {
				facts.CommitsRead = true
				for _, c := range commits {
					facts.AskerCommitted = facts.AskerCommitted || c.AuthorID == grant.AskerID || c.CommitterID == grant.AskerID
				}
			}
		}
	}
	return grant.Policy, facts, nil
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
// state as the transport read them from GitHub, and says yes only when
// ReviewEvent still approves, the app has not approved the commit already,
// and this run wins the commit's durable claim. A request for another pull
// request, or one nothing planned, is no. A grant that cannot be read is an
// error, which posts nothing and is retried.
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
			dismissStale(ctx, cfg, a.host(), refOf(req), p.view.Agent, req.Commit, "COMMENT", false)
			return false, nil
		}
		// One approval for each commit, whichever run or replica posts it:
		// none the app posted already, and the commit's claim, which only
		// one run ever wins.
		rows, err := a.host().OwnReviews(ctx, req.Owner, req.Repo, req.Number)
		if err != nil {
			return false, err
		}
		for _, row := range rows {
			if row.State == "APPROVED" && strings.EqualFold(row.CommitID, req.Commit) {
				return false, nil
			}
		}
		return claimApproval(ctx, cfg, req)
	}
}

// approvalClaims is the namespace an approval's claim is kept in.
const approvalClaims = "github_approval_claims"

// claimApproval takes the one approval of req's commit for req's run: a
// record created once, in a store every replica shares. The run that
// creates it approves; any other is told no and posts a comment. Without a
// store nothing approves.
func claimApproval(ctx context.Context, cfg Config, req transport.ApprovalRequest) (bool, error) {
	if cfg.ApprovalClaims == nil {
		return false, nil
	}
	key := strings.ToLower(fmt.Sprintf("%s/%s#%d@%s", req.Owner, req.Repo, req.Number, req.Commit))
	err := cfg.ApprovalClaims.Write(ctx, approvalClaims, state.Create(cfg.Tenant, key, []byte(req.RunID)))
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, state.ErrConflict):
		// A retry of the run that won the claim keeps it.
		held, readErr := cfg.ApprovalClaims.Get(ctx, approvalClaims, cfg.Tenant, key)
		if readErr != nil {
			return false, readErr
		}
		return string(held) == req.RunID, nil
	}
	return false, err
}

// staleReviewMessage is what a dismissed review says it was dismissed for.
const staleReviewMessage = "A later review of this pull request replaces this one."

// staleReviews are the earlier reviews of agent a new review at commit,
// posted as event, makes stale: an approval of another commit when the new
// review does not approve, and a change request when the new review has no
// finding that blocks. Only reviews that carry agent's mark are named:
// another agent's review, and one from before reviews were marked, stay.
func staleReviews(own []transport.ReviewRecord, agent, commit, event string, blocked bool) []int64 {
	mark := presentation.AgentMark(agent)
	if mark == "" {
		return nil
	}
	var out []int64
	for _, row := range own {
		if !strings.Contains(row.Body, mark) {
			continue
		}
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

// dismissStale dismisses the reviews staleReviews names. It never stops the
// new review: a read or a dismissal that fails is logged, and the review
// posts. An API that cannot dismiss dismisses nothing.
func dismissStale(ctx context.Context, cfg Config, host reviewHost, ref transport.Ref, agent, commit, event string, blocked bool) {
	if _, ok := cfg.API.(transport.ReviewDismissAPI); !ok || host == nil {
		return
	}
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}
	own, err := host.OwnReviews(ctx, ref.Owner, ref.Repo, ref.Number)
	if err != nil {
		log.Warn("github: earlier reviews could not be read, so none were dismissed", "repository", ref.Owner+"/"+ref.Repo, "pull", ref.Number, "error", err)
		return
	}
	for _, id := range staleReviews(own, agent, commit, event, blocked) {
		if err := host.DismissReview(ctx, ref.Owner, ref.Repo, ref.Number, id, staleReviewMessage); err != nil && !errors.Is(err, transport.ErrNotOwnReview) {
			log.Warn("github: a stale review could not be dismissed", "repository", ref.Owner+"/"+ref.Repo, "pull", ref.Number, "review", id, "error", err)
		}
	}
}

func refOf(req transport.ApprovalRequest) transport.Ref {
	return transport.Ref{Owner: req.Owner, Repo: req.Repo, Number: req.Number}
}
