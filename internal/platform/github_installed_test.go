package platform_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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

// barrier is an installation whose listing waits until every installation
// of the test is listing at once, so it answers only if they are read side
// by side.
type barrier struct {
	*github.Fake
	repos   []string
	started *atomic.Int32
	all     chan struct{}
	want    int32
}

func (b *barrier) InstallationRepositories(ctx context.Context, _ int) ([]string, error) {
	if b.started.Add(1) == b.want {
		close(b.all)
	}
	select {
	case <-b.all:
		return b.repos, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestSessionOptionsReadEveryInstallationSideBySide(t *testing.T) {
	f := &fakes{}
	started, all := &atomic.Int32{}, make(chan struct{})
	apis := map[string]*barrier{
		"token-a": {Fake: f.githubFor("token-a"), repos: []string{"acme/api"}, started: started, all: all, want: 2},
		"token-b": {Fake: f.githubFor("token-b"), repos: []string{"globex/web"}, started: started, all: all, want: 2},
	}
	cfg := f.config()
	cfg.GitHubAPI = func(token string) github.API { return apis[token] }
	p := stage(t, &cast{}, cfg)
	for id, owner := range map[string]string{"a": "acme", "b": "globex"} {
		create(t, p, tenant, platform.ClientInput{
			Client: clients.Client{
				ID: "gh-" + id, Name: "gh-" + id, Surface: clients.GitHub, Enabled: true,
				Identity: clients.Identity{Owners: []string{owner}}, DefaultAgent: "docs",
			},
			Token: clients.SecretInput{Value: "token-" + id}, SigningSecret: clients.SecretInput{Value: "secret-" + id},
		})
	}
	// Read in turn, the first listing would wait out this deadline alone.
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	opts, err := p.SessionOptions(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(opts.Repos, ","); got != "acme/api,globex/web" {
		t.Fatalf("repos = %s, want both installations' repositories", got)
	}
}

func TestSessionOptionsWithACancelledContextOfferTheNamedRepositories(t *testing.T) {
	f := &fakes{}
	fake := f.githubFor("token-gh")
	gate := make(chan struct{})
	t.Cleanup(func() { close(gate) })
	api := &slowInstallation{Fake: fake, gate: gate}
	cfg := f.config()
	cfg.GitHubAPI = func(string) github.API { return api }
	p := stage(t, &cast{}, cfg)
	create(t, p, tenant, platform.ClientInput{
		Client: clients.Client{
			ID: "gh", Name: "gh", Surface: clients.GitHub, Enabled: true,
			Identity: clients.Identity{Owners: []string{"acme"}}, DefaultAgent: "docs",
			GitHub: &clients.GitHubOptions{ReadRepositories: []string{"other/docs"}},
		},
		Token: clients.SecretInput{Value: "token-gh"}, SigningSecret: clients.SecretInput{Value: "secret-gh"},
	})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	opts, err := p.SessionOptions(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(opts.Repos, ","); got != "other/docs" {
		t.Fatalf("repos = %s, want the named one while the listing has not answered", got)
	}
}

// slowInstallation is an installation whose listing answers once gate closes.
type slowInstallation struct {
	*github.Fake
	gate chan struct{}
}

func (g *slowInstallation) InstallationRepositories(ctx context.Context, _ int) ([]string, error) {
	select {
	case <-g.gate:
		return []string{"acme/api"}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
