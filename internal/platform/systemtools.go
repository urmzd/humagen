package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"go.yaml.in/yaml/v3"

	"github.com/urmzd/orchagent/flow/event"
	"github.com/urmzd/orchagent/flow/runtime"
	"github.com/urmzd/orchagent/flow/turn/native"
	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/humagen/api"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/operations"
)

// System tools are the platform's tools: what an administrator runs the
// workspace with, and what an agent's run is offered. They live in one
// registry (SystemTools). Each tool declares itself once: its name, what it
// does (Category, ReadOnly, Destructive, Idempotent, Parallel), whether it
// acts with a person's own account, who approves a call (Approval), and who
// it is offered to (Exposure): MCP clients through the humagen MCP, agent
// runs, or both. Every call is made as someone, with their permissions.
//
// Most are operations of the API contract (api/openapi.yaml), named, described
// and checked by it, and served by their own routes in-process as the caller
// (APIDispatch), so a call is authorized, validated and audited exactly as the
// same call to the API. Others are the platform's own (RegisterSystemTool):
// a tool bound to the message an agent answers, or one that calls another
// agent and reports what it does as it does it (SystemCall.Progress), and the
// tools a run is offered (systemtools_run.go), each built for the turn that
// uses it (Run).

// SystemTool is one tool of the registry.
type SystemTool struct {
	// Name is what a caller knows the tool by. Key is its place in the
	// registry, the name unless Key says otherwise: two tools share a name
	// only when they are offered to different callers (Exposure) and each
	// has a key of its own.
	Name        string
	Key         string
	Title       string
	Description string
	// Input is the JSON Schema of the tool's arguments: an object. A tool
	// offered only to runs may leave it out: its constructor (Run) holds
	// its one definition.
	Input map[string]any
	// Permission is what a caller needs to be offered the tool, "" for any
	// signed-in caller. Calling it is checked again by what it calls.
	Permission auth.Permission
	// Category is the kind of action a call is, which the action policy
	// (workspace, project, agent, personal) decides by. Every tool states
	// one; nothing is read from its name.
	Category operations.Category
	// Also is a second category whose policy holds a call as well: a tool
	// that sends something outside and changes files. With BothAllow a call
	// runs unasked only when both categories allow it.
	Also      operations.Category
	BothAllow bool
	// ReadOnly, Destructive and Idempotent describe what a call does, for a
	// client deciding whether to ask its person first. Parallel reports
	// that calls may run side by side.
	ReadOnly, Destructive, Idempotent, Parallel bool
	// ActsForPerson reports that a call runs with the credential of the
	// person the run acts for.
	ActsForPerson bool
	// Approval is who approves a call before it runs.
	Approval ToolApproval
	// Exposure is who the tool is offered to. The zero value offers it to
	// nobody.
	Exposure Exposure
	// Upstream marks a tool the runtime builds itself: the registry
	// declares it and does not construct it.
	Upstream bool
	// Call runs the tool for a caller of the system tools. A refusal the
	// caller should read comes back as a SystemResult with IsError, not as
	// an error.
	Call func(ctx context.Context, c SystemCall, args json.RawMessage) (SystemResult, error)
	// Run builds the tool for one turn of a run: what a tool offered to
	// runs is, bound to the run's request.
	Run func(t *ToolTurn) (saigetypes.Tool, error)
}

// key is the tool's place in the registry.
func (t SystemTool) key() string {
	if t.Key != "" {
		return t.Key
	}
	return t.Name
}

// Exposure is who a tool is offered to.
type Exposure uint8

const (
	// ExposeMCP lists the tool at /api/mcp, for a signed-in MCP client.
	ExposeMCP Exposure = 1 << iota
	// ExposeRuns offers the tool to an agent's run, when the agent's
	// revision names it.
	ExposeRuns
)

// MCP reports whether the tool is listed at /api/mcp.
func (e Exposure) MCP() bool { return e&ExposeMCP != 0 }

// Runs reports whether an agent's run may be offered the tool.
func (e Exposure) Runs() bool { return e&ExposeRuns != 0 }

// ApprovalMode is how a tool's calls are approved.
type ApprovalMode string

const (
	// ApprovalByPolicy leaves it to the action policy for the tool's
	// category: allow, ask or never. It is the zero value, so a tool that
	// declares nothing is held by policy.
	ApprovalByPolicy ApprovalMode = ""
	// ApprovalAlways asks the tool's own approvers about every call,
	// whatever the policy allows.
	ApprovalAlways ApprovalMode = "always"
	// ApprovalNone asks nobody: the tool is called by a signed-in person's
	// own client, which decides what to confirm. A tool offered to runs
	// cannot declare it.
	ApprovalNone ApprovalMode = "none"
)

// ToolApproval is a tool's one declaration of who approves a call. The one
// wrapper every run tool passes through (operationTool) reads it, with the
// action policy.
type ToolApproval struct {
	Mode ApprovalMode
	// Approvers, SelfApproval and Expires are who is asked, whether the
	// person the run acts for may answer, and how long the question waits
	// (zero is the runtime's default), for ApprovalAlways.
	Approvers    []string
	SelfApproval bool
	Expires      time.Duration
	// Prompt words the question; nil asks "Approve <the tool's name>?".
	Prompt func(ApprovalCall) string
	// PerCall, when set, decides for each call whether the tool's own
	// approvers are asked, and who they are: the agent tool, held by the
	// called agent's call policy.
	PerCall func(ApprovalCall) (ToolApproval, bool)
}

// ApprovalCall is the call an approval is asked about.
type ApprovalCall struct {
	Request runtime.Request
	Tool    string
	Call    saigetypes.ToolCallPart
}

// ToolTurn is one turn of a run, as the constructor of a run tool sees it.
type ToolTurn struct {
	Request runtime.Request
	// Action reports what a tool did on the run's stream.
	Action func(event.Action) error
	// connectors are the actions of the clients the turn answers through.
	connectors native.Tools
}

// SystemCall is who calls a system tool, and from where.
type SystemCall struct {
	Who auth.Principal
	// Message is the message being answered when an agent calls in a run: a
	// tool that reads from it is bound to it. An MCP client names the
	// message in its arguments instead.
	Message string
	// Run is the run that calls, and ToolUseID the id the platform minted
	// for this call (htu_<run>_<seq>, runs.Run.CallID), when a run calls.
	Run       string
	ToolUseID string
	// Progress, when set, receives what a long call does while it does it,
	// such as the activity of an agent it calls. Nil discards it.
	Progress func(SystemProgress)
}

// SystemResult is what a call answered.
type SystemResult struct {
	Text    string
	IsError bool
}

// APIDispatch serves one API call in-process as who, through the server's
// own routes, and returns its status and body. The API server installs it
// (SetAPIDispatch); until it does, the contract's tools answer that the API
// is not being served.
type APIDispatch func(ctx context.Context, who auth.Principal, method, path string, query url.Values, body []byte) (status int, out []byte)

// SetAPIDispatch installs how the contract's tools reach the API's routes.
func (p *Platform) SetAPIDispatch(d APIDispatch) {
	p.systemTools.mu.Lock()
	defer p.systemTools.mu.Unlock()
	p.systemTools.dispatch = d
}

// RegisterSystemTool adds one of the platform's own tools to the registry.
// A tool declares who it is offered to and its category. A key the registry
// already has is refused, and so is a name already offered to the same
// callers: two tools share a name only across exposures, under keys of
// their own.
func (p *Platform) RegisterSystemTool(t SystemTool) error {
	switch {
	case t.Name == "":
		return fmt.Errorf("platform: a system tool needs a name")
	case t.Exposure == 0:
		return fmt.Errorf("platform: the system tool %q says who it is offered to (Exposure)", t.Name)
	case t.Category == "":
		return fmt.Errorf("platform: the system tool %q states its category", t.Name)
	case t.Exposure.MCP() && (t.Call == nil || t.Input == nil):
		return fmt.Errorf("platform: the system tool %q is offered over MCP, so it needs an input schema and a call", t.Name)
	case t.Exposure.Runs() && t.Run == nil && !t.Upstream:
		return fmt.Errorf("platform: the system tool %q is offered to runs, so it needs a constructor (Run)", t.Name)
	case t.Exposure.Runs() && t.Approval.Mode == ApprovalNone:
		return fmt.Errorf("platform: the system tool %q is offered to runs, so the action policy holds it", t.Name)
	}
	ops, err := contractTools()
	if err != nil {
		return err
	}
	p.systemTools.mu.Lock()
	defer p.systemTools.mu.Unlock()
	clash := func(o SystemTool) bool {
		return o.key() == t.key() || o.Name == t.Name && o.Exposure&t.Exposure != 0
	}
	taken := slices.ContainsFunc(ops, func(o contractTool) bool { return clash(o.def) }) || slices.ContainsFunc(p.systemTools.own, clash)
	if taken {
		return fmt.Errorf("platform: the system tool %q is registered twice", t.key())
	}
	p.systemTools.own = append(p.systemTools.own, t)
	return nil
}

// systemRegistry is the platform's half of the registry: its own tools, how
// the contract's reach the API, and the run machinery behind the agent call.
type systemRegistry struct {
	mu        sync.Mutex
	dispatch  APIDispatch
	own       []SystemTool
	callAgent AgentCaller
}

// SystemTools is the registry: the contract's operations, then the
// platform's own tools, in the order they were registered. It holds the
// tools of every exposure; a caller offers the ones meant for it.
func (p *Platform) SystemTools() ([]SystemTool, error) {
	ops, err := contractTools()
	if err != nil {
		return nil, err
	}
	p.systemTools.mu.Lock()
	own := slices.Clone(p.systemTools.own)
	p.systemTools.mu.Unlock()
	out := make([]SystemTool, 0, len(ops)+len(own))
	for _, op := range ops {
		out = append(out, p.contractSystemTool(op))
	}
	return append(out, own...), nil
}

// SystemTool is the registry's one tool under key, a contract operation or
// one of the platform's own: what a run's tool set reads a tool's definition
// from, and calls it through. A tool's key is its name, unless it shares the
// name with a tool of another exposure.
func (p *Platform) SystemTool(key string) (SystemTool, bool) {
	if ops, err := contractTools(); err == nil {
		for _, op := range ops {
			if op.def.key() == key {
				return p.contractSystemTool(op), true
			}
		}
	}
	p.systemTools.mu.Lock()
	defer p.systemTools.mu.Unlock()
	for _, t := range p.systemTools.own {
		if t.key() == key {
			return t, true
		}
	}
	return SystemTool{}, false
}

// runTool is the registry's tool a run knows as name: the one of that name
// offered to runs.
func (p *Platform) runTool(name string) (SystemTool, bool) {
	p.systemTools.mu.Lock()
	defer p.systemTools.mu.Unlock()
	for _, t := range p.systemTools.own {
		if t.Name == name && t.Exposure.Runs() {
			return t, true
		}
	}
	return SystemTool{}, false
}

// contractSystemTool is op as a tool of the registry: its definition, called
// through its own route.
func (p *Platform) contractSystemTool(op contractTool) SystemTool {
	t := op.def
	t.Call = func(ctx context.Context, c SystemCall, args json.RawMessage) (SystemResult, error) {
		return p.callOperation(ctx, op, c.Who, args)
	}
	return t
}

// SystemOperations are the API operations the registry offers, by
// operationId: what an administrator sets the workspace up and keeps it
// running with.
var SystemOperations = []string{
	// Agents.
	"listAgents", "listAgentRevisions", "createAgent", "reviseAgent", "resetAgent",
	"getAgentControl", "putAgentControl", "getAgentRoster", "getAgentHealth", "listAgentChanges",
	// The agent store.
	"listStorePackages", "installStorePackage", "removeStorePackage",
	// Evals.
	"listEvalSuites", "createEvalSuite", "updateEvalSuite", "deleteEvalSuite", "runEvalSuite", "listEvalRuns",
	// Sessions and runs.
	"listThreads", "getRun", "getAnalytics",
	// Messages.
	"sendMessages", "listMessages", "sendMessagesUnder", "getMessage", "rateMessage", "stopMessage", "getMessageContext", "traceMessage",
	// Alerts.
	"listAlerts", "acknowledgeAlert", "changeAlertState",
	"listAlertRules", "createAlertRule", "updateAlertRule", "deleteAlertRule",
	// Connections.
	"listConnectionPresets", "getConnection", "checkConnection", "authorizeConnection", "disconnectConnection",
	// Model providers.
	"listModelProviders", "setModelProvider", "deleteModelProvider", "checkModelProvider",
	// Schedules.
	"listSchedules", "createSchedule", "updateSchedule", "deleteSchedule", "pauseSchedule", "resumeSchedule", "runSchedule",
	// Settings.
	"getSettings", "updateSettings",
}

// MaxSystemToolOutput bounds what one call returns to its caller.
const MaxSystemToolOutput = 64 << 10

// contractTool is one operation of the API contract as a tool: its
// definition, and where its call goes.
type contractTool struct {
	def    SystemTool
	method string
	// path is the operation's path, with a {name} segment per path parameter.
	path   string
	params []string
	query  []string
	body   bool
}

var contractTools = sync.OnceValues(func() ([]contractTool, error) {
	return loadContractTools(api.OpenAPI, SystemOperations)
})

// callOperation calls op as who, through its own route.
func (p *Platform) callOperation(ctx context.Context, op contractTool, who auth.Principal, raw json.RawMessage) (SystemResult, error) {
	p.systemTools.mu.Lock()
	dispatch := p.systemTools.dispatch
	p.systemTools.mu.Unlock()
	if dispatch == nil {
		return SystemResult{IsError: true, Text: "the API is not being served here"}, nil
	}
	args := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return SystemResult{IsError: true, Text: "the arguments are not a JSON object"}, nil
		}
	}
	path := op.path
	for _, name := range op.params {
		v, _ := args[name].(string)
		if strings.TrimSpace(v) == "" {
			return SystemResult{IsError: true, Text: name + " is required"}, nil
		}
		path = strings.Replace(path, "{"+name+"}", url.PathEscape(v), 1)
	}
	q := url.Values{}
	for _, name := range op.query {
		switch v := args[name].(type) {
		case nil:
		case []any:
			for _, each := range v {
				q.Add(name, fmt.Sprint(each))
			}
		default:
			q.Set(name, fmt.Sprint(v))
		}
	}
	var body []byte
	if b, ok := args["body"]; ok && op.body {
		raw, err := json.Marshal(b)
		if err != nil {
			return SystemResult{IsError: true, Text: "the body is not JSON"}, nil
		}
		body = raw
	}
	status, out := dispatch(ctx, who, op.method, path, q, body)
	if status >= http.StatusBadRequest {
		return SystemResult{IsError: true, Text: apiErrorText(status, out)}, nil
	}
	text := string(out)
	if len(text) > MaxSystemToolOutput {
		text = text[:MaxSystemToolOutput] + "\n… (cut: narrow the call to read the rest)"
	}
	if text == "" {
		text = http.StatusText(status)
	}
	return SystemResult{Text: text}, nil
}

// apiErrorText is what the API said was wrong.
func apiErrorText(status int, out []byte) string {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(out, &e) == nil && e.Error != "" {
		return fmt.Sprintf("%d %s: %s", status, http.StatusText(status), e.Error)
	}
	return fmt.Sprintf("%d %s", status, http.StatusText(status))
}

// loadContractTools reads ops out of the API contract.
func loadContractTools(contract []byte, ops []string) ([]contractTool, error) {
	var spec map[string]any
	if err := yaml.Unmarshal(contract, &spec); err != nil {
		return nil, fmt.Errorf("platform: the API contract could not be read: %w", err)
	}
	components, _ := spec["components"].(map[string]any)
	byID := map[string]contractTool{}
	paths, _ := spec["paths"].(map[string]any)
	for path, item := range paths {
		methods, _ := item.(map[string]any)
		for method, raw := range methods {
			op, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			id, _ := op["operationId"].(string)
			if !slices.Contains(ops, id) {
				continue
			}
			byID[id] = contractToolOf(id, strings.ToUpper(method), path, op, components)
		}
	}
	out := make([]contractTool, 0, len(ops))
	for _, id := range ops {
		t, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("platform: the API contract has no operation %q for the system tools", id)
		}
		out = append(out, t)
	}
	return out, nil
}

// contractToolOf is one operation as a tool: path and query parameters as
// named arguments, and the request body, its schema resolved, as "body".
func contractToolOf(id, method, path string, op, components map[string]any) contractTool {
	t := contractTool{method: method, path: path}
	if perm, _ := op["x-permission"].(string); perm != "" && perm != "authenticated" {
		t.def.Permission = auth.Permission(perm)
	}
	props := map[string]any{}
	var required []string
	params, _ := op["parameters"].([]any)
	for _, raw := range params {
		p, ok := resolveRef(raw, components).(map[string]any)
		if !ok {
			continue
		}
		name, _ := p["name"].(string)
		in, _ := p["in"].(string)
		if name == "" || (in != "path" && in != "query") {
			continue
		}
		schema, _ := resolveSchema(p["schema"], components, 0).(map[string]any)
		if schema == nil {
			schema = map[string]any{"type": "string"}
		}
		if d, _ := p["description"].(string); d != "" {
			schema["description"] = d
		}
		props[name] = schema
		if in == "path" {
			t.params = append(t.params, name)
		} else {
			t.query = append(t.query, name)
		}
		if req, _ := p["required"].(bool); req || in == "path" {
			required = append(required, name)
		}
	}
	if rb, ok := resolveRef(op["requestBody"], components).(map[string]any); ok {
		content, _ := rb["content"].(map[string]any)
		if js, ok := content["application/json"].(map[string]any); ok {
			body, _ := resolveSchema(js["schema"], components, 0).(map[string]any)
			if body == nil {
				body = map[string]any{"type": "object"}
			}
			if _, has := body["description"]; !has {
				body["description"] = "The request body, as the API takes it."
			}
			props["body"] = body
			t.body = true
			if req, _ := rb["required"].(bool); req {
				required = append(required, "body")
			}
		}
	}
	input := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		input["required"] = required
	}
	summary, _ := op["summary"].(string)
	desc, _ := op["description"].(string)
	description := strings.TrimSpace(summary + ".\n\n" + strings.TrimSpace(desc))
	if t.def.Permission != "" {
		description += "\n\nNeeds " + string(t.def.Permission) + "."
	}
	t.def.Name = snake(id)
	t.def.Title = summary
	t.def.Description = description
	t.def.Input = input
	// What a call does is what the contract says of the operation
	// (x-readonly, x-destructive, x-idempotent), never its HTTP verb: a
	// read that takes a body is a POST.
	t.def.ReadOnly, _ = op["x-readonly"].(bool)
	t.def.Destructive, _ = op["x-destructive"].(bool)
	idempotent, _ := op["x-idempotent"].(bool)
	t.def.Idempotent = t.def.ReadOnly || idempotent
	t.def.Parallel = t.def.ReadOnly
	t.def.Category = operations.Other
	if t.def.ReadOnly {
		t.def.Category = operations.ReadContext
	}
	// An operation is called by a signed-in person's own client, as them.
	t.def.Approval = ToolApproval{Mode: ApprovalNone}
	t.def.Exposure = ExposeMCP
	return t
}

// snake is an operationId as a tool name: listAgents is list_agents.
func snake(id string) string {
	var b strings.Builder
	for i, r := range id {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('_')
			}
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return b.String()
}

// resolveRef follows one local $ref ("#/components/<kind>/<name>").
func resolveRef(node any, components map[string]any) any {
	m, ok := node.(map[string]any)
	if !ok {
		return node
	}
	ref, ok := m["$ref"].(string)
	if !ok {
		return node
	}
	parts := strings.Split(strings.TrimPrefix(ref, "#/components/"), "/")
	if len(parts) != 2 {
		return node
	}
	kind, _ := components[parts[0]].(map[string]any)
	return kind[parts[1]]
}

// maxSchemaDepth bounds how deep a schema's references are followed; past
// it, and in a schema that refers to itself, an object stands in.
const maxSchemaDepth = 8

// resolveSchema is a schema with its references followed, as plain JSON
// Schema a client can read.
func resolveSchema(node any, components map[string]any, depth int) any {
	switch v := node.(type) {
	case map[string]any:
		if _, ref := v["$ref"]; ref {
			if depth >= maxSchemaDepth {
				return map[string]any{"type": "object"}
			}
			return resolveSchema(resolveRef(v, components), components, depth+1)
		}
		out := make(map[string]any, len(v))
		for k, val := range v {
			if strings.HasPrefix(k, "x-") || k == "example" || k == "discriminator" {
				continue
			}
			out[k] = resolveSchema(val, components, depth)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, val := range v {
			out[i] = resolveSchema(val, components, depth)
		}
		return out
	default:
		return v
	}
}

// SystemProgress is one step of a call's activity, as the response stream
// carries it (StreamEventOf): an event of an agent it called, a change of a
// call's status, or a passing status line.
type SystemProgress struct {
	Kind ProgressKind
	// ToolUseID is the call the step is about.
	ToolUseID string
	// For ProgressNested: the called agent's run, how deep it is under the
	// response's root (1 for a call the root made), and its event, an
	// Anthropic stream event as the agent's own content produces it.
	AgentRun string
	Depth    int
	Event    json.RawMessage
	// For ProgressStatus: the call's state, the calls from the root down to
	// it, its title, the question it waits on, and how long it has run.
	Status   ToolStatus
	Path     []string
	Title    string
	Question *ProgressQuestion
	Elapsed  time.Duration
	// For ProgressLine: the line.
	Text string
}

// ProgressKind is which response stream event a step becomes.
type ProgressKind string

const (
	// ProgressNested is a called agent's own event: humagen.nested.
	ProgressNested ProgressKind = "nested"
	// ProgressStatus is a call changing state: humagen.tool_status.
	ProgressStatus ProgressKind = "status"
	// ProgressLine is a passing status line: humagen.progress.
	ProgressLine ProgressKind = "line"
)

// ToolStatus is a call's state in the response stream.
type ToolStatus string

// The states of a call.
const (
	ToolQueued           ToolStatus = "queued"
	ToolRunning          ToolStatus = "running"
	ToolAwaitingApproval ToolStatus = "awaiting_approval"
	ToolAwaitingInput    ToolStatus = "awaiting_input"
	ToolDone             ToolStatus = "done"
	ToolError            ToolStatus = "error"
	ToolDenied           ToolStatus = "denied"
	ToolStopped          ToolStatus = "stopped"
)

var toolStatuses = []ToolStatus{ToolQueued, ToolRunning, ToolAwaitingApproval, ToolAwaitingInput, ToolDone, ToolError, ToolDenied, ToolStopped}

// ProgressQuestion is what a call waits on a person for.
type ProgressQuestion struct {
	Kind    string   `json:"kind"`
	Prompt  string   `json:"prompt"`
	Choices []string `json:"choices,omitempty"`
	// Who may answer: principals, and roles as "role:<name>".
	Who []string `json:"who,omitempty"`
}

// The response stream's own event types.
const (
	EventNested     = "humagen.nested"
	EventToolStatus = "humagen.tool_status"
	EventProgress   = "humagen.progress"
)

// StreamEventOf is the response stream event a step of a call becomes: its
// type and its body. A step that cannot be one is an error, so nothing
// malformed reaches a reader.
func StreamEventOf(s SystemProgress) (string, map[string]any, error) {
	switch s.Kind {
	case ProgressNested:
		switch {
		case s.ToolUseID == "" || s.AgentRun == "":
			return "", nil, fmt.Errorf("platform: a nested event names its call and the agent's run")
		case s.Depth < 1:
			return "", nil, fmt.Errorf("platform: a nested event is at depth 1 or more")
		case !json.Valid(s.Event):
			return "", nil, fmt.Errorf("platform: a nested event carries an event")
		}
		return EventNested, map[string]any{
			"parent_tool_use_id": s.ToolUseID,
			"agent_run_id":       s.AgentRun,
			"depth":              s.Depth,
			"event":              json.RawMessage(s.Event),
		}, nil
	case ProgressStatus:
		if s.ToolUseID == "" || !slices.Contains(toolStatuses, s.Status) {
			return "", nil, fmt.Errorf("platform: a status names its call and one of its states, not %q", s.Status)
		}
		path := s.Path
		if len(path) == 0 {
			path = []string{s.ToolUseID}
		}
		body := map[string]any{
			"tool_use_id": s.ToolUseID,
			"path":        path,
			"status":      s.Status,
			"elapsed_ms":  s.Elapsed.Milliseconds(),
		}
		if s.Title != "" {
			body["title"] = s.Title
		}
		if s.Question != nil {
			body["question"] = s.Question
		}
		return EventToolStatus, body, nil
	case ProgressLine:
		if strings.TrimSpace(s.Text) == "" {
			return "", nil, fmt.Errorf("platform: a progress line has text")
		}
		body := map[string]any{"text": s.Text}
		if s.ToolUseID != "" {
			body["parent_tool_use_id"] = s.ToolUseID
		}
		return EventProgress, body, nil
	}
	return "", nil, fmt.Errorf("platform: no stream event for progress %q", s.Kind)
}
