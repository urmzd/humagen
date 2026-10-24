# Humagen agentic workspace: product and design spec

Prepared design, October 2026. Not implemented beyond what
[gap-analysis.md](gap-analysis.md) marks as built. Where this spec and
[AGENTS.md](../../../AGENTS.md) disagree on presentation, AGENTS.md wins (see
[Identity](#objects-scopes-and-identity)).

## Summary and principles

Humagen is a project-based workspace where people and agents work together.
Projects hold the people, agents, context, skills, memory and outcomes for one
body of work. A workspace-level control plane lets one person oversee far more
delegated work without watching every step.

1. **Outcome first, steps on demand.** Every agent result leads with the
   recommendation, decisions, deliverables and next action. Steps, tool calls
   and handoffs are one click away.
2. **One room per body of work.** Projects and channels are the same concept at
   two sizes. A conversation, its agents, its context and its outputs live
   together.
3. **Every object says what it is and whose it is.** People, agents, skills,
   memories and sources each have one fixed mark. Every configurable thing
   carries a scope chip.
4. **Visible provenance.** Any fact an agent uses shows where it came from,
   whether it is live or a snapshot, and when it was last good.
5. **People decide; agents carry.** Agents act within explicit autonomy
   (Allow, Ask, Never). Anything outside it becomes a decision routed to a named
   person.
6. **Quiet by default, loud on exceptions.** The interface is calm until a
   decision, permission, failure or stale source needs a person.
7. **Narrower can tighten, never loosen.** Settings inherit Workspace, Project,
   Agent, Personal, and a lower level can only be stricter.

## Mental model and information architecture

A workspace contains projects, a shared Library, the Operations control plane
and a personal layer (You). Projects are where work happens; everything else
feeds or oversees them.

| Area | Holds |
|---|---|
| Projects | Channels and subchannels, conversations, members and agents, context sources, project skills, project memory, outcomes and decisions |
| Library | Skills (versioned) and memory (reviewed), each scoped to workspace, project or person |
| Operations | Needs you, Agents, Agent network, Context health, Integrations, Policies, Usage, Audit log |
| You | Profile and briefing style, notifications, personal agents, personal skills and memory, connected accounts, privacy defaults |

- A conversation is the unit of work. It can start private and be added to a
  project later; its runs, Activity, sources and deliverables move with it.
- Inside a project the left nav shows its channels and conversations, each
  listing the agents that took part. The conversation's right side holds
  Context, People and Settings (which holds Activity).

## Objects, scopes and identity

Each object type has one mark used everywhere.

| Object | Mark in this repo | What it is |
|---|---|---|
| Person | Circle (`PersonMark`) | A human member; the only one who can decide |
| Agent | `AgentMark` colour, never letters in a box | A worker with a role, assignments and autonomy |
| Skill | Badge with the skill icon | A reusable, versioned way of doing a job |
| Memory | Badge with the bookmark icon | A durable fact, decision or preference with provenance |
| Source | Badge with the database icon | Context from outside: indexed, live or hybrid |
| Decision | Check-circle with the accent | A choice routed to a named person |
| Handoff | Arrow | Work passed between agents, with a contract |

The canvas boards draw agents as initials in rounded squares. That predates
this review and conflicts with AGENTS.md ("Agents are told apart by color, not
by letters in boxes"). Implementations use `AgentMark`; the boards' layouts and
flows still apply.

**Scope chips.** Skills, memories, sources, policies and agents carry a neutral
chip: icon and word (Workspace, a project name, an agent name, or "Only you").
Colour never carries scope on its own.

## Core flows

1. **Source change to decision (the delegated-work loop).** A source changes;
   Monitor Agent detects it and hands off to Launch Planner; Launch Planner
   assesses impact and asks Ops Agent to draft, held for approval; the person
   decides in the conversation (approve or redirect); Ops Agent acts within its
   autonomy; the decision is saved to project memory. Every step writes to
   Activity; only the decision reaches the person.
2. **Start private, add to a project.** The dialog shows the audience change
   before confirming, what moves (messages, runs and Activity, sources), and any
   private source that would become visible, with "share a summary" as the
   default. The move is logged with before and after audience.
3. **Create an agent.** Role, assignments, skills and context, autonomy
   (pre-filled and locked by policy), review, then a dry run with every action
   forced to Ask.
4. **Teach a skill.** Save a good outcome as a skill draft, test, publish as
   v1 with personal or project scope, promote to workspace when it proves
   itself.
5. **Remember and forget.** Approved decisions are saved automatically; other
   memories are suggestions until a person keeps them; forget is immediate and
   logged.

## Agents and how they talk to each other

| Part | Holds |
|---|---|
| Role | One plain-language job description |
| Standing assignments | Trigger and action pairs that keep work going across sessions |
| Skills | Versioned skills it may run |
| Context | Sources it may read, with mode and freshness |
| Autonomy | Allow, Ask or Never per action category, inherited from policy |
| Handoffs | Agents it may hand work to |
| Escalation and limits | Who it asks, reminders, stop rules, run budget |
| Manager | The person accountable for it |

States: Working, Waiting on people, Watching, Paused, Failed; always words, not
colour alone.

**Handoff contract.** Goal, inputs (attached objects), due and done-when, with
states Requested, Accepted, In progress, Result, Blocked, Needs a person.
Agents hand off only to allowed agents; a chain over 3 hops or any loop
escalates to the manager; a blocked handoff names its blocker and raises one
Needs-you item.

**Where coordination shows.** The conversation shows the outcome and one line
("3 agents coordinated, 2 handoffs"). Activity shows each step. The Agent
network shows who hands to whom, volumes, waiting time and blocked threads.

## Context: provenance and the sync lifecycle

Every source shows origin and owner, access mode, last good sync, status and
dependents. Lifecycle: Connected, Importing, Indexing, Available, then for live
sources Syncing and In sync, with a side state Needs attention (auth expired,
quota, schema changed, unsupported files). A failing source keeps its last
good copy; agents say they used it. Repeated failure or a stale relied-on
source raises one Needs-you item per cause, and dependent assignments pause
past the agent's stop rule (default 24 hours).

## Context access modes

| | Indexed | Live search | Hybrid |
|---|---|---|---|
| What happens | Copied and indexed, kept in sync | Queried when an agent asks | Pinned pages indexed, the rest live |
| Freshness | Last good sync | Current at query time | Per part |
| Permissions | Project rules on the copy | The asker's own access, checked each time | Per part |
| Source down | Works from last good copy | Unavailable, said so | Pinned part works |
| Default for | Drive folders, uploads, GitHub issues | Notion workspaces, GitHub code | Notion with a few canonical pages |

Answers cite each source with its mode. Live results the asker cannot see are
never shown to them.

## Skills and memory

- **Skills:** name, purpose, inputs and outputs, allowed tools, steps, version.
  Scope Personal, Project, Workspace; promotion is logged; edits create a new
  version; test runs force every action to Ask.
- **Memory:** Decisions, Facts, Preferences, People and owners. Each item shows
  scope, provenance, status (Confirmed, Suggested, Expires) and use count.
  Agents only suggest project memory. Personal memory never enters projects
  unless the person allows it.

## User-level configuration (You)

| Panel | Controls |
|---|---|
| Profile | Name, role, how agents should brief you |
| Notifications | Per event kind: immediately, digest or off; quiet hours |
| My agents | Personal agents, their autonomy, pause, add to a project |
| Skills and memory | Personal skills; "remember how I work"; personal memory in projects (off by default) |
| Connected accounts | Granted scopes, last agent use, disconnect |
| Privacy and defaults | New conversations start private; private sources move as a summary |

User settings only narrow; they never widen a project or workspace policy.

## Control plane (Operations)

| Screen | Answers |
|---|---|
| Needs you | What is waiting on me, anywhere? One item per cause |
| Agents | Who is doing what, and how much may they do alone? Pause all |
| Agent settings | What is this agent for and what are its bounds? |
| Agent network | How do agents coordinate, and where does it jam? |
| Context health | Can agents trust what they read? |
| Integrations | Which systems are connected and linked where? |
| Policies | What are the outer bounds? |
| Usage | What does it cost, and how often did it need a person? |
| Audit log | Who did what, when, under which rule, with before and after? |

## Hierarchy: initiatives, projects and sub-projects

Workspace, Initiative (optional parent), Project, Sub-project, Channel,
Conversation. Parents pass down policies (tighten-only), shared sources,
skills, read-only memory and initiative agents. Children roll up status,
risks, decisions waiting, Needs-you items, usage and promoted memory. An
initiative agent acts inside a child only by handing off to that child's
agents. Project management offers one tree, a quick switcher, and four ways to
start: blank, from a template, fork existing, under an initiative.

## Integrations

Every external system wraps in through five layers: Connect, Resources, Links
to projects, Access mode, Events and actions.

- **Link modes:** Suggest (default), Auto-link by rule, Off. Rules match
  existing attributes (repo topic, label, path, contributors; Slack channel
  prefix; Drive folder; Notion parent page) and can target any level of the
  hierarchy. Suggestions show why; a resource matching two destinations always
  goes to Suggest. New rules preview what they would link.
- **Slack bridge:** two-way, inbound only, or outcomes only. Human messages,
  outcomes and decision buttons cross; Activity steps and private sources never
  do. Unmatched Slack users are guests who cannot approve.
- **Actions back out** (PR comments, Slack posts, page edits) follow the
  external-message policy and each agent's autonomy, and are audited.

## Forking

- **Conversation fork:** "Fork from here" on any message. History up to that
  message is read-only context; the person picks agents and sources; the
  original shows a lineage marker; "Bring back result" posts the fork's outcome
  into the parent for comparison. A fork's pending actions never run in the
  parent.
- **Project fork:** copies structure and agent configs; links or copies
  skills; inherits policies from the new parent; copies no memory unless
  confirmed items are chosen; re-links live sources after an access check;
  copies integration rules disabled; copies no conversations by default.
  Lineage shows on both sides.

## Visual tokens

The canvas uses Inter, a warm off-white ground (#F6F6F4), one muted blue
accent (#2E5B88) and status tints reserved for meaning, with all text at 4.5:1
or better. In this repo the console's own `aui` tokens and `Page`, `AgentMark`
and copy conventions take precedence; the canvas tokens describe intent, not a
replacement theme. The full token list and icon set are in
[design-rules.md](design-rules.md) and on the Identity and icons board.

## System foundations and phasing

| Foundation | Records | Unlocks |
|---|---|---|
| Conversation re-homing | Runs, Activity, sources and deliverables move atomically; audience before and after | Start private, add to project |
| Source lifecycle | Per-source state, sync attempts, last good copy, failure reason, cadence | Context health, stop rules, stale-context items |
| Dependency graph | Source to agent, skill, memory, conversation, deliverable | "Used by", one Needs-you item per cause |
| Action policy | Scoped Allow, Ask, Never by action category, tighten-only | Autonomy controls, locks, permission requests |
| Handoff contracts | Due, done-when, contract states, hop counts | Agent network, loop and bottleneck detection |
| Skill and memory store | Versioned skills, scoped memory with provenance and expiry | Library, citations in outcomes |

Phasing: (1) foundation: re-homing, source lifecycle, outcome-first answers;
(2) delegation: assignments, action policy, Needs you, roster states, pause;
(3) coordination: handoff contracts, Agent network, Context health;
(4) compounding: skills library and promotion, memory review and expiry,
personal skills and memory, initiatives, forking, integration auto-linking.

## Open questions

- Is an agent's manager set per project or workspace-wide?
- Should admins see personal agents' activity or only usage totals?
- What are the default run budgets per agent and project, and who raises them?
- Do snapshots expire by default (proposed: flag at 30 days, never delete)?
- Can agents hand off across projects, or only through a person?
- How are external guests represented?
