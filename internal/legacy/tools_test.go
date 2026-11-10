package legacy_test

import (
	"slices"
	"testing"

	"github.com/urmzd/humagen/internal/legacy"
)

// Every earlier way to call an agent reads as the agent tool, and nothing
// else does.
func TestEarlierNamesOfTheAgentToolReadAsIt(t *testing.T) {
	for name, want := range map[string]string{
		"call_agent": "agent", "delegate_task": "agent", "agent": "agent",
		"tag_helper": "tag_helper", "file_read": "file_read", "session_call": "session_call",
	} {
		if got := legacy.ToolName(name); got != want {
			t.Errorf("%s reads as %s, want %s", name, got, want)
		}
	}
	if !legacy.IsAgentTool("delegate_task") || !legacy.IsAgentTool("call_agent") || legacy.IsAgentTool("agent") || legacy.IsAgentTool("tag_helper") {
		t.Error("the earlier names of the agent tool are call_agent and delegate_task")
	}
	if agent, ok := legacy.TagTarget("tag_helper"); !ok || agent != "helper" {
		t.Errorf("tag_helper called %q, %v", agent, ok)
	}
	if _, ok := legacy.TagTarget("tag_"); ok {
		t.Error("tag_ names no agent")
	}
}

// A list of tool names, such as a skill's restriction, covers a call when it
// names the agent tool by any name, or the callee's own.
func TestAListNamesTheAgentToolByAnyName(t *testing.T) {
	for _, tc := range []struct {
		names  []string
		callee string
		want   bool
	}{
		{[]string{"file_read", "agent"}, "helper", true},
		{[]string{"call_agent"}, "helper", true},
		{[]string{"delegate_task"}, "helper", true},
		{[]string{"tag_helper"}, "helper", true},
		{[]string{"tag_other"}, "helper", false},
		{[]string{"file_read"}, "helper", false},
		{nil, "helper", false},
	} {
		if got := legacy.AgentToolNamed(tc.names, tc.callee); got != tc.want {
			t.Errorf("%v covers a call to %s: %v, want %v", tc.names, tc.callee, got, tc.want)
		}
	}
	if !legacy.NamesAgentTool([]string{"tag_other"}) || legacy.NamesAgentTool([]string{"file_read", "session_call"}) {
		t.Error("NamesAgentTool is any name of the agent tool")
	}
}

// A saved spec keeps loading: the earlier names leave its tools, their
// approvals move to the agent tool, and an approval of one agent's calls is
// held apart.
func TestASavedSpecsEarlierNamesAreReadAsTheAgentTool(t *testing.T) {
	spawn := []string{"helper", "worker"}
	for _, tc := range []struct {
		name                         string
		tools, approvals, spawn      []string
		wantTools, wantApprovals, on []string
	}{
		{name: "nothing earlier is left as it is", tools: []string{"file_read"}, approvals: []string{"file_read"}, spawn: spawn, wantTools: []string{"file_read"}, wantApprovals: []string{"file_read"}},
		{name: "delegate_task", tools: []string{"file_read", "delegate_task"}, approvals: []string{"delegate_task"}, spawn: spawn, wantTools: []string{"file_read"}, wantApprovals: []string{"agent"}},
		{name: "call_agent", tools: []string{"call_agent"}, approvals: []string{"call_agent"}, spawn: spawn, wantTools: []string{}, wantApprovals: []string{"agent"}},
		{name: "both, and the agent tool, once", tools: []string{"call_agent", "delegate_task"}, approvals: []string{"call_agent", "agent", "delegate_task", "file_read"}, spawn: spawn, wantTools: []string{}, wantApprovals: []string{"agent", "file_read"}},
		{name: "one agent's calls", tools: []string{"tag_helper", "file_read"}, approvals: []string{"tag_helper", "file_read"}, spawn: spawn, wantTools: []string{"file_read"}, wantApprovals: []string{"file_read"}, on: []string{"helper"}},
		{name: "a tag_ tool of no callable agent is another tool", tools: []string{"tag_issue"}, approvals: []string{"tag_issue"}, spawn: spawn, wantTools: []string{"tag_issue"}, wantApprovals: []string{"tag_issue"}},
		{name: "no agent to call, nothing to approve", tools: []string{"delegate_task"}, approvals: []string{"delegate_task"}, wantTools: []string{}, wantApprovals: []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tools, approvals, held := legacy.WithoutAgentTools(tc.tools, tc.approvals, tc.spawn)
			if !slices.Equal(tools, tc.wantTools) || !slices.Equal(approvals, tc.wantApprovals) || !slices.Equal(held, tc.on) {
				t.Fatalf("tools %v, approvals %v, held %v", tools, approvals, held)
			}
		})
	}
}
