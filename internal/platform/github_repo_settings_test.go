package platform_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/integrations/github"
	"github.com/urmzd/humagen/pkg/prompts"
	"github.com/urmzd/humagen/pkg/reposettings"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
	saigetypes "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/runtime"
)

// repoStage is a tenant with one GitHub client serving acme, whose default
// agent is lead and whose automatic review is left out, so off. reviewer is
// another agent it routes to.
func repoStage(t *testing.T) (*platform.Platform, *fakes, func(kind, delivery, body string)) {
	t.Helper()
	f := &fakes{}
	answers := map[string][][]saigetypes.Delta{}
	for _, name := range []string{"lead", "reviewer"} {
		for range 4 {
			answers[name] = append(answers[name], say("Done."))
		}
	}
	p := stage(t, &cast{says: answers}, f.config())
	create(t, p, tenant, platform.ClientInput{
		Client: clients.Client{
			ID: "gh", Name: "GitHub", Surface: clients.GitHub, Enabled: true, Agents: []string{clients.AnyAgent},
			Identity:     clients.Identity{Owners: []string{"acme"}, Installation: "101"},
			DefaultAgent: "lead", GitHub: &clients.GitHubOptions{Login: "humagen[bot]", ReadRepositories: []string{"acme/site", "acme/other", "acme/quiet"}},
		},
		Token: clients.SecretInput{Value: "token-gh"}, SigningSecret: clients.SecretInput{Value: "secret-gh"},
	})
	deliver := func(kind, delivery, body string) {
		t.Helper()
		mac := hmac.New(sha256.New, []byte("secret-gh"))
		mac.Write([]byte(body))
		r := httptest.NewRequest(http.MethodPost, "/connectors/github", strings.NewReader(body))
		r.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
		r.Header.Set("X-GitHub-Event", kind)
		r.Header.Set("X-GitHub-Delivery", delivery)
		w := httptest.NewRecorder()
		p.IngressHandler().ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("%s delivery = %d %s", kind, w.Code, w.Body)
		}
	}
	return p, f, deliver
}

func pullDelivery(action, repo, sha string) string {
	owner, name, _ := strings.Cut(repo, "/")
	return fmt.Sprintf(`{"action":%q,"number":7,"installation":{"id":101},"repository":{"full_name":%q,"name":%q,"owner":{"login":%q}},"sender":{"login":"ada","type":"User"},"pull_request":{"number":7,"state":"open","draft":false,"user":{"login":"ada"},"author_association":"MEMBER","head":{"sha":%q}}}`, action, repo, name, owner, sha)
}

func reviewRun(p *platform.Platform, repo, sha string) (runs.Run, bool) {
	r, err := p.Runs.Get(tenant, runtime.RunID(tenant, "autoreview/gh/"+strings.ReplaceAll(strings.ToLower(repo), "/", "%2F")+"/7/"+sha))
	return r, err == nil
}

// No pull request is reviewed on its own until a repository turns review on
// in its settings; then only the events it chose are reviewed, by the agent
// it chose, and only there. Returning it to the client turns review off
// again.
func TestARepositorysSettingsDecideItsAutomaticReview(t *testing.T) {
	p, _, deliver := repoStage(t)
	sha := func(i int) string { return fmt.Sprintf("%040x", i) }

	deliver("pull_request", "d0", pullDelivery("opened", "acme/site", sha(0)))
	if _, ok := reviewRun(p, "acme/site", sha(0)); ok {
		t.Fatal("a pull request was reviewed with automatic review left out")
	}

	kept, err := p.SetGitHubRepository(t.Context(), admin, "gh", "acme/site", reposettings.Settings{AutoReview: &reposettings.AutoReview{Opened: true}, Agent: "reviewer"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !kept.AutoReview.Opened || kept.AutoReview.Pushes || kept.Agent != "reviewer" || kept.Version != 1 {
		t.Fatalf("kept %+v", kept)
	}
	deliver("pull_request", "d1", pullDelivery("reopened", "acme/site", sha(1)))
	run, ok := reviewRun(p, "acme/site", sha(1))
	if !ok || run.Agent != "reviewer" {
		t.Fatalf("the repository's review: %+v, %v", run, ok)
	}
	deliver("pull_request", "d2", pullDelivery("synchronize", "acme/site", sha(2)))
	if _, ok := reviewRun(p, "acme/site", sha(2)); ok {
		t.Fatal("a push was reviewed where only opening is")
	}
	deliver("pull_request", "d3", pullDelivery("opened", "acme/other", sha(3)))
	if _, ok := reviewRun(p, "acme/other", sha(3)); ok {
		t.Fatal("another repository was reviewed")
	}

	if _, err := p.ResetGitHubRepository(t.Context(), admin, "gh", "acme/site", kept.Version); err != nil {
		t.Fatal(err)
	}
	deliver("pull_request", "d4", pullDelivery("reopened", "acme/site", sha(4)))
	if _, ok := reviewRun(p, "acme/site", sha(4)); ok {
		t.Fatal("a repository returned to its client was still reviewed")
	}
}

// The client's own switch, which a deployment sets from its environment, is
// the default, and a repository's settings win over it either way.
func TestTheClientsSwitchIsTheDefaultARepositoryOverrides(t *testing.T) {
	p, _, deliver := repoStage(t)
	c, err := p.Clients.Get(tenant, "gh")
	if err != nil {
		t.Fatal(err)
	}
	c.GitHub.AutoReview = reviewOn()
	if _, err := p.UpdateClient(t.Context(), tenant, "gh", "test", platform.ClientInput{Client: c}); err != nil {
		t.Fatal(err)
	}
	first, second := strings.Repeat("a", 40), strings.Repeat("b", 40)
	deliver("pull_request", "d1", pullDelivery("opened", "acme/site", first))
	if run, ok := reviewRun(p, "acme/site", first); !ok || run.Agent != "lead" {
		t.Fatalf("the client's default review: %+v, %v", run, ok)
	}
	if _, err := p.SetGitHubRepository(t.Context(), admin, "gh", "acme/quiet", reposettings.Settings{AutoReview: &reposettings.AutoReview{}}, 0); err != nil {
		t.Fatal(err)
	}
	deliver("pull_request", "d2", pullDelivery("opened", "acme/quiet", second))
	if _, ok := reviewRun(p, "acme/quiet", second); ok {
		t.Fatal("a repository that turned review off was reviewed")
	}
	list, err := p.GitHubRepositories(t.Context(), tenant, "gh")
	if err != nil || !list.Defaults.AutoReview.Opened || list.Defaults.Agent != "lead" {
		t.Fatalf("defaults %+v, %v", list.Defaults, err)
	}
	i := slices.IndexFunc(list.Repositories, func(r platform.GitHubRepository) bool { return r.Repository == "acme/quiet" })
	if i < 0 || list.Repositories[i].AutoReview.On() || list.Repositories[i].Settings == nil {
		t.Fatalf("acme/quiet is listed as %+v", list.Repositories)
	}
}

// A mention that names no agent is answered by the repository's agent, its
// run is given the repository's instructions, and what the repository asks
// to be read is read before the run starts.
func TestAMentionFollowsTheRepositorysAgentInstructionsAndReads(t *testing.T) {
	p, f, deliver := repoStage(t)
	fake := f.githubFor("token-gh")
	pull := github.Context{Subject: github.ContextSubject{Number: 7, Title: "Speed up search", Body: "Fixes #3 and see acme/other#9.", State: "open"}}
	pull.Subject.Head.SHA = strings.Repeat("c", 40)
	pull.Files = []github.ContextFile{{Filename: "search.go", Status: "modified", Patch: "@@ -1 +1 @@\n-slow\n+fast"}}
	issue := github.Context{Subject: github.ContextSubject{Number: 3, Title: "Search is slow", Body: "It takes ten seconds.", State: "open"}}
	for ref, c := range map[github.Ref]github.Context{{Owner: "acme", Repo: "site", Number: 7}: pull, {Owner: "acme", Repo: "site", Number: 3}: issue} {
		if err := fake.SeedContext(ref, c); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.SetGitHubRepository(t.Context(), admin, "gh", "acme/site", reposettings.Settings{
		Agent: "reviewer", Instructions: "Run make check before you answer.", Reads: []string{reposettings.ReadPullDiff, reposettings.ReadLinkedIssues},
	}, 0); err != nil {
		t.Fatal(err)
	}
	deliver("issue_comment", "c1", `{"action":"created","installation":{"id":101},"repository":{"full_name":"acme/site","name":"site","owner":{"login":"acme"}},"sender":{"login":"ada","type":"User"},"issue":{"number":7,"pull_request":{"url":"x"}},"comment":{"id":91,"body":"@humagen is this safe?","user":{"login":"ada"},"author_association":"MEMBER"}}`)
	var run runs.Run
	eventually(t, "the mention starts a run", func() bool {
		for _, r := range p.Runs.List(tenant, 50) {
			if strings.Contains(r.Text, "is this safe?") {
				run = r
				return true
			}
		}
		return false
	})
	if run.Agent != "reviewer" {
		t.Fatalf("the mention was answered by %s, want the repository's agent", run.Agent)
	}
	for _, want := range []string{"Pull request #7: Speed up search", "search.go (modified)", "+fast", "Linked #3: Search is slow", "It takes ten seconds."} {
		if !strings.Contains(run.Text, want) {
			t.Errorf("the run's text lacks %q:\n%s", want, run.Text)
		}
	}
	if strings.Contains(run.Text, "Linked #9") {
		t.Errorf("an issue of another repository was read:\n%s", run.Text)
	}
	snapshot, ok, err := prompts.Read(wire.Meta(run.Tag()))
	if err != nil || !ok {
		t.Fatalf("no instructions pinned: %v", err)
	}
	last := snapshot.Layers[len(snapshot.Layers)-1]
	if last.Kind != "repository" || !strings.Contains(last.Text, "Run make check before you answer.") {
		t.Fatalf("the repository's instructions are not the last layer: %+v", snapshot.Layers)
	}
}

// Writing settings checks the agent, the repository and the version, and
// another workspace finds neither the client nor its repositories.
func TestRepositorySettingsAreCheckedAndTenantScoped(t *testing.T) {
	p, _, _ := repoStage(t)
	if _, err := p.SetGitHubRepository(t.Context(), admin, "gh", "acme/site", reposettings.Settings{Agent: "nobody"}, 0); !errors.Is(err, reposettings.ErrInvalid) {
		t.Fatalf("an agent that does not exist: %v", err)
	}
	if _, err := p.SetGitHubRepository(t.Context(), admin, "gh", "globex/site", reposettings.Settings{Agent: "lead"}, 0); !errors.Is(err, clients.ErrNotFound) {
		t.Fatalf("a repository the client does not cover: %v", err)
	}
	// Serving the account is not covering the repository: only the
	// installation's repositories, the named ones and those with settings are.
	if _, err := p.GitHubRepository(t.Context(), tenant, "gh", "acme/unnamed"); !errors.Is(err, clients.ErrNotFound) {
		t.Fatalf("a repository of an account the client serves, but not covered: %v", err)
	}
	kept, err := p.SetGitHubRepository(t.Context(), admin, "gh", "acme/site", reposettings.Settings{Agent: "lead"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.SetGitHubRepository(t.Context(), admin, "gh", "acme/site", reposettings.Settings{Agent: "reviewer"}, 0); !errors.Is(err, reposettings.ErrVersion) {
		t.Fatalf("a write over settings not read: %v", err)
	}
	other := auth.Principal{Subject: "eve", Tenant: "globex", Kind: auth.User, Roles: []auth.Role{auth.Admin}}
	if _, err := p.GitHubRepositories(t.Context(), other.Tenant, "gh"); !errors.Is(err, clients.ErrNotFound) {
		t.Fatalf("another workspace lists the client's repositories: %v", err)
	}
	if _, err := p.SetGitHubRepository(t.Context(), other, "gh", "acme/site", reposettings.Settings{Agent: "lead"}, kept.Version); !errors.Is(err, clients.ErrNotFound) {
		t.Fatalf("another workspace writes the client's repository: %v", err)
	}
	if _, err := p.ResetGitHubRepository(t.Context(), other, "gh", "acme/site", kept.Version); !errors.Is(err, clients.ErrNotFound) {
		t.Fatalf("another workspace resets the client's repository: %v", err)
	}
}

// A repository kept to its own runs is not offered to a session elsewhere,
// not chosen for one, and its source is not shown in the console, while the
// repositories beside it still are.
func TestARepositoryKeptToItsOwnRunsIsNotReadElsewhere(t *testing.T) {
	p, _ := installedStage(t, nil)
	off := false
	if _, err := p.SetGitHubRepository(t.Context(), admin, "gh", "partner/tools", reposettings.Settings{CrossSurfaceReads: &off}, 0); err != nil {
		t.Fatal(err)
	}
	opts, err := p.SessionOptions(t.Context(), admin)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(opts.Repos, ","); got != "acme/api,acme/web,other/docs" {
		t.Fatalf("repos = %s, want partner/tools left out", got)
	}
	if _, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Text: "look", Repos: []string{"partner/tools"}}); !errors.Is(err, threads.ErrInvalid) || !strings.Contains(err.Error(), "not a repository you may choose") {
		t.Fatalf("a session chose a repository kept to its own runs: %v", err)
	}
	if _, err := p.GitHubSource(t.Context(), tenant, platform.SourceRequest{Repo: "partner/tools", SHA: sourceSHA, Path: "a.go", Start: 1, End: 1}); !errors.Is(err, platform.ErrSourceNotFound) {
		t.Fatalf("the console read a repository kept to its own runs: %v", err)
	}
	if _, err := p.GitHubSource(t.Context(), tenant, platform.SourceRequest{Repo: "acme/api", SHA: sourceSHA, Path: "a.go", Start: 1, End: 1}); err != nil {
		t.Fatalf("a repository beside it: %v", err)
	}
	list, err := p.GitHubRepositories(t.Context(), tenant, "gh")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range list.Repositories {
		if want := r.Repository != "partner/tools"; r.CrossSurfaceReads != want {
			t.Errorf("%s is listed readable elsewhere = %v", r.Repository, r.CrossSurfaceReads)
		}
	}
}

// A repository one client keeps to its own runs is kept so in the whole
// workspace: a second client that covers it reads it no more than the first.
func TestARepositoryKeptToItsOwnRunsByOneClientIsKeptByEvery(t *testing.T) {
	p, _ := installedStage(t, nil)
	create(t, p, tenant, platform.ClientInput{
		Client: clients.Client{
			ID: "gh-partner", Name: "gh-partner", Surface: clients.GitHub, Enabled: true,
			Identity:     clients.Identity{Owners: []string{"partner"}},
			DefaultAgent: "docs", GitHub: &clients.GitHubOptions{ReadRepositories: []string{"partner/tools"}},
		},
		Token: clients.SecretInput{Value: "token-gh-partner"}, SigningSecret: clients.SecretInput{Value: "secret-gh-partner"},
	})
	read := func() error {
		_, err := p.GitHubSource(t.Context(), tenant, platform.SourceRequest{Repo: "partner/tools", SHA: sourceSHA, Path: "a.go", Start: 1, End: 1})
		return err
	}
	if err := read(); err != nil {
		t.Fatalf("before any setting: %v", err)
	}
	off := false
	// The second client keeps it; the first, whose installation covers it,
	// is the one the console would otherwise read through.
	if _, err := p.SetGitHubRepository(t.Context(), admin, "gh-partner", "partner/tools", reposettings.Settings{CrossSurfaceReads: &off}, 0); err != nil {
		t.Fatal(err)
	}
	if err := read(); !errors.Is(err, platform.ErrSourceNotFound) {
		t.Fatalf("the console read it through the other client: %v", err)
	}
	opts, err := p.SessionOptions(t.Context(), admin)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(opts.Repos, "partner/tools") {
		t.Fatalf("the picker offers it through the other client: %v", opts.Repos)
	}
	if _, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Text: "look", Repos: []string{"partner/tools"}}); !errors.Is(err, threads.ErrInvalid) {
		t.Fatalf("a session chose it through the other client: %v", err)
	}
	// Both clients' lists show the workspace's answer and the client that
	// keeps it, the one that does not keep it too, with no setting of its own.
	for _, id := range []string{"gh-partner", "gh"} {
		list, err := p.GitHubRepositories(t.Context(), tenant, id)
		if err != nil {
			t.Fatal(err)
		}
		i := slices.IndexFunc(list.Repositories, func(r platform.GitHubRepository) bool { return r.Repository == "partner/tools" })
		if i < 0 || list.Repositories[i].CrossSurfaceReads || !slices.Equal(list.Repositories[i].KeptBy, []string{"gh-partner"}) {
			t.Fatalf("%s lists partner/tools as %+v", id, list.Repositories)
		}
		if id == "gh" && list.Repositories[i].Settings != nil {
			t.Fatalf("gh has settings of its own for partner/tools: %+v", list.Repositories[i].Settings)
		}
	}
	one, err := p.GitHubRepository(t.Context(), tenant, "gh", "partner/tools")
	if err != nil || one.CrossSurfaceReads || !slices.Equal(one.KeptBy, []string{"gh-partner"}) {
		t.Fatalf("gh reads partner/tools as %+v, %v", one, err)
	}
	// A repository nobody keeps reads as readable, with nobody named.
	if api, err := p.GitHubRepository(t.Context(), tenant, "gh", "acme/api"); err != nil || !api.CrossSurfaceReads || len(api.KeptBy) != 0 {
		t.Fatalf("acme/api reads as %+v, %v", api, err)
	}
}
