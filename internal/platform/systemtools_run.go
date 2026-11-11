package platform

import (
	"errors"
	"fmt"
	"slices"
	"sync"

	saigetypes "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/runtime"
	"github.com/urmzd/saige/flow/turn/native"

	"github.com/urmzd/humagen/pkg/operations"
)

// The tools an agent's run is offered, as the registry declares them. Each
// is declared once here: its category, what a call does, whether it acts
// with a person's own account, who approves a call, and the constructor
// that builds it for a turn. A turn's tools are built from these
// declarations (registryTools), and the one wrapper every run tool passes
// through reads them (operationTool). A tool's description and arguments
// are its constructor's.
//
// Three names are also operations of the API contract, with their own
// implementation: list_agents, list_alerts and run_eval_suite. A run is
// offered the run tool and an MCP client the operation, so each run tool
// has a registry key of its own (runToolKey) and is never listed over MCP.

// runToolKey is the registry key of a run tool whose name an operation of the
// API contract also has.
func runToolKey(name string) string { return "run/" + name }

// connectorActions are the actions of the clients a workspace answers
// through, as the connectors name them, each with what it declares.
func connectorActions() []SystemTool {
	read := SystemTool{Category: operations.ReadContext, ReadOnly: true, Idempotent: true, Parallel: true}
	send := SystemTool{Category: operations.ExternalMessages}
	named := func(t SystemTool, name string) SystemTool {
		t.Name = name
		return t
	}
	// A change to a pull request sends something outside and changes files.
	apply := named(send, "github_apply_changes")
	apply.Also = operations.WriteFiles
	// Opening a fix pull request runs unasked only when both are allowed.
	fix := named(send, "github_create_fix_pr")
	fix.Also, fix.BothAllow = operations.WriteFiles, true
	return []SystemTool{
		named(read, "github_read_context"),
		named(read, "github_fix_base"),
		named(read, "github_read_file"),
		named(read, "github_workflow_status"),
		named(send, "github_comment"),
		named(send, "github_create_review"),
		named(send, "github_request_review"),
		named(send, "github_add_labels"),
		apply,
		fix,
		named(SystemTool{Category: operations.Other}, "github_run_checks"),
		named(send, "slack_post_message"),
		named(send, "slack_add_reaction"),
	}
}

// sessionReadTools are the runtime's session verbs a run is still offered:
// reading another session of its tree, or one shared with it, and linking
// one as context or evidence. The verbs that call an agent or wait on one
// (session_call, session_listen, session_status) are not: the agent tool is
// the one way a run calls an agent.
var sessionReadTools = []string{"session_read", "session_link"}

// upstreamTools are the tools the runtime builds itself and offers a run
// ahead of the platform's catalog: the session verbs that read and
// ask_person. The registry declares them so that each has a category. Their
// calls do not pass the wrapper (operationTool): the runtime resolves them
// before it asks the catalog, which only the runtime can change.
func upstreamTools() []SystemTool {
	out := []SystemTool{}
	for _, name := range append(slices.Clone(sessionReadTools), toolAskPerson) {
		out = append(out, SystemTool{Name: name, Category: operations.Other, Upstream: true})
	}
	return out
}

// registerRunTools declares the tools only a run is offered.
func (p *Platform) registerRunTools() error {
	// of builds a tool from the turn's request, and acting from the
	// request and the way to report on the run's stream.
	of := func(build func(runtime.Request) saigetypes.Tool) func(*ToolTurn) (saigetypes.Tool, error) {
		return func(t *ToolTurn) (saigetypes.Tool, error) { return build(t.Request), nil }
	}
	acting := func(build func(runtime.Request, func(event.Action) error) saigetypes.Tool) func(*ToolTurn) (saigetypes.Tool, error) {
		return func(t *ToolTurn) (saigetypes.Tool, error) { return build(t.Request, t.Action), nil }
	}
	file := func(name string) func(*ToolTurn) (saigetypes.Tool, error) {
		return func(t *ToolTurn) (saigetypes.Tool, error) { return p.fileTool(name, t.Request, t.Action), nil }
	}
	builtin := func(name string) func(*ToolTurn) (saigetypes.Tool, error) {
		return func(t *ToolTurn) (saigetypes.Tool, error) { return p.builtinTool(name, t.Request), nil }
	}
	const (
		read    = operations.ReadContext
		write   = operations.WriteFiles
		publish = operations.PublishArtifacts
		other   = operations.Other
	)
	// reads marks a tool that only reads: safe to repeat and to run side
	// by side.
	reads := func(t SystemTool) SystemTool {
		t.ReadOnly, t.Idempotent, t.Parallel = true, true, true
		return t
	}
	tools := []SystemTool{
		// Files and the sandbox.
		reads(SystemTool{Name: ToolFileRead, Category: read, Run: file(ToolFileRead)}),
		reads(SystemTool{Name: ToolFileList, Category: read, Run: file(ToolFileList)}),
		{Name: ToolFileWrite, Category: write, Idempotent: true, Run: file(ToolFileWrite)},
		{Name: ToolPublish, Category: publish, Run: file(ToolPublish)},
		{Name: ToolOutcome, Category: other, Run: file(ToolOutcome)},
		{Name: ToolShell, Category: write, Destructive: true, Run: acting(p.shellTool)},

		// The analyst's.
		reads(SystemTool{Name: ToolRunsSummary, Category: read, Run: builtin(ToolRunsSummary)}),
		reads(SystemTool{Name: ToolListAlerts, Key: runToolKey(ToolListAlerts), Category: read, Run: builtin(ToolListAlerts)}),
		reads(SystemTool{Name: ToolProjectChanges, Category: other, Run: of(p.projectChangesTool)}),

		// The builder's. The ones that change an agent, its evals or the
		// shared instructions carry their own approvers.
		reads(SystemTool{Name: ToolListAgents, Key: runToolKey(ToolListAgents), Category: read, Run: builtin(ToolListAgents)}),
		reads(SystemTool{Name: ToolReadAgent, Category: read, Run: builtin(ToolReadAgent)}),
		{Name: ToolSaveAgent, Category: write, Run: builtin(ToolSaveAgent)},
		{Name: ToolPauseAgent, Category: other, Approval: builderApproval(), Run: of(func(r runtime.Request) saigetypes.Tool { return p.builderLifecycleTool(r, false) })},
		{Name: ToolRetireAgent, Category: other, Approval: builderApproval(), Run: of(func(r runtime.Request) saigetypes.Tool { return p.builderLifecycleTool(r, true) })},
		{Name: ToolWriteEval, Category: other, Approval: builderApproval(), Run: of(p.builderWriteEvalTool)},
		{Name: ToolRunEval, Key: runToolKey(ToolRunEval), Category: other, Run: of(p.builderRunEvalTool)},
		reads(SystemTool{Name: ToolReadEvals, Category: other, Run: of(p.builderReadEvalsTool)}),
		{Name: ToolPreviewChange, Category: other, Run: of(p.builderPreviewChangeTool)},
		{Name: ToolApplyChange, Category: other, Approval: withPrompt(builderApproval(), p.applyChangePrompt), Run: of(p.builderApplyChangeTool)},
		{Name: ToolRecordEval, Category: other, Approval: builderApproval(), Run: of(p.builderRecordEvalTool)},
		reads(SystemTool{Name: ToolConnections, Category: other, Run: of(func(r runtime.Request) saigetypes.Tool { return p.builderConnectionsTool(r, false) })}),
		{Name: ToolConnect, Category: other, Run: of(func(r runtime.Request) saigetypes.Tool { return p.builderConnectionsTool(r, true) })},
		{Name: ToolWorkspacePrompt, Category: other, Approval: sharedPromptApproval(workspacePromptPrompt), Run: of(p.builderWorkspacePromptTool)},
		{Name: ToolPreviewWorkspacePrompt, Category: other, Run: of(p.builderPreviewWorkspacePromptTool)},
		{Name: ToolApplyWorkspacePrompt, Category: other, Approval: sharedPromptApproval(p.applyWorkspacePromptPrompt), Run: of(p.builderApplyWorkspacePromptTool)},

		// Alerts on the run's session, and what the run remembers.
		{Name: ToolAlertCreate, Category: other, Run: of(p.sessionAlertCreateTool)},
		{Name: ToolAlertResolve, Category: other, Idempotent: true, Run: of(p.sessionAlertResolveTool)},
		reads(SystemTool{Name: ToolMemorySearch, Category: other, Run: of(p.memorySearchTool)}),

		// Knowledge. The ones that use the reader's own access act for
		// that person.
		reads(SystemTool{Name: ToolSourceSearch, Category: read, ActsForPerson: true, Run: of(p.sourceSearchTool)}),
		reads(SystemTool{Name: ToolCatalogSearch, Category: read, Run: of(p.catalogSearchTool)}),
		reads(SystemTool{Name: ToolCatalogQuery, Category: read, ActsForPerson: true, Run: of(p.catalogQueryTool)}),
		reads(SystemTool{Name: ToolCorpusQuery, Category: read, ActsForPerson: true, Run: of(p.corpusQueryTool)}),
		reads(SystemTool{Name: ToolCorpusSearch, Category: read, Run: of(p.corpusSearchTool)}),
		reads(SystemTool{Name: ToolCorpusGraph, Category: read, Run: of(p.corpusGraphTool)}),
	}
	// The actions of the clients the turn answers through: the connector
	// defines each, and the turn has it when its client allows it.
	for _, action := range connectorActions() {
		name := action.Name
		action.Run = func(t *ToolTurn) (saigetypes.Tool, error) { return t.connector(name) }
		tools = append(tools, action)
	}
	tools = append(tools, upstreamTools()...)
	for _, t := range tools {
		t.Exposure = ExposeRuns
		if err := p.RegisterSystemTool(t); err != nil {
			return err
		}
	}
	return nil
}

// withPrompt is a with its question worded by prompt.
func withPrompt(a ToolApproval, prompt func(ApprovalCall) string) ToolApproval {
	a.Prompt = prompt
	return a
}

// connector is the turn's connector action named name.
func (t *ToolTurn) connector(name string) (saigetypes.Tool, error) {
	if t.connectors == nil {
		return nil, fmt.Errorf("%w: %q", native.ErrNoSuchTool, name)
	}
	return t.connectors.Lookup(name)
}

// registryTools is the catalog of one turn's tools, built from the
// registry: a name resolves to the tool the registry offers runs under it,
// built for this turn by its constructor, once. An agent has a tool only if
// its spec names it; this decides what the tool is, not who has it. An
// action of a connector the registry does not declare still resolves
// through the turn's connectors, and is held as an undeclared tool.
func (p *Platform) registryTools() runtime.Catalog {
	return func(req runtime.Request, action func(event.Action) error) (native.Tools, error) {
		connectors, err := p.connectorTools(req, action)
		if err != nil {
			return nil, err
		}
		turn := &ToolTurn{Request: req, Action: action, connectors: connectors}
		var mu sync.Mutex
		built := map[string]saigetypes.Tool{}
		return native.ToolsFunc(func(name string) (saigetypes.Tool, error) {
			mu.Lock()
			defer mu.Unlock()
			if t, ok := built[name]; ok {
				return t, nil
			}
			def, ok := p.runTool(name)
			if !ok || def.Run == nil {
				return turn.connector(name)
			}
			t, err := def.Run(turn)
			if err != nil {
				return nil, err
			}
			if t == nil {
				return nil, errors.New("platform: the tool " + name + " could not be built")
			}
			built[name] = t
			return t, nil
		}), nil
	}
}
