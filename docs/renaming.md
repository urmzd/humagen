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
GitHub rows are warp's own and in effect now. The Postgres, durable, Redis and
revision rows are fiber's and take effect with the first fiber release after
v0.20.1, and the wire codes with saige v0.41.0; until warp's `go.mod` names
those, the old identifiers are still the ones written.

| Identifier | Was | Is | Upgrade |
|------------|-----|----|---------|
| Postgres tables and indexes | `mandatum_*` | `fiber_*` | renamed in place at startup |
| Durable activity and record names | `mandatum.*` | `fiber.*` | drain runs in flight before upgrading; they are not replayed across it |
| Redis bus keys (`-bus=redis`) | `mandatum:` | `fiber:` | nothing: the bus starts empty under the new prefix; delete the old keys when convenient |
| Agent revision hashes | under `mandatum/agentspec/v1` | under `fiber/agentspec/v1` | fiber re-addresses stored revisions at startup; a hash kept outside warp (a link, a log) names the old one |
| Saige wire error codes | `durable.duraturo.*` | `durable.anchor.*` | upgrade every process together |
| GitHub review format | `mandatum.github.review.v1` | `warp.github.review.v1` | agents' prompts name the new one (see `examples/github-review`) |
| GitHub `/fix` grant | `mandatum.github.mutation.v1` | `warp.github.mutation.v1` | a grant issued before the upgrade is refused; ask for a new `/fix` |
| GitHub review format | `warp.github.review.v1` | `humagen.github.review.v1` | nothing: both are rendered; the new one is advertised |
| GitHub `/fix` grant, after Humagen | `warp.github.mutation.v1` | `warp.github.mutation.v1` | kept, so a grant issued before the upgrade still verifies |

Environment variables use `HUMAGEN_` (`WARP_` is still read), `FIBER_`,
`PLAYSCAPE_` and `ANCHOR_`.
