# AGENTS.md

## What this is

A Go module that composes fiber, saige, playscape and anchor into one agent
platform and its web and Slack presentation in one repository. The design is in
[docs/architecture.md](docs/architecture.md).

## Rules

- **Every choice is a file or an interface, never a fork.** A new backend is a kind in a spec slot (`pkg/spec`, `warp.WithKind`); a new setting is a spec field with a default, documented in docs/spec.md, whose examples are tested.

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
- **One title per screen, on the page.** There is no top bar. Every page uses `Page` from `apps/web/src/ui.tsx`. What is running shows in the sidebar's Tasks, and a task holds the history of every run in it.
- **Every place has an address.** Pages, tabs and selections are paths in `apps/web/src/route.ts`. A new page or tab adds its route there, with a test.
- **Agents are told apart by color, not by letters in boxes.** Use `AgentMark`. Controls under the composer are text, not boxes.
- **The API has one contract.** A route, a handler's answer or a type's JSON
  fields change together with `api/openapi.yaml`, then `make client`. Never edit
  `packages/client/src/generated`.
- **Keep the status table honest.** When a piece lands, update
  `docs/architecture.md#status` in the same change.

## Layout

- `pkg/call`: the call primitive. `pkg/runs`: the run index and statistics. `pkg/auth`: principals, roles, OIDC. `pkg/authz`: the policy language. `pkg/auth/mockidp`: the loopback development provider.
- `pkg/journal`: the event log, queries and traces. `pkg/alerts`: rules and alerts. `pkg/knowledge`: sources and search.
- `pkg/memory`: what an agent carries between conversations. `internal/platform/hooks.go`: housekeeping runs.
- `pkg/clients`: the Slack apps and GitHub installations a workspace answers through. `internal/platform/connectors.go`: one surface per scheme, fanning in and out to them.
- `pkg/evals`: suites and runs, scored by saige. `pkg/pick`: who answers a message. `pkg/prefs`: a person's own settings.
- `internal/platform`: the composition root. `internal/httpapi`: routes, sessions, event stream.
- `pkg/spec`: the configuration language, System and Platform documents (docs/spec.md). `internal/app`: the command tree (`start`, `serve`, `setup`, `config`, `slack manifest`) and the composition root, which turns a spec and flags into a running platform. The module root package `warp` (`warp.go`): its public face, `warp.Main` and the options a downstream program passes instead of forking. `cmd/warp`: `warp.Main` with no options. `pkg/credref`: credential references (`env:`, `keychain:`, `op://`), read when used and never stored.
- `cmd/warp`: the binary. `apps/web`: the console, built on the shared UI packages. `apps/slack`: the Slack app, a client of the API.
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
