# AGENTS.md

## What this is

A Go module that composes fiber, saige, playscape and anchor into one agent
platform and its web and Slack presentation in one repository. The design is in
[docs/architecture.md](docs/architecture.md). Current console design requirements
and their acceptance criteria are tracked in [design.md](design.md). Read it
before changing presentation or integrations, and update its requirement status
and evidence when implementing a requirement.

## Rules

- **Every choice is a file or an interface, never a fork.** A new backend is a kind in a spec slot (`pkg/spec`, `humagen.WithKind`); a new setting is a spec field with a default, documented in docs/spec.md, whose examples are tested.

- **Compose, don't reimplement.** If fiber, saige, playscape or anchor owns
  a concern, use it or change it upstream.
- **One way to call an agent.** Subagents, handoffs and delegations all go
  through `pkg/call`. Do not add a second path.
- **Default deny.** A zero value grants nothing: no calls, no budget, no access.
- **Fail closed.** A missing or malformed edge on an agent's tag is an error.
- **Keep presentation separate.** Shared UI packages render data and emit actions.
  API calls, persistence and identity belong in the platform and surface hosts.
- **Tenant comes from the Principal.** Never read it from a request, and return 404 for another tenant's objects.
- **Every route checks one permission.** Add it in `Server.Handler`, next to the route.
- **The ledger decides whether a run is over.** Events report attempts. Do not infer an ending from silence.
- **Bound every query.** A window, a row cap and a bucket cap, enforced on the server.
- **Evals are deterministic.** A check is a pure function of the output. Do not add a model as a judge without saying so in the suite.
- **One title per screen, on the page.** There is no top bar. Every page uses `Page` from `apps/web/src/ui.tsx`. What is running shows in the sidebar's Conversations, and a conversation holds the history of every run in it.
- **Every place has an address.** Pages, tabs and selections are paths in `apps/web/src/route.ts`. A new page or tab adds its route there, with a test.
- **Agents are told apart by color, not by letters in boxes.** Use `AgentMark`. Controls under the composer are text, not boxes.
- **The API has one contract.** A route, a handler's answer or a type's JSON
  fields change together with `api/openapi.yaml`, then `make client`. Never edit
  `packages/client/src/generated`.
- **Old names live in one place.** The product was called warp. A name from
  then is read only through `internal/legacy` (Go) or `apps/*/src/legacy.ts`,
  and a name stored data depends on is kept and pinned by a test.
  `TestNoNewOldName` fails on any other mention; docs/upgrading-from-warp.md
  lists both kinds.
- **Keep the status table honest.** When a piece lands, update
  `docs/architecture.md#status` in the same change.

## Layout

- `internal/legacy`: every name from before the rename to Humagen that is still read, the names kept for stored data, and the notes printed at start. `pkg/call`: the call primitive. `pkg/runs`: the run index and statistics. `pkg/auth`: principals, roles, OIDC. `pkg/authz`: the policy language. `pkg/auth/mockidp`: the loopback development provider.
- `pkg/journal`: the event log, queries and traces. `pkg/alerts`: rules and alerts. `pkg/knowledge`: sources and search.
- `pkg/memory`: what an agent carries between conversations. `internal/platform/hooks.go`: housekeeping runs.
- `pkg/clients`: the Slack apps and GitHub installations a workspace answers through. `internal/platform/connectors.go`: one surface per scheme, fanning in and out to them.
- `pkg/evals`: suites and runs, scored by saige. `pkg/join`: whether each agent takes a message that names none (the dynamic router). `pkg/prefs`: a person's own settings.
- `pkg/state`: the record store every Humagen store writes through, in memory or in Postgres, with the `statetest` suite. A store keeps its logic in memory and gains `Open(state.Store)`; `internal/platform/backends.go` chooses the backends.
- `internal/platform`: the composition root. `internal/httpapi`: routes, sessions, event stream.
- `pkg/spec`: the configuration language, System and Platform documents (docs/spec.md). `internal/app`: the command tree (`start`, `serve`, `deploy`, `setup`, `config`, `slack manifest`, `authz`) and the composition root, which turns a spec and flags into a running platform. The module root package `humagen` (`humagen.go`): its public face, `humagen.Main` and the options a downstream program passes instead of forking. `cmd/humagen`: `humagen.Main` with no options. `pkg/credref`: credential references (`env:`, `keychain:`, `op://`), read when used and never stored. `pkg/credentials`: the credential each person gives for a person-scoped tool provider: a reference, or a value kept in the secrets store client secrets are kept in.
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
- `apps/storybook`: component stories. `apps/slack-sandbox`: the real Slack fixture bench.
- Copy lives in `packages/components/src/copy.ts`. Buttons use Title Case; other copy uses sentence case.
- Anything waiting on a person must not animate.
- Every Slack-facing change must pass `pnpm validate:slack`.
- Install once at the root with `pnpm install`; all internal dependencies use `workspace:*`.
