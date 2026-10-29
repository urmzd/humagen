# Humagen product roadmap

Humagen is a shared workspace where humans and agents organize work, share knowledge and improve their capabilities under explicit policies.

This document states direction. The [architecture status](../architecture/overview.md#status) is the source of truth for implemented behavior.

| Area | Intended behavior | Next delivery boundary |
|------|-------------------|------------------------|
| Agents and hiring | Create and version agents, give them reusable abilities, and hire them into project roles | Project memberships with scoped responsibilities, capability grants and budgets |
| Projects and tasks | Assign work to people or agents, track dependencies and durable outcomes | Persistent assignments, task state and run links |
| Shared knowledge | Maintain linked project pages, decisions, sources and agent memory | Page history and provenance |
| Connectors | Bring requests in and publish outcomes through extensible integrations | Independently validated connectors |
| Governance | Apply default-deny access, capability, spending and approval policies | Complete project access and human approval at run limits |
| Human participation | Ask questions, request approvals, hand off work and notify the right person | Reply entry on every surface, project-aware subscriptions and delivery tracking |
| Shared improvement | Propose improvements to abilities, prompts and knowledge from completed work | Versioned proposals, deterministic evaluations, human review and rollback |

## Delivery sequence

1. Establish persistent tenant, project and membership records, keeping every query tenant-bound.
2. Connect project roles and policies to agent hiring, assignments and runs. Agents are enabled in projects or live in one, and a message is answered by the agent it tags or the default its project inherits; project roles and members follow ([projects](../architecture/projects.md)).
3. Deliver durable tasks and linked knowledge with attribution and visible outcomes.
4. Extend connectors and targeted notifications, preserving human approvals and recorded decisions.
5. Add the reviewed improvement loop across agent definitions, abilities and shared knowledge.

Each increment must update the architecture status and demonstrate the full human-and-agent workflow it enables.
