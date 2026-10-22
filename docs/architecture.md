# Architecture

This is the plan of record. The [status table](#status) says what exists.

## What this repo is

warp is the composition. It owns the pieces that only exist once
the parts are put together: the call tree, governance, routing work to people,
and the stores behind them. It does not reimplement what it composes.

| Part | Owns | Repo |
|------|------|------|
| Bus, topics, connectors, agent specs, revision pinning | Transport and surfaces | [fiber](https://github.com/urmzd/fiber) |
| Agent loop, tools, MCP client, RAG, evals, OTel | Runtime | [saige](https://github.com/urmzd/saige) |
| Sandboxed execution nodes, NGAC | Confinement | [playscape](https://github.com/urmzd/playscape) |
| Durable runs and replay | Crash survival | [anchor](https://github.com/urmzd/anchor) |
| Components and helpers for web, Slack and GitHub | Presentation | This repository: `packages/`, `internal/presentation/github` |

## The presentation boundary

The `packages/` UI libraries and `internal/presentation/github` are components and helpers only. They take data in as props and events and emit actions out.

The test for where something goes: if it needs a network call, a database or an
identity, it belongs in the platform or a surface host. The platform owns the wire. It translates fiber
envelopes into warp events and turns warp actions back into tags. That
adapter is the only place the two meet.

## One primitive: the call

An agent is defined by a spec and runs in a sandbox. When one agent needs
another, it makes a call: a new run of the other agent, in its own sandbox,
raised as a tag on the bus and linked to its caller by an edge.

A subagent, a handoff and a delegation are the same call with a different mode.

| Mode | Context passed | Caller after the call | Answer goes to |
|------|----------------|-----------------------|----------------|
| Subagent | Explicit ordered messages | Waits, then resumes | The caller |
| Handoff, low-level API only | Explicit input | No automatic termination; ownership transfer is not exposed as an agent tool | The inherited surface |
| Delegate | Explicit ordered messages | Keeps working | The inbox, later |

### One origin, two lifetimes

Every run belongs to one origin: the run a person, a schedule or a trigger
started. Runs nest under it as deep as the limits allow, and each records its
origin, its caller and how it was called.

| Lifetime | How it is started | If the caller fails or is stopped | When the caller answers first |
|----------|-------------------|-----------------------------------|-------------------------------|
| Attached | `call_agent` in subagent mode | It is stopped, and so is all that is attached to it | The caller is held open until it ends |
| Delegated | `call_agent` in delegate mode, legacy `delegate_task`, or a low-level handoff call | Nothing. It carries on | Nothing. The caller is done |

What a delegated run attaches dies with it, not with whoever delegated to it.
A run stopped this way is `stopped`, not `failed`, and says why.

### Stopping a run

A stop is the runtime's, not the platform's. `POST /api/runs/{id}/stop` and a
Slack stop button both call fiber's `runtime.Stop` with a reason and who
asked. The runtime records the request on the run in the anchor ledger
(`runtime.WithStopLedger`), so a run still in the queue or between attempts
honours it whenever it starts, and publishes it on the bus to the attempt
executing the run, which cancels the model call in flight and any running
tools, and ends the run with `lifecycle.stopped` carrying what the agent had
said. "Stopped" means cut short: the agent loop judges it, and a turn that had
finished its answer before the request took effect ends `completed`.

| State | Status | `stopping` | What it means |
|-------|--------|------------|---------------|
| Asked | unchanged (`accepted`, `running`, `parked`) | `true` | The request is published. The run can start no further calls from this moment. |
| Stopped | `stopped` | absent | The runtime confirmed it. `answer` is the partial answer people saw, `cause` is the reason. |
| Finished first | `completed` or `failed` | absent | The turn ended before the request reached it. It is reported as it ended, never as stopped. |

The route answers 202 with `stopping` while the run has not ended and does not
wait for it. A run that has ended is answered 200 as it is, so asking twice is
safe. The runtime gives a turn `StopGrace` (ten seconds by default) to wrap up
before it cancels it, and the run still ends stopped. A run in the queue stops
before its first model call when a worker picks it up.

After asking, the platform reads the ledger (`runtime.StopRequested`). A run
the ledger holds no request for had already ended there, so it is settled from
the ledger at once and the route answers 200 with how it ended. When the runtime cancels a
delegation because the caller was stopped (`runtime.ErrStopped` as the
context's cause), the run warp started for it is asked to stop too.

A run parked on a person is stopped the same way: the runtime resolves its
question as stopped and ends it without calling a model, and its inbox message
settles as stopped, with the cause.

A run that has answered and is only held open by what it attached has no turn
left to interrupt, and was not cut short. What is attached to it is stopped
through the runtime, and once they have ended it completes with its answer.

If the runtime cannot carry a stop (`runtime.ErrStopUnavailable`, a bus that
cannot be subscribed to), the request fails closed: the run is still marked
`stopping`, so it starts nothing more, the route answers 503, and the run ends
as its own events say. It is never reported stopped while it is still working.

A stopped turn is kept in its conversation. The next turn is sent its partial
answer followed by fiber's `StopMarker` line. Who stopped it and why is not
sent to a model. Housekeeping (titles, memory) does not run after a stopped
turn, an eval case that was stopped is a failed case, and a stopped run counts
as `stopped` in the statistics, not as active.

### Detached branches

The child is a durable run of its own, with its own journal, context window and
topic (`agent:<name>:<runID>`). The caller holds a reference and a result, never
the child's work. Each branch compacts alone, and a tree can spread across
nodes.

Warp exposes one `call_agent` tool for allowed targets. Its input combines
ordered messages, examples and references, with a discriminated return contract.
The harness resolves and freezes inputs, injects the return instruction, and
validates results. See [Agent calls](agent-calls.md) for the contract and lifecycle.

## Governance rides the edge

| Concern | Rule |
|---------|------|
| Cost | A child leases budget from its caller and settles when it completes: the lease is released and what the child spent, priced by the runtime, becomes the caller's. Limits apply to the whole tree: every run counts against its root's project and client. See [Usage and spend](#usage-and-spend). |
| Access | A child gets the intersection of its spec's access and its caller's grant. It is never wider. |
| Consent | Both sides agree to a call. The caller's spec names the callee in its spawn allowlist, and the callee's call policy (`pkg/callpolicy`) admits the caller: `anyone`, only the agents it `listed`, or `none`. The policy also says which admitted calls wait for a person (`never`, `always`, or callers not `trusted` when `unlisted`), who approves them, and how long they wait. A refusal names the side that refused. The `call_agent` tool offers only the agents both sides agree to. |
| Shape | Depth and fan-out are capped. The zero limits allow no calls. |
| Tracking | Root, parent, depth and trace context travel with the tag, so telemetry shows one tree. |
| Stopping | Stopping a run stops everything attached beneath it, through the runtime. What it delegated carries on. |

## Usage and spend

The runtime reports what every model call used (fiber's `usage` family,
v0.10.0 and later): tokens, cost from saige's price catalog in integer micros
of a currency, and the model's context window. warp counts it, holds it
against limits, and stops what goes over.

### Counted once

| Source | When | Key | What it adds |
|--------|------|-----|--------------|
| `usage.call` on a run's stream | As each call completes | The runtime's `accounting_id` | The call, to the usage ledger, under the run's place |
| The run's total (`lifecycle.completed` or `lifecycle.stopped`, else the ledger's output, else `runtime.RecordedUsage` for a failed run) | When the run ends | `run:<id>` | Only what the calls already counted for the run do not cover: a call whose event was missed |

The platform's observer sees every event (a subscription with no kind filter
receives the usage family). A redelivered call, a redelivered terminal event
and a reconciler pass all find their key already counted and add nothing. A
run is counted as the larger, field by field, of the calls seen for it and its
settled total, and both are kept after it settles: a call whose event arrives
after the settlement (the call a stop cut short is reported late, while the
settlement runs from the reconciler or the stop) adds only what goes beyond
the settled total, in either order. Once a run has settled its `spent` no
longer moves, since it has settled into its caller. A run's settled total is
its own calls (`RunUsage.Own`), never the runs it called, which settle their
own: each model call in a tree is counted once, in the run that made it.

Every ledger entry is stamped with the server's clock when it is appended
(the call's own time is kept as `event_at`), and counts in the period its
stamp falls in, whatever order entries arrive in. Counts are kept per scope
and UTC day as entries are applied, so judging a call reads at most 62 days
of totals.

### Roll-up

An attached run (tagged, the caller waits) is rolled up into its caller: once
it has ended and the ledger records it, the Delegator calls
`runtime.ReportChildUsage`, and the caller's `usage.children` and `usage.total`
include it, once. A delegated run (`delegate_task`) is not rolled up: the turn
that handed it over has moved on, so there is no turn to report it to. Its
spend is the same request's work, so it counts against the same budgets: every
run in a tree counts against its root's project and client, its own agent, and
the workspace. The run manifest's `usage.total` sums every run's own calls in
the tree, attached and delegated alike.

### Enforced

| When | What is judged | What happens |
|------|----------------|--------------|
| A run asks to start | Spend and tokens in the period, with runs and concurrency, under one lock | A scope that has reached its spend limit, to the micro-dollar, or its token limit refuses: 402 for spend, 429 for tokens, with the limit, the policy and its version, and `resets_at` |
| A call is counted | Spend and tokens of every scope the run is in (`policy.Exceeded`) | At the warning threshold, one budget alert and one inbox message per scope and limit while it is open. At the limit, the run is stopped through `runtime.Stop`, with a reason naming the limit, the scope and the policy version, a `halted` notice, and one alert. Every other run in the scope is stopped at its own next call |
| A run with a lease spends | Its `spent` plus what it has handed to calls still working (`granted`), against its `lease` | Stopped through the runtime once the two reach the lease |
| A called run ends | Its lease and its spend | The caller's `granted` releases the lease and its `spent` gains what the child spent. If the caller has already settled, the spend climbs on to the nearest ancestor still open, or the root |

An agent's own calls (tagging, `delegate_task`) lease from a metered caller
without naming an amount: each is granted half of what the caller has left.
What is left is read from the calls already counted, and the model call that
asked may not be counted yet, so the other half holds room for it; everything
is trued up when the call settles. A caller with nothing left is refused. A
stop at a limit that could not be requested is asked again at the run's next
counted call. Housekeeping runs are never refused or stopped,
and what they spend is still counted.

### Unpriced calls and other currencies

A limit is in US dollars. A call saige could not price (a model with no rate
card) and money in any other currency are never counted as zero dollars and
never compared with a dollar limit. They are counted apart: `unpriced_calls`
and `other_costs` on the run, on `GET /api/usage`, and in Analytics, where the
spend is shown as a lower bound. The first such call in a scope with a spend
limit raises one alert, open until settled, saying its spend is a lower bound.
Tokens are counted for every call, priced or not, so `max_tokens` on a policy
is the bound that holds whatever the price.

### Where it shows

| Surface | What |
|---------|------|
| `GET /api/runs/{id}` | `usage` (own, children, total, context), `spent`, `granted` |
| The event stream and `listRunEvents` | `usage.call` and `usage.child` |
| `GET /api/usage` | `spend` (used, limit, `unpriced_calls`, `other_costs`) and `tokens` per scope |
| `GET /api/analytics` | `usage` per group: calls, tokens, spend, unpriced calls |
| Console | The run's Activity page (calls, tokens, cost, context), the session gauge's context edge, Analytics, Limits |
| Slack app | Nothing. Usage is not shown on a turn in the console either, so the shared adapter carries none (see [clients](clients.md#usage-on-a-turn)) |

## People in the loop

A run that needs a person parks on a question. fiber (v0.11.0) owns the
waiting: the question is recorded on the run in the ledger, the run holds no
worker while it waits, and the first answer recorded is the answer. warp
owns who is asked and where.

| Question | Raised by | Answered with |
|---|---|---|
| Approval | A call to a tool the agent's spec lists in `approvals` | `approve` (optionally with edited `arguments`, checked against the tool's schema) or `deny` (with a note the model is shown) |
| Choice | The agent calls `ask_person` with `options` | `choose` one option |
| Reply | The agent calls `ask_person` without options | `reply` in words |

### Who is asked

Who may answer is decided from the run's pinned revision, so revising an agent
does not change who may answer a question a run already asked:

| Question | Who may answer |
|---|---|
| Approval | `approvers` when the spec names them, else anyone in the workspace. Never the person whose request raised the run, nor the person who started the tree it is in, by any name they go by (a Slack user is also every principal a Slack client of the run's team maps them to, pinned when the run starts), unless the spec sets `self_approval`. fiber's `AnswerRequest` takes no requester alias, so warp checks this before it asks the runtime. A GitHub actor is mapped to no principal, so self-approval there is not detected |
| Choice, reply | `answerers` when the spec names them, else anyone in the workspace |

`approvers` and `answerers` name principals by subject and this workspace's
roles as `role:<name>`. The runtime asks warp for a person's roles
(`runtime.WithRoles`): the roles they signed in with, or for a person a surface
answers for, what the authorization policy grants their subject. Anyone else
holds no role, so a role admits nobody it was not asked about.

Each question becomes one inbox message (kind `approval` or `clarification`,
with an `approval`, `choice` or `reply` part), addressed to exactly those
people: a reader sees it only when the spec names them or a role they hold,
and the requester never sees an approval of their own call. The agent's
`asked_of` hint narrows that to whom it asked, when that is one of them; it
admits nobody. A question whose rules cannot be read is addressed to nobody.
Answering needs `runs:write` as well (for a person a surface answers for,
their own grant must include it, not only the surface's credential's), and the
runtime checks who may answer again before it records anything.

Addressing routes a question to its inbox; it is not confidential. The
question and an approval's arguments are also on the run, its event stream
and its manifest, for anyone with `runs:read`. See
[security](security.md#the-inbox).

### Answering

From the inbox (`resolveInboxPart`), on the run (`answerQuestion`), or in Slack,
an answer is one call: `runtime.Answer`, with the person as `By` and the
credential's own subject as `Via`. A refusal is 403 and changes nothing. A
second answer, or one after the question expired or its run was stopped, is
answered with the message showing what was recorded. The decision is recorded
in the inbox's record of changes, with who answered and, for a surface, which
surface user.

The message settles in place when the run resumes (`approved by olu`,
`chose eu-west`), when nobody answered in time (`expired`, with the default
the run carried on with: a denial for an approval), when the run is stopped
while it waits (`stopped`, with the cause), and when the run ends without an
answer (`closed`). A run is woken through the queue as soon as it is answered
or stopped (`runtime.WithWaker`), and at its question's deadline, so a default
applies when it is due. Questions wait a day by default (`QuestionTTL`).

### Called runs

An attached run (tagged) that parks shows its question on its caller's stream
as `lifecycle.child.parked` (`runtime.WatchChildQuestions`), and its message is
addressed by the called agent's own spec. The caller keeps waiting in the
Delegator, holding its place, while the called run holds none. fiber's
`WithChildRuns`, which frees the caller too, is not used: it starts the child
inside the runtime, so the call would bypass warp's call path (the spawn
allowlist, depth and fan-out, the lease, admission and spend, the call tree,
and the conversation and sandbox the child shares), and it has no hook for a
host to make or refuse that call. Usage is rolled up as before, by the
Delegator's `ReportChildUsage`, once per child. Since one place is always kept
for work that waits on nobody, parents waiting on parked children can refuse
new attached calls (`ErrBusy`), never hang.

### Not built

A budget request is not an approval yet: a run at a limit is stopped or
refused, not parked. See the status table.

A call the callee's policy holds for a person is decided by `callApproval` in
`internal/platform/call_policy.go`. The `call_agent` tool is an
`agentrt.Approving` tool built on it: the runtime parks the caller on an
approval addressed to the policy's approvers (`role:manager` and `role:admin`
when it names nobody, so a held call is never open to the whole workspace).
The call goes through only when a person approved that very tool call;
otherwise the callee refuses it. fiber's host call tool wrapper does not yet
forward `Approving`, so today such a call is refused rather than parked. The
question's wait is the runtime's question TTL, not the policy's
`expires_seconds`, which fiber's `ApprovalRequest` cannot carry yet.

## Decentralized

A peer node is a connector. One node addresses an agent on another the same way
it addresses a Slack thread, so a cross-node call is a tag with a route.

This depends on execution nodes that publish to the bus themselves. Today
fiber streams out of the sandbox through an in-process channel, so every
agent runs inside the serving process.

## Storage

Target the Postgres wire protocol. AlloyDB is one deployment of it, not a
requirement.

Every store has one choice, memory or Postgres, made in one place:
`platform.Config.Backends`, which `warp serve -state=postgres` fills from
`WARP_POSTGRES_URL` through `platform.OpenPostgres`. fiber's families use
fiber's own Postgres stores: `registry.Postgres` for agents, and `pkg/durable`
for anchor's run ledger and queue. warp's own stores do not each get a SQL
copy of their rules. They keep their logic and indexes in memory, as the read
model, and write the records that state is built from through `pkg/state`: a
namespaced, tenant-scoped document store with a Memory and a Postgres backend,
one table, held to one conformance suite. A store loads its namespace when it
is built, and writes before it reports a change, undoing the change when the
write fails. Three stores fold in events that already happened (the journal,
the run index after a run is recorded, and the usage ledger after an
admission) and write those behind, every 250ms and when the platform stops.

Because a store trusts its memory after it loads, this is one writer per
database. `pkg/state` holds a Postgres advisory lock for as long as it is
open, per schema, so a second process is refused at start rather than allowed
to diverge.

Known limit: anchor's Postgres queue does not implement the delta log, so a
Postgres-only deployment runs durably but cannot stream. Redis stays in the
stack until that is closed.

## Limits at scale

Admission is one process holding one lock, reading counts from memory. That is correct for a single server and wrong for more than one.

| Limit today | What it needs |
|-------------|---------------|
| The admission lock is in process, so two servers can both admit the last run | A counter in the shared store, taken in the same transaction as the ledger entry |
| Client secret values are kept in the database as written | A secret manager behind `clients.Secrets`, or encryption at rest |
| Every store reads its whole namespace into memory at start and trusts its memory from then on, so one process holds a database (`pkg/state` takes an advisory lock) | Stores that read and write the database per request, with the counters below in it |
| Usage is kept as counters per scope and UTC day in memory, rebuilt from the entries, and spend has an entry per model call | The same counters in the shared store, checked against a rebuild |
| The live spend tally is per process, so two servers each see half a scope's calls between reads | The spend counter in the same shared store as the admission counter |
| Policy history grows without bound | Paging, as for conversations |
| The inbox record grows without bound, and open counts are a scan | A durable log, with counters per reader |
| The run index and the usage ledger are read whole at start and kept for as long as their rules say (the run index for ever, usage for 62 days) | Retention for runs, and reads that page from the database |
| A download reads the whole file into memory, which the 2 MB cap makes safe | Streaming, before the cap is raised |
| The journal is a ring of 100,000 events | An event store, or OTel export |

## Status

The platform and presentation sources now share this repository. UI packages use the `@warp/*` scope and one pnpm workspace; GitHub presentation belongs to the main Go module. No sibling UI checkout is required.

Releases run the reusable CI workflow before publishing a versioned Go module
and GitHub release from `main`, using the main-only `release` environment.

| Piece | State |
|-------|-------|
| GitHub and Slack integrations: signed webhook ingress, tenant-bound tools and delivery through `Invoke` | Built, tested with simulated APIs |
| `pkg/clients`: a workspace's Slack apps and GitHub installations as records, each with an identity, an allow-list of agents, options and an enabled flag. Several per surface per workspace, and several workspaces per process. See [clients](clients.md) | Built behind `clients.Store`, in memory or kept with `-state=postgres`, where a restart starts every enabled client again. A client the environment configures is not kept: it is made again at every start. Tested with two clients in one workspace and two workspaces in one team |
| Routing a webhook to a client: offered to the clients whose identity it states, taken by the first whose own secret verifies the bytes. 404 for unknown or disabled, 401 for a signature no offered client accepts | Built. One surface per address scheme sits in fiber's registry and fans in and out to the clients behind it |
| Client secrets: written as a value or an `env:WARP_*` reference, never returned or logged | Built behind `clients.Secrets`. In memory, or kept with `-state=postgres`, where a value is stored as written: guard the database as you would the secrets. A reference keeps no value |
| Secret references are default deny per tenant: read only for a variable the operator listed for that tenant with `-secret-refs`, checked where the secret is read, refused the same way whatever the reason | Built, tested across tenants and against a record written straight into the store |
| Client limits: `max_clients` and `max_clients_per_surface` on the tenant's policy, and a `client` policy scope for runs, concurrency, spend and tokens | Built on `pkg/policy` and the admission lock. Storage per client is not |
| A default client per surface from the `WARP_SLACK_*` and `WARP_GITHUB_*` variables | Built. Managed by the environment and read-only in the API |
| Client API: `/api/clients` create, read, replace, delete, behind `clients:read` and `clients:write` | Built, tested across tenants |
| Console for clients: Settings, Connections lists them, and an admin can disable or remove one | Built. Creating one, and setting its limits, are API calls. Not checked in a browser |
| Minting GitHub installation tokens, Slack OAuth installation | Not built. A client is given a token |
| GitHub auto-label and per-commit reviews, PR guides, explicit writer-authorized fix commits and workflow reruns | Built in `pkg/integrations/github`, on fiber API capabilities. Rendering in `internal/presentation/github` |
| Slack agent-session status and code-channel command policy | Built in `pkg/integrations/slack`. Signed command decoding and optional `AgentAPI` in fiber. Not live-validated |
| Slack stop button: `agent_session_stopped` asks the runtime to stop the runs answering in that session, confirms in the thread and settles the status. The run's message keeps its partial answer, marked stopped | Built, tested end to end against the fake with a scripted model whose call is shown to be cancelled. The session-to-run map is in memory and per client: one app's button cannot stop a run another app raised. Not live-validated |
| Slack app (`apps/slack`): Socket Mode, a client of the HTTP API as a machine. DMs, mentions, `/agent`, shortcuts, Home, native streaming, session status, stop, inbox items in the run's thread. A thread's conversation is derived from the thread, so a restart loses neither it nor deduplication. Name, tagline, disclaimer and suggested prompts come from a checked branding file (`SLACK_BRANDING`). Streams a crashed process left open are closed at the next start from a file journal. A silent or slept-through socket is replaced, with backoff, and the app exits non-zero after repeated failures. A person the app's client maps posts as themselves into the warp thread their Slack thread is (`POST /api/threads` with a surface): the platform reads the tags and files it, the people's messages since the app was last asked there go first as their own messages, posted only, and a message posted before starts nothing again. Anyone else asks through the app, as before, with people's messages folded into the run's text. The Home tab reads a mapped person's own inbox through the client (`act_as`), five that need them and five announcements, to read | Built, tested with fakes for Slack and warp (`history.test.ts`, `announce.test.tsx`, `dm.test.tsx`) and against a real server with a fake Slack. A run is recorded as the Slack person's when the app's client maps them, else as the app's. A message that starts several runs is followed for the first; the others answer in warp. The shared Home (`homeView`) is not used yet: it has no What's new or Your projects section, and its start buttons have no handler here. Not live-validated |
| Questions in Slack: a run's question shows in its thread with warp's approval and choice cards and live buttons, answered through warp as the Slack person the app's client maps (`slack.machine`, `slack.people`), refused for anyone unmapped without asking, and settled in place | Built, tested with fakes for Slack and warp. A reply to a question in the inbox, and to a mention in a person's direct messages, is typed in the reply modal (`replyModal`), sent as the person through the client; Something Else on a run's own card still points to the console. Not live-validated, and not run against a real server: no model that asks is wired into `test:server` |
| Slack and GitHub coworking in projects | Built for Slack. A project's owners bind a channel to a Slack channel of a Slack client (`slack {client, channel}` on the channel, in the API or the Platform document; one Slack channel bound once, archiving unbinds it; `GET /api/clients/{id}/bindings`). A client's `home` is a project and one of its channels. The client's machine posts a mapped person's Slack message with its surface: one Slack thread is one warp thread, found by its surface or started and filed from the binding, shared in a bound channel and otherwise in the home, private for a direct message or the assistant pane; the text is read in the one grammar, each message ts is posted once, the client's default answers outside a bound channel, and its allow-list holds, `@auto` included. The machine reads its own surface's threads wherever they are filed. An in-process connector's run is filed by the same binding (`stamp`), and `runs.ProjectOf` believes it for `slack` and `github`. Announcements of a project whose general channel is bound are posted there top-level once, with a delivery receipt, so an edit, a withdrawal or a restart updates that post; drafts never reach a channel. The event stream tells a client's machine of a mention or a question addressed to a person it maps, and for whom; the app reads it for them (`act_as`), posts it to their direct messages with Open Thread and Reply, unless it was raised in the app's own Slack thread, and keeps it current. A reply in the thread settles the mention of its poster. Tested in `pkg/projects`, `pkg/threads`, the platform (`surface_threads_test.go`), the contract and the Slack app. Not built: GitHub threads (a comment's `@agent` and `auto_labels` already tag, and its runs are filed in the client's home, but an untagged comment answered by the client's default needs fiber's GitHub connector to raise one), a reply in an announcement's Slack thread starting a shared thread seeded with it, and announcements of members-only projects, which the machine cannot read |
| `packages/adapter`: the shared platform-to-UI adapter, run events and inbox messages to warp's model and warp actions back to platform calls, shared by the console and the Slack app | Built. A parity test renders one adapted state with both renderers |
| Slack app routing: a message that names no agent, with no client or app default, is sent with no agent, so the platform's default chain decides. The app picks nothing itself, and when the platform answers 409 it says "Tag an agent, for example @helper". `@auto` in a message, or a default of `auto`, is sent as `auto` only where the surface may route to any agent (no client, or a client whose list is `*`): the platform picks among every agent available for an API run, so a client that lists its agents treats `auto` as a name outside its list | Built, tested with fakes. `pnpm validate:slack` was not run for this change: its checks call slack.com, which this environment could not reach |
| `pkg/call`: edge, modes, depth, fan-out and lease checks | Built, tested |
| `pkg/runs`: run index, call tree, statistics, and what each project holds (its conversations, the runs in flight in them and when one last changed), counted as runs are recorded and change rather than scanned | Built. Kept with `-state=postgres`: recording a run is written through, and what its events change is written behind, every 250ms and when the platform stops. A run's usage tally is not kept, so a run working across a restart tallies afresh until the runtime's own figure arrives |
| `internal/platform`: fiber composed, `Invoke`, `Call` with the spawn allowlist | Built, tested |
| Ledger reconciler: ends runs whose retries ran out with no final event | Built, tested |
| `pkg/auth`: OIDC sign-in, machine tokens, roles and scopes | Built, tested against an in-process issuer |
| Okta SSO and Tailscale Serve identity | Okta uses OIDC without forcing reauthentication. `-auth=tailscale` trusts Serve on loopback, maps namespaced subjects through the authorization policy, and needs no application sign-in |
| `pkg/authz`: a policy language mapping claims to roles and roles to permissions, with Explain | Built. The built-in policy reproduces the three roles. `WARP_AUTHZ_POLICY` loads a file |
| `pkg/auth/mockidp`: a loopback-only development identity provider, `-auth=mock` | Built. Tokens go through the real verifier |
| HTTP API, sessions, event stream | Built, tested |
| API contract: `api/openapi.yaml`, OpenAPI 3.2, every route with its permission and a typed event union. Tests check it against the registered routes, the permissions the server enforces, the Go types' JSON fields, and real responses and the event stream. See [API](api.md) | Built. Webhook ingress under `/connectors` is left out on purpose |
| `packages/client`: `@warp/client`, generated by oag from the contract, with a hand-written layer for auth, decoded events and downloads | Built, tested against an in-process server and run once against `make dev`. No surface uses it yet: the console still has its own `apps/web/src/api.ts` |
| Schedules through fiber's cron trigger: pause, run now, next fires, history | Built. Kept with `-state=postgres`, paused or not |
| Editing a schedule (`PUT /api/schedules/{name}`): expression, agent, text, project and a description, keeping its pause state, creator and history. The edit is checked in full before it replaces anything, and an edited schedule starts afresh rather than catching up on its new expression | Built, tested |
| `pkg/journal`: event log, query, run traces | Built, a bounded ring. Kept with `-state=postgres`, one record per place in the ring, written behind every 250ms and when the platform stops, so a crash loses what arrived since |
| `pkg/alerts`: rules on run failure, failure rate and duration | Built. Kept with `-state=postgres`: rules, alerts, numbering and cooldowns |
| Configurable alert rules: replace (`PUT /api/alert-rules/{name}`), description, pause, project scope beside agent scope, a cooldown, a `spend` kind over a window in micro-USD, and `notify`, which addresses the inbox message to subjects and `role:<name>` entries among the people who may read alerts | Built, tested. Notify names people by subject, not by email |
| `pkg/knowledge`: sources, documents, BM25 search, per-document exclusion, saved variants | Built, lexical only. Kept with `-state=postgres`, passages included |
| `pkg/memory`: per-agent notes sent with every turn, written by people or by a memory agent | Built. Kept with `-state=postgres` |
| Housekeeping runs: a title agent names a conversation, a memory agent writes notes after a turn | Built. Off until a workspace picks the agents, and never on the offline provider |
| `pkg/evals`: suites of deterministic checks for agents, retrieval metrics for knowledge, scored by saige | Built. Kept with `-state=postgres`. A run going when the process stopped is read back as errored |
| Conversations: threads, branches, any agent on any turn, replayed history with a configurable limit. Listed by project (`project=`), by a project and everything below it (`within=`), `~` for the workspace, and by channel (`channel=`). A conversation from before threads reads as a shared thread in `general` of its project, with its people and agents as participants, and gains a record the first time it changes. `GET /api/threads/{id}` is paged: the thread's record, who answers there, and 200 turns a page, newest page first | Built. Kept with `-state=postgres`, with the run index and the thread store |
| Channels on the project record (`pkg/projects`): every project has `general`, which cannot be archived; at most 20 a project, archived ones included; a channel's title, purpose (sent to every agent answering there) and default, which must be able to answer in the project; the file's channels changed only by the file; every change in the project's history. Routes `GET`, `POST /api/projects/{name}/channels`, `PUT`, `DELETE` (archives) `/api/projects/{name}/channels/{channel}`, behind `projects:read` and `projects:write`. The Platform document's `channels`, on the workspace and on each project, are merged at every start and changed only by the file | Built and tested, kept with `-state=postgres`. A file's channel default is checked for its form at `warp config check`, and whether it can answer only when a message is posted. The console's project page has a Channels tab (`/projects/{name}/channels`) listing each channel with who answers there and New Channel, and each channel is a page (`/projects/{name}/channels/{channel}`) listing its threads, paged on the server, with a composer that starts a shared thread |
| Shared threads, tagging and the room (`pkg/threads`, `internal/platform/threads.go`): a thread's record with participants and a default, people's messages that start no run, the tag grammar (`@agent`, `@auto`, `@handle`, `@sme`, `@sme:topic`, Slack `<@U…>` through the posting client's people, nothing inside code), what a message starts (tags first, each tagged agent side by side, at most 3; tagging only people or experts, an answer, a reply or Post Only starts nothing; otherwise the default the thread inherits from the thread, the channel and the project's chain; one that stopped being available starts nothing and says why). Each run is sent the room up to its message, people by name and agents by theirs, and records the channel and the message it answers. A person posts only as themselves or as the person a client's credential acts for, and no route names an author. Routes `POST /api/threads`, `PUT /api/threads/{id}`, `POST /api/threads/{id}/messages`, `PUT /api/threads/{id}/participants` behind `runs:write`, and an `event: thread` notice on `/api/events` | Built and tested under the race detector, kept with `-state=postgres`. A private thread is read by its participants alone (see Read confinement). In the console a thread people have posted in opens as a room (`/tasks/{id}`, `?message=` to open at one): people's messages by name, agents' answers by AgentMark, where it is shared, an `@` typeahead over @auto, the agents available in the project, people and expert topics, Post Only, and who answers an untagged message, in words; a reply to a question in words is posted in the thread and recorded as its answer. Tested as DOM interactions (`room.dom.test.tsx`) and the typeahead's rules (`tags.test.ts`). The Slack app still files runs, not messages, and reads only the runs of a thread |
| Experts on projects: a person or `role:<name>` with up to 8 topics, at most 50 a project, found along the chain nearest first. `GET`, `PUT /api/projects/{name}/experts`. Tagging `@sme:<topic>` notifies them; a question an agent asks with `asked_of: sme:<topic>` (or `sme`) is addressed to them, narrowed to the spec's `answerers` when it names any, and an answer posted in the thread resumes the run. Every turn in a project is told which topics have experts. The Platform document's `experts`, on the workspace and on each project, are the file's when set | Built and tested. A question with no expert on the chain goes to the spec's `answerers`: project owners, its intended fallback, arrive with project roles |
| Mentions in the inbox: kind and part `mention`, raised once for each person a message tags (and once for the experts a role names), read with `runs:read`; Reply posts a message in the thread as the reader and starts no run, Done sets it aside, both with `runs:write` | Built and tested. The adapter lays a mention out with its own template (Mention) as a message to reply to, the same in the console and in Slack (`parity.test.tsx`); Done is the header's Dismiss |
| Prompt landing, tasks and areas navigation, configuration overview and routing settings | Built. Tasks use existing threads and areas use projects; conversation and execution history remain available |
| Structured helper calls: ordered messages and references, text/summary/JSON Schema return contracts, frozen inputs, validated outputs and one answering agent | Built and tested, on fiber v0.20.0 |
| `@auto`, the built-in router: `pkg/pick`, lexical over name, description and prompt, choosing among the agents available where it is asked, and handing a message nothing matches to the nearest default on the chain that names an agent ("Picked by @auto: matched billing."). With none, nothing runs: "no agent fits; tag one" (409). `auto` is reserved: never an agent's name, never enabled or allowed, only a tag or a default. `GET /api/pick?q=&project=` previews it, and a client tag for `auto` is resolved in `stamp` among what the client may route to, at the workspace until clients have a home | Built, tested in `pkg/pick`, the platform and the contract. The console's composer offers only the agents available where the message goes, with `@auto` first when it is the default there, and says under it who answers ("@ignite answers", "Nobody answers; tag an agent"). A message the person leaves untagged is sent with no agent, so the platform's default chain decides |
| Projects: a tree under the workspace, which is the root. Conversations filed together, shared memory and instructions sent to every agent in the project and below it, schedules whose runs are filed there | Built as a tree with kinds, in its own store: `pkg/projects`, kept with `-state=postgres`. Kinds company, team and account with their rules, and the workspace's own kinds over team or account; a name is the identity, unique ignoring case and never changed; a project moves under another parent under one lock, with no cycle and its height counted, and is archived rather than deleted when it should stay; at most 500 projects, paged, 4 levels, 100 children and 10 company projects. A change and its history entry are one write, and a deleted name leaves a tombstone only `projects:govern` reuses. Delete is refused while sub-projects, schedules, alert rules or limits point at a project. The projects a workspace kept in its settings move there at start, once, repaired if a start stops between the two writes. The Platform document declares projects (title, kind, parent, account, instructions, members), `workspace.instructions`, `workspace.members` and `workspace.project_kinds`: `warp config check` refuses a broken tree with the entry's path, and each start merges the file in, parents first. A field the file sets is the file's (409 in the API), a project made in the console survives a start, and one that leaves the file stays, unmanaged. Tested on memory and Postgres, with 40 racers on one name, and across a restart on Postgres |
| Project API: `/api/projects` pages the tree in order (`parent=`, `within=`, `q=`, `kind=`, `limit`, `before`), each view with the caller's `access` and `actions`; creates, reads a project with its ancestors, sub-projects and the limits that hold it, replaces its title, description, account, instructions, kind, archiving and who may announce by version, moves it (`/move`), deletes, and pages its history; `/members`, `/limits` and `/memory`. `~` is the workspace. Behind `projects:read` and `projects:write`, then the caller's role on the project, with `projects:govern` for the workspace, company projects, their notes and a deleted name. `Settings.projects` left the wire | Built, tested: every route's permission, shapes and documented statuses in the contract tests, another workspace's projects not found, an empty workspace listing `[]`, filing a schedule, an alert rule or a limit racing a delete of its project, where exactly one wins, and the role tests below |
| Layered context: a turn is sent its project chain's instructions, the workspace's first, then the project's notes, its ancestors' notes nearest first, the agent's own notes, and the conversation. The first line says which instruction wins | Built in one function, for conversations and for scheduled and run-now runs filed in a project, and pinned in the run's tag so every attempt is sent the same text. Instructions share 8,000 characters and ancestors' notes 6,000; what is cut is named in the text. The workspace's own notes are kept under the memory owner `project:`, which no agent's name can be, and the agent memory routes answer 404 for any project's owner. Tested in order and within budget, and that an agent named workspace never reads or writes the workspace's notes |
| Console for the project tree: Projects as a tree grouped by kind (Company-wide, headed by the workspace, then Teams, then Accounts), searched by name, title, description or customer (`?q=`, `?kind=`). A project page at `/projects/{name}`, `~` for the workspace, with a breadcrumb and back to its parent. Overview: its tasks paged on the server (`project=`, or `within=` with its sub-projects), Sub-projects and New Sub-project, shared memory, schedules, and its limits with every limit above it in words ("Field's limit also applies here"). Context: its instructions, and read only what each project above sends, the workspace's first. New Project takes a title, a name marked as fixed, a kind and a parent; company-wide is offered only with `projects:govern`, and the workspace and company-wide projects are changed only with it. Settings' projects editor is gone, replaced by links to Projects and the workspace | Built. Checked in a browser at desktop width and at 390px, where the tree indents at most one 16px level and deeper rows print their path, with no sideways scroll. Tested: every address round trips, the tree's layout at 390px, the kind rules, what an update sends, and the pages against a stubbed API. No History, People or Limits tabs yet: they come with project roles |
| Composer project selection and creation | Built. Select project, explicit Projectless choice, the project tree with each project's kind, customer and path ("Account · in Field › Globex"), searched by name, title, description or customer, and inline creation of a top-level team, offered with `projects:write`, that selects the project and preserves the draft. Console terminology uses Projects throughout. See [UI review](ui-review.md) |
| Project icons and collapsed navigation | Built. Shared warp folder icons across project surfaces. Collapsed, the sidebar is a rail of icons, Projects among them, each with a hover and keyboard label; there is no flyout of projects. Compose, history and sidebar direction use distinct symbols |
| Configurable console titles and labels | Not built. Follow-up: allow workspace configuration to override display titles and labels while keeping project identifiers stable |
| Workspace settings, `pkg/prefs`: per-person theme, accent and profile picture, and the projects a person mutes | Built. Kept with `-state=postgres`, the picture included. `muted` holds at most 100 projects the person can see, never the workspace |
| Agents in projects: `agents {enable, allow, default}` on every project, and a home for an agent that lives in one (`pkg/projects`). Available in a project is what its chain offers (an enable list; `*`, every agent with no home; the agents living there) and every allow list on the chain permits. One check, at admission: a run whose agent is not available where it is filed is refused, 403 `globex-helper is not available in Initech`, for every origin (API turns, schedules and run-now, called runs where their root is filed, client tags). Housekeeping is exempt and an eval tries an agent where it lives. A scheduled run refused this way raises one alert while it stays open. Writing a schedule, a default or an enable list answers the same sooner. `GET`, `PUT /api/projects/{name}/agents`; `PUT /api/agents/{name}/home`, 409 while a default, a list, a schedule or alert rule elsewhere or a client would lose the agent; `GET /api/agents` gains `homes` and `enabled_in`. In the Platform document as `workspace.agents`, `projects[].agents` and `agents[].project`, checked by rule 6 | Built, tested: the store on memory and Postgres (restart, a faulty write, 40 racers on one home, tenant isolation), admission and homes in the platform, the routes in the contract test, the file's lists and homes at start. Call policies are not counted as losing a homed agent: they name callers, not places. A schedule written in the instant before its agent is homed elsewhere is not caught by the home's check, and is refused at each occurrence instead. In the console, a project's Agents tab (`/projects/{name}/agents`) shows what is available there and where each comes from ("from Acme", "Lives here"), the default with what it inherits, the enable and allow lists, and Lives Here, which says how many places would lose an agent; on the workspace the default is the Workspace Default, which replaced the baseline switch on the agent page. An agent's Access tab says where it lives and where it is enabled, and moves it. Tested in happy-dom and static renders, and looked at in headless Chromium at 1280 and 390 pixels wide against `make dev` |
| Defaults along the chain: a message that tags no agent goes to the default its place inherits, the project's, each ancestor's nearest first, then the workspace's (the root's `default`, the Workspace Default). With none set nothing runs: `POST /api/runs` answers 409 "tag an agent". A default that has stopped being available is refused, not passed over. The workspace baseline is gone: at the upgrade each workspace's root enables every platform agent and takes the old baseline as its default, or `auto` with none, then the settings are written without it; a stop between the two writes is repaired at the next start. A workspace never written reads the same. Setting the workspace default needs `projects:govern` | Built, tested with the migration on memory and Postgres, including a stop between its writes. Thread and channel defaults come with threads, and a client's with its home |
| `pkg/people` and the Directory's people: each person's name, handle, roles and where they were seen. Recorded when a person who holds any permission makes a request (a sign-in; nothing is written while nothing changed, for an hour), when a client maps a Slack person, and from the policy's grants to a subject at start. A handle is unique across people and agents, `auto` and `sme` are reserved, an agent cannot take a person's handle (409 "a person goes by @dana"), and no email is asked for or shown: a subject that holds an address, as a Tailscale login does, is kept as the key it is, left out of `GET /api/people`, and subjects are never searched. `GET /api/people` (`people:read`, which every built-in role holds through `read on *`) pages it with the person's own picture, served by `GET /api/people/{handle}/photo` and never fetched from elsewhere | Built, tested: the store on memory and Postgres (restart, a faulty write, 40 racers on one subject, the 5,000 cap, tenant isolation), sign-in, a subject that is an address, the contract. Machines are not people. Which projects a person belongs to waits for project roles. The console's Directory (`/directory`, `/directory/agents`, `/directory/projects`, in the sidebar) lists people by name and handle with their own picture and roles, agents by color with where each lives and is enabled, and the tree with what each project holds and the agents living in it; each tab only for those who may read it. Tested in happy-dom, and looked at in headless Chromium against `make dev` |
| Memory approval: what the memory agent writes waits for a person, following saige's memory policy | Built |
| `pkg/policy`: versioned limits per workspace, project, agent and client on runs, concurrency, spend, tokens and lease; a narrower scope can only tighten. A project's limit holds everything below it: a run falls under its project's policy and every ancestor's, and the strictest refuses | Built. Kept with `-state=postgres`, history included. Two pure functions, `Decide` at admission and `Exceeded` mid-run, so a refusal or a stop can be replayed from the policy version it names. The ancestors a run counts toward are pinned at admission (`Run.Within`, `usage.Entry.Within`), and a called run takes its root's. A run filed in a project the tree no longer holds is refused (409), so deleting a child with no limit of its own does not let its conversations out from under its parent's; a file keeps the ancestors it was published under, so the parent's storage still counts it. Tested with 40 racers in two children against a parent's limit of 7, with spend stopped at a parent's limit to the micro-dollar, and with a deleted child's next turns and files |
| `pkg/usage`: append-only ledger of admitted, refused, warned, halted and ended runs, and of spend (one entry per model call, keyed by the runtime's `accounting_id`, and one settlement per run), with counters per scope and day and `Rebuild` from its entries | Built. Kept with `-state=postgres`: an admission that cannot be written is refused, and every other entry is counted at once and written then or, if that fails, at the next write or flush. Admissions and spend are kept 62 days, whole days, in memory and in the database. Tested: a call or a settlement delivered twice counts once, a call after its run settled counts only beyond the settled total, entries out of time order count in their own period, and a rebuild agrees |
| Admission: every run is checked under one lock before it is accepted; a refusal answers 429 or 402 with the limit, the policy version and the reset time, and raises a budget alert | Built, tested with 40 racers against a limit of 7, and at a spend limit reached to the micro-dollar |
| Console for limits: Settings, each project page, and usage against limits in Analytics, with token limits and a lower-bound note on spend | Built. Not checked in a browser |
| Spend and token limits: enforced at admission and mid-run, with the warning threshold and a stop through the runtime. See [Usage and spend](#usage-and-spend) | Built on fiber v0.10.0's usage reports, tested with scripted priced models under the race detector. Not run against a real provider here: no model key was available. Cost is a list price from saige's catalog, not a provider's invoice. A call in flight when the limit is crossed finishes and is counted, so a scope can end a little over its limit |
| Unpriced calls and other currencies: counted apart, never as dollars, one lower-bound alert per scope, bounded by `max_tokens` | Built, tested |
| Asking a person to approve a run at a limit | Not built. A run parks only at a tool call (`approvals`, `ask_person`): fiber has no way for a host to park a run on a question of its own, so a mid-run limit can only stop it, and admission refuses before there is a run to park. It needs a runtime call that raises a question on a run from outside its turn (or parks it at its next model call), with a `budget` question kind |
| Limits set by a project's owner | Built. An owner of a project above sets a tighten-only limit on it (`PUT /api/projects/{name}/limits`, `authority: owner`), refused where an admin set one and when looser than any limit over it, naming the scope that binds, both decided under the policy store's lock. An admin's limits stay `billing:write`'s, and the console's Limits tab shows the chain ("Field's limit also applies here"). A move that leaves a project holding a limit needs `billing:write`, so no move escapes one. Tested in `pkg/policy` and through the routes (`TestAnOwnerLimitsOnlyWhatIsBelowWhatTheyOwn`, `TestAMoveNeverEscapesALimit`) |
| Direct entry after sign-in, with optional personal preferences | Built. Everyone opens the console directly, including first admins. Profile and Appearance settings hold optional preferences |
| Recents: conversations paged by an opaque keyset cursor, 200 a page | Built |
| Read confinement: members-only projects and private threads, one predicate on every read path | Built. One rule, `platform.Seer` over `seesPlace`: a role of at least viewer in the project, and in a private thread taking part, or `projects:govern`. Every read path asks it and answers 404 "not found" for what it hides, exactly as for what does not exist: runs, their events, manifest and trace, threads and their pages, the journal, analytics, alerts and rules, published files and downloads, storage, schedules, the inbox and its counts, memory, limits, usage and notices, eval suites, and the agents that live in a project. The event stream drops a hidden run's events and a hidden thread's or message's notice. Members-only projects are accepted, and account projects are members only from creation; a private thread is read by its participants, tagging a person invites them, and nothing said there is proposed as a note or titled where others read it. What a deleted project held is read by whom it was read by when it was deleted: its tombstone keeps its parent and members. A recap reads only what everyone it reaches may read. The sweep reads every GET in the contract and the event stream as another workspace and as a non-member of a members-only project in the same workspace, checking that a hidden object answers as a missing one and that no list, filter or event carries it (`TestAnOutsiderReadsNothingHidden`, `TestAnOutsiderStreamCarriesNothingHidden`), with `TestSeesNeedsARoleAndInAPrivateThreadToTakePart`, `TestAPrivateThreadsHousekeepingStaysInIt`, `TestTaggingAPersonInAPrivateThreadInvitesThem` and `TestADeletedProjectIsReadByWhoReadItThen`. The console says "Private" in a thread's meta and its row, offers Private in a channel's composer, says on New Project that accounts are members only, and asks an account's owners whether to make one everyone sees members only; nothing is closed for them |
| Project membership and per-project access | Built. Members hold viewer, member or owner, through `*`, `role:<name>` or a subject, read along the chain with `Resolve`: an owner above owns everything below, no role at one level means none below, `projects:govern` owns every project, and a machine matches only `*` and itself. Every project route checks its permission and then the role, which only narrows: a viewer reads, a member creates under a project and proposes notes, which reach its owners' inbox, an owner changes it, its members, notes and who may announce; the workspace's and a company project's notes need `projects:govern` as well, through the memory routes and the inbox alike. A project the caller has no role in is 404, and so is everything in it (see Read confinement); too low a role is 403 ("you need to own Globex"). A project without an entry for everyone is members only; a company-wide project keeps everyone as at least a member, no project is more open than its parent, and nobody makes a project members only below one everyone owns. Accounts are members only from creation, and existing projects keep their entries. Migrated records read as everyone owning them. The console's People tab lists members and owners from above, says when a project is members only, and lets an owner remove everyone. Tested in `pkg/projects` and `internal/httpapi` (`TestAProjectRoleOnlyNarrowsATenantPermission`, `TestAMembersOnlyProjectIsHiddenFromEveryoneElse`, `TestAProjectMayBeMembersOnlyWhereNobodyOwnsAll`, `TestANewProjectGetsDefaultMembers`, `TestMembersProposeNotesAndOwnersKeepThem`) and as DOM interactions (`roles.dom.test.tsx`) |
| Web console on warp: chat, inbox, agent editor with revisions, evals, knowledge, schedules, analytics, alerts, settings | Built |
| Console navigation: New, Inbox, Projects, Schedules, Monitor, Context, Agents, Artifacts, then Pinned and Tasks. Every page, tab and selection has a path, with back and forward, and old `/alerts` and `/knowledge` links land where those moved. Pins are kept in a person's preferences | Built, checked in a browser. Preferences, pins included, are kept with `-state=postgres` |
| Monitor (`/monitor`, `/monitor/rules`): run statistics and the alert rules, with a count of what fired that sends the reader to the inbox (`/inbox?kind=alert`). An alert's link opens the message it raised | Built, tested as rendered markup and by route |
| On a phone: no bottom bar. One small button in the top corner opens the sidebar as a drawer, with a dot when the inbox needs you, and the drawer closes on navigation. Also 40px touch targets on coarse pointers, dictation in the composer, and an installable web app manifest that opens on the inbox | Built, checked in a mobile browser at 390px. No offline support or push notifications |
| Model policies: an agent's primary model, up to four ordered fallbacks and the errors that move a call on (`fallback_on`), on fiber's agent spec. A workspace keeps named policies that agents follow with the provider `policy`; the policy is read when each call starts, and one in use cannot be removed | Built, on fiber v0.20.0. Policies live with the workspace settings, kept with `-state=postgres` |
| Call governance: the agent being called decides which agents may call it (anyone its spec allows, only those listed, or none) and when a call waits for a person (never, always, or unless the caller is trusted), who approves (roles such as manager and admin, or named people) and how long a call waits. A caller is only offered agents that take its calls. Each agent has an approval queue; a held call also waits in its task and in the approvers' inboxes | Built. Policies are kept with `-state=postgres`. Approval rides fiber's per-call approval hook |
| Agent marks: exact pixel footprints with room for the shared orb's halo and satellite, aligned in compact rows | Built on warp `AgentOrb` numeric sizes. The console gallery covers every activity at 12, 14, 16, 18, 24 and 32 pixels in clipped rows |
| Console landing: a prompt-first Home page using the conversation composer, with optional attachments, project selection and agent selection before sending. The former overview is removed. The root opens Home; chat has its own explicit address | Built. Home and new conversation share the same submission flow; sending opens the conversation. Run permissions are enforced |
| Console menu: Home, New conversation, Inbox, then Work, Build, Watch and Workspace. An entry is hidden from someone who may not read what it opens | Built. Connections, Access and Limits open their Settings sections |
| "Why can I do this?": `GET /api/access/explain` and the Access panel. A sign-in keeps the grant behind each role, not the claims or the token | Built, tested. Sessions are in memory, so a restart signs everyone out |
| Local accounts with a first-user admin, for running without an identity provider | Not built. Without OIDC, use Tailscale Serve identity, loopback dev auth or the loopback mock provider |
| Model parameters on an agent (temperature, token and step limits) | Not built. fiber's agent spec has no fields for them |
| A stop control in the console's composer | Not built. The API stops a run in flight and the console's event adapter ends a turn on `lifecycle.stopped`, but the composer has no stop button and its queue still waits for the run to end |
| Skill consolidation | Not built |
| Sandbox: each conversation has a scratch directory that agents with the file tools read and write. It is opened as an `os.Root`, bounded in file size, count and total, and removed after 7 days untouched | Built, tested against traversal, absolute paths and planted links |
| `pkg/blob`: put, get, stat, delete, list behind one interface, on disk, in memory or in an S3-compatible bucket (AWS, MinIO, R2), with a conformance suite a new backend must pass | Built. The S3 store is chosen with `-storage=s3` and the `-s3-*` flags, credentials from `WARP_S3_ACCESS_KEY_ID` and `WARP_S3_SECRET_ACCESS_KEY` or the AWS default chain. It passes the suite against a fake S3 in the unit tests and against a real S3-compatible gateway (versitygw) through `TestS3Live`, which runs when `WARP_TEST_S3_BUCKET` is set. Not run against AWS, MinIO or R2 themselves |
| `pkg/artifacts`: published files with a sniffed type, sha256, and a version per title per conversation | Built. The index is kept with `-state=postgres`, the bytes are in the blob store. An id that was published is never issued again, so no blob key is reused. Unit tested on its own: titles, versions, tenants, paging, usage, and concurrent use under the race detector |
| Publishing: the `publish_artifact` tool copies a sandbox file to storage and reports it on the run's stream, where the console shows it as a card | Built |
| Storage limits per workspace, project and agent, as `max_storage` on a policy | Built. A project's use is what its whole subtree holds, and a file counts toward every ancestor's limit. A refusal raises a message in the inbox |
| Artifacts page (`/artifacts`, one artifact at `/artifacts/{id}`): delete a version, image preview, over the limit, See more | Built. Tested through the API (`TestStoragePageFlows`), as rendered markup (`storage.test.tsx`), and by clicking through it in happy-dom (`storage.dom.test.tsx`). Not checked in a real browser |
| Downloads: tenant and right checked on every request, always an attachment unless an inert type is asked for inline, never HTML or SVG inline | Built, tested |
| Built-in agents: `warp-writer` writes a document and publishes it. Provided by the platform, not editable, present only when the server is given `-builtin-provider` and `-builtin-model` | Built. One agent. The server refuses a save, and the console's agent page opens it read only with a note saying why; its memory can still be changed, and it can still be the workspace default. Tested in happy-dom (`agents.dom.test.tsx`). Not run against a real model here: no provider key was available |
| Called agents are runs: an agent that tags another starts a run with its own id, events and record, in the caller's conversation and sandbox | Built, on fiber's `WithDelegator` hook |
| Run ids are the runtime's, derived from the workspace and the tag id together, so two workspaces offering one trigger id get two runs. Every index, route, ledger entry and grant uses the id the runtime returns | Built on fiber v0.7.0 and later, tested with two workspaces reusing one tag id and one Slack event id |
| Two lifetimes for a called run. Attached (`call_agent`, subagent mode): ends with its caller, and the caller is not done until it is. Delegated (`call_agent`, delegate mode): independent, and its answer goes to the inbox | Built, tested three levels deep under the race detector |
| Stopping a run: `POST /api/runs/{id}/stop` asks the runtime to stop it and everything attached to it. The model call in flight is cancelled, the run ends `stopped` with its partial answer and cause, and what it delegated keeps running. See [Stopping a run](#stopping-a-run) | Built on fiber v0.8.0's `runtime.Stop`, recorded in the run ledger, tested with scripted models under the race detector, three levels deep, and against a completion racing the stop |
| A place is always kept for work that waits on nobody, so runs waiting on each other cannot fill every place and hang | Built. With `Concurrency` 4 by default, three runs may wait at once and a fourth is told no |
| Publishing that waits for approval | Built: list `publish_artifact` in an agent's `approvals`. Nothing is stored until a person approves, a denial publishes nothing, and an approver who edits the arguments publishes what they approved. Tested with scripted models |
| Built-in agents, when the server is given `-builtin-provider` and `-builtin-model`: `warp-writer` (documents), `warp-analyst` (`runs_summary`, `list_alerts`, and files for reports) and `warp-builder` (`list_agents`, `read_agent`, `save_agent`). Every save waits for a manager or an admin; only the builder may save, it cannot edit built-ins, and it may grant only file tools and `ask_person`. The console offers them as starters on a new conversation, from Monitor and from Agents | Built, tested with the offline provider (`TestBuiltinAnalystAndBuilder`, `TestRunsSummaryTool`, `TestSaveAgentTool`). Not run against a real model here
| Parking a run on a person: approvals of tool calls, choices and replies (`ask_person`), on fiber v0.11.0 with the run queue as waker, warp's roles, and a question deadline. See [People in the loop](#people-in-the-loop) | Built. Answer recording and inbox settlement are serialized so resumed or terminal events cannot drop surface attribution. Tested under the race detector with scripted models: each kind, who may answer, a requester refused, two answers at once, expiry, stop while parked, a called run's question, usage counted once, tenant isolation. Not run against a real model here: no model key was available |
| Agent editor: may ask a person, approve a tool first, who may approve, self-approval, who may answer | Built. Not checked in a browser |
| Run manifest: one JSON document of a run, every run under it, each pinned definition, every event with its body, and the tree's usage | Built, from the journal. A run older than the journal's ring comes back marked incomplete |
| Token counts, context window and cost per run: `usage` on the run (own, rolled-up attached runs, total, context), live while it works and the runtime's record once it ends, recorded once whatever is redelivered | Built, tested with redelivered events, a reconciler pass, a stopped run and a three-run tree. Context fill is the latest call's prompt against the window saige's catalog declares; a model with no declared window shows tokens only |
| Leases on real cost: a called run is held to its lease by what it spends and what it has handed on, and settles with its caller when it ends, climbing past a caller that settled first | Built, tested, with a grant made before the caller's own call was counted and a call that ends after its caller. An API caller still names a lease; an agent's own calls lease half of what is left |
| Usage in the Slack app | Not shown, on purpose: the console shows none on a turn either. A warp usage summary would let both show it |
| Uploading binary files (PDF, images) to knowledge or a message | Not built. Files are read as text in the browser |
| Evals against a knowledge variant | Not built. Evals search the current view |
| Summarizing history | Not built. Long conversations are truncated to the limit, oldest turns first |
| Knowledge retrieval inside a run: an agent's declared `sources` ground each turn, searched in the run's own tenant, the passages labelled so fiber's citation events line up with what the model saw | Built, tested with a scripted model in two tenants. Lexical (BM25) only; a source the workspace no longer has grounds nothing |
| Connections: MCP servers, OAuth to third parties | Not built. Slack and GitHub credentials are held per client, see above |
| Schedule delivery: where a scheduled run's answer goes, set as `deliver` on the schedule and pinned on each run it raises | Built for `deliver: inbox` and `deliver: project`. Inbox: a completed run's answer is sent to the schedule's creator as a result only they see, from the clock or a person's run now (`TestAScheduledAnswerReachesItsCreator`). Project: see the recaps row. An answer of NONE sends nothing. Slack, email or another system: not built, needs connections |
| Scheduled agents in projects (recaps): a schedule with `deliver: project` posts each answer in its project as an announcement from the agent, the first line its subject and the run linked; `agent_posts` (`publish` or `review`, review by default at the workspace and in company projects) holds it instead as a draft addressed to the project's owners, who publish it (posted from the agent, recording who approved it) or discard it. `project_changes(days)` reads the run's own project and the projects below it (runs by agent and project, threads started and settled, announcements, files published, notes kept, schedules created or changed, the projects' own record changes; 50 rows a section, 32 KB in all), and `runs_summary(project)` counts a subtree; both are granted to `warp-analyst`. File-managed schedules (`projects[].schedules`, `agent_posts`), the project's Schedules tab and Add Weekly Recap | Built, tested: a team project's post reaches exactly its members, NONE posts nothing, a draft is shown only to an owner and a member cannot publish it, publishing twice posts once, a post past 20 a day or 20 open drafts goes to the schedule's creator saying why (`TestARecapIsPostedInItsProject`, `TestAReviewedPostWaitsForAnOwner`, `TestARecapPastTheDailyLimitReachesItsCreator`), `project_changes` reads only its subtree, a file schedule is the file's (409 on a change or delete through the API, released when it leaves the file, its agent available in its project or the start fails). A schedule in the file cannot deliver to an inbox: it has no creator. A recap at the workspace has no `project_changes`, which needs a project |
| Call tool inside the sandbox | Not built. Calls are made through the API |
| Returning a subagent's answer to its caller | Built for a call made by tagging: the answer is the tool's result. A subagent that was stopped returns an error saying so, not its partial answer. A call made through the API is not returned to the caller's model |
| Cancel propagation down a tree | Built through attached runs: each is asked to stop through the runtime, at the request and again when the runtime cancels the delegation it was waiting on, and keeps its own partial answer. Delegated runs are not touched |
| Access intersection across a call | Not built |
| `pkg/inbox`: one server inbox of messages with typed parts, per-person read state, paging, and a record of changes that rebuilds it | Built. Kept with `-state=postgres`: each change is written before it takes effect and replayed at start, and one that cannot be written is undone and not announced |
| What lands in the inbox: fired alert rules, failed runs no rule caught, proposed memories, reached limits, the answers of delegated runs, and published artifacts | Built. An alert and its message are one state: settling either settles both. An artifact's message is there to be read: a new version withdraws the message about the one before, and deleting a version withdraws its message (`TestPublishingTellsTheInbox`) |
| Inbox templates: each message is laid out by its template (alert, approval, question, failed run, memory, limit, artifact, result, note), named by the server's kind or read from its parts (`messageTemplate`). The console's list puts what needs a person above updates, filters by kind with chips, and reads a message as an email does: subject, sender, then its parts, with Snooze and Dismiss in the header | Built, tested as rendered markup (`ui.test.tsx`) and checked in a browser through the gallery, dark and light, wide and phone width |
| Resolving in the inbox: acknowledge, retry a failed turn, keep or discard proposed memories, raise a limit, dismiss, snooze | Built. Each needs the right for the thing it does, and a second resolve returns the first outcome. A project's proposed notes are kept or discarded with `projects:write` by an owner of the project, and with `projects:govern` as well for the workspace's and a company project's: what the project's memory routes need |
| Approve, deny, choose and reply parts for a run waiting on a person | Built. Raised from `lifecycle.parked`, answered through `runtime.Answer` from the inbox, the run, or Slack, each action needing `runs:write`, and settled in place when the run resumes, expires, stops or ends. Shown by the shared adapter as warp's cards on the run and in the inbox alike, with a parity test per kind |
| Messages addressed to one person, one role, a project's members or its owners | Built. Questions are addressed to the principals and `role:<name>` entries that may answer, never to the requester of an approval. An announcement is addressed to `project:<name>`, which reaches the members of the project and of every project below it, and a proposed project note to `owners:<name>`, which reaches whoever owns the project; both are decided from the tree at each read, so a revoked membership hides them at the next one. The event stream carries a notice only to a reader the message is for. Every other kind still goes to everyone who may read it |
| Project inboxes and announcements | Built. A project's inbox is every message filed in it (`GET /api/projects/{name}/inbox`, with its sub-projects on request), read by everyone who can see it. An announcement is one broadcast message (`POST /api/projects/{name}/announcements`, by the role the project lets announce, owner by default; at most 20 a project and 200 a workspace a UTC day, counted under the inbox lock), which each reader hides ("Hide for Me") or snoozes for themselves, rebuilt on replay, and its author or an owner withdraws for everyone, recorded with who. `Platform.Announce` is the one way in, for people and for scheduled agents. Muted projects (`muted` in preferences) stay out of a person's own inbox and in the project's; the workspace is never muted. `GET /api/inbox/count` adds `updates`, so announcements never grow the badge. The console opens a project on its Inbox tab and shows What's new on Home. Posting from a scheduled agent, drafts and Slack delivery are not built. Tested in `pkg/inbox`, `internal/platform` and `internal/httpapi` (`TestAnAnnouncementReachesTheMembersOfItsProjectAndBelow`, `TestAnnouncementsNeverGrowTheBadge`, `TestHidingAnAnnouncementHidesOnlyTheReadersCopy`, `TestMutingKeepsAProjectsNewsOutOfOnesOwnInboxOnly`) |
| Alert delivery outside the console | Not built |
| OTel export | Not built. Traces are rebuilt from the journal |
| Evals that run on their own, on a schedule or on each revision | Not built. Evals run when someone starts them |
| Model-graded evals | Not built, on purpose for now: every check is exact |
| Email connector, MCP server and Jira wiring | Not built |
| Remote execution nodes, peer connector | Not built |
| `pkg/state`: one record store for warp's own stores, by namespace, tenant and key, in memory or in Postgres (one table, migrated by `OpenPostgres`), held to the `statetest` suite. Each store keeps its rules and indexes in memory and writes through it, undoing a change that cannot be written | Built, tested against both backends on Postgres 16, with a restart test per store and a failed-write test per store. One writer: a second process on the same schema gets `state.ErrLocked` |
| Choosing the backends: `platform.Config.Backends` (warp's `state.Store`, fiber's `registry.Store`, anchor's ledger and queue), zero for memory; `platform.OpenPostgres` puts all of them on one database, with fiber's `registry.Postgres` for agents and fiber's `pkg/durable` for the run ledger and queue. `warp serve -state=postgres`, the default when `WARP_POSTGRES_URL` is set | Built. `TestThePlatformSurvivesARestart` runs a platform on each backend, stops it and starts another on the same one. Run by hand against Postgres 16 with `-auth dev -seed`: settings, an agent, a client, an alert rule, runs, the inbox and artifacts survived a restart. Sessions are still in memory |
| In-repo deploy, on [the spec](spec.md): a container image (`Dockerfile`: the console and a static `warp`, non-root, `/var/lib/warp` volume, `serve -f /etc/warp`), a compose stack (`deploy/compose`: a mounted `warp.yaml` with System and Platform documents, secrets in `.env` named by references, Postgres, MinIO and Redis profiles, a dev override on host networking, `make up`/`make down`), a Helm chart (`deploy/helm/warp`: `system` and `platforms` values rendered into one `warp.yaml`, one replica with `Recreate`, `existingSecret`, a workspace PVC; refuses loopback-only sign-in) and an example Flux setup (`deploy/flux`). See [deploy](deploy.md) | Written. `make chart-check` lints and renders the chart, runs `kubeconform -strict` on the renders and `warp config check` on each rendered `warp.yaml` and on both compose specs. The compose files pass `docker compose config`; the dev spec, with memory stores, was started and served the console. The Flux manifests pass `kubeconform` after `kustomize build`. `config check` lists env references the shell has not set; it does not read keychains or named files, so those fail only at start. The image was not built (no Docker daemon), and nothing was run in a cluster. No image is published by releases |
| Call policies: each agent says which agents may call it (`anyone`, `listed`, `none`) and which calls wait for a person. `GET`/`PUT /api/agents/{name}/call-policy` (`agents:read`/`agents:write`), enforced in the call path beside the caller's spawn allowlist, and the `call_agent` tool offers only callees that admit the caller | Built, tested. In memory per tenant, at most 1,000 policies and 100 names per list. No console screen yet |
| Discoverability: `GET /api/agents/{name}/reachable` says, for that agent as a caller, whether each other agent admits it, is in its spawn allowlist, and needs approval, with a reason | Built, tested. One page of up to 500 agents, with `truncated` |
| Per-call approval held by the callee's policy: `call_agent` implements `agentrt.Approving`, the question carries the policy's approvers and self-approval, the inbox addresses it to them, and only a call a person approved goes through | Built, unit tested. End to end it waits on fiber's host call tool wrapper forwarding `Approving`; until then the call is refused (fail closed), which the end-to-end test checks and then skips. `expires_seconds` is not applied yet |
| An agent's approval queue: approval questions about calling an agent carry it as `source.target`, and `GET /api/inbox?kind=approval&agent=X&state=open` lists what was raised by or would call `X` | Built, tested |
| Built-in `manager` role: everything an operator has, and writing agents (their call policies among them) and knowledge. Granted by `-manager-group` (`warp-managers`); mock persona `mock-manager` | Built, tested |
