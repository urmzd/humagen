# Isolated runs and person context

Status: proposed. This document changes no behavior. It describes the target
design and the order of the changes that reach it.

## Goal

One agent, many people, at once. The same agent definition runs for every
person who tags it, in parallel, and each run sees only what its project and
its requester bring to it. Every run executes in an isolated sandbox, not in
the serving process. The design must hold for a single room with a million
people in it.

## Current boundary

- **Execution.** Every turn runs in the `humagen serve` process. playscape's
  sandbox is policy confinement over the workspace, not process isolation:
  the platform builds its plane on `inproc` nodes
  (`internal/platform/platform.go`).
- **Files.** A conversation has one scratch directory, opened as an
  `os.Root`, shared by every run in it, including called runs
  (`internal/platform/sandbox.go`).
- **Tool servers.** A stdio MCP server is a host process started with the
  platform's own privileges (`fiber/pkg/mcptools`). A person-scoped provider
  already connects with that person's credential, one session per person.
- **Person state.** It is spread over `pkg/credentials` (credentials),
  `pkg/prefs` (display choices only, which never grant) and nothing else.
  `pkg/memory` is a per-agent list of notes sent on every turn.
- **Scale.** Humagen is one process. Stores read their namespace into memory
  and own it as a single writer, and admission is an in-process lock
  ([deploy](../guides/deploy.md), [limits at scale](../architecture/overview.md#limits-at-scale)).

## Decision

A run is four things, each resolved once and pinned:

| Part | Shared or private | Holds | Pinned as |
|------|-------------------|-------|-----------|
| **Agent definition** | Shared | Instructions, tools, model, policy | `definition.Pin` (exists) |
| **Project settings** | Shared, inherited down the project chain | Ceilings, preferences, knowledge, base snapshot | Part of the run context |
| **Person context** | Private to the requester | Credentials (by reference), MCP servers, preferences, memory volume | Part of the run context |
| **Sandbox** | Private to the run | Processes, scratch writes | A lease on an instance |

The same agent answers different people differently because of the person
context, never because the definition changes.

The agent loop stays in a trusted worker. Only tools run in the sandbox: the
shell, the file tools, and the person's tool servers. Model keys, the
platform's database credentials and other people's secrets never enter a
sandbox.

## The run context

The run context is resolved at admission and pinned to the run with a digest,
the way the definition is.

- Parallel runs of one person never race a settings change made mid-run.
- A retry or a replay through anchor sees exactly what the first attempt saw.
- The pinned context is the record of what the run could do and whose access
  it used.

A called run inherits its caller's run context. It can narrow it, never widen
it. In a shared thread, a run uses the context of the person whose message
started it.

### Two kinds of setting

Every setting is one of two kinds, declared once in its spec field:

| Kind | Resolution | Examples |
|------|------------|----------|
| **Ceiling** | The strictest value on the chain wins: workspace, each project from the root down, then the person. A lower level can only tighten | Budget, allowed tools, egress hosts, allowed models, call depth |
| **Preference** | The nearest value wins: the person, then the project, then its ancestors, then the default. It is clamped by the ceilings | Model, compaction threshold, prompt caching, temperature |

Permissions are an intersection: a run may do what the agent's grants allow
**and** what its requester may do. Without that, a person could use an agent
to reach what they cannot reach themselves.

### The person store

One store holds what a person brings to their runs:

- **Credentials**, as references (`pkg/credref`) or values in the secrets
  store, as today.
- **Tool servers**: MCP servers a person adds for themselves, within the
  project's allowed list. They run in the person's sandbox compartment (below),
  never on the host.
- **Agent preferences**: the preference settings above.
- **Memory**: a volume the person owns (see [Memory](#memory)).

`pkg/prefs` keeps the display choices. Its rule, that a personal setting only
narrows, holds for the whole store.

## The sandbox

The sandbox is playscape's. Humagen asks for an instance by policy and never
names a technology.

```go
// playscape: pkg/isolate
type Driver interface {
	Start(ctx context.Context, spec Spec) (Instance, error)
}

type Instance interface {
	ID() string
	Exec(ctx context.Context, cmd Command, stdio Stdio) (ExitStatus, error)
	Files() workspace.Workspace
	Close(ctx context.Context) error
}
```

| Driver | Boundary | Use |
|--------|----------|-----|
| `docker` with `runsc` (gVisor) | A user-space kernel per instance | Default for production |
| `docker` with `runc` | Namespaces, cgroups, seccomp, no capabilities | Development, trusted tools |
| `firecracker` | A microVM per instance | Hostile code, later |
| `local` | None, and it says so at start | Tests only |

Every instance starts with no network, a read-only root, no capabilities, a
non-root user, and CPU, memory, process and disk limits from the run's
ceilings.

### Compartments

A run's sandbox has two compartments that share nothing but a socket:

- **Workload**: the shell and file tools. It sees the run's files.
- **Connectors**: the person's tool servers. It holds the credentials those
  servers need, so code the agent runs in the workload cannot read them.

### Files

| Mount | Source | Access |
|-------|--------|--------|
| `/project` | The project's base snapshot | Read-only |
| `/work` | The run's own layer over `/project` | Read and write, discarded at the end unless promoted |
| `/me` | The requester's volume | Read and write, only in that person's runs |

Promotion is an explicit, recorded write: publishing a file (as today),
writing to project memory, or posting in the thread. One person's run cannot
quietly change what another person's run sees.

### Network

Default deny. A run reaches the hosts its ceilings allow, through an egress
gate that injects credentials on the way out. The sandbox holds a placeholder,
and the gate swaps in the real value for an allowed host. A leaked environment
variable is then worthless anywhere else.

### Instances at scale

Starting an instance per run must cost milliseconds, not seconds, so a pool
keeps warm instances per image and leases one to each run. An instance is
never handed to a second run. It is destroyed at the end, and the pool starts
a fresh one in the background.

## Memory

`pkg/memory`'s notes are replaced by files.

- **Agent memory in a project** lives under `/project/memory/<agent>/`. A run
  reads it. A run's write to it is a proposal that a person approves, as
  today, and becomes part of the next base snapshot.
- **A person's memory** lives under `/me/memory/`. Their runs read and write
  it freely. Nobody else's run mounts it.

saige's memory policy still screens what is written to project memory. Notes
in `pkg/memory` are exported to files once. `pkg/memory` becomes a read
adapter over the files, and is removed when nothing reads it.

### Where the files live

A file is two records: its content and its place.

| Store | Holds | Role |
|-------|-------|------|
| **Database** | The manifest: path, content hash, version, owner, access, and the version history | Metadata, durability, restore |
| **Blob store** | The content, addressed by its hash | Bytes, shared by every run that reads them |
| **Platform cache** | Recent manifests and blobs, in memory | Fast reads for the platform and for leases |

At lease time the platform installs the manifest's files into the instance.
Content comes from a per-host cache keyed by hash, so a million runs over one
project fetch its memory once per host. When a run ends, the platform diffs
the run's layer, uploads changed content, and writes new manifest entries:
committed for `/me`, proposed for project memory. Restore is checking out an
earlier manifest version.

Queries do not go through the files. A `memory_search` tool calls the API,
which answers from the index with ranking and access checks. The files are for
reading and editing. The API is for finding.

A live mount (FUSE) that reads through to the API was considered and
rejected: it needs `/dev/fuse` and extra privileges inside the sandbox, which
gVisor restricts, every read becomes a network round trip, and a stalled read
stalls the agent. Fetching large trees lazily, by manifest entry, can come
later without changing this shape.

## A million people in one room

Isolation is the easy part. One process is the hard limit. The target has
three roles that scale independently:

| Role | Holds | Scales by |
|------|-------|-----------|
| `humagen serve` | The API, sign-in, the event stream | Replicas behind a load balancer, stateless |
| `humagen worker` | Agent loops: anchor activities from the run queue | Replicas, as the queue grows |
| Sandbox hosts | The instance pool | Nodes, as concurrent runs grow |

The numbers that drive it, for one room of 1,000,000 people where 1% post once
a minute:

| Quantity | Value | Consequence |
|----------|-------|-------------|
| Messages | about 170 a second | Admission and the ledger are shared-store transactions, not an in-process lock |
| Runs, at one in ten messages, 30 seconds each | about 500 at once | A pool of about 500 instances, roughly 50 GB at 100 MB each |
| Deliveries, if every message went to every viewer | 170,000,000 a second | **Not possible.** A room is not a broadcast |

So a large room changes how it is read:

- A viewer receives the runs and messages that involve them, and the room's
  tail as a coalesced page every few seconds, not each event as it happens.
- Streams are sharded by conversation on the bus (Redis streams today), and
  an SSE gateway holds the connections.
- Admission adds a per-room rate and a per-person concurrency limit to the
  existing per-scope limits.

Before any of this, the [limits at scale](../architecture/overview.md#limits-at-scale)
must go: the admission and spend counters move into the shared store, and the
stores stop being single-writer.

## Horizontal data

Today every store reads its namespace into memory at start, keeps its logic
and indexes there, and treats the database as a log to replay. Two processes
would hold two different copies, so `pkg/state` takes a lock that refuses the
second. That caps the platform at one process and one database.

The table is not the problem. `pkg/state` is one table keyed by
`(namespace, tenant, key)`, and the tenant is already a shard key. The memory
copy is the problem.

### Rules

- **The database is the truth.** A store reads and writes it per request. A
  cache in front of it may be dropped at any time without changing an answer.
- **Every key carries its shard key.** A query names one shard. Nothing scans
  across shards on a request path.
- **No transaction crosses shards.** A link between shards is an id, and work
  that spans them is an anchor run that retries, not a distributed
  transaction.
- **Postgres wire protocol.** Citus distributes by the shard key with little
  change to queries. Distributed SQL (CockroachDB, Spanner) stays possible.

### Shard keys

| Data | Shard key | Why |
|------|-----------|-----|
| Tenant records: projects, agents, settings, people, credentials | `tenant` | Small, read often, never joined across tenants |
| Conversation streams: messages, events, runs | `conversation`, then a time bucket | The hot path. Appended, never updated in place |
| A very large room | `conversation`, then a hash of the sender | One room must not be one shard |
| Counters: admission, spend, rate | The scope, then a stripe | Many small rows, summed on read |
| File manifests | The owner | Content is addressed by hash and shared |
| The shard directory: which tenant lives where | Copied to every node | Tiny, read on every request, written rarely |

### Striped counters

A counter per scope behind one lock serializes every admission in that scope,
and one large room is one scope. A striped counter keeps N rows per scope. An
admission takes one stripe, chosen at random, and checks it against its share
of the limit. When a stripe runs out, it borrows from the others in one short
transaction. Reads sum the stripes. The limit holds exactly; only the order of
admissions near the limit is less fair.

### Order of work

1. `pkg/state` gains per-request reads, bounded queries and compare-and-set
   writes, and the conformance suite covers them on both backends.
2. Stores move one at a time, the conversation stream and run index first,
   since they grow fastest. Each loses its replay at start.
3. When no store needs the lock, `pkg/state` drops it, and the chart allows
   more than one replica.
4. Counters become striped.
5. The schema is distributed by its shard keys.

## Alternatives

- **The whole turn in the sandbox.** Rejected. The model key and the
  platform's credentials would enter it, and the loop is trusted code.
- **One warm environment per project.** Rejected as the default. State left by
  one person's run would run inside the next person's. A project keeps a
  base snapshot instead, and every run starts clean from it.
- **A Kubernetes Job per run.** Too slow to start for a conversation. It stays
  available for long batch runs through `runner/k8s`.

## Changes

| # | Repository | Change | Depends on |
|---|------------|--------|------------|
| 1 | humagen | This document | |
| 2 | playscape | `pkg/isolate`: the driver seam, the `docker` driver (`runc`, `runsc`), the `local` driver, and the pool | |
| 3 | playscape | The egress gate, with credential injection | 2 |
| 4 | humagen (fiber) | Tools run through a run's instance: the file tools, the shell, stdio MCP servers | 2 |
| 5 | humagen | The run context: the person store, setting kinds, resolution and pinning at admission | |
| 6 | humagen | Memory as files, and the export from `pkg/memory` | 4, 5 |
| 7 | humagen | `humagen worker`, and admission and spend counters in the shared store | 5 |
| 8 | humagen | Large rooms: coalesced reads, per-room admission | 7 |
| 9 | humagen | Horizontal data: stores read and write per request, keys carry their shard key, striped counters, then the single-writer lock goes | 7 |

anchor needs no change: a run in a remote instance is an activity that can
fail and be retried.

## Open questions

- Which image a project's sandbox uses, and who may change it.
- How long a person's volume is kept after they leave the workspace.
- Whether a project may opt into a warm environment for long, single-owner
  work, where cross-person persistence is not a risk.
