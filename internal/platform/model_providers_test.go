package platform

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	saigetypes "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/runtime"
	"github.com/urmzd/saige/flow/turn/native"

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
	// Offline never; a name nobody set up is no provider. A kind an
	// administrator may enable, such as anthropic, may be named before it is.
	for _, provider := range []string{"offline", "team"} {
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
	if i := slices.IndexFunc(choices, func(c ModelProviderChoice) bool { return c.Name == "team" }); i < 0 ||
		choices[i] != (ModelProviderChoice{Name: "team", Declared: true, Kind: KindAnthropic, Source: "workspace"}) {
		t.Fatalf("choices = %+v", choices)
	}
	for _, c := range choices {
		if c.Name != "team" && c.Declared {
			t.Fatalf("%s is offered as set up: %+v", c.Name, choices)
		}
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

// A provider an agent or a policy runs on cannot be removed, and
// removing one drops its kept key.
func TestDeleteModelProviderRefusesOneInUse(t *testing.T) {
	p, _ := configured(t, Config{})
	ctx := context.Background()
	if _, err := p.SetModelProvider(ctx, admin("acme"), "team", ModelProviderInput{Kind: KindAnthropic, Key: "sk"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Agents.Create(ctx, "acme", wire.AgentSpec{Name: "writer", Provider: "team", Model: "claude-sonnet-5-5", SystemPrompt: "Write."}, "ada"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.SetModelProviderEnabled(ctx, admin("acme"), "team", false); !errors.Is(err, ErrModelProviderInUse) || !strings.Contains(err.Error(), "@writer") {
		t.Fatalf("turn off while an agent uses it = %v", err)
	}
	if err := p.DeleteModelProvider(ctx, admin("acme"), "team"); !errors.Is(err, ErrModelProviderInUse) || !strings.Contains(err.Error(), "@writer") {
		t.Fatalf("delete while an agent uses it = %v", err)
	}
	if _, err := p.Agents.Revise(ctx, "acme", wire.AgentSpec{Name: "writer", Provider: "offline", Model: "offline", SystemPrompt: "Write."}, 1, "ada"); err != nil {
		t.Fatal(err)
	}
	if err := p.DeleteModelProvider(ctx, admin("acme"), "team"); err != nil {
		t.Fatal(err)
	}
	if _, ok := p.secrets.Get("acme", secretsOwner("team"), "key"); ok {
		t.Fatal("the removed provider's key is still kept")
	}
}

func TestTurningOffModelProviderKeepsTheKey(t *testing.T) {
	p, seen := configured(t, Config{
		Ext: Extensions{Providers: map[string]native.ProviderFunc{"declared": func(native.Spec) (saigetypes.Provider, error) {
			return nil, nil
		}}},
	})
	ctx := t.Context()
	actor := admin("acme")
	viewer := auth.Principal{Tenant: "acme", Subject: "vi", Roles: []auth.Role{auth.Viewer}}
	in := ModelProviderInput{Kind: KindOpenAI, BaseURL: "https://proxy.example/v1", Key: "sk-team"}
	m, err := p.SetModelProvider(ctx, actor, "team", in)
	if err != nil || m.Disabled {
		t.Fatalf("created provider = %+v, %v", m, err)
	}
	if _, err := p.SetModelProviderEnabled(ctx, viewer, "team", false); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("a viewer turned off a provider: %v", err)
	}
	if _, err := p.SetModelProviderEnabled(ctx, admin("globex"), "team", false); !errors.Is(err, wire.ErrNotFound) {
		t.Fatalf("another workspace turned off a provider: %v", err)
	}
	if _, err := p.SetModelProviderEnabled(ctx, actor, "declared", false); !errors.Is(err, ErrModelProvider) {
		t.Fatalf("the workspace turned off a deployment provider: %v", err)
	}
	for range 2 {
		off, err := p.SetModelProviderEnabled(ctx, actor, "team", false)
		if err != nil || !off.Disabled || !off.KeyStored || off.BaseURL != in.BaseURL {
			t.Fatalf("disabled provider = %+v, %v", off, err)
		}
	}
	ref := workspaceRef("acme", "team")
	if _, ok := p.resolveProvider(ref); ok {
		t.Fatal("a saved provider reference still resolves after turning it off")
	}
	if slices.ContainsFunc(p.ModelProviderChoices("acme"), func(c ModelProviderChoice) bool { return c.Name == "team" }) {
		t.Fatal("a disabled provider is still offered")
	}
	if slices.ContainsFunc(p.HealthFor(ctx, "acme").Components, func(c HealthComponent) bool { return c.ID == "provider:team" }) {
		t.Fatal("a disabled provider is still reported as operational")
	}
	if _, err := p.CheckModelProvider(ctx, actor, "team"); !errors.Is(err, ErrModelProvider) {
		t.Fatalf("a disabled provider can be checked: %v", err)
	}
	updated, err := p.SetModelProvider(ctx, actor, "team", ModelProviderInput{Kind: KindOpenAI, BaseURL: "https://new.example/v1"})
	if err != nil || !updated.Disabled || !updated.KeyStored {
		t.Fatalf("editing a disabled provider = %+v, %v", updated, err)
	}
	on, err := p.SetModelProviderEnabled(ctx, actor, "team", true)
	if err != nil || on.Disabled || on.BaseURL != updated.BaseURL {
		t.Fatalf("re-enabled provider = %+v, %v", on, err)
	}
	fn, ok := p.resolveProvider(ref)
	if !ok {
		t.Fatal("the provider did not resolve after turning it on")
	}
	if _, err := fn(native.Spec{Name: "writer"}); err != nil {
		t.Fatal(err)
	}
	if len(*seen) != 1 || (*seen)[0].key != in.Key {
		t.Fatal("turning the provider off and on lost its original key")
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

// The platform's agents ship with their own chain and exist from the start.
// A run is cut to the providers the workspace enabled, and with none it says
// which it needs; an agent may name a kind before it is enabled, never offline.
func TestBuiltinsShipWithAChainAndRunOnWhatIsEnabled(t *testing.T) {
	p, _ := configured(t, Config{ShipBuiltins: true})
	ctx := context.Background()
	if err := p.EnsureBuiltins(ctx, "acme"); err != nil {
		t.Fatal(err)
	}
	installForTest(t, p, "acme", "humagen/core")
	writer, err := p.Catalog().Get(ctx, "acme", BuiltinWriter, 0)
	if err != nil || writer.Spec.Provider != KindAnthropic || len(writer.Spec.Fallbacks) != 2 || writer.CreatedBy != "platform" {
		t.Fatalf("writer = %+v, %v", writer.Spec, err)
	}
	run := func() (native.Spec, error) {
		def := native.Revision{Spec: native.Spec{Name: writer.Spec.Name, Provider: writer.Spec.Provider, Model: writer.Spec.Model}}
		for _, f := range writer.Spec.Fallbacks {
			def.Spec.Fallbacks = append(def.Spec.Fallbacks, native.ModelRef{Provider: f.Provider, Model: f.Model})
		}
		got, err := p.resolveModel(runtime.Request{Tenant: "acme", Definition: def})
		return got.Definition.Spec, err
	}
	if _, err := run(); !errors.Is(err, ErrModelProvider) || !strings.Contains(err.Error(), "anthropic, openai, google") {
		t.Fatalf("a run with nothing enabled = %v", err)
	}
	if _, err := p.SetModelProvider(ctx, admin("acme"), "openai", ModelProviderInput{Kind: KindOpenAI, Key: "sk"}); err != nil {
		t.Fatal(err)
	}
	s, err := run()
	if err != nil || s.Provider != workspaceRef("acme", "openai") || s.Model != "gpt-5.1" || len(s.Fallbacks) != 0 {
		t.Fatalf("with openai enabled = %+v, %v", s, err)
	}
	if _, err := p.SetModelProvider(ctx, admin("acme"), "google", ModelProviderInput{Kind: KindGoogle, Vertex: &spec.Vertex{Project: "p", Location: "us-central1"}}); err != nil {
		t.Fatal(err)
	}
	s, err = run()
	if err != nil || s.Provider != workspaceRef("acme", "openai") || len(s.Fallbacks) != 1 || s.Fallbacks[0].Provider != workspaceRef("acme", "google") {
		t.Fatalf("with openai and google enabled = %+v, %v", s, err)
	}
	if _, err := p.SetModelProviderEnabled(ctx, admin("acme"), KindOpenAI, false); err != nil {
		t.Fatal(err)
	}
	s, err = run()
	if err != nil || s.Provider != workspaceRef("acme", "google") || len(s.Fallbacks) != 0 {
		t.Fatalf("with openai turned off = %+v, %v", s, err)
	}
	if _, err := p.SetModelProviderEnabled(ctx, admin("acme"), KindGoogle, false); err != nil {
		t.Fatal(err)
	}
	if _, err := run(); !errors.Is(err, ErrModelProvider) {
		t.Fatalf("a run with all providers turned off = %v", err)
	}
	// A kind may be named before it is enabled; offline never.
	for provider, ok := range map[string]bool{KindAnthropic: true, KindGoogle: true, wire.ProviderOffline: false} {
		spec := wire.AgentSpec{Name: "writer2", Provider: provider, Model: "m", SystemPrompt: "Write."}
		if err := spec.Normalize().ValidateWith(p.agentProviders("acme")); (err == nil) != ok {
			t.Errorf("an agent on %s: %v", provider, err)
		}
	}
}

// A preset the platform's own agents ship on can be removed, as it can be
// turned off: they answer on the next provider, and say what they need when
// none is left. A person's own agent on it still holds it.
func TestRemovingAPresetTheShippedAgentsRunOn(t *testing.T) {
	p, _ := configured(t, Config{ShipBuiltins: true})
	ctx := t.Context()
	for kind, key := range map[string]string{KindOpenAI: "sk-o", KindAnthropic: "sk-a"} {
		if _, err := p.SetModelProvider(ctx, admin("acme"), kind, ModelProviderInput{Kind: kind, Key: key}); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.EnsureBuiltins(ctx, "acme"); err != nil {
		t.Fatal(err)
	}
	installForTest(t, p, "acme", "humagen/core")
	writer, err := p.Catalog().Get(ctx, "acme", BuiltinWriter, 0)
	if err != nil || writer.CreatedBy != "platform" {
		t.Fatalf("writer = %+v, %v", writer.Spec, err)
	}
	run := func() (native.Spec, error) {
		def := native.Revision{Spec: native.Spec{Name: writer.Spec.Name, Provider: writer.Spec.Provider, Model: writer.Spec.Model}}
		for _, f := range writer.Spec.Fallbacks {
			def.Spec.Fallbacks = append(def.Spec.Fallbacks, native.ModelRef{Provider: f.Provider, Model: f.Model})
		}
		got, err := p.resolveModel(runtime.Request{Tenant: "acme", Definition: def})
		return got.Definition.Spec, err
	}
	if s, err := run(); err != nil || s.Provider != workspaceRef("acme", KindAnthropic) {
		t.Fatalf("with both set up = %+v, %v", s, err)
	}
	// A person's own agent on the preset holds it.
	if _, err := p.Agents.Create(ctx, "acme", wire.AgentSpec{Name: "mine", Provider: KindAnthropic, Model: "claude-sonnet-5-5", SystemPrompt: "Write."}, "ada"); err != nil {
		t.Fatal(err)
	}
	if err := p.DeleteModelProvider(ctx, admin("acme"), KindAnthropic); !errors.Is(err, ErrModelProviderInUse) || !strings.Contains(err.Error(), "@mine") || strings.Contains(err.Error(), "@"+BuiltinWriter) {
		t.Fatalf("remove under a person's own agent = %v", err)
	}
	if _, err := p.Agents.Revise(ctx, "acme", wire.AgentSpec{Name: "mine", Provider: KindOpenAI, Model: "gpt-5.1", SystemPrompt: "Write."}, 1, "ada"); err != nil {
		t.Fatal(err)
	}
	// Only the platform's agents name it now, so it goes, with its key.
	if err := p.DeleteModelProvider(ctx, admin("acme"), KindAnthropic); err != nil {
		t.Fatalf("remove a preset only shipped agents name = %v", err)
	}
	if _, ok := p.secrets.Get("acme", secretsOwner(KindAnthropic), "key"); ok {
		t.Fatal("the removed preset's key is still kept")
	}
	if s, err := run(); err != nil || s.Provider != workspaceRef("acme", KindOpenAI) || s.Model != "gpt-5.1" {
		t.Fatalf("a shipped agent after the removal = %+v, %v", s, err)
	}
	// The last one a shipped agent could use is held only by the person's agent.
	if err := p.DeleteModelProvider(ctx, admin("acme"), KindOpenAI); !errors.Is(err, ErrModelProviderInUse) || !strings.Contains(err.Error(), "@mine") {
		t.Fatalf("remove the last preset under a person's agent = %v", err)
	}
	if _, err := p.Agents.Revise(ctx, "acme", wire.AgentSpec{Name: "mine", Provider: "offline", Model: "offline", SystemPrompt: "Write."}, 2, "ada"); err != nil {
		t.Fatal(err)
	}
	if err := p.DeleteModelProvider(ctx, admin("acme"), KindOpenAI); err != nil {
		t.Fatalf("remove the last preset = %v", err)
	}
	if _, err := run(); !errors.Is(err, ErrModelProvider) || !strings.Contains(err.Error(), "anthropic, openai, google") {
		t.Fatalf("a shipped agent with nothing set up = %v", err)
	}
}

// A deployment may rename the entry agent. It still ships on the platform's
// chain, so it never holds a preset on; a person's own revision of it does.
func TestARenamedEntryAgentDoesNotHoldAPresetOn(t *testing.T) {
	p, _ := configured(t, Config{ShipBuiltins: true, EntryAgent: "acme-bot"})
	ctx := t.Context()
	for kind, key := range map[string]string{KindOpenAI: "sk-o", KindAnthropic: "sk-a"} {
		if _, err := p.SetModelProvider(ctx, admin("acme"), kind, ModelProviderInput{Kind: kind, Key: key}); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.EnsureBuiltins(ctx, "acme"); err != nil {
		t.Fatal(err)
	}
	entry, err := p.Catalog().Get(ctx, "acme", "acme-bot", 0)
	if err != nil || !p.isEntry(entry) || IsBuiltin(entry.Spec.Name) {
		t.Fatalf("the renamed entry agent = %+v, %v", entry, err)
	}
	names := func(kind string) bool {
		return entry.Spec.Provider == kind || slices.ContainsFunc(entry.Spec.Fallbacks, func(f wire.ModelRef) bool { return f.Provider == kind })
	}
	if !names(KindOpenAI) || !names(KindAnthropic) {
		t.Fatalf("the entry agent's chain = %s %+v", entry.Spec.Provider, entry.Spec.Fallbacks)
	}
	for _, enabled := range []bool{false, true} {
		if _, err := p.SetModelProviderEnabled(ctx, admin("acme"), KindOpenAI, enabled); err != nil {
			t.Fatalf("openai enabled=%v with a renamed entry agent: %v", enabled, err)
		}
	}
	if _, err := p.SetModelProviderEnabled(ctx, admin("acme"), KindAnthropic, false); err != nil {
		t.Fatalf("turning anthropic off with a renamed entry agent: %v", err)
	}
	// A person who revises it has made an assignment of their own.
	if _, err := p.Agents.Revise(ctx, "acme", entry.Spec, entry.Rev, "ada"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.SetModelProviderEnabled(ctx, admin("acme"), KindOpenAI, false); !errors.Is(err, ErrModelProviderInUse) || !strings.Contains(err.Error(), "@acme-bot") {
		t.Fatalf("turning openai off under a person's own revision = %v", err)
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
