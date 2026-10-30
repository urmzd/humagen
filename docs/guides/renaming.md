# Names

The ecosystem uses one set of names, everywhere: in code, configuration,
persisted data and on the wire.

| Name | Responsibility |
|------|----------------|
| Humagen | The platform: the one process, its API, console and Slack app |
| Fiber | The library for the event bus, connectors, routing and the agent runtime |
| Saige | The library for agent execution and evaluations |
| Playscape | The library for governed execution environments and workspace isolation |
| Anchor | The library for durable execution, replay and recovery |

Earlier names (Collegium, Vultus and Warp for Humagen, Mandatum for Fiber,
Legatus for Playscape, Duraturo for Anchor) are retired. They survive only in
changelog history and in the one migration step that renames an existing
database's tables, so a deployment moves forward without a manual step.

Warp is the exception: the rename to Humagen keeps reading every name people
typed, with a note at start, and keeps every name stored data or a running
deployment depends on. [upgrading-from-warp.md](upgrading-from-warp.md) lists
both and what an operator does, and `internal/legacy` is the one place the
code names them.

Identifiers that changed with the last of the rename, and what to do. The
GitHub rows are warp's own and in effect now. The Postgres, durable and
revision rows are fiber's and take effect with the first fiber release after
v0.20.1, and the wire codes with saige v0.41.0; until warp's `go.mod` names
those, the old identifiers are still the ones written.

| Identifier | Was | Is | Upgrade |
|------------|-----|----|---------|
| Postgres tables and indexes | `mandatum_*` | `fiber_*` | renamed in place at startup |
| Durable activity and record names | `mandatum.*` | `fiber.*` | drain runs in flight before upgrading; they are not replayed across it |
| Agent revision hashes | under `mandatum/agentspec/v1` | under `fiber/agentspec/v1` | fiber re-addresses stored revisions at startup; a hash kept outside warp (a link, a log) names the old one |
| Saige wire error codes | `durable.duraturo.*` | `durable.anchor.*` | upgrade every process together |
| GitHub review format | `mandatum.github.review.v1` | `warp.github.review.v1` | agents' prompts name the new one (see `examples/github-review`) |
| GitHub `/fix` grant | `mandatum.github.mutation.v1` | `warp.github.mutation.v1` | a grant issued before the upgrade is refused; ask for a new `/fix` |
| GitHub review format | `warp.github.review.v1` | `humagen.github.review.v1` | nothing: both are rendered; the new one is advertised |
| GitHub `/fix` grant, after Humagen | `warp.github.mutation.v1` | `warp.github.mutation.v1` | kept, so a grant issued before the upgrade still verifies |

Environment variables use `HUMAGEN_` (`WARP_` is still read), `FIBER_`,
`PLAYSCAPE_` and `ANCHOR_`.

## Fiber

Fiber was called Mandatum. Its code now lives in this repository, under
`fiber/`, and the old name is read only by `internal/legacy/fiber.go`. Its
ecosystem table had this row:

| Previous | Current |
|----------|---------|
| Collegium, Vultus and Warp | Humagen |

Warp was Humagen's name when fiber was renamed, and Collegium and Vultus
before that. Humagen's own rename keeps more than fiber's did: names people
typed are still read, and names stored data depends on are kept, as
[upgrading-from-warp.md](upgrading-from-warp.md) lists. Nothing below applies
to them. The fiber names this section covers are the ones that moved from
`mandatum` to `fiber`; the `fiber_*` tables, activity names and record keys
they moved to are now kept names themselves.

### Persisted identifiers that changed

| What | Before | Now |
|------|--------|-----|
| Postgres tables, indexes and constraints | `mandatum_*` | `fiber_*` |
| anchor activities and records | `mandatum.turn`, `mandatum.turn.agent`, `mandatum.turn.resume`, `mandatum.park`, `mandatum.stop`, `mandatum.question.*`, `mandatum.answer.*`, `mandatum.usage.*`, `mandatum.child.*` | the same under `fiber.` |
| Agent hash domain | the old spec domain under `mandatum/` | the same under `fiber/`, retired with the event union (see [events.md](../../fiber/docs/architecture/events.md#10-upgrade)) |
| Knowledge document hash domain | `mandatum.knowledge.v1` | `fiber.knowledge.v1` |

### Upgrading a deployment

1. **Drain first, parked runs included.** Stop accepting tags and let every
   run end on the old version. A run in flight across the upgrade cannot be
   resumed: its recorded activities and records carry the old names, and the
   new code reads only the new ones. A run parked on a question does not end
   by itself: it waits until someone answers it, it is stopped, or its
   question expires (`runtime.WithQuestionTTL`, 24 hours by default, applied
   at the next janitor poll). Answer or stop each one on the old version.
   This lists what is still pending there:

   ```sql
   SELECT run_id, created_at FROM mandatum_runs WHERE status = 'pending' ORDER BY created_at;
   ```

   and the old version's run status says which of them are parked and on
   what question.

   A run that is still pending when the new version starts is not resumed.
   The first time a worker claims it (the janitor's poll reaches a parked
   one within its interval, 30 seconds by default) it ends failed, once and
   terminally: the ledger records why, `lifecycle.failed` with the same
   reason is published on its topic, and the runtime logs an error naming
   the run. The agent does not run again for it. A surface the old process
   delivered to is not updated, because a run's delivery routes are
   registered in the process that accepted it; anyone subscribed to the
   topic is told. Raise it again as a new tag (the same tag id lands on the
   failed run), and its answer is delivered where the new tag's routes say.
   After the upgrade, these are the runs it applies to:

   ```sql
   SELECT run_id, created_at FROM fiber_runs
   WHERE status = 'pending' AND envelope->>'Name' = 'mandatum.turn';
   ```

2. **Stop every old process before the first new one starts.** A process
   from before the rename creates its `mandatum_*` tables again when it
   starts, and writes there. If the new version finds an old table beside
   its new one, its `Migrate` fails with an error naming both instead of
   choosing between them; stop the old process, move any rows it wrote, drop
   the old table, and start again.
3. **Postgres moves itself.** Every store's `Migrate` (which `fiber.New`
   runs for `WithPostgres`, and `durable.Open` runs for the ledger) first
   renames its own `mandatum_*` tables in the current schema, with their
   indexes and constraints, to `fiber_*`, in the same transaction as the rest
   of the migration, so rows are kept and the order stores migrate in does not
   matter. The old agent registry tables are no longer read: agents are
   saige definitions in `fiber_definitions` (see
   [events.md](../../fiber/docs/architecture/events.md#7-definitions)). `internal/legacy` (`FiberTablesSQL`) is the only code that knows the old names.
4. **Knowledge re-embeds once.** A document's hash includes its domain, so
   the first re-index after the upgrade treats every document as changed.
5. **Anything that stored a revision hash** outside fiber (a link, an audit
   log) holds the old address. The new one is the definition content hash,
   `App.Definitions().Get(ctx, tenant, name, version)` then `.Digest` (see
   [events.md](../../fiber/docs/architecture/events.md#7-definitions)).

Historical changelog entries keep the names they were written with.
