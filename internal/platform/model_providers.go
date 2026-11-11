package platform

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/urmzd/saige/flow/turn/native"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/spec"
)

// A workspace's model providers are the ones its administrators set up while
// the server runs, beside any the deployment declares in its spec. There is
// no built-in model and no stand-in: an agent runs only on a provider someone
// set up, so a workspace with none runs no agent until an administrator adds
// one.
//
// An agent names a workspace provider by its name, as it names a declared
// one. The agent store admits any well-formed name (acceptProviderName),
// because providers come and go per workspace while the store is shared; the
// platform holds every write to the workspace's own (agentProviders). When a
// turn runs, resolveModel renames the provider to a reference that carries
// the workspace (workspaceRef), and the runtime asks resolveProvider for it.

// MaxModelProviders bounds how many model providers a workspace sets up.
const MaxModelProviders = 16

// MaxModelKey bounds a key kept as a value.
const MaxModelKey = 4096

// Errors.
var (
	// ErrModelProvider is a model provider that is malformed or missing.
	ErrModelProvider = errors.New("platform: model provider")
	// ErrModelProviderInUse is a provider an agent, a policy or routing
	// still runs on.
	ErrModelProviderInUse = errors.New("platform: model provider in use")
)

// The kinds of model provider an administrator may set up.
const (
	KindAnthropic = "anthropic"
	KindGoogle    = "google"
	KindOpenAI    = "openai"
)

// ModelProviderKinds lists the kinds an administrator may set up, sorted.
func ModelProviderKinds() []string { return []string{KindAnthropic, KindGoogle, KindOpenAI} }

// ModelProvider is a model provider an administrator set up for a workspace.
// Its key is kept apart from it and never returned: as a reference to an
// environment variable the server's configuration gives the workspace
// (Config.SecretRefs), or as a value in Secrets, as a client's secret is.
type ModelProvider struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	// BaseURL sends an anthropic or openai provider's calls to another
	// address: a proxy, or a server that speaks the OpenAI API, such as
	// Ollama or vLLM.
	BaseURL string `json:"base_url,omitempty"`
	// Vertex sends a google provider's calls to Vertex AI. With no key it
	// signs in as the server does (Application Default Credentials).
	Vertex *spec.Vertex `json:"vertex,omitempty"`
	// KeyRef is the key's reference, "env:NAME", when it is one.
	KeyRef string `json:"key_ref,omitempty"`
	// KeyStored says a value is kept for it in Secrets.
	KeyStored bool      `json:"key_stored,omitempty"`
	AddedBy   string    `json:"added_by,omitempty"`
	AddedAt   time.Time `json:"added_at,omitzero"`
}

// ModelProviderInput is a provider as an administrator writes it. Key is a
// value to keep; KeyRef a reference. With neither, a provider that is
// replaced keeps the key it had.
type ModelProviderInput struct {
	Kind    string       `json:"kind"`
	BaseURL string       `json:"base_url,omitempty"`
	Vertex  *spec.Vertex `json:"vertex,omitempty"`
	KeyRef  string       `json:"key_ref,omitempty"`
	Key     string       `json:"key,omitempty"`
}

// KeyFunc reads a provider's key when it is used, so a changed key applies
// at the next call. It returns "" for a provider that has none.
type KeyFunc func(ctx context.Context) (string, error)

// ModelKinds builds a workspace's providers from what an administrator set
// up: Build for a turn, Probe for a check that spends no tokens. The server
// supplies both; with Build nil no workspace provider can run.
type ModelKinds struct {
	Build func(kind, name string, b spec.ModelBlock, key KeyFunc) native.ProviderFunc
	Probe func(kind string, b spec.ModelBlock, key KeyFunc) Probe
}

// validProviderName is how a provider is named: as a policy is.
var validProviderName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// workspacePrefix starts the name a turn's runtime knows a workspace
// provider by. No provider may be named with it.
const workspacePrefix = "ws-"

// workspaceRef is the name a turn's runtime knows tenant's provider by: the
// tenant in hex, which holds no dash, then the provider's own name.
func workspaceRef(tenant, name string) string {
	return workspacePrefix + hex.EncodeToString([]byte(tenant)) + "-" + name
}

// parseWorkspaceRef reads a workspaceRef back.
func parseWorkspaceRef(ref string) (tenant, name string, ok bool) {
	rest, ok := strings.CutPrefix(ref, workspacePrefix)
	if !ok {
		return "", "", false
	}
	h, name, ok := strings.Cut(rest, "-")
	if !ok || name == "" {
		return "", "", false
	}
	raw, err := hex.DecodeString(h)
	if err != nil || len(raw) == 0 {
		return "", "", false
	}
	return string(raw), name, true
}

// acceptProviderName is what the agent store admits beyond the providers it
// was opened with: any well-formed provider name, and a workspaceRef. The
// platform holds each write to the workspace's own providers first.
func acceptProviderName(name string) bool {
	if _, _, ok := parseWorkspaceRef(name); ok {
		return true
	}
	return validProviderName.MatchString(name)
}

// ModelProviders lists a workspace's providers, in the order they were added.
func (p *Platform) ModelProviders(tenant string) []ModelProvider {
	return slices.Clone(p.Settings(tenant).ModelProviders)
}

// workspaceProvider finds one of a workspace's providers by name. A name the
// deployment declares is the deployment's, never a workspace's.
func (p *Platform) workspaceProvider(tenant, name string) (ModelProvider, bool) {
	if p.cfg.Ext.Providers[name] != nil {
		return ModelProvider{}, false
	}
	list := p.Settings(tenant).ModelProviders
	i := slices.IndexFunc(list, func(m ModelProvider) bool { return m.Name == name })
	if i < 0 {
		return ModelProvider{}, false
	}
	return list[i], true
}

// runName is the name a turn's runtime knows a provider an agent names by:
// a declared one by its own name, a workspace's by its workspaceRef.
func (p *Platform) runName(tenant, name string) string {
	if _, ok := p.workspaceProvider(tenant, name); ok {
		return workspaceRef(tenant, name)
	}
	return name
}

// resolveProvider builds the provider a workspaceRef names, when a turn runs.
// It is the runtime's ProviderResolver.
func (p *Platform) resolveProvider(ref string) (native.ProviderFunc, bool) {
	tenant, name, ok := parseWorkspaceRef(ref)
	if !ok || p.cfg.ModelKinds.Build == nil {
		return nil, false
	}
	m, ok := p.workspaceProvider(tenant, name)
	if !ok {
		return nil, false
	}
	return p.cfg.ModelKinds.Build(m.Kind, m.Name, m.block(), p.modelKey(tenant, m)), true
}

// nativeOptions are the options every native turn runner is built with: the
// providers the deployment declares, by name, and the resolver that builds a
// workspace's own when a turn names its workspaceRef. Humagen builds its own
// turn runtime, so the resolver is passed here as well as to flow.
func (p *Platform) nativeOptions() []native.Option {
	return append(p.cfg.Ext.agentOptions(), native.WithProviderResolver(p.resolveProvider))
}

// block is the provider as the server's model kinds read it. The key is
// passed apart, as a KeyFunc.
func (m ModelProvider) block() spec.ModelBlock {
	b := spec.ModelBlock{BaseURL: m.BaseURL}
	if m.Vertex != nil {
		v := *m.Vertex
		b.Vertex = &v
	}
	return b
}

// secretsOwner is who a provider's key is kept under in Secrets. A client's
// id holds no slash and a person's owner starts "person/", so it is no one
// else's.
func secretsOwner(name string) string { return "model-provider/" + name }

// modelKey reads a provider's key: its stored value, or the environment
// variable its reference names, as the server's configuration lets this
// workspace read it.
func (p *Platform) modelKey(tenant string, m ModelProvider) KeyFunc {
	return func(context.Context) (string, error) {
		switch {
		case m.KeyStored:
			if p.secrets == nil {
				return "", fmt.Errorf("%w: %q: no secret store", ErrModelProvider, m.Name)
			}
			v, ok := p.secrets.Get(tenant, secretsOwner(m.Name), "key")
			if !ok || v == "" {
				return "", fmt.Errorf("%w: %q: its key is no longer kept; set it again", ErrModelProvider, m.Name)
			}
			return v, nil
		case m.KeyRef != "":
			v, ok := p.envSecret(tenant, m.KeyRef)
			if !ok {
				return "", fmt.Errorf("%w: %q: %w", ErrModelProvider, m.Name, errSecretRef)
			}
			return v, nil
		}
		return "", nil
	}
}

// SetModelProvider adds a provider to who's workspace, or replaces the one
// of that name. Only someone who may manage the workspace's clients may: a
// provider holds a key as a client does.
func (p *Platform) SetModelProvider(ctx context.Context, who auth.Principal, name string, in ModelProviderInput) (ModelProvider, error) {
	if !who.Can(auth.ClientsWrite) {
		return ModelProvider{}, auth.ErrForbidden
	}
	tenant := who.Tenant
	name = strings.TrimSpace(name)
	in.Kind, in.BaseURL = strings.TrimSpace(in.Kind), strings.TrimSpace(in.BaseURL)
	in.KeyRef, in.Key = strings.TrimSpace(in.KeyRef), strings.TrimSpace(in.Key)
	if err := p.checkProviderName(name); err != nil {
		return ModelProvider{}, err
	}
	had, replacing := p.workspaceProvider(tenant, name)
	next := ModelProvider{Name: name, Kind: in.Kind, BaseURL: in.BaseURL, Vertex: in.Vertex, AddedBy: who.Subject, AddedAt: p.now()}
	if replacing {
		next.AddedBy, next.AddedAt = had.AddedBy, had.AddedAt
		if had.Kind == in.Kind && in.Key == "" && in.KeyRef == "" {
			next.KeyRef, next.KeyStored = had.KeyRef, had.KeyStored
		}
	}
	switch {
	case in.Key != "" && in.KeyRef != "":
		return ModelProvider{}, fmt.Errorf("%w: give a key or a reference to one, not both", ErrModelProvider)
	case len(in.Key) > MaxModelKey:
		return ModelProvider{}, fmt.Errorf("%w: a key is at most %d characters", ErrModelProvider, MaxModelKey)
	case in.KeyRef != "":
		if _, ok := p.envSecret(tenant, in.KeyRef); !ok {
			return ModelProvider{}, fmt.Errorf("%w: %w", ErrModelProvider, errSecretRef)
		}
		next.KeyRef, next.KeyStored = in.KeyRef, false
	case in.Key != "":
		next.KeyRef, next.KeyStored = "", true
	}
	if err := checkProviderShape(next); err != nil {
		return ModelProvider{}, err
	}
	if !replacing && len(p.Settings(tenant).ModelProviders) >= MaxModelProviders {
		return ModelProvider{}, fmt.Errorf("%w: a workspace sets up at most %d model providers", ErrModelProvider, MaxModelProviders)
	}
	if in.Key != "" {
		if p.secrets == nil {
			return ModelProvider{}, fmt.Errorf("%w: this server keeps no secrets; give a reference instead", ErrModelProvider)
		}
		if err := p.secrets.Put(tenant, secretsOwner(name), "key", in.Key); err != nil {
			return ModelProvider{}, err
		}
	}
	err := p.settings.update(tenant, func(v *storedSettings) error {
		list := v.ModelProviders
		if i := slices.IndexFunc(list, func(m ModelProvider) bool { return m.Name == name }); i >= 0 {
			list[i] = next
		} else {
			if len(list) >= MaxModelProviders {
				return fmt.Errorf("%w: a workspace sets up at most %d model providers", ErrModelProvider, MaxModelProviders)
			}
			v.ModelProviders = append(list, next)
		}
		return nil
	})
	if err != nil {
		return ModelProvider{}, err
	}
	// A key that moved from a value to a reference leaves no value behind.
	if replacing && had.KeyStored && !next.KeyStored && p.secrets != nil {
		_ = p.secrets.Drop(tenant, secretsOwner(name))
	}
	p.log.Info("platform: model provider set", "tenant", tenant, "provider", name, "kind", next.Kind, "by", who.Subject, "replaced", replacing)
	return next, nil
}

// checkProviderName refuses a name a workspace provider cannot have.
func (p *Platform) checkProviderName(name string) error {
	switch {
	case !validProviderName.MatchString(name):
		return fmt.Errorf("%w: a name is lowercase letters, digits and dashes, at most 63", ErrModelProvider)
	case strings.HasPrefix(name, workspacePrefix):
		return fmt.Errorf("%w: a name may not start %q", ErrModelProvider, workspacePrefix)
	case name == PolicyProvider || name == wire.ProviderOffline:
		return fmt.Errorf("%w: %q is reserved", ErrModelProvider, name)
	case p.cfg.Ext.Providers[name] != nil:
		return fmt.Errorf("%w: the deployment already declares %q", ErrModelProvider, name)
	}
	return nil
}

// checkProviderShape holds a provider to what its kind needs.
func checkProviderShape(m ModelProvider) error {
	key := m.KeyRef != "" || m.KeyStored
	if m.BaseURL != "" {
		u, err := url.Parse(m.BaseURL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
			return fmt.Errorf("%w: a base URL is http or https, with a host and no credentials", ErrModelProvider)
		}
	}
	switch m.Kind {
	case KindAnthropic:
		if !key {
			return fmt.Errorf("%w: an anthropic provider needs a key", ErrModelProvider)
		}
	case KindOpenAI:
		// A server on the operator's own network may need no key; OpenAI does.
		if !key && m.BaseURL == "" {
			return fmt.Errorf("%w: an openai provider needs a key, or a base URL for a server that takes none", ErrModelProvider)
		}
	case KindGoogle:
		if m.BaseURL != "" {
			return fmt.Errorf("%w: a google provider takes no base URL", ErrModelProvider)
		}
		if m.Vertex != nil && (strings.TrimSpace(m.Vertex.Project) == "" || strings.TrimSpace(m.Vertex.Location) == "") {
			return fmt.Errorf("%w: Vertex AI needs a project and a location", ErrModelProvider)
		}
		if !key && m.Vertex == nil {
			return fmt.Errorf("%w: a google provider needs a key, or Vertex AI", ErrModelProvider)
		}
	default:
		return fmt.Errorf("%w: the kind is one of %s", ErrModelProvider, strings.Join(ModelProviderKinds(), ", "))
	}
	if m.Kind != KindGoogle && m.Vertex != nil {
		return fmt.Errorf("%w: only a google provider runs on Vertex AI", ErrModelProvider)
	}
	return nil
}

// DeleteModelProvider removes one of who's workspace's providers and its
// key. It is refused while an agent, a model policy or routing runs on it,
// naming each, so nothing is left pointing at a provider that is gone.
func (p *Platform) DeleteModelProvider(ctx context.Context, who auth.Principal, name string) error {
	if !who.Can(auth.ClientsWrite) {
		return auth.ErrForbidden
	}
	tenant := who.Tenant
	if _, ok := p.workspaceProvider(tenant, name); !ok {
		return fmt.Errorf("%w: the workspace has no provider named %q", ErrModelProvider, name)
	}
	if users := p.providerUsers(ctx, tenant, name); len(users) > 0 {
		return fmt.Errorf("%w: %q is used by %s; move them to another provider first", ErrModelProviderInUse, name, strings.Join(users, ", "))
	}
	err := p.settings.update(tenant, func(v *storedSettings) error {
		v.ModelProviders = slices.DeleteFunc(v.ModelProviders, func(m ModelProvider) bool { return m.Name == name })
		return nil
	})
	if err != nil {
		return err
	}
	if p.secrets != nil {
		_ = p.secrets.Drop(tenant, secretsOwner(name))
	}
	p.log.Info("platform: model provider removed", "tenant", tenant, "provider", name, "by", who.Subject)
	return nil
}

// providerUsers names what runs on a provider: each agent whose latest
// revision names it, first or as a fallback, each model policy, and routing.
func (p *Platform) providerUsers(ctx context.Context, tenant, name string) []string {
	var users []string
	names := func(spec wire.AgentSpec) bool {
		if spec.Provider == name {
			return true
		}
		return slices.ContainsFunc(spec.Fallbacks, func(f wire.ModelRef) bool { return f.Provider == name })
	}
	page := wire.Page{}
	for range 100 {
		all, next, err := p.Catalog().List(ctx, tenant, page)
		if err != nil {
			break
		}
		for _, rev := range all {
			if names(rev.Spec) {
				users = append(users, "@"+rev.Spec.Name)
			}
		}
		if next == "" {
			break
		}
		page.Token = next
	}
	cfg := p.Settings(tenant)
	for _, m := range cfg.ModelPolicies {
		if slices.ContainsFunc(m.Models, func(r wire.ModelRef) bool { return r.Provider == name }) {
			users = append(users, "the model policy "+m.Name)
		}
	}
	return users
}

// CheckModelProvider asks a provider, with its key, whether it answers. It
// spends no tokens: it lists the provider's models, as Health does for the
// deployment's. A kind with no check, such as Vertex AI, is said to have
// none.
func (p *Platform) CheckModelProvider(ctx context.Context, who auth.Principal, name string) (HealthComponent, error) {
	if !who.Can(auth.ClientsWrite) {
		return HealthComponent{}, auth.ErrForbidden
	}
	m, ok := p.workspaceProvider(who.Tenant, name)
	if !ok {
		return HealthComponent{}, fmt.Errorf("%w: the workspace has no provider named %q", ErrModelProvider, name)
	}
	var probe Probe
	if p.cfg.ModelKinds.Probe != nil {
		probe = p.cfg.ModelKinds.Probe(m.Kind, m.block(), p.modelKey(who.Tenant, m))
	}
	c := probeComponent(ctx, "provider:"+name, name, probe)
	if probe == nil {
		c.Detail = "Set up; this kind has no check"
	}
	c.History = []HealthSample{{At: p.now(), Status: c.Status}}
	return c, nil
}
