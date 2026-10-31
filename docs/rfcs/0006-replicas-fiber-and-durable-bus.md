# Multiple replicas, fiber folded in, and a durable event bus

Status: proposed. This document changes no behavior. It describes the target
design and the order of the changes that reach it.

## Goal

Humagen runs as more than one replica on one database. Any replica can serve
any request and any event stream, and every event is delivered to a surface
once, even when a replica stops mid-delivery. fiber is part of Humagen in
every sense, not only in its import path, and the standalone `urmzd/fiber`
repository has a clear end.

This RFC covers three things: the fold of fiber, the fate of the standalone
repository, and the event bus, including how the bus behaves with many
replicas. It does not plan the stores. The store plan is
[RFC 0004, horizontal data](0004-isolated-runs-and-person-context.md#horizontal-data)
(change 9 there). This RFC names where the bus depends on it.

## Current boundary

- **fiber's code is in the module.** It lives in `fiber/` and is imported as
  `github.com/urmzd/humagen/fiber/...` since #106. It still carries a separate
  project's shape: its own `README.md`, `AGENTS.md`, `CHANGELOG.md`,
  `llms.txt`, `docs/architecture/`, a root package `fiber` with `fiber.New`,
  the `cmd/fiber` command and `examples/`. Humagen imports only `fiber/pkg/...`.
  Nothing outside `fiber/` imports the root package.
- **The standalone repository is still released.** `urmzd/fiber` is at
  v0.26.1. Its code has stopped moving; the changes since the fold are in
  Humagen only.
- **The bus.** The Redis bus is gone (#133). `bus.Memory` serves one process.
  `bus.Postgres` keeps events in `fiber_events` and `fiber_event_runs`, wakes
  readers with LISTEN/NOTIFY on one connection per process, and is the
  default whenever the stores are on Postgres. Every publish runs one
  function, `fiber_event_publish`, under one advisory lock per schema, so
  publishes from every tenant and every replica are serialized. The lock
  exists so an `id` commits in order and a reader scanning `id > cursor`
  never skips a row. Events are kept as a ring per run and per channel and
  forgotten after 24 hours.
- **Delivery is at-least-once, per process.** Each subscriber keeps its
  cursor in memory. The bus is never the record: anchor's ledger is.
- **One replica.** `pkg/state` holds an advisory lock per schema, so a second
  process is refused at start. RFC 0004 change 9 removes it store by store.

## Bus-side state that assumes one replica

The stores are listed in
[limits at scale](../architecture/overview.md#limits-at-scale) and planned in
RFC 0004. These are the parts around the bus that would misbehave with a
second replica even once every store reads per request:

| Where | What it assumes | With two replicas | Fix |
|-------|-----------------|-------------------|-----|
| `fiber/pkg/egress` (`Engine`, lanes) | It is the only deliverer. It skips "a sequence a run already reached" per Engine, in memory, and skips old events on resubscribe by age | Every replica renders every event: two Slack posts, two GitHub comments | A consumer group: one replica holds a tenant's delivery lease and commits a cursor after the sink accepts |
| `internal/platform` observer (`watch`, `observeTenant`) | One process folds run events into the journal, the run index and usage | Each replica folds every event | A consumer group, and a fold idempotent by `(run, seq)`. The stores side is RFC 0004 change 9 |
| `fiber/pkg/dispatch` (`Dispatcher`, `recent`) | The runs it started per channel are in memory, for `slack.session.stopped.v1` | Both replicas call `Accept`; the run key joins them, so no double run, but twice the work. After a failover, `recent` is empty and a session stop misses its runs | A consumer group, and the stop found from the run index by channel, not from memory |
| `fiber/pkg/connectors/cron` | Each process ticks | Each replica ticks. The occurrence id joins them into one run | None needed. A group is optional, to save the duplicate work |
| `fiber/pkg/runtime` control (`stop.go`, `compact.go`) | Reads stop and compact requests from the bus | Works: every replica reads the control channel, and the one running the run acts | None |
| `internal/httpapi` event stream, `pkg/rooms` | A reader's stream and a room's coalescing are per process | Works on `bus.Postgres`: any replica can serve any reader. Coalescing thresholds are per replica | None for correctness. Rooms move to the bus sharded by conversation (RFC 0004) |
| `bus.Memory` | One process | Two replicas do not see each other's events | Refuse to start more than one replica on the memory bus |
| `bus.Postgres` publish lock | One lock per schema | Every replica's publishes queue on one lock: one short transaction per event for the whole database | A cursor per tenant and a lock per tenant (below) |

## Decision

### fiber is folded in fully

fiber stays a layer with its own directory and import path,
`github.com/urmzd/humagen/fiber/pkg/...`. Moving it to `pkg/` would rename
hundreds of imports for no change in behavior, and the directory keeps the
layer visible ([ecosystem](../architecture/ecosystem.md)). What goes is the
shape of a separate project:

| Piece | Fate |
|-------|------|
| `fiber/docs/architecture/*.md` | Move into `docs/architecture/` (events, Slack, GitHub), merged with what Humagen already says. One set of docs |
| `fiber/README.md`, `fiber/llms.txt` | Removed. Humagen's README and `llms.txt` describe the layer |
| `fiber/AGENTS.md` | Reduced to what is specific to the layer, or merged into the root `AGENTS.md` |
| `fiber/CHANGELOG.md` | Frozen with a line that says later changes are in Humagen's changelog, then removed after the final standalone release |
| Root package `fiber` (`fiber.New`, options, `status.go`) | Removed. `humagen.Main` and its options are the one public face. Its tests that still hold move to the packages they test |
| `cmd/fiber` (`fiber run`) | Becomes a `humagen` subcommand that runs a definition locally, or is removed if `humagen start` covers it |
| `fiber/examples/` | `local` folds into the subcommand's docs; `downstream` is covered by Humagen's own downstream example |
| `FIBER_POSTGRES_TEST_URL`, the `fiber-postgres` compose service | One integration database for the whole module |

Names stored data depends on (`fiber_events`, `fiber_event_runs`, the
definition tables) keep their names. Renaming a table buys nothing and costs a
migration.

### The standalone repository is deprecated, not archived

1. **A final release.** `urmzd/fiber` gets one last minor release, v0.27.0. It
   changes no code. It adds a deprecation notice at the top of the README that
   points to `github.com/urmzd/humagen/fiber`, and a `// Deprecated:` comment
   on the `module` line of `go.mod`, so `go get` and pkg.go.dev warn every
   importer.
2. **No more releases.** Issues and pull requests point to Humagen. Security
   fixes are made in Humagen only.
3. **Archived later.** The repository stays readable and unarchived. It is
   archived only when no public module imports it, checked on pkg.go.dev, and
   not before Humagen's fold has had one full minor release without a fiber
   regression.

That release is a change to `urmzd/fiber`, made there and approved by its
owner. Nothing in this RFC's Humagen changes touches it.

### The bus stays on Postgres, as an outbox with consumer groups

Postgres LISTEN/NOTIFY with `fiber_events` as the outbox is the bus. Redpanda
or Kafka is not added now. The `Bus` interface stays the seam, so a log broker
can be a kind later, fed from the same outbox.

| | Postgres LISTEN/NOTIFY + outbox | Redpanda or Kafka |
|---|---|---|
| Moving parts | The database every deployment already has | A second stateful cluster to run, secure, back up and upgrade |
| Event and state change together | One transaction: the event row is the outbox | Needs an outbox and a relay anyway, so Postgres is still in the path |
| Tenant isolation and default-deny prefixes | A `WHERE` on tenant, channel and type, served by indexes | Topics and partitions do not map to per-subscription prefix filters; every consumer filters a shared partition |
| Resume a run from a sequence | An index on `(tenant, run, seq)` | A scan of the partition, or a separate index |
| Throughput | One primary's commit rate, today behind one lock | Far higher, partitioned |
| Retention and replay | Hours, bounded by table size | Days to forever, cheap |
| Consumer groups | To be built: a lease and a cursor per group | Built in |

The deciding facts: Humagen's events are small and per tenant, the hot
question is "this tenant's events after this cursor" or "this run after this
sequence", and anchor's ledger, not the bus, is the record. A log broker
answers a different question (high-volume ordered streams with long replay)
and would still need the Postgres outbox to avoid writing state and event
separately. The Redis bus showed the cost of a second broker: it was removed
because nothing needed it.

Move to a log broker when one of these is measured, not before:

- the sustained publish rate nears what the primary commits, after the
  per-tenant lock below;
- consumers need replay longer than the bus's retention, and the journal or
  OTel export cannot serve it;
- consumers outside Humagen need the stream.

## How the bus works with many replicas

### A cursor per tenant

Every subscription names one tenant, so readers never need an order across
tenants. Each tenant gets its own sequence (`fiber_event_tenants.top`), taken
under an advisory lock on that tenant. A reader's cursor is `(tenant, tseq)`.
Publishes from different tenants no longer wait on each other. The global
`id` stays for retention and the sweep. `Subscription.From` keeps its meaning
for run sequences. A very large room that makes one tenant hot is split later
by conversation, the shard key RFC 0004 names.

LISTEN/NOTIFY has a ceiling of its own: Postgres serializes the commit of
every transaction that notifies. The benchmark in change 1 measures it with
and without NOTIFY per publish, and coalescing (one NOTIFY per tenant per
short window, not per event) is applied if it is the bottleneck.

### Consumer groups

A consumer group is a named consumer with one cursor per tenant, held by one
replica at a time:

- `fiber_consumers (group, tenant, cursor, owner, lease_until)`. A replica
  takes a tenant's lease when it is free or expired, renews it while it
  reads, and commits the cursor after it has handled an event.
- `bus.Group` (or `Bus.Consume(ctx, group, sub)`) reads from the committed
  cursor. `bus.Memory` implements it trivially, so one process behaves the
  same, and `bustest` holds both to failover cases: a lease that expires
  mid-batch hands over without a gap.
- Delivery stays at-least-once. After a failover the new owner may redo the
  events after the last commit. Consumers stay idempotent by `(run, seq)` and
  by event id, as the bus contract already says.

Egress, the observer and dispatch each become one group. Plain subscribers
(an event stream to a browser, a control reader, a room) stay plain: every
replica may serve them.

### The outbox

`bus.Postgres` gains `PublishTx(ctx, pgx.Tx, e)`: the event row is written in
the caller's transaction, so a state change and the event that announces it
commit or fail together. `fiber_events` is the outbox; Postgres consumers read
it directly, with no relay. If a log broker is ever added, a relay reads the
same rows and writes them to topics partitioned by tenant, and Postgres stays
the index for resuming a run.

## Changes

| # | Area | Change | Depends on |
|---|------|--------|------------|
| 1 | bus | This document. A publish and fan-out benchmark for `bus.Postgres` across processes, and the storage section of the overview corrected (the table is `fiber_events`; the NOTIFY payload is the tenant) | |
| 2 | bus | A cursor and a lock per tenant in `bus.Postgres`, with a migration that fills `tseq` for retained rows | 1 |
| 3 | bus | Consumer groups: `fiber_consumers`, the group API on both backends, and `bustest` failover cases | 2 |
| 4 | egress | Egress reads through a group; the cursor is committed after the sink accepts. The age skip on resubscribe goes | 3 |
| 5 | dispatch | Dispatch reads through a group; a session stop finds its runs from the run index | 3, RFC 0004 change 9 (run index) |
| 6 | platform | The observer reads through a group and folds idempotently by `(run, seq)` | 3, RFC 0004 change 9 |
| 7 | bus | `PublishTx`, used where an event announces a state change | 2 |
| 8 | platform | Refuse more than one replica on `bus.Memory` | |
| 9 | fiber | Docs moved into `docs/architecture/`; README, `llms.txt` and the changelog retired; `AGENTS.md` merged | |
| 10 | fiber | The root package and `examples/` removed; `cmd/fiber` becomes a `humagen` subcommand or goes; one integration database | 9 |
| 11 | urmzd/fiber | The final release with the deprecation notice, made in that repository | 9 |

Each change is its own pull request and keeps today's behavior on one
replica. Allowing more than one replica in the chart is the last step of
RFC 0004 change 9, and it waits for changes 4 to 6 here.

## Alternatives

- **Redpanda or Kafka now.** Rejected for now (above): a second cluster, an
  outbox still needed, and a poor fit for per-tenant default-deny filters. It
  stays possible behind the `Bus` interface.
- **NATS JetStream.** The same trade as a log broker with less operational
  weight, and the same need for an outbox. Not worth a second system before
  the Postgres ceiling is measured.
- **Leader election for the whole process.** One replica would run egress,
  the observer and dispatch for every tenant. Simpler, but one replica would
  carry all delivery, and a failover would move every tenant at once. Leases
  per tenant spread the work and move one tenant at a time.
- **Move fiber to `pkg/`.** Rejected: a large rename with no behavior change,
  and the layer would disappear from the tree.
- **Archive `urmzd/fiber` now.** Rejected: an archived repository cannot take
  the deprecation release, and importers would get no warning from the Go
  tooling.

## Open questions

- Whether `cmd/fiber`'s local run earns a `humagen` subcommand, or
  `humagen start` with the offline provider already covers it.
- How long a consumer group's lease should be, against how long a slow sink
  may hold a delivery.
- Whether anchor can take the caller's transaction, so a ledger step and its
  run event commit together through `PublishTx`.
- Whether egress should commit per event or per batch, given that a Slack
  post is not idempotent without a key, while an edit is.
