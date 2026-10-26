package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/urmzd/fiber/pkg/agentrt"
	"github.com/urmzd/fiber/pkg/agentrt/payload"
	"github.com/urmzd/fiber/pkg/envelope"

	"github.com/urmzd/fiber/pkg/runtime"
	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/pkg/audit"
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
func (p *Platform) SetAgentControl(who auth.Principal, agent string, c operations.Control, version int) (result operations.Control, resultErr error) {
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
	scope := "workspace"
	id := "workspace"
	if agent != "" {
		scope = "agent"
		id = agent
	}
	before := p.AgentControl(who.Tenant, agent)
	attempt, err := p.BeginAudit(who, audit.Change{Action: "agent.control", Resource: "agent_control", ID: id, Scope: scope, Agent: agent, Before: map[string]string{"paused": strconv.FormatBool(before.Paused)}, After: map[string]string{"paused": strconv.FormatBool(c.Paused)}})
	if err != nil {
		return operations.Control{}, err
	}
	defer func() { p.finishOperationsAudit(attempt, resultErr) }()
	p.admission.Lock()
	defer p.admission.Unlock()
	return p.Operations.SetControl(who.Tenant, legacy.BuiltinAgent(agent), c, version, who.Subject, p.now())
}
func (p *Platform) operationAdmission(tenant, agent string) error {
	if p.Operations.Paused(tenant, legacy.BuiltinAgent(agent)) || !p.agentReady(tenant, agent) {
		return operations.ErrPaused
	}
	return nil
}
func (p *Platform) operationPlace(req runtime.Request) (project, person string) {
	if run, err := p.Runs.Get(req.Tenant, req.RunID); err == nil {

		return p.RunPlace(req.Tenant, run).Project, p.operationPerson(run)
	}
	if v, ok := p.admitting.Load(req.RunID); ok && v.(admitted).tenant == req.Tenant {
		a := v.(admitted)
		return p.CurrentPlace(req.Tenant, a.place).Project, a.person
	}
	return "", ""
}
func (p *Platform) operationDecision(req runtime.Request, c operations.Category) operations.Level {
	project, person := p.operationPlace(req)
	level := p.ActionDecision(req.Tenant, project, req.Agent(), person, c)
	force := p.operationForced(req)
	if force && level != operations.Never {
		return operations.Ask
	}
	return level
}
func (p *Platform) resolveOperations(req runtime.Request) (runtime.Request, error) {
	if err := p.operationAdmissionRequest(req); err != nil {
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
			return c.p.wrapOperationGroup(c.req, name, ts)
		}
	}
	if g, ok := c.inner.(agentrt.ToolGroups); ok {
		group, err := g.ToolGroup(name)
		if err != nil {
			return group, err
		}
		return c.p.wrapOperationGroup(c.req, name, group.Tools)
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
	if err := t.p.operationAdmissionRequest(t.req); err != nil {
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
	if t.ActsForPerson() {
		return t.p.MarkPrivateContext(t.req.Tenant, t.req.RunID)
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
	out, err := t.Tool.Execute(context.WithValue(ctx, operationRequestKey{}, t.req), args)
	return saigetypes.TextResult(out), err
}

var _ agentrt.Approving = (*operationTool)(nil)
var _ agentrt.PersonScoped = (*operationTool)(nil)
var _ saigetypes.RichTool = (*operationTool)(nil)

func (p *Platform) SetActionPolicy(who auth.Principal, scope operations.Scope, rules map[operations.Category]operations.Level, version int) (result operations.Policy, resultErr error) {
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
	change := audit.Change{Action: "policy.action", Resource: "action_policy", ID: scope.Kind + ":" + scope.Name, Scope: scope.Kind, Before: operationPolicyFields(p.Operations.Policy(who.Tenant, scope).Rules), After: operationPolicyFields(rules)}
	switch scope.Kind {
	case "agent":
		change.Agent = scope.Name
	case "project":
		change.Project = scope.Name
	case "personal":
		change.Subject = who.Subject
	}
	attempt, err := p.BeginAudit(who, change)
	if err != nil {
		return operations.Policy{}, err
	}
	defer func() { p.finishOperationsAudit(attempt, resultErr) }()
	return p.Operations.SetPolicy(who.Tenant, operations.Policy{Scope: scope, Rules: rules}, version, who.Subject, p.now())
}
func (p *Platform) seesHandoff(seer *Seer, h operations.Handoff) bool {
	return seer.Project(h.Project) && seer.Place(Place{Project: h.Project, Thread: h.Thread}) && seer.Agent(h.From) && seer.Agent(h.To)
}
func (p *Platform) shownHandoff(who auth.Principal, h operations.Handoff) operations.Handoff {
	seer := p.Seer(who)
	h.Chain = slices.DeleteFunc(slices.Clone(h.Chain), func(name string) bool { return !seer.Agent(name) })
	inputs := []operations.Input{}
	for _, in := range h.Inputs {
		allowed := false
		switch in.Type {
		case "text":
			allowed = true
		case "run":
			allowed = seer.RunID(in.Reference)
		case "thread":
			if th, err := p.Threads.Get(who.Tenant, in.Reference); err == nil {
				allowed = seer.Place(Place{Project: th.Project, Thread: th.ID})
			}
		case "artifact":
			id, _, _ := strings.Cut(in.Reference, "@")
			if a, err := p.Artifacts.Get(who.Tenant, id); err == nil {
				allowed = seer.Artifact(a)
			}
		}
		if allowed {
			inputs = append(inputs, in)
		}
	}
	h.Inputs = inputs
	return h
}
func (p *Platform) Handoffs(who auth.Principal, limit int) []operations.Handoff {
	seer := p.Seer(who)
	out := p.Operations.Handoffs(who.Tenant, limit, func(h operations.Handoff) bool { return p.seesHandoff(seer, h) })
	for i, h := range out {
		out[i] = p.shownHandoff(who, h)
	}
	return out
}
func (p *Platform) Handoff(who auth.Principal, id string) (operations.Handoff, error) {
	h, ok := p.Operations.Handoff(who.Tenant, id)
	if !ok || !p.seesHandoff(p.Seer(who), h) {
		return operations.Handoff{}, operations.ErrNotFound
	}
	return p.shownHandoff(who, h), nil
}

type HandoffUpdate struct {
	Version int                  `json:"version"`
	State   operations.WorkState `json:"state"`
	Owner   string               `json:"owner"`
	Blocker string               `json:"blocker"`
	DueAt   time.Time            `json:"due_at"`
}

func (p *Platform) UpdateHandoff(who auth.Principal, id string, in HandoffUpdate) (result operations.Handoff, resultErr error) {
	visible, err := p.Handoff(who, id)
	if err != nil {
		return operations.Handoff{}, err
	}
	if visible.Owner != who.Subject {
		if _, _, err := p.ProjectRole(who, visible.Project, projects.Owner); err != nil {
			return operations.Handoff{}, err
		}
	}
	if in.Owner != "" && in.Owner != who.Subject {
		if _, err := p.People.Get(who.Tenant, in.Owner); err != nil {
			return operations.Handoff{}, operations.ErrInvalid
		}
	}
	if !slices.Contains([]operations.WorkState{operations.Accepted, operations.Blocked, operations.NeedsPerson, operations.Result}, in.State) || in.DueAt.IsZero() {
		return operations.Handoff{}, operations.ErrInvalid
	}
	h, _ := p.Operations.Handoff(who.Tenant, id)
	if in.State == operations.Result {
		run, err := p.Runs.Get(who.Tenant, h.RunID)
		if err != nil || run.Status != runs.Completed {
			return operations.Handoff{}, operations.ErrConflict
		}
	}
	attempt, err := p.BeginAudit(who, audit.Change{Action: "handoff.update", Resource: "handoff", ID: id, Scope: "project", Project: h.Project, Thread: h.Thread, Agent: h.From, Before: map[string]string{"state": string(h.State)}, After: map[string]string{"state": string(in.State)}})
	if err != nil {
		return operations.Handoff{}, err
	}
	defer func() { p.finishOperationsAudit(attempt, resultErr) }()
	h.State, h.Owner, h.Blocker, h.DueAt = in.State, in.Owner, in.Blocker, in.DueAt
	out, err := p.Operations.SetHandoff(who.Tenant, h, in.Version, p.now())
	if err != nil {
		return operations.Handoff{}, err
	}
	return p.shownHandoff(who, out), nil
}

type NetworkEdge struct {
	From           string `json:"from"`
	To             string `json:"to"`
	Handoffs       int    `json:"handoffs"`
	Blocked        int    `json:"blocked"`
	Waiting        int    `json:"waiting"`
	Working        int    `json:"working"`
	WaitingSeconds int64  `json:"waiting_seconds"`
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
		if h.State == operations.InProgress {
			e.Working++
		}
		if h.State == operations.Requested || h.State == operations.Accepted || h.State == operations.NeedsPerson {
			e.Waiting++
			wait := p.now().Sub(h.UpdatedAt)
			if wait < 0 {
				wait = 0
			}
			if wait > 31*24*time.Hour {
				wait = 31 * 24 * time.Hour
			}
			e.WaitingSeconds += int64(wait / time.Second)
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
		if v, exists := p.admitting.Load(req.RunID); exists && v.(admitted).tenant == req.Tenant {
			raw, ok = v.(admitted).operationMeta[operations.MetaTools]
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

func (p *Platform) wrapOperationGroup(req runtime.Request, name string, ts []saigetypes.Tool) (agentrt.ToolGroup, error) {
	allowed, restricted, err := p.operationTools(req)
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
		wrapped := p.operationTool(req, t, category).(*operationTool)
		if strings.HasSuffix(name, ":write") {
			wrapped.extra = operations.ExternalMessages
		}
		out = append(out, wrapped)
	}
	return agentrt.ToolGroup{Name: name, Tools: out}, nil
}

// operationPerson supplies policy identity only. It never supplies a credential.
// A clock has no actor but remains subject to its assignment owner's tightening.
func (p *Platform) operationPerson(run runs.Run) string {
	root := run
	if run.Parent != "" {
		if got, err := p.Runs.Get(run.Tenant, run.Root); err == nil {
			root = got
		}
	}
	if id := root.Tag().Meta[metaAssignment]; id != "" {
		if a, ok := p.Operations.Assignment(run.Tenant, id); ok {
			return a.Owner
		}
	}
	return runs.AskedBy(root)
}

// ConversationOperationsSettled is deliberately conservative: raw retained
// contract provenance requires a reviewed summary rather than history publication.
func (p *Platform) ConversationOperationsSettled(tenant, thread string) bool {
	return !p.Operations.HasConversation(tenant, thread)
}

type AgentControlView struct {
	operations.Control
	State           string      `json:"state"`
	EffectivePaused bool        `json:"effective_paused"`
	ManagerName     string      `json:"manager_name,omitempty"`
	LastRun         string      `json:"last_run,omitempty"`
	LastRunAt       time.Time   `json:"last_run_at,omitzero"`
	LastRunStatus   runs.Status `json:"last_run_status,omitempty"`
}

// AgentState describes only work this reader can see, never hidden activity.
func (p *Platform) AgentState(who auth.Principal, agent string) AgentControlView {
	c := p.AgentControl(who.Tenant, agent)
	view := AgentControlView{Control: c, State: "ready", EffectivePaused: p.Operations.Paused(who.Tenant, legacy.BuiltinAgent(agent)) || !p.agentReady(who.Tenant, agent), ManagerName: p.NameOf(who.Tenant, c.Manager)}
	if view.EffectivePaused {
		view.State = "paused"
	}
	if agent == "" || !who.Can(auth.RunsRead) {
		return view
	}
	now := p.now()
	latest := p.Runs.AgentWindow(who.Tenant, agent, now.Add(-31*24*time.Hour), now, 1, p.Seer(who).Run)
	if len(latest) > 0 {
		r := latest[0]
		view.LastRun, view.LastRunAt, view.LastRunStatus = r.ID, r.CreatedAt, r.Status
		if !view.EffectivePaused {
			switch r.Status {
			case runs.Parked:
				view.State = "waiting_on_people"
			case runs.Running, runs.Accepted:
				view.State = "working"
			case runs.Failed:
				view.State = "failed"
			}
		}
	}
	if view.State == "ready" {
		for _, a := range p.Assignments(who, operations.MaxResults) {
			if a.Agent == agent && a.State == operations.Watching {
				view.State = "watching"
				break
			}
		}
	}
	return view
}

func operationMetaOf(meta map[string]string) map[string]string {
	out := map[string]string{}
	for _, key := range []string{operations.MetaForceAsk, operations.MetaTools, metaAssignment, metaAssignmentSource} {
		if value, ok := meta[key]; ok {
			out[key] = value
		}
	}
	return out
}
func (p *Platform) operationForced(req runtime.Request) bool {
	if req.Meta[operations.MetaForceAsk] == "true" {
		return true
	}
	if run, err := p.Runs.Get(req.Tenant, req.RunID); err == nil {
		return run.Tag().Meta[operations.MetaForceAsk] == "true"
	}
	if v, ok := p.admitting.Load(req.RunID); ok && v.(admitted).tenant == req.Tenant {
		return v.(admitted).operationMeta[operations.MetaForceAsk] == "true"
	}
	return false
}
