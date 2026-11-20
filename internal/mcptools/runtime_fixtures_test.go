package mcptools_test

// Fixtures for the runtime tests in this package, the same as saige's
// flow/runtime tests use: a control plane's definitions that can be revised,
// a request pinned as accept pins it, and canned models per agent.

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/urmzd/saige/agent/agenttest"
	saigetypes "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/definition"
	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/runtime"
	"github.com/urmzd/saige/flow/turn/native"
)

// specs is a control plane's definitions, complete enough to be revised and
// to answer a pinned lookup after it has been. Every tenant reads the same
// definitions, as the fixtures here are not about tenancy.
type specs struct {
	*definition.Resolver
	src *anyTenant

	mu       sync.Mutex
	versions map[string]int // agent -> latest major version
}

func newSpecs() *specs {
	src := &anyTenant{Source: definition.NewMemory(), tampered: map[string]string{}}
	return &specs{Resolver: definition.NewResolver(src, definition.Checks{}), src: src, versions: map[string]int{}}
}

// specContent is the definition file every fixture agent is written as.
func specContent(agent, version, prompt string) string {
	return fmt.Sprintf("---\napiVersion: saige/v1\nname: %s\nversion: %s\nmodel: offline/scripted\n---\n%s\n", agent, version, prompt)
}

// revise writes the next major version of an agent and returns it.
func (s *specs) revise(agent, prompt string) native.Revision {
	return s.set(native.Spec{Name: agent, Provider: native.ProviderOffline, Model: "scripted", SystemPrompt: prompt})
}

// set writes spec as the next major version of its agent and returns it. The
// agents it delegates to must already exist.
func (s *specs) set(spec native.Spec) native.Revision {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.versions[spec.Name]++
	old := definition.AgentSpec{
		Name: spec.Name, Description: spec.Description, Model: spec.Model, Provider: spec.Provider,
		SystemPrompt: spec.SystemPrompt, Tools: spec.Tools,
		Access:     definition.AgentSpecAccess{Spawn: spec.Access.Spawn, WorkspaceAreas: spec.Access.WorkspaceAreas},
		FallbackOn: spec.FallbackOn, Approvals: spec.Approvals, Approvers: spec.Approvers,
		SelfApproval: spec.SelfApproval, Answerers: spec.Answerers, TaggedOnly: spec.TaggedOnly,
	}
	for _, f := range spec.Fallbacks {
		old.Fallbacks = append(old.Fallbacks, definition.AgentSpecModel{Provider: f.Provider, Model: f.Model})
	}
	for _, c := range spec.Sources {
		old.Sources = append(old.Sources, definition.Corpus{Name: c.Name, URI: c.URI, Options: c.Options})
	}
	rec, err := definition.FromAgentSpec(old, s.versions[spec.Name])
	if err != nil {
		panic(err)
	}
	rec.Tenant, rec.CreatedBy = "acme", "test"
	if rec, err = s.src.Put(context.Background(), rec); err != nil {
		panic(err)
	}
	a, err := definition.Parse(rec.Content, rec.Policy, definition.Checks{})
	if err != nil {
		panic(err)
	}
	// The digest a run reports is the resolved one, which needs every
	// sub-agent to exist. One written before its sub-agents reports the
	// file's own until then.
	digest := rec.Digest
	if pin, err := s.ResolveAt(context.Background(), fixtureTenant, rec.Name, rec.Version); err == nil {
		digest = pin.Digest
	}
	rev := native.Revision{Name: rec.Name, Version: rec.Version, Digest: digest, Spec: a.Spec}
	fixtures.Store(pinKey(rev), s)
	return rev
}

// fixtures maps every version a fixture wrote to the fixture, so a test that
// calls an executor directly can send the request accept would have built.
var fixtures sync.Map

func pinKey(rev native.Revision) string { return rev.Name + "@" + rev.Version + "#" + rev.Digest }

// withPin is req for rev, pinned now as accept pins it.
func withPin(req runtime.Request, rev native.Revision) runtime.Request {
	v, ok := fixtures.Load(pinKey(rev))
	if !ok {
		panic("no fixture wrote " + pinKey(rev))
	}
	s := v.(*specs)
	pin, err := s.ResolveAt(context.Background(), fixtureTenant, rev.Name, rev.Version)
	if err != nil {
		panic(err)
	}
	loaded, err := s.Verify(context.Background(), fixtureTenant, pin)
	if err != nil {
		panic(err)
	}
	req.Definition = native.Revision{Name: pin.Name, Version: pin.Version, Digest: pin.Digest, Spec: loaded.Agent.Spec}
	req.Pin, req.Policy = pin, loaded.Agent.Policy
	return req
}

// edit writes the next version of an agent with its latest spec changed by
// mod.
func (s *specs) edit(agent string, mod func(*native.Spec)) native.Revision {
	rec, err := s.src.Latest(context.Background(), fixtureTenant, agent)
	if err != nil {
		panic(err)
	}
	a, err := definition.Parse(rec.Content, rec.Policy, definition.Checks{})
	if err != nil {
		panic(err)
	}
	spec := a.Spec
	mod(&spec)
	return s.set(spec)
}

// anyTenant is a Source that keeps every tenant's definitions as one, and can
// read a version back tampered with.
type anyTenant struct {
	definition.Source
	mu       sync.Mutex
	tampered map[string]string // name@version -> prompt
}

const fixtureTenant = "any"

func (a *anyTenant) Put(ctx context.Context, r definition.Record) (definition.Record, error) {
	r.Tenant = fixtureTenant
	return a.Source.Put(ctx, r)
}

func (a *anyTenant) Get(ctx context.Context, _, name, version string) (definition.Record, error) {
	r, err := a.Source.Get(ctx, fixtureTenant, name, version)
	a.mu.Lock()
	prompt, ok := a.tampered[name+"@"+version]
	a.mu.Unlock()
	if err == nil && ok {
		r.Content = specContent(name, version, prompt)
	}
	return r, err
}

func (a *anyTenant) Versions(ctx context.Context, _, name string) ([]definition.Record, error) {
	return a.Source.Versions(ctx, fixtureTenant, name)
}

func (a *anyTenant) Latest(ctx context.Context, _, name string) (definition.Record, error) {
	return a.Source.Latest(ctx, fixtureTenant, name)
}

// scripted gives each agent its own canned model, keyed by the agent's name,
// so a turn can call a tool and then answer without a model being called.
func scripted(scripts map[string][][]saigetypes.Delta) native.Option {
	var mu sync.Mutex
	return native.WithProvider(native.ProviderOffline, func(spec native.Spec) (saigetypes.Provider, error) {
		mu.Lock()
		defer mu.Unlock()
		return &agenttest.ScriptedProvider{Responses: scripts[spec.Name]}, nil
	})
}

func toolResult(t *testing.T, chunks []event.Body) event.ToolDone {
	t.Helper()
	for _, c := range chunks {
		if done, ok := c.(event.ToolDone); ok {
			return done
		}
	}
	t.Fatalf("no tool result among %d chunks", len(chunks))
	return event.ToolDone{}
}
