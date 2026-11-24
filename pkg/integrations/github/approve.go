package github

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
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
	// People are the logins the client maps to a person, each with the
	// address it maps them by. A Co-authored-by trailer is someone's when
	// it carries their address, or their GitHub no-reply address.
	People map[string]string
	// Author is the pull request's author and AuthorID their GitHub user
	// ID, as the signed event carried them. An author unknown by ID
	// approves nothing.
	Author   string
	AuthorID int64
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
	facts := presentation.ReviewFacts{Author: grant.Author, AuthorID: grant.AuthorID, App: cfg.Login, AppID: cfg.AppID, Trusted: grant.Trusted, Asker: grant.Asker, AskerID: grant.AskerID}
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
	// Who wrote the commits is read only for a person who asked.
	if grant.AskerID > 0 {
		if api, readable := cfg.API.(transport.PullRequestCommitsAPI); readable {
			if commits, err := api.PullRequestCommits(ctx, owner, repo, number); err == nil {
				facts.CommitsRead, facts.AskerCommitted = commitsRead(commits, report.Scope.Commit, grant.AskerID)
				facts.CoAuthorsRead, facts.AskerCoAuthored = coAuthorsRead(commits, grant)
			}
		}
	}
	return grant.Policy, facts, nil
}

// commitsRead reads who wrote a pull request's commits for the person who
// asked, asker by GitHub user ID. read is true only for a whole list: one
// or more commits, fewer than saige's bound, the reviewed commit among them,
// and every commit's author and committer linked to a GitHub account
// (GitHub links them by email, so an unlinked one could be anyone). A
// commit GitHub's web-flow account committed (CommittedByWebFlow), a merge
// or an edit made on the web, is read by its author alone. committed is true
// when asker authored or committed one of them. A list that is not read
// approves nothing.
func commitsRead(commits []transport.PRCommit, reviewed string, asker int64) (read, committed bool) {
	if len(commits) == 0 || len(commits) >= transport.MaxPullRequestCommits || asker <= 0 {
		return false, false
	}
	found := false
	for _, c := range commits {
		web := c.CommittedByWebFlow()
		if c.AuthorID <= 0 || !web && c.CommitterID <= 0 {
			return false, false
		}
		found = found || strings.EqualFold(c.SHA, reviewed)
		committed = committed || c.AuthorID == asker || !web && c.CommitterID == asker
	}
	return found, committed
}

// noReply is a GitHub no-reply address: an optional user ID and a plus,
// then the login.
var noReply = regexp.MustCompile(`^(?:([0-9]+)\+)?([a-z0-9](?:[a-z0-9-]{0,38}))@users\.noreply\.github\.com$`)

// coAuthorsRead reads the Co-authored-by trailers of a pull request's
// commits for the person who asked. read is true only when every co-author
// is someone the host can name: an address the client maps a person by
// (grant.People), a GitHub no-reply address with a user ID, or a login-only
// no-reply address whose login is a mapped person's current login. A
// trailer with no address, or one nobody is known by, leaves the list
// unread. coAuthored is true when a co-author is, or may be, the asker: an
// address the asker is among the people mapped by, a no-reply address with
// the asker's user ID, or the asker's login.
func coAuthorsRead(commits []transport.PRCommit, grant ReviewGrant) (read, coAuthored bool) {
	if grant.AskerID <= 0 {
		return false, false
	}
	byEmail := map[string][]string{}
	mapped := map[string]bool{}
	for login, email := range grant.People {
		login = strings.ToLower(strings.TrimSpace(login))
		mapped[login] = true
		if email = strings.ToLower(strings.TrimSpace(email)); email != "" {
			byEmail[email] = append(byEmail[email], login)
		}
	}
	asker := strings.ToLower(strings.TrimSpace(grant.Asker))
	for _, c := range commits {
		for _, co := range c.CoAuthors {
			email := strings.ToLower(strings.TrimSpace(co.Email))
			if email == "" {
				return false, false
			}
			// Several people may share an address: it is the asker's when
			// it is any of theirs.
			if logins, ok := byEmail[email]; ok {
				coAuthored = coAuthored || slices.Contains(logins, asker)
				continue
			}
			m := noReply.FindStringSubmatch(email)
			if m == nil {
				return false, false
			}
			if id, _ := strconv.ParseInt(m[1], 10, 64); id > 0 {
				coAuthored = coAuthored || id == grant.AskerID
				continue
			}
			// A login-only address names whoever holds the login now; one
			// from before a rename is no one the host can name.
			if !mapped[m[2]] {
				return false, false
			}
			coAuthored = coAuthored || m[2] == asker
		}
	}
	return true, coAuthored
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
// state, the author's user ID and the app's outstanding reviews as the
// transport read them from GitHub, and says yes only when the app did not
// open the pull request, saige's ChecksAllow and ReviewEvent still approve,
// the app has not approved the commit already, every change request the app
// has outstanding is one the host recorded for this agent, and this run
// wins the commit's durable claim. A request for another pull
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
		// The author, the draft state and the app are as the transport read
		// them from GitHub. The author is compared by user ID; one whose ID
		// differs from the one the run started with is refused.
		facts.Author, facts.Draft, facts.App, facts.AppID = req.Author, req.Draft, req.App, req.AppID
		if facts.AuthorID > 0 && req.AuthorID != facts.AuthorID {
			facts.AuthorID = 0
		} else {
			facts.AuthorID = req.AuthorID
		}
		if req.AuthoredByApp() || !facts.ChecksRead || !req.ChecksAllow(facts.Checks) || presentation.ReviewEvent(p.report, policy, facts) != "APPROVE" {
			dismissStale(ctx, cfg, a.host(), refOf(req), p.view.Agent, req.Commit, "COMMENT")
			return false, nil
		}
		// One approval for each commit, whichever run or replica posts it:
		// none the app posted already, and the commit's claim, which only
		// one run ever wins. Every agent posts as the app, and GitHub counts
		// the app's latest review: an approval would replace a change request
		// the app has outstanding, so each must be this agent's, as the
		// host recorded when its review posted.
		for _, r := range req.AppReviews {
			if r.State == "APPROVED" && strings.EqualFold(r.CommitID, req.Commit) {
				return false, nil
			}
		}
		for _, r := range req.ChangesRequested() {
			agent, err := recordedAgent(ctx, cfg, r.Marker)
			if err != nil {
				return false, err
			}
			if agent == "" || agent != p.view.Agent {
				return false, nil
			}
		}
		return claimApproval(ctx, cfg, req)
	}
}

// reviewAgents is the namespace the agent of each review the sink posts is
// kept in, by the review's delivery marker (transport.ReviewMarker).
const reviewAgents = "github_review_agents"

// recordReviewAgent keeps that agent wrote the review the sink posts for
// v's run at commit, before it posts. The record is created once and never
// changed; a record that names another agent fails the review, which then
// does not post. Without a store nothing is kept, and a change request of that
// review is then no agent's.
func recordReviewAgent(ctx context.Context, cfg Config, v transport.View, commit string) error {
	if cfg.ApprovalClaims == nil || presentation.AgentMark(v.Agent) == "" || v.RunID == "" {
		return nil
	}
	key := markerKey(transport.ReviewMarker(v.Tenant, v.RunID, commit))
	err := cfg.ApprovalClaims.Write(ctx, reviewAgents, state.Create(cfg.Tenant, key, []byte(v.Agent)))
	if !errors.Is(err, state.ErrConflict) {
		return err
	}
	// Kept already, by a retry of this review: it must name the same agent.
	held, err := cfg.ApprovalClaims.Get(ctx, reviewAgents, cfg.Tenant, key)
	if err != nil {
		return err
	}
	if string(held) != v.Agent {
		return fmt.Errorf("github: the review of run %s is recorded for another agent", v.RunID)
	}
	return nil
}

// recordedAgent is the agent the host recorded for a review's delivery
// marker, "" for a marker it holds no record of, or none.
func recordedAgent(ctx context.Context, cfg Config, marker string) (string, error) {
	key := markerKey(marker)
	if cfg.ApprovalClaims == nil || key == "" {
		return "", nil
	}
	agent, err := cfg.ApprovalClaims.Get(ctx, reviewAgents, cfg.Tenant, key)
	if errors.Is(err, state.ErrNotFound) {
		return "", nil
	}
	return string(agent), err
}

// deliveryMarker is the delivery marker the review sink writes, and the
// digest that names it.
var deliveryMarker = regexp.MustCompile(`^<!-- humagen-review:([0-9a-f]{64}) -->$`)

// markerKey is the digest a delivery marker carries, "" for anything else.
func markerKey(marker string) string {
	m := deliveryMarker.FindStringSubmatch(strings.TrimSpace(marker))
	if m == nil {
		return ""
	}
	return m[1]
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

// staleReviews are the earlier approvals of agent's that a new review at
// commit, posted as event, makes stale: an approval of another commit when
// the new review does not approve. Only reviews whose host-written mark
// names agent (presentation.ReviewAgent) are named: another agent's review,
// one posted another way and one from before reviews were marked stay.
func staleReviews(own []transport.ReviewRecord, agent, commit, event string) []int64 {
	if presentation.AgentMark(agent) == "" || event == "APPROVE" {
		return nil
	}
	var out []int64
	for _, row := range own {
		if row.State == "APPROVED" && !strings.EqualFold(row.CommitID, commit) && presentation.ReviewAgent(row.Body) == agent {
			out = append(out, row.ID)
		}
	}
	return out
}

// replacedChangeRequests are agent's change requests that a later review of
// agent's, posted and not itself a change request, replaces: every one
// older than agent's latest review, when that review commented or
// approved. While agent's latest review asks for changes, none is named.
func replacedChangeRequests(own []transport.ReviewRecord, agent string) []int64 {
	if presentation.AgentMark(agent) == "" {
		return nil
	}
	var latest *transport.ReviewRecord
	for i, row := range own {
		if row.State == "DISMISSED" || presentation.ReviewAgent(row.Body) != agent {
			continue
		}
		if latest == nil || row.ID > latest.ID {
			latest = &own[i]
		}
	}
	if latest == nil || latest.State != "COMMENTED" && latest.State != "APPROVED" {
		return nil
	}
	var out []int64
	for _, row := range own {
		if row.State == "CHANGES_REQUESTED" && row.ID < latest.ID && presentation.ReviewAgent(row.Body) == agent {
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

// dismissStale dismisses the approvals staleReviews names. It never stops
// the new review: a read or a dismissal that fails is logged, and the review
// posts. An API that cannot dismiss dismisses nothing.
func dismissStale(ctx context.Context, cfg Config, host reviewHost, ref transport.Ref, agent, commit, event string) {
	dismissNamed(ctx, cfg, host, ref, func(own []transport.ReviewRecord) []int64 { return staleReviews(own, agent, commit, event) })
}

// dismissReplaced dismisses the change requests replacedChangeRequests
// names, once a review has posted. A read or a dismissal that fails is
// logged.
func dismissReplaced(ctx context.Context, cfg Config, host reviewHost, ref transport.Ref, agent string) {
	dismissNamed(ctx, cfg, host, ref, func(own []transport.ReviewRecord) []int64 { return replacedChangeRequests(own, agent) })
}

// dismissNamed reads the app's own reviews and dismisses those named.
func dismissNamed(ctx context.Context, cfg Config, host reviewHost, ref transport.Ref, named func([]transport.ReviewRecord) []int64) {
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
	for _, id := range named(own) {
		if err := host.DismissReview(ctx, ref.Owner, ref.Repo, ref.Number, id, staleReviewMessage); err != nil && !errors.Is(err, transport.ErrNotOwnReview) {
			log.Warn("github: a stale review could not be dismissed", "repository", ref.Owner+"/"+ref.Repo, "pull", ref.Number, "review", id, "error", err)
		}
	}
}

func refOf(req transport.ApprovalRequest) transport.Ref {
	return transport.Ref{Owner: req.Owner, Repo: req.Repo, Number: req.Number}
}

// DismissApprovalsBefore dismisses the app's approvals of a pull request
// that are on a commit other than head, once a push moved it there and
// while head is still the pull request's head. Only a
// review whose host-written mark names an agent is dismissed. A read or a
// dismissal that fails is logged; an API that cannot dismiss dismisses
// nothing.
func (c *Connector) DismissApprovalsBefore(ctx context.Context, owner, repo string, number int, head string) {
	if _, ok := c.cfg.API.(transport.ReviewDismissAPI); !ok || c.Connector == nil {
		return
	}
	log := c.cfg.Log
	if log == nil {
		log = slog.Default()
	}
	// A delivery replayed after later pushes names a head that is no
	// longer the pull request's: it dismisses nothing.
	reader, ok := c.cfg.API.(transport.ContextAPI)
	if !ok {
		return
	}
	now, err := reader.ReadContext(ctx, transport.Ref{Owner: owner, Repo: repo, Number: number}, 1)
	if err != nil {
		log.Warn("github: the pull request's head could not be read, so no approval was dismissed", "repository", owner+"/"+repo, "pull", number, "error", err)
		return
	}
	if !strings.EqualFold(now.Subject.Head.SHA, head) {
		return
	}
	own, err := c.OwnReviews(ctx, owner, repo, number)
	if err != nil {
		log.Warn("github: earlier approvals could not be read, so none were dismissed", "repository", owner+"/"+repo, "pull", number, "error", err)
		return
	}
	for _, row := range own {
		if row.State != "APPROVED" || strings.EqualFold(row.CommitID, head) || presentation.ReviewAgent(row.Body) == "" {
			continue
		}
		if err := c.DismissReview(ctx, owner, repo, number, row.ID, staleReviewMessage); err != nil && !errors.Is(err, transport.ErrNotOwnReview) {
			log.Warn("github: an approval of an earlier commit could not be dismissed", "repository", owner+"/"+repo, "pull", number, "review", row.ID, "error", err)
		}
	}
}
