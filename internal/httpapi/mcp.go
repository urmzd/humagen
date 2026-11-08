package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"unicode"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.yaml.in/yaml/v3"

	"github.com/urmzd/humagen/api"
	"github.com/urmzd/humagen/pkg/auth"
)

// The humagen MCP: the workspace's management as MCP tools, at POST
// /api/mcp, for any MCP client a person signs in with and for the platform's
// own helper agent. Each tool is one operation of the API contract
// (api/openapi.yaml): its name, description, permission and input come from
// that operation, and a call is that operation, served in-process by its own
// route as the caller (dispatch), so it is authorized, validated, audited and
// logged exactly as the API is. A caller is offered only the tools its
// permissions allow, and gets no more through them than through the API.

// mcpOperations are the operations the humagen MCP offers, by operationId:
// what an administrator, or the helper acting for one, sets the workspace up
// and keeps it running with.
var mcpOperations = []string{
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

// mcpInstructions is what an MCP client is told about the server.
const mcpInstructions = "Humagen's own tools for running this workspace: its agents, their evals, sessions and runs, alerts, connections, model providers, schedules and settings. Each tool is one call of the Humagen API, made as you, with your permissions: you are offered only what you may do. Read before you change: a revise takes the revision it edits from (expected_rev)."

// mcpTool is one operation of the API contract offered as a tool.
type mcpTool struct {
	tool   *mcp.Tool
	method string
	// path is the operation's path, with a {name} segment per path parameter.
	path   string
	params []string
	query  []string
	body   bool
	// perm is the operation's x-permission: "" for any signed-in caller.
	perm auth.Permission
}

// maxToolOutput bounds what one tool call returns to the model.
const maxToolOutput = 64 << 10

var mcpTools = sync.OnceValues(func() ([]mcpTool, error) { return loadMCPTools(api.OpenAPI, mcpOperations) })

// loadMCPTools reads ops out of the API contract.
func loadMCPTools(contract []byte, ops []string) ([]mcpTool, error) {
	var spec map[string]any
	if err := yaml.Unmarshal(contract, &spec); err != nil {
		return nil, fmt.Errorf("httpapi: the API contract could not be read: %w", err)
	}
	components, _ := spec["components"].(map[string]any)
	byID := map[string]mcpTool{}
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
			t, err := toolOf(id, strings.ToUpper(method), path, op, components)
			if err != nil {
				return nil, err
			}
			byID[id] = t
		}
	}
	out := make([]mcpTool, 0, len(ops))
	for _, id := range ops {
		t, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("httpapi: the API contract has no operation %q for the MCP", id)
		}
		out = append(out, t)
	}
	return out, nil
}

// toolOf is one operation as a tool: path and query parameters as named
// arguments, and the request body, its schema resolved, as "body".
func toolOf(id, method, path string, op, components map[string]any) (mcpTool, error) {
	t := mcpTool{method: method, path: path}
	if perm, _ := op["x-permission"].(string); perm != "" && perm != "authenticated" {
		t.perm = auth.Permission(perm)
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
	if t.perm != "" {
		description += "\n\nNeeds " + string(t.perm) + "."
	}
	readOnly := method == http.MethodGet
	t.tool = &mcp.Tool{
		Name:        snake(id),
		Title:       summary,
		Description: description,
		InputSchema: input,
		Annotations: &mcp.ToolAnnotations{
			Title:           summary,
			ReadOnlyHint:    readOnly,
			DestructiveHint: ptr(method == http.MethodDelete),
			IdempotentHint:  readOnly || method == http.MethodPut || method == http.MethodDelete,
			OpenWorldHint:   ptr(false),
		},
	}
	return t, nil
}

func ptr[T any](v T) *T { return &v }

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
// Schema an MCP client can read.
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

// dispatchedKey carries the caller of a call the MCP dispatches to a route.
// Only dispatch puts it in a request's context, and nothing outside the
// process can, so authed takes it as signed in: the caller signed in at
// /api/mcp.
type dispatchedKey struct{}

// callerOf is who a request is from: the caller a dispatched call carries,
// or whoever signed in.
func (s *Server) callerOf(r *http.Request) (auth.Principal, error) {
	if who, ok := r.Context().Value(dispatchedKey{}).(auth.Principal); ok {
		return who, nil
	}
	return s.Authn.Authenticate(r)
}

// mcpEndpoint serves the humagen MCP for one signed-in caller: a server of
// the tools they may use, stateless, answering in JSON.
func (s *Server) mcpEndpoint(routes http.Handler) handler {
	return func(w http.ResponseWriter, r *http.Request, who auth.Principal) {
		tools, err := mcpTools()
		if err != nil {
			s.Log.Error("httpapi: the MCP's tools could not be read", "error", err)
			fail(w, http.StatusInternalServerError, "the MCP is not available")
			return
		}
		srv := mcp.NewServer(&mcp.Implementation{Name: "humagen", Title: "Humagen", Version: "1"}, &mcp.ServerOptions{Instructions: mcpInstructions})
		for _, t := range tools {
			if t.perm != "" && !who.Can(t.perm) {
				continue
			}
			srv.AddTool(t.tool, s.callTool(routes, who, t))
		}
		mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}).ServeHTTP(w, r)
	}
}

// callTool calls t's operation as who, through its own route.
func (s *Server) callTool(routes http.Handler, who auth.Principal, t mcpTool) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := map[string]any{}
		if len(req.Params.Arguments) > 0 {
			if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
				return toolError("the arguments are not a JSON object"), nil
			}
		}
		path := t.path
		for _, name := range t.params {
			v, _ := args[name].(string)
			if strings.TrimSpace(v) == "" {
				return toolError(name + " is required"), nil
			}
			path = strings.Replace(path, "{"+name+"}", url.PathEscape(v), 1)
		}
		q := url.Values{}
		for _, name := range t.query {
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
		var body io.Reader = http.NoBody
		if b, ok := args["body"]; ok && t.body {
			raw, err := json.Marshal(b)
			if err != nil {
				return toolError("the body is not JSON"), nil
			}
			body = bytes.NewReader(raw)
		}
		status, out := dispatch(ctx, routes, who, t.method, path, q, body)
		if status >= http.StatusBadRequest {
			return toolError(errorText(status, out)), nil
		}
		text := string(out)
		if len(text) > maxToolOutput {
			text = text[:maxToolOutput] + "\n… (cut: narrow the call to read the rest)"
		}
		if text == "" {
			text = http.StatusText(status)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil
	}
}

func toolError(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: msg}}}
}

// errorText is what the API said was wrong.
func errorText(status int, out []byte) string {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(out, &e) == nil && e.Error != "" {
		return fmt.Sprintf("%d %s: %s", status, http.StatusText(status), e.Error)
	}
	return fmt.Sprintf("%d %s", status, http.StatusText(status))
}

// dispatch serves one API call in-process as who, through routes, the
// server's own mux: the route authorizes, validates, audits and logs it as it
// would the same call from outside.
func dispatch(ctx context.Context, routes http.Handler, who auth.Principal, method, path string, q url.Values, body io.Reader) (int, []byte) {
	target := path
	if len(q) > 0 {
		target += "?" + q.Encode()
	}
	r, err := http.NewRequestWithContext(context.WithValue(ctx, dispatchedKey{}, who), method, target, body)
	if err != nil {
		return http.StatusBadRequest, []byte(`{"error":"the call could not be made"}`)
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json")
	rec := &recorder{header: http.Header{}, status: http.StatusOK}
	routes.ServeHTTP(rec, r)
	return rec.status, rec.body.Bytes()
}

// recorder keeps a dispatched call's answer, bounded.
type recorder struct {
	header http.Header
	status int
	wrote  bool
	body   bytes.Buffer
}

func (r *recorder) Header() http.Header { return r.header }

func (r *recorder) WriteHeader(status int) {
	if !r.wrote {
		r.status, r.wrote = status, true
	}
}

func (r *recorder) Write(b []byte) (int, error) {
	r.WriteHeader(http.StatusOK)
	if room := maxToolOutput + 1 - r.body.Len(); room > 0 {
		r.body.Write(b[:min(len(b), room)])
	}
	return len(b), nil
}
