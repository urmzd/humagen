# Humagen agentic workspace and control plane design

A design for taking the conversation workspace (#64) to persistent, delegated
agent work: agents with roles and standing assignments, a control plane to
oversee them, visible context provenance, skills and memory, initiatives,
forking, and integrations with GitHub, Slack, Drive and Notion.

This is a prepared design. Nothing here is implemented beyond what
[gap-analysis.md](../../reference/control-plane-gap-analysis.md) marks as built.

- [Product and design spec](../../rfcs/0001-agentic-workspace-control-plane.md)
- [Gap analysis against the code](../../reference/control-plane-gap-analysis.md)
- [Design rules: tokens, identity, icons, navigation](../../reference/board-design-rules.md)

The 25 clickable HTML boards and their GitHub Pages site were removed on
October 30, 2026. They remain in the repository history before that date.

## Where the design differs from the repo's rules

The boards drew agents as initials in rounded squares and composer controls as
pills. AGENTS.md requires `AgentMark` colours for agents and text controls under
the composer. Implementations follow AGENTS.md; see the "Diverges on purpose"
table in the gap analysis.
