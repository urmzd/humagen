# Company, personal work and finding help

Implementation in progress. This follows the workspace direction given
on 2026-10-22. The current behavior remains described in [Projects](projects.md)
and the [architecture status table](architecture.md#status).

## One Company project

The workspace root is the default Company project. Its visible name is
Company until the operator supplies the company's name. Do not introduce a
second root or a sibling company project just to hold the built-in agents.
The stored root key stays unchanged; an empty stored root key identifies
Company and must never be presented as a projectless state.

Every agent has an explicit home. The writer, analyst and builder supplied by
Humagen live in Company and are offered in its descendants under the existing
enable and allow rules. Their stored agent IDs stay unchanged. A home says
where an agent belongs, not where every run of that agent executes: asking
the analyst inside a person's project files that conversation's work there.

Today `SetHome(..., Root)` deletes the home record and calls the result a
platform agent. That representation must change before the UI can claim that
all agents belong to a project. Keep an explicit root home, migrate legacy
missing homes to root once, and distinguish an omitted home update from an
explicit Company selection. Update the agent editor, directory and project
rows together. New agents require a valid home, defaulting to the project in
which the person creates them.

## A personal project for each person

Within each company, provision one personal project for each human member.
Keep a durable tenant-and-subject association and an opaque immutable project
ID. A display name or an email address is not an identity key. Concurrent
first visits and subsequent sign-ins find the same project; machines do not
receive one. Provision at a trusted membership boundary, with an idempotent
first-use path for existing members. Existing limits still apply: a failed
provisioning attempt must not silently file private work in Company.

Home's composer starts new internal conversations in that person's project,
private to their participants. Company agents remain available there through
the existing chain. A selected public project or a bound Slack channel
continues to use its explicit destination. Show the current destination by
name before sending; there is no projectless choice.

The project record needs an immutable, server-managed `personal_owner`
association. Its owner is its only ordinary member by default. Other people
must not discover its name, counts, notes, sources or messages through lists,
search, discovery or notifications. Do not advertise this privacy merely by
setting the existing member list: ancestor ownership currently flows down
through `projects.step`. Personal project confinement must explicitly limit
ordinary reads to its owner, while `projects:govern` remains an exceptional,
recorded read. Route permissions continue to apply separately.

Apply the same boundary to writes: inherited Company ownership alone permits
no changes to a personal project's records, settings or conversations.
Administrative changes require explicit governance authority and the normal
write audit. A thread invitation cannot grant access outside the personal
project's owner boundary; refuse such invitations without notifying anyone.
Collaboration begins by publishing or moving the conversation into a shared
project, rather than changing the personal project's audience.

Use the same rule through `p.Sees` on every read path, including tombstones,
inherited context and aggregate counts. A personal project cannot acquire
wildcard membership, be reparented into a more open audience, be Slack-bound,
or be shared by publishing its whole record. Sharing work publishes a
conversation into another project. Deactivation preserves records and their
confinement under the workspace's retention policy.

## Moving work into a public project

The normal flow is **Move to Project**, then choose a readable project and
channel in which the person is allowed to publish. Show the resulting
audience before committing the move. Public means shared with that project's
readers, never anonymous internet access. Only the conversation's starter or
an authorized owner who participates may publish it; an invited participant
and a governor merely reading it cannot publish someone else's work.

This must work after an agent has answered. Today `Share` refuses moves after
the first run because run records and their artifacts remain filed in the
original project. Removing that guard alone would produce inconsistent read
permissions and accounting. The implementation must first separate:

| Concern | After a move |
|---|---|
| Current audience and placement | Resolve from the conversation's current project and channel |
| Historical spend, policy decisions and execution provenance | Keep the project recorded when each run was admitted |
| Future turns | Admit and compose against the destination's policy and context |
| Agent availability | Revalidate against the destination; explain unavailable selections |

One authoritative placement resolver must cover the thread, its runs and
child runs, attachments, artifacts, events, inbox references, traces, search
and counts. Do not implement a best-effort rewrite of many independent
stores. Retain immutable accounting attribution and a durable, versioned
conversation placement. Commit the placement and its history atomically,
with optimistic concurrency and the existing admission/move lock. Refuse a
move while a run is active, parked, being admitted or delivering work; the
person can move it after that work settles.

Previously composed private project instructions, notes and source material
can appear in the conversation. Preview what is being published and require
an explicit publish action before changing its audience. Do not copy the
personal project's settings, memories or knowledge declarations. Recheck
notifications and deliveries after the move so an old address grants no
new access. Surface-bound conversations keep their binding's placement.
Replay must preserve both the new audience and the original accounting.

Immutable attribution is internal accounting data, not permission to reveal
the former personal project's identity. Run JSON, manifests, tag metadata,
history and artifact provenance must omit its name and ID for destination
readers who cannot see it. Return a neutral indication that the conversation
was moved; an authorized auditor can still inspect the full attribution.

## An announcement above the greeting

The page order is a compact announcement banner, the greeting, the composer,
then Needs you and What's new. Keep the greeting as the page's only `h1`.
The banner is a labelled region, not a second page title or an animated alert.
It remains above greetings such as Good afternoon, Developer at phone and
desktop widths.

The welcome message becomes a real Company announcement with a durable
idempotency key, using the existing system announcement path and retention.
Banner presentation reads the same announcement and per-person unread,
read and hide state as the inbox. No second welcome flag, duplicated welcome
box below the composer, or arbitrary raw HTML. Show at most one announcement
with its source, a short title, readable copy, an optional action and the
existing read/hide controls. Existing announcement permissions and
confinement decide whether it is shown. Centralize text in
`packages/components/src/copy.ts`.

## Find the project and agent for the work

Remove SME from the product vocabulary and retire new `@sme` and
`@sme:<topic>` routing, expert configuration controls and expert prompt
injection. Replace them with **Find Help**, a bounded search that takes the
person's description and suggests a project and an agent available there,
with a short reason. It identifies where work belongs and who can help.

Build on existing project availability, agent descriptions and the picker;
do not create a parallel permission system. Filter candidates with `p.Sees`
and the relevant read permission before ranking, counting or explaining
results. Hidden projects, agents, private chats and source names contribute
neither results nor suggestive counts. A suggestion grants no access and
starts no run. The person opens a result or confirms the destination and
agent beside the composer. Selecting a public destination does not move or
publish an existing private conversation implicitly.

Explicit human mentions, approvals and `ask_person` remain separate actions.
Removing SME must not strand an existing question or disable human approval.
Keep old messages and settled expert-addressed inbox history readable under
their original confinement. During migration stop creating new expert
routing, finish existing questions through their recorded recipients, and
report deprecated expert fields in configuration with an actionable
diagnostic. Never reinterpret old SME text as permission to run an agent or
publish a chat. Remove the experts line from real composition and the context
preview together after the retirement is implemented.

## Implementation order and proof

1. Explicit agent homes and Company defaults, with memory/Postgres migration
   and no changes to stored IDs or historical run placement.
2. Personal project identity, provisioning and confinement across the entire
   read sweep, including inherited owners and recorded govern reads.
3. Conversation placement versus immutable execution attribution, then moving
   completed private conversations with replay and admission race coverage.
4. Announcement banner, tested with loading, empty, unread, hidden and long
   copy at phone and desktop widths.
5. Find Help and SME retirement, including pending-question migration and
   hidden-result tests.

Each implementation changes OpenAPI before regenerating the client, adds
regressions that fail first, updates the architecture status in its PR, and
passes the requested CI checks with the Postgres variant and screen captures.
This document prepares the work; it does not change the current runtime or
claim that completed conversations can already move between projects.

## Implementation evidence

Company homes are explicit stored root records. Missing legacy homes migrate
idempotently in registry pages without changing enable or allow rules.
`GET /api/me` provisions a human's personal project and returns its address;
provisioning errors are returned, never replaced with Company. The project
store retains a random identity and immutable `personal_owner`, serializes
concurrent provisioning, and restores the association on memory and Postgres
replay. Machines receive no personal project.

Personal ownership narrows inherited Company ownership before role resolution.
Membership widening, reparenting, deletion, subprojects, file adoption and Slack
bindings are refused. Invitations and notification routing cannot add another
human to that private audience. Completed ordinary work can be published after an explicit versioned audience
preview. Private connected-source history requires a reviewed summary path; raw
publication is refused. The run index retains its original accounting, while
operational lists, counts, files, inbox addressing, manifests and live event
streams resolve the conversation's current placement. Publication attempts are
recorded under the source's private scope and placement/history commit in the
same thread-store write. Historical runs cannot replay old private inputs
after publication; the person posts a fresh turn in the destination instead.

Reviewed summary publication creates a separate shared conversation containing
only the exact text the participating owner reviewed. It copies no private
thread link, run, source, citation, file, operation contract or capability. A
content-and-audience token prevents stale consent and makes retries idempotent.
The original conversation and all accounting remain private.

The welcome copy is generated from `packages/components/src/copy.ts` by
`pnpm welcome-copy`; `pnpm welcome-copy:check` checks freshness. `GET /api/me`
returns its retained announcement ID, so Home selects the actual Company
welcome and uses existing per-reader read/hide state. The banner component
precedes the greeting and empty Home boxes no longer duplicate it. Host wiring
and reproducible phone/desktop captures are validated in the upper UI layer.

Find Help scores only readable projects and readable agents available there,
returns at most eight suggestions, and neither starts work nor changes an
existing conversation's placement. The component requires explicit selection
and shows actual request failures. New expert-routing tags and cached completion choices are retired, and the
expert prompt layer is removed from real turns and context preview together.
Legacy explicit human questions retain their recorded recipients. Config
check/startup diagnostics direct operators to Find Help.

Publication consent now holds the project store's snapshot read guard across
the thread commit, serializing destination membership/channel writes. A
barrier-controlled race regression covers memory and Postgres. Preview tokens
bind source/destination chains and the complete destination channel record.

Private context classification covers durable personal-note markers, retained
source audiences, person-scoped MCP grants and optional registered subsystem
guards, including conversations with no runs. A person-scoped tool must call
`MarkPrivateContext` before executing. Its write-through run bit survives
Flush/replay and never changes the accepted tag or appears in run JSON.

Project history identifiers/cursors are project-scoped opaque HMACs; external
readers never receive the tenant-global sequence. Company rows and current
agent choices hide compatibility builtin aliases without rewriting stored
homes or historical revisions.
