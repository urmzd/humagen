package platform_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/urmzd/fiber/pkg/agentrt"
	"github.com/urmzd/fiber/pkg/agentspec"
	"github.com/urmzd/fiber/pkg/registry"
	"github.com/urmzd/fiber/pkg/runtime"

	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/call"
	"github.com/urmzd/warp/pkg/projects"
	"github.com/urmzd/warp/pkg/runs"
)

// as is a tool request from a run of one agent.
func as(run runs.Run, agent string) runtime.Request {
	return runtime.Request{RunID: run.ID, Tenant: tenant, Revision: agentrt.Revision{Spec: agentrt.Spec{Name: agent}, Rev: 1}}
}

// The analyst and the builder exist once a model is given, stay at their first
// revision when nothing changed, and the builder's saves wait for a manager.
func TestBuiltinAnalystAndBuilder(t *testing.T) {
	ctx := context.Background()
	p, err := platform.New(platform.Config{Workspace: t.TempDir(), BuiltinProvider: "offline", BuiltinModel: "offline"})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := p.EnsureBuiltins(ctx, tenant); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{platform.BuiltinWriter, platform.BuiltinAnalyst, platform.BuiltinBuilder} {
		rev, err := p.Agents.Get(ctx, tenant, name, 0)
		if err != nil || rev.Rev != 1 {
			t.Fatalf("%s = rev %d, %v", name, rev.Rev, err)
		}
	}
	analyst, _ := p.Agents.Get(ctx, tenant, platform.BuiltinAnalyst, 0)
	for _, tool := range append(platform.AnalystTools(), platform.ToolPublish) {
		if !slices.Contains(analyst.Spec.Tools, tool) {
			t.Fatalf("the analyst lacks %s", tool)
		}
	}
	builder, _ := p.Agents.Get(ctx, tenant, platform.BuiltinBuilder, 0)
	if !slices.Equal(builder.Spec.Approvals, []string{platform.ToolSaveAgent}) || !slices.Contains(builder.Spec.Approvers, "role:manager") {
		t.Fatalf("the builder saves without approval: %+v", builder.Spec)
	}
}

func TestRunsSummaryTool(t *testing.T) {
	p := start(t, call.Limits{})
	run, _ := turn(t, p, "api-1", "")
	req := as(run, platform.BuiltinAnalyst)
	out, err := use(t, p, req, platform.ToolRunsSummary, map[string]any{"hours": "24"})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Total struct {
			Runs      int `json:"runs"`
			Completed int `json:"completed"`
		} `json:"total"`
		ByAgent []struct {
			Name string `json:"name"`
		} `json:"by_agent"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if got.Total.Runs < 1 || got.Total.Completed < 1 || len(got.ByAgent) == 0 || got.ByAgent[0].Name != "lead" {
		t.Fatalf("summary = %s", out)
	}
	if out, _ := use(t, p, req, platform.ToolRunsSummary, map[string]any{"agent": "nobody"}); !strings.Contains(out, "No runs of nobody") {
		t.Fatalf("an agent with no runs = %s", out)
	}
	for _, bad := range []string{"0", "-3", "721", "a day"} {
		if _, err := use(t, p, req, platform.ToolRunsSummary, map[string]any{"hours": bad}); err == nil {
			t.Fatalf("hours %q was taken", bad)
		}
	}
	if out, err := use(t, p, req, platform.ToolListAlerts, nil); err != nil || !strings.Contains(out, `"rules"`) {
		t.Fatalf("list_alerts = %s, %v", out, err)
	}
}

func TestSaveAgentTool(t *testing.T) {
	p := startWith(t, platform.Config{BuiltinProvider: "offline", BuiltinModel: "offline"})
	ctx := t.Context()
	if err := p.EnsureBuiltins(ctx, tenant); err != nil {
		t.Fatal(err)
	}
	run, _ := turn(t, p, "api-1", "")
	req := as(run, platform.BuiltinBuilder)
	save := func(r runtime.Request, args map[string]any) (string, error) {
		t.Helper()
		out, _, err := p.UseTool(ctx, r, platform.ToolSaveAgent, args)
		return out, err
	}

	out, err := save(req, map[string]any{"name": "notes", "description": "Writes release notes", "system_prompt": "Write notes.", "tools": "file_write, publish_artifact"})
	if err != nil || !strings.Contains(out, "Created notes") {
		t.Fatalf("create = %q, %v", out, err)
	}
	rev, err := p.Agents.Get(ctx, tenant, "notes", 0)
	if err != nil || rev.CreatedBy != "agent:"+platform.BuiltinBuilder || rev.Spec.Provider != "offline" ||
		!slices.Equal(rev.Spec.Tools, []string{platform.ToolFileWrite, platform.ToolPublish}) {
		t.Fatalf("created = %+v by %q, %v", rev.Spec, rev.CreatedBy, err)
	}

	// A revision changes what was asked and keeps what a person set elsewhere.
	spec := rev.Spec
	spec.Access.Spawn = []string{"helper"}
	if _, err := p.Agents.Revise(ctx, tenant, spec, rev.Rev, "ada"); err != nil {
		t.Fatal(err)
	}
	if out, err := save(req, map[string]any{"name": "notes", "description": "Notes", "system_prompt": "Write shorter notes.", "tools": "file_read"}); err != nil || !strings.Contains(out, "Revised notes") {
		t.Fatalf("revise = %q, %v", out, err)
	}
	rev, _ = p.Agents.Get(ctx, tenant, "notes", 0)
	if rev.Spec.SystemPrompt != "Write shorter notes." || !slices.Equal(rev.Spec.Tools, []string{platform.ToolFileRead}) || !slices.Equal(rev.Spec.Access.Spawn, []string{"helper"}) {
		t.Fatalf("revised = %+v", rev.Spec)
	}

	for name, args := range map[string]map[string]any{
		"a built-in":         {"name": platform.BuiltinWriter, "description": "x", "system_prompt": "x"},
		"its own tool":       {"name": "sneaky", "description": "x", "system_prompt": "x", "tools": platform.ToolSaveAgent},
		"the analyst's tool": {"name": "sneaky", "description": "x", "system_prompt": "x", "tools": platform.ToolRunsSummary},
		"no prompt":          {"name": "empty", "description": "x"},
	} {
		if _, err := save(req, args); !errors.Is(err, platform.ErrBuilder) {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
	// Only the builder saves agents, whoever else is granted the tool.
	if _, err := save(as(run, "lead"), map[string]any{"name": "x", "description": "x", "system_prompt": "x"}); !errors.Is(err, platform.ErrBuilder) {
		t.Fatalf("another agent saved one: %v", err)
	}
	if out, err := use(t, p, req, platform.ToolListAgents, nil); err != nil || !strings.Contains(out, `"name":"notes"`) || !strings.Contains(out, `"builtin":true`) {
		t.Fatalf("list_agents = %s, %v", out, err)
	}
	if out, err := use(t, p, req, platform.ToolReadAgent, map[string]any{"name": "notes"}); err != nil || !strings.Contains(out, "Write shorter notes.") {
		t.Fatalf("read_agent = %s, %v", out, err)
	}
}

// An agent that lives in a project is its home's: the builder reads one in a
// members-only project only from inside it, and revises no agent that lives
// anywhere, as its save is approved by a manager who need not own the home.
// A new agent's name is held to what the agents route holds it to.
func TestTheBuilderNeverReadsOrRevisesAnAgentOutsideItsReach(t *testing.T) {
	p := startWith(t, platform.Config{BuiltinProvider: "offline", BuiltinModel: "offline"})
	ctx := t.Context()
	if err := p.EnsureBuiltins(ctx, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err := p.CreateProject(ctx, tenant, projects.Writer{By: "olu"}, platform.ProjectInput{Name: "Vault", Kind: projects.Account}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Agents.Create(ctx, tenant, agentspec.AgentSpec{Name: "spy", Model: "offline", Provider: "offline", SystemPrompt: "SECRET-PROMPT"}, "olu"); err != nil {
		t.Fatal(err)
	}
	if err := p.SetAgentHome(ctx, tenant, "spy", "Vault", projects.Writer{By: "olu"}); err != nil {
		t.Fatal(err)
	}
	outside, _ := turn(t, p, "api-root", "")
	inside, _ := turn(t, p, "api-vault", "Vault")
	req := as(outside, platform.BuiltinBuilder)
	if out, err := use(t, p, req, platform.ToolListAgents, nil); err != nil || strings.Contains(out, "spy") || !strings.Contains(out, "lead") {
		t.Fatalf("list_agents from outside = %s, %v", out, err)
	}
	if out, err := use(t, p, req, platform.ToolReadAgent, map[string]any{"name": "spy"}); !errors.Is(err, registry.ErrNotFound) || strings.Contains(out, "SECRET") {
		t.Fatalf("read_agent from outside = %q, %v", out, err)
	}
	if out, err := use(t, p, as(inside, platform.BuiltinBuilder), platform.ToolReadAgent, map[string]any{"name": "spy"}); err != nil || !strings.Contains(out, "SECRET-PROMPT") {
		t.Fatalf("read_agent from inside Vault = %q, %v", out, err)
	}
	for _, r := range []runtime.Request{req, as(inside, platform.BuiltinBuilder)} {
		if _, _, err := p.UseTool(ctx, r, platform.ToolSaveAgent, map[string]any{"name": "spy", "description": "x", "system_prompt": "HIJACKED"}); !errors.Is(err, platform.ErrBuilder) {
			t.Fatalf("revising an agent that lives in a project = %v", err)
		}
	}
	if rev, _ := p.Agents.Get(ctx, tenant, "spy", 0); rev.Spec.SystemPrompt != "SECRET-PROMPT" {
		t.Fatalf("the builder revised it: %+v", rev.Spec)
	}
	if _, _, err := p.UseTool(ctx, req, platform.ToolSaveAgent, map[string]any{"name": "sme", "description": "x", "system_prompt": "x"}); !errors.Is(err, platform.ErrNameTaken) {
		t.Fatalf("creating an agent with a reserved name = %v", err)
	}
}
