package platform

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/urmzd/fiber/pkg/agentspec"
	"github.com/urmzd/fiber/pkg/registry"
)

// Agents the platform provides.
//
// A built-in is an ordinary agent revision that the platform writes instead of
// a person. It runs like any other agent, in a sandbox, under the same limits,
// and another agent reaches it the way it reaches any agent: by naming it in
// its spawn allowlist. What sets it apart is only that nobody edits it, so
// every workspace has the same one.
//
// A built-in needs a model to be of any use, so none exists until the server
// is told which provider and model to give them.

// BuiltinWriter writes a document and publishes it.
const BuiltinWriter = "warp-writer"

const writerPrompt = `You write documents and publish them as files.

When asked for a document, report, summary, table or dataset:
1. Write it with file_write. Use Markdown (.md) for prose and CSV (.csv) for tables of data. Choose a short, descriptive file name.
2. Publish it with publish_artifact, with a one-line description.
3. Reply in one or two sentences saying what you published. Do not paste the document into your reply.

To change a file you published before, read it with file_read, write the new version to the same path, and publish it again under the same title: that makes a new version.

Write only what you were asked for. If the request does not give you enough to write something true, say what is missing instead of inventing it.`

// BuiltinAnalyst answers questions about how the workspace's agents are doing.
const BuiltinAnalyst = "warp-analyst"

const analystPrompt = `You answer questions about how this workspace's agents are doing: how many runs there were, which failed and why, how long they took, what they cost, and what the alert rules are watching.

1. Call runs_summary for the window the question is about (24 hours unless it says otherwise), and list_alerts when alerts or rules matter.
2. Answer with the numbers first, in a sentence or two a person can read on a phone. Name the agent, the window and the figure: "oncall failed 6 of 10 runs in the last 24 hours."
3. Say what stands out and what to look at next: a failure rate that rose, a slow agent, spend that grew, a rule that keeps firing, or an agent with no rule watching it.
4. When asked for a report or a table, write it with file_write as Markdown or CSV and publish it with publish_artifact. Do not paste long tables into your reply.

Use only what the tools return. When they hold no data for the question, say so instead of guessing.`

// BuiltinBuilder writes and revises agents, each change after a person approves it.
const BuiltinBuilder = "warp-builder"

const builderPrompt = `You help people create and improve agents for this workspace.

1. Ask what the agent is for, who will use it, and what it must never do, unless the request already says. Keep it to one or two short questions.
2. Look at what exists with list_agents, and read_agent before changing one, so you build on it instead of starting over.
3. Write a clear system prompt: the agent's job, the steps it follows, how it answers, and what it refuses or hands to a person. Grant only the tools it needs. Use ask_person when it must check with someone before acting.
4. Save it with save_agent. A person approves every save, so say in one or two sentences what you are about to create or change, and why, before you call it.
5. After it is saved, suggest one message to try it with.

You cannot change the platform's own agents, give an agent calls to other agents, or grant tools beyond those save_agent names.`

// Builtins lists the names of the agents the platform provides.
func Builtins() []string { return []string{BuiltinWriter, BuiltinAnalyst, BuiltinBuilder} }

// IsBuiltin reports whether a name belongs to the platform.
func IsBuiltin(name string) bool { return slices.Contains(Builtins(), name) }

func (p *Platform) builtinSpecs() []agentspec.AgentSpec {
	if p.cfg.BuiltinProvider == "" || p.cfg.BuiltinModel == "" {
		return nil
	}
	return []agentspec.AgentSpec{{
		Name:         BuiltinWriter,
		Description:  "Writes documents, reports and tables, and publishes them as files",
		Provider:     p.cfg.BuiltinProvider,
		Model:        p.cfg.BuiltinModel,
		SystemPrompt: writerPrompt,
		Tools:        FileTools(),
	}, {
		Name:         BuiltinAnalyst,
		Description:  "Answers questions about runs, failures, speed, spend and alerts, and writes reports",
		Provider:     p.cfg.BuiltinProvider,
		Model:        p.cfg.BuiltinModel,
		SystemPrompt: analystPrompt,
		Tools:        append(AnalystTools(), FileTools()...),
	}, {
		Name:         BuiltinBuilder,
		Description:  "Creates and improves agents with you. You approve every change",
		Provider:     p.cfg.BuiltinProvider,
		Model:        p.cfg.BuiltinModel,
		SystemPrompt: builderPrompt,
		Tools:        BuilderTools(),
		// Changing an agent is a manager's or an admin's call, and the
		// person who asked may make it when they hold one of those roles.
		Approvals:    []string{ToolSaveAgent},
		Approvers:    []string{"role:admin", "role:manager"},
		SelfApproval: true,
	}}
}

// EnsureBuiltins makes sure a tenant has the platform's agents, at the
// definition this server was started with. It is safe to call on every
// request: when nothing has changed it only reads.
func (p *Platform) EnsureBuiltins(ctx context.Context, tenant string) error {
	for _, want := range p.builtinSpecs() {
		have, err := p.Agents.Get(ctx, tenant, want.Name, 0)
		switch {
		case errors.Is(err, registry.ErrNotFound):
			if _, err := p.Agents.Create(ctx, tenant, want, "platform"); err != nil && !errors.Is(err, registry.ErrExists) {
				return fmt.Errorf("platform: built-in %s: %w", want.Name, err)
			}
		case err != nil:
			return err
		case have.Spec.Provider != want.Provider || have.Spec.Model != want.Model || have.Spec.SystemPrompt != want.SystemPrompt ||
			have.Spec.Description != want.Description || !slices.Equal(have.Spec.Tools, want.Tools) ||
			!slices.Equal(have.Spec.Approvals, want.Approvals) || !slices.Equal(have.Spec.Approvers, want.Approvers) ||
			have.Spec.SelfApproval != want.SelfApproval:
			if _, err := p.Agents.Revise(ctx, tenant, want, have.Rev, "platform"); err != nil && !errors.Is(err, registry.ErrConflict) {
				return fmt.Errorf("platform: built-in %s: %w", want.Name, err)
			}
		}
	}
	return nil
}
