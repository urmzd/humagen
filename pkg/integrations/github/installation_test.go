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
	"sync"
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
// counts the reads. With gate set, a read waits for gate to close, and fails
// as GitHub would if its own context ends first.
type lister struct {
	mu    sync.Mutex
	repos []string
	err   error
	gate  chan struct{}
	reads atomic.Int32
}

func (l *lister) set(repos []string, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.repos, l.err = repos, err
}

func (l *lister) InstallationRepositories(ctx context.Context, _ int) ([]string, error) {
	l.reads.Add(1)
	l.mu.Lock()
	gate := l.gate
	l.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err != nil {
		return nil, l.err
	}
	return append([]string(nil), l.repos...), nil
}

// clock is a time a test moves on, safe to read from the list's own read.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func newClock() *clock { return &clock{now: time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)} }

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func TestInstalledReposKeepsTheListUntilItIsDue(t *testing.T) {
	now := newClock()
	l := &lister{repos: []string{"acme/web", "acme/api", "ACME/api"}}
	c := github.NewInstalledRepos(l, now.Now, nil)
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
	l.set([]string{"acme/api", "acme/new"}, nil)
	now.Add(github.InstalledReposTTL)
	if !c.Covers(ctx, "acme", "new") || l.reads.Load() != 2 {
		t.Fatalf("a due list was not read again (%d reads)", l.reads.Load())
	}
}

func TestInstalledReposDropsARepositoryRemovedFromTheInstallation(t *testing.T) {
	now := newClock()
	l := &lister{repos: []string{"acme/api", "acme/old"}}
	c := github.NewInstalledRepos(l, now.Now, nil)
	ctx := context.Background()
	if !c.Covers(ctx, "acme", "old") {
		t.Fatal("acme/old was not covered while installed")
	}
	l.set([]string{"acme/api"}, nil)
	now.Add(github.InstalledReposTTL - time.Second)
	if !c.Covers(ctx, "acme", "old") {
		t.Fatal("the list was read again before it was due")
	}
	now.Add(time.Second)
	if c.Covers(ctx, "acme", "old") || !c.Covers(ctx, "acme", "api") {
		t.Fatal("a repository removed from the installation is still covered after the TTL")
	}
}

func TestInstalledReposKeepsTheLastListWhenAReadFails(t *testing.T) {
	now := newClock()
	var mu sync.Mutex
	var heard []error
	l := &lister{err: errors.New("github down")}
	c := github.NewInstalledRepos(l, now.Now, func(err error) { mu.Lock(); heard = append(heard, err); mu.Unlock() })
	ctx := context.Background()
	if repos, ok := c.Repos(ctx); ok || len(repos) != 0 {
		t.Fatalf("a failed first read gave %v, %v", repos, ok)
	}
	// The failure is not asked again until the retry is due.
	c.Repos(ctx)
	mu.Lock()
	n := len(heard)
	mu.Unlock()
	if l.reads.Load() != 1 || n != 1 {
		t.Fatalf("%d reads, %d errors heard, before the retry", l.reads.Load(), n)
	}
	l.set([]string{"acme/api"}, nil)
	now.Add(github.InstalledReposRetry)
	if !c.Covers(ctx, "acme", "api") {
		t.Fatal("the retry did not read the list")
	}
	l.set(nil, errors.New("github down again"))
	now.Add(github.InstalledReposTTL)
	if repos, ok := c.Repos(ctx); !ok || len(repos) != 1 {
		t.Fatalf("a failed refresh dropped the last list: %v, %v", repos, ok)
	}
}

// A first caller that gives up does not fail the read for everyone else: the
// read goes on without its context, and the next caller finds its list.
func TestInstalledReposReadOutlivesACancelledCaller(t *testing.T) {
	gate := make(chan struct{})
	l := &lister{repos: []string{"acme/api"}, gate: gate}
	c := github.NewInstalledRepos(l, newClock().Now, nil)
	ctx, cancel := context.WithCancel(context.Background())
	answered := make(chan bool)
	go func() { answered <- c.Covers(ctx, "acme", "api") }()
	for l.reads.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	cancel()
	if <-answered {
		t.Fatal("a cancelled caller was answered with a list nobody had read")
	}
	close(gate)
	if !c.Covers(context.Background(), "acme", "api") {
		t.Fatal("the read failed with the first caller's context")
	}
	if l.reads.Load() != 1 {
		t.Fatalf("%d reads, want the one the cancelled caller started", l.reads.Load())
	}
	if !c.Covers(context.Background(), "acme", "api") {
		t.Fatal("the list was not kept")
	}
}

// Callers that find the list due share one read, outside the lock, and each
// waits for it only until its own deadline, then has the last list read.
func TestInstalledReposCallersShareOneReadAndKeepTheirDeadlines(t *testing.T) {
	now := newClock()
	l := &lister{repos: []string{"acme/api"}}
	c := github.NewInstalledRepos(l, now.Now, nil)
	if !c.Covers(context.Background(), "acme", "api") {
		t.Fatal("first read")
	}
	gate := make(chan struct{})
	l.mu.Lock()
	l.gate, l.repos = gate, []string{"acme/api", "acme/web"}
	l.mu.Unlock()
	now.Add(github.InstalledReposTTL)

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			start := time.Now()
			repos, ok := c.Repos(ctx)
			if !ok || strings.Join(repos, ",") != "acme/api" {
				t.Errorf("a caller past its deadline had %v, %v, want the last list", repos, ok)
			}
			if waited := time.Since(start); waited > 2*time.Second {
				t.Errorf("a caller waited %s past its deadline", waited)
			}
		}()
	}
	wg.Wait()
	if n := l.reads.Load(); n != 2 {
		t.Fatalf("%d reads, want one shared by every caller after the first", n)
	}
	close(gate)
	if !c.Covers(context.Background(), "acme", "web") {
		t.Fatal("the shared read's list was not kept")
	}
}

func TestInstalledReposOfNoInstallationCoversNothing(t *testing.T) {
	var c *github.InstalledRepos
	if repos, ok := c.Repos(context.Background()); ok || repos != nil || c.Covers(context.Background(), "acme", "api") {
		t.Fatal("a nil list covered something")
	}
}
