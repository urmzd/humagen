package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/threads"
)

// The platform's own system tools: the agent call, through which an answer
// brings in another agent for part of the work, and get_context, through
// which it pulls context from the place the message it answers came from.
// The agent call's execution is the platform's run machinery (AgentCaller);
// get_context is the platform's own (contextFor). The registry owns their
// definitions, and SystemProgress maps what a call does onto the response
// stream (StreamEventOf).

// ToolAgent calls another agent: its work streams under the call and its
// answer is the call's result. Parallel agents are parallel calls.
const ToolAgent = "agent"

// MaxAgentCallTitle bounds an agent call's title, which cards and
// checklists show.
const MaxAgentCallTitle = 60

// MaxAgentCallBrief bounds what an agent call hands the agent.
const MaxAgentCallBrief = 16 << 10

// AgentCall is one call of the agent tool.
type AgentCall struct {
	Agent string `json:"agent"`
	// Title names the work in a few words, imperative ("Review the auth
	// change"), at most MaxAgentCallTitle characters.
	Title string `json:"title"`
	// Brief is what the agent is told: the context it needs and what done
	// looks like.
	Brief string `json:"brief"`
}

// AgentCaller runs an agent call as c: the child run, its nested activity
// reported through c.Progress, its answer as the result.
type AgentCaller func(ctx context.Context, c SystemCall, a AgentCall) (SystemResult, error)

// SetAgentCaller installs the run machinery behind the agent tool.
func (p *Platform) SetAgentCaller(fn AgentCaller) {
	p.systemTools.mu.Lock()
	defer p.systemTools.mu.Unlock()
	p.systemTools.callAgent = fn
}

// registerOwnSystemTools adds the agent call and get_context to the registry.
func (p *Platform) registerOwnSystemTools() error {
	if err := p.RegisterSystemTool(SystemTool{
		Name:  ToolAgent,
		Title: "Call an agent",
		Description: "Bring in another agent for part of the work. Give it a title, a few imperative words that name the work (at most 60 characters, such as \"Review the auth change\"), and a brief: the context it needs and what done looks like. " +
			"Its work streams under this call and its answer is the result. Call several at once to run them in parallel.",
		Input: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"agent", "title", "brief"},
			"properties": map[string]any{
				"agent": map[string]any{"type": "string", "description": "The agent's name."},
				"title": map[string]any{"type": "string", "maxLength": MaxAgentCallTitle, "description": "A few imperative words naming the work, shown on its card."},
				"brief": map[string]any{"type": "string", "maxLength": MaxAgentCallBrief, "description": "The context the agent needs and what done looks like."},
			},
		},
		Permission: auth.RunsWrite,
		Call:       p.systemAgentCall,
	}); err != nil {
		return err
	}
	return p.RegisterSystemTool(SystemTool{
		Name:        ToolGetContext,
		Title:       "Get context",
		Description: getContextDescription,
		Input: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"request"},
			"properties": map[string]any{
				"request": map[string]any{"type": "string", "maxLength": maxContextRequest, "description": getContextRequestDescription},
			},
		},
		Permission: auth.RunsRead,
		ReadOnly:   true,
		Idempotent: true,
		Call:       p.systemGetContext,
	})
}

// systemAgentCall checks an agent call's arguments and hands it to the run
// machinery.
func (p *Platform) systemAgentCall(ctx context.Context, c SystemCall, raw json.RawMessage) (SystemResult, error) {
	var a AgentCall
	if err := json.Unmarshal(raw, &a); err != nil {
		return SystemResult{IsError: true, Text: "the arguments are agent, title and brief"}, nil
	}
	a.Agent, a.Title, a.Brief = strings.TrimSpace(a.Agent), strings.TrimSpace(a.Title), strings.TrimSpace(a.Brief)
	switch {
	case a.Agent == "":
		return SystemResult{IsError: true, Text: "name the agent to call"}, nil
	case a.Title == "":
		return SystemResult{IsError: true, Text: "give the call a title: a few imperative words naming the work"}, nil
	case utf8.RuneCountInString(a.Title) > MaxAgentCallTitle:
		return SystemResult{IsError: true, Text: fmt.Sprintf("the title is at most %d characters", MaxAgentCallTitle)}, nil
	case a.Brief == "":
		return SystemResult{IsError: true, Text: "give the agent a brief: the context it needs and what done looks like"}, nil
	case len(a.Brief) > MaxAgentCallBrief:
		return SystemResult{IsError: true, Text: fmt.Sprintf("the brief is at most %d bytes", MaxAgentCallBrief)}, nil
	}
	p.systemTools.mu.Lock()
	call := p.systemTools.callAgent
	p.systemTools.mu.Unlock()
	if call == nil {
		return SystemResult{IsError: true, Text: "agent calls are not available here yet"}, nil
	}
	return call(ctx, c, a)
}

// systemGetContext pulls context, as the caller, from the place the message
// they answer came from: the platform binds the place, never the caller. A
// caller with no message bound, such as an MCP client, names the message
// with get_message_context instead.
func (p *Platform) systemGetContext(ctx context.Context, c SystemCall, raw json.RawMessage) (SystemResult, error) {
	var in struct {
		Request string `json:"request"`
	}
	if err := json.Unmarshal(raw, &in); err != nil || strings.TrimSpace(in.Request) == "" {
		return SystemResult{IsError: true, Text: "say in words what you need"}, nil
	}
	if c.Message == "" {
		return SystemResult{IsError: true, Text: "get_context reads the message being answered, and this call answers none; name the message with get_message_context instead"}, nil
	}
	session, _ := SplitMessageID(c.Message)
	if !p.SeesSession(c.Who, session) {
		return SystemResult{IsError: true, Text: "the message being answered is not one you can read"}, nil
	}
	out, err := p.contextFor(ctx, c.Who.Tenant, humanSubject(c.Who), session, in.Request, c.Run)
	switch {
	case errors.Is(err, threads.ErrInvalid), errors.Is(err, threads.ErrNotFound):
		return SystemResult{IsError: true, Text: err.Error()}, nil
	case err != nil:
		return SystemResult{}, err
	}
	return SystemResult{Text: contextToolText(out)}, nil
}
