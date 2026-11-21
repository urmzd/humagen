package platform

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/saige/flow/event"
)

// MaxSessionUsageAgents caps the per-agent breakdown of a session's usage.
const MaxSessionUsageAgents = 20

// AgentUsage is what one agent's own calls used across a session.
type AgentUsage struct {
	Agent string      `json:"agent"`
	Runs  int         `json:"runs"`
	Usage event.Usage `json:"usage"`
}

// SessionContext is the context fill of the latest run that reported one.
type SessionContext struct {
	Run    string `json:"run"`
	Agent  string `json:"agent"`
	Model  string `json:"model,omitempty"`
	Window int    `json:"window,omitempty"`
	Tokens int    `json:"tokens,omitempty"`
}

// ThreadUsage is what a conversation has used so far, read from the run index.
// Truncated says the index read hit its cap, so the figures are a lower bound.
type ThreadUsage struct {
	Runs      int             `json:"runs"`
	Usage     event.Usage     `json:"usage"`
	Agents    []AgentUsage    `json:"agents"`
	Context   *SessionContext `json:"context,omitempty"`
	Working   int             `json:"working"`
	Truncated bool            `json:"truncated"`
}

// SessionUsage folds the usage of a conversation's runs. A run called by
// another counted run is already in its caller's total, so only the totals of
// runs whose caller is not counted are summed. A conversation with no runs
// answers zeros.
func (p *Platform) SessionUsage(ctx context.Context, who auth.Principal, id string) (ThreadUsage, error) {
	th, err := p.workspaceThread(who, id)
	if err != nil || (th.Session != nil && !p.SeesSession(who, id)) {
		return ThreadUsage{}, threads.ErrNotFound
	}
	rr, err := p.Runs.Conversation(who.Tenant, id)
	if err != nil && !errors.Is(err, runs.ErrNotFound) {
		return ThreadUsage{}, err
	}
	return foldUsage(rr), nil
}

func foldUsage(rr []runs.Run) ThreadUsage {
	out := ThreadUsage{Runs: len(rr), Agents: []AgentUsage{}, Truncated: len(rr) >= runs.MaxTree}
	counted := make(map[string]bool, len(rr))
	for _, r := range rr {
		counted[r.ID] = true
	}
	by := map[string]*AgentUsage{}
	for _, r := range rr {
		if !r.Status.Terminal() {
			out.Working++
		}
		u := r.Usage
		if u == nil {
			continue
		}
		if r.Parent == "" || !counted[r.Parent] {
			out.Usage = out.Usage.Add(u.Total)
		}
		a := by[r.Agent]
		if a == nil {
			a = &AgentUsage{Agent: r.Agent}
			by[r.Agent] = a
		}
		a.Runs++
		a.Usage = a.Usage.Add(u.Own)
		if c := u.Context; c != nil {
			out.Context = &SessionContext{Run: r.ID, Agent: r.Agent, Model: c.Model, Window: c.Window, Tokens: c.Tokens}
		}
	}
	for _, a := range by {
		out.Agents = append(out.Agents, *a)
	}
	slices.SortFunc(out.Agents, func(a, b AgentUsage) int {
		if c := costMicros(b.Usage) - costMicros(a.Usage); c != 0 {
			if c > 0 {
				return 1
			}
			return -1
		}
		if c := usageTokens(b.Usage) - usageTokens(a.Usage); c != 0 {
			return c
		}
		return strings.Compare(a.Agent, b.Agent)
	})
	if len(out.Agents) > MaxSessionUsageAgents {
		out.Agents = out.Agents[:MaxSessionUsageAgents]
	}
	return out
}

func costMicros(u event.Usage) int64 {
	if u.Cost == nil {
		return 0
	}
	return u.Cost.Micros
}

func usageTokens(u event.Usage) int { return u.PromptTokens + u.CompletionTokens }
