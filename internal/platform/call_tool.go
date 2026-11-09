package platform

import (
	"fmt"
	"strings"

	"github.com/urmzd/orchagent/flow/turn/native"
	saigetypes "github.com/urmzd/saige/agent/types"
)

// ToolCallAgent is the tool agents called other agents with before the agent
// tool (ToolAgent) took its place. A revision that still names it is read as
// granting the agent tool (resolveOperations).
const ToolCallAgent = "call_agent"

// agentProperty is the call tool's agent argument: one of targets. With no
// targets there is no enum, since an empty one is not a schema every model
// accepts, and the call path refuses whatever is named.
func agentProperty(targets []string) saigetypes.PropertyDef {
	if len(targets) == 0 {
		return saigetypes.PropertyDef{Type: "string", Description: "No agent accepts calls from you right now"}
	}
	return saigetypes.PropertyDef{Type: "string", Enum: targets}
}

// callTool is the agent tool. It is an native.Approving tool: a call the
// callee's policy holds for a person parks the run on an approval question
// addressed to the policy's approvers.
type callTool struct {
	*saigetypes.ToolFunc
	p      *Platform
	tenant string
	caller string
}

// maxExcerpt bounds how much of a call's request its approval prompt quotes.
const maxExcerpt = 200

// Approval is native.Approving. It reads only the call's arguments and the
// callee's stored policy, so it answers the same for the same arguments.
func (t *callTool) Approval(c saigetypes.ToolCallPart) (native.ApprovalRequest, bool) {
	callee, _ := c.Arguments["agent"].(string)
	if callee == "" {
		return native.ApprovalRequest{}, false
	}
	pol, _, err := t.p.CallPolicies.Get(t.tenant, callee)
	if err != nil || !pol.Admits(t.caller) {
		// Refused when it runs, with the callee's reason, or with why its
		// policy could not be read. Nobody is asked about a call that
		// cannot happen.
		return native.ApprovalRequest{}, false
	}
	needed, approvers, self, expires := t.p.callApproval(t.tenant, t.caller, callee)
	if !needed {
		return native.ApprovalRequest{}, false
	}
	prompt := fmt.Sprintf("Allow %s to call %s?", t.caller, callee)
	if excerpt := requestExcerpt(c.Arguments); excerpt != "" {
		prompt += " It asks: " + excerpt
	}
	// The callee's policy says how long a held call waits before it is denied.
	return native.ApprovalRequest{Prompt: prompt, Approvers: approvers, SelfApproval: self, ExpiresIn: expires}, true
}

// requestExcerpt is the last text the call sends (an agent call's title and
// brief), on one line and clipped.
func requestExcerpt(args map[string]any) string {
	messages, _ := args["messages"].([]any)
	text := ""
	if brief, _ := args["brief"].(string); strings.TrimSpace(brief) != "" {
		text = brief
		if title, _ := args["title"].(string); strings.TrimSpace(title) != "" {
			text = title + ": " + brief
		}
	}
	for _, m := range messages {
		msg, _ := m.(map[string]any)
		parts, _ := msg["content"].([]any)
		for _, part := range parts {
			if p, _ := part.(map[string]any); p != nil {
				if s, ok := p["text"].(string); ok && strings.TrimSpace(s) != "" {
					text = s
				}
			}
		}
	}
	text = strings.Join(strings.Fields(text), " ")
	if r := []rune(text); len(r) > maxExcerpt {
		text = string(r[:maxExcerpt]) + "..."
	}
	return text
}

var _ native.Approving = (*callTool)(nil)
