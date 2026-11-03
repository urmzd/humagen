package platform_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/integrations/github"
)

const sourceSHA = "316c56f81540c3eaca02184173df322a48aebdd9"

// sourceStage is a tenant with one GitHub client serving acme and granted
// reads of other/docs, whose fake holds files at sourceSHA in both.
func sourceStage(t *testing.T) (*platform.Platform, *github.Fake) {
	t.Helper()
	f := &fakes{}
	p := stage(t, &cast{}, f.config())
	create(t, p, tenant, platform.ClientInput{
		Client: clients.Client{
			ID: "gh", Name: "gh", Surface: clients.GitHub, Enabled: true,
			Identity: clients.Identity{Owners: []string{"acme"}}, DefaultAgent: "docs",
			GitHub: &clients.GitHubOptions{ReadRepositories: []string{"other/docs"}},
		},
		Token: clients.SecretInput{Value: "token-gh"}, SigningSecret: clients.SecretInput{Value: "secret-gh"},
	})
	fake := f.githubFor("token-gh")
	var lines []string
	for i := 1; i <= 50; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	for _, repo := range []string{"acme/api", "other/docs", "stranger/secret"} {
		owner, name, _ := strings.Cut(repo, "/")
		ref := github.Ref{Owner: owner, Repo: name, Number: 1}
		var c github.Context
		c.Subject.Head.SHA = sourceSHA
		if err := fake.SeedContext(ref, c); err != nil {
			t.Fatal(err)
		}
		if err := fake.SeedFiles(ref, map[string]string{"pkg/a.go": strings.Join(lines, "\n") + "\n"}); err != nil {
			t.Fatal(err)
		}
	}
	return p, fake
}

func TestGitHubSourceReadsTheLinesWithContextThroughAGrantedClient(t *testing.T) {
	p, _ := sourceStage(t)
	for _, repo := range []string{"acme/api", "ACME/api", "other/docs"} {
		ex, err := p.GitHubSource(t.Context(), tenant, platform.SourceRequest{Repo: repo, SHA: sourceSHA, Path: "pkg/a.go", Start: 10, End: 12, Context: 2})
		if err != nil {
			t.Fatalf("%s: %v", repo, err)
		}
		if ex.First != 8 || len(ex.Lines) != 7 || ex.Lines[0] != "line 8" || ex.Lines[6] != "line 14" || ex.Total != 50 {
			t.Fatalf("%s: excerpt = first %d, %d lines %q, total %d", repo, ex.First, len(ex.Lines), ex.Lines, ex.Total)
		}
		if !strings.HasSuffix(ex.URL, "/blob/"+sourceSHA+"/pkg/a.go#L10-L12") {
			t.Fatalf("%s: url = %s", repo, ex.URL)
		}
	}
	// Context stops at the file's edges, and an end past the last line is cut to it.
	ex, err := p.GitHubSource(t.Context(), tenant, platform.SourceRequest{Repo: "acme/api", SHA: sourceSHA, Path: "pkg/a.go", Start: 49, End: 60, Context: 5})
	if err != nil || ex.First != 44 || ex.End != 50 || len(ex.Lines) != 7 {
		t.Fatalf("at the end: %+v, %v", ex, err)
	}
}

func TestGitHubSourceIsNotFoundWithoutAGrant(t *testing.T) {
	p, _ := sourceStage(t)
	cases := []struct{ name, tenant, repo, path string }{
		{"a repository no client serves or is granted", tenant, "stranger/secret", "pkg/a.go"},
		{"another workspace", "globex", "acme/api", "pkg/a.go"},
		{"a file the commit lacks", tenant, "acme/api", "pkg/missing.go"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := p.GitHubSource(t.Context(), tc.tenant, platform.SourceRequest{Repo: tc.repo, SHA: sourceSHA, Path: tc.path, Start: 1, End: 1})
			if !errors.Is(err, platform.ErrSourceNotFound) {
				t.Fatalf("err = %v, want not found", err)
			}
		})
	}
}

func TestGitHubSourceRefusesUnboundedOrMalformedRequests(t *testing.T) {
	p, _ := sourceStage(t)
	ok := platform.SourceRequest{Repo: "acme/api", SHA: sourceSHA, Path: "pkg/a.go", Start: 1, End: 1}
	cases := map[string]func(r *platform.SourceRequest){
		"a short sha":         func(r *platform.SourceRequest) { r.SHA = sourceSHA[:7] },
		"a branch":            func(r *platform.SourceRequest) { r.SHA = "main" },
		"no repo owner":       func(r *platform.SourceRequest) { r.Repo = "api" },
		"an absolute path":    func(r *platform.SourceRequest) { r.Path = "/etc/passwd" },
		"line zero":           func(r *platform.SourceRequest) { r.Start, r.End = 0, 1 },
		"end before start":    func(r *platform.SourceRequest) { r.Start, r.End = 5, 4 },
		"more than 200 lines": func(r *platform.SourceRequest) { r.Start, r.End = 1, 201 },
		"too much context":    func(r *platform.SourceRequest) { r.Context = 21 },
		"past the file's end": func(r *platform.SourceRequest) { r.Start, r.End = 51, 51 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			r := ok
			mutate(&r)
			if _, err := p.GitHubSource(t.Context(), tenant, r); !errors.Is(err, platform.ErrSourceInvalid) {
				t.Fatalf("err = %v, want invalid", err)
			}
		})
	}
}

func TestGitHubSourceKeepsAFileReadAtACommit(t *testing.T) {
	p, fake := sourceStage(t)
	r := platform.SourceRequest{Repo: "acme/api", SHA: sourceSHA, Path: "pkg/a.go", Start: 1, End: 1}
	if _, err := p.GitHubSource(t.Context(), tenant, r); err != nil {
		t.Fatal(err)
	}
	// GitHub failing now does not matter: the file at that commit is kept.
	fake.Err = errors.New("github down")
	if _, err := p.GitHubSource(t.Context(), tenant, r); err != nil {
		t.Fatalf("a kept file was read again: %v", err)
	}
	r.Path = "pkg/b.go"
	if _, err := p.GitHubSource(t.Context(), tenant, r); !errors.Is(err, platform.ErrSourceUnavailable) {
		t.Fatalf("err = %v, want unavailable", err)
	}
}
