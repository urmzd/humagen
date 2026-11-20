package legacy

import (
	"slices"
	"strings"
)

// The tools an agent called other agents with before the agent tool. A
// saved spec, an approval, or a skill's tool restriction that names one is
// read as naming the agent tool. This is the one place that says so.

// AgentTool is the tool an agent calls another agent with: the name every
// earlier way to call an agent reads as.
const AgentTool = "agent"

const (
	// CallAgentTool is the agent tool's first name.
	CallAgentTool = "call_agent"
	// DelegateTool handed work to another agent without waiting. It is the
	// agent tool called with background set.
	DelegateTool = "delegate_task"
	// TagToolPrefix starts the name of the tool that called one agent,
	// "tag_<agent>". A spec's approvals still name one to hold the calls to
	// that agent alone.
	TagToolPrefix = "tag_"
)

// AgentTools are the earlier names of the agent tool that named no agent.
func AgentTools() []string { return []string{CallAgentTool, DelegateTool} }

// IsAgentTool reports whether name is an earlier name of the agent tool.
func IsAgentTool(name string) bool { return slices.Contains(AgentTools(), name) }

// TagTarget is the agent the tool "tag_<agent>" called.
func TagTarget(name string) (agent string, ok bool) {
	agent, ok = strings.CutPrefix(name, TagToolPrefix)
	return agent, ok && agent != ""
}

// ToolName is the tool a stored name reads as now: the agent tool for an
// earlier name of it, and the name itself otherwise. A "tag_<agent>" name
// stays as it is, since it also says which agent.
func ToolName(name string) string {
	if IsAgentTool(name) {
		return AgentTool
	}
	return name
}

// AgentToolNamed reports whether names holds the agent tool under any name
// that covers a call to callee: the agent tool, an earlier name of it, or
// callee's own "tag_<agent>".
func AgentToolNamed(names []string, callee string) bool {
	for _, name := range names {
		if ToolName(name) == AgentTool {
			return true
		}
		if target, ok := TagTarget(name); ok && target == callee {
			return true
		}
	}
	return false
}

// NamesAgentTool reports whether names holds the agent tool under any name
// at all: the agent tool, an earlier name of it, or any "tag_<agent>".
func NamesAgentTool(names []string) bool {
	return slices.ContainsFunc(names, func(name string) bool {
		_, tag := TagTarget(name)
		return tag || ToolName(name) == AgentTool
	})
}

// WithoutAgentTools reads a spec's tools and approvals as they are held now,
// given the agents it may call. Every earlier name of the agent tool, and
// the "tag_<agent>" of every agent it may call, leaves the tools: the agents
// the spec may call grant the agent tool, so no tool name does. An approval
// under an earlier name is required of the agent tool, once, when the spec
// may call any agent. An approval of the "tag_<agent>" of an agent it may
// call leaves the approvals and is returned in held, as the agents whose
// calls wait for a person. A "tag_" name of no agent the spec may call is
// some other tool's and is left alone.
func WithoutAgentTools(tools, approvals, spawn []string) (nowTools, nowApprovals, held []string) {
	old := func(name string) bool {
		target, tag := TagTarget(name)
		return tag && slices.Contains(spawn, target) || IsAgentTool(name)
	}
	if !slices.ContainsFunc(tools, old) && !slices.ContainsFunc(approvals, old) {
		return tools, approvals, nil
	}
	nowTools = slices.DeleteFunc(slices.Clone(tools), old)
	nowApprovals = make([]string, 0, len(approvals))
	for _, name := range approvals {
		if target, ok := TagTarget(name); ok && slices.Contains(spawn, target) {
			if !slices.Contains(held, target) {
				held = append(held, target)
			}
			continue
		}
		name = ToolName(name)
		if name == AgentTool && len(spawn) == 0 {
			// Nothing to approve: the spec may call no agent.
			continue
		}
		if !slices.Contains(nowApprovals, name) {
			nowApprovals = append(nowApprovals, name)
		}
	}
	return nowTools, nowApprovals, held
}
