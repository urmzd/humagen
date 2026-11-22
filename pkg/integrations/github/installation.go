package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/urmzd/saige/flow/address"
)

// The bounds of an installation's repository list: how many repositories are
// kept, how many one page asks for, and how large one page's answer may be.
const (
	MaxInstallationRepos = 1000
	installationPage     = 100
	maxInstallationBody  = 8 << 20
)

// The refresh of an installation's repository list: a good list is kept for
// InstalledReposTTL, a failed read is tried again after InstalledReposRetry,
// and one read of every page gives up after InstalledReposTimeout.
const (
	InstalledReposTTL     = 5 * time.Minute
	InstalledReposRetry   = time.Minute
	InstalledReposTimeout = 10 * time.Second
)

// InstallationLister lists the "owner/name" repositories a GitHub App
// installation covers, at most limit of them.
type InstallationLister interface {
	InstallationRepositories(ctx context.Context, limit int) ([]string, error)
}

// InstallationRepositories reads GET /installation/repositories page by page
// with the installation token the client already uses. It stops at limit
// (MaxInstallationRepos when limit is out of range) and at the last page.
func (a *HTTPAPI) InstallationRepositories(ctx context.Context, limit int) ([]string, error) {
	if limit <= 0 || limit > MaxInstallationRepos {
		limit = MaxInstallationRepos
	}
	out := []string{}
	for page := 1; len(out) < limit && (page-1)*installationPage < MaxInstallationRepos; page++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/installation/repositories?per_page=%d&page=%d", a.base, installationPage, page), nil)
		if err != nil {
			return nil, err
		}
		if a.token != "" {
			req.Header.Set("Authorization", "Bearer "+a.token)
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		resp, err := a.http.Do(req)
		if err != nil {
			return nil, err
		}
		var body struct {
			TotalCount   int `json:"total_count"`
			Repositories []struct {
				FullName string `json:"full_name"`
			} `json:"repositories"`
		}
		status := resp.StatusCode
		if status == http.StatusOK {
			err = json.NewDecoder(io.LimitReader(resp.Body, maxInstallationBody)).Decode(&body)
		}
		resp.Body.Close()
		if status != http.StatusOK {
			return nil, fmt.Errorf("github installation repositories: status %d", status)
		}
		if err != nil {
			return nil, fmt.Errorf("github installation repositories: %w", err)
		}
		for _, r := range body.Repositories {
			if len(out) >= limit {
				break
			}
			if name, ok := repoName(r.FullName); ok {
				out = append(out, name)
			}
		}
		if len(body.Repositories) < installationPage || page*installationPage >= body.TotalCount {
			break
		}
	}
	return out, nil
}

// repoName is s when it is a well-formed "owner/name".
func repoName(s string) (string, bool) {
	owner, name, ok := strings.Cut(strings.TrimSpace(s), "/")
	if !ok || address.ValidWorkspace(owner) != nil || name == "" || len(name) > 100 || strings.ContainsAny(name, "/ \t\r\n") || name == "." || name == ".." {
		return "", false
	}
	return owner + "/" + name, true
}

// InstalledRepos keeps the list an InstallationLister gives, read when it is
// first asked for and again once it is older than InstalledReposTTL. A read
// that fails keeps the list it had (or none) and is tried again after
// InstalledReposRetry, so a GitHub outage is not asked again on every call.
// One read runs at a time; callers wait for it, at most InstalledReposTimeout.
type InstalledRepos struct {
	list    InstallationLister
	clock   func() time.Time
	onError func(error)

	mu    sync.Mutex
	repos []string
	index map[string]bool
	have  bool
	next  time.Time
}

// NewInstalledRepos keeps list's repositories. clock is time.Now when nil;
// onError, when set, hears each failed read.
func NewInstalledRepos(list InstallationLister, clock func() time.Time, onError func(error)) *InstalledRepos {
	if clock == nil {
		clock = time.Now
	}
	return &InstalledRepos{list: list, clock: clock, onError: onError}
}

// Repos is the installation's repositories, sorted, and whether any list was
// ever read. It reads the list again when it is due.
func (c *InstalledRepos) Repos(ctx context.Context) ([]string, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.refresh(ctx)
	return slices.Clone(c.repos), c.have
}

// Covers says whether the installation covers "owner/name", in any case.
func (c *InstalledRepos) Covers(ctx context.Context, owner, repo string) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.refresh(ctx)
	return c.index[strings.ToLower(owner+"/"+repo)]
}

// refresh reads the list when it is due. c.mu is held.
func (c *InstalledRepos) refresh(ctx context.Context) {
	now := c.clock()
	if now.Before(c.next) {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, InstalledReposTimeout)
	defer cancel()
	got, err := c.list.InstallationRepositories(ctx, MaxInstallationRepos)
	if err != nil {
		c.next = now.Add(InstalledReposRetry)
		if c.onError != nil {
			c.onError(err)
		}
		return
	}
	index := make(map[string]bool, len(got))
	repos := make([]string, 0, min(len(got), MaxInstallationRepos))
	for _, r := range got[:min(len(got), MaxInstallationRepos)] {
		key := strings.ToLower(r)
		if !index[key] {
			index[key] = true
			repos = append(repos, r)
		}
	}
	slices.SortFunc(repos, func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })
	c.repos, c.index, c.have, c.next = repos, index, true, now.Add(InstalledReposTTL)
}
