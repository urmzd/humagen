package platform

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/urmzd/humagen/fiber/pkg/event"
	"github.com/urmzd/humagen/fiber/pkg/runtime"
	"github.com/urmzd/humagen/fiber/pkg/turn/native"
	"github.com/urmzd/humagen/internal/wire"

	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/personctx"
	"github.com/urmzd/humagen/pkg/runctx"
	"github.com/urmzd/humagen/pkg/runs"
	runsettings "github.com/urmzd/humagen/pkg/settings"
)

// The run context (pkg/runctx): who a run acts for, and the ceilings and
// preferences of its place and its person, resolved once at admission and
// pinned to the run's trigger with a digest.
//
//   - Invoke resolves it (pinRunContext) after the run's place is known and
//     before admission, so every attempt reads the same one back.
//   - A run another agent calls inherits its caller's, narrowed by its own
//     place's ceilings, and keeps the caller's person.
//   - Each turn applies it (preferRunContext, boundRunContext): the
//     preferred model in place of the agent's, the ceilings on models and
//     tools, and the history budget.
//   - Each call is held to it (callLimits): the call depth ceiling, and only
//     agents the person the run acts for may read.

// ErrRunContext reports a run its context refuses, or a pinned context that
// cannot be read.
var ErrRunContext = errors.New("platform: run context")

// runLayers is the file's run settings for one workspace: the workspace's
// own and each project's.
type runLayers struct {
	workspace runsettings.Layer
	projects  map[string]runsettings.Layer
}

// runSettings holds each tenant's runLayers. The file is applied at every
// start, so they are kept in memory only.
type runSettings struct {
	mu sync.RWMutex
	by map[string]runLayers
}

// SetRunSettings replaces a workspace's run settings: the workspace's level
// and each project's, by name. A project not named sets nothing. They take
// effect for runs admitted from now on.
func (p *Platform) SetRunSettings(tenant string, workspace runsettings.Layer, projectLayers map[string]runsettings.Layer) error {
	if err := workspace.Check(); err != nil {
		return fmt.Errorf("%w: workspace.run: %w", ErrSettings, err)
	}
	for name, l := range projectLayers {
		if err := l.Check(); err != nil {
			return fmt.Errorf("%w: project %q run: %w", ErrSettings, name, err)
		}
	}
	p.runSettings.mu.Lock()
	defer p.runSettings.mu.Unlock()
	if p.runSettings.by == nil {
		p.runSettings.by = map[string]runLayers{}
	}
	p.runSettings.by[tenant] = runLayers{workspace: workspace, projects: maps.Clone(projectLayers)}
	return nil
}

// resolveSettings is what a run filed in project, within its ancestors
// (nearest first), acting for person resolves to.
func (p *Platform) resolveSettings(tenant, project string, within []string, person string) runsettings.Resolved {
	p.runSettings.mu.RLock()
	layers := p.runSettings.by[tenant]
	p.runSettings.mu.RUnlock()
	var below []runsettings.Layer
	for _, name := range slices.Backward(chainOf(project, within)) {
		if l, ok := layers.projects[name]; ok {
			below = append(below, l)
		}
	}
	if person != "" {
		below = append(below, p.PersonContexts.Get(tenant, person).Run)
	}
	return runsettings.Resolve(layers.workspace, below...)
}

// pinRunContext writes the run's context into its tag. Whatever the tag
// said about one is dropped first: only the platform pins a context.
func (p *Platform) pinRunContext(tag event.Envelope, edge call.Edge, project string, within []string) (event.Envelope, error) {
	var c runctx.Context
	if edge.Parent != "" {
		parent, err := p.callerContext(tag.Tenant, edge.Parent)
		if err != nil {
			return tag, err
		}
		own := p.resolveSettings(tag.Tenant, project, within, parent.Person)
		c = parent.Narrow(own.Ceilings)
	} else {
		person := p.operationTagPerson(tag, edge)
		c = runctx.New(person, p.resolveSettings(tag.Tenant, project, within, person), p.personContext(tag.Tenant, person))
	}
	var err error
	wire.EditMeta(&tag, func(m map[string]string) {
		runctx.Unpin(m)
		err = runctx.Pin(m, c)
	})
	return tag, err
}

func (p *Platform) personContext(tenant, person string) personctx.Context {
	if person == "" {
		return personctx.Context{}
	}
	return p.PersonContexts.Get(tenant, person)
}

// callerContext is the context pinned to a calling run, named by its tag.
func (p *Platform) callerContext(tenant, parentTag string) (runctx.Context, error) {
	parent, err := p.Runs.Get(tenant, runtime.RunID(tenant, parentTag))
	if err != nil {
		if parent, err = p.Runs.Get(tenant, parentTag); err != nil {
			return runctx.Context{}, fmt.Errorf("%w: the calling run %q is not known", ErrRunContext, parentTag)
		}
	}
	return p.contextOfRun(parent)
}

// contextOfRun is the context pinned to a run. A run admitted before
// contexts were pinned has none, and gets the one its place and the person
// it acts for resolve to now.
func (p *Platform) contextOfRun(run runs.Run) (runctx.Context, error) {
	c, ok, err := runctx.Of(wire.Meta(run.Tag()))
	if err != nil {
		return runctx.Context{}, fmt.Errorf("%w: %w", ErrRunContext, err)
	}
	if !ok {
		person := personOf(run.Actor)
		return runctx.New(person, p.resolveSettings(run.Tenant, run.Project, run.Within, person), p.personContext(run.Tenant, person)), nil
	}
	return c, nil
}

// RunContext is the context a run was admitted with. ok is false for a run
// admitted before contexts were pinned.
func (p *Platform) RunContext(tenant, runID string) (c runctx.Context, ok bool, err error) {
	run, err := p.Runs.Get(tenant, runID)
	if err != nil {
		return runctx.Context{}, false, err
	}
	c, ok, err = runctx.Of(wire.Meta(run.Tag()))
	if err != nil {
		return runctx.Context{}, true, fmt.Errorf("%w: %w", ErrRunContext, err)
	}
	return c, ok, nil
}

// requestContext is the context a turn's request carries: the one its
// trigger was pinned with. A request with none (a run admitted before
// contexts were pinned) gets the zero context, which narrows nothing.
func requestContext(req runtime.Request) (runctx.Context, error) {
	c, _, err := runctx.Of(req.Meta)
	if err != nil {
		return runctx.Context{}, fmt.Errorf("%w: %w", ErrRunContext, err)
	}
	return c, nil
}

// preferRunContext puts the run's preferred model in place of its agent's,
// before a model policy it names is resolved.
func preferRunContext(req runtime.Request) (runtime.Request, error) {
	c, err := requestContext(req)
	if err != nil {
		return req, err
	}
	if m := c.Settings.Preferences.Model; m != "" {
		provider, model, _ := runsettings.SplitModel(m)
		req.Definition.Spec.Provider, req.Definition.Spec.Model = provider, model
	}
	if n := c.Settings.Preferences.CompactionTokens; n > 0 {
		req.Compaction = &native.Compaction{MaxInputTokens: n}
	}
	return req, nil
}

// boundRunContext holds the turn to the run's ceilings: its model and
// fallbacks to the models allowed, the first allowed becoming the model, and
// its tools to the tools allowed. A run left with no model is refused.
func boundRunContext(req runtime.Request) (runtime.Request, error) {
	c, err := requestContext(req)
	if err != nil {
		return req, err
	}
	ceil := c.Settings.Ceilings
	spec := req.Definition.Spec
	if ceil.Models != nil {
		chain := append([]native.ModelRef{{Provider: spec.Provider, Model: spec.Model}}, spec.Fallbacks...)
		chain = slices.DeleteFunc(chain, func(m native.ModelRef) bool { return !ceil.AllowsModel(m.Provider + "/" + m.Model) })
		if len(chain) == 0 {
			return req, fmt.Errorf("%w: agent %q runs on %s/%s, which this run's settings do not allow, and has no allowed fallback", ErrRunContext, spec.Name, spec.Provider, spec.Model)
		}
		spec.Provider, spec.Model = chain[0].Provider, chain[0].Model
		spec.Fallbacks = nil
		if len(chain) > 1 {
			spec.Fallbacks = chain[1:]
		}
	}
	if ceil.Tools != nil {
		spec.Tools = slices.DeleteFunc(slices.Clone(spec.Tools), func(name string) bool { return !ceil.AllowsTool(name) })
	}
	req.Definition.Spec = spec
	return req, nil
}

// callLimits is the deployment's call limits held to the calling run's
// context, and refuses a call to an agent the person the run acts for may
// not read: a run can do what its agent's grants allow and its requester
// may do, never more.
func (p *Platform) callLimits(parent runs.Run, agent string) (call.Limits, error) {
	lim := p.cfg.Limits
	c, err := p.contextOfRun(parent)
	if err != nil {
		return lim, err
	}
	lim.MaxDepth = c.Settings.Ceilings.Depth(lim.MaxDepth)
	if c.Person != "" && !p.Seer(p.byPolicy(parent.Tenant, c.Person)).Agent(agent) {
		return lim, fmt.Errorf("%w: %s, whom this run acts for, may not read %q", ErrForbidden, p.NameOf(parent.Tenant, c.Person), agent)
	}
	return lim, nil
}

// PersonContext is the caller's own context.
func (p *Platform) PersonContext(who auth.Principal) (personctx.Context, error) {
	if who.Kind == auth.Machine {
		return personctx.Context{}, fmt.Errorf("%w: a machine has no context of its own", ErrForbidden)
	}
	return p.PersonContexts.Get(who.Tenant, who.Subject), nil
}

// SetPersonContext replaces the caller's own context. Every tool server's
// credential reference must be under one of the caller's own starts, as a
// credential they connect must (System person_refs).
func (p *Platform) SetPersonContext(_ context.Context, who auth.Principal, in personctx.Input) (personctx.Context, error) {
	if who.Kind == auth.Machine {
		return personctx.Context{}, fmt.Errorf("%w: a machine has no context of its own; each person sets theirs", ErrForbidden)
	}
	if err := in.Check(); err != nil {
		return personctx.Context{}, err
	}
	for _, t := range in.ToolServers {
		if t.Credential != "" && !p.refAllowed(who.Tenant, who.Subject, t.Credential) {
			return personctx.Context{}, fmt.Errorf("%w: tool server %q: %w", personctx.ErrInvalid, t.Name, errPersonRef)
		}
	}
	attempt, err := p.BeginAudit(who, audit.Change{Action: "person_context.set", Resource: "person_context", ID: who.Subject, Subject: who.Subject, Route: "PUT /api/me/context", Rule: string(auth.RunsWrite)})
	if err != nil {
		return personctx.Context{}, err
	}
	out, err := p.PersonContexts.Set(who.Tenant, who.Subject, in, p.now())
	if completion := attempt.Finish(err); completion != nil {
		p.LogAuditCompletionFailure(completion)
	}
	return out, err
}
