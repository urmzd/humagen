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

	"github.com/urmzd/humagen/api"
	"github.com/urmzd/humagen/pkg/auth"
)

// System tools are the platform's own tools: what an administrator, or an
// agent acting for one, runs the workspace with. They live in one registry
// (SystemTools), offered alike to any MCP client through the humagen MCP and
// to the platform's agents, and every call is made as someone, with their
// permissions.
//
// Most are operations of the API contract (api/openapi.yaml), named, described
// and checked by it, and served by their own routes in-process as the caller
// (APIDispatch), so a call is authorized, validated and audited exactly as the
// same call to the API. Others are the platform's own (RegisterSystemTool):
// a tool bound to the message an agent answers, or one that calls another
// agent and reports what it does as it does it (SystemCall.Progress).

// SystemTool is one tool of the registry.
type SystemTool struct {
	Name        string
	Title       string
	Description string
	// Input is the JSON Schema of the tool's arguments: an object.
	Input map[string]any
	// Permission is what a caller needs to be offered the tool, "" for any
	// signed-in caller. Calling it is checked again by what it calls.
	Permission auth.Permission
	// ReadOnly, Destructive and Idempotent describe what a call does, for a
	// client deciding whether to ask its person first.
	ReadOnly, Destructive, Idempotent bool
	// Call runs the tool. A refusal the caller should read comes back as a
	// SystemResult with IsError, not as an error.
	Call func(ctx context.Context, c SystemCall, args json.RawMessage) (SystemResult, error)
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
// A name the registry already has is refused.
func (p *Platform) RegisterSystemTool(t SystemTool) error {
	if t.Name == "" || t.Call == nil || t.Input == nil {
		return fmt.Errorf("platform: a system tool needs a name, an input schema and a call")
	}
	ops, err := contractTools()
	if err != nil {
		return err
	}
	p.systemTools.mu.Lock()
	defer p.systemTools.mu.Unlock()
	taken := slices.ContainsFunc(ops, func(o contractTool) bool { return o.def.Name == t.Name }) ||
		slices.ContainsFunc(p.systemTools.own, func(o SystemTool) bool { return o.Name == t.Name })
	if taken {
		return fmt.Errorf("platform: the system tool %q is registered twice", t.Name)
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
// platform's own tools, in the order they were registered.
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
		t := op.def
		t.Call = func(ctx context.Context, c SystemCall, args json.RawMessage) (SystemResult, error) {
			return p.callOperation(ctx, op, c.Who, args)
		}
		out = append(out, t)
	}
	return append(out, own...), nil
}

// SystemOperations are the API operations the registry offers, by
// operationId: what an administrator sets the workspace up and keeps it
// running with.
var SystemOperations = []string{
	// Agents.
	"listAgents", "listAgentRevisions", "createAgent", "reviseAgent", "resetAgent",
	"getAgentControl", "putAgentControl", "getAgentRoster", "getAgentHealth", "listAgentChanges",
	// Evals.
	"listEvalSuites", "createEvalSuite", "updateEvalSuite", "deleteEvalSuite", "runEvalSuite", "listEvalRuns",
	// Sessions and runs.
	"listThreads", "getRun", "getAnalytics",
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
			required = append(required, name)
		} else {
			t.query = append(t.query, name)
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
	t.def.ReadOnly = method == http.MethodGet
	t.def.Destructive = method == http.MethodDelete
	t.def.Idempotent = t.def.ReadOnly || method == http.MethodPut || method == http.MethodDelete
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
