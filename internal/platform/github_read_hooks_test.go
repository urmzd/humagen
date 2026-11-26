package platform_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/integrations/github"
	"github.com/urmzd/humagen/pkg/reposettings"
	"github.com/urmzd/saige/flow/address"
	ghtransport "github.com/urmzd/saige/flow/connectors/github"
)

// A GitHub client's read tools are granted by the platform's own grants: a
// repository its installation covers (listed here as "Acme/API") or its
// read_repositories name is read from another surface, in any case the
// reference spells it, and one the workspace keeps to its own runs
// is read by no run elsewhere, even when the client names it in
// read_repositories, while a run on it still reads it.
func TestAClientsReadToolsFollowTheWorkspacesGrantsInAnyCase(t *testing.T) {
	f := &fakes{}
	inst := &installation{Fake: f.githubFor("token-gh"), repos: []string{"Acme/API"}}
	cfg := f.config()
	cfg.GitHubAPI = func(string) github.API { return inst }
	p := stage(t, &cast{}, cfg)
	create(t, p, tenant, platform.ClientInput{
		Client: clients.Client{
			ID: "gh", Name: "gh", Surface: clients.GitHub, Enabled: true,
			Identity: clients.Identity{Owners: []string{"acme"}}, DefaultAgent: "docs",
			GitHub: &clients.GitHubOptions{ReadRepositories: []string{"acme/docs"}},
		},
		Token: clients.SecretInput{Value: "token-gh"}, SigningSecret: clients.SecretInput{Value: "secret-gh"},
	})
	for _, repo := range []string{"api", "docs", "secret"} {
		var c github.Context
		c.Subject.Title = repo + " issue"
		if err := inst.SeedContext(github.Ref{Owner: "acme", Repo: repo, Number: 1}, c); err != nil {
			t.Fatal(err)
		}
	}
	slack := address.MustParse("slack://T01/C01")
	read := func(source address.Address, reference string) (string, error) {
		return p.ClientAction(t.Context(), tenant, "gh", "github_read_context", source, `{"address":"`+reference+`"}`)
	}
	refused := func(err error) bool {
		var re *github.ReadError
		return errors.As(err, &re)
	}
	for _, ref := range []string{"github://acme/api/issues/1", "github://acme/docs/issues/1"} {
		if _, err := read(slack, ref); err != nil {
			t.Fatalf("%s before any setting: %v", ref, err)
		}
	}
	for _, ref := range []string{"github://ACME/Api/issues/1", "github://Acme/DOCS/issues/1"} {
		if _, err := read(slack, ref); refused(err) {
			t.Fatalf("%s in another case was refused: %v", ref, err)
		}
	}
	off := false
	for _, repo := range []string{"Acme/API", "acme/docs"} {
		if _, err := p.SetGitHubRepository(t.Context(), admin, "gh", repo, reposettings.Settings{CrossSurfaceReads: &off}, 0); err != nil {
			t.Fatal(err)
		}
	}
	for _, ref := range []string{"github://acme/api/issues/1", "github://ACME/Api/issues/1", "github://acme/docs/issues/1", "github://ACME/docs/issues/1"} {
		for _, source := range []address.Address{slack, address.MustParse("github://acme/web/pull/3")} {
			_, err := read(source, ref)
			if !refused(err) || !strings.Contains(err.Error(), "not readable from other surfaces") {
				t.Fatalf("%s read %s, kept to its own runs: %v", source, ref, err)
			}
		}
	}
	if summary, err := read(address.MustParse("github://acme/api/pull/9"), "github://acme/api/issues/1"); err != nil {
		t.Fatalf("a run on the kept repository could not read it: %q, %v", summary, err)
	}
	if _, err := read(slack, "github://acme/secret/issues/1"); !refused(err) {
		t.Fatalf("read a repository nothing grants: %v", err)
	}
}

// whereNamed records whether each console read was marked to follow no
// redirect.
type whereNamed struct {
	*installation
	mu     sync.Mutex
	marked map[string]bool
}

func (w *whereNamed) note(read string, ctx context.Context) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.marked[read] = ghtransport.IsReadWhereNamed(ctx)
}

func (w *whereNamed) ReadContext(ctx context.Context, ref github.Ref, page int) (github.Context, error) {
	w.note("context", ctx)
	return w.installation.ReadContext(ctx, ref, page)
}

func (w *whereNamed) ReadFile(ctx context.Context, ref github.Ref, sha, name string) (github.FileContent, error) {
	w.note("file", ctx)
	return w.installation.ReadFile(ctx, ref, sha, name)
}

// The console reads a repository the caller names, so its reads follow no
// redirect: a renamed or transferred repository is not read in its place.
func TestConsoleReadsOfARepositoryFollowNoRedirect(t *testing.T) {
	f := &fakes{}
	w := &whereNamed{installation: &installation{Fake: f.githubFor("token-gh"), repos: []string{"acme/api"}}, marked: map[string]bool{}}
	cfg := f.config()
	cfg.GitHubAPI = func(string) github.API { return w }
	p := stage(t, &cast{}, cfg)
	create(t, p, tenant, platform.ClientInput{
		Client: clients.Client{
			ID: "gh", Name: "gh", Surface: clients.GitHub, Enabled: true,
			Identity: clients.Identity{Owners: []string{"acme"}}, DefaultAgent: "docs",
		},
		Token: clients.SecretInput{Value: "token-gh"}, SigningSecret: clients.SecretInput{Value: "secret-gh"},
	})
	ref := github.Ref{Owner: "acme", Repo: "api", Number: 1}
	var issue github.Context
	issue.Subject.Number, issue.Subject.Title, issue.Subject.Head.SHA = 1, "api issue", sourceSHA
	if err := w.SeedContext(ref, issue); err != nil {
		t.Fatal(err)
	}
	if err := w.SeedFiles(ref, map[string]string{"a.go": "one\n"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.GitHubIssue(t.Context(), tenant, "acme/api", 1, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := p.GitHubSource(t.Context(), tenant, platform.SourceRequest{Repo: "acme/api", SHA: sourceSHA, Path: "a.go", Start: 1, End: 1}); err != nil {
		t.Fatal(err)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.marked["context"] || !w.marked["file"] {
		t.Fatalf("console reads marked to follow no redirect: %v", w.marked)
	}
}
