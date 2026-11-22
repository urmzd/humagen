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
	"github.com/urmzd/humagen/pkg/threads"
)

// installation is a GitHub fake that also lists the repositories an App
// installation covers, as the real API does for an installation token.
type installation struct {
	*github.Fake
	mu    sync.Mutex
	repos []string
	err   error
}

func (i *installation) InstallationRepositories(context.Context, int) ([]string, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.err != nil {
		return nil, i.err
	}
	return append([]string(nil), i.repos...), nil
}

// installedStage is a tenant with one GitHub client serving acme, granted
// reads of other/docs by name, whose installation covers acme/api, acme/web
// and partner/tools. Its fake holds a file at sourceSHA in each of those
// and in stranger/secret, which nothing covers.
func installedStage(t *testing.T, listErr error) (*platform.Platform, *installation) {
	t.Helper()
	f := &fakes{}
	fake := f.githubFor("token-gh")
	inst := &installation{Fake: fake, repos: []string{"acme/web", "acme/api", "partner/tools"}, err: listErr}
	cfg := f.config()
	cfg.GitHubAPI = func(string) github.API { return inst }
	p := stage(t, &cast{}, cfg)
	create(t, p, tenant, platform.ClientInput{
		Client: clients.Client{
			ID: "gh", Name: "gh", Surface: clients.GitHub, Enabled: true,
			Identity: clients.Identity{Owners: []string{"acme"}}, DefaultAgent: "docs",
			GitHub: &clients.GitHubOptions{ReadRepositories: []string{"other/docs"}},
		},
		Token: clients.SecretInput{Value: "token-gh"}, SigningSecret: clients.SecretInput{Value: "secret-gh"},
	})
	for _, repo := range []string{"acme/api", "partner/tools", "stranger/secret"} {
		owner, name, _ := strings.Cut(repo, "/")
		ref := github.Ref{Owner: owner, Repo: name, Number: 1}
		var c github.Context
		c.Subject.Head.SHA = sourceSHA
		if err := fake.SeedContext(ref, c); err != nil {
			t.Fatal(err)
		}
		if err := fake.SeedFiles(ref, map[string]string{"a.go": "one\ntwo\n"}); err != nil {
			t.Fatal(err)
		}
	}
	return p, inst
}

func TestSessionOptionsOfferTheRepositoriesTheInstallationCovers(t *testing.T) {
	p, _ := installedStage(t, nil)
	opts, err := p.SessionOptions(t.Context(), admin)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(opts.Repos, ","); got != "acme/api,acme/web,other/docs,partner/tools" {
		t.Fatalf("repos = %s, want the installed ones and the named one, sorted", got)
	}
	// Another workspace is offered none of them.
	other := admin
	other.Tenant = "globex"
	if opts, err := p.SessionOptions(t.Context(), other); err != nil || len(opts.Repos) != 0 {
		t.Fatalf("another workspace was offered %v, %v", opts.Repos, err)
	}
}

func TestSessionOptionsFallBackToTheNamedRepositoriesWhenListingFails(t *testing.T) {
	p, _ := installedStage(t, errors.New("github down"))
	opts, err := p.SessionOptions(t.Context(), admin)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(opts.Repos, ","); got != "other/docs" {
		t.Fatalf("repos = %s, want the named one alone", got)
	}
}

func TestASessionMayChooseAnInstalledRepositoryAndNoOther(t *testing.T) {
	p, _ := installedStage(t, nil)
	_, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Text: "look", Repos: []string{"stranger/secret"}})
	if !errors.Is(err, threads.ErrInvalid) || !strings.Contains(err.Error(), "not a repository you may choose") {
		t.Fatalf("an uncovered repository: %v", err)
	}
	_, err = p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Text: "look", Repos: []string{"partner/tools"}})
	if err != nil && strings.Contains(err.Error(), "not a repository you may choose") {
		t.Fatalf("an installed repository was refused: %v", err)
	}
}

func TestGitHubSourceReadsARepositoryTheInstallationCovers(t *testing.T) {
	p, inst := installedStage(t, nil)
	// partner/tools is no owner the client serves and no repository it
	// names: the installation covering it is what grants the read.
	ex, err := p.GitHubSource(t.Context(), tenant, platform.SourceRequest{Repo: "partner/tools", SHA: sourceSHA, Path: "a.go", Start: 1, End: 2})
	if err != nil || len(ex.Lines) != 2 {
		t.Fatalf("an installed repository: %+v, %v", ex, err)
	}
	if _, err := p.GitHubSource(t.Context(), tenant, platform.SourceRequest{Repo: "stranger/secret", SHA: sourceSHA, Path: "a.go", Start: 1, End: 1}); !errors.Is(err, platform.ErrSourceNotFound) {
		t.Fatalf("an uncovered repository: %v, want not found", err)
	}
	// A failed listing keeps the list it had: a GitHub outage revokes nothing
	// the token could still reach, and grants nothing new.
	inst.mu.Lock()
	inst.err = errors.New("github down")
	inst.mu.Unlock()
	if _, err := p.GitHubSource(t.Context(), tenant, platform.SourceRequest{Repo: "partner/tools", SHA: sourceSHA, Path: "a.go", Start: 2, End: 2}); err != nil {
		t.Fatalf("after a failed listing: %v", err)
	}
}
