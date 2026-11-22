package github_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/integrations/github"
)

// installationAPI answers GET /installation/repositories with total
// repositories named acme/r0000 and up, per_page at a time, and counts the
// pages it was asked for.
func installationAPI(t *testing.T, total int, pages *atomic.Int32) *github.HTTPAPI {
	t.Helper()
	return github.NewHTTPAPI("installation-token", &http.Client{Transport: labelTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/installation/repositories" || r.Header.Get("Authorization") != "Bearer installation-token" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		pages.Add(1)
		per, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if per != 100 || page < 1 {
			t.Errorf("unbounded page: %s", r.URL.RawQuery)
		}
		type repo struct {
			FullName string `json:"full_name"`
		}
		body := struct {
			TotalCount   int    `json:"total_count"`
			Repositories []repo `json:"repositories"`
		}{TotalCount: total, Repositories: []repo{}}
		for i := (page - 1) * per; i < min(total, page*per); i++ {
			body.Repositories = append(body.Repositories, repo{FullName: fmt.Sprintf("acme/r%04d", i)})
		}
		raw, _ := json.Marshal(body)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(raw))), Header: http.Header{}}, nil
	})})
}

func TestInstallationRepositoriesReadsEveryPage(t *testing.T) {
	var pages atomic.Int32
	repos, err := installationAPI(t, 250, &pages).InstallationRepositories(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 250 || repos[0] != "acme/r0000" || repos[249] != "acme/r0249" || pages.Load() != 3 {
		t.Fatalf("got %d repos over %d pages, first %v", len(repos), pages.Load(), repos[:1])
	}
}

func TestInstallationRepositoriesStopsAtTheCap(t *testing.T) {
	var pages atomic.Int32
	repos, err := installationAPI(t, 5000, &pages).InstallationRepositories(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != github.MaxInstallationRepos || pages.Load() != github.MaxInstallationRepos/100 {
		t.Fatalf("got %d repos over %d pages, want %d over %d", len(repos), pages.Load(), github.MaxInstallationRepos, github.MaxInstallationRepos/100)
	}
	pages.Store(0)
	repos, err = installationAPI(t, 5000, &pages).InstallationRepositories(context.Background(), 150)
	if err != nil || len(repos) != 150 || pages.Load() != 2 {
		t.Fatalf("limit 150: %d repos over %d pages, %v", len(repos), pages.Load(), err)
	}
}

func TestInstallationRepositoriesReportsARefusal(t *testing.T) {
	api := github.NewHTTPAPI("t", &http.Client{Transport: labelTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader(`{"message":"Resource not accessible by personal access token"}`)), Header: http.Header{}}, nil
	})})
	if _, err := api.InstallationRepositories(context.Background(), 0); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("err = %v, want the status", err)
	}
}

func TestInstallationRepositoriesSkipsMalformedNames(t *testing.T) {
	api := github.NewHTTPAPI("t", &http.Client{Transport: labelTransport(func(*http.Request) (*http.Response, error) {
		body := `{"total_count":4,"repositories":[{"full_name":"acme/api"},{"full_name":"no-slash"},{"full_name":"acme/a/b"},{"full_name":"acme/.."}]}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})})
	repos, err := api.InstallationRepositories(context.Background(), 0)
	if err != nil || len(repos) != 1 || repos[0] != "acme/api" {
		t.Fatalf("repos = %v, %v", repos, err)
	}
}

// lister is an InstallationLister that answers what it holds, or err, and
// counts the reads.
type lister struct {
	repos []string
	err   error
	reads atomic.Int32
}

func (l *lister) InstallationRepositories(context.Context, int) ([]string, error) {
	l.reads.Add(1)
	if l.err != nil {
		return nil, l.err
	}
	return append([]string(nil), l.repos...), nil
}

func TestInstalledReposKeepsTheListUntilItIsDue(t *testing.T) {
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	l := &lister{repos: []string{"acme/web", "acme/api", "ACME/api"}}
	c := github.NewInstalledRepos(l, func() time.Time { return now }, nil)
	ctx := context.Background()
	repos, ok := c.Repos(ctx)
	if !ok || strings.Join(repos, ",") != "acme/api,acme/web" {
		t.Fatalf("repos = %v, %v: want sorted and deduplicated in any case", repos, ok)
	}
	if !c.Covers(ctx, "Acme", "API") || c.Covers(ctx, "acme", "secret") {
		t.Fatal("covers answered wrongly")
	}
	if l.reads.Load() != 1 {
		t.Fatalf("read %d times before the list was due", l.reads.Load())
	}
	l.repos = []string{"acme/api", "acme/new"}
	now = now.Add(github.InstalledReposTTL)
	if !c.Covers(ctx, "acme", "new") || l.reads.Load() != 2 {
		t.Fatalf("a due list was not read again (%d reads)", l.reads.Load())
	}
}

func TestInstalledReposKeepsTheLastListWhenAReadFails(t *testing.T) {
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	var heard []error
	l := &lister{err: errors.New("github down")}
	c := github.NewInstalledRepos(l, func() time.Time { return now }, func(err error) { heard = append(heard, err) })
	ctx := context.Background()
	if repos, ok := c.Repos(ctx); ok || len(repos) != 0 {
		t.Fatalf("a failed first read gave %v, %v", repos, ok)
	}
	// The failure is not asked again until the retry is due.
	c.Repos(ctx)
	if l.reads.Load() != 1 || len(heard) != 1 {
		t.Fatalf("%d reads, %d errors heard, before the retry", l.reads.Load(), len(heard))
	}
	l.err, l.repos = nil, []string{"acme/api"}
	now = now.Add(github.InstalledReposRetry)
	if !c.Covers(ctx, "acme", "api") {
		t.Fatal("the retry did not read the list")
	}
	l.err = errors.New("github down again")
	now = now.Add(github.InstalledReposTTL)
	if repos, ok := c.Repos(ctx); !ok || len(repos) != 1 {
		t.Fatalf("a failed refresh dropped the last list: %v, %v", repos, ok)
	}
}

func TestInstalledReposOfNoInstallationCoversNothing(t *testing.T) {
	var c *github.InstalledRepos
	if repos, ok := c.Repos(context.Background()); ok || repos != nil || c.Covers(context.Background(), "acme", "api") {
		t.Fatal("a nil list covered something")
	}
}
