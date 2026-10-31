# AGENTS.md

## What this is

A Go module that composes saige, playscape and anchor into one agent platform
and its web and Slack presentation in one repository. fiber, its transport,
lives in `fiber/` in the same module and is no longer an upstream dependency. The design is in
[docs/architecture/overview.md](docs/architecture/overview.md). Current console design requirements
and their acceptance criteria are tracked in [docs/reference/design-requirements.md](docs/reference/design-requirements.md). Read it
before changing presentation or integrations, and update its requirement status
and evidence when implementing a requirement.

## Rules

- **Every choice is a file or an interface, never a fork.** A new backend is a kind in a spec slot (`pkg/spec`, `humagen.WithKind`); a new setting is a spec field with a default, documented in docs/reference/spec.md, whose examples are tested.

- **Compose, don't reimplement.** If saige, playscape or anchor owns a
  concern, use it or change it upstream. fiber is in `fiber/`: change it here,
  where it reads an old name only through `internal/legacy`.
- **One way to call an agent.** Subagents, handoffs and delegations all go
  through `pkg/call`. Do not add a second path.
- **Default deny.** A zero value grants nothing: no calls, no budget, no access.
- **Fail closed.** A missing or malformed edge on an agent's tag is an error.
- **Every event type has a renderer and an OpenAPI mapping.** A type is
  registered once in fiber's catalog (`fiber/pkg/event`) with its body, its
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
- **One title per screen, on the page.** There is no top bar. Every page uses `Page` from `apps/web/src/ui.tsx`. What is running shows in the sidebar's Conversations, and a conversation holds the history of every run in it.
- **Every place has an address.** Pages, tabs and selections are paths in `apps/web/src/route.ts`. A new page or tab adds its route there, with a test.
- **An agent is its initials in its color, in a thin ring.** Use `AgentMark` (or `AgentInitials` in shared packages): the ring tells an agent from a person, the orb on its corner shows it working. Controls under the composer are text, not boxes.
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
- `pkg/journal`: the event log, queries and traces. `pkg/alerts`: rules and alerts. `pkg/knowledge`: sources and search. `pkg/catalog`: what exists in connected systems, metadata only, searched hybrid (RFC 0005); `pkg/catalog/bigquery` its first connector kind; `internal/platform/catalog.go` crawls, publishing and the `catalog_search` and `catalog_query` tools.
- `pkg/memory`: what an agent carries between conversations, as notes over `pkg/drive`. `pkg/drive`: versioned file trees (manifests on `pkg/state`, content by hash in `pkg/blob`), installed into and diffed from a run's files. `internal/platform/hooks.go`: housekeeping runs.
- `pkg/clients`: the Slack apps and GitHub installations a workspace answers through. `internal/platform/connectors.go`: one surface per scheme, fanning in and out to them.
- `pkg/evals`: suites and runs, scored by saige. `pkg/join`: whether each agent takes a message that names none (the dynamic router). `pkg/prefs`: a person's own settings.
- `pkg/state`: the record store every Humagen store writes through, in memory or in Postgres, with the `statetest` suite. A store keeps its logic in memory and gains `Open(state.Store)`, or reads and writes per request (`Lookup`, `List`, versioned ops) with the tenant as its shard key (docs/architecture/overview.md#storage); `internal/platform/backends.go` chooses the backends.
- `fiber/`: fiber, folded into this module and imported as `github.com/urmzd/humagen/fiber/...`. Its own guide is `fiber/AGENTS.md`.
  - `fiber/pkg/event`: the envelope, the sealed bodies and the catalog. `fiber/pkg/bus`: the bus, by channel and type prefix. `fiber/pkg/egress`: delivery of a run's events to its channel's sink.
  - `fiber/pkg/definition`: saige definitions plus a policy, and the pin a run carries. `fiber/pkg/turn`: the `Runtime` interface, with `turn/native` (saige) and `turn/sandbox` (playscape around any Runtime).
  - `fiber/pkg/runtime` and `fiber/pkg/durable`: durable runs on anchor. `fiber/pkg/membership`, `fiber/pkg/dispatch`: channel membership and dispatch. `fiber/pkg/connectors`: Slack, GitHub, Jira, cron and webhook adapters.
- `pkg/agents`: each tenant's agent definitions on `pkg/state`, a `definition.Source`. `internal/legacy` moves the old registry into it once.
- `internal/wire`: the one seam onto fiber's run model: a trigger's request and context, connector meta keys, event types and bodies, and agent definitions (`AgentCatalog`). Nothing else touches them; `TestSeam` checks.
- `internal/platform`: the composition root. `internal/httpapi`: routes, sessions, event stream.
- `pkg/spec`: the configuration language, System and Platform documents (docs/reference/spec.md). `internal/app`: the command tree (`start`, `serve`, `deploy`, `setup`, `config`, `slack manifest`, `authz`) and the composition root, which turns a spec and flags into a running platform. The module root package `humagen` (`humagen.go`): its public face, `humagen.Main` and the options a downstream program passes instead of forking. `cmd/humagen`: `humagen.Main` with no options. `pkg/credref`: credential references (`env:`, `keychain:`, `op://`), read when used and never stored. `pkg/credentials`: the credential each person gives for a person-scoped tool provider: a reference, or a value kept in the secrets store client secrets are kept in.
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
- Money is an `int64` of micro-USD. No floats.
- Tests run with `-race`.
- No em dashes in docs or comments.

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
