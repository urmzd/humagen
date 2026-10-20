package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/urmzd/fiber/pkg/agentrt"
	"github.com/urmzd/fiber/pkg/agentrt/payload"
	"github.com/urmzd/fiber/pkg/runtime"
	saigetypes "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/warp/pkg/call"
)

// ToolCallAgent is available whenever the pinned agent has a spawn grant.
// The same tool can wait for a helper or start independent background work.
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

func callParameters(targets []string) saigetypes.ParameterSchema {
	str := func(description string) saigetypes.PropertyDef {
		return saigetypes.PropertyDef{Type: "string", Description: description}
	}
	part := saigetypes.PropertyDef{Type: "object", Required: []string{"type"}, Properties: map[string]saigetypes.PropertyDef{
		"type": {Type: "string", Enum: []string{"text", "conversation_range", "artifact", "run_result"}, Description: "Discriminated content: text requires text; conversation_range requires thread/from/through run IDs; artifact requires id/version; run_result requires run. Other variant fields are rejected."},
		"text": str("Inline instructions, context, or an example"), "thread": str("Conversation ID"), "from": str("First turn's run ID, inclusive"), "through": str("Last turn's run ID, inclusive"),
		"id": str("Artifact ID"), "version": {Type: "integer", Description: "Exact artifact version, at least 1"}, "run": str("Completed run ID"),
	}}
	message := saigetypes.PropertyDef{Type: "object", Required: []string{"role", "content"}, Properties: map[string]saigetypes.PropertyDef{
		"role": {Type: "string", Enum: []string{"user", "assistant"}}, "content": {Type: "array", Items: &part},
	}}
	return saigetypes.ParameterSchema{Type: "object", Required: []string{"agent", "messages", "response_format"}, Properties: map[string]saigetypes.PropertyDef{
		"agent":    agentProperty(targets),
		"messages": {Type: "array", Items: &message, Description: "Ordered input, including examples and selected context. Nothing is inherited from your conversation. End with a user message. References are resolved and frozen before the helper starts."},
		"response_format": {Type: "object", Required: []string{"type"}, Description: "Closed union: text accepts only max_chars (default 16000); summary accepts only max_words (default 200); json_schema requires schema. The harness generates the return instruction and validates the result.", Properties: map[string]saigetypes.PropertyDef{
			"type": {Type: "string", Enum: []string{"text", "summary", "json_schema"}}, "max_chars": {Type: "integer"}, "max_words": {Type: "integer"}, "schema": {Type: "object", Description: "JSON Schema checked by saige; unsupported keywords are rejected"},
		}},
		"mode":  {Type: "string", Enum: []string{string(call.Subagent), string(call.Delegate)}, Description: "subagent (default) waits and returns the helper result to you. delegate starts independent work and returns a run reference; inspect its result later in execution history or the inbox."},
		"lease": {Type: "integer", Description: "Budget in micro-USD. Required to be positive when this run has a metered lease."},
	}}
}

func (p *Platform) callAgentTool(parent runtime.Request, report func(payload.Action) error) saigetypes.Tool {
	// Offered are the agents both sides agree to: the caller's spawn
	// allowlist, narrowed to the callees whose policy admits it. An agent
	// that would refuse is never offered.
	targets := p.callTargets(parent.Tenant, parent.Revision.Spec.Name, parent.Revision.Spec.Access.Spawn)
	described := "Targets: " + strings.Join(targets, ", ")
	if len(targets) == 0 {
		described = "No agent accepts calls from you right now, so any call will be refused."
	}
	caller := parent.Revision.Spec.Name
	return &callTool{p: p, tenant: parent.Tenant, caller: caller, ToolFunc: &saigetypes.ToolFunc{
		Def: saigetypes.ToolDef{Name: ToolCallAgent, Description: "Ask an allowed agent to perform helper work. Construct its complete input from ordered messages, examples and references. Choose the result format you need. You retain ownership of the user-facing answer. Use subagent to receive its result, or delegate for background work. " + described, Parameters: callParameters(targets)},
		Fn: func(ctx context.Context, args map[string]any) (string, error) {
			b, err := json.Marshal(args)
			if err != nil {
				return "", err
			}
			if len(b) > call.MaxInputBytes {
				return "", fmt.Errorf("%w: call input is too large", call.ErrInvalid)
			}
			var wire struct {
				Agent    string          `json:"agent"`
				Messages []call.Message  `json:"messages"`
				Format   json.RawMessage `json:"response_format"`
				Mode     call.Mode       `json:"mode,omitempty"`
				Lease    int64           `json:"lease,omitempty"`
			}
			if err := call.StrictDecode(b, &wire); err != nil {
				return "", err
			}
			// Decode through Input so the return contract's discriminator is checked
			// by the same codec used by HTTP and the durable snapshot.
			raw, err := json.Marshal(struct {
				Messages []call.Message  `json:"messages"`
				Format   json.RawMessage `json:"response_format"`
			}{wire.Messages, wire.Format})
			if err != nil {
				return "", err
			}
			var input call.Input
			if err := call.StrictDecode(raw, &input); err != nil {
				return "", err
			}
			if wire.Mode == "" {
				wire.Mode = call.Subagent
			}
			if wire.Mode != call.Subagent && wire.Mode != call.Delegate {
				return "", fmt.Errorf("%w: helper mode must be subagent or delegate", call.ErrInvalid)
			}
			key := callKey(string(wire.Mode), wire.Agent, string(raw)+fmt.Sprintf("\nlease:%d", wire.Lease))
			if info, ok := saigetypes.ToolCallInfoFromContext(ctx); ok && info.ID != "" {
				key = info.ID
			}
			req := call.Request{Key: key, Agent: wire.Agent, Input: &input, Mode: wire.Mode, Lease: wire.Lease}
			// A call the callee's policy holds runs only when a person
			// approved this very tool call. The runtime asked them (see
			// Approval); the note says what they decided.
			if needed, _, _, _ := p.callApproval(parent.Tenant, caller, wire.Agent); needed {
				info, _ := saigetypes.ToolCallInfoFromContext(ctx)
				if p.approvedCallTo(parent.Tenant, parent.RunID, info.ID, wire.Agent) {
					ctx = withApprovedCall(ctx, wire.Agent)
				}
			}
			if wire.Mode == call.Subagent {
				return p.waitForCall(ctx, parent, req, ToolCallAgent, report)
			}
			child, err := p.call(ctx, parent.Tenant, parent.RunID, req, true)
			if err != nil {
				return "", err
			}
			tell(ctx, report, ToolCallAgent, child, "Background helper started")
			data, err := json.Marshal(calledRun{Run: child.ID, Agent: child.Agent, Lifecycle: child.Lifecycle})
			return string(data), err
		},
	}}
}

// callTool is call_agent. It is an agentrt.Approving tool: a call the
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

// Approval is agentrt.Approving. It reads only the call's arguments and the
// callee's stored policy, so it answers the same for the same arguments.
func (t *callTool) Approval(c saigetypes.ToolCallPart) (agentrt.ApprovalRequest, bool) {
	callee, _ := c.Arguments["agent"].(string)
	if callee == "" {
		return agentrt.ApprovalRequest{}, false
	}
	pol, _ := t.p.CallPolicies.Get(t.tenant, callee)
	if !pol.Admits(t.caller) {
		// Refused when it runs, with the callee's reason. Nobody is asked
		// about a call that cannot happen.
		return agentrt.ApprovalRequest{}, false
	}
	needed, approvers, self, expires := t.p.callApproval(t.tenant, t.caller, callee)
	if !needed {
		return agentrt.ApprovalRequest{}, false
	}
	prompt := fmt.Sprintf("Allow %s to call %s?", t.caller, callee)
	if excerpt := requestExcerpt(c.Arguments); excerpt != "" {
		prompt += " It asks: " + excerpt
	}
	// The callee's policy says how long a held call waits before it is denied.
	return agentrt.ApprovalRequest{Prompt: prompt, Approvers: approvers, SelfApproval: self, ExpiresIn: expires}, true
}

// requestExcerpt is the last text the call sends, on one line and clipped.
func requestExcerpt(args map[string]any) string {
	messages, _ := args["messages"].([]any)
	text := ""
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

var _ agentrt.Approving = (*callTool)(nil)
