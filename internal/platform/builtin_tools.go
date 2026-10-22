package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/urmzd/fiber/pkg/agentspec"
	"github.com/urmzd/fiber/pkg/registry"
	"github.com/urmzd/fiber/pkg/runtime"
	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/warp/pkg/runs"
)

// The tools behind the platform's analyst and builder.
//
// The analyst's tools read: what ran, how it ended, what it cost, and what
// the alert rules are watching. They read only the tenant the run is in, and
// any agent a person grants them to may use them.
//
// The builder's tools change the workspace's agents, so they are held
// tighter: save_agent works only for the builder itself, every call waits for
// a manager or an admin to approve it, and what it may grant is a short list
// that does not include itself.
const (
	ToolRunsSummary = "runs_summary"
	ToolListAlerts  = "list_alerts"
	ToolListAgents  = "list_agents"
	ToolReadAgent   = "read_agent"
	ToolSaveAgent   = "save_agent"
)

// AnalystTools are the read-only tools about how the workspace is doing.
func AnalystTools() []string { return []string{ToolRunsSummary, ToolListAlerts, ToolProjectChanges} }

// BuilderTools are the tools for reading and changing agents.
func BuilderTools() []string { return []string{ToolListAgents, ToolReadAgent, ToolSaveAgent} }

// toolAskPerson is the runtime's tool for asking a person something.
const toolAskPerson = "ask_person"

// grantable is what the builder may give an agent it writes: files and
// asking a person. Never its own tools, the analyst's, or calls to agents.
func grantable() []string { return append(FileTools(), toolAskPerson) }

// MaxSummaryHours bounds how far back runs_summary looks.
const MaxSummaryHours = 30 * 24

// ErrBuilder reports a change the builder may not make.
var ErrBuilder = errors.New("platform: builder")

func (p *Platform) builtinTools(req runtime.Request) map[string]saigetypes.Tool {
	tool := func(name, desc string, params saigetypes.ParameterSchema, fn func(ctx context.Context, args map[string]any) (string, error)) saigetypes.Tool {
		return &saigetypes.ToolFunc{Def: saigetypes.ToolDef{Name: name, Description: desc, Parameters: params}, Fn: fn}
	}
	return map[string]saigetypes.Tool{
		ToolRunsSummary: tool(ToolRunsSummary,
			"Summarize the workspace's runs over the last hours: how many ran, completed, failed and were stopped, durations (p50 and p95), tokens and spend, by agent and by origin, and the slowest runs. Use it before answering any question about how agents are doing.",
			schema(nil, map[string]string{
				"hours":   fmt.Sprintf("How many hours back to look, 1 to %d. Defaults to 24", MaxSummaryHours),
				"agent":   "Only this agent's runs. Leave out for every agent",
				"project": "Only the runs filed in this project and the projects below it. Leave out for the whole workspace. Members-only projects other than this conversation's, and private threads, are never read",
			}),
			func(_ context.Context, args map[string]any) (string, error) {
				hours := 24
				if h := strings.TrimSpace(arg(args, "hours")); h != "" {
					n, err := strconv.Atoi(h)
					if err != nil || n < 1 || n > MaxSummaryHours {
						return "", fmt.Errorf("hours must be a whole number from 1 to %d", MaxSummaryHours)
					}
					hours = n
				}
				scope, err := p.summaryScope(req.Tenant, p.ownProject(req.Tenant, req.RunID), strings.TrimSpace(arg(args, "project")))
				if err != nil {
					return "", err
				}
				return p.runsSummary(req.Tenant, time.Duration(hours)*time.Hour, strings.TrimSpace(arg(args, "agent")), scope)
			}),
		ToolProjectChanges: p.projectChangesTool(req),
		ToolListAlerts: tool(ToolListAlerts,
			"List the alert rules the workspace has, what each watches for, and the alerts that are still open.",
			schema(nil, nil),
			func(context.Context, map[string]any) (string, error) {
				return p.alertsReport(req.Tenant, p.ownProject(req.Tenant, req.RunID))
			}),
		ToolListAgents: tool(ToolListAgents,
			"List the workspace's agents: name, description, model, tools and revision.",
			schema(nil, nil),
			func(ctx context.Context, _ map[string]any) (string, error) {
				return p.agentsReport(ctx, req.Tenant, p.ownProject(req.Tenant, req.RunID))
			}),
		ToolReadAgent: tool(ToolReadAgent,
			"Read one agent's latest revision in full, system prompt included.",
			schema([]string{"name"}, map[string]string{"name": "The agent's name"}),
			func(ctx context.Context, args map[string]any) (string, error) {
				name := strings.TrimSpace(arg(args, "name"))
				if !p.agentOpenFor(req.Tenant, p.ownProject(req.Tenant, req.RunID))(name) {
					return "", fmt.Errorf("%w: %q", registry.ErrNotFound, name)
				}
				rev, err := p.Agents.Get(ctx, req.Tenant, name, 0)
				if err != nil {
					return "", err
				}
				b, _ := json.MarshalIndent(map[string]any{"rev": rev.Rev, "spec": rev.Spec}, "", "  ")
				return string(b), nil
			}),
		ToolSaveAgent: tool(ToolSaveAgent,
			"Create an agent, or revise one, after a person approves. Give the whole system prompt and the whole list of tools each time. Only "+strings.Join(grantable(), ", ")+" may be granted.",
			schema([]string{"name", "description", "system_prompt"}, map[string]string{
				"name":          "Lowercase letters, digits and dashes, such as release-notes",
				"description":   "One line on what the agent does, shown wherever it can be picked",
				"system_prompt": "The agent's instructions, in full",
				"tools":         "Comma-separated tools to grant, from: " + strings.Join(grantable(), ", ") + ". Leave out for none",
			}),
			func(ctx context.Context, args map[string]any) (string, error) {
				if req.Agent() != BuiltinBuilder {
					return "", fmt.Errorf("%w: only %s saves agents", ErrBuilder, BuiltinBuilder)
				}
				rev, created, err := p.saveAgent(ctx, req.Tenant, args)
				if err != nil {
					return "", err
				}
				verb := "Revised"
				if created {
					verb = "Created"
				}
				return fmt.Sprintf("%s %s, now at revision %d.", verb, rev.Spec.Name, rev.Rev), nil
			}),
	}
}

// runsSummary is runs_summary's answer: compact JSON a model can read and quote.
func (p *Platform) runsSummary(tenant string, window time.Duration, agent string, scope runs.SummaryQuery) (string, error) {
	until := p.now()
	bucket := time.Hour
	if window > 7*24*time.Hour {
		bucket = 24 * time.Hour
	}
	scope.Since, scope.Until, scope.Bucket = until.Add(-window), until, bucket
	s, err := p.Runs.SummarizeWhere(tenant, scope)
	if err != nil {
		return "", err
	}
	type group struct {
		Name        string  `json:"name"`
		Runs        int     `json:"runs"`
		Completed   int     `json:"completed"`
		Failed      int     `json:"failed"`
		Stopped     int     `json:"stopped"`
		Active      int     `json:"active"`
		FailureRate float64 `json:"failure_rate_pct"`
		P50         int64   `json:"p50_ms"`
		P95         int64   `json:"p95_ms"`
		Tokens      int64   `json:"tokens"`
		SpendUSD    float64 `json:"spend_usd"`
	}
	of := func(g runs.Group) group {
		rate := 0.0
		if ended := g.Completed + g.Failed; ended > 0 {
			rate = float64(g.Failed*1000/ended) / 10
		}
		return group{
			Name: g.Name, Runs: g.Runs, Completed: g.Completed, Failed: g.Failed, Stopped: g.Stopped, Active: g.Active,
			FailureRate: rate, P50: g.P50, P95: g.P95, Tokens: g.Usage.Tokens, SpendUSD: float64(g.Usage.Spend) / 1_000_000,
		}
	}
	out := struct {
		Since   time.Time `json:"since"`
		Until   time.Time `json:"until"`
		Total   group     `json:"total"`
		ByAgent []group   `json:"by_agent"`
		Origins []group   `json:"by_origin"`
		Slowest []struct {
			ID     string `json:"id"`
			Agent  string `json:"agent"`
			Status string `json:"status"`
			Ms     int64  `json:"duration_ms"`
		} `json:"slowest"`
		Note string `json:"note,omitempty"`
	}{Since: s.Since, Until: s.Until, Total: of(s.Total)}
	for _, g := range s.ByAgent {
		if agent == "" || g.Name == agent {
			out.ByAgent = append(out.ByAgent, of(g))
		}
	}
	if agent != "" {
		if len(out.ByAgent) == 0 {
			out.Note = "No runs of " + agent + " in this window."
		} else {
			out.Total = out.ByAgent[0]
		}
	} else {
		for _, g := range s.ByOrigin {
			out.Origins = append(out.Origins, of(g))
		}
	}
	sort.SliceStable(out.ByAgent, func(i, j int) bool { return out.ByAgent[i].Runs > out.ByAgent[j].Runs })
	out.ByAgent = out.ByAgent[:min(len(out.ByAgent), 20)]
	for _, r := range s.Slowest {
		if agent != "" && r.Agent != agent {
			continue
		}
		if len(out.Slowest) == 5 {
			break
		}
		out.Slowest = append(out.Slowest, struct {
			ID     string `json:"id"`
			Agent  string `json:"agent"`
			Status string `json:"status"`
			Ms     int64  `json:"duration_ms"`
		}{r.ID, r.Agent, string(r.Status), r.UpdatedAt.Sub(r.CreatedAt).Milliseconds()})
	}
	b, err := json.Marshal(out)
	return string(b), err
}

// alertsReport is list_alerts' answer for a run filed in own: the rules, then
// the open alerts, newest first. What is read is what the answer may be read
// for by everyone it reaches (openFor): a rule or an alert of a members-only
// project other than own, of an agent that lives in one, or of a run in a
// private thread, is left out.
func (p *Platform) alertsReport(tenant, own string) (string, error) {
	type rule struct {
		Name, Kind, Severity, Description, Agent, Project string
		Paused                                            bool `json:",omitempty"`
	}
	type alert struct {
		ID, Rule, Severity, Title, Agent string
		At                               time.Time
	}
	out := struct {
		Rules []rule  `json:"rules"`
		Open  []alert `json:"open_alerts"`
	}{Rules: []rule{}, Open: []alert{}}
	open, tree := p.openFor(tenant, own), p.ProjectTree(tenant)
	agentOpen := func(name string) bool {
		home, homed := tree.Home(name)
		return name == "" || !homed || open(home)
	}
	for _, r := range p.Alerts.Rules(tenant) {
		if open(r.Project) && agentOpen(r.Agent) {
			out.Rules = append(out.Rules, rule{r.Name, string(r.Kind), string(r.Severity), r.Description, r.Agent, r.Project, r.Paused})
		}
	}
	all := p.Alerts.Alerts(tenant)
	for i := len(all) - 1; i >= 0 && len(out.Open) < 50; i-- {
		a := all[i]
		if !a.Open() || !open(a.Project) || !agentOpen(a.Agent) {
			continue
		}
		if at, ok := p.placeOfRunID(tenant, a.RunID); a.RunID != "" && ok && (!open(at.Project) || p.privateThread(tenant, at.Thread)) {
			continue
		}
		out.Open = append(out.Open, alert{a.ID, a.Rule, string(a.Severity), a.Title, a.Agent, a.At})
	}
	b, err := json.Marshal(out)
	return string(b), err
}

// agentOpenFor reports, for a tool of a run filed in own, whether an agent
// may be read for everyone its answer reaches: a platform agent, or one that
// lives in a project openFor allows. One living in a members-only project
// other than own reads as one that is not there.
func (p *Platform) agentOpenFor(tenant, own string) func(name string) bool {
	open, tree := p.openFor(tenant, own), p.ProjectTree(tenant)
	return func(name string) bool {
		home, homed := tree.Home(agentspec.NormalizeName(name))
		return !homed || open(home)
	}
}

// agentsReport is list_agents' answer for a run filed in own, bounded to the
// first 200 agents it may read (agentOpenFor).
func (p *Platform) agentsReport(ctx context.Context, tenant, own string) (string, error) {
	type agent struct {
		Name        string   `json:"name"`
		Description string   `json:"description,omitempty"`
		Model       string   `json:"model"`
		Tools       []string `json:"tools,omitempty"`
		Rev         int      `json:"rev"`
		Builtin     bool     `json:"builtin,omitempty"`
	}
	out := []agent{}
	open := p.agentOpenFor(tenant, own)
	page := registry.Page{Size: 100}
	for len(out) < 200 {
		revs, next, err := p.Agents.List(ctx, tenant, page)
		if err != nil {
			return "", err
		}
		for _, r := range revs {
			if !open(r.Spec.Name) || len(out) >= 200 {
				continue
			}
			out = append(out, agent{r.Spec.Name, r.Spec.Description, r.Spec.Provider + "/" + r.Spec.Model, r.Spec.Tools, r.Rev, IsBuiltin(r.Spec.Name)})
		}
		if next == "" {
			break
		}
		page.Token = next
	}
	b, err := json.Marshal(out)
	return string(b), err
}

// saveAgent creates or revises an agent from save_agent's arguments. A
// revision changes only the description, the prompt and the grantable
// tools: what a person set elsewhere (model, calls, approvals of other
// tools, sources) stays as it was. An agent that lives in a project is its
// home's owners' to change, in its own routes, so the builder, whose save a
// manager approves without owning anything, never revises one; and a new
// agent's name is held to what the agents route holds it to.
func (p *Platform) saveAgent(ctx context.Context, tenant string, args map[string]any) (agentspec.Revision, bool, error) {
	name := strings.TrimSpace(arg(args, "name"))
	if IsBuiltin(name) {
		return agentspec.Revision{}, false, fmt.Errorf("%w: %s belongs to the platform", ErrBuilder, name)
	}
	if _, homed := p.ProjectTree(tenant).Home(agentspec.NormalizeName(name)); homed {
		return agentspec.Revision{}, false, fmt.Errorf("%w: %s lives in a project, so only that project's owners change it", ErrBuilder, name)
	}
	var tools []string
	for t := range strings.SplitSeq(arg(args, "tools"), ",") {
		t = strings.TrimSpace(t)
		if t == "" || slices.Contains(tools, t) {
			continue
		}
		if !slices.Contains(grantable(), t) {
			return agentspec.Revision{}, false, fmt.Errorf("%w: %q cannot be granted; choose from %s", ErrBuilder, t, strings.Join(grantable(), ", "))
		}
		tools = append(tools, t)
	}
	description := strings.TrimSpace(arg(args, "description"))
	prompt := strings.TrimSpace(arg(args, "system_prompt"))
	if prompt == "" {
		return agentspec.Revision{}, false, fmt.Errorf("%w: a system prompt is required", ErrBuilder)
	}

	have, err := p.Agents.Get(ctx, tenant, name, 0)
	switch {
	case errors.Is(err, registry.ErrNotFound):
		if err := p.CheckAgentName(tenant, name); err != nil {
			return agentspec.Revision{}, false, fmt.Errorf("%w: %w", ErrBuilder, err)
		}
		spec := agentspec.AgentSpec{
			Name: name, Description: description, SystemPrompt: prompt, Tools: tools,
			Provider: p.cfg.BuiltinProvider, Model: p.cfg.BuiltinModel,
		}
		if err := p.CheckAgentModel(tenant, spec); err != nil {
			return agentspec.Revision{}, false, err
		}
		rev, err := p.Agents.Create(ctx, tenant, spec, "agent:"+BuiltinBuilder)
		return rev, err == nil, err
	case err != nil:
		return agentspec.Revision{}, false, err
	}
	spec := have.Spec
	spec.Description, spec.SystemPrompt = description, prompt
	// Tools the builder cannot grant stay; the grantable ones are what was asked.
	kept := slices.DeleteFunc(slices.Clone(spec.Tools), func(t string) bool { return slices.Contains(grantable(), t) })
	spec.Tools = append(kept, tools...)
	// An approval of a tool the agent no longer holds would stop it from starting.
	spec.Approvals = slices.DeleteFunc(slices.Clone(spec.Approvals), func(t string) bool {
		return slices.Contains(grantable(), t) && !slices.Contains(tools, t)
	})
	if len(spec.Approvals) == 0 {
		spec.Approvers, spec.SelfApproval = nil, false
	}
	rev, err := p.Agents.Revise(ctx, tenant, spec, have.Rev, "agent:"+BuiltinBuilder)
	return rev, false, err
}
