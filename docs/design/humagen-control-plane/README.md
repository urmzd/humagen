# Humagen agentic workspace and control plane design

A design for taking the conversation workspace (#64) to persistent, delegated
agent work: agents with roles and standing assignments, a control plane to
oversee them, visible context provenance, skills and memory, initiatives,
forking, and integrations with GitHub, Slack, Drive and Notion.

This is a prepared design. Nothing here is implemented beyond what
[gap-analysis.md](gap-analysis.md) marks as built.

- [Product and design spec](spec.md)
- [Gap analysis against the code](gap-analysis.md)
- [Design rules: tokens, identity, icons, navigation](design-rules.md)
- [Board sources](canvas/): 25 boards plus `canvas.json` (their layout)

## The boards

Each board is a Design Component (`.dc.html`): markup with `{{holes}}`,
`<sc-for>` and `<sc-if>`, and a small `Component` class whose `renderVals()`
supplies data and handlers. They render in the Claude design canvas they were
made in, which supplies `support.js`; opened directly in a browser they show
unstyled markup. They are kept here as the reference for layouts, flows and
copy.

| Area | Boards |
|---|---|
| Work | `Main` (conversation), `MoveConversation`, `Activity`, `Context`, `ForkConversation` |
| Control plane | `OpsAgents`, `AgentDetail`, `AgentComms` (network), `CreateAgent`, `OpsContext` (context health), `Integrations`, `LinkRules`, `SlackBridge`, `AddSource`, `Policies`, `Usage`, `AuditLog` |
| Structure | `Projects`, `Initiative`, `ForkProject` |
| Library and You | `Skills`, `Memory`, `UserSettings` |
| Foundations | `Icons`, `FlowMap` |

## Where the boards differ from the repo's rules

The boards draw agents as initials in rounded squares and composer controls as
pills. AGENTS.md requires `AgentMark` colours for agents and text controls under
the composer. Implementations follow AGENTS.md; see the "Diverges on purpose"
table in the gap analysis.
