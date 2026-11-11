# AGENTS.md

## What this is

A Go module that composes saige into one agent platform and its
web and Slack presentation in one repository. saige
(`github.com/urmzd/saige`) is one module holding the agent SDK, `flow` (the
transport, formerly fiber), `durable` (formerly anchor) and `sandbox` (formerly
playscape). The design is in
[docs/architecture/overview.md](docs/architecture/overview.md). Current console design requirements
and their acceptance criteria are tracked in [docs/reference/design-requirements.md](docs/reference/design-requirements.md). Read it
before changing presentation or integrations, and update its requirement status
and evidence when implementing a requirement.

## Rules

- **Every choice is a file or an interface, never a fork.** A new backend is a kind in a spec slot (`pkg/spec`, `humagen.WithKind`); a new setting is a spec field with a default, documented in docs/reference/spec.md, whose examples are tested.

- **Compose, don't reimplement.** If saige (its agent packages, `flow`,
  `durable`, `sandbox`) owns a concern, use it or change it upstream in that
  repository. The ledger and queue open only through `internal/runschema`, which keeps the
  stored `fiber_*` table names.
- **One way to call an agent.** Subagents, handoffs and delegations all go
  through `pkg/call`. Do not add a second path.
- **Default deny.** A zero value grants nothing: no calls, no budget, no access.
- **Fail closed.** A missing or malformed edge on an agent's tag is an error.
- **Every event type has a renderer and an OpenAPI mapping.** A type is
  registered once in flow's catalog (`saige/flow/event`) with its body, its
  renderer and its sensitivity; `TestCatalogComplete` fails without a
  renderer. A `run.*` type also needs its `RunEvent` variant in
  `api/openapi.yaml`, which `TestSpecSchemasHaveTheFieldsTheGoTypesMarshal`
  checks.
- **Subscriptions are default deny.** An empty subscription receives
  nothing, an agent's own output never wakes it, and the opt-in families
  (`run.usage`, `run.context`) arrive only when named.
- **Keep presentation separate.** Shared UI packages render data and emit actions.
  API calls, persistence and identity belong in the platform and surface hosts.
- **Tenant comes from the Principal.** Never read it from a request, and return 404 for another tenant's objects.
- **Every route checks one permission.** Add it in `Server.Handler`, next to the route.
- **The ledger decides whether a run is over.** Events report attempts. Do not infer an ending from silence.
- **Bound every query.** A window, a row cap and a bucket cap, enforced on the server.
- **Evals are deterministic.** A check is a pure function of the output. Do not add a model as a judge without saying so in the suite.
- **A page is headed by its tabs, or else by one title.** Every page uses `Page` from `apps/web/src/ui.tsx`, which does this: a page with tabs (its own, or a hub's) shows the tab row as its header, with no visible title and no lede (the title stays for assistive technology); a page without tabs shows one title. Page actions sit at the right of the header. Do not add explanatory paragraphs to pages; an empty state is a title and one action. Settings (yours, and the workspace's) open from the account row, and Connections is a section of your settings at `/settings/connections`.
- **Every place has an address.** Pages, tabs and selections are paths in `apps/web/src/route.ts`. A new page or tab adds its route there, with a test.
- **An agent is a small bot in its color.** Use `AgentMark` (or `AgentInitials` in shared packages): the bot's shape comes from the agent's name, so no person's mark looks like it; it hops while the agent works and the orb on its corner shows what it is doing. Below 16px the mark is the orb alone, and without a canvas it is the agent's initials in a thin ring. The marks are drawn by `bot-avatars` and `thinking-orbs`; the composer's light by `border-beam`, `voice-glow` and `metal-fx`. Controls under the composer are text, not boxes.
- **The API has one contract.** A route, a handler's answer or a type's JSON
  fields change together with `api/openapi.yaml`, then `make client`. Never edit
  `packages/client/src/generated`.
- **Old names live in one place.** The product was called warp, and fiber
  had an earlier name of its own (docs/guides/renaming.md#fiber, the only doc that
  spells it). A name from either is read only through `internal/legacy` (Go)
  or `apps/*/src/legacy.ts`, and a name stored data depends on is kept and
  pinned by a test. `TestNoNewOldName` fails on any other mention;
  docs/guides/upgrading-from-warp.md lists both kinds.
- **Keep the status table honest.** When a piece lands, update
  `docs/architecture/overview.md#status` in the same change.

## Layout

- `internal/legacy`: every name from before the rename to Humagen that is still read, the names kept for stored data, and the notes printed at start. `pkg/call`: the call primitive. `pkg/runs`: the run index and statistics. `pkg/auth`: principals, roles, OIDC. `pkg/authz`: the policy language. `pkg/auth/mockidp`: the loopback development provider.
- `pkg/journal`: the event log, queries and traces. `pkg/alerts`: rules and alerts. `pkg/knowledge`: sources and search. `pkg/corpora`: corpora, the named bodies of knowledge that hold sources and catalog connectors. `pkg/graph`: the workspace knowledge graph, one slice per corpus. `pkg/catalog`: what exists in connected systems, metadata only, searched hybrid (RFC 0005); `pkg/catalog/bigquery` its first connector kind; `internal/platform/catalog.go` crawls, publishing and the `catalog_search` and `catalog_query` tools.
- `pkg/memory`: what an agent carries between conversations, as notes over `pkg/drive`. `pkg/drive`: versioned file trees (manifests on `pkg/state`, content by hash in `pkg/blob`), installed into and diffed from a run's files. `internal/platform/hooks.go`: housekeeping runs.
- `pkg/clients`: the Slack apps and GitHub installations a workspace answers through. `internal/platform/connectors.go`: one surface per scheme, fanning in and out to them.
- `pkg/evals`: suites and runs, scored by saige. `pkg/signals`: one run's signals, folded from its events and ratings. `pkg/join`: whether each agent takes a message that names none (the dynamic router). `pkg/prefs`: a person's own settings. `pkg/marks`: what a person marked on conversations (archived for them alone, when last read, dismissed tips).
- `pkg/state`: the record store every Humagen store writes through, in memory or in Postgres, with the `statetest` suite. A store keeps its logic in memory and gains `Open(state.Store)`, or reads and writes per request (`Lookup`, `List`, versioned ops) with the tenant as its shard key (docs/architecture/overview.md#storage); `internal/platform/backends.go` chooses the backends. `pkg/notice`: a store's change notices (`Watch`) to its watchers in every process, relayed by flow's `bus.Notices` on Postgres.
- saige's `flow`, `durable` and `sandbox`, imported as `github.com/urmzd/saige/...` (not in this repository):
  - `flow/event`: the envelope, the sealed bodies and the catalog. `flow/bus`: the bus, by channel and type prefix. `flow/egress`: delivery of a run's events to its channel's sink.
  - `flow/definition`: saige definitions plus a policy, and the pin a run carries. `flow/turn`: the `Runtime` interface, with `turn/native` (saige) and `turn/sandbox` (a sandbox around any Runtime).
  - `flow/runtime`: durable runs on `durable`; `internal/platform/platform.go` composes it with `runtime.New(durable.New(ledger, queue), resolver, bus, ...)`. `flow/membership`: channel membership. `flow/connectors`: Slack, GitHub and cron adapters.
  - `durable`: the run ledger, queue, worker and replay. `sandbox`: isolated instances, workspaces, NGAC, the egress gate.
- `internal/runschema`: the `fiber_*` tables the run ledger and queue are stored in. `internal/toolprovider`: the record of the MCP servers a tenant may reach and the tools an admin accepted from each. `internal/mcptools`: those accepted tools as callables for a run. `pkg/dense`: the embedders.
- `pkg/agents`: each tenant's agent definitions on `pkg/state`, a `definition.Source`. `internal/legacy` moves the old registry into it once.
- `internal/wire`: the one seam onto flow's run model: a trigger's request and context, connector meta keys, event types and bodies, and agent definitions (`AgentCatalog`). Nothing else touches them; `TestSeam` checks.
- `internal/platform`: the composition root. `pkg/leader`: the one process per database that runs the singleton loops (`internal/platform/process_role.go`: process roles and leadership). `internal/httpapi`: routes, sessions, event stream.
- `pkg/spec`: the configuration language, System and Platform documents (docs/reference/spec.md). `internal/app`: the command tree (`start`, `serve`, `worker`, `deploy`, `setup`, `config`, `slack manifest`, `authz`) and the composition root, which turns a spec and flags into a running platform. The module root package `humagen` (`humagen.go`): its public face, `humagen.Main` and the options a downstream program passes instead of forking. `cmd/humagen`: `humagen.Main` with no options. `pkg/credref`: credential references (`env:`, `keychain:`, `op://`), read when used and never stored. `pkg/credentials`: the credential each person gives for a person-scoped tool provider: a reference, or a value kept in the secrets store client secrets are kept in.
- `cmd/humagen`: the binary. `apps/web`: the console, built on the shared UI packages. `apps/slack`: the Slack app, a client of the API.
- `packages/adapter`: the one adapter between the platform's wire shapes and the shared UI model. The console and the Slack app both use it; nothing surface-specific goes in it.
- `api/openapi.yaml`: the API contract, checked by `internal/httpapi/openapi_test.go`. `packages/client`: the TypeScript client generated from it by oag.

## Commands

| Task | Command |
|------|---------|
| Set up | `make init` |
| Format, lint and test | `make check` |
| Test | `make test` |
| Preview | `make dev`, then `make web` |
| Preview with sign-in | `make dev-signin`, then `make web` |
| Develop with API, hot-reloading console and Postgres | `make tilt` (docs/guides/develop.md) |
| Preview on a local Kubernetes cluster | `tilt up` (docs/guides/deploy.md#local-kubernetes-with-tilt) |
| Web checks | `cd apps/web && pnpm typecheck && pnpm test` |
| Adapter checks | `cd packages/adapter && pnpm typecheck && pnpm test` |
| Slack app checks | `cd apps/slack && pnpm exec biome check && pnpm typecheck && pnpm test` |
| Regenerate the client | `make client` (oag version pinned in the `Makefile`) |
| Client checks | `make client-check` |
| Lint the spec | `make spec-lint` |

## Conventions

- Conventional commits. `sr.yaml` derives versions from them.
- Work in a worktree, never the primary checkout. Each change gets its own branch at `.worktrees/<name>`, started from a fresh `origin/main`: `git fetch origin && git worktree add -b <branch> .worktrees/<name> origin/main`. The primary checkout stays on `main`, clean and fast-forwarded with `git pull --ff-only`. If you find uncommitted changes there, stop and ask; do not build on them.
- Money is an `int64` of micro-USD. No floats.
- Tests run with `-race`.
- No em dashes in docs or comments.

## Console patterns

New console pages use these and never hand-roll them:

- **Cards are `ItemCard`** (`apps/web/src/cards.tsx`), in the `itemGrid` grid. The whole card is its link; there is no Open button. Slots: a leading mark, the title, one status at the top right (a Pill or a dot, never empty), a one-line description, a facts row, and a ⋯ menu.
- **References are `Reference`** (`apps/web/src/cards.tsx`): an origin, a source an agent pulled in, an item or backlink, a knowledge source, a search hit, a child session. A leading mark, a label, an optional count, and a link when it has a target.
- **Status is a `Pill` or a dot.** Pills are for state only, never for references.
- **Menus use the shared rows**: `MoreMenu`, or `menuRow` and `MenuSeparator` from `apps/web/src/ui.tsx` inside a `Popover`. Plain rows, an optional leading icon, destructive rows last after a separator.
- **The composer is `Composer`** (`apps/web/src/Composer.tsx`).
- **A page's header is `Page`** (`apps/web/src/ui.tsx`): one title, or else its tabs, never both and never a line of explanation under either. A list page is titled as the sidebar names it.
- **Tabs are one strip**: `Tabs` for a page's own tabs (passed to `Page` as `tabs`) and `WorkspaceTabs` for a hub's. Both draw `tabStrip` and `tabItem` from `apps/web/src/ui.tsx` at the top of the page's column, on a rule as wide as the column, with the open tab underlined in the accent. A page headed by its tabs puts its actions at the right of the tab row.
- **Loading is `SkeletonRows`** for a list's first read, and `LoadMore` (`apps/web/src/ui.tsx`) for the next page of a list already on view. No bare "Loading" text and no spinners in a page.
- **Relative times come from `apps/web/src/format.ts`**: `age` (short, "1d") and `ago` (words, "yesterday"), which count the same way, and `daysAgo` for a heading such as Yesterday. Do not round a time any other way.
- **Event streams open through `openStream`** (`apps/web/src/stream.ts`), never `new EventSource`: it gives the connection back while the tab is hidden and keeps every tab's streams within a shared limit, so streams cannot use up the browser's connections to the server. The one exception is `liveSource`, a stream that ends with its answer.
- **The way back to the end of a growing list is `LatestButton` with `useLatest`** (`packages/web/src/Latest.tsx`), as the thread and the Activity panel use it.

## Shared presentation

- `packages/core`: UI types, reducers, actions, queue, session, inbox and fixtures. No React.
- `packages/primitives`: the primitive contract implemented by every renderer.
- `packages/components`: shared components built only from primitives. No DOM, Block Kit or effects.
- `packages/render-web`, `packages/render-slack`: one renderer per surface.
- `packages/web`: web-only components. `packages/slack-host`: the generic Slack host loop.
- `internal/presentation/github`: pure GitHub Markdown rendering, without API calls.
- `apps/slack-sandbox`: the real Slack fixture bench.
- Copy lives in `packages/components/src/copy.ts`. Buttons use Title Case; other copy uses sentence case.
- Anything waiting on a person must not animate.
- Every Slack-facing change must pass `pnpm validate:slack`.
- Install once at the root with `pnpm install`; all internal dependencies use `workspace:*`.
