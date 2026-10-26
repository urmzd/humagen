package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/urmzd/fiber/pkg/agentrt"
	"github.com/urmzd/fiber/pkg/agentrt/payload"
	"github.com/urmzd/fiber/pkg/envelope"

	"github.com/urmzd/fiber/pkg/runtime"
	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/operations"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	saigetypes "github.com/urmzd/saige/agent/types"
)

// ActionDecision tightens existing grants. An unspecified rule grants nothing.
func (p *Platform) ActionDecision(tenant, project, agent, person string, c operations.Category) operations.Level {
	scopes := []operations.Scope{{Kind: "workspace"}}
	for _, pr := range p.Projects.Tree(tenant).Chain(project) {
		scopes = append(scopes, operations.Scope{Kind: "project", Name: pr.Name})
	}
	scopes = append(scopes, operations.Scope{Kind: "agent", Name: legacy.BuiltinAgent(agent)})
	if person != "" {
		scopes = append(scopes, operations.Scope{Kind: "personal", Name: person})
	}
	return p.Operations.Decision(tenant, scopes, c)
}
func (p *Platform) AgentControl(tenant, agent string) operations.Control {
	return p.Operations.Control(tenant, legacy.BuiltinAgent(agent))
}
func (p *Platform) SetAgentControl(who auth.Principal, agent string, c operations.Control, version int) (operations.Control, error) {
	if agent != "" {
		if _, err := p.Agents.Get(context.Background(), who.Tenant, agent, 0); err != nil {
			return operations.Control{}, err
		}
		if !p.Seer(who).Agent(agent) {
			return operations.Control{}, operations.ErrNotFound
		}
		if err := p.OwnsHome(who, agent); err != nil {
			return operations.Control{}, err
		}
	}
	if c.Manager != "" {
		if _, err := p.People.Get(who.Tenant, c.Manager); err != nil {
			return operations.Control{}, fmt.Errorf("%w: manager does not exist", operations.ErrInvalid)
		}
	}
	p.admission.Lock()
	defer p.admission.Unlock()
	return p.Operations.SetControl(who.Tenant, legacy.BuiltinAgent(agent), c, version, who.Subject, p.now())
}
func (p *Platform) operationAdmission(tenant, agent string) error {
	if p.Operations.Paused(tenant, legacy.BuiltinAgent(agent)) {
		return operations.ErrPaused
	}
	return nil
}
func (p *Platform) operationPlace(req runtime.Request) (project, person string) {
	if run, err := p.Runs.Get(req.Tenant, req.RunID); err == nil {
		person = runs.AskedBy(run)
		if run.Parent != "" {
			if root, err := p.Runs.Get(req.Tenant, run.Root); err == nil {
				person = runs.AskedBy(root)
			}
		}
		return run.Project, person
	}
	if v, ok := p.admitting.Load(req.RunID); ok {
		return v.(admitted).place.Project, ""
	}
	return "", ""
}
func (p *Platform) operationDecision(req runtime.Request, c operations.Category) operations.Level {
	project, person := p.operationPlace(req)
	level := p.ActionDecision(req.Tenant, project, req.Agent(), person, c)
	force := req.Meta[operations.MetaForceAsk] == "true"
	if run, err := p.Runs.Get(req.Tenant, req.RunID); err == nil {
		force = force || run.Tag().Meta[operations.MetaForceAsk] == "true"
	}
	if force && level != operations.Never {
		return operations.Ask
	}
	return level
}
func (p *Platform) resolveOperations(req runtime.Request) (runtime.Request, error) {
	if err := p.operationAdmission(req.Tenant, req.Agent()); err != nil {
		return req, err
	}
	allowed, restricted, err := p.operationTools(req)
	if err != nil {
		return req, err
	}
	if restricted {
		req.Revision.Spec.Tools = slices.DeleteFunc(slices.Clone(req.Revision.Spec.Tools), func(name string) bool { return !agentrt.IsToolGroup(name) && !slices.Contains(allowed, name) })
		req.Revision.Spec.Access.Spawn = slices.DeleteFunc(slices.Clone(req.Revision.Spec.Access.Spawn), func(agent string) bool {
			return !slices.Contains(allowed, ToolCallAgent) && !slices.Contains(allowed, ToolDelegate) && !slices.Contains(allowed, tagTool+agent)
		})
	}
	if p.operationDecision(req, operations.CallAgents) == operations.Ask {
		req.Revision.Spec.Approvals = slices.Clone(req.Revision.Spec.Approvals)
		for _, agent := range req.Revision.Spec.Access.Spawn {
			name := tagTool + agent
			if slices.Contains(req.Revision.Spec.Tools, name) && !slices.Contains(req.Revision.Spec.Approvals, name) {
				req.Revision.Spec.Approvals = append(req.Revision.Spec.Approvals, name)
			}
		}
	}
	control := p.AgentControl(req.Tenant, req.Agent())
	if control.StopRules != "" {
		req.Instructions += "\nAgent stop rules and reminders: " + control.StopRules
	}
	// Spec copies retain their original grants. Every offered tool is wrapped
	// at the catalog boundary, and calls also check the call admission path.
	return req, nil
}
func categoryOf(name string) operations.Category {
	switch name {
	case ToolCallAgent, ToolDelegate:
		return operations.CallAgents
	case ToolFileRead, ToolFileList, ToolRunsSummary, ToolListAlerts, ToolListAgents, ToolReadAgent, "source_search":
		return operations.ReadContext
	case ToolFileWrite, ToolSaveAgent:
		return operations.WriteFiles
	case ToolPublish:
		return operations.PublishArtifacts
	}
	if strings.HasPrefix(name, "tag_") {
		return operations.CallAgents
	}
	if strings.Contains(name, "send") || strings.Contains(name, "reply") || strings.Contains(name, "post_message") {
		return operations.ExternalMessages
	}
	return operations.Other
}

type operationRequestKey struct{}
type operationsCatalog struct {
	p      *Platform
	req    runtime.Request
	inner  agentrt.Tools
	groups runtime.ToolGroupSource
}

func (p *Platform) operationsCatalog(base runtime.Catalog, groups runtime.ToolGroupSource) runtime.Catalog {
	return func(req runtime.Request, action func(payload.Action) error) (agentrt.Tools, error) {
		inner, err := base(req, action)
		if err != nil {
			return nil, err
		}
		return &operationsCatalog{p: p, req: req, inner: inner, groups: groups}, nil
	}
}
func (c *operationsCatalog) Lookup(name string) (saigetypes.Tool, error) {
	t, err := c.inner.Lookup(name)
	if err != nil {
		return nil, err
	}
	return c.p.operationTool(c.req, t, categoryOf(name)), nil
}
func (c *operationsCatalog) ToolGroup(name string) (agentrt.ToolGroup, error) {
	if c.groups != nil {
		// The sandbox's execution context is rebuilt inside playscape. Derive
		// identity only from this run's durable tag (or its admission record),
		// never from a process context or another person's active turn.
		var actor envelope.Actor
		if run, err := c.p.Runs.Get(c.req.Tenant, c.req.RunID); err == nil {
			actor = run.Actor
		} else if v, ok := c.p.admitting.Load(c.req.RunID); ok && v.(admitted).tenant == c.req.Tenant {
			actor = v.(admitted).actor
		} else {
			return agentrt.ToolGroup{}, operations.ErrDenied
		}
		execution, exists := c.p.operationContexts.Load(operationExecutionKey{c.req.Tenant, c.req.RunID, c.req.Attempt})
		if !exists {
			return agentrt.ToolGroup{}, operations.ErrDenied
		}
		ctx := envelope.WithActor(execution.(context.Context), actor)
		ts, ok, err := c.groups.Expand(ctx, c.req.Tenant, name)
		if err != nil {
			return agentrt.ToolGroup{}, err
		}
		if ok {
			allowed, restricted, err := c.p.operationTools(c.req)
			if err != nil {
				return agentrt.ToolGroup{}, err
			}
			category := operations.ReadContext
			if strings.HasSuffix(name, ":write") {
				category = operations.Other
			}
			out := make([]saigetypes.Tool, 0, len(ts))
			for _, t := range ts {
				if restricted && !slices.Contains(allowed, t.Definition().Name) {
					continue
				}
				wrapped := c.p.operationTool(c.req, t, category).(*operationTool)
				if strings.HasSuffix(name, ":write") {
					wrapped.extra = operations.ExternalMessages
				}
				out = append(out, wrapped)
			}
			return agentrt.ToolGroup{Name: name, Tools: out}, nil
		}
	}
	if g, ok := c.inner.(agentrt.ToolGroups); ok {
		return g.ToolGroup(name)
	}
	return agentrt.ToolGroup{}, agentrt.ErrNoSuchToolGroup
}

type operationTool struct {
	saigetypes.Tool
	p        *Platform
	req      runtime.Request
	category operations.Category
	extra    operations.Category
}

func (p *Platform) operationTool(req runtime.Request, t saigetypes.Tool, c operations.Category) saigetypes.Tool {
	return &operationTool{Tool: t, p: p, req: req, category: c}
}
func (t *operationTool) ActsForPerson() bool {
	v, ok := t.Tool.(agentrt.PersonScoped)
	return ok && v.ActsForPerson()
}
func (t *operationTool) Approval(c saigetypes.ToolCallPart) (agentrt.ApprovalRequest, bool) {
	if a, ok := t.Tool.(agentrt.Approving); ok {
		if q, needed := a.Approval(c); needed {
			return q, true
		}
	}
	if t.decision() != operations.Ask {
		return agentrt.ApprovalRequest{}, false
	}
	control := t.p.AgentControl(t.req.Tenant, t.req.Agent())
	approvers := slices.Clone(fallbackApprovers)
	if control.Manager != "" {
		approvers = append(approvers, control.Manager)
	}
	return agentrt.ApprovalRequest{Prompt: fmt.Sprintf("Allow %s to use %s?", t.req.Agent(), t.Definition().Name), Approvers: approvers, ExpiresIn: 24 * time.Hour}, true
}
func (t *operationTool) check(ctx context.Context) error {
	if allowed, restricted, err := t.p.operationTools(t.req); err != nil {
		return err
	} else if restricted && !slices.Contains(allowed, t.Definition().Name) {
		return operations.ErrDenied
	}
	if err := t.p.operationAdmission(t.req.Tenant, t.req.Agent()); err != nil {
		return err
	}
	switch t.decision() {
	case operations.Never:
		return operations.ErrDenied
	case operations.Ask:
		info, _ := saigetypes.ToolCallInfoFromContext(ctx)
		approved := t.p.approvedCallTo(t.req.Tenant, t.req.RunID, info.ID+":action", "tool:"+t.Definition().Name)
		if t.req.Resume != nil {
			if a, ok := t.req.Resume.Answers[info.ID]; ok && a.Decision == payload.DecisionApprove {
				approved = true
			}
		}
		if !approved {
			return fmt.Errorf("%w: a person must approve this action", operations.ErrDenied)
		}
	}
	return nil
}
func (t *operationTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if err := t.check(ctx); err != nil {
		return "", err
	}
	return t.Tool.Execute(context.WithValue(ctx, operationRequestKey{}, t.req), args)
}
func (t *operationTool) ExecuteRich(ctx context.Context, args map[string]any) (saigetypes.ToolResult, error) {
	if err := t.check(ctx); err != nil {
		return saigetypes.ToolResult{}, err
	}
	if rich, ok := t.Tool.(saigetypes.RichTool); ok {
		return rich.ExecuteRich(context.WithValue(ctx, operationRequestKey{}, t.req), args)
	}
	out, err := t.Tool.Execute(ctx, args)
	return saigetypes.TextResult(out), err
}

var _ agentrt.Approving = (*operationTool)(nil)
var _ agentrt.PersonScoped = (*operationTool)(nil)
var _ saigetypes.RichTool = (*operationTool)(nil)

func (p *Platform) SetActionPolicy(who auth.Principal, scope operations.Scope, rules map[operations.Category]operations.Level, version int) (operations.Policy, error) {
	switch scope.Kind {
	case "agent":
		if _, err := p.Agents.Get(context.Background(), who.Tenant, scope.Name, 0); err != nil {
			return operations.Policy{}, err
		}
		if !p.Seer(who).Agent(scope.Name) {
			return operations.Policy{}, operations.ErrNotFound
		}
		if err := p.OwnsHome(who, scope.Name); err != nil {
			return operations.Policy{}, err
		}
		scope.Name = legacy.BuiltinAgent(scope.Name)
	case "project":
		if _, _, err := p.projectRole(p.Projects.Tree(who.Tenant), who, scope.Name, projects.Owner); err != nil {
			return operations.Policy{}, err
		}
	case "personal":
		scope.Name = who.Subject
	case "workspace":
	default:
		return operations.Policy{}, operations.ErrInvalid
	}
	return p.Operations.SetPolicy(who.Tenant, operations.Policy{Scope: scope, Rules: rules}, version, who.Subject, p.now())
}
func (p *Platform) Handoffs(who auth.Principal, limit int) []operations.Handoff {
	seer := p.Seer(who)
	return p.Operations.Handoffs(who.Tenant, limit, func(h operations.Handoff) bool {
		return seer.Place(Place{Project: h.Project, Thread: h.Thread}) && seer.Agent(h.From) && seer.Agent(h.To)
	})
}

type NetworkEdge struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Handoffs int    `json:"handoffs"`
	Blocked  int    `json:"blocked"`
	Waiting  int    `json:"waiting"`
}
type AgentNetwork struct {
	Edges     []NetworkEdge `json:"edges"`
	Truncated bool          `json:"truncated"`
}

func (p *Platform) AgentNetwork(who auth.Principal) AgentNetwork {
	hs := p.Handoffs(who, operations.MaxResults)
	out := AgentNetwork{Edges: []NetworkEdge{}, Truncated: len(hs) == operations.MaxResults}
	idx := map[[2]string]int{}
	for _, h := range hs {
		key := [2]string{h.From, h.To}
		i, ok := idx[key]
		if !ok {
			i = len(out.Edges)
			idx[key] = i
			out.Edges = append(out.Edges, NetworkEdge{From: h.From, To: h.To})
		}
		e := &out.Edges[i]
		e.Handoffs++
		if h.State == operations.Blocked || h.State == operations.NeedsPerson {
			e.Blocked++
		}
		if h.State == operations.Requested || h.State == operations.Accepted || h.State == operations.InProgress {
			e.Waiting++
		}
	}
	return out
}

func (p *Platform) operationTools(req runtime.Request) ([]string, bool, error) {
	raw, ok := req.Meta[operations.MetaTools]
	if run, err := p.Runs.Get(req.Tenant, req.RunID); err == nil {
		if v, pinned := run.Tag().Meta[operations.MetaTools]; pinned {
			raw, ok = v, true
		}
	}
	if !ok {
		return nil, false, nil
	}
	var tools []string
	if len(raw) > 32768 || json.Unmarshal([]byte(raw), &tools) != nil || tools == nil || len(tools) > 256 {
		return nil, true, fmt.Errorf("%w: malformed pinned skill tool restriction", operations.ErrDenied)
	}
	for _, name := range tools {
		if name == "" || len(name) > 256 {
			return nil, true, operations.ErrDenied
		}
	}
	return tools, true, nil
}

func (t *operationTool) decision() operations.Level {
	level := t.p.operationDecision(t.req, t.category)
	if t.extra != "" {
		extra := t.p.operationDecision(t.req, t.extra)
		if extra == operations.Never || extra == operations.Ask && level != operations.Never {
			level = extra
		}
	}
	return level
}

type operationExecutionKey struct {
	tenant, run string
	attempt     int
}

func (p *Platform) bindOperationContext(ctx context.Context, req runtime.Request) func() {
	key := operationExecutionKey{req.Tenant, req.RunID, req.Attempt}
	p.operationContexts.Store(key, ctx)
	return func() { p.operationContexts.Delete(key) }
}
