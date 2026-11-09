package platform

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/orchagent/flow/event"
	"github.com/urmzd/orchagent/flow/runtime"
	"github.com/urmzd/orchagent/flow/turn/native"
	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/humagen/pkg/operations"
)

// namedCategories is the category each tool had while a tool's category was
// read from its name. The registry declares the same one for each: moving
// the declaration into the registry changed no tool's category.
var namedCategories = map[string]operations.Category{
	"add_recorded_eval_case": operations.Other, "agent": operations.CallAgents, "alert_create": operations.Other, "alert_resolve": operations.Other,
	"apply_agent_change": operations.Other, "apply_workspace_prompt": operations.Other, "ask_person": operations.Other, "call_agent": operations.CallAgents,
	"catalog_query": operations.ReadContext, "catalog_search": operations.ReadContext, "connect_connection": operations.Other, "corpus_query": operations.ReadContext,
	"corpus_graph": operations.ReadContext, "corpus_search": operations.ReadContext,
	"delegate_task": operations.CallAgents, "file_list": operations.ReadContext, "file_read": operations.ReadContext, "file_write": operations.WriteFiles,
	"get_context": operations.ReadContext, "github_add_labels": operations.ExternalMessages, "github_apply_changes": operations.ExternalMessages,
	"github_comment": operations.ExternalMessages, "github_create_fix_pr": operations.ExternalMessages, "github_create_review": operations.ExternalMessages,
	"github_fix_base": operations.ReadContext, "github_read_context": operations.ReadContext, "github_read_file": operations.ReadContext,
	"github_request_review": operations.ExternalMessages, "github_run_checks": operations.Other, "github_workflow_status": operations.ReadContext,
	"list_agents": operations.ReadContext, "list_alerts": operations.ReadContext, "list_connections": operations.Other, "memory_search": operations.Other,
	"pause_agent": operations.Other, "preview_agent_change": operations.Other, "preview_workspace_prompt": operations.Other, "project_changes": operations.Other,
	"publish_artifact": operations.PublishArtifacts, "read_agent": operations.ReadContext, "read_agent_evals": operations.Other, "record_outcome": operations.Other,
	"retire_agent": operations.Other, "run_eval_suite": operations.Other, "runs_summary": operations.ReadContext, "save_agent": operations.WriteFiles,
	"session_call": operations.Other, "session_link": operations.Other, "session_listen": operations.Other, "session_read": operations.Other,
	"session_status": operations.Other, "set_workspace_prompt": operations.Other, "shell": operations.WriteFiles, "slack_add_reaction": operations.ExternalMessages,
	"slack_post_message": operations.ExternalMessages, "source_search": operations.ReadContext, "tag_writer": operations.CallAgents, "write_eval_suite": operations.Other,
}

func registryPlatform(t *testing.T) *Platform {
	t.Helper()
	p, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// Every tool a run may be offered states its category in the registry, and
// it is the category the tool had: none is left to be read from a name.
func TestEveryRunToolDeclaresTheCategoryItHad(t *testing.T) {
	p := registryPlatform(t)
	for name, want := range namedCategories {
		if got := p.categoryOf(name); got != want {
			t.Errorf("%s is %s, was %s", name, got, want)
		}
	}
	all, err := p.SystemTools()
	if err != nil {
		t.Fatal(err)
	}
	for _, tl := range all {
		if tl.Category == "" {
			t.Errorf("%s states no category", tl.key())
		}
		if !tl.Exposure.Runs() {
			continue
		}
		if _, pinned := namedCategories[tl.Name]; !pinned {
			t.Errorf("%s is offered to runs and its category is not pinned here", tl.Name)
		}
		if tl.Approval.Mode == ApprovalNone {
			t.Errorf("%s is offered to runs and no policy holds it", tl.Name)
		}
	}
}

// A tool's declared category wins over anything its name says, and only a
// tool the registry does not hold is held by its name.
func TestOnlyAToolOutsideTheRegistryIsHeldByItsName(t *testing.T) {
	p := registryPlatform(t)
	build := func(*ToolTurn) (saigetypes.Tool, error) { return &saigetypes.ToolFunc{}, nil }
	if err := p.RegisterSystemTool(SystemTool{Name: "send_nothing", Category: operations.ReadContext, Exposure: ExposeRuns, Run: build}); err != nil {
		t.Fatal(err)
	}
	if got := p.categoryOf("send_nothing"); got != operations.ReadContext {
		t.Errorf("a registered tool was held by its name: %s", got)
	}
	for name, want := range map[string]operations.Category{
		"crm__send_email": operations.ExternalMessages, "mail_reply": operations.ExternalMessages, "crm__lookup": operations.Other,
		"tag_reviewer": operations.CallAgents,
	} {
		if _, registered := p.runTool(name); registered {
			t.Fatalf("%s is in the registry", name)
		}
		if got := p.categoryOf(name); got != want {
			t.Errorf("%s is %s, want %s", name, got, want)
		}
	}
}

// The registry refuses a tool that does not declare itself: who it is
// offered to, its category, and what calls or builds it. A tool offered to
// runs is always held by the action policy.
func TestAToolDeclaresItselfToBeRegistered(t *testing.T) {
	p := registryPlatform(t)
	call := func(context.Context, SystemCall, json.RawMessage) (SystemResult, error) { return SystemResult{}, nil }
	build := func(*ToolTurn) (saigetypes.Tool, error) { return &saigetypes.ToolFunc{}, nil }
	input := map[string]any{"type": "object"}
	for why, tl := range map[string]SystemTool{
		"no name":                 {Category: operations.Other, Exposure: ExposeMCP, Input: input, Call: call},
		"no exposure":             {Name: "a", Category: operations.Other, Input: input, Call: call},
		"no category":             {Name: "a", Exposure: ExposeMCP, Input: input, Call: call},
		"MCP without a call":      {Name: "a", Category: operations.Other, Exposure: ExposeMCP, Input: input},
		"MCP without an input":    {Name: "a", Category: operations.Other, Exposure: ExposeMCP, Call: call},
		"runs without a builder":  {Name: "a", Category: operations.Other, Exposure: ExposeRuns},
		"runs held by no policy":  {Name: "a", Category: operations.Other, Exposure: ExposeRuns, Run: build, Approval: ToolApproval{Mode: ApprovalNone}},
		"a run tool's name again": {Name: ToolFileRead, Category: operations.ReadContext, Exposure: ExposeRuns, Run: build},
		"a run tool's key":        {Name: ToolFileRead, Category: operations.ReadContext, Exposure: ExposeMCP, Input: input, Call: call},
		"an operation's name":     {Name: "list_schedules", Category: operations.ReadContext, Exposure: ExposeRuns, Run: build},
	} {
		if err := p.RegisterSystemTool(tl); err == nil {
			t.Errorf("registered a tool with %s", why)
		}
	}
}

// Three names are both an operation of the API contract and a run tool,
// with different implementations. Each has its own registry key, a run is
// offered the run tool and an MCP client the operation, and neither is
// reached through the other's name.
func TestANameSharedAcrossExposuresHasAKeyOfItsOwn(t *testing.T) {
	p := registryPlatform(t)
	for _, name := range []string{ToolListAgents, ToolListAlerts, ToolRunEval} {
		operation, ok := p.SystemTool(name)
		if !ok || operation.Exposure != ExposeMCP || operation.Call == nil || operation.Run != nil {
			t.Errorf("SystemTool(%q) is not the API operation: %+v", name, operation)
		}
		own, ok := p.SystemTool(runToolKey(name))
		if !ok || own.Name != name || own.Exposure != ExposeRuns || own.Run == nil || own.Call != nil {
			t.Errorf("SystemTool(%q) is not the run tool: %+v", runToolKey(name), own)
		}
		offered, ok := p.runTool(name)
		if !ok || offered.key() != runToolKey(name) {
			t.Errorf("a run is offered %q as %+v", name, offered)
		}
	}
	// No other name is in the registry twice.
	all, err := p.SystemTools()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for _, tl := range all {
		seen[tl.Name]++
	}
	for name, n := range seen {
		if n > 1 && !slices.Contains([]string{ToolListAgents, ToolListAlerts, ToolRunEval}, name) {
			t.Errorf("%s is in the registry %d times", name, n)
		}
	}
}

// A turn's tools are built from the registry: every tool the registry
// offers runs and builds resolves under its own name, the agent tool and
// get_context are the registry's one definition of each, and a name the
// registry lacks is no tool.
func TestATurnsToolsAreBuiltFromTheRegistry(t *testing.T) {
	p := registryPlatform(t)
	req := runtime.Request{Tenant: "acme", RunID: "r1", Definition: native.Revision{Spec: native.Spec{Name: "lead"}}}
	tools, err := p.registryTools()(req, func(event.Action) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	all, err := p.SystemTools()
	if err != nil {
		t.Fatal(err)
	}
	connectors := map[string]bool{}
	for _, action := range connectorActions() {
		connectors[action.Name] = true
	}
	built := 0
	for _, tl := range all {
		if !tl.Exposure.Runs() || tl.Upstream {
			continue
		}
		got, err := tools.Lookup(tl.Name)
		if connectors[tl.Name] {
			// No client is seated, so the turn has no connector actions.
			if !errors.Is(err, native.ErrNoSuchTool) {
				t.Errorf("%s = %v, want no such tool without a client", tl.Name, err)
			}
			continue
		}
		if err != nil || got.Definition().Name != tl.Name || got.Definition().Description == "" {
			t.Errorf("%s was not built: %v", tl.Name, err)
			continue
		}
		if again, _ := tools.Lookup(tl.Name); again != got {
			t.Errorf("%s was built twice in one turn", tl.Name)
		}
		built++
	}
	if built != 37 {
		t.Errorf("built %d tools, want 37", built)
	}
	for _, name := range []string{"no_such_tool", "list_schedules", "session_call"} {
		if _, err := tools.Lookup(name); !errors.Is(err, native.ErrNoSuchTool) {
			t.Errorf("%s = %v, want no such tool", name, err)
		}
	}
	// One definition each: what a run is shown is the registry's entry.
	for _, name := range []string{ToolAgent, ToolGetContext} {
		entry, _ := p.SystemTool(name)
		got, err := tools.Lookup(name)
		if err != nil {
			t.Fatal(err)
		}
		def := got.Definition()
		if !strings.HasPrefix(def.Description, entry.Description) {
			t.Errorf("%s is described to a run apart from the registry: %q", name, def.Description)
		}
		properties, _ := entry.Input["properties"].(map[string]any)
		if len(def.Parameters.Properties) != len(properties) || !slices.Equal(def.Parameters.Required, entry.Input["required"].([]string)) {
			t.Errorf("%s takes other arguments in a run than the registry declares: %+v", name, def.Parameters)
		}
		for arg, raw := range properties {
			if name == ToolAgent && arg == "agent" {
				// Narrowed to the agents this caller may call.
				continue
			}
			if want, _ := raw.(map[string]any)["description"].(string); def.Parameters.Properties[arg].Description != want {
				t.Errorf("%s.%s is described to a run apart from the registry", name, arg)
			}
		}
	}
}

// The tools that carry their own approvers ask exactly who they asked
// before the registry held the declaration: the approvers, whether the
// person asking may answer, how long the question waits, and its wording.
func TestEachToolsOwnApprovalIsWhatItAsked(t *testing.T) {
	p := registryPlatform(t)
	req := runtime.Request{Tenant: "acme", Definition: native.Revision{Spec: native.Spec{Name: "humagen"}}}
	managers, admins := []string{"role:admin", "role:manager"}, []string{"role:admin"}
	for name, want := range map[string]native.ApprovalRequest{
		ToolPauseAgent:           {Prompt: "Approve pause agent?", Approvers: managers, SelfApproval: true},
		ToolRetireAgent:          {Prompt: "Approve retire agent?", Approvers: managers, SelfApproval: true},
		ToolWriteEval:            {Prompt: "Approve write eval suite?", Approvers: managers, SelfApproval: true},
		ToolRecordEval:           {Prompt: "Approve add recorded eval case?", Approvers: managers, SelfApproval: true},
		ToolApplyChange:          {Prompt: "Approve this agent change?", Approvers: managers, SelfApproval: true},
		ToolWorkspacePrompt:      {Prompt: "Apply these shared instructions after every agent's sampled evals pass?", Approvers: admins},
		ToolApplyWorkspacePrompt: {Prompt: "Approve the reviewed shared instruction change?", Approvers: admins},
	} {
		held := p.operationTool(req, &saigetypes.ToolFunc{Def: saigetypes.ToolDef{Name: name}}, p.toolDeclaration(name))
		got, needed := held.(native.Approving).Approval(saigetypes.ToolCallPart{Name: name})
		if !needed || got.Prompt != want.Prompt || !slices.Equal(got.Approvers, want.Approvers) || got.SelfApproval != want.SelfApproval || got.ExpiresIn != 0 {
			t.Errorf("%s asks %+v, %v; want %+v", name, got, needed, want)
		}
	}
	// Every other run tool declares no approvers of its own: the policy for
	// its category decides, and where it allows, nobody is asked.
	all, err := p.SystemTools()
	if err != nil {
		t.Fatal(err)
	}
	own := 0
	for _, tl := range all {
		if !tl.Exposure.Runs() {
			continue
		}
		if tl.Approval.Mode == ApprovalAlways {
			own++
			continue
		}
		if tl.Name == ToolAgent {
			if tl.Approval.PerCall == nil {
				t.Error("the agent tool lost the called agent's call policy")
			}
			continue
		}
		if tl.Approval.PerCall != nil || len(tl.Approval.Approvers) > 0 {
			t.Errorf("%s declares approvers of its own", tl.Name)
		}
	}
	if own != 7 {
		t.Errorf("%d tools carry their own approvers, want 7", own)
	}
}

// Where the policy asks, a tool that declares no approvers of its own asks
// the agent's approvers, for a day, as before; where it refuses, the call is
// denied; and a tool that acts for a person says so through the wrapper.
func TestTheWrapperReadsTheDeclarationAndThePolicy(t *testing.T) {
	p := registryPlatform(t)
	req := runtime.Request{Tenant: "t", Definition: native.Revision{Spec: native.Spec{Name: "a", Approvers: []string{"reviewer"}, SelfApproval: true}}}
	rule := func(c operations.Category, level operations.Level) {
		pol, _ := p.Operations.Policy("t", operations.Scope{Kind: "workspace"})
		if _, err := p.Operations.SetPolicy("t", operations.Policy{Scope: pol.Scope, Rules: map[operations.Category]operations.Level{c: level}}, pol.Version, "manager", time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	held := func(name string) saigetypes.Tool {
		return p.operationTool(req, &saigetypes.ToolFunc{Def: saigetypes.ToolDef{Name: name}, Fn: func(context.Context, map[string]any) (string, error) { return "ok", nil }}, p.toolDeclaration(name))
	}
	rule(operations.ReadContext, operations.Ask)
	got, needed := held(ToolCatalogQuery).(native.Approving).Approval(saigetypes.ToolCallPart{Name: ToolCatalogQuery})
	if !needed || got.Prompt != "Allow a to use catalog_query?" || !slices.Equal(got.Approvers, []string{"reviewer"}) || !got.SelfApproval || got.ExpiresIn != 24*time.Hour {
		t.Errorf("a policy ask = %+v, %v", got, needed)
	}
	// The same policy does not reach a tool of another category.
	if _, needed := held(ToolFileWrite).(native.Approving).Approval(saigetypes.ToolCallPart{Name: ToolFileWrite}); needed {
		t.Error("file_write was asked about under a read_context rule")
	}
	rule(operations.ReadContext, operations.Never)
	if _, err := held(ToolFileRead).Execute(t.Context(), nil); !errors.Is(err, operations.ErrDenied) {
		t.Errorf("file_read under never = %v", err)
	}
	for name, want := range map[string]bool{ToolSourceSearch: true, ToolCatalogQuery: true, ToolCorpusQuery: true, ToolCatalogSearch: false, ToolFileRead: false} {
		if got := held(name).(native.PersonScoped).ActsForPerson(); got != want {
			t.Errorf("%s acts for a person: %v, want %v", name, got, want)
		}
	}
}

// What a call of an API operation does is what the contract says of it
// (x-readonly, x-destructive, x-idempotent), not its HTTP verb: a read that
// takes a body is a POST, and is still a read.
func TestAnOperationsHintsComeFromTheContract(t *testing.T) {
	contract := []byte(`
paths:
  /api/things/{id}/context:
    post:
      operationId: readThing
      summary: Read a thing
      x-permission: runs:read
      x-readonly: true
  /api/things:
    get:
      operationId: listThings
      summary: List things
      x-permission: runs:read
  /api/things/{id}:
    delete:
      operationId: dropThing
      summary: Drop a thing
      x-permission: runs:write
      x-destructive: true
      x-idempotent: true
`)
	tools, err := loadContractTools(contract, []string{"readThing", "listThings", "dropThing"})
	if err != nil {
		t.Fatal(err)
	}
	read, list, drop := tools[0].def, tools[1].def, tools[2].def
	if !read.ReadOnly || !read.Idempotent || read.Destructive || read.Category != operations.ReadContext {
		t.Errorf("a POST the contract marks read-only = %+v", read)
	}
	// Nothing is taken from the verb: an unmarked GET is not called a read.
	if list.ReadOnly || list.Idempotent || list.Category != operations.Other {
		t.Errorf("an unmarked GET = %+v", list)
	}
	if drop.ReadOnly || !drop.Destructive || !drop.Idempotent {
		t.Errorf("a marked DELETE = %+v", drop)
	}
	// In the real contract every GET the registry offers is marked a read,
	// and get_message_context, a POST, is one too.
	real, err := contractTools()
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range real {
		if op.method == "GET" && !op.def.ReadOnly {
			t.Errorf("%s is a GET the contract does not mark x-readonly", op.def.Name)
		}
		if op.def.Exposure != ExposeMCP || op.def.Approval.Mode != ApprovalNone {
			t.Errorf("%s = %+v", op.def.Name, op.def)
		}
	}
	if context, ok := registryPlatform(t).SystemTool("get_message_context"); !ok || !context.ReadOnly || !context.Idempotent {
		t.Errorf("get_message_context = %+v", context)
	}
}
