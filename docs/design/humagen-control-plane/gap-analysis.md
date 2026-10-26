# Gap analysis: spec against the code

Reviewed against `main` at 8cd34d5 (the conversation workspace, #64) on
October 24, 2026. Verdicts: **Built**, **Partial**, **Not built**,
**Diverges** (built differently on purpose). Paths are relative to the
repository root.

## Built as specified

| Spec item | Evidence |
|---|---|
| Centered conversation title; project left; Context, People, Settings right; title on its own row on a phone | `apps/web/src/ui.tsx` (`Page` conversation layout), `apps/web/src/ConversationShell.tsx`, `apps/web/src/styles.css` |
| Conversations grouped by project with the agents that took part | `apps/web/src/Sidebar.tsx` |
| Activity replaces execution history, opens from conversation settings, shows handoffs, subagents and tools, and has its own route | `apps/web/src/Activity.tsx`, `apps/web/src/route.ts` |
| Clarifications separate from approvals | `packages/components/src/components.tsx` (`ClarificationCard`, `ApprovalCard`) |
| Versioned deliverables with previews, presentation and revision requests | `apps/web/src/Deliverables.tsx`, `apps/web/src/artifactPreview.ts` |
| Context provenance: origin and real import time, honestly labelled Indexed | `apps/web/src/ConversationShell.tsx` |
| Project tree with kinds and sub-projects; instructions, notes, agents, skills, knowledge and experts inherited down | `pkg/projects`, `internal/platform/projects.go` |
| Tighten-only limits, usage ledger, admission, budgets | `pkg/policy`, `pkg/usage`, `apps/web/src/Billing.tsx` |
| Memory approval queue with provenance and forget | `pkg/memory`, `apps/web/src/AgentDetail.tsx` |
| Slack channel bound to a project channel, two-way, Slack users mapped to people | `pkg/projects/channels.go`, `pkg/clients` |
| One way external systems wrap in: one surface per scheme, tenant from the client record, every run through `Invoke` | `internal/platform/connectors.go` |
| Create-agent form; personal credentials | `apps/web/src/AgentDetail.tsx`, `internal/platform/credentials.go` |
| Monitor and alert rules | `apps/web/src/Monitor.tsx`, `pkg/alerts` |

## Partial

| Spec item | What exists | Missing |
|---|---|---|
| Needs you | Inbox with approvals, questions, failures, alerts, budget, memory, mentions | One item per cause (failures are per run); stale-context items |
| Agent roster | Working, waiting, idle; home project; run tape | Paused and failed states; every project that enables the agent; last run time |
| Agent autonomy | Per-tool: listed, in `approvals`, or absent; callee call policy | Workspace or project Allow, Ask, Never by action category, inherited tighten-only; reminders; stop rules; manager field |
| Handoffs | `pkg/call` modes, depth and fan-out limits, typed inputs and validated output | Due date, contract states (Blocked, Needs a person), cycle detection |
| Skills | Workspace and project skills inherited down | Personal scope, per-skill versions, provenance, promotion, console editor |
| Memory | Agent and project notes, approval, provenance | Expiry, personal memory, per-note scope |
| Audit | Policy, project and agent histories with before and after; break-glass reads | One cross-domain log; memory and call-policy changes |
| Sharing and People | Share inspector; read-only participant list | Who can see this; roles; removal |
| Evidence chips | Bounded citation projection on run records and streaming events, including memory/Postgres restart | Source lifecycle health is tracked separately |
| Thread branches | Regenerate creates sibling answers with a switcher | Fork into a new conversation; branches in rooms |
| GitHub | Installations, webhooks, reviews, labels | Repo-to-project links; GitHub user mapping |
| Live context | Person-scoped MCP tool providers | First-party live search, OAuth, hybrid mode |
| Composer | One-line initial height, bounded growth, wrapping controls | Existing text-control styling is retained |
| Workspace message presentation | Shared persistent Copy/Reply footer for people and agents; recorded agent timestamps with focus, hover and tap detail; answer before expandable work | Structured outcomes and Redirect |

## Not built

| Spec item | Note |
|---|---|
| Structured outcomes | Built through shared primitives and the explicit `record_outcome` tool; Redirect prepares editable human intent through the normal conversation path |
| Moving a conversation after agents ran | Refused by design (`ErrStaysInProject` in `internal/platform/threads.go`); see [company-workspace.md](../../company-workspace.md) |
| Durable assignments | Listed as next work in [conversation-workspace.md](../../conversation-workspace.md) |
| Pause an agent, pause all | Console Stop Turn calls the existing stop API and waits for the run ledger, with bounded polling and retryable errors |
| Context health and source lifecycle | `pkg/knowledge` sources have no state, attempts or last good copy |
| Agent network view | No component |
| Dry run for new agents | Evals run only after saving |
| Initiatives with cross-child agents; status rollups | Counts and unread are per project only |
| Project fork | No route |
| Integration auto-linking rules and suggestions | Links are manual (Slack) or the client's home (GitHub); no Slack binding UI in the console |
| Google Drive and Notion connectors | Knowledge arrives as text through the API; retrieval is lexical |
| Quick switcher | No Cmd-K |
| Notification preferences, personal agents, privacy defaults | Not in `pkg/prefs` |

## Diverges on purpose

| Spec item | Repo rule |
|---|---|
| Agents as initials in rounded squares | AGENTS.md: agents are told apart by color (`AgentMark`), not letters in boxes |
| Composer chips as pills | AGENTS.md: controls under the composer are text |
| Outcome sections as web-only markup | Shared components are built from primitives and render in Slack too |

## Suggested order

1. Answer before work steps, then outcome structure with Redirect, through the
   shared primitives.
2. Pause and stop: agent pause, pause all, the console stop control.
3. Source lifecycle records: the base for Context health, stale-context items
   and Drive ingestion.
4. Durable assignments.
5. Conversation re-homing after runs, per company-workspace.md.
6. Repo-to-project links and a Slack binding screen.
