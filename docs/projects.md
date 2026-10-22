# Projects

Design of record for nested projects, project policies, agents in projects,
project inboxes, scheduled agents and shared threads. The
[status table](architecture.md#status) says what exists; this page says what
is planned and in which phase.

## Overview

A workspace becomes a coworking space: a tree of projects, each with channels,
each channel with threads where people and agents work side by side. Nothing
here is a second system. Each piece is built on what warp or fiber already has:

| Need | Built on |
|---|---|
| Nested projects | a new `pkg/projects` store on `pkg/state`. A project's name stays its identity, so nothing filed under a name moves |
| Project level policies | `pkg/policy` project scopes, applied along the project's chain of ancestors. A narrower scope only tightens |
| Platform and project agents | warp's project record. fiber's `AgentSpec` is unchanged. Checked once, at admission |
| Scheduled agents (recaps) | fiber's cron trigger through warp's schedules, filed in the project |
| Project inbox and announcements | `pkg/inbox`, read by `Source.Project`. An announcement is a message kind, not a store |
| Shared threads | warp's conversations (threads, branches, any agent on any turn), plus human messages that start no run |
| Experts (SMEs) | `ask_person`, inbox questions and their addressing |
| Context | fiber's pinned system prompt (the agent's instructions), `pkg/memory` (agent and project notes), instructions on the project record |

Decisions:

1. **The workspace is the root project**, written `~` in addresses. Runs filed
   with no project (`project: ""`) are filed at the root, so no run is
   re-keyed. Company-wide means "in the root".
2. **The tree is a parent pointer**, at most 4 levels below the root. A name
   never changes; `title` is what people see.
3. **Policies walk the chain.** A run counts toward its project, every ancestor
   and the tenant, and is refused by the strictest. This lands in phase 1, with
   the first child project.
4. **No implicit routing.** An agent answers when it is tagged (`@ignite`). An
   untagged message goes to the default the thread inherits, if one is set,
   else it is a human message and starts no run. `@auto` is a built-in agent:
   today's `pkg/pick` and baseline behind an explicit name. The workspace
   baseline setting is replaced by the root's default.
5. **Context is layered and explicit**: the project chain's instructions and
   memory, then the agent's instructions and memory, then the thread.
6. **Every project has an inbox**: the inbox messages whose `Source.Project`
   is that project. Announcements are items in it, from a person or from an
   agent scheduled in the project. A recap is such an agent, not a feature.
7. **Shared threads** are conversations with participants and a visibility.
   Anyone in a thread can tag a person or an agent.
8. **Route permission, then project role.** Every route checks one tenant
   permission; the platform then checks the caller's role on that one project.
   A project role only narrows, never grants.
9. **Privacy is never claimed before it is enforced.** Members-only projects
   and private threads are refused until every read path is confined
   (phase 7).

## Concepts

### The tree

```
~ Acme (root: everyone is a member)    #general  #announcements
├── Platform (team)                     #general  #releases
├── Company news (company)              #general
└── Field (team)                        #general
    ├── Globex (account)                #general  #renewals
    └── Initech (account)               #general
```

Members and reach apply from phase 2, when projects gain roles.

| Kind | Where it sits | Members by default | Its announcements reach | Notes |
|---|---|---|---|---|
| root | top, one per workspace | `*` member | everyone | the workspace itself: cannot be moved, archived or deleted |
| `company` | under the root or another company project | `*` member, required | everyone | at most 10. Created and changed only with `projects:govern` |
| `team` | anywhere | `*` member, creator owner | its members and the members of projects below | the default kind |
| `account` | under a team or company project, never under an account | creator owner, `*` viewer (members only from phase 7) | its members | for field engineers; carries `account{customer, external_id, url}` |
| custom | as its `like` | as its `like` | as its `like` | a workspace label over `team` or `account` (phase 2). Behaviors stay closed and testable, names stay open |

A project inherits down the tree: policies tighten, instructions and memory
are sent, enabled agents add up, ownership flows down. Membership, the `*`
entry and schedules do not flow down, and a child is never more open than its
parent.

### Channels and threads

- Every project, the root included, has channels, like Slack channels.
  `general` always exists and cannot be removed. A channel has an immutable
  slug, a title, a purpose and an optional default agent. A channel has the
  project's visibility; per-channel membership is a follow-up.
- Every thread is in exactly one channel of one project. A thread is a warp
  conversation: turns, branches, any agent on any turn. It gains participants
  (people and agents), an optional default agent and a visibility:
  - **shared**: everyone who can see the project reads it, like a thread in a
    Slack channel;
  - **private**: its participants only (phase 7).
- A turn is either a **human message** (a person posted; no run) or an
  **agent's run** answering a message. A person posts only as themselves; no
  route lets anyone post as an agent, and agent turns exist only as runs.

### Tagging and defaults

One grammar, parsed by the platform for every surface (`pkg/threads`):

| Token | Means |
|---|---|
| `@<agent>` | that agent answers. It must be available in the thread's project |
| `@auto` | the built-in router picks the agent (see [Agents in projects](#agents-in-projects)) |
| `@<handle>` | that person is notified: their inbox, and Slack when a client maps them |
| `@sme` or `@sme:<topic>` | the project's experts (for that topic) are notified |
| `<@U…>` (Slack only) | the Slack person, mapped to a subject through the client's `slack.people` |

What a message starts, first rule that applies:

1. It tags agents or `@auto`: each tagged agent answers, side by side, each
   seeing the thread up to this message. At most 3 runs per message.
2. It tags only people or experts: no run. It is a message to people.
3. It answers a question or replies to a mention: no run. It is a human
   response.
4. The poster chose **Post Only**: no run.
5. Otherwise the inherited default answers. With none, no run.

The default is the first set of: the thread's, the client's (for a thread a
Slack or GitHub client files outside a bound channel), the channel's, the
project's, each ancestor's nearest first, the root's. A default must be
available in the thread's project when it is written (400 otherwise). A
default that has stopped being available is not skipped in favor of the next
one: the message is kept as a human message and the answer says why (fail
closed).

At most 10 mentions per message. A token inside inline code or a code block is
not a tag.

Every run a message starts acts for the person who posted it, never for
another participant: a tool that acts as a person uses the poster's own
credential. The run is sent the room up to the message, as a mention in a
Slack channel thread is: one turn a line, its line breaks made spaces, after
who took it, a person by name and handle, which no one else has, and an
agent marked as one. The room is fenced off from the poster's words, which
follow with who posted them, so nothing anyone else wrote reads as the
poster's, and another person's request in the thread is acted on with the
poster's credential only when the poster's own words ask for it. A failed
answer is retried only by the poster, from their inbox.

### Experts

A project lists experts: a person (subject) or `role:<name>`, with up to 8
topics. Experts are found along the chain: the nearest project that has an
expert for the topic answers for it, then its ancestors, up to the root.

- **A person tags experts** with `@sme:<topic>`: each is notified.
- **An agent asks an expert** with `ask_person` and `asked_of: "sme:<topic>"`
  (or `"sme"`). The run parks on the question as today. `addressOf` resolves
  the hint to the experts of the run's project chain, intersected with the
  spec's `answerers` when it names them. An empty intersection falls back to
  `answerers`, as `audienceOf` does today. If there is no expert at all, the
  question goes to the project's owners. An expert's reply, from the inbox,
  the run, the thread or Slack, is `runtime.Answer` and resumes the run.
- Addressing routes; it does not admit. Who may answer stays fiber's check
  against `answerers` (`mayAnswer`), unchanged.
- **Except in a run that uses a person's own credential.** A run that acts
  for a person and is granted a tool that takes each person's own
  credential asks that person alone its replies and choices (`maySteer`), so
  a question it asks of the experts goes to that person, and an expert's
  answer is refused: it would reach that person's tools with their
  credential.
- The project layer of every turn lists the topics with experts ("Experts
  here: billing, security. Ask one with ask_person, asked_of sme:<topic>"), at
  most 16 topics.

### Context layers

An agent's turn is given, in this order:

| # | Layer | Source | Bound |
|---|---|---|---|
| 1 | Project instructions, root first, then each project down to the thread's, then the channel's purpose and the experts line | `Project.Instructions`, `Channel.Purpose` | 4,000 characters per project, 8,000 in all; the farthest ancestors are dropped first, with "(instructions of Acme not sent)" |
| 2 | Project memory, the thread's project first, then its ancestors | `pkg/memory` owners `project:<name>`, and `project:` for the root | own notes as today (at most 15,000 characters); ancestors share 6,000, nearest first, with "(n more notes from Field not sent)" |
| 3 | The agent's instructions | the pinned revision's `AgentSpec.SystemPrompt`, which fiber sends as the system message | fiber's |
| 4 | The agent's memory | `pkg/memory` owner `<agent>` | as today |
| 5 | The thread | the branch so far, one turn a line: human messages, and follow-ups through the API, by the name and handle of who wrote or asked them, agents' answers by agent name, then a line that ends it and names who posted the message | the workspace history limit, as today |
| 6 | The message | as posted | 8,000 characters |

Which wins on conflict: the later instruction layer. The agent's
instructions win over a project's, and a child project's over its
ancestors'. Memory is facts, not instructions, and nothing in the thread
changes instructions: a message directs the work within them. Anything that
must hold whatever the prompt says is a policy, enforced by the platform and
never written into the prompt: limits, availability, tool approvals, call
policies. The first line of layer 1 says this in words, so the order of
authority does not depend on position.

### Inbox, announcements and scheduled agents

- A project's inbox is every inbox message whose `Source.Project` is the
  project (optionally with its descendants): alerts, failures, questions,
  results, artifacts, memory proposals and announcements about it. One store,
  one set of rights per kind, as today.
- An **announcement** is a message of kind `announcement` in a project's
  inbox, posted by a person, or by an agent scheduled in the project. It fans
  out to the members of that project and of every project below it. Who gets
  it is decided when read: one message, never a copy per person.
- A **recap** is an agent scheduled inside a project: a project-owned schedule
  on fiber's cron, running an agent available in the project, with
  `deliver: project`. Its answer is posted as an announcement from that agent.

### Roles (phase 2)

`viewer < member < owner`, granted by entries on the project: `*` (everyone
who can see the parent; at the root, everyone holding `projects:read`), a
subject, or `role:<name>`. People in IdP groups are reached through the policy
language, with no claims kept on the Principal:

```
role field-eng {}
grant operator, field-eng to group "field-engineering"
```

and then `{who: "role:field-eng", role: member}` on the project. An owner of a
project owns everything below it. A role never grants a tenant permission: a
viewer listed as an owner still cannot write.

## Data model

### `pkg/projects` (new; the store pattern of `pkg/clients`)

```go
package projects

type Kind string

const (
	Company Kind = "company"
	Team    Kind = "team"
	Account Kind = "account"
)

// KindSpec is a workspace's own label over a built-in behavior (phase 2).
type KindSpec struct {
	Name  Kind   `json:"name"`            // ^[a-z][a-z0-9-]{0,31}$, not a built-in
	Title string `json:"title,omitempty"` // <= 40
	Like  Kind   `json:"like"`            // team or account
}

type Role string // "" < viewer < member < owner

const (
	None   Role = ""
	Viewer Role = "viewer"
	Member Role = "member"
	Owner  Role = "owner"
)

// Entry is one member. Who is "*", "role:<name>", or a subject (a subject
// that starts with "role:" is refused). It is not called Member, which is
// the role.
type Entry struct {
	Who  string `json:"who"`
	Role Role   `json:"role"`
}

// Expert is who answers for some topics here (phase 2).
type Expert struct {
	Who    string   `json:"who"`    // a subject or "role:<name>"
	Topics []string `json:"topics"` // 1..8, each ^[a-z][a-z0-9-]{0,31}$
}

// Agents is what a project turns on (phase 3).
type Agents struct {
	// Enable is platform agents available here and below. "*" alone is
	// every platform agent. Nil adds nothing.
	Enable []string `json:"enable"`
	// Allow, when set, is the only agents that may run here and below:
	// a policy that only tightens. Nil narrows nothing.
	Allow []string `json:"allow,omitempty"`
	// Default answers an untagged message: an agent or "auto". "" inherits.
	Default string `json:"default,omitempty"`
}

type Account struct {
	Customer   string `json:"customer,omitempty"`    // <= 120
	ExternalID string `json:"external_id,omitempty"` // <= 120
	URL        string `json:"url,omitempty"`         // https, <= 500
}

type Project struct {
	Name         string     `json:"name"`                   // identity: immutable, ^[A-Za-z0-9][A-Za-z0-9 _-]{0,62}$; "" is the root
	Title        string     `json:"title,omitempty"`        // <= 80; the root shows Settings.WorkspaceName instead
	Description  string     `json:"description,omitempty"`  // <= 500
	Kind         Kind       `json:"kind"`                   // default team; the root is company
	Parent       string     `json:"parent,omitempty"`       // "" is the root
	Account      *Account   `json:"account,omitempty"`      // account-like kinds only
	Instructions string     `json:"instructions,omitempty"` // <= 4000 (phase 1)
	Members      []Entry    `json:"members"`                // <= 100, never nil when stored (phase 2)
	Experts      []Expert   `json:"experts"`                // <= 50 (phase 2)
	Kinds        []KindSpec `json:"kinds,omitempty"`        // root only, <= 16 (phase 2)
	Archived     bool       `json:"archived,omitempty"`     // phase 2
	Agents       Agents     `json:"agents"`                 // phase 3
	Announce     Role       `json:"announce,omitempty"`     // lowest role that may post; default owner (phase 4)
	AgentPosts   string     `json:"agent_posts,omitempty"`  // publish | review (phase 5)

	Managed   string    `json:"managed,omitempty"` // "file" when a Platform document declares it
	Version   int       `json:"version"`           // from 1; every write names it
	CreatedBy string    `json:"created_by,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedBy string    `json:"updated_by,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Channel is one place in a project where threads live (phase 6).
type Channel struct {
	Project  string        `json:"project"`           // "" is the root
	Name     string        `json:"name"`              // ^[a-z0-9][a-z0-9-]{0,39}$, immutable; "general" always exists
	Title    string        `json:"title,omitempty"`   // <= 80
	Purpose  string        `json:"purpose,omitempty"` // <= 200, sent to agents
	Default  string        `json:"default,omitempty"` // an agent or "auto"; "" inherits from the project
	Slack    *SlackBinding `json:"slack,omitempty"`   // phase 8: {client, channel}
	Archived bool          `json:"archived,omitempty"`
	Managed  string        `json:"managed,omitempty"`
	Version  int           `json:"version"`
	// created/updated by and at, as on Project
}

// Change is one entry of the record of what changed.
type Change struct {
	Seq     int64           `json:"seq"`
	At      time.Time       `json:"at"`
	By      string          `json:"by"`
	Project string          `json:"project"`
	What    string          `json:"what"` // created | updated | moved | archived | deleted | members | experts | agents | channel | managed
	Before  json.RawMessage `json:"before,omitempty"`
	After   json.RawMessage `json:"after,omitempty"`
}
```

Store: `Get`, `List`, `Create`, `Update(tenant, name, version, fn, by, at)`,
`Move` (phase 2), `Delete`, `Tree(tenant)`,
`History(tenant, name, limit, before)`, `Upsert` (the spec). `Tree` is an
immutable snapshot replaced on every write. Admission, Converse, the inbox
filter and the console each read one consistent tree, with no lock held
while they do their own work. It answers `Chain(name)` (the project, then its
ancestors, then the root), `Subtree(name)`, `Depth`, `Children`,
`Home(agent)` and `Resolve(name, reader) Access`.

```go
type Reader struct{ Subject string; Roles []string; Govern bool; Machine bool }
type Access struct{ Role Role; From, Via string } // via: "*", "role:x", subject, "owner above", "projects:govern"
```

`Resolve` is a pure function over the chain, root first. At each level it takes
the best matching entry. An owner above makes every level below owner. No role
at some level means no role below it. `Govern` is owner everywhere. A machine
matches `*` and its own subject only.

**Storage.** One namespace, `projects`, so a record and its history entry are
one `state.Write` (all or none within a namespace):

| Key | Value |
|---|---|
| `p:<name>` (`p:` is the root) | the project, with the fields the file owns |
| `c:<project>/<channel>` | a channel (names cannot hold `/`) |
| `h:<seq>` | a `Change`, a ring of 1,000 per tenant pruned in the same write |
| `x:<lower(name)>` | a tombstone `{name, by, at, parent, members}`, at most 1,000; one is forgotten to make room only when no conversation or file is filed under its name, and a delete that would need to forget one that has something is refused (429) |

`Open(st)` loads everything once and trusts its memory (one writer). A write
that fails changes nothing. Open refuses a cycle or an orphan (fail closed).
`internal/platform/backends.go` opens it beside the other stores.
`HasProject`, `CreateProject` and `DeleteProject` become calls to it. That
ends today's lost update in `CreateProject`, which reads and writes under
separate locks: check and insert become one critical section.

**Invariants**, checked under the store's lock before the write:

1. A name is unique ignoring case, tombstones included, and never changes. A
   tombstoned name is reused only with `projects:govern`, recorded as
   "adopts its old conversations". However many deletes follow, a tombstone
   whose name still has conversations or files under it is never forgotten.
2. The parent exists in the same tenant and is not archived. Phase 1 fixes
   the parent at creation, so no cycle can form. A move (phase 2) refuses a
   parent inside the moved subtree, and requires
   `depth(parent) + 1 + height(moved) <= 4`. Both checks run against the
   snapshot taken under the write lock.
3. Kinds: a company project sits under the root or a company project; an
   account-like project never sits under an account-like one; `account` only
   on account-like kinds; a `KindSpec` in use cannot be removed.
4. Openness only tightens: `rank(child's * entry) <= rank(parent's)`, where
   none < viewer < member and a `*` owner counts as member. A move that would
   widen who sees something is refused. Until phase 7 every chain must hold a
   `*` entry of at least viewer: members-only answers 409 "members-only
   projects need read confinement, not built". From phase 7 a members-only
   project cannot sit under a project with a `*` owner entry, since ownership
   flows down.
5. Delete is refused (409, naming them) while children, schedules, alert
   rules or policies point at the project, so nothing is left pointing at a
   name that no longer exists. From phase 2, archiving is the alternative.
   Delete drops the project's memory and leaves a tombstone. Its
   conversations keep the name, and are adopted only if a `projects:govern`
   holder recreates the project. Until then they are read, not continued,
   by whom they were read by when it was deleted: the tombstone keeps its
   parent and members, read under the parent as it stands now (phase 7).
   A run filed in a name the tree does not hold is refused at admission
   (409), since nothing says which ancestors it would count toward, and a
   file is published only in a project the tree holds. Files keep the
   ancestors they were published under, so a parent's storage still counts
   what a deleted child published. Delete never drops a policy: only
   `billing:write` removes one.
6. A file-managed project cannot be deleted through the API, and a field the
   file sets answers 409 `ErrManaged`.

**Bounds.** Projects per tenant: 100 in phase 1 (today's cap), 500 from phase 2
with paging. Depth 4 below the root. Children per parent 100. Company projects
10. Members 100, experts 50, channels 20 per project, agent lists 64 (as
`clients.Client.Agents`).

Errors, mapped in `Server.problem`: `ErrNotFound` 404 (also for a project the
caller cannot see), `ErrExists` 409, `ErrInvalid` 400, `ErrLimit` 429,
`ErrManaged`, `ErrInUse` and `ErrVersion` 409, `ErrForbidden` 403.

### `pkg/threads` (new, phase 6)

```go
package threads

type Visibility string // "shared" | "private" (private from phase 7)

type Participant struct {
	Kind    string    `json:"kind"`            // person | agent
	Name    string    `json:"name"`            // subject or agent name
	Owner   bool      `json:"owner,omitempty"` // the person who started it
	AddedBy string    `json:"added_by,omitempty"`
	At      time.Time `json:"at"`
}

type Thread struct {
	ID           string        `json:"id"`      // today's conversation id (its first run) or "th-..."
	Project      string        `json:"project"` // "" is the root
	Channel      string        `json:"channel"`
	Title        string        `json:"title,omitempty"`
	Visibility   Visibility    `json:"visibility"`
	Participants []Participant `json:"participants"` // <= 50
	Default      string        `json:"default,omitempty"`
	Surface      *Surface      `json:"surface,omitempty"` // {client, channel, ts}: one Slack thread
	Version      int           `json:"version"`
	CreatedBy    string        `json:"created_by"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
}

// Message is a human turn. It starts no run of its own.
type Message struct {
	ID        string     `json:"id"`                   // "m-<seq>"
	Thread    string     `json:"thread"`
	Prev      string     `json:"prev,omitempty"`       // the turn it follows: a message, or a run of a conversation from before threads
	By        string     `json:"by"`                   // the person's subject; never an agent
	Name      string     `json:"name"`                 // display name when posted
	Text      string     `json:"text"`                 // <= 8000
	Mentions  []Mention  `json:"mentions"`             // <= 10
	Answers   *AnswerRef `json:"answers,omitempty"`    // {run, question}: recorded as that question's answer
	Via       string     `json:"via"`                  // console | <client id>
	SurfaceID string     `json:"surface_id,omitempty"` // a Slack ts; makes a post idempotent
	At        time.Time  `json:"at"`
}

type Mention struct {
	Kind    string `json:"kind"`              // agent | person | sme
	Name    string `json:"name"`              // agent, subject or topic
	Run     string `json:"run,omitempty"`     // the run an agent tag started
	Refused string `json:"refused,omitempty"` // why a tag started nothing
}
```

Namespace `threads`: `t:<id>` a thread, `m:<thread>:<seq>` a message,
`s:<client>:<channel>:<ts>` the surface index. At most 5,000 messages per
thread (then 409 "start a new thread") and 50 participants. A workspace holds
at most 10,000 threads and a project 2,000 (429), a conversation from before
threads counting once its record is written. A person's message starts no
run, so no limit on runs holds it: a person posts at most 30 messages a
minute and a project takes at most 120 (429), counted in at most 10,000
windows. One message notifies at most 20 people and roles.

The record keeps a person by subject, the key they sign in with. What a
thread hands out names them by handle instead (a participant, an author, a
tag, who was notified, the people in a thread's summary, a mention's `by`),
and calls them by their name in the directory, or their handle without one,
so a subject that holds an address is never shown. People are added to and
removed from a thread by handle.

How it joins the run index:

- A tagged agent's run has `Prev` = the message and `warp.message` meta = the
  message id. Runs that answer one message are shown together, not as
  branches. A branch is a message posted after an earlier turn, as today.
- `Path` walks messages and runs; the transcript (`threads.Ask`) renders a
  message as `Dana (@dana): text` and a run as `ignite (agent): answer`, each
  on one line. A run that answers no message, a follow-up through the API or
  a run of a conversation from before threads, renders as who asked it and
  its text, then its answer. The words the run answers follow a closing line
  that names who posted them.
- A conversation with no record reads as shared, in `general` of its project,
  with the people its runs acted for, and its agents, as participants. A record is written the first time
  anything about it changes. Nothing is migrated in bulk.

### `pkg/people` (new, phase 2)

The directory that tagging and member lists need: `Person{Subject, Name,
Handle, SeenAt}`, written at sign-in and when a client maps a Slack person. At
most 5,000 per tenant. `Handle` is `^[a-z0-9][a-z0-9._-]{0,31}$`, unique per
tenant across people and agent names. A new agent named like a handle is
refused (409 "a person goes by @dana"), and a colliding handle at first
sign-in gets a suffix. `auto` and `sme` are reserved. No email is asked for
or shown: a subject that holds an address (a Tailscale login) is kept as the
key it is, left out of the directory, and subjects are never searched.

### Changes to existing stores

| Store | Change | Phase |
|---|---|---|
| `platform.Settings` | `projects` leaves the wire. A storage-only type, `storedSettings{Settings; LegacyProjects []Project}`, is read once by the migration, because today one struct is both the wire and the record | 1 |
| `platform.Settings` | `Baseline` is removed; the root's `Agents.Default` replaces it | 3 |
| `runs.Run` | `Within []string` (ancestors nearest first, pinned at admission as `warp.within`), `Channel`, `Message` | 1, 6 |
| `runs.Index` | per-project counters `{Threads, Active, ActiveAt}`, updated in `Record` and on status changes, replacing the `Threads(tenant, 0)` scan in `listProjects`. `ThreadQuery.Projects []string` (one project or its subtree) and `Channel` | 1, 6 |
| `runs.Index.Summarize` | a project filter (a set of names, at most the subtree cap) | 5 |
| `policy.Store.For` | `For(tenant, chain []string, agent, client)`: agent, client, the project and each ancestor nearest first, then the tenant. `Decide` applies all, so the strictest wins | 1 |
| `policy.Policy` | `Authority` (admin or owner); existing policies are admin | 2 |
| `usage.Entry` | `Within []string`. `admission.scopes()` adds `p\x00<ancestor>` for each; `matches` checks the project or `Within`. Spent and settled entries take the run's pinned `Within` | 1 |
| storage limits | `storageAllows` sums what `Tree.Subtree` holds for a project policy, and what was published below the project in one deleted since: an artifact keeps the ancestors it was published under (`Within`, stored and not on the wire) | 1 |
| `pkg/memory` | owner `project:` for the root's notes: it holds the colon no agent's name can, so no agent shares them | 1 |
| `pkg/inbox` | kind `announcement`; `Message.Broadcast` (stored, `json:"-"`); per-reader hide and snooze for broadcasts; Audience entry `project:<name>`; `Reader.Projects`; `Query.Projects`; `Count` returns `updates`; `Withdraw` takes `by` (today it records `"system"`) | 4 |
| `pkg/inbox` | part `draft` (actions `publish`, `discard`) | 5 |
| `pkg/inbox` | kind `mention`, part `mention` (actions `reply`, `done`) | 6 |
| `pkg/prefs` | `Muted []string` (at most 100 project names) | 4 |
| `pkg/clients` | `Home{Project, Channel}` (default root, `general`) | 8 |

## Agents in projects

### Platform agents and project agents

- **A platform agent** belongs to the workspace (no home). It is available in
  a project when the project or an ancestor enables it. Every existing agent,
  and the built-ins `warp-writer`, `warp-analyst` and `warp-builder`, is a
  platform agent.
- **A project agent** lives in one project (its home): it is available there
  and in every descendant, and nowhere else. `*` never reaches it, and it is
  not available at the root, so a projectless conversation cannot reach it.

Both relations are kept on warp's records (the project's `Agents`, and an
agent-to-home index in `pkg/projects`). fiber's `AgentSpec` is unchanged.

```
offered(P)   = ∪ over Q in Chain(P) of (Q.Enable ∪ Homes(Q))
               "*" in Q.Enable = every agent with no home
allowed(P)   = ∩ over Q in Chain(P) with Q.Allow set, of Q.Allow   (none set: no narrowing)
available(P) = offered(P) ∩ allowed(P)
```

A zero `Agents{}` adds nothing: availability below comes from an ancestor's
explicit `enable`. `*` is written, never implied. The migration writes
`enable: ["*"]` on the root, which is today's behavior: every agent answers
everywhere. A project that narrows sets `allow`; a child cannot widen it.

### Enforcement: one check, at admission

In `Platform.Invoke`, after `placeOf` and under `p.admission`, a run filed in a
project (the root included) must use an agent in `available(project)`. If it
does not, it is refused with `ErrNotAvailable` (403):
`"globex-helper is not available in Initech"`. This covers every origin,
because every run passes through admission:

- API turns and thread messages;
- cron and run-now (a schedule whose agent stops being available keeps firing,
  is refused, and raises the existing failure message);
- called runs: `placeOf` gives them their root's project, so a call made in
  Globex reaches only agents available in Globex. Spawn lists, call policies,
  limits and leases still decide every call first; this only narrows;
- Slack and GitHub tags, once the platform files them (phase 8);
- evals: a case is filed where its agent lives, its home for a project agent
  and the root for a platform agent, so the home's limits count it and the
  runs it calls are checked there, as for any run of that agent.

Housekeeping (`OriginSystem`) is exempt, as it is from admission. Earlier
checks use the same function and give the same answer sooner: writing a
schedule, a default or a tag, and `GET /api/pick`.

### Homes

`PUT /api/agents/{name}/home {project}` needs `agents:write` and ownership of
the new home and of the old one. It answers 409, listing them, when a
schedule, alert rule, default or call-policy list outside the new home's
subtree still names the agent. Editing a homed agent also needs ownership of
its home. The spec says the same on warp's wrapper:
`agents: [{project: Globex, spec: {...}}]`.

### `@auto`, the built-in router

- `auto` is a reserved name. It is not in the registry and is never homed,
  enabled or listed in `allow`. It is available wherever an agent is.
- Tagging `@auto`, or a default of `auto`, runs `pkg/pick` over
  `available(project)`. With no match it hands the message to the nearest
  default on the chain that names an agent. With none, no run, and the answer
  says "no agent fits; tag one". The run records `warp.picked`
  ("Picked by @auto: matched billing").
- `GET /api/pick?q=&project=` previews it.
- Nothing routes automatically otherwise. `POST /api/runs` with `agent`
  omitted uses the default chain, and with none answers 409 "tag an agent".
  `agent: "auto"` means `@auto`.

### Clients

A client's `DefaultAgent` follows the same rule, as one more layer. It is the
default for the threads the client files outside a bound channel: DMs, the
assistant pane, an unbound channel, a GitHub issue. The order there is the
thread, then the client, then the client's home channel and its chain. In a
bound Slack channel the bound warp channel's chain decides. The client's
allow-list (`client.Routes`) still filters the result: a default the client
may not route to is refused, never swapped. A GitHub label in `auto_labels`
names an agent, so it is a tag. fiber's in-process connectors may set an
agent of `auto`; `surface.stamp` resolves it with `@auto` before admission,
among the agents the client may route to. The Slack app built on the API
cannot have its list applied by the platform (`POST /api/runs` takes no
client), so it sends `auto` only for a client whose list is `*`: a client that
lists its agents treats `@auto`, and a default of `auto`, as names outside the
list.
`stamp` also writes `warp.project` and `warp.channel` from the binding, and
`runs.ProjectOf` believes them for `slack` and `github` origins, because
ingress overwrites whatever a connector put there (phase 8).

### How context maps onto fiber and warp

- The agent's instructions are its pinned `AgentSpec.SystemPrompt`. fiber
  sends them as the system message for that revision. warp never rewrites
  them per run. `runtime.Request.Instructions` stays only the host's return
  contract (fiber: "callers must not supply arbitrary system text"), so
  project instructions are not put there.
- Layers 1, 2, 4, 5 and 6 are composed into the turn's text by one function,
  `p.contextOf(place, agent)`, and pinned in the ledger with the tag, so every
  attempt replays the same bytes. Converse uses it for thread turns. Invoke
  uses it for `cron` and `manual` runs filed in a project, so a scheduled
  agent gets its project's instructions and memory (today only API
  conversations get memory). Called runs keep their explicit input messages.
- Memory stays in `pkg/memory`: owner `<agent>`, `project:<name>`, and
  `project:` for the root. An agent's name cannot hold a colon, so no agent
  owns a project's notes, and the agent memory routes answer 404 for a
  project's owner: a project's notes are changed only through its own
  routes and the inbox, each asking what the project asks. The memory agent
  writes as today: to the run's own project, or to the agent for a run at
  the root. It never writes from a private thread (phase 7), nor from a
  conversation that acted for a person with a person-scoped tool, whose
  answers may carry what that person's own credential reached. With
  `memory_auto_approve` on, what it writes for the workspace or a company
  project still waits for someone holding `projects:govern`, since the turn
  that taught it needed only `runs:write`. What it writes for any other
  project is kept unasked only when the person the turn was for may change
  the project's notes themselves (phase 4: an owner): a member's turn
  teaches only proposals, which reach the project's owners, as a member's
  own note does. Filing the turn decides it, and the turn carries it.
- Knowledge sources inherited down the tree (phase 9) use warp's executor
  seam, `callExecutor.resolve`, which already rewrites the revision for model
  policies. The chain's sources are pinned at accept as `warp.sources` and
  added to the revision's `Sources` before fiber's knowledge hook reads them.
  No fiber change.
- fiber's own thread journal (`pkg/thread`) is per agent by design ("not a
  transcript of the room"), so warp keeps its own room transcript.

## Announcements and recaps

### Carrier: the inbox

- New kind `announcement`, info only (its part is `PartInfo`), never in
  "Needs you". `reads(announcement) = projects:read`. `writes` is also
  `projects:read`, because hiding your own copy changes nothing others see.
- `Message.Broadcast` is stored on the raised message. On a broadcast, Dismiss
  and Snooze record only the reader's own state (`tenant.hidden[id][subject]`).
  Replaying the `DidDismiss` and `DidSnooze` changes of broadcast messages
  rebuilds it. Every other message keeps today's shared state. Withdraw stays
  shared and records who withdrew.
- `Count` returns `{open, unread, updates}`. Broadcasts count only in
  `updates`; every existing kind counts exactly as today, so the badge and the
  phone's dot never grow from announcements.

### Fan-out, decided when read

An announcement in P has `Source.Project = P` and `Audience ["project:P"]`.
`Message.For` matches `project:P` when P is in `Reader.Projects`. The platform
computes that set from the tree on each read. It holds every project where the
reader is a member (an explicit entry, or `*` at member or above) and every
ancestor of those. So an announcement reaches the members of P and of every
project below it. Viewers who are not members can read it in the project's
inbox but are not sent it. Muted projects (`prefs.Muted`) are left out of a
person's inbox, never out of the project's. The root cannot be muted.

- The root's announcements reach everyone: company-wide.
- A company project's announcements reach everyone (its `*` entry must be
  member).
- An account's announcements reach its account team.

Revoking membership hides past announcements at the next read. SSE `inbox`
notices are filtered by `InboxSees` and `For(reader)`, so an id from a project
the reader cannot see never reaches their stream.

### Who posts

- **People**: `POST /api/projects/{name}/announcements` with `{subject, text,
  severity}`, by anyone whose role is at least `Project.Announce` (owner by
  default). `From{person}`, key `announce:<project>:<seq>`. The author or an
  owner withdraws it.
- **Agents, on a schedule** (phase 5): a schedule filed in P with
  `deliver: project` and an agent available in P. When its cron or run-now
  run completes, `ended()` raises an announcement `From{agent}`:
  - the first line is the subject (at most 150 characters) and the rest is
    the text (at most 4,000);
  - an answer of `NONE` posts nothing;
  - the run is linked.

  Under `agent_posts: review` it is raised instead as a draft (kind
  `announcement`, part `draft`, actions Publish and Discard), addressed to P's
  owners. Publishing raises the announcement from the agent and records who
  approved it. `agent_posts` defaults to `review` at the root and in company
  projects, which everyone reads, and to `publish` in team and account
  projects.
- `deliver: inbox` sends the answer to the schedule's creator (kind
  `result`), which closes the gap that a scheduled run's answer reaches nobody
  today.

### A recap is a scheduled agent

Nothing recap-specific is stored. "Add Weekly Recap" on a project creates a
schedule: a jittered Monday morning, `warp-analyst` (or any agent available
there), the text "What changed in {title} since the last recap?",
`deliver: project`. It is a `cron` run: admitted, counted under every limit on
the chain, never housekeeping. Its grounding:

- the project's instructions and memory, through the context layers;
- `project_changes(days <= 31)`, a built-in read tool granted to
  `warp-analyst` and to any agent an admin grants it. It reads only the run's
  own project subtree:
  - runs (a `Summarize` filtered by project);
  - threads started and finished;
  - artifacts, approved notes and announcements;
  - schedule and project record changes.

  It returns at most 50 rows a section and 32 KB in all. A run with no
  project gets an error;
- `runs_summary` gains a `project` argument.

Model text is untrusted. Announcements are plain text, Slack mention syntax
(`<!here>`, `<!channel>`, `<@U…>`, `<!subteam^…>`) is escaped before
rendering, and the run that wrote the text is one click away.

### Bounds

20 announcements per project per day and 200 per tenant per day (429). 20 open
drafts per project. A project inbox page is at most 200 (default 30) with a
cursor. Inbox retention for closed broadcasts is a follow-up (see Risks).

### Release notes (phase 9)

`workspace.release_notes: true` (default false) posts the running version's
`feat` entries from the embedded CHANGELOG.md into the root's inbox as
`From{system, warp}`, keyed `release:<version>`, once.

## Permissions

### New tenant permissions

`pkg/auth` gains, in `Permissions()`:

- `projects:read`: see projects, their inboxes, channels, instructions,
  memory, experts and people.
- `projects:write`: create projects, and do what your project role allows.
- `projects:govern`: break-glass. Owner of every project, the root included.
  Needed to create or change company projects and to adopt a tombstoned name.

`default.authz`:

```
role viewer   { allow read on * }                                                    # gains projects:read
role operator extends viewer { allow write on runs, schedules, alerts, evals, artifacts, projects }
role manager  extends operator { allow write on agents, knowledge }
role admin    { allow * on * }                                                       # gains projects:govern
```

Operators keep what they have today ("anyone who can start a run can make a
project"). The same change updates:

- the legacy table in `pkg/auth/policy_test.go`;
- the `Permission` enum in `api/openapi.yaml`;
- `everything` in `internal/httpapi/observe_test.go`;
- `apps/web/src/access.ts` (`PERMISSIONS`, `doing`, `NOTABLE`:
  "projects:govern owns every project") and its test;
- docs/security.md and docs/authorization.md.

### Route permission, then project role

Every route checks exactly one permission in `Server.Handler`. The platform
then checks `Resolve(chain, reader).Role` on the one project the route names,
the way inbox resolution checks each part. Effective =
`who.Can(route permission) && role >= needed`. A project the caller cannot see
answers 404, with the body of a missing one; a visible project with too low a
role answers 403 ("you need to own Globex"). Another tenant's project is 404
everywhere; the tenant comes only from `who.Tenant`. Until phase 2 there are
no roles: the route permission alone decides, and company kind and the root
need `projects:govern`.

| Action | Route permission | Role needed (from phase 2) |
|---|---|---|
| See a project, its inbox, channels, instructions, memory, experts | `projects:read` | viewer |
| Create a project under P (top level: P is the root) | `projects:write` | member of P. Company kind: `projects:govern` |
| Edit title, description, instructions, account | `projects:write` | owner |
| Move | `projects:write` | owner, and member of the new parent. Leaving an ancestor that holds a policy also needs `billing:write`, and so does moving a project that holds, or has below it, an owner's limit the caller could not change before (they do not own the parent it was set from): a move never escapes a limit. Leaving an ancestor whose allow list keeps agents out needs owning it, unless the new chain allows no more: a move never widens which agents may run. Nor does it leave anything below it without an agent it has now (409, naming each): a project's, a channel's or a thread's default, an agent that lives there, or a schedule filed there. An archived channel holds no move back |
| Archive, delete | `projects:write` | owner |
| Members, experts, channels, agents (`enable`, `allow`, `default`), `announce`, `agent_posts` | `projects:write` | owner |
| Make an agent live here | `agents:write` | owner of the new and old home |
| Edit a homed agent: its spec, call policy and notes, in its routes and the inbox | `agents:write` | owner of its home |
| Propose a project note (stored pending, owners approve in the inbox) | `projects:write` | member |
| Add an approved note, approve, remove, in the memory routes and the inbox | `projects:write` | owner; the workspace and company projects: `projects:govern` too, and an owner without it proposes |
| Set any project's limit | `billing:write` (unchanged) | none |
| Set a tighten-only limit on a descendant (`Authority: owner`; never touches an admin policy) | `projects:write` | owner of a strict ancestor |
| Post an announcement | `projects:write` | `announce` (default owner) |
| Withdraw an announcement | `projects:write` | its author or an owner |
| Publish or discard an agent's draft | `runs:read` (the inbox resolve route) | `needs(draft, publish)` is `projects:write`, then owner |
| Schedule in P, change, pause, delete or run one filed there | `schedules:write` | member; the agent available in P |
| Read a shared thread and its runs | `runs:read` | viewer |
| Read a private thread and its runs (phase 7) | `runs:read` | participant, or `projects:govern` |
| Start a thread, post a message | `runs:write` | member; in a private thread, participant. A viewer posts only an answer to what they were asked (a question a run put to them, or a reply to a message that tagged them) that tags no one, which starts nothing and notifies no one: an answer's tags start agents and notify people as any message's do, so tagging in one takes a member. An answer is posted only if it is the one the run was given (409 for a question already answered, or whose run stopped), so a viewer posts once for each question |
| Tag an agent or `@auto` | `runs:write` | member; the agent available in P |
| Tag a person or `@sme` | `runs:write` | member; the person must be at least a viewer of P. In a private thread, tagging a person invites them |
| Add to or remove from a shared thread | `runs:write` | member (removing anyone but oneself: the thread's owner or a project owner) |
| Invite to or remove from a private thread | `runs:write` | participant (remove: the thread's owner or a project owner) |
| Change a thread's visibility, title or default | `runs:write` | the thread's owner while a member, or a project owner |
| Answer an expert's question | `runs:write` (existing answer routes) | fiber's `answerers` check, unchanged |

Machines hold exactly their scopes and match only `*` and their own subject. A
surface acting for a person (`act_as`) is checked against that person's own
roles on the project, as answering questions is today, and against their own
tenant permission: starting a run for them, posting for them and answering
for them each need their `runs:write`, which the policy grants their subject. Agents are never
members: their relation to a project is availability.

### Default deny

A stored project never has nil members or a nil `enable`. An unknown `who`
form is 400. A reader with no subject matches nothing. An omitted `members` on
create becomes `[{who: "*", role: member}, {who: <creator>, role: owner}]`,
which is what every project meant before. This is a documented exception, kept
for continuity. For `account`, the `*` entry is viewer instead of member, and
there is none from phase 7.

## API

Every route is written literally in `projectRoutes`, `threadRoutes` or the
existing groups as `mux.HandleFunc("METHOD /path", s.authed(auth.X, h))`. In
`api/openapi.yaml` each has an operationId, a tag (`projects`, `threads`),
`x-permission`, declared path params, inline query enums (the oag 0.20.6
`$ref` bug) and the shared responses. Bodies keep `DisallowUnknownFields` and
the 1 MiB cap, and field caps are mirrored as `maxLength` and `maxItems`.
Lists use opaque `before`/`next` cursors ending in `next: null`; a bad cursor
is 400; an empty list is `[]`. Then run `make client`, `make client-check`
and `make spec-lint`, and update the hand-written `apps/web/src/api.ts`
mirror. `{name}` is `~` for the root (a name cannot start with `~`).

### Phase 1

| Route | Permission | Notes |
|---|---|---|
| `GET /api/projects` | `projects:read` | `?parent=&within=`. `{projects: ProjectView[]}` in tree order, at most 100. `ProjectView` = Project + `{depth, path: [{name, title, kind}] (<= 4), children, conversations, active, active_at?, schedules, notes, proposed}`. Today's `updated_at`, which meant activity, becomes `active_at` |
| `POST /api/projects` | `projects:write` | `ProjectInput{name, title?, description?, kind?, parent?, account?, instructions?}` → 201. 400 kind rule or depth; 409 exists; 429 cap |
| `GET /api/projects/{name}` | `projects:read` | `ProjectDetail{project: ProjectView, ancestors (<= 4), children (<= 100), limits: [{scope, from}]}` |
| `PUT /api/projects/{name}` | `projects:write` | `ProjectUpdate{version, title, description, account, instructions}`; `name`, `kind`, `parent` are 400. 409 stale version or managed field |
| `DELETE /api/projects/{name}` | `projects:write` | 204; 409 `ErrInUse` names what still points at it |
| `GET /api/projects/{name}/history` | `projects:read` | `?limit (<= 200, default 50)&before` → `{changes, next}` |
| memory routes | `projects:read` (GET) / `projects:write` | paths and shapes unchanged; regated from `runs:*` |
| `GET /api/threads` | `runs:read` | gains `project=&within=` |
| `GET /api/settings`, `PUT /api/settings` | unchanged | `projects` removed; a PUT that names it is 400 |
| `PUT /api/policies` | `billing:write` | `exists()` reads the store; applied along the chain |

### Phase 2

| Route | Permission | Notes |
|---|---|---|
| `GET /api/projects` | `projects:read` | paged: `limit` (<= 200), `before`, `q` (name, title, description, customer), `kind`; the cap rises to 500. Each view adds `access {role, from, via}` and `actions: string[]` |
| `GET`, `PUT /api/projects/{name}/members` | `projects:read` / `projects:write` | `{version, members (<= 100)}`; GET also lists inherited owners with `from` |
| `GET`, `PUT /api/projects/{name}/experts` | `projects:read` / `projects:write` | `{version, experts (<= 50)}` |
| `POST /api/projects/{name}/move` | `projects:write` | `{version, parent}` → ProjectView. 400 cycle, depth, kind, openness; 403 the limit rule; 409 a default, a homed agent or a schedule below it that would lose its agent |
| `PUT /api/projects/{name}` | `projects:write` | gains `kind`, `archived`; on the root, `kinds` |
| `PUT`, `DELETE /api/projects/{name}/limits` | `projects:write` | owner of a strict ancestor; writes a project-scope policy with `Authority: owner`; looser than the effective chain is 400 naming the binding scope; an admin policy is 409 |
| `GET /api/people` | `projects:read` | `?q=&limit=` (<= 50) → `{people: [{subject, name, handle}]}` |

### Phase 3

| Route | Permission | Notes |
|---|---|---|
| `GET`, `PUT /api/projects/{name}/agents` | `projects:read` / `projects:write` | `{version, enable (<= 64), allow?, default?}`; GET adds `available {all, names (<= 64)}`, `homes`, `default_resolved {agent, from}` |
| `PUT /api/agents/{name}/home` | `agents:write` | `{project}` ("" is none); 409 in use |
| `GET /api/agents/{name}` | `agents:read` | gains `home` |
| `GET /api/pick` | `agents:read` | gains `project` |
| `POST /api/runs` | `runs:write` | `agent` omitted uses the default chain; none is 409; `auto` is `@auto`; not available is 403 |
| `PUT /api/settings` | `agents:write` | `baseline` removed (400) |

### Phase 4

| Route | Permission | Notes |
|---|---|---|
| `GET /api/projects/{name}/inbox` | `projects:read` | `?kind` (inline enum, repeatable) `&children&state&limit&before` (state open or all) → `{messages, next}`. A `project:` audience entry matches any reader of the project here |
| `POST /api/projects/{name}/announcements` | `projects:write` | `{subject 1..150, text 1..4000, severity}` (info or warning) → 201 InboxMessage; 429 rate |
| `DELETE /api/projects/{name}/announcements/{id}` | `projects:write` | 204; 404 for an id that is not an announcement in this project |
| `GET /api/inbox/count` | `runs:read` | `{open, unread, updates}` |
| `GET /api/inbox` | `runs:read` | gains `project=`; `InboxKind` gains `announcement` |
| `PUT /api/preferences` | none | gains `muted` |

### Phase 5

The `Schedule` wire type gains `deliver: ""|inbox|project` (`project` needs a
project and an available agent, else 400). `Project` gains `agent_posts`. The
inbox part enum gains `draft`; resolve accepts `publish` and `discard` on it.
Tools: `project_changes`, `runs_summary(project)`.

### Phase 6

| Route | Permission | Notes |
|---|---|---|
| `GET /api/projects/{name}/channels` | `projects:read` | at most 20 |
| `POST /api/projects/{name}/channels` | `projects:write` | `{name, title?, purpose?, default?}`; owner |
| `PUT`, `DELETE /api/projects/{name}/channels/{channel}` | `projects:write` | DELETE archives; `general` is 409 |
| `GET /api/threads` | `runs:read` | gains `channel=`; only threads the caller may read |
| `POST /api/threads` | `runs:write` | `{project, channel, visibility (required), title?, default?, text, post_only?}` → 201 `{thread, message, runs, refused}`. `private` is 409 until phase 7 |
| `GET /api/threads/{id}` | `runs:read` | `{thread, turns, next}`: messages and runs in order, 200 a page with a cursor (today unbounded) |
| `POST /api/threads/{id}/messages` | `runs:write` | `{text, after?, post_only?, answers? {run, question}, act_as?}` → 201 `{message, runs, notified, refused}` |
| `PUT /api/threads/{id}` | `runs:write` | `{version, title?, visibility?, default?}` |
| `PUT /api/threads/{id}/participants` | `runs:write` | `{version, add?, remove?}` (<= 50 in all) |
| `GET /api/events` | `runs:read` | new `event: thread` notice `{id, message, at}`, filtered by who may read the thread |

The inbox gains kind `mention`: To is the tagged person and the key is
`mention:<message>:<subject>`. Its `mention` part is `{thread, message,
project, channel, by (the poster's handle), excerpt (<= 300)}`, with actions `reply` (posts a human
message as the reader; `runs:write`) and `done`.

### Phase 7

No new routes. `visibility: private` and members-only projects are accepted.
One predicate, `p.Sees(who, place)`, filters every read of:

- runs, threads, run events, manifests and traces;
- the journal and analytics;
- artifacts and their downloads;
- schedules and the inbox;
- memory and pending memory;
- `/api/agents` (homed agents).

A hidden object answers 404.

### Phase 8

| Route | Permission | Notes |
|---|---|---|
| `PUT /api/projects/{name}/channels/{channel}` | `projects:write` | gains `slack {client, channel}`; owner; the client must be a Slack client in the tenant; binding a Slack channel, or moving a binding, also takes `clients:write` (403), as every new thread there is filed in the project; keeping or removing one takes only the owner |
| `GET /api/clients/{id}/bindings` | `clients:read` | `[{slack_channel, project, channel}]`, at most 200 |
| `PUT /api/clients/{id}` | `clients:write` | gains `home {project, channel}` |
| `POST /api/threads`, `POST /api/threads/{id}/messages` | `runs:write` | gain `surface {client, channel, ts}`: find or create the thread by surface. The platform files it from the binding, never from a request field, and only for the client's `slack.machine` |
| `PUT /api/inbox/{id}/deliveries/{client}` | `runs:write` | `{channel, ts}` → 204; only the client's machine |
| `GET /api/inbox`, `GET /api/inbox/count` | `runs:read` | gain `act_as` for a client's machine, resolved through `Client.ActsFor` |

### Contract upkeep, every phase

- New schemas go in the `shapes` map of
  `TestSpecSchemasHaveTheFieldsTheGoTypesMarshal` (inputs `written: true`).
- Each operation gets a validated 2xx and its documented errors in
  `TestResponsesMatchTheSpec`.
- Each `{name}`, `{channel}` and `{id}` path joins the `real` and `valid` maps
  for the globex 404 sweep and the empty-workspace `[]` sweep.
- `TestSpecListsExactlyTheRegisteredRoutes` and
  `TestSpecPermissionsAreTheOnesTheServerEnforces` cover 401 and 403 "you
  need projects:write".

## Spec

The fields live in `pkg/spec/platform.go`. Every field is optional, has the
stated default, and is documented in docs/spec.md. Each phase documents only
the fields it ships, so `TestTheReferenceExamplesLoad` always loads the
examples.

```yaml
spec:
  workspace:
    instructions: Acme sells warehouse robots. Answer plainly.  # the root's (phase 1)
    agents: {enable: [warp-writer, warp-analyst], default: auto}  # the root's (phase 3); replaces baseline
    members: [{who: "*", role: member}, {who: "role:comms", role: owner}]  # phase 2
    experts: [{who: "role:security", topics: [security]}]                # phase 2
    project_kinds: [{name: partner, title: Partner, like: account}]      # phase 2
    agent_posts: review                     # phase 5
    release_notes: false                    # phase 9
    channels: [{name: announcements, purpose: Company news}]             # phase 6
    projects:
      - name: Field
        title: Field engineering           # phase 1
        kind: team                         # phase 1: company | team | account | a project_kinds name
        instructions: Customer accounts. Never promise dates.            # phase 1
        members: [{who: "*", role: viewer}, {who: "role:field-eng", role: member}, {who: "role:field-leads", role: owner}]
        agents: {enable: [field-helper], default: field-helper}  # phase 3: adds to what the root enables
        schedules:                         # phase 5: file-managed, filed here
          - {name: field-recap, cron: "41 8 * * 1", agent: warp-analyst, text: What changed in Field this week?, deliver: project}
      - name: Globex
        parent: Field                      # phase 1: a project in this file, or omitted for the root
        kind: account
        account: {customer: Globex Inc, external_id: "sf:0015g00000ABC"}
        members: [{who: "*", role: viewer}, {who: "okta|00u1ana", role: owner}]  # "*" required until phase 7
        experts: [{who: "okta|00u1ana", topics: [billing, renewals]}]
        agents: {allow: [globex-helper, warp-writer], default: globex-helper}  # narrows: field-helper stops here
        channels:                          # phase 6
          - {name: renewals, purpose: Renewal work, default: globex-helper}
          - {name: general, slack: {client: file-slack, channel: C0123GLOBEX}}  # phase 8
  agents:
    - spec: {name: field-helper, provider: policy, model: default, system_prompt: You help field engineers.}
    - project: Globex                      # phase 3: lives in Globex, available there and below
      spec: {name: globex-helper, provider: policy, model: default, system_prompt: You know the Globex account.}
```

`Workspace.Baseline` is removed in phase 3; a file that still sets it fails
`warp config check` with "baseline is now workspace.agents.default".

**`Platform.check`** runs these, so `warp config check` catches them, each
error with its path, such as `workspace.projects[2].parent`:

1. Names match the project regex and are unique ignoring case; channel slugs
   match theirs.
2. A `parent` names a project in the same file. A parent that exists only in
   the console cannot be checked before start, so it is refused.
3. No cycle; depth at most 4; at most 10 company projects; company only under
   the root or company; account-like never under account-like; `account` only
   on account-like kinds.
4. Kinds are built in or in `project_kinds`; a `KindSpec` has `like` team or
   account.
5. `who` forms, roles, topics and caps; until phase 7 every chain has a `*`
   entry.
6. `enable`, `allow` and `default` name agents in the file, built-ins, `*`
   (enable only) or `auto` (default only). An agent has at most one home, in
   the file. A default is available where it is set.
7. A schedule's cron parses, its agent is available in its project, and
   `deliver: project` sits on a project.

**Applying** (`internal/app/config.go` `applyWorkspace` becomes
`applyProjects`) is a merge, never a replace. This fixes today's
`s.Projects = nil`, which contradicts "Nothing is deleted because it left the
file":

- Projects are upserted parents first with `managed: file`. The fields the
  file sets win and answer 409 in the API; fields it leaves out stay the
  console's. An unchanged entry makes no new version.
- A console project with the same name is taken over, recorded as
  `managed`.
- A project that leaves the file stays, unmanaged.
- File schedules and channels are managed the same way.
- Order: root, projects, agents, homes, channels, schedules.

docs/spec.md gains:

- a "Projects" bullet in "What the file owns";
- a row in "Which level a knob lives at": "Project tree, kinds, members,
  experts, availability, channels | Platform | the workspace's own";
- "Baseline" leaves the "Baseline, history, projects" row.

## Console and Slack

### Console addresses (`apps/web/src/route.ts`, each with a round trip in `route.test.ts`)

| Route | Path |
|---|---|
| `{page: "projects"; q?; kind?}` | `/projects`, `?q=`, `?kind=account` |
| `{page: "project"; name; tab; message?}` | `/projects/{name}` (default tab), `/projects/{name}/{tab}`, `?message=<id>` |
| `{page: "channel"; project; channel}` | `/projects/{name}/channels/{channel}` |
| `{page: "task"; id; message?}` | `/tasks/{id}` (unchanged), `?message=<id>` |
| `{page: "new"; project?; channel?; agent?; ask?}` | `/?project=&channel=&agent=&ask=` |

`PROJECT_TABS` grow by phase:

- phase 1: `overview`, `context`;
- phase 2: `people`, `history`, `limits`;
- phase 3: `agents`;
- phase 4: `inbox`, which becomes the default, so `/projects/{name}` opens
  the project's inbox;
- phase 5: `schedules`;
- phase 6: `channels`.

An unknown tab falls back to the default through `pick`. Names never hold
`/`, so nesting lives in `parent` and breadcrumbs, never in path segments.
`~` is the root.

### Screens

- **Projects.** One `Page` titled Projects. Groups in words: Company-wide,
  Teams, Accounts, then custom kinds by title. At 390px the tree indents at
  most one level of 16px, and deeper rows print their path ("in Field ›
  Globex"). Rows show the title, the kind in words, the customer,
  conversations, active, AgentMarks of the project's agents (phase 3) and a
  still dot for unread updates (phase 4). "New Project" opens an inline form
  (title, name marked "Can't change later", kind, parent, description),
  shown only with `can("projects:write")`.
- **Project.** A `Page` with back to the parent's title or All projects, the
  title, meta in words ("Account · in Field · Lead globex-helper") and a
  breadcrumb of plain links. Tabs through `Page.tabs`, scrolling sideways on
  a phone:
  - Overview (phase 1): today's layout (tasks paged on the server with
    `threads?project=&within=`, shared memory, schedules, limits) plus
    Sub-projects with "New Sub-project".
  - Context: the project's instructions (editable with `projects:write`, by
    owners from phase 2), inherited instructions and notes, read only, under
    "From Field", and from phase 3 a preview of what a turn here is sent,
    layer by layer.
  - People: members with display names and roles, inherited owners "from
    Field", "Add Person" (a person or `role:`), experts by topic.
  - Agents: Available here, each with its AgentMark and where it comes from
    ("from Acme", "lives here"); Default; Allow. "Lives Here" is editable
    only with `agents:write` and warns how many projects lose the agent.
  - Inbox: the project's inbox in the mailbox look:
    - announcements carry the sender's AgentMark or avatar, and the agent's
      `Stroke` edge;
    - drafts sit at the top for owners, with Publish and Discard;
    - "Post Announcement" for those allowed, and Include Sub-projects.
  - Schedules: today's form plus `deliver`, and "Add Weekly Recap".
  - Channels: the channel list; each opens the channel view.
  - Limits: today's ScopeLimits with the chain ("Field's limit also applies
    here").
  - History: the paged change log.
- **Channel.** A `Page` titled `#renewals`, with the project in the meta.
  Threads are listed newest first and paged on the server, each with its
  title, AgentMarks of its agents and avatars of its people. A composer at the
  bottom starts a shared thread. Under it, in text: "globex-helper answers
  untagged messages. Tag anyone with @."
- **Thread.** Today's conversation view, with:
  - human messages with the person's avatar and name, agent answers with
    their AgentMark, and replies to one message shown together;
  - participants in the meta, and "Shared in Globex › #renewals" or
    "Private" in words;
  - an `@` typeahead over available agents (AgentMark), `@auto`, people
    (avatar) and `@sme:<topic>`;
  - under the composer, in text, who will answer ("@ignite answers" or
    "Nobody answers; tag an agent"), and a "Post Only" control;
  - a question addressed to an expert shown still, with "Reply".

  Nothing waiting on a person animates; a mark shows thinking only while its
  run is live.
- **Home.** The prompt-first composer, then two quiet lines from the shared
  `HomeModel`: "Needs you · 2" and "What's new" (up to 5 announcements from
  your projects, unread first), each linking to
  `/projects/{name}/inbox?message=`.
- **Inbox.** Announcements sit under Updates with a megaphone template;
  drafts and mentions sit in Needs you. On a broadcast the header reads "Hide
  for Me", with "Hidden for you. Others still see it." The Inbox adopts
  `Page` for its title. The badge counts `open` only.
- **Composer ProjectPicker.** Reads `GET /api/projects`, shows paths and kind
  words, and searches name, title, description and customer. With a project
  chosen, a channel select appears. The agent menu lists only available
  agents, with `@auto` first when the default is auto. The Auto switch
  becomes the `@auto` tag. "Create Project" in Title Case.
- **Settings › Workspace.** The projects editor goes, replaced by links to
  Projects and to the root (`/projects/~`). The baseline control on the
  agent page becomes "Workspace Default" on the root's Agents tab.
- **Agent page, Access tab.** "Lives in" and "Available in" (the projects
  that enable it), using AgentDetail's unused `projects` prop.

Copy goes in `packages/components/src/copy.ts`:

- kind names;
- buttons "New Project", "New Sub-project", "Move Project", "Post
  Announcement", "Add Weekly Recap", "Hide for Me", "Publish", "Discard
  Draft", "Post Only", "Add Person", "Name Owners", "New Channel", "Lives
  Here", "Workspace Default";
- sentence-case empty states ("Nothing has been announced here yet.").

Shared pieces: `packages/core` gains `HomeModel.updates`, and
`packages/adapter` maps `announcement`, `draft` and `mention` (`kinds`,
`templateOf`), with parity tests in `apps/slack/src/parity.test.tsx`.

### Slack (one app per client)

- **Channels.** A project owner who also administers the client
  (`clients:write`) binds an existing Slack channel to a warp channel. The
  bot must already be in it, so no scope widens. Which of a client's Slack
  channels a project answers in is the client's admins' to decide: a
  binding files every new thread there in the project. A Slack thread
  in a bound channel is a shared warp thread in that channel. A DM or the
  assistant pane is a thread in the client's home channel, private from
  phase 7.
- **Addressing.** In a channel, Slack delivers a message to the app only when
  it mentions the app, so:
  - `@Warp @ignite draft the renewal note` tags ignite: mention the app, then
    the agent's handle as plain text;
  - `@Warp what changed?` goes to the thread's default chain; with none the
    app answers ephemerally "Tag an agent, for example @ignite";
  - `@Warp @auto ...` routes with `@auto`;
  - `@Warp <@U_dana> can you check?` tags Dana (mapped through
    `slack.people`): no run;
  - in a DM or the assistant pane the app mention is not needed;
  - a native mention of a bot user in `client.Bots` is a tag of the agent it
    stands for.

  The app sends the raw text; the platform parses it with the one grammar.
  `apps/slack/src/agents.ts` stops picking: its `picker` path goes.
- **The room.** When the app is mentioned in a thread, the people's messages
  since it was last asked there are posted as human messages, idempotent on
  their `ts`, instead of being folded into the run's text.
- **Mentions and experts.** A mention or an expert question for a mapped
  person who is not already in the Slack thread goes to their DM with the app
  (`chat.postMessage` to the user id, covered by `chat:write`), with "Open
  Thread" and "Reply". "Reply" opens a reply modal in `@warp/render-slack`
  (the listed follow-up). The modal posts, as that person, a human message or
  the question's answer.
- **Announcements.** The app's `watchInbox` sees an announcement of a project
  whose `general` channel is bound. It posts it top-level once and records
  `PUT /api/inbox/{id}/deliveries/{client}`, so a restart, an edit or a
  withdrawal updates that post instead of posting again. A reply in its
  thread starts a shared thread seeded with the announcement. Slack delivers
  a reply in a channel thread only when it mentions the app, so the reply
  that mentions it posts, with the post's surface, into the thread the
  platform finds by that post's receipt: in the bound channel, its turns
  start with the announcement from whoever posted it (the scheduled agent
  for a recap), then the reply as the person's own message, answered by the
  channel's chain. A later reply finds the same thread, and once the
  announcement is withdrawn the thread says only that. A reply under one
  withdrawn before anyone replied starts nothing, and drafts never reach a
  channel. Only a thread the binding files in the announcement's project is
  seeded: after the Slack channel is bound to another project, or unbound, a
  reply under an old post starts a thread there as any other.
- **App Home.** The shared Home, through `homeView`, replaces the bare
  MailboxView. It shows Needs you (at most 5), What's new (at most 5) and Your
  projects (at most 8), read as the mapped person through `act_as`, within the
  100-block budget.
- **GitHub.** The same rule: `@agent` in a comment or an `auto_labels` label is
  a tag; otherwise the client's default; otherwise nothing. Threads are filed
  in the client's home.

Every Slack change passes `pnpm validate:slack`, and
`TestTheSlackManifestIsWhatWarpGenerates` stays green with no new scope.

## Migration

### Phase 1: projects out of settings

1. At platform start, the migration runs for each tenant whose `projects`
   namespace is empty and whose stored settings hold the legacy array
   (`storedSettings.LegacyProjects`):
   - One write imports every entry as `{kind: team, parent: "", title: "",
     created_by: "migration", created_at: SetupAt or now}` with one `h:`
     entry each, and creates the root.
   - The same write imports, the same way, every name a stored schedule
     (`warp.project`), alert rule or project-scope policy still points at
     that the array lacks: a project an earlier version dropped by a delete
     or at start while things stayed filed in it. Each one's `h:` entry says
     "kept because a schedule, an alert rule or a limit still names it". So
     a schedule that ran before the upgrade is not refused after it, and
     nothing is left pointing at a name the tree does not hold (invariant
     5). A record whose array was null imports only such names. A name the
     tree cannot hold (not a project's name, or one name ignoring case with
     an imported one) is logged and left, and what points at it is refused
     until it is changed. Past the 100-project cap the start fails, naming
     what it would have kept.
   - Then settings are rewritten without the array.
   - If the platform stops between the two writes, the next start repairs it:
     the store has records, so the import is skipped and only the array is
     cleared.
   - A failed import fails the start and changes nothing.
2. Nothing keyed by a name moves: `Run.Project`, thread summaries, usage
   entries and counters, policy scopes, `project:<name>` memory, schedule
   `warp.project` meta, alert rules, artifacts and inbox `Source.Project`.
3. Usage entries written before phase 1 have no `Within`, so they count toward
   their own project only, which is correct: no tree existed. `usage.Rebuild`
   replays them unchanged. The ledger keeps 62 days.
4. The spec's `workspace.projects` is applied after the import, as a merge.
   From now on, projects made in the console survive a start. Projects a
   previous version dropped at start are gone as records. Their runs and
   memory remain under the name and reappear if the project is recreated.
5. Contract changes, in the same PR as the console and `make client`:
   - `Settings.projects` leaves GET and PUT;
   - `updated_at` on a project view becomes `active_at`;
   - project and project-memory routes move from `runs:*` to `projects:*`.

   Committed as `feat(projects)!` with a `BREAKING CHANGE` footer.
6. Machine tokens with explicit scopes (the Slack app, CI) need
   `projects:read` to list projects. `warp config check` warns when a
   configured machine has `runs:read` but not `projects:read`. People are
   unaffected under `default.authz`. A custom policy file adds `projects` to
   its write rules; the release notes give the one-line diff.

### Phase 2: members

Every migrated project gets `members: [{who: "*", role: owner}]`. Under the
intersection rule only `projects:write` holders can act on an owner role. So
this keeps exactly today's "any operator may change any project" until an
owner narrows it. Ownership flows down, so a project created under a migrated
one is also owned by every `projects:write` holder. The People tab of a
`*`-owned project says so and offers "Name Owners".

The root gets `[{who: "*", role: member}]` and no explicit owner:
`projects:govern` holders own it. Account projects made in phase 1 keep their
entries. From phase 7 the console nudges their owners ("Globex is visible to
everyone. Make it members only?"). Nothing is closed automatically.

### Phase 3: agents and the baseline

- The root gets `agents.enable: ["*"]`, so every agent stays available
  everywhere. No agent has a home until one is given.
- The root's default comes from the baseline:
  - a workspace whose baseline was set gets that agent as its default, which
    is what the console sent when nobody chose an agent;
  - a workspace with no baseline gets `auto`, which keeps the picker for API
    and Slack callers that name no agent.

  Settings shows "Workspace Default: @auto, set by the upgrade". An admin can
  clear it.
- Setting the workspace default moves from `agents:write` (managers) to
  `projects:govern` (admins by default), since it changes the workspace
  itself. Owning the root is not enough: to keep the old split, an admin
  grants managers `govern on projects`, or a role that holds it (`steward` in
  docs/authorization.md).

### Phases 4 and 5: inbox

New kinds, parts, `Broadcast` and per-reader state are additive in the
inbox's replayed change log. Old records read as non-broadcast, so replay is
unchanged. `Withdraw` records `by` from now on; old withdrawals read as
`system`.

### Phase 6: threads

No bulk migration. A conversation with no thread record reads as shared, in
`general` of its project (or of the root), with its runs' actors as
participants. It gains a record the first time it changes. `POST /api/runs`
keeps working and files a new conversation as a shared thread in `general`.

### Phase 7: confinement

Existing threads stay shared and existing projects keep their `*` entries.
Private threads and members-only projects exist only when someone chooses
them. Account projects created from then on default to members only, and the
New Project form says so.

### Phase 8: Slack

Existing Slack conversations keep their run-derived mapping. A thread gains a
surface record the first time the app is mentioned in it after the upgrade.
Channels are bound only by a project owner.

### Docs in the same PRs

- docs/architecture.md#status, existing rows:
  - conversations (404), `pkg/pick` (407), Projects (408), composer selection
    (409), the rail flyout claim (410), labels (411);
  - `pkg/policy` (414), owner limits (421), membership (424);
  - the Slack app (381), Slack questions and the reply modal (382);
  - schedule delivery (465), the inbox and addressing (470 to 475).
- New status rows: project tree, project policies along the tree, layered
  context, agent availability and homes, `@auto`, project inbox and
  announcements, scheduled agents in projects, channels, shared threads and
  tagging, experts, read confinement, Slack project channels.
- docs/spec.md, docs/security.md and docs/slack.md.
- docs/authorization.md, with a "Project roles" section covering the
  intersection rule, `Resolve` and the matrix.
- The README feature list, and docs/roadmap.md (delivery steps 1, 2 and 4
  point here).

## Phases

### Phase 1: the project tree and project policies (one PR, M-L)

This is the smallest slice in which nesting is safe: a child can never escape
its parent's limit, and nothing claims privacy.

- **Backend.**
  - `pkg/projects` with the root and the fields `Name`, `Title`,
    `Description`, `Kind` (company, team, account), `Parent` (fixed at
    creation), `Account`, `Instructions`, `Managed`, `Version` and audit
    fields.
  - History and tombstones in the same namespace, and the `Tree` snapshot.
  - Caps (100 per tenant, depth 4, 100 children, 10 company) and invariants
    1, 2, 3, 5 and 6.
  - Opened in `backends.go`, with the migration out of settings.
- **Policies along the tree.** `policy.Store.For(tenant, chain, agent,
  client)`; `Run.Within` pinned at admission; `usage.Entry.Within` and
  ancestor scopes; storage summed over the subtree.
- **Context.** `p.contextOf` sends, in order: instructions root first, the
  project's notes, ancestors' notes within 6,000 characters nearest first,
  the agent's notes, the transcript. Converse uses it, and so do `cron` and
  `manual` runs filed in a project. The root's notes live under owner
  `project:`.
- **Permissions.** `projects:read`, `projects:write` and `projects:govern`,
  through `default.authz`, `policy_test.go`, the openapi enum, `everything`,
  `access.ts` and the docs. No roles yet: company kind and the root need
  `projects:govern`.
- **API.** The phase 1 routes, memory routes regated,
  `threads?project=&within=`, and per-project counters in `runs.Index`.
- **Spec.** `title`, `kind`, `parent`, `account` and `instructions` on
  projects, and `workspace.instructions`; `Platform.check` rules 1 to 4; the
  merge apply.
- **Console.** Projects tree grouped by kind; project page with breadcrumbs,
  Sub-projects, the Context tab (instructions, inherited notes) and the chain
  under Limits; ProjectPicker on `/api/projects` with paths; Settings'
  projects editor removed; routes and tests.
- **Tests.**
  - `pkg/projects`:
    - 40 racers creating one name keep one; the cap holds under racing
      creates;
    - depth 5 and the 101st child are 400; kind rules; name identity
      ignoring case; a tombstoned name needs govern;
    - delete refused while children, schedules, alert rules or policies point
      at the project; managed fields; tenant isolation;
    - `TestRestart`, a failed write through `statetest.NewFaulty`, and Open
      refusing a cycle.
  - `internal/platform`:
    - the migration, including a stop between its two writes, and a legacy
      schedule filed in a dropped project still running after it;
    - 40 racers in two children against a parent `max_runs` of 7 admit 7,
      and spend stops at the parent's limit to the micro-dollar;
    - storage over the subtree;
    - context layers in order and within budget, and a cron run in a project
      gets its context;
    - delete keeps the policy.
  - `pkg/usage` and `pkg/policy`: `Within` counts from day totals and from
    inside a day; `Rebuild` agrees; `For` returns the chain nearest first.
  - `internal/httpapi`: route and permission tests, shapes, 2xx and errors
    for each operation, the globex 404 sweep, `[]` on an empty workspace.
  - `pkg/spec` and `internal/app`: the reference examples load; each check
    with its path; a console project survives a start; a project that left
    the file stays unmanaged; an unchanged file makes no version.
  - Web: route round trips; `access.test.ts`; the tree at depth 4 indents one
    level at 390px; the picker shows paths.
  - The commands `make check`, `pnpm typecheck && pnpm test`,
    `make client-check` and `make spec-lint` all pass.
- **Status.** Row 408 becomes "a tree with kinds, in its own store", and row
  414 adds the chain. A new row says plainly that every project is visible to
  everyone and has no owners yet.

### Later phases

The owner set this order: explicit addressing and shared threads come right
after the tree, and everyone can see who and what is in the workspace.
Sections above still name phases by their first numbering: their phase 3 is
phase 2 here, 6 is 3, 2 is 4, 4 is 5 and 5 is 6. `pkg/people` moves to
phase 2 and experts to phase 3, because addressing and threads need them.

| # | Phase | Scope | Size |
|---|---|---|---|
| 2 | Explicit addressing, agents in projects, and the directory | `Agents{enable, allow, default}`, homes and the home route, `available(P)`, the one admission check. `@auto` as a reserved built-in over `pkg/pick`, defaults along the chain, the baseline migration. `POST /api/runs` without an agent, `GET /api/pick?project=`, `stamp` resolving `auto`, the Slack app's picker path removed. `pkg/people` and `GET /api/people` (who is in the workspace: name, handle, picture, roles, projects), from sign-ins, client people maps and the policy; a Directory in the console with People, Agents (platform and project agents, where each lives and is enabled, its handle and color) and Projects (the tree, kinds, what each holds), readable by everyone with `people:read`, `agents:read` and `projects:read`. Agents tab, composer menu and `@auto`, agent Access tab; spec `agents` and `agents[].project`. Status rows 407 and new rows | M-L |
| 3 | Channels and shared threads | Channels on the record and their routes. `pkg/threads` with records, human messages, the tag grammar, participants, and defaults per channel and thread. Runs answering messages and the room transcript. Experts on projects (data and routes). Mentions in the inbox with Reply; `@sme` and `ask_person` addressed to experts; replies recorded as answers. Thread SSE and a paged `GET /api/threads/{id}`. Channel and Thread views with the `@` typeahead and Post Only. Shared threads only. Status row 404 and new rows | L |
| 4 | People and project roles | Members, `Resolve` and the intersection rule on every project route. Members propose notes and owners approve them. Owner-set tighten-only limits with `Policy.Authority`. Move (subtree check under the lock, the `billing:write` rule) and archive. Custom kinds; paged listing and the 500 cap. People, History and Limits tabs. Every project stays visible: `*` required. Status rows 421 and 424 | L |
| 5 | Project inboxes and announcements | Kind `announcement`, `Broadcast` with per-reader hide and snooze, `project:` audiences and `Reader.Projects`, `Count.updates`, `Withdraw(by)`. The project inbox route, post and withdraw routes with rate limits, `prefs.Muted`. Inbox tab, Home "What's new", Updates and "Hide for Me", adapter templates and parity tests. Status rows 470 to 475 | M |
| 6 | Scheduled agents in projects (recaps) | `deliver` (project or inbox), posting on `ended()`, `agent_posts` and the draft part. `project_changes`, `runs_summary(project)`, the `Summarize` project filter, file-managed schedules in the spec. Schedules tab and "Add Weekly Recap". Status row 465 | M |
| 7 | Read confinement: members-only projects and private threads | `p.Sees` with 404 on every read path (runs, threads, events, manifest, trace, journal, analytics, artifacts and downloads, schedules, inbox, memory, homed agents). Private threads; members-only projects and the account default; no memory proposals from private threads. A contract sweep reads every GET and the event stream as a non-member. Status row 424 built | L, two PRs |
| 8 | Slack and GitHub coworking | Channel bindings, client home, surface-keyed threads, addressing, the room's messages, mention and expert DMs, the reply modal. Announcements posted top-level with delivery receipts; App Home on the shared Home through `act_as`; GitHub on the same rule; docs/slack.md. Status rows 381, 382 and 465 | L |

## Risks

- **A missed read path leaks.** `Run.Project` and thread content appear on
  many reads: threads, runs, events, manifests, traces, the journal,
  analytics, artifacts, the inbox and Slack notices. Members-only projects and
  private threads are refused until phase 7. That phase puts one predicate on
  every read, plus a sweep test that reads every GET as a non-member.
- **Migrated ownership is broad.** `{who: "*", role: owner}` keeps today's
  rights exactly, and it flows down to new children. Until owners are named,
  an account created under a migrated project is not private, and from phase
  7 it cannot be made members only (invariant 4).
- **The name is the identity forever.** Renames are impossible; `title`
  covers display. Delete is refused while anything points at a name, and
  tombstones stop a recreated name from silently adopting old conversations.
- **Breaking contract and rights.**
  - `Settings.projects` leaves the wire.
  - Project routes move to `projects:*`.
  - The baseline goes, and managers lose the workspace default to
    `projects:govern`.
  - An untagged API run with no default is 409.

  All of it is flagged as breaking, with release notes and a `config check`
  warning, and the migration writes explicit defaults.
- **Moves and usage.** `Within` is pinned when a run is admitted. After a
  move, later spend counts toward the new chain, so a parent's period total
  shifts. That is deterministic for `Rebuild` but surprising to read. Leaving
  a limited ancestor needs `billing:write`.
- **Prompt growth.** Inherited instructions (8,000 characters) and notes
  (6,000) ride on every turn in a deep project, on top of the project's own
  15,000. Budgets are fixed, the prompt says when something was cut, and
  depth is capped at 4.
- **Prompt injection.** Project instructions are owner-written text in the
  turn, not system text. Thread messages come from anyone in the thread.
  Recaps are model text over conversation content. Mitigations:
  - precedence is stated in words in the prompt;
  - anything that must hold is a policy, not an instruction;
  - posts are plain text with Slack mentions escaped;
  - wide audiences default to review;
  - `project_changes` reads only the run's subtree.
- **Defaults in busy threads.** A shared thread with a default answers every
  untagged message, which can be noisy. Messages that tag people, and replies to
  mentions and answers that tag no agent, start no run, and Post Only is
  always there. In Slack,
  only messages that mention the app reach warp.
- **Homing narrows widely.** Giving an agent a home removes it from `*`
  everywhere else. The route answers 409 while anything outside the new
  subtree uses the agent, and the console counts the projects that lose it.
  Schedules that lose their agent fail visibly.
- **Inbox growth.** The inbox has no retention and `Count` scans every
  message. Broadcasts add one message per post, not one per person, and
  per-reader state adds one change for each person who hides one. Rate limits
  bound posts; retention and per-reader counters are phase 9.
- **Addressing is not confidentiality.** Expert questions are routed to
  experts, but anyone fiber's `answerers` check admits may still answer
  through the run, as today. A project that needs a closed set of answerers
  names them in the agent's spec. A run that uses the credential of the
  person it acts for is answered by that person alone, experts included.
- **Membership by IdP group** relies on a policy file with empty roles and
  `grant ... to group`. The default policy has only four roles, and a group
  change applies at the next sign-in. Subjects can be listed directly.
- **`projects:govern` sees everything**, customer accounts included. Reads
  are not audited; writes are, through the `httpapi: change` log. Some
  customers may need recorded break-glass reads; that is a follow-up row.
- **Phase 1 is broad**: store, migration, permissions, the policy chain,
  context and the console tree. If review finds it too big, the safe split is
  1a (store, migration, permissions, routes, spec merge) and 1b (the chain
  and context). But 1a must refuse children until 1b lands, or a child
  escapes its parent's limit.
- **One app, many agents in Slack.** Slack shows every answer as the one bot
  user, so the agent's name and color go in the message. In a channel, a
  person must mention the app before the agent's handle. Code channels and
  per-agent bot users keep working through `client.Bots`.
