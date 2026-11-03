package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/urmzd/humagen/pkg/integrations/github"
)

// GitHubPull is a bounded page of evidence at the pull request's observed head.
type GitHubPull struct {
	Subject   github.ContextSubject   `json:"subject"`
	Files     []github.ContextFile    `json:"files"`
	Comments  []github.ContextComment `json:"comments"`
	Workflows []github.WorkflowRun    `json:"workflows"`
	NextPage  int                     `json:"next_page,omitempty"`
}

var githubRepoName = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

type GitHubIssue struct {
	github.Context
	Labels []string `json:"labels"`
}

// GitHubIssue reads through an enabled tenant client after checking repository access.
func (p *Platform) GitHubIssue(ctx context.Context, tenant, repo string, number, page int) (GitHubIssue, error) {
	owner, name, valid := strings.Cut(repo, "/")
	if !githubRepoName.MatchString(repo) || !valid || owner == ".." || name == ".." || owner == "." || name == "." || owner == "" || name == "" || strings.ContainsAny(owner+name, "?#%\\") || strings.Contains(name, "/") || number < 1 || number > 2147483647 || page < 1 || page > 10 {
		return GitHubIssue{}, ErrSourceInvalid
	}
	reader := p.sourceReader(tenant, owner, name)
	api, ok := reader.(github.ContextAPI)
	if !ok {
		return GitHubIssue{}, ErrSourceNotFound
	}
	out, err := api.ReadContext(ctx, github.Ref{Owner: owner, Repo: name, Number: number}, page)
	if err != nil {
		var missing *github.NotFoundError
		if errors.As(err, &missing) || strings.Contains(strings.ToLower(err.Error()), "not found") || strings.Contains(strings.ToLower(err.Error()), "forbidden") {
			return GitHubIssue{}, ErrSourceNotFound
		}
		return GitHubIssue{}, fmt.Errorf("%w: %v", ErrSourceUnavailable, err)
	}
	if len(out.Subject.Body) > 65536 {
		out.Subject.Body = out.Subject.Body[:65536]
	}
	// Independent caps also cover custom client implementations.
	out.Comments = out.Comments[:min(len(out.Comments), 30)]
	out.Files = out.Files[:min(len(out.Files), 30)]
	out.Reviews = out.Reviews[:min(len(out.Reviews), 30)]
	out.ReviewComments = out.ReviewComments[:min(len(out.ReviewComments), 30)]
	for i := range out.Files {
		if len(out.Files[i].Patch) > 65536 {
			out.Files[i].Patch = ""
		}
	}
	for i := range out.Comments {
		if len(out.Comments[i].Body) > 65536 {
			out.Comments[i].Body = out.Comments[i].Body[:65536]
		}
	}
	if page == 10 || out.NextPage > 10 || out.NextPage <= page {
		out.NextPage = 0
	}
	labels := []string{}
	if labelsAPI, ok := reader.(github.LabelAPI); ok {
		var e error
		labels, e = labelsAPI.IssueLabels(ctx, github.Ref{Owner: owner, Repo: name, Number: number})
		if e != nil {
			return GitHubIssue{}, fmt.Errorf("%w: labels: %v", ErrSourceUnavailable, e)
		}
	}
	return GitHubIssue{Context: out, Labels: labels}, nil
}

// GitHubPull reads CI through the same workflow status path the agent uses.
func (p *Platform) GitHubPull(ctx context.Context, tenant, repo string, number int) (GitHubPull, error) {
	return p.GitHubPullPage(ctx, tenant, repo, number, 1)
}
func (p *Platform) GitHubPullPage(ctx context.Context, tenant, repo string, number, page int) (GitHubPull, error) {
	c, err := p.GitHubIssue(ctx, tenant, repo, number, page)
	if err != nil {
		return GitHubPull{}, err
	}
	if c.Subject.PullRequest == nil || !fullSHA(c.Subject.Head.SHA) {
		return GitHubPull{}, ErrSourceNotFound
	}
	cacheKey := fmt.Sprintf("pull:%s@%s:%d", strings.ToLower(repo), c.Subject.Head.SHA, page)
	cache := &p.surfaces["github"].sources
	if cached, ok := cache.get(cacheKey); ok {
		_ = json.Unmarshal([]byte(cached), &c.Files)
	} else if encoded, e := json.Marshal(c.Files); e == nil {
		cache.put(cacheKey, string(encoded))
	}
	out := GitHubPull{Subject: c.Subject, Files: c.Files, Comments: c.Comments, Workflows: []github.WorkflowRun{}, NextPage: c.NextPage}
	owner, name, _ := strings.Cut(repo, "/")
	if api, ok := p.sourceReader(tenant, owner, name).(github.WorkflowAPI); ok {
		ci, e := api.WorkflowRuns(ctx, github.Ref{Owner: owner, Repo: name, Number: number}, c.Subject.Head.SHA, 1)
		if e != nil {
			return GitHubPull{}, fmt.Errorf("%w: CI: %v", ErrSourceUnavailable, e)
		}
		out.Workflows = ci.Runs[:min(30, len(ci.Runs))]
	}
	return out, nil
}
