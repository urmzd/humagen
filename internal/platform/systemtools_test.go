package platform

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/api"
	"github.com/urmzd/humagen/pkg/auth"
)

// The registry's operations come from the API contract, and an operation the
// contract lacks is refused rather than offered.
func TestTheContractToolsComeFromTheContract(t *testing.T) {
	tools, err := loadContractTools(api.OpenAPI, SystemOperations)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != len(SystemOperations) {
		t.Fatalf("%d tools for %d operations", len(tools), len(SystemOperations))
	}
	for _, tl := range tools {
		if tl.def.Name == "" || tl.def.Description == "" || tl.def.Input["type"] != "object" || tl.method == "" || tl.path == "" {
			t.Errorf("tool %+v is incomplete", tl.def)
		}
	}
	if got := snake("listAgentRevisions"); got != "list_agent_revisions" {
		t.Errorf("snake = %q", got)
	}
	if _, err := loadContractTools(api.OpenAPI, []string{"noSuchOperation"}); err == nil {
		t.Fatal("an operation the contract lacks was offered")
	}
}

// The agent call checks its arguments, then hands the call, as made, to the
// run machinery installed behind it.
func TestTheAgentCallHandsOnWhatItIsGiven(t *testing.T) {
	p, _ := configured(t, Config{})
	ctx := context.Background()
	agent, ok := p.SystemTool(ToolAgent)
	get, okGet := p.SystemTool(ToolGetContext)
	if !ok || !okGet || agent.Call == nil || get.Call == nil || agent.Permission != auth.RunsWrite || get.Permission != auth.RunsRead || !get.ReadOnly {
		t.Fatalf("agent = %+v, get_context = %+v", agent, get)
	}
	call := SystemCall{Who: admin("acme"), Message: "msg-1", Run: "run-1", ToolUseID: "htu_run-1_3"}
	p.SetAgentCaller(nil)
	if res, _ := agent.Call(ctx, call, json.RawMessage(`{"agent":"security","title":"Review the auth change","brief":"Check it."}`)); !res.IsError || !strings.Contains(res.Text, "not available") {
		t.Fatalf("with nothing behind it = %+v", res)
	}
	for args, want := range map[string]string{
		`{"title":"Review","brief":"x"}`:   "name the agent",
		`{"agent":"security","brief":"x"}`: "give the call a title",
		`{"agent":"security","title":"` + strings.Repeat("r", MaxAgentCallTitle+1) + `","brief":"x"}`: "at most 60 characters",
		`{"agent":"security","title":"Review"}`:                                                       "give the agent a brief",
	} {
		if res, _ := agent.Call(ctx, call, json.RawMessage(args)); !res.IsError || !strings.Contains(res.Text, want) {
			t.Errorf("%s = %+v, want %q", args, res, want)
		}
	}
	var got AgentCall
	var gotCall SystemCall
	p.SetAgentCaller(func(_ context.Context, c SystemCall, a AgentCall) (SystemResult, error) {
		got, gotCall = a, c
		return SystemResult{Text: "No issues."}, nil
	})
	res, err := agent.Call(ctx, call, json.RawMessage(`{"agent":" security ","title":"Review the auth change","brief":"Check the token handling."}`))
	if err != nil || res.Text != "No issues." || got.Agent != "security" || got.Brief != "Check the token handling." || gotCall.ToolUseID != "htu_run-1_3" {
		t.Fatalf("agent call = %+v, %v; handed %+v as %+v", res, err, got, gotCall)
	}
}

// SystemTool finds one tool of the registry by name, a contract operation or
// one of the platform's own, with the definition SystemTools lists.
func TestASystemToolIsFoundByName(t *testing.T) {
	p, _ := configured(t, Config{})
	all, err := p.SystemTools()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range all {
		got, ok := p.SystemTool(want.Name)
		if !ok || got.Name != want.Name || got.Description != want.Description || got.Permission != want.Permission || got.ReadOnly != want.ReadOnly || got.Call == nil {
			t.Errorf("SystemTool(%q) = %+v, %v; listed as %+v", want.Name, got, ok, want)
		}
	}
	if list, ok := p.SystemTool("list_agents"); !ok || list.Permission != auth.AgentsRead || !list.ReadOnly {
		t.Errorf("list_agents = %+v, %v", list, ok)
	}
	if _, ok := p.SystemTool("no_such_tool"); ok {
		t.Error("a tool the registry lacks was found")
	}
	if _, ok := p.SystemTool("echo"); ok {
		t.Fatal("echo was found before it was registered")
	}
	if err := p.RegisterSystemTool(SystemTool{Name: "echo", Input: map[string]any{"type": "object"}, Call: func(context.Context, SystemCall, json.RawMessage) (SystemResult, error) {
		return SystemResult{Text: "echo"}, nil
	}}); err != nil {
		t.Fatal(err)
	}
	if echo, ok := p.SystemTool("echo"); !ok || echo.Call == nil {
		t.Fatalf("a registered tool was not found: %+v, %v", echo, ok)
	}
}

// The messages API is in the registry, each operation needing the permission
// the contract gives it, and a query parameter the contract requires is a
// required argument.
func TestTheMessagesOperationsAreSystemTools(t *testing.T) {
	p, _ := configured(t, Config{})
	for name, perm := range map[string]auth.Permission{
		"send_messages":       auth.RunsWrite,
		"list_messages":       auth.RunsRead,
		"send_messages_under": auth.RunsWrite,
		"get_message":         auth.RunsRead,
		"rate_message":        auth.RunsRead,
		"stop_message":        auth.RunsWrite,
		"get_message_context": auth.RunsRead,
		"trace_message":       auth.RunsRead,
	} {
		if tl, ok := p.SystemTool(name); !ok || tl.Permission != perm {
			t.Errorf("%s = %+v, %v; want it with %s", name, tl, ok, perm)
		}
	}
	list, _ := p.SystemTool("list_messages")
	if required, _ := list.Input["required"].([]string); !slices.Contains(required, "roots") {
		t.Errorf("list_messages requires %v, not roots", list.Input["required"])
	}
	under, _ := p.SystemTool("send_messages_under")
	if required, _ := under.Input["required"].([]string); !slices.Equal(required, []string{"id", "body"}) {
		t.Errorf("send_messages_under requires %v", under.Input["required"])
	}
}

// What a call does maps onto the response stream's own events, and a step
// that could not be one is refused.
func TestProgressBecomesTheResponseStreamsEvents(t *testing.T) {
	event, body, err := StreamEventOf(SystemProgress{Kind: ProgressNested, ToolUseID: "htu_r_1", AgentRun: "run-2", Depth: 1, Event: json.RawMessage(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi"}}`)})
	if err != nil || event != EventNested || body["parent_tool_use_id"] != "htu_r_1" || body["agent_run_id"] != "run-2" || body["depth"] != 1 {
		t.Fatalf("nested = %s %v %v", event, body, err)
	}
	event, body, err = StreamEventOf(SystemProgress{Kind: ProgressStatus, ToolUseID: "htu_r_2", Path: []string{"htu_r_1", "htu_r_2"}, Status: ToolAwaitingApproval, Title: "Review the auth change", Question: &ProgressQuestion{Kind: "approval", Prompt: "Post the comment?", Who: []string{"role:admin"}}, Elapsed: 1500 * time.Millisecond})
	if err != nil || event != EventToolStatus || body["status"] != ToolAwaitingApproval || body["elapsed_ms"] != int64(1500) || body["question"] == nil {
		t.Fatalf("status = %s %v %v", event, body, err)
	}
	if _, body, _ = StreamEventOf(SystemProgress{Kind: ProgressStatus, ToolUseID: "htu_r_1", Status: ToolDone}); !slices.Equal(body["path"].([]string), []string{"htu_r_1"}) {
		t.Fatalf("a root call's path = %v", body["path"])
	}
	event, body, err = StreamEventOf(SystemProgress{Kind: ProgressLine, ToolUseID: "htu_r_1", Text: "Reading the diff"})
	if err != nil || event != EventProgress || body["text"] != "Reading the diff" || body["parent_tool_use_id"] != "htu_r_1" {
		t.Fatalf("line = %s %v %v", event, body, err)
	}
	for name, bad := range map[string]SystemProgress{
		"nested at depth 0": {Kind: ProgressNested, ToolUseID: "h", AgentRun: "r", Event: json.RawMessage(`{}`)},
		"nested, no event":  {Kind: ProgressNested, ToolUseID: "h", AgentRun: "r", Depth: 1},
		"an unknown status": {Kind: ProgressStatus, ToolUseID: "h", Status: "thinking"},
		"an empty line":     {Kind: ProgressLine, Text: " "},
		"an unknown kind":   {Kind: "other"},
	} {
		if _, _, err := StreamEventOf(bad); err == nil {
			t.Errorf("%s was mapped", name)
		}
	}
}
