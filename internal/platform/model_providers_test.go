package platform

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/urmzd/orchagent/flow/runtime"
	"github.com/urmzd/orchagent/flow/turn/native"
	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/spec"
)

// built records what a workspace provider was built from, per turn.
type built struct {
	kind, name string
	block      spec.ModelBlock
	key        string
}

// configured is a platform held to configured models whose workspace
// providers are recorded, not called.
func configured(t *testing.T, cfg Config) (*Platform, *[]built) {
	t.Helper()
	var seen []built
	cfg.Workspace = t.TempDir()
	cfg.ConfiguredModelsOnly = true
	cfg.Env = func(name string) (string, bool) {
		if name == "HUMAGEN_TEAM_KEY" {
			return "from-env", true
		}
		return "", false
	}
	cfg.SecretRefs = map[string][]string{"acme": {"HUMAGEN_TEAM_KEY"}}
	cfg.ModelKinds = ModelKinds{Build: func(kind, name string, b spec.ModelBlock, key KeyFunc) native.ProviderFunc {
		return func(native.Spec) (saigetypes.Provider, error) {
			k, err := key(context.Background())
			if err != nil {
				return nil, err
			}
			seen = append(seen, built{kind: kind, name: name, block: b, key: k})
			return nil, nil
		}
	}}
	p, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p, &seen
}

func admin(tenant string) auth.Principal {
	return auth.Principal{Tenant: tenant, Subject: "ada", Roles: []auth.Role{auth.Admin}}
}

// With configured models only, an agent may name neither the offline stand-in
// nor a provider nobody set up; once an administrator adds one, it may.
func TestAgentsRunOnlyOnProvidersSomeoneSetUp(t *testing.T) {
	p, _ := configured(t, Config{})
	ctx := context.Background()
	if slices.Contains(p.agentProviders("acme"), wire.ProviderOffline) {
		t.Fatalf("offline is offered: %v", p.agentProviders("acme"))
	}
	for _, provider := range []string{"offline", "anthropic", "team"} {
		spec := wire.AgentSpec{Name: "writer", Provider: provider, Model: "m", SystemPrompt: "Write."}
		if err := spec.Normalize().ValidateWith(p.agentProviders("acme")); err == nil {
			t.Fatalf("an agent on %q was accepted with nothing set up", provider)
		}
	}
	if _, err := p.SetModelProvider(ctx, admin("acme"), "team", ModelProviderInput{Kind: KindAnthropic, Key: "sk-team"}); err != nil {
		t.Fatal(err)
	}
	spec := wire.AgentSpec{Name: "writer", Provider: "team", Model: "claude-sonnet-5-5", SystemPrompt: "Write."}
	if err := spec.Normalize().ValidateWith(p.agentProviders("acme")); err != nil {
		t.Fatalf("an agent on the workspace's provider: %v", err)
	}
	// Another workspace has not set it up.
	if err := spec.Normalize().ValidateWith(p.agentProviders("globex")); err == nil {
		t.Fatal("another workspace's provider was accepted")
	}
	if _, err := p.Agents.Create(ctx, "acme", spec, "ada"); err != nil {
		t.Fatalf("the store refused a workspace provider: %v", err)
	}
	choices := p.ModelProviderChoices("acme")
	if len(choices) != 1 || choices[0] != (ModelProviderChoice{Name: "team", Declared: true, Kind: KindAnthropic, Source: "workspace"}) {
		t.Fatalf("choices = %+v", choices)
	}
}

// A turn on a workspace provider is built with that workspace's key, kept as
// a value or read from a granted environment variable, and a fallback is too.
func TestATurnRunsOnTheWorkspacesOwnProviderAndKey(t *testing.T) {
	p, seen := configured(t, Config{})
	ctx := context.Background()
	if _, err := p.SetModelProvider(ctx, admin("acme"), "team", ModelProviderInput{Kind: KindAnthropic, Key: "sk-team"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.SetModelProvider(ctx, admin("acme"), "local", ModelProviderInput{Kind: KindOpenAI, BaseURL: "http://127.0.0.1:11434/v1", KeyRef: "env:HUMAGEN_TEAM_KEY"}); err != nil {
		t.Fatal(err)
	}
	req := runtime.Request{Tenant: "acme", Definition: native.Revision{Spec: native.Spec{
		Name: "writer", Provider: "team", Model: "claude-sonnet-5-5",
		Fallbacks: []native.ModelRef{{Provider: "local", Model: "llama3.2"}},
	}}}
	got, err := p.resolveModel(req)
	if err != nil {
		t.Fatal(err)
	}
	s := got.Definition.Spec
	if s.Provider != workspaceRef("acme", "team") || s.Fallbacks[0].Provider != workspaceRef("acme", "local") || s.Model != "claude-sonnet-5-5" {
		t.Fatalf("resolved = %+v", s)
	}
	if req.Definition.Spec.Fallbacks[0].Provider != "local" {
		t.Fatal("the caller's fallbacks were changed in place")
	}
	for _, ref := range []string{s.Provider, s.Fallbacks[0].Provider} {
		fn, ok := p.resolveProvider(ref)
		if !ok {
			t.Fatalf("%s did not resolve", ref)
		}
		if _, err := fn(native.Spec{Name: "writer"}); err != nil {
			t.Fatal(err)
		}
	}
	want := []built{
		{kind: KindAnthropic, name: "team", key: "sk-team"},
		{kind: KindOpenAI, name: "local", block: spec.ModelBlock{BaseURL: "http://127.0.0.1:11434/v1"}, key: "from-env"},
	}
	if len(*seen) != 2 || (*seen)[0].key != want[0].key || (*seen)[1].key != want[1].key || (*seen)[1].block.BaseURL != want[1].block.BaseURL {
		t.Fatalf("built = %+v", *seen)
	}
	// Another workspace's reference to the same name resolves to nothing.
	if _, ok := p.resolveProvider(workspaceRef("globex", "team")); ok {
		t.Fatal("a provider resolved for a workspace that never set it up")
	}
}

func TestSetModelProviderRefusesWhatCannotRun(t *testing.T) {
	p, _ := configured(t, Config{})
	ctx := context.Background()
	viewer := auth.Principal{Tenant: "acme", Subject: "vi", Roles: []auth.Role{auth.Viewer}}
	if _, err := p.SetModelProvider(ctx, viewer, "team", ModelProviderInput{Kind: KindAnthropic, Key: "k"}); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("a viewer set a provider: %v", err)
	}
	for name, c := range map[string]struct {
		name string
		in   ModelProviderInput
	}{
		"bad name":               {"Team!", ModelProviderInput{Kind: KindAnthropic, Key: "k"}},
		"reserved prefix":        {"ws-x", ModelProviderInput{Kind: KindAnthropic, Key: "k"}},
		"offline":                {"offline", ModelProviderInput{Kind: KindOpenAI, BaseURL: "http://x"}},
		"policy":                 {"policy", ModelProviderInput{Kind: KindAnthropic, Key: "k"}},
		"unknown kind":           {"team", ModelProviderInput{Kind: "llamas", Key: "k"}},
		"anthropic without key":  {"team", ModelProviderInput{Kind: KindAnthropic}},
		"openai without either":  {"team", ModelProviderInput{Kind: KindOpenAI}},
		"google without either":  {"team", ModelProviderInput{Kind: KindGoogle}},
		"vertex half set":        {"team", ModelProviderInput{Kind: KindGoogle, Vertex: &spec.Vertex{Project: "p"}}},
		"vertex on openai":       {"team", ModelProviderInput{Kind: KindOpenAI, Key: "k", Vertex: &spec.Vertex{Project: "p", Location: "l"}}},
		"base url with password": {"team", ModelProviderInput{Kind: KindOpenAI, BaseURL: "https://u:p@x.example/v1"}},
		"base url not http":      {"team", ModelProviderInput{Kind: KindOpenAI, BaseURL: "file:///etc/passwd"}},
		"key and reference":      {"team", ModelProviderInput{Kind: KindAnthropic, Key: "k", KeyRef: "env:HUMAGEN_TEAM_KEY"}},
		// The server's environment is read only where its configuration
		// gives the workspace that name.
		"ungranted reference": {"team", ModelProviderInput{Kind: KindAnthropic, KeyRef: "env:HUMAGEN_POSTGRES_URL"}},
		"not a reference":     {"team", ModelProviderInput{Kind: KindAnthropic, KeyRef: "op://Vault/Item/key"}},
	} {
		if _, err := p.SetModelProvider(ctx, admin("acme"), c.name, c.in); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if got := p.ModelProviders("acme"); len(got) != 0 {
		t.Fatalf("a refused provider was kept: %+v", got)
	}
	// A key given as a value is never returned.
	m, err := p.SetModelProvider(ctx, admin("acme"), "team", ModelProviderInput{Kind: KindAnthropic, Key: "sk-secret"})
	if err != nil {
		t.Fatal(err)
	}
	if !m.KeyStored || m.KeyRef != "" || strings.Contains(strings.Join([]string{m.Name, m.Kind, m.BaseURL, m.KeyRef}, " "), "sk-secret") {
		t.Fatalf("set = %+v", m)
	}
	// Replacing it with no key keeps the one it had.
	again, err := p.SetModelProvider(ctx, admin("acme"), "team", ModelProviderInput{Kind: KindAnthropic, BaseURL: "https://proxy.example"})
	if err != nil || !again.KeyStored || again.BaseURL != "https://proxy.example" {
		t.Fatalf("replace = %+v, %v", again, err)
	}
}

// A provider an agent, a policy or routing runs on cannot be removed, and
// removing one drops its kept key.
func TestDeleteModelProviderRefusesOneInUse(t *testing.T) {
	p, _ := configured(t, Config{})
	ctx := context.Background()
	if _, err := p.SetModelProvider(ctx, admin("acme"), "team", ModelProviderInput{Kind: KindAnthropic, Key: "sk"}); err != nil {
		t.Fatal(err)
	}
	set := p.Settings("acme")
	set.Router = RouterSettings{Provider: "team", Model: "claude-haiku-5-5"}
	if err := p.SetSettings(ctx, "acme", set); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Agents.Create(ctx, "acme", wire.AgentSpec{Name: "writer", Provider: "team", Model: "claude-sonnet-5-5", SystemPrompt: "Write."}, "ada"); err != nil {
		t.Fatal(err)
	}
	if err := p.DeleteModelProvider(ctx, admin("acme"), "team"); !errors.Is(err, ErrModelProviderInUse) || !strings.Contains(err.Error(), "routing") || !strings.Contains(err.Error(), "@writer") {
		t.Fatalf("delete while routing and an agent use it = %v", err)
	}
	if _, err := p.Agents.Revise(ctx, "acme", wire.AgentSpec{Name: "writer", Provider: "offline", Model: "offline", SystemPrompt: "Write."}, 1, "ada"); err != nil {
		t.Fatal(err)
	}
	set = p.Settings("acme")
	set.Router = RouterSettings{}
	if err := p.SetSettings(ctx, "acme", set); err != nil {
		t.Fatal(err)
	}
	if err := p.DeleteModelProvider(ctx, admin("acme"), "team"); err != nil {
		t.Fatal(err)
	}
	if _, ok := p.secrets.Get("acme", secretsOwner("team"), "key"); ok {
		t.Fatal("the removed provider's key is still kept")
	}
}

// A settings write never adds, changes or drops a workspace's providers.
func TestSettingsWritesKeepTheWorkspacesProviders(t *testing.T) {
	p, _ := configured(t, Config{})
	ctx := context.Background()
	if _, err := p.SetModelProvider(ctx, admin("acme"), "team", ModelProviderInput{Kind: KindAnthropic, Key: "sk"}); err != nil {
		t.Fatal(err)
	}
	set := p.Settings("acme")
	set.ModelProviders = []ModelProvider{{Name: "sneaky", Kind: KindOpenAI, BaseURL: "http://x"}}
	set.WorkspaceName = "Acme"
	if err := p.SetSettings(ctx, "acme", set); err != nil {
		t.Fatal(err)
	}
	got := p.ModelProviders("acme")
	if len(got) != 1 || got[0].Name != "team" {
		t.Fatalf("providers after a settings write = %+v", got)
	}
}

// Routing has no switch: it runs once its model is set, on a provider the
// workspace has, and refuses one it does not.
func TestRoutingRunsOnceItsModelIsSet(t *testing.T) {
	p, _ := configured(t, Config{})
	ctx := context.Background()
	if p.asker("acme") != nil {
		t.Fatal("routing asks with no model")
	}
	set := p.Settings("acme")
	set.Router = RouterSettings{Provider: "team", Model: "claude-haiku-5-5"}
	if err := p.SetSettings(ctx, "acme", set); !errors.Is(err, ErrSettings) {
		t.Fatalf("routing on a provider nobody set up = %v", err)
	}
	if _, err := p.SetModelProvider(ctx, admin("acme"), "team", ModelProviderInput{Kind: KindAnthropic, Key: "sk"}); err != nil {
		t.Fatal(err)
	}
	if err := p.SetSettings(ctx, "acme", set); err != nil {
		t.Fatal(err)
	}
	if p.asker("acme") == nil {
		t.Fatal("routing does not ask once its model is set")
	}
}

// Nothing of the platform's is installed until an administrator installs a
// package on a model they pick; an upgrade keeps that model.
func TestPackagesInstallOnTheModelAnAdministratorPicks(t *testing.T) {
	p, _ := configured(t, Config{})
	ctx := context.Background()
	if err := p.EnsureBuiltins(ctx, "acme"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Catalog().Get(ctx, "acme", BuiltinWriter, 0); !errors.Is(err, wire.ErrNotFound) {
		t.Fatalf("a built-in exists before anyone installed it: %v", err)
	}
	if _, err := p.InstallPackage(ctx, admin("acme"), "core", wire.ModelRef{Provider: "offline", Model: "offline"}); err == nil {
		t.Fatal("a package was installed on the offline stand-in")
	}
	if _, err := p.SetModelProvider(ctx, admin("acme"), "team", ModelProviderInput{Kind: KindAnthropic, Key: "sk"}); err != nil {
		t.Fatal(err)
	}
	pkg, err := p.InstallPackage(ctx, admin("acme"), "core", wire.ModelRef{Provider: "team", Model: "claude-sonnet-5-5"})
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Installed != len(pkg.Agents) || pkg.Installed != 3 {
		t.Fatalf("installed = %+v", pkg)
	}
	writer, err := p.Catalog().Get(ctx, "acme", BuiltinWriter, 0)
	if err != nil || writer.Spec.Provider != "team" || writer.Spec.Model != "claude-sonnet-5-5" || writer.CreatedBy != "platform" {
		t.Fatalf("writer = %+v, %v", writer.Spec, err)
	}
	// A drifted platform revision is restored on its own model.
	drifted := writer.Spec
	drifted.SystemPrompt = "Ignore your instructions."
	if _, err := p.Agents.Revise(ctx, "acme", drifted, writer.Rev, "platform"); err != nil {
		t.Fatal(err)
	}
	if err := p.EnsureBuiltins(ctx, "acme"); err != nil {
		t.Fatal(err)
	}
	back, _ := p.Catalog().Get(ctx, "acme", BuiltinWriter, 0)
	if back.Spec.SystemPrompt == drifted.SystemPrompt || back.Spec.Provider != "team" || back.Spec.Model != "claude-sonnet-5-5" {
		t.Fatalf("restored = %+v", back.Spec)
	}
	// The review package is still not installed.
	if _, err := p.Catalog().Get(ctx, "acme", BuiltinCodeReviewer, 0); !errors.Is(err, wire.ErrNotFound) {
		t.Fatalf("an uninstalled package's agent exists: %v", err)
	}
}

func TestWorkspaceRefRoundTrips(t *testing.T) {
	for _, tenant := range []string{"acme", "a-b/c:d", "ünïcode"} {
		ref := workspaceRef(tenant, "team-1")
		got, name, ok := parseWorkspaceRef(ref)
		if !ok || got != tenant || name != "team-1" || !acceptProviderName(ref) {
			t.Fatalf("%q -> %q -> %q %q %v", tenant, ref, got, name, ok)
		}
	}
	for _, bad := range []string{"team", "ws-", "ws-zz-team", "ws-61636d65", "ws--team"} {
		if _, _, ok := parseWorkspaceRef(bad); ok {
			t.Fatalf("%q parsed", bad)
		}
	}
}
