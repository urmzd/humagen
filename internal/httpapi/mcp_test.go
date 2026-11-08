package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
)

// bearer signs every request it sends with a token.
type bearer struct {
	token string
	next  http.RoundTripper
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return b.next.RoundTrip(r)
}

// mcpSession connects an MCP client to the fixture's /api/mcp as tok.
func (f *fixture) mcpSession(t *testing.T, tok string) *mcp.ClientSession {
	t.Helper()
	c := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	cs, err := c.Connect(t.Context(), &mcp.StreamableClientTransport{
		Endpoint:   f.srv.URL + "/api/mcp",
		HTTPClient: &http.Client{Transport: bearer{token: tok, next: http.DefaultTransport}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func toolNames(t *testing.T, cs *mcp.ClientSession) []string {
	t.Helper()
	res, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	return names
}

func textOf(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if t, ok := c.(*mcp.TextContent); ok {
			b.WriteString(t.Text)
		}
	}
	return b.String()
}

// The MCP offers each caller the operations their permissions allow, named
// and described by the API contract.
func TestTheMCPOffersOnlyWhatTheCallerMayDo(t *testing.T) {
	f := newFixture(t)
	admin := f.person(t, "acme", "ada", "Ada", "humagen-admins")
	viewer := f.person(t, "acme", "vic", "Vic", "humagen-viewers")

	all := toolNames(t, f.mcpSession(t, admin))
	for _, want := range []string{"list_agents", "revise_agent", "update_settings", "set_model_provider", "run_eval_suite", "create_schedule"} {
		if !slices.Contains(all, want) {
			t.Errorf("an admin is not offered %s: %v", want, all)
		}
	}
	if len(all) != len(platform.SystemOperations) {
		t.Errorf("an admin is offered %d tools, the registry has %d", len(all), len(platform.SystemOperations))
	}
	read := toolNames(t, f.mcpSession(t, viewer))
	if !slices.Contains(read, "list_agents") || !slices.Contains(read, "list_alerts") {
		t.Errorf("a viewer cannot read: %v", read)
	}
	for _, write := range []string{"revise_agent", "update_settings", "set_model_provider", "create_alert_rule"} {
		if slices.Contains(read, write) {
			t.Errorf("a viewer is offered %s", write)
		}
	}
	// Nobody signed in reaches it.
	if res, _ := f.do(t, nil, "POST", "/api/mcp", "", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, "Content-Type", "application/json", "Accept", "application/json, text/event-stream"); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unsigned = %d", res.StatusCode)
	}
}

// A tool call is the API call, made as the caller: it reads and writes what
// the route does, and says what the route refuses.
func TestAnMCPToolCallIsTheAPICallAsTheCaller(t *testing.T) {
	f := newFixture(t)
	admin := f.person(t, "acme", "ada", "Ada", "humagen-admins")
	cs := f.mcpSession(t, admin)
	ctx := t.Context()

	created, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "create_agent", Arguments: map[string]any{
		"body": map[string]any{"name": "notes", "provider": "offline", "model": "offline", "system_prompt": "Take notes."},
	}})
	if err != nil || created.IsError {
		t.Fatalf("create_agent = %s, %v", textOf(created), err)
	}
	history, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "list_agent_revisions", Arguments: map[string]any{"name": "notes"}})
	if err != nil || history.IsError {
		t.Fatalf("list_agent_revisions = %s, %v", textOf(history), err)
	}
	var got struct {
		Revisions []struct {
			Rev       int    `json:"rev"`
			CreatedBy string `json:"created_by"`
		} `json:"revisions"`
	}
	if err := json.Unmarshal([]byte(textOf(history)), &got); err != nil || len(got.Revisions) != 1 || got.Revisions[0].CreatedBy != "ada" {
		t.Fatalf("history = %s, %v", textOf(history), err)
	}
	// What the route refuses comes back as the tool's error, in its words.
	stale, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "revise_agent", Arguments: map[string]any{
		"name": "notes",
		"body": map[string]any{"expected_rev": 7, "spec": map[string]any{"name": "notes", "provider": "offline", "model": "offline", "system_prompt": "Take notes well."}},
	}})
	if err != nil || !stale.IsError || !strings.HasPrefix(textOf(stale), "409") {
		t.Fatalf("a stale revise = %s, %v", textOf(stale), err)
	}
	missing, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "list_agent_revisions", Arguments: map[string]any{}})
	if err != nil || !missing.IsError || !strings.Contains(textOf(missing), "name is required") {
		t.Fatalf("no name = %s, %v", textOf(missing), err)
	}
	// A viewer is not offered a write, and calling it anyway is no way round.
	viewer := f.mcpSession(t, f.person(t, "acme", "vic", "Vic", "humagen-viewers"))
	if res, err := viewer.CallTool(ctx, &mcp.CallToolParams{Name: "update_settings", Arguments: map[string]any{"body": map[string]any{}}}); err == nil && !res.IsError {
		t.Fatalf("a viewer changed the settings: %s", textOf(res))
	}
}

// Every tool of the contract is served by a route that needs the permission
// the contract gives its operation: a caller who holds nothing is refused by
// the route itself, naming that permission.
func TestEveryContractToolIsItsRouteWithItsPermission(t *testing.T) {
	f := newFixture(t)
	tools, err := f.p.SystemTools()
	if err != nil {
		t.Fatal(err)
	}
	nobody := auth.Principal{Subject: "nobody", Tenant: "acme", Kind: auth.User}
	for _, tl := range tools {
		if tl.Permission == "" {
			t.Errorf("%s needs no permission", tl.Name)
			continue
		}
		args := map[string]any{}
		required, _ := tl.Input["required"].([]string)
		for _, name := range required {
			if name != "body" {
				args[name] = "x"
			}
		}
		if props, _ := tl.Input["properties"].(map[string]any); props["body"] != nil {
			args["body"] = map[string]any{}
		}
		raw, _ := json.Marshal(args)
		res, err := tl.Call(context.Background(), platform.SystemCall{Who: nobody}, raw)
		if err != nil || !res.IsError || !strings.HasPrefix(res.Text, "403") || !strings.Contains(res.Text, "you need "+string(tl.Permission)) {
			t.Errorf("%s = %+v, %v, want 403 for %s", tl.Name, res, err, tl.Permission)
		}
		if tl.Description == "" || tl.Input == nil {
			t.Errorf("%s has no description or input", tl.Name)
		}
	}
}

// A tool the platform registers itself is offered and called like the
// contract's: to those its permission admits, as the caller.
func TestARegisteredSystemToolIsOfferedAndCalledAsTheCaller(t *testing.T) {
	f := newFixture(t)
	var called platform.SystemCall
	err := f.p.RegisterSystemTool(platform.SystemTool{
		Name: "echo_caller", Title: "Echo the caller", Description: "Says who called.",
		Input:      map[string]any{"type": "object", "properties": map[string]any{}},
		Permission: auth.AgentsWrite,
		Call: func(_ context.Context, c platform.SystemCall, _ json.RawMessage) (platform.SystemResult, error) {
			called = c
			return platform.SystemResult{Text: "called by " + c.Who.Subject}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.p.RegisterSystemTool(platform.SystemTool{Name: "list_agents", Input: map[string]any{"type": "object"}, Call: func(context.Context, platform.SystemCall, json.RawMessage) (platform.SystemResult, error) {
		return platform.SystemResult{}, nil
	}}); err == nil {
		t.Fatal("a name the contract has was registered again")
	}
	admin := f.mcpSession(t, f.person(t, "acme", "ada", "Ada", "humagen-admins"))
	res, err := admin.CallTool(t.Context(), &mcp.CallToolParams{Name: "echo_caller", Arguments: map[string]any{}})
	if err != nil || res.IsError || textOf(res) != "called by ada" || called.Who.Tenant != "acme" {
		t.Fatalf("echo_caller = %s, %v (%+v)", textOf(res), err, called)
	}
	if slices.Contains(toolNames(t, f.mcpSession(t, f.person(t, "acme", "vic", "Vic", "humagen-viewers"))), "echo_caller") {
		t.Fatal("a viewer is offered a tool that needs agents:write")
	}
}
