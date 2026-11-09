package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/orchagent/flow/event"
	"github.com/urmzd/orchagent/flow/runtime"
	saigetypes "github.com/urmzd/saige/agent/types"
)

// The agent tool, as a run calls it: one tool call brings in another agent
// for part of the work. The registry declares it (systemtools_agent.go);
// here a run's tool set offers it, held to the agents the caller may call,
// and runAgentCall runs each call as a child run of the caller that carries
// the call's id (runs.Run.CallID), so the child's activity nests under the
// call and its answer is the call's result. Stopping the caller stops the
// child, and a call the callee's policy holds waits for a person, as for
// call_agent.

// agentTool is the agent tool for one run.
func (p *Platform) agentTool(parent runtime.Request, report func(event.Action) error) saigetypes.Tool {
	targets := p.callTargets(parent.Tenant, parent.RunID, parent.Definition.Spec.Name, parent.Definition.Spec.Access.Spawn)
	description := "Bring in another agent for part of the work."
	if def, ok := p.SystemTool(ToolAgent); ok {
		description = def.Description
	}
	if len(targets) == 0 {
		description += " No agent accepts calls from you right now, so any call will be refused."
	} else {
		description += " Agents you may call: " + strings.Join(targets, ", ") + "."
	}
	caller := parent.Definition.Spec.Name
	return &callTool{p: p, tenant: parent.Tenant, caller: caller, ToolFunc: &saigetypes.ToolFunc{
		Def: saigetypes.ToolDef{Name: ToolAgent, Description: description, Parameters: saigetypes.ParameterSchema{
			Type: "object", Required: []string{"agent", "title", "brief"},
			Properties: map[string]saigetypes.PropertyDef{
				"agent": agentProperty(targets),
				"title": {Type: "string", Description: fmt.Sprintf("A few imperative words naming the work, shown on its card, at most %d characters", MaxAgentCallTitle)},
				"brief": {Type: "string", Description: "The context the agent needs and what done looks like. Nothing else of your conversation reaches it."},
			},
		}},
		Fn: func(ctx context.Context, args map[string]any) (string, error) {
			raw, err := json.Marshal(args)
			if err != nil {
				return "", err
			}
			info, _ := saigetypes.ToolCallInfoFromContext(ctx)
			if info.ID == "" {
				return "", fmt.Errorf("%w: an agent call needs its tool call's id", call.ErrInvalid)
			}
			id, err := p.toolUseIDFor(ctx, parent.Tenant, parent.RunID, info.ID)
			if err != nil {
				return "", err
			}
			run, err := p.runRecordOf(parent.Tenant, parent.RunID)
			if err != nil {
				return "", err
			}
			message := run.Thread
			if run.Message != "" {
				message = run.Thread + ":" + run.Message
			}
			if callee, _ := args["agent"].(string); callee != "" {
				// A call the callee's policy holds runs only when a person
				// approved this very tool call (callTool.Approval).
				if needed, _, _, _ := p.callApproval(parent.Tenant, caller, callee); needed && p.approvedCallTo(parent.Tenant, parent.RunID, info.ID, callee) {
					ctx = withApprovedCall(ctx, callee)
				}
			}
			def, ok := p.SystemTool(ToolAgent)
			if !ok {
				return "", errors.New("agent calls are not available here")
			}
			ctx = context.WithValue(ctx, agentCallReportKey{}, report)
			out, err := def.Call(ctx, SystemCall{Who: p.runPrincipal(run.Tenant, run.Actor), Message: message, Run: parent.RunID, ToolUseID: id}, raw)
			if err != nil {
				return "", err
			}
			if out.IsError {
				return "", errors.New(out.Text)
			}
			return out.Text, nil
		},
	}}
}

type agentCallReportKey struct{}

// runAgentCall is the AgentCaller: the agent call c makes, run as a child of
// the run that called, keyed by the call's id so a retried turn joins the
// child it already started, waited on, and answered with the child's answer.
// A refusal (the agent is not one the caller may call, the callee's policy
// says no, the tree is out of depth or budget) is the call's error result,
// for the caller to read. Only a run makes agent calls.
func (p *Platform) runAgentCall(ctx context.Context, c SystemCall, a AgentCall) (SystemResult, error) {
	if c.Run == "" || c.ToolUseID == "" {
		return SystemResult{IsError: true, Text: "agent calls are made by a run; to start one, use send_messages"}, nil
	}
	report, _ := ctx.Value(agentCallReportKey{}).(func(event.Action) error)
	if report == nil {
		report = func(event.Action) error { return nil }
	}
	req := call.Request{Key: c.ToolUseID, Agent: a.Agent, Text: a.Brief, Mode: call.Subagent, CallID: c.ToolUseID}
	answer, err := p.waitForCall(ctx, runtime.Request{Tenant: c.Who.Tenant, RunID: c.Run}, req, ToolAgent, report)
	if err != nil {
		return SystemResult{IsError: true, Text: err.Error()}, nil
	}
	return SystemResult{Text: answer}, nil
}

// toolUseIDFor is the API's id for the call the provider named callID in a
// run. The runtime records a call before it runs it, and the journal keeps
// what it records a moment later, so this waits briefly for it.
func (p *Platform) toolUseIDFor(ctx context.Context, tenant, run, callID string) (string, error) {
	deadline := time.Now().Add(toolUseIDWait)
	for {
		if held, err := p.Journal.Events(tenant, []string{run}); err == nil {
			if n, ok := callSeqs(held)[callID]; ok {
				return toolUseID(run, n), nil
			}
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("the call %q was not recorded on its run", callID)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// toolUseIDWait bounds how long a call waits for its run to record it.
const toolUseIDWait = 5 * time.Second

// runPrincipal is who a run acts as when it calls a system tool: the person
// it acts for, with what the workspace's policy grants them, or the agent
// that started it, with nothing granted.
func (p *Platform) runPrincipal(tenant string, actor event.Actor) auth.Principal {
	if actor.User != "" {
		return p.byPolicy(tenant, actor.User)
	}
	return auth.Principal{Tenant: tenant, Subject: actor.ID, Kind: auth.Machine}
}
