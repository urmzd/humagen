package platform_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/runs"
	saigetypes "github.com/urmzd/saige/agent/types"
)

// An agent call runs the agent as a child of the run that called, under the
// call's id: the child carries it, and its answer is the call's result.
func TestAnAgentCallRunsTheAgentUnderItsCallID(t *testing.T) {
	c := &cast{says: map[string][][]saigetypes.Delta{
		"lead":   {tagging("helper", "Check the service and say if it is up."), say("The helper says it is up.")},
		"helper": {say("It is up.")},
	}, spawn: map[string][]string{"lead": {"helper"}}}
	p := stage(t, c, platform.Config{Limits: call.Limits{MaxDepth: 2, MaxFanOut: 4}})
	first, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "agent-call", Agent: "lead", Text: "Is the service up?"})
	if err != nil {
		t.Fatal(err)
	}
	lead := await(t, p, first.ID, runs.Completed)
	helper := called(t, p, lead.ID, "helper", runs.Completed)
	if want := "htu_" + lead.ID + "_1"; helper.CallID != want || helper.Parent != lead.ID || helper.Mode != call.Subagent {
		t.Fatalf("the helper run = call %q (want %q), parent %q, mode %q", helper.CallID, want, helper.Parent, helper.Mode)
	}
	if lead.Answer != "The helper says it is up." || lead.CallID != "" {
		t.Fatalf("lead = answer %q, call %q", lead.Answer, lead.CallID)
	}
	requests := modelFor(t, c, "helper").Requests()
	if len(requests) != 1 || !strings.Contains(fmt.Sprint(requests[0].Messages), "Check the service and say if it is up.") {
		t.Fatalf("the helper was asked %+v", requests)
	}
	result := false
	for _, m := range modelFor(t, c, "lead").Requests()[1].Messages {
		for _, r := range saigetypes.Each[saigetypes.ToolResultPart](m) {
			result = result || strings.Contains(fmt.Sprint(r.Parts), "It is up.")
		}
	}
	if !result {
		t.Fatal("the helper's answer was not the call's result")
	}
}

// A client of the humagen MCP makes no agent call of its own: it is pointed
// at posting a message, which starts a run with its own response.
func TestAnAgentCallFromOutsideARunIsPointedAtSendingAMessage(t *testing.T) {
	p := start(t, call.Limits{})
	agent, ok := p.SystemTool(platform.ToolAgent)
	if !ok {
		t.Fatal("the agent tool is not in the registry")
	}
	raw, _ := json.Marshal(map[string]string{"agent": "helper", "title": "Check it", "brief": "Check the service."})
	res, err := agent.Call(t.Context(), platform.SystemCall{Who: admin}, raw)
	if err != nil || !res.IsError || !strings.Contains(res.Text, "send_messages") {
		t.Fatalf("an agent call from outside a run = %+v, %v", res, err)
	}
}
