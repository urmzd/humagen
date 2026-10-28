package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/signature"
	"github.com/urmzd/humagen/internal/wire"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/integrations/github"
	"github.com/urmzd/humagen/pkg/integrations/slack"
	"github.com/urmzd/humagen/pkg/spec"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func envOf(vars map[string]string) func(string) string {
	return func(name string) string { return vars[name] }
}

// With nothing set there are no clients, so nothing is exposed.
func TestNoEnvironmentConfiguresNoClient(t *testing.T) {
	got, err := defaultClients(envOf(nil))
	if err != nil || len(got) != 0 {
		t.Fatalf("%d clients, %v", len(got), err)
	}
	p, err := platform.New(platform.Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := seedClients(context.Background(), p, "default", envOf(nil)); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/connectors/slack", "/connectors/slack/commands", "/connectors/github"} {
		w := httptest.NewRecorder()
		p.IngressHandler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`)))
		if w.Code != http.StatusNotFound {
			t.Errorf("%s with no clients = %d, want 404", path, w.Code)
		}
	}
}

// A half-configured or malformed environment stops the server from starting,
// as it did when the environment was the only configuration.
func TestAMalformedEnvironmentIsAnError(t *testing.T) {
	for name, vars := range map[string]map[string]string{
		"a slack token with no secret":  {EnvSlackToken: "t"},
		"slack with no team":            {EnvSlackToken: "t", EnvSlackSecret: "s"},
		"slack sessions not a boolean":  {EnvSlackToken: "t", EnvSlackSecret: "s", EnvSlackTeam: "T01", EnvSlackSessions: "perhaps"},
		"slack commands not json":       {EnvSlackToken: "t", EnvSlackSecret: "s", EnvSlackTeam: "T01", EnvSlackCommands: "create-pr"},
		"a github secret with no token": {EnvGitHubSecret: "s"},
		"github with no owners":         {EnvGitHubToken: "t", EnvGitHubSecret: "s"},
		"github labels not json":        {EnvGitHubToken: "t", EnvGitHubSecret: "s", EnvGitHubOwners: "acme", EnvGitHubAutoLabels: "{"},
	} {
		if _, err := defaultClients(envOf(vars)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// The variables that configured the one installation per surface now yield
// exactly one default client per surface, which works as that installation
// did: any agent in the tenant, the same options, the secrets left where they
// were.
func TestTheLegacyEnvironmentYieldsOneWorkingDefaultClientPerSurface(t *testing.T) {
	vars := map[string]string{
		EnvSlackToken: "xoxb-legacy", EnvSlackSecret: "legacy-slack-secret", EnvSlackTeam: "T01ABCDEF",
		EnvSlackDefault: "helper", EnvSlackSessions: "true", EnvSlackBots: "U08BOT=helper",
		EnvSlackCommands: `[{"name":"create-pr","description":"Open a pull request","argument_hint":"[title]"}]`,
		EnvGitHubToken:   "ghs-legacy", EnvGitHubSecret: "legacy-github-secret-of-16-bytes", EnvGitHubOwners: "acme, acme-labs",
		EnvGitHubBots: "helper[bot]=helper", EnvGitHubChecks: "false", EnvGitHubReadRepositories: "acme/handbook",
		EnvGitHubAutoLabels: `{"acme/handbook":{"agent:auto":"helper"}}`, EnvGitHubLogin: "humagen[bot]", EnvGitHubDefault: "helper",
	}
	defaults, err := defaultClients(envOf(vars))
	if err != nil {
		t.Fatal(err)
	}
	if len(defaults) != 2 {
		t.Fatalf("%d default clients, want one per surface", len(defaults))
	}

	refs, err := secretRefs("", "trial", envOf(vars))
	if err != nil {
		t.Fatal(err)
	}
	slackFake, githubFake := slack.NewFake(), github.NewFake()
	tokens := map[string]bool{}
	p, err := platform.New(platform.Config{
		Workspace: t.TempDir(), ClientInterval: -1,
		Env:        func(name string) (string, bool) { v, ok := vars[name]; return v, ok },
		SecretRefs: refs,
		SlackAPI:   func(token string) slack.API { tokens[token] = true; return slackFake },
		GitHubAPI:  func(token string) github.API { tokens[token] = true; return githubFake },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("platform shutdown: %v", err)
		}
	})
	if _, err := p.Agents.Create(ctx, "trial", wire.AgentSpec{Name: "helper", Provider: "offline", Model: "offline", SystemPrompt: "Answer."}, "test"); err != nil {
		t.Fatal(err)
	}
	if err := seedClients(ctx, p, "trial", envOf(vars)); err != nil {
		t.Fatal(err)
	}
	if !tokens["xoxb-legacy"] || !tokens["ghs-legacy"] {
		t.Fatalf("the connectors were built with %v", tokens)
	}

	all := p.Clients.List("trial")
	if len(all) != 2 || len(p.Clients.List("default")) != 0 {
		t.Fatalf("clients = %+v", all)
	}
	for _, c := range all {
		if c.Managed != clients.ManagedEnv || !c.Enabled || len(c.Agents) != 1 || c.Agents[0] != clients.AnyAgent {
			t.Errorf("%s = %+v, want an enabled, env-managed client for any agent", c.ID, c)
		}
		// The record names where the secret is and holds none.
		if !strings.HasPrefix(c.Credentials.Token.Ref, "env:HUMAGEN_") || !strings.HasPrefix(c.Credentials.SigningSecret.Ref, "env:HUMAGEN_") {
			t.Errorf("%s credentials = %+v", c.ID, c.Credentials)
		}
	}
	sl, _ := p.Clients.Get("trial", defaultSlackClient)
	if sl.Identity.Team != "T01ABCDEF" || sl.DefaultAgent != "helper" || !sl.Slack.Sessions || len(sl.Slack.Commands) != 1 || sl.Bots["U08BOT"] != "helper" {
		t.Fatalf("slack client = %+v", sl)
	}
	gh, _ := p.Clients.Get("trial", defaultGitHubClient)
	if len(gh.Identity.Owners) != 2 || gh.GitHub.AutoLabels["acme/handbook"]["agent:auto"] != "helper" || gh.GitHub.ReadRepositories[0] != "acme/handbook" ||
		gh.GitHub.Login != "humagen[bot]" || gh.DefaultAgent != "helper" {
		t.Fatalf("github client = %+v", gh)
	}

	ingress := p.IngressHandler()
	ready := func() bool {
		w := httptest.NewRecorder()
		ingress.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		return w.Code == http.StatusOK
	}
	wait := func(what string, ok func() bool) {
		t.Helper()
		for deadline := time.Now().Add(10 * time.Second); !ok(); time.Sleep(5 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("never happened: %s", what)
			}
		}
	}
	wait("the default clients are ingesting", ready)

	// The legacy payloads carry no app id, and are still routed and verified.
	slackBody := `{"type":"event_callback","event_id":"slack-1","team_id":"T01ABCDEF","event":{"type":"app_mention","user":"U01","channel":"C01","ts":"1700000000.000100","text":"explain"}}`
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(vars[EnvSlackSecret]))
	mac.Write([]byte("v0:" + ts + ":" + slackBody))
	r := httptest.NewRequest(http.MethodPost, "/connectors/slack", strings.NewReader(slackBody))
	r.Header.Set(signature.HeaderSlackTimestamp, ts)
	r.Header.Set(signature.HeaderSlackSignature, "v0="+hex.EncodeToString(mac.Sum(nil)))
	w := httptest.NewRecorder()
	ingress.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("slack webhook = %d %s", w.Code, w.Body)
	}

	githubBody := `{"action":"created","repository":{"name":"repo","owner":{"login":"acme"}},"sender":{"login":"human","type":"User"},"issue":{"number":7},"comment":{"id":91,"body":"@helper explain"}}`
	mac = hmac.New(sha256.New, []byte(vars[EnvGitHubSecret]))
	mac.Write([]byte(githubBody))
	r = httptest.NewRequest(http.MethodPost, "/connectors/github", strings.NewReader(githubBody))
	r.Header.Set(signature.HeaderGitHubSignature, "sha256="+hex.EncodeToString(mac.Sum(nil)))
	r.Header.Set("X-GitHub-Event", "issue_comment")
	r.Header.Set("X-GitHub-Delivery", "github-1")
	w = httptest.NewRecorder()
	ingress.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("github webhook = %d %s", w.Code, w.Body)
	}

	wait("both surfaces were answered", func() bool { return len(slackFake.Messages()) >= 1 && len(githubFake.Comments()) == 1 })
	// Slack's first message can be the placeholder it posts before the run
	// is admitted, so the runs are waited for too.
	wait("both runs were admitted", func() bool { return len(p.Runs.List("trial", 10)) == 2 })
	origins := map[string]string{}
	for _, run := range p.Runs.List("trial", 10) {
		origins[run.Origin] = run.Client
	}
	if origins["slack"] != defaultSlackClient || origins["github"] != defaultGitHubClient {
		t.Fatalf("runs by origin = %v", origins)
	}
	// Seeding twice, as a restart with a durable store would, is refused
	// rather than doubled.
	if err := seedClients(ctx, p, "trial", envOf(vars)); err == nil {
		t.Fatal("the default clients were created twice")
	}
}

// Which variables a tenant may reference is the operator's to say, on the
// command line. The server's own tenant is also given the variables behind
// its default clients, and no other tenant is.
func TestSecretReferencesAreGivenToTenantsByTheOperator(t *testing.T) {
	env := envOf(map[string]string{EnvSlackToken: "t", EnvSlackSecret: "s"})
	got, err := secretRefs("acme=HUMAGEN_SLACK_TOKEN_DOCS, HUMAGEN_SLACK_SECRET_DOCS; globex=HUMAGEN_GH_GLOBEX;", "trial", env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || len(got["acme"]) != 2 || got["globex"][0] != "HUMAGEN_GH_GLOBEX" {
		t.Fatalf("refs = %v", got)
	}
	if len(got["trial"]) != 2 || got["trial"][0] != EnvSlackToken || got["trial"][1] != EnvSlackSecret {
		t.Fatalf("the server's tenant = %v, want its default slack client's variables and no github ones", got["trial"])
	}
	for _, other := range []string{"acme", "globex"} {
		for _, name := range got[other] {
			if name == EnvSlackToken || name == EnvSlackSecret {
				t.Fatalf("%s was given the default client's variables", other)
			}
		}
	}
	// With nothing configured, nobody may reference anything.
	if got, err := secretRefs("", "trial", envOf(nil)); err != nil || len(got) != 0 {
		t.Fatalf("refs with nothing set = %v, %v", got, err)
	}
	for _, raw := range []string{"acme", "=HUMAGEN_X", "acme=", "acme=ANTHROPIC_API_KEY", "acme=HUMAGEN_OIDC_CLIENT_SECRET", "acme=humagen_x"} {
		if _, err := secretRefs(raw, "trial", envOf(nil)); err == nil {
			t.Errorf("-secret-refs %q was accepted", raw)
		}
	}
}

// The file's GitHub installation carries the app's own login, which tells a
// comment addressed to the app apart from one addressed to nobody. A login
// that is not one fails the start.
func TestTheFilesGitHubClientCarriesTheAppsLogin(t *testing.T) {
	vars := map[string]string{EnvGitHubToken: "ghs-file", EnvGitHubSecret: "file-github-secret-of-16-bytes"}
	apply := func(login string) (*platform.Platform, error) {
		p, err := platform.New(platform.Config{
			Workspace:  t.TempDir(),
			Env:        func(name string) (string, bool) { v, ok := vars[name]; return v, ok },
			SecretRefs: map[string][]string{"acme": {EnvGitHubToken, EnvGitHubSecret}},
			GitHubAPI:  func(string) github.API { return github.NewFake() },
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = p.Close() })
		g := spec.GitHub{
			Enabled: true, Owners: []string{"acme"}, Login: login,
			Token: &spec.Ref{Ref: "env:" + EnvGitHubToken}, WebhookSecret: &spec.Ref{Ref: "env:" + EnvGitHubSecret},
		}
		return p, applyGitHub(t.Context(), p, "acme", g, slog.Default())
	}
	p, err := apply("humagen[bot]")
	if err != nil {
		t.Fatal(err)
	}
	c, err := p.Clients.Get("acme", "file-github")
	if err != nil || c.GitHub == nil || c.GitHub.Login != "humagen[bot]" || c.Managed != clients.ManagedFile {
		t.Fatalf("the file's client = %+v, %v", c, err)
	}
	if _, err := apply("@humagen"); err == nil {
		t.Fatal("a login that is no GitHub login was accepted")
	}
}

// The file is applied at every start, its GitHub installation too: a client an
// earlier start kept follows the file, so a login added later takes effect at
// the next start. An unchanged file makes no new version, the API still
// cannot change the client, and a client of that id the console made is left
// as it is.
func TestTheFilesGitHubClientFollowsTheFileAtEveryStart(t *testing.T) {
	vars := map[string]string{EnvGitHubToken: "ghs-file", EnvGitHubSecret: "file-github-secret-of-16-bytes"}
	start := func(st state.Store) *platform.Platform {
		p, err := platform.New(platform.Config{
			Workspace:  t.TempDir(),
			Backends:   platform.Backends{State: st},
			Env:        func(name string) (string, bool) { v, ok := vars[name]; return v, ok },
			SecretRefs: map[string][]string{"acme": {EnvGitHubToken, EnvGitHubSecret}},
			GitHubAPI:  func(string) github.API { return github.NewFake() },
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = p.Close() })
		return p
	}
	file := func(login string) spec.GitHub {
		return spec.GitHub{
			Enabled: true, Owners: []string{"acme"}, Login: login,
			Token: &spec.Ref{Ref: "env:" + EnvGitHubToken}, WebhookSecret: &spec.Ref{Ref: "env:" + EnvGitHubSecret},
		}
	}
	apply := func(p *platform.Platform, g spec.GitHub) clients.Client {
		t.Helper()
		if err := applyGitHub(t.Context(), p, "acme", g, slog.Default()); err != nil {
			t.Fatal(err)
		}
		c, err := p.Clients.Get("acme", "file-github")
		if err != nil {
			t.Fatal(err)
		}
		return c
	}

	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		if c := apply(start(st), file("")); c.Version != 1 || c.GitHub.Login != "" {
			t.Fatalf("the first start kept %+v", c)
		}
		p := start(st)
		c := apply(p, file("humagen[bot]"))
		if c.Version != 2 || c.GitHub == nil || c.GitHub.Login != "humagen[bot]" || c.Managed != clients.ManagedFile || !c.Enabled {
			t.Fatalf("after the login was added, the client is %+v", c)
		}
		if c.Credentials.Token.Ref != "env:"+EnvGitHubToken || c.Credentials.SigningSecret.Ref != "env:"+EnvGitHubSecret {
			t.Fatalf("the client's secrets are %+v", c.Credentials)
		}
		if again := apply(start(st), file("humagen[bot]")); again.Version != 2 || again.GitHub.Login != "humagen[bot]" {
			t.Fatalf("an unchanged file made %+v", again)
		}
		if _, err := p.UpdateClient(t.Context(), "acme", "file-github", "admin", platform.ClientInput{Client: c}); !errors.Is(err, clients.ErrManaged) {
			t.Fatalf("the API changed the file's client: %v", err)
		}
	})

	// A console client of that id is not the file's to change.
	console := start(state.NewMemory())
	made, err := console.CreateClient(t.Context(), "acme", "admin", platform.ClientInput{
		Client: clients.Client{
			ID: "file-github", Name: "GitHub", Surface: clients.GitHub, Enabled: true,
			Identity: clients.Identity{Owners: []string{"acme"}}, DefaultAgent: "helper",
		},
		Token: clients.SecretInput{Value: "ghs-console"}, SigningSecret: clients.SecretInput{Value: "console-secret-of-16-bytes"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if kept := apply(console, file("humagen[bot]")); kept.Managed != "" || kept.Version != made.Version || kept.GitHub != nil {
		t.Fatalf("the file took over the console's client: %+v", kept)
	}
}

// A section that turns GitHub off turns off the client an earlier start kept.
// The record stays the file's and stops answering, so a signed hook is
// answered 404 as for no client, and the next start keeps it off without a new
// version. Turning it off needs none of the section's other fields. Turning
// the section back on brings the client back. A console client of that id is
// left as it is.
func TestEnabledFalseTurnsTheFilesGitHubClientOff(t *testing.T) {
	vars := map[string]string{EnvGitHubToken: "ghs-file", EnvGitHubSecret: "file-github-secret-of-16-bytes"}
	start := func(st state.Store) *platform.Platform {
		p, err := platform.New(platform.Config{
			Workspace:  t.TempDir(),
			Backends:   platform.Backends{State: st},
			Env:        func(name string) (string, bool) { v, ok := vars[name]; return v, ok },
			SecretRefs: map[string][]string{"acme": {EnvGitHubToken, EnvGitHubSecret}},
			GitHubAPI:  func(string) github.API { return github.NewFake() },
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = p.Close() })
		return p
	}
	on := spec.GitHub{
		Enabled: true, Owners: []string{"acme"},
		Token: &spec.Ref{Ref: "env:" + EnvGitHubToken}, WebhookSecret: &spec.Ref{Ref: "env:" + EnvGitHubSecret},
	}
	off := on
	off.Enabled = false
	apply := func(p *platform.Platform, g spec.GitHub) clients.Client {
		t.Helper()
		doc := spec.Platform{Metadata: spec.Metadata{Name: "acme"}, Spec: spec.PlatformSpec{Surfaces: spec.Surfaces{GitHub: &g}}}
		if err := applyPlatforms(t.Context(), p, spec.Config{Platforms: []spec.Platform{doc}}, slog.Default()); err != nil {
			t.Fatal(err)
		}
		c, err := p.Clients.Get("acme", "file-github")
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	ping := func(p *platform.Platform) int {
		t.Helper()
		body := `{"zen":"Keep it logically awesome.","repository":{"name":"repo","owner":{"login":"acme"}},"sender":{"login":"human","type":"User"}}`
		mac := hmac.New(sha256.New, []byte(vars[EnvGitHubSecret]))
		mac.Write([]byte(body))
		r := httptest.NewRequest(http.MethodPost, "/connectors/github", strings.NewReader(body))
		r.Header.Set(signature.HeaderGitHubSignature, "sha256="+hex.EncodeToString(mac.Sum(nil)))
		r.Header.Set("X-GitHub-Event", "ping")
		r.Header.Set("X-GitHub-Delivery", "ping-1")
		w := httptest.NewRecorder()
		p.IngressHandler().ServeHTTP(w, r)
		return w.Code
	}

	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		if c, p := apply(start(st), on), start(st); !c.Enabled || ping(p) != http.StatusOK {
			t.Fatalf("the first start kept %+v, which does not answer", c)
		}
		p := start(st)
		c := apply(p, off)
		if c.Enabled || c.Managed != clients.ManagedFile || c.Version != 2 {
			t.Fatalf("after the file turned GitHub off, the client is %+v", c)
		}
		if code := ping(p); code != http.StatusNotFound {
			t.Fatalf("a client the file turned off answered a signed hook with %d", code)
		}
		p = start(st)
		if again := apply(p, spec.GitHub{Enabled: false}); again.Enabled || again.Version != 2 {
			t.Fatalf("a second start with GitHub off made %+v", again)
		}
		if code := ping(p); code != http.StatusNotFound {
			t.Fatalf("after a restart, the client the file turned off answered %d", code)
		}
		p = start(st)
		if back := apply(p, on); !back.Enabled || back.Version != 3 || ping(p) != http.StatusOK {
			t.Fatalf("turned back on, the client is %+v", back)
		}
	})

	// With no client yet, turning it off makes none.
	none := start(state.NewMemory())
	doc := spec.Platform{Metadata: spec.Metadata{Name: "acme"}, Spec: spec.PlatformSpec{Surfaces: spec.Surfaces{GitHub: &off}}}
	if err := applyPlatforms(t.Context(), none, spec.Config{Platforms: []spec.Platform{doc}}, slog.Default()); err != nil {
		t.Fatal(err)
	}
	if _, err := none.Clients.Get("acme", "file-github"); !errors.Is(err, clients.ErrNotFound) {
		t.Fatalf("turning GitHub off made a client: %v", err)
	}

	// A console client of that id is not the file's to turn off.
	console := start(state.NewMemory())
	if _, err := console.CreateClient(t.Context(), "acme", "admin", platform.ClientInput{
		Client: clients.Client{ID: "file-github", Name: "GitHub", Surface: clients.GitHub, Enabled: true, Identity: clients.Identity{Owners: []string{"acme"}}},
		Token:  clients.SecretInput{Value: "ghs-console"}, SigningSecret: clients.SecretInput{Value: "console-secret-of-16-bytes"},
	}); err != nil {
		t.Fatal(err)
	}
	if kept := apply(console, off); !kept.Enabled || kept.Managed != "" || kept.Version != 1 {
		t.Fatalf("the file turned off the console's client: %+v", kept)
	}
}
