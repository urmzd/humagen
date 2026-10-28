package platform

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/urmzd/fiber/pkg/agentrt"
	"github.com/urmzd/fiber/pkg/runtime"

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

func checkModelPolicies(list []ModelPolicy) error {
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
		// A policy is checked exactly as fiber checks the agent fields it becomes.
		if err := m.spec("policy-" + m.Name).ValidateWith(wire.KnownProviders()); err != nil {
			return fmt.Errorf("%w: %q: %v", ErrModelPolicy, m.Name, err)
		}
	}
	return nil
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
func (p *Platform) resolveModel(req runtime.Request) (runtime.Request, error) {
	spec := req.Revision.Spec
	if spec.Provider != PolicyProvider {
		return req, nil
	}
	m, ok := policyIn(p.Settings(req.Tenant).ModelPolicies, spec.Model)
	if !ok {
		return req, fmt.Errorf("%w: agent %q runs on the model policy %q, which the workspace no longer has", ErrModelPolicy, spec.Name, spec.Model)
	}
	resolved := m.spec(spec.Name)
	spec.Provider, spec.Model, spec.FallbackOn = resolved.Provider, resolved.Model, resolved.FallbackOn
	spec.Fallbacks = make([]agentrt.ModelRef, 0, len(resolved.Fallbacks))
	for _, f := range resolved.Fallbacks {
		spec.Fallbacks = append(spec.Fallbacks, agentrt.ModelRef{Provider: f.Provider, Model: f.Model})
	}
	if len(spec.Fallbacks) == 0 {
		spec.Fallbacks = nil
	}
	req.Revision.Spec = spec
	return req, nil
}

// ModelProviderChoice describes configuration names, never credential values.
// Declared records a host registration; actual connectivity requires a run.
type ModelProviderChoice struct {
	Name     string `json:"name"`
	Declared bool   `json:"declared"`
}

func (p *Platform) ModelProviderChoices() []ModelProviderChoice {
	names := p.agentProviders()
	slices.Sort(names)
	names = slices.Compact(names)
	out := []ModelProviderChoice{}
	for _, name := range names {
		if name == PolicyProvider {
			continue
		}
		out = append(out, ModelProviderChoice{Name: name, Declared: p.cfg.Ext.Providers[name] != nil || (name != "offline" && p.cfg.BuiltinProvider == name)})
		if len(out) >= 100 {
			break
		}
	}
	return out
}
