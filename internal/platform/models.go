package platform

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/urmzd/saige/agent/provider/catalog"
	saigetypes "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/runtime"
	"github.com/urmzd/saige/flow/turn"
	"github.com/urmzd/saige/flow/turn/native"

	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/internal/wire"
)

// PolicyProvider is the provider an agent names to run on one of its
// workspace's model policies. Its model is the policy's name. The platform
// puts the policy's models in its place just before each call, so the
// workspace decides, in one place, which models every such agent runs on and
// when a call moves to the next one.
const PolicyProvider = "policy"

// MaxModelPolicies bounds how many model policies a workspace keeps.
const MaxModelPolicies = 32

// Errors.
var (
	// ErrModelPolicy is a model policy that is malformed or missing.
	ErrModelPolicy = errors.New("platform: model policy")
	// ErrModelPolicyInUse is a policy an agent still runs on.
	ErrModelPolicyInUse = errors.New("platform: model policy in use")
)

var validPolicyName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// ModelPolicy is a named, ordered list of models and the errors that move a
// call from one to the next. The first model answers; the rest are fallbacks.
type ModelPolicy struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Models      []wire.ModelRef `json:"models"`
	// FallbackOn names the classes of error that move a call on (see
	// wire.FallbackConditions). Empty falls through on any error another
	// model could get past.
	FallbackOn []string `json:"fallback_on,omitempty"`
}

// providers is what an agent spec may name on this platform: fiber's built-in
// providers, and a workspace model policy.
func providers() []string { return append(wire.KnownProviders(), PolicyProvider) }

// spec is the policy written as the agent fields fiber runs, so fiber's own
// rules check it.
func (m ModelPolicy) spec(agent string) wire.AgentSpec {
	s := wire.AgentSpec{Name: agent, SystemPrompt: "-"}
	if len(m.Models) > 0 {
		s.Provider, s.Model = m.Models[0].Provider, m.Models[0].Model
		if len(m.Models) > 1 {
			s.Fallbacks = slices.Clone(m.Models[1:])
		}
	}
	if len(m.FallbackOn) > 0 {
		s.FallbackOn = slices.Clone(m.FallbackOn)
	}
	return s
}

// checkModelPolicies checks each policy as fiber checks an agent, against the
// providers this deployment has: fiber's built-in ones and every model the
// spec names, such as a second OpenAI-compatible server.
func checkModelPolicies(list []ModelPolicy, known ...string) error {
	return checkPoliciesAgainst(list, append(wire.KnownProviders(), known...), nil)
}

// checkPoliciesAgainst checks each policy against exactly the providers
// named, with none of fiber's built-ins added. A policy kept exactly as it
// was stored (kept) is not held to them again: it was checked when it was
// written, a provider it names may have left the deployment since, and a run
// on it goes to the providers still enabled (resolveModel). So a settings
// write, such as the one every start makes, never fails on stored data.
func checkPoliciesAgainst(list []ModelPolicy, providers []string, kept []ModelPolicy) error {
	if len(list) > MaxModelPolicies {
		return fmt.Errorf("%w: a workspace keeps at most %d model policies", ErrModelPolicy, MaxModelPolicies)
	}
	seen := map[string]bool{}
	for i, m := range list {
		switch {
		case !validPolicyName.MatchString(m.Name):
			return fmt.Errorf("%w: policy %d: a name is lowercase letters, digits and dashes", ErrModelPolicy, i+1)
		case seen[m.Name]:
			return fmt.Errorf("%w: two policies are named %q", ErrModelPolicy, m.Name)
		case len(m.Description) > 300 || strings.ContainsAny(m.Description, "<>"):
			return fmt.Errorf("%w: %q: a description is plain text, at most 300 characters", ErrModelPolicy, m.Name)
		case len(m.Models) == 0:
			return fmt.Errorf("%w: %q names no model", ErrModelPolicy, m.Name)
		}
		for _, ref := range m.Models {
			if ref.Provider == PolicyProvider {
				return fmt.Errorf("%w: %q: a policy names models, not another policy", ErrModelPolicy, m.Name)
			}
		}
		seen[m.Name] = true
		if old, ok := policyIn(kept, m.Name); ok && samePolicy(old, m) {
			continue
		}
		// A policy is checked exactly as fiber checks the agent fields it becomes.
		if err := m.spec("policy-" + m.Name).ValidateWith(providers); err != nil {
			return fmt.Errorf("%w: %q: %v", ErrModelPolicy, m.Name, err)
		}
	}
	return nil
}

// samePolicy reports whether two policies say the same thing.
func samePolicy(a, b ModelPolicy) bool {
	return a.Name == b.Name && a.Description == b.Description && slices.Equal(a.Models, b.Models) && slices.Equal(a.FallbackOn, b.FallbackOn)
}

// policyIn finds a policy by name.
func policyIn(list []ModelPolicy, name string) (ModelPolicy, bool) {
	i := slices.IndexFunc(list, func(m ModelPolicy) bool { return m.Name == name })
	if i < 0 {
		return ModelPolicy{}, false
	}
	return list[i], true
}

// CheckAgentModel refuses a spec that names a model policy the workspace does
// not have, or that names its own fallbacks on top of one. It runs before a
// spec is saved, so an agent is never left pointing at nothing.
func (p *Platform) CheckAgentModel(tenant string, spec wire.AgentSpec) error {
	if spec.Provider != PolicyProvider {
		return nil
	}
	if len(spec.Fallbacks) > 0 || len(spec.FallbackOn) > 0 {
		return fmt.Errorf("%w: an agent on a model policy takes its fallbacks from the policy", ErrModelPolicy)
	}
	if _, ok := policyIn(p.Settings(tenant).ModelPolicies, spec.Model); !ok {
		return fmt.Errorf("%w: the workspace has no model policy named %q", ErrModelPolicy, spec.Model)
	}
	return nil
}

// policyUsers lists the agents whose latest revision runs on a policy.
func (p *Platform) policyUsers(ctx context.Context, tenant, name string) []string {
	var users []string
	page := wire.Page{}
	// Agents are listed a page at a time, up to a bound.
	for range 100 {
		all, next, err := p.Catalog().List(ctx, tenant, page)
		if err != nil {
			return users
		}
		for _, rev := range all {
			if rev.Spec.Provider == PolicyProvider && rev.Spec.Model == name {
				users = append(users, rev.Spec.Name)
			}
		}
		if next == "" {
			return users
		}
		page.Token = next
	}
	return users
}

// resolveModel puts a workspace policy's models in place of the policy an
// agent names, for one call. The pinned revision is not changed: the policy
// is read as it stands when the call starts, which is what lets a workspace
// move every agent off a failing provider at once.
//
// It then names each workspace provider the call runs on by its
// workspaceRef, which carries the workspace, so the runtime builds it with
// that workspace's key (resolveProvider). A declared provider keeps its name.
func (p *Platform) resolveModel(req runtime.Request) (runtime.Request, error) {
	spec := req.Definition.Spec
	// A provider stored under a name since renamed runs as the new one.
	has := func(name string) bool { return p.hasProvider(req.Tenant, name) }
	spec.Provider = p.readProvider(req.Tenant, spec.Provider, has)
	if len(spec.Fallbacks) > 0 {
		spec.Fallbacks = slices.Clone(spec.Fallbacks)
		for i := range spec.Fallbacks {
			spec.Fallbacks[i].Provider = p.readProvider(req.Tenant, spec.Fallbacks[i].Provider, has)
		}
	}
	primary := native.ModelRef{Provider: spec.Provider, Model: spec.Model}
	if spec.Provider == PolicyProvider {
		m, ok := policyIn(p.Settings(req.Tenant).ModelPolicies, spec.Model)
		if !ok {
			return req, fmt.Errorf("%w: agent %q runs on the model policy %q, which the workspace no longer has", ErrModelPolicy, spec.Name, spec.Model)
		}
		resolved := m.spec(spec.Name)
		spec.Provider, spec.Model, spec.FallbackOn = resolved.Provider, resolved.Model, resolved.FallbackOn
		spec.Fallbacks = make([]native.ModelRef, 0, len(resolved.Fallbacks))
		for _, f := range resolved.Fallbacks {
			spec.Fallbacks = append(spec.Fallbacks, native.ModelRef{Provider: f.Provider, Model: f.Model})
		}
		if len(spec.Fallbacks) == 0 {
			spec.Fallbacks = nil
		}
	}
	if p.cfg.ConfiguredModelsOnly {
		// A model whose provider the workspace has not enabled is passed
		// over, so a chain runs on what is set up; with none, the run says
		// what it needs rather than reach for a key nobody chose.
		chain := append([]native.ModelRef{{Provider: spec.Provider, Model: spec.Model}}, spec.Fallbacks...)
		var named []string
		kept := slices.DeleteFunc(slices.Clone(chain), func(m native.ModelRef) bool {
			if !slices.Contains(named, m.Provider) {
				named = append(named, m.Provider)
			}
			return !p.enabled(req.Tenant, m.Provider)
		})
		if len(kept) == 0 {
			return req, fmt.Errorf("%w: @%s needs a model provider its models name (%s); an administrator enables one in Settings, under Model providers", ErrModelProvider, spec.Name, strings.Join(named, ", "))
		}
		spec.Provider, spec.Model = kept[0].Provider, kept[0].Model
		spec.Fallbacks = kept[1:]
		if len(spec.Fallbacks) == 0 {
			spec.Fallbacks, spec.FallbackOn = nil, nil
		}
	}
	// The agent's effort is for its own primary: a run moved to another
	// model, or on one that takes no such effort, runs at the model's
	// default rather than fail before its first call.
	if spec.Effort != "" && (spec.Provider != primary.Provider || spec.Model != primary.Model || !p.takesEffort(req.Tenant, spec.Provider, spec.Model, spec.Effort)) {
		spec.Effort = ""
	}
	spec.Provider = p.runName(req.Tenant, spec.Provider)
	if len(spec.Fallbacks) > 0 {
		spec.Fallbacks = slices.Clone(spec.Fallbacks)
		for i := range spec.Fallbacks {
			spec.Fallbacks[i].Provider = p.runName(req.Tenant, spec.Fallbacks[i].Provider)
		}
	}
	req.Definition.Spec = spec
	return req, nil
}

// enabled reports whether a provider can run in tenant's workspace: the
// deployment declares it, or the workspace's administrators set it up.
func (p *Platform) enabled(tenant, name string) bool {
	if p.cfg.Ext.Providers[name] != nil {
		return true
	}
	_, ok := p.workspaceProvider(tenant, name)
	return ok
}

// ModelProviderChoice describes configuration names, never credential values.
// Declared records that someone set it up: the deployment, or the
// workspace's administrators (Source). Actual connectivity requires a run.
type ModelProviderChoice struct {
	Name     string `json:"name"`
	Declared bool   `json:"declared"`
	// Kind is anthropic, google or openai, where it is known.
	Kind string `json:"kind,omitempty"`
	// Source is "deployment" or "workspace" for a declared provider.
	Source string `json:"source,omitempty"`
}

func (p *Platform) ModelProviderChoices(tenant string) []ModelProviderChoice {
	names := p.agentProviders(tenant)
	slices.Sort(names)
	names = slices.Compact(names)
	out := []ModelProviderChoice{}
	for _, name := range names {
		if name == PolicyProvider {
			continue
		}
		c := ModelProviderChoice{Name: name}
		if p.cfg.Ext.Providers[name] != nil {
			c.Declared, c.Source, c.Kind = true, "deployment", p.cfg.DeclaredKinds[name]
		} else if m, ok := p.workspaceProvider(tenant, name); ok {
			c.Declared, c.Source, c.Kind = true, "workspace", m.Kind
		}
		out = append(out, c)
		if len(out) >= 100 {
			break
		}
	}
	return out
}

// providerKind is the kind of the provider an agent names in tenant's
// workspace: a declared provider's, a workspace provider's, or for the
// built-in kinds the name itself.
func (p *Platform) providerKind(tenant, name string) string {
	if kind := p.cfg.DeclaredKinds[name]; kind != "" {
		return kind
	}
	if m, ok := p.workspaceProvider(tenant, name); ok {
		return m.Kind
	}
	return name
}

// takesEffort reports whether model, behind the provider named provider,
// takes reasoning effort effort, as the model catalog the runners build
// with says.
func (p *Platform) takesEffort(tenant, provider, model, effort string) bool {
	caps, ok := catalog.Lookup(saigetypes.ProviderName(p.providerKind(tenant, provider)), model)
	return ok && caps.Supports(saigetypes.CapReasoningEffort) && slices.Contains(caps.ReasoningEfforts, effort)
}

// withEffort puts what the platform keeps with the pinned revision, since
// saige's definitions carry neither, on the request's copy of its spec: its
// reasoning effort (wire.AgentSpec.Effort), which resolveModel decides
// whether to send, and the schema its answer is asked for under
// (outputSchema). A revision that cannot be read runs at the model's
// default effort, and with no schema.
func (p *Platform) withEffort(req runtime.Request) runtime.Request {
	rev, err := p.Catalog().Get(context.Background(), req.Tenant, req.Definition.Name, wire.RevOf(req.Definition.Version))
	if err == nil {
		req.Definition.Spec.Effort = rev.Spec.Effort
		if schema := outputSchema(rev); schema != nil {
			req.Definition.Spec.Output = &turn.Output{Schema: schema}
		}
	}
	return req
}

// readProvider is what a stored provider name reads as in tenant's workspace:
// a name since renamed (legacy.ProviderName) reads as its new name, unless
// something there still goes by the old one (has), and the first such read
// for a workspace is noted. Any other name is itself.
func (p *Platform) readProvider(tenant, name string, has func(string) bool) string {
	to, renamed := legacy.ProviderName(name)
	if !renamed || has(name) {
		return name
	}
	if _, noted := p.renamedProviders.LoadOrStore(tenant+"\x00"+name, true); !noted {
		p.log.Info("platform: a stored model provider name reads as its new name", "tenant", tenant, "stored", name, "reads_as", to)
	}
	return to
}

// hasProvider reports whether a provider by name is one tenant's agents may
// name: the deployment's, the workspace's own, or a kind an administrator
// may enable.
func (p *Platform) hasProvider(tenant, name string) bool {
	return slices.Contains(p.agentProviders(tenant), name)
}

// readPolicies is a workspace's stored model policies with each provider
// name read as it is now. has says what the workspace has by name.
func (p *Platform) readPolicies(tenant string, list []ModelPolicy, has func(string) bool) []ModelPolicy {
	out, copied := list, false
	for i := range out {
		for j, ref := range out[i].Models {
			now := p.readProvider(tenant, ref.Provider, has)
			if now == ref.Provider {
				continue
			}
			if !copied {
				out = slices.Clone(list)
				for k := range out {
					out[k].Models = slices.Clone(out[k].Models)
				}
				copied = true
			}
			out[i].Models[j].Provider = now
		}
	}
	return out
}

// readChain is spec with each provider name it names read as it is now.
func (p *Platform) readChain(tenant string, spec wire.AgentSpec) wire.AgentSpec {
	has := func(name string) bool { return p.hasProvider(tenant, name) }
	spec.Provider = p.readProvider(tenant, spec.Provider, has)
	if len(spec.Fallbacks) > 0 {
		spec.Fallbacks = slices.Clone(spec.Fallbacks)
		for i := range spec.Fallbacks {
			spec.Fallbacks[i].Provider = p.readProvider(tenant, spec.Fallbacks[i].Provider, has)
		}
	}
	return spec
}
