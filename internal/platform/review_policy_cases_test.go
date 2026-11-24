package platform

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	presentation "github.com/urmzd/humagen/internal/presentation/github"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/clients"
	ghconnector "github.com/urmzd/humagen/pkg/integrations/github"
	"github.com/urmzd/humagen/pkg/reposettings"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/saige/flow/address"
	transport "github.com/urmzd/saige/flow/connectors/github"
	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/runtime"
)

// hookedCase is a review delivery with hooks that change what the host reads
// while the review is decided.
type hookedCase struct {
	approvalCase
	// checksFor builds the commit's checks once the run ID is known.
	checksFor func(runID string) []transport.CommitCheck
	// between runs before the grant is read the n-th time (1: Review's
	// plan, 2: the MayApprove hook).
	between func(t *testing.T, p *Platform, n int)
	// before runs after the run is recorded and before delivery.
	before func(t *testing.T, p *Platform)
	// commits are the pull request's commits, and commitsFail makes their
	// read fail.
	commits     []transport.PRCommit
	commitsFail bool
	// coAuthors are the addresses the commits' Co-authored-by trailers
	// name; coAuthorsFail makes their read fail, and noCoAuthors leaves the
	// API without the read.
	coAuthors     []string
	coAuthorsFail bool
	noCoAuthors   bool
	// stampedBy, when set, is the client the trigger's ingress stamp names
	// in place of the client that raised it; "" is none.
	stampedBy *string
}

// withCommits is a GitHub API whose read of a pull request's commits can
// be made to fail.
type withCommits struct {
	*transport.Fake
	fail bool
}

func (w withCommits) PullRequestCommits(ctx context.Context, owner, repo string, number int) ([]transport.PRCommit, error) {
	if w.fail {
		return nil, errors.New("github: 502 listing commits")
	}
	return w.Fake.PullRequestCommits(ctx, owner, repo, number)
}

// withCoAuthors is a GitHub API that also reads the Co-authored-by
// trailers of a pull request's commits.
type withCoAuthors struct {
	withCommits
	emails []string
	fail   bool
}

func (w withCoAuthors) PullRequestCoAuthors(context.Context, string, string, int) ([]string, error) {
	if w.fail {
		return nil, errors.New("github: 502 reading commits")
	}
	return w.emails, nil
}

var _ ghconnector.CoAuthorsAPI = withCoAuthors{}

func hookedEvent(t *testing.T, c hookedCase) (string, error) {
	t.Helper()
	p, _, _, at := reviewFixture(t)
	if _, err := p.Agents.Create(t.Context(), "t", wire.AgentSpec{Name: "gatekeeper", Provider: "offline", Model: "offline", SystemPrompt: "Review the change", Review: c.review}, "test"); err != nil {
		t.Fatal(err)
	}
	client := clients.Client{Tenant: "t", ID: "gh", Surface: clients.GitHub, Enabled: true, Managed: clients.ManagedStatic, DefaultAgent: "gatekeeper",
		GitHub: &clients.GitHubOptions{Login: "humagen[bot]", People: map[string]string{"ada": "ada@acme.example"}}}
	api := transport.NewFake()
	ref := transport.Ref{Owner: "acme", Repo: "site", Number: 7}
	proof := transport.Context{Files: []transport.ContextFile{{Filename: "a.go", Patch: "@@ -1 +1 @@\n-old\n+new"}}}
	proof.Subject.Head.SHA = approvalSHA
	proof.Subject.State = "open"
	proof.Subject.User.Login, proof.Subject.User.ID = c.author, githubID(c.author)
	if err := api.SeedContext(ref, proof); err != nil {
		t.Fatal(err)
	}
	target := address.MustParse("github://acme/site/pull/7?head_sha=" + approvalSHA)
	var tag event.Envelope
	if c.asker == "" {
		req := clients.AutoReviewRequest{Repository: "acme/site", Number: 7, SHA: approvalSHA, Author: c.author, AuthorID: githubID(c.author)}
		if _, _, _, err := p.planAutoReview(client, req); err != nil {
			t.Fatal(err)
		}
		tag = autoReviewTag(client, "pull_request", req, "")
	} else {
		gref := event.GitHubRef{Owner: "acme", Repo: "site", Number: 7, Pull: true, Sender: c.asker, SenderID: githubID(c.asker), Author: c.author, AuthorID: githubID(c.author), HeadSHA: approvalSHA}
		var body event.Body = event.GitHubIssueCommentCreated{Request: event.Request{Actor: event.Actor{ID: c.asker}, Text: "@humagen review"}, GitHubRef: gref}
		if c.requested {
			body = event.GitHubReviewRequested{Request: event.Request{Actor: event.Actor{ID: c.asker}}, GitHubRef: gref, Reviewer: "gatekeeper"}
		}
		tag = event.Envelope{ID: "asked-" + c.asker, Tenant: "t", Agent: "gatekeeper", Source: target.String(), Channel: target.String(), Time: at, Type: body.Type(), Data: body}
		wire.SetShape(&tag, wire.ShapeReview)
	}
	tag.Time = at
	// The platform's ingress stamps the client that raised the trigger.
	wire.SetMeta(&tag, runs.MetaClient, client.ID)
	if c.stampedBy != nil {
		wire.DeleteMeta(&tag, runs.MetaClient)
		if *c.stampedBy != "" {
			wire.SetMeta(&tag, runs.MetaClient, *c.stampedBy)
		}
	}
	runID := runtime.RunID("t", tag.ID)
	if err := p.Runs.Record(runID, tag, 1); err != nil {
		t.Fatal(err)
	}
	run, err := p.Runs.Get("t", runID)
	if err != nil {
		t.Fatal(err)
	}
	checks := c.checks
	if c.checksFor != nil {
		checks = c.checksFor(runID)
	}
	for _, check := range checks {
		if err := api.SeedCommitCheck("acme", "site", approvalSHA, check); err != nil {
			t.Fatal(err)
		}
	}
	report := presentation.ReviewReport{Format: presentation.ReviewFormatV2, Title: "Review", Verdict: c.verdict, Summary: c.summary, Findings: []presentation.ReviewFinding{}}
	if c.finding != "" {
		report.Findings = append(report.Findings, presentation.ReviewFinding{ID: "f1", Severity: c.finding, Title: "Check input", Suggestion: "Validate input", Prompt: "Fix validation",
			Evidence: []presentation.ReviewEvidence{{Source: "a.go", Observation: "Input is not validated"}}})
	}
	text, _ := json.Marshal(report)
	n := 0
	if err := api.SeedPullRequestCommits(ref, c.commits); err != nil {
		t.Fatal(err)
	}
	var gh transport.API = withCoAuthors{withCommits: withCommits{Fake: api, fail: c.commitsFail}, emails: c.coAuthors, fail: c.coAuthorsFail}
	if c.noCoAuthors {
		gh = withCommits{Fake: api, fail: c.commitsFail}
	}
	peer, err := ghconnector.New(ghconnector.Config{API: gh, Owners: []string{"acme"}, Tenant: "t", Login: "humagen[bot]", Interval: -1, ApprovalClaims: state.NewMemory(),
		AsksReview: func(v transport.View) (bool, error) { return p.asksReview(client, v) },
		ReviewPolicy: func(ctx context.Context, v transport.View) (ghconnector.ReviewGrant, error) {
			n++
			if c.between != nil {
				c.between(t, p, n)
			}
			return p.reviewGrant(ctx, client, v)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	done := reviewAppend(t, p, run, 1, at.Add(30*time.Second), event.Completed{Text: string(text)})
	done.Channel = target.String()
	if c.before != nil {
		c.before(t, p)
	}
	if err := peer.Deliver(t.Context(), target, done); err != nil {
		return "", err
	}
	reviews := api.SubmittedReviews()
	if len(reviews) != 1 {
		t.Fatalf("posted %+v", reviews)
	}
	return reviews[0].Review.Event, nil
}

func fiber(runID, conclusion string) transport.CommitCheck {
	status := "completed"
	if conclusion == "" {
		status = "in_progress"
	}
	return transport.CommitCheck{Kind: transport.CheckKindRun, Name: "fiber/gatekeeper", Status: status, Conclusion: conclusion, App: "humagen", ExternalID: runID}
}

// a repository with no CI. An earlier Humagen run on the same commit
// finished, so its own fiber check passed. That check is the only "CI".
func TestAnEarlierHumagenCheckCountsAsCI(t *testing.T) {
	c := hookedCase{approvalCase: cleanApproval()}
	c.asker = "ada"
	c.checksFor = func(runID string) []transport.CommitCheck {
		return []transport.CommitCheck{fiber("earlier-run", "success"), fiber(runID, "")}
	}
	got, err := hookedEvent(t, c)
	if err != nil {
		t.Fatal(err)
	}
	if got == "APPROVE" {
		t.Fatalf("approved with no CI: the only passing check is Humagen's own check from an earlier run")
	}
}

// every check skipped or neutral.
func TestAllChecksSkipped(t *testing.T) {
	c := hookedCase{approvalCase: cleanApproval()}
	c.checks = []transport.CommitCheck{
		{Kind: transport.CheckKindRun, Name: "test", Status: "completed", Conclusion: "skipped"},
		{Kind: transport.CheckKindRun, Name: "lint", Status: "completed", Conclusion: "neutral"},
	}
	if got, err := hookedEvent(t, c); err != nil || got == "APPROVE" {
		t.Fatalf("every check skipped or neutral: %s %v", got, err)
	}
}

// an unmapped collaborator requests a review of their own pull
// request.
func TestTheAuthorRequestsReviewOfTheirOwnPR(t *testing.T) {
	c := hookedCase{approvalCase: cleanApproval()}
	c.asker, c.requested, c.author = "mallory", true, "mallory"
	got, err := hookedEvent(t, c)
	if err != nil {
		t.Fatal(err)
	}
	if got == "APPROVE" {
		t.Fatalf("the pull request's author, unmapped, got the app's approval by requesting it")
	}
}

// an unmapped person comments "@humagen review" on their own PR.
func TestUnmappedAuthorComments(t *testing.T) {
	c := hookedCase{approvalCase: cleanApproval()}
	c.asker, c.author = "mallory", "mallory"
	got, err := hookedEvent(t, c)
	if err != nil || got == "APPROVE" {
		t.Fatalf("unmapped commenter: %s %v", got, err)
	}
}

// the agent becomes never between the plan and the hook.
func TestPolicyBecomesNeverBeforeTheHook(t *testing.T) {
	c := hookedCase{approvalCase: cleanApproval()}
	c.between = func(t *testing.T, p *Platform, n int) {
		if n != 2 {
			return
		}
		if _, err := p.Agents.Revise(t.Context(), "t", wire.AgentSpec{Name: "gatekeeper", Provider: "offline", Model: "offline", SystemPrompt: "Review the change", Review: wire.Review{Approve: wire.ReviewApproveNever}}, 0, "test"); err != nil {
			t.Fatal(err)
		}
	}
	reached := 0
	inner := c.between
	c.between = func(t *testing.T, p *Platform, n int) { reached = n; inner(t, p, n) }
	got, err := hookedEvent(t, c)
	if err != nil || got != "COMMENT" || reached != 2 {
		t.Fatalf("policy changed to never: %s %v, hook reached %d", got, err, reached)
	}
}

// the repository becomes never between the plan and the hook.
func TestRepositoryBecomesNeverBeforeTheHook(t *testing.T) {
	c := hookedCase{approvalCase: cleanApproval()}
	c.between = func(t *testing.T, p *Platform, n int) {
		if n != 2 {
			return
		}
		if _, err := p.RepoSettings.Put("t", "gh", reposettings.Settings{Repository: "acme/site", Approve: reposettings.ApproveNever}, 0, "ada", time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	reached := 0
	inner := c.between
	c.between = func(t *testing.T, p *Platform, n int) { reached = n; inner(t, p, n) }
	got, err := hookedEvent(t, c)
	if err != nil || got != "COMMENT" || reached != 2 {
		t.Fatalf("repository changed to never: %s %v, hook reached %d", got, err, reached)
	}
}

// the run started under never; the agent became when_clean later.
func TestWhenCleanAddedMidRun(t *testing.T) {
	c := hookedCase{approvalCase: cleanApproval()}
	c.review = wire.Review{Approve: wire.ReviewApproveNever}
	c.before = func(t *testing.T, p *Platform) {
		if _, err := p.Agents.Revise(t.Context(), "t", wire.AgentSpec{Name: "gatekeeper", Provider: "offline", Model: "offline", SystemPrompt: "Review the change", Review: wire.Review{Approve: wire.ReviewApproveWhenClean}}, 0, "test"); err != nil {
			t.Fatal(err)
		}
	}
	got, err := hookedEvent(t, c)
	if err != nil || got != "COMMENT" {
		t.Fatalf("when_clean added mid-run: %s %v", got, err)
	}
}

// an agent with approve: never is deleted while
// its review run works. Before this PR the review posted as a comment.
func TestDeletedNeverAgentStillPostsItsReview(t *testing.T) {
	c := hookedCase{approvalCase: cleanApproval()}
	c.review = wire.Review{}
	c.verdict = "comment"
	c.before = func(t *testing.T, p *Platform) {
		if err := p.Agents.Delete(t.Context(), "t", "gatekeeper", 0); err != nil {
			t.Fatal(err)
		}
	}
	got, err := hookedEvent(t, c)
	if err != nil {
		t.Fatalf("a never agent's review no longer posts once the agent is deleted: %v", err)
	}
	if got != "COMMENT" {
		t.Fatalf("posted %s", got)
	}
}

// a never agent's review whose run record cannot
// be read. Before this PR the review posted; now the grant error blocks it.
func TestNeverAgentWithUnreadableGrantStillPosts(t *testing.T) {
	c := hookedCase{approvalCase: cleanApproval()}
	c.review = wire.Review{}
	c.verdict = "comment"
	c.between = func(t *testing.T, p *Platform, n int) {
		if err := p.Agents.Delete(t.Context(), "t", "gatekeeper", 0); err != nil && n == 1 {
			t.Fatal(err)
		}
	}
	if got, err := hookedEvent(t, c); err != nil || got != "COMMENT" {
		t.Fatalf("an agent deleted before the grant: %s %v", got, err)
	}
}

// An automatic review a person resumed approves only when the client maps
// that person, they are known by GitHub user ID, they are not the author
// under any login, and the host read the commits and they wrote none.
func TestAResumedAutomaticReviewApprovesOnlyForASecondPerson(t *testing.T) {
	clean := []transport.PRCommit{{SHA: approvalSHA, AuthorID: 583231, CommitterID: 583231}}
	for name, c := range map[string]struct {
		by          string
		byID        int64
		author      string
		commits     []transport.PRCommit
		commitsFail bool
		want        string
	}{
		"nobody resumed it":                         {author: "octocat", want: "APPROVE"},
		"a mapped person, commits read":             {by: "ada", byID: 1001, author: "octocat", commits: clean, want: "APPROVE"},
		"a mapped person, commits unreadable":       {by: "ada", byID: 1001, author: "octocat", commitsFail: true, want: "COMMENT"},
		"a mapped person with no user ID":           {by: "ada", author: "octocat", commits: clean, want: "COMMENT"},
		"a mapped person who pushed a commit":       {by: "ada", byID: 1001, author: "octocat", commits: append(clean, transport.PRCommit{SHA: approvalSHA, AuthorID: 1001, CommitterID: 583231}), want: "COMMENT"},
		"a mapped person who committed one":         {by: "ada", byID: 1001, author: "octocat", commits: append(clean, transport.PRCommit{SHA: approvalSHA, AuthorID: 583231, CommitterID: 1001}), want: "COMMENT"},
		"a commit matched to no account":            {by: "ada", byID: 1001, author: "octocat", commits: append(clean, transport.PRCommit{SHA: approvalSHA}), want: "COMMENT"},
		"a stranger":                                {by: "stranger", byID: 4242, author: "octocat", commits: clean, want: "COMMENT"},
		"the author, renamed since they resumed it": {by: "ada", byID: 1001, author: "ada-new", commits: clean, want: "COMMENT"},
	} {
		t.Run(name, func(t *testing.T) {
			hc := hookedCase{approvalCase: cleanApproval(), commits: c.commits, commitsFail: c.commitsFail}
			hc.author = c.author
			hc.before = func(t *testing.T, p *Platform) {
				if c.by == "" {
					return
				}
				if _, err := p.pullReviews.Update("t", pullReviewKey("gh", "acme/site", 7), func(v PullReview, _ bool) (PullReview, error) {
					v.ResumedBy, v.ResumedByID = c.by, c.byID
					return v, nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			if got, err := hookedEvent(t, hc); err != nil || got != c.want {
				t.Fatalf("%s %v, want %s", got, err, c.want)
			}
		})
	}
}

// A person who asks by a comment or a review request approves only when the
// client maps them, they did not write the pull request, and the host read
// its commits and their Co-authored-by trailers, every commit's author and
// committer is linked to a GitHub account, and the asker wrote, committed
// or co-authored none of them.
func TestAnAskerApprovesOnlyWhenTheyWroteNoneOfIt(t *testing.T) {
	theirs := []transport.PRCommit{{SHA: approvalSHA, AuthorID: 583231, CommitterID: 583231}}
	many := make([]transport.PRCommit, transport.MaxPullRequestCommits)
	for i := range many {
		many[i] = theirs[0]
	}
	plus := func(c transport.PRCommit) []transport.PRCommit {
		return append(append([]transport.PRCommit(nil), theirs...), c)
	}
	for name, c := range map[string]struct {
		requested     bool
		commits       []transport.PRCommit
		commitsFail   bool
		coAuthors     []string
		coAuthorsFail bool
		noCoAuthors   bool
		want          string
	}{
		"a comment":                                 {commits: theirs, coAuthors: []string{"someone@acme.example"}, want: "APPROVE"},
		"a review request":                          {requested: true, commits: theirs, want: "APPROVE"},
		"an author of a commit":                     {commits: plus(transport.PRCommit{SHA: approvalSHA, AuthorID: 1001, CommitterID: 583231}), want: "COMMENT"},
		"a committer of a commit":                   {requested: true, commits: plus(transport.PRCommit{SHA: approvalSHA, AuthorID: 583231, CommitterID: 1001}), want: "COMMENT"},
		"a commit whose author has no account":      {commits: plus(transport.PRCommit{SHA: approvalSHA, CommitterID: 583231}), want: "COMMENT"},
		"a commit whose committer has no account":   {commits: plus(transport.PRCommit{SHA: approvalSHA, AuthorID: 583231}), want: "COMMENT"},
		"commits without the reviewed one":          {commits: []transport.PRCommit{{SHA: "cd" + approvalSHA[2:], AuthorID: 583231, CommitterID: 583231}}, want: "COMMENT"},
		"no commits":                                {want: "COMMENT"},
		"commits that cannot be read":               {commitsFail: true, want: "COMMENT"},
		"commits at saige's bound":                  {commits: many, want: "COMMENT"},
		"a review request, commits unread":          {requested: true, commitsFail: true, want: "COMMENT"},
		"a co-author by the mapped address":         {commits: theirs, coAuthors: []string{"Ada@Acme.example"}, want: "COMMENT"},
		"a co-author by the no-reply address":       {commits: theirs, coAuthors: []string{"1001+ada@users.noreply.github.com"}, want: "COMMENT"},
		"a co-author by the older no-reply address": {requested: true, commits: theirs, coAuthors: []string{"ada@users.noreply.github.com"}, want: "COMMENT"},
		"co-authors that cannot be read":            {commits: theirs, coAuthorsFail: true, want: "COMMENT"},
		"an API that does not read co-authors":      {commits: theirs, noCoAuthors: true, want: "COMMENT"},
		"a review request, co-authors not readable": {requested: true, commits: theirs, noCoAuthors: true, want: "COMMENT"},
	} {
		t.Run(name, func(t *testing.T) {
			hc := hookedCase{approvalCase: cleanApproval(), commits: c.commits, commitsFail: c.commitsFail, coAuthors: c.coAuthors, coAuthorsFail: c.coAuthorsFail, noCoAuthors: c.noCoAuthors}
			hc.asker, hc.requested = "ada", c.requested
			if got, err := hookedEvent(t, hc); err != nil || got != c.want {
				t.Fatalf("%s %v, want %s", got, err, c.want)
			}
		})
	}
}

// Only a trigger the platform's ingress stamped with the GitHub client is
// trusted to approve, and only its user IDs are believed: one that names no
// client, or another client, approves nothing, though it says the same.
func TestOnlyTheClientsOwnTriggerApproves(t *testing.T) {
	theirs := []transport.PRCommit{{SHA: approvalSHA, AuthorID: 583231, CommitterID: 583231}}
	for name, c := range map[string]struct {
		asker  string
		client string
		want   string
	}{
		"an automatic review the client raised": {client: "gh", want: "APPROVE"},
		"an automatic review no client stamped": {client: "", want: "COMMENT"},
		"an automatic review of another client": {client: "other", want: "COMMENT"},
		"a comment the client raised":           {asker: "ada", client: "gh", want: "APPROVE"},
		"a comment no client stamped":           {asker: "ada", client: "", want: "COMMENT"},
		"a comment another client stamped":      {asker: "ada", client: "other", want: "COMMENT"},
	} {
		t.Run(name, func(t *testing.T) {
			hc := hookedCase{approvalCase: cleanApproval(), commits: theirs}
			hc.asker = c.asker
			hc.stampedBy = &c.client
			if got, err := hookedEvent(t, hc); err != nil || got != c.want {
				t.Fatalf("%s %v, want %s", got, err, c.want)
			}
		})
	}
}
