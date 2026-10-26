# Agent operations

Agent controls, action policies, assignments and handoff contracts are records
in `pkg/operations`, backed by the same memory or Postgres `state.Store` as the
rest of the platform. Each tenant is capped at 10,000 records. HTTP lists return
at most 200 visible records; hidden projects, private threads and hidden agents
contribute no rows or network counts. Updates use an expected version.

## Controls and action policy

`GET/PUT /api/agent-controls` pauses or resumes new workspace admissions.
`GET/PUT /api/agents/{name}/control` controls one agent and records its accountable
manager and instructional stop rules. The manager is workspace-wide per agent;
project action policies remain scoped to projects. Current and compatibility
handles for a built-in agent share the same control. Pause changes serialize
with admission, never mark a run stopped, and never grant tools. The ledger
continues to determine every accepted run's actual outcome.

The action-policy routes cover workspace, project, agent and the reader's own
personal scope. Allow, Ask and Never compose by taking the strictest rule across
the workspace, the whole project chain, agent and person. A missing rule leaves
the existing spec grants alone. Allow never adds a tool or a spawn grant. A
lower policy cannot override an upper Ask or Never. Existing call allowlists,
budget policies and callee approvals still apply independently.

Categories are read context, write files, publish artifacts, send external
messages, call agents and other actions. MCP read groups are read context;
write groups conservatively apply both other-actions and external-message
rules. A provider's tool name is not evidence that an action is read-only.
The catalog preserves rich results and person-scoped credential behavior.
MCP discovery uses the exact run actor, with a session bounded by that run's
execution, and never borrows another actor or a process credential.

Ask uses fiber's existing durable approval mechanism. Questions, deadlines,
edited approved arguments and continuation use the existing runtime records.
Never and pause are checked again at tool execution and on every agent call.
Internal `Turn.ForceAsk` tightens a draft test to Ask without weakening Never.
A pinned skill may restrict exact tool names with `operations.MetaTools`;
explicit `[]` grants no actions. Restrictions and ForceAsk follow helper calls
so delegating does not escape the test or skill's restrictions.

## Assignments and contracts

Assignments have an owner, project, optional conversation, goal, done-when,
deadline and optional five-field cron expression. A manual run reserves its
deterministic identity before calling the existing conversation composer and
`Invoke`. A repeated request joins that reservation, including after a crash
between reservation and admission. An assignment in an existing conversation
must pass the same posting rule, including private-thread participation.

Standing assignments are project-scoped. Cron plus an existing conversation is
refused, so a private conversation goal cannot be replayed into a wider room.
Every occurrence rechecks the owner's ordinary project membership without
borrowing a sign-in or bearer scope. Losing access pauses and blocks the work.
Ordinary schedule mutation routes cannot bypass the assignment owner.

Source-change assignments use a durable content-version cursor. Creating one
requires knowledge read access and an existing source grant on its agent or
project. The source-lifecycle layer registers its resident `SourceChanges`
provider. Missing tracking, a hard-stale source or lost project membership
blocks work. The existing reconciliation ticker scans at most 200 watchers
per page, coalesces changes while work is active and raises deterministic tags
through `Invoke`. Unchanged refreshes start no run; cursor and admission are
kept together, and restart resumes observing after the stored version. Source
recovery never resumes blocked work without an explicit owner action.

Standing assignments create paused schedules in the existing cron store. The
schedule activates only after the contract is durable and the assignment is
accepted or watching. Cancel pauses the schedule before saving the state.
Changing an execution contract requires a new assignment, so an old scheduled
occurrence cannot execute newly substituted instructions. No second scheduler,
agent loop or model invocation path is introduced.

An observed terminal run and periodic reconciliation settle work from the run
index's ledger-backed status. Success records a result; a standing assignment
returns to watching. Failure records a blocker. A missing or silent run is not
considered complete. Missed deadlines become Needs a person while retaining
the actual run reference. The inbox uses a deterministic key per work item so
recovery between the operations write and inbox write does not create duplicate
Needs-you items. Deadlines and pending work survive restart.

Every `pkg/call` invocation records a handoff contract before admission: typed
input references, goal, done-when, deadline, chain and accountable manager.
Callers may supply explicit `contract` terms. For old callers, the input and
response format supply terms with a 24-hour deadline. A cycle or more than
three hops records an escalation and refuses the child. Cross-project handoff remains the design spec's open question. Calls keep the
existing root-project, thread, source and accounting boundary; a child agent
unavailable there is refused without fabricating a run in another project.
The run ledger, not
the contract, determines whether accepted helper work succeeded.

`GET /api/agent-network` derives bounded visible edges from those contracts.
Operation attention has its own inbox kind and part. Deadlines and escalation
never claim a successful run failed, offer Retry Run or create a synthetic
runtime question. Open Assignment and Open Handoff navigate to the actual
versioned record; acknowledgement uses the existing inbox resolver.

The console components expose controls, policy editing, creation and running
of assignments, recorded run links and contract inspection. Navigation and
phone/desktop captures are integrated by the presentation layer of the stack.

Private assignment and handoff provenance is not automatically published with
a conversation. The operations publication guard requires reviewed summary
publication for any retained linked contract, and legacy operation reads must
pass both the original project and current conversation audience. A host
setup gate may allow an exact draft-owner/revision test to bypass that agent's
pause only with ForceAsk. Setup readiness remains a separate admission check,
so an ordinary unpause cannot activate an incomplete setup. Workspace pause and
Never still deny those tests and their tools.

Source-hard-staleness admission and source dependency presentation are supplied
by the source-lifecycle layer. The operations store does not infer source
health or silently resume work when a source recovers.

## Verification

The store tests run memory and Postgres restart, failed-write, version-conflict,
reservation, policy tightening, contract bounds and deadline cases. HTTP tests
exercise actual assignment execution and settlement, paused admission, missing
and hidden objects, query bounds and the OpenAPI response sweep. The exhaustive
confinement sweep includes assignments in hidden projects and private threads.
Tool tests cover Never, unapproved Ask, pause and ForceAsk without adding grants;
the existing person-credential and approval regressions cover MCP identity.
Console DOM tests exercise successful and refused pauses, policy changes,
assignment run references and network contract navigation.
