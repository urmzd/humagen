<p align="center">
  <h1 align="center">warp</h1>
  <p align="center">
    A shared workspace for humans and agents: create agents, hire them into projects, and coordinate governed work with shared knowledge.
    <br /><br />
    <a href="#installation">Install</a> &middot;
    <a href="https://github.com/urmzd/warp/issues">Report Bug</a> &middot;
    <a href="docs/architecture.md">Architecture</a> &middot;
    <a href="docs/deploy.md">Deploy</a>
  </p>
</p>

<p align="center">
  <a href="https://github.com/urmzd/warp/actions/workflows/ci.yml"><img src="https://github.com/urmzd/warp/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/urmzd/warp" alt="License"></a>
</p>

<p align="center">
  <img src="showcase/agents.png" alt="Warp agent console with the two seeded offline example agents" width="80%">
</p>
<p align="center"><em>Local development preview with seeded example data, captured with teasr. No model calls.</em></p>

## Features

- Branch conversations across agents, with a shared call primitive for subagents, handoffs and delegations.
- Edit agents and revisions, run deterministic evals, schedule work and inspect run traces.
- Apply tenant permissions, depth and fan-out limits, run admission policies and usage accounting.
- Search knowledge, manage project memory and inspect alerts and analytics in the web console.
- Tag who answers (`@agent` or `@auto`) or let the default a project inherits answer, enable platform agents per project or give an agent a home in one, and browse the workspace's people, agents and projects in the Directory.
- Connect GitHub and Slack through signed webhook ingress and tenant-bound connector capabilities.
- Answer in Slack with the console's own components through a Socket Mode app that streams natively.

**Early development:** pre-1.0, one replica per database, sessions in memory, Slack and GitHub tested against simulated APIs. The full, current list is in [the ecosystem's limitations](docs/ecosystem.md#limitations-in-one-place); what each piece has been tested against is in the [status table](docs/architecture.md#status).

## Installation

### Prerequisites

| Requirement | Purpose |
|-------------|---------|
| Go version from [`go.mod`](go.mod) | Build the server |
| Node.js 22.12+ or 24+, pnpm | Build the Vite console |
| Chrome/Chromium and [teasr](https://github.com/urmzd/teasr) | Optional screenshot capture |

The platform, console, shared UI and Slack renderers live in this repository. The Go libraries are published separately.

```sh
git clone https://github.com/urmzd/warp.git
cd warp
go mod download
pnpm install
```

A workspace answers through one or more [clients](docs/clients.md): a Slack app
or a GitHub installation, each with its own identity, secrets, agents and limits.
See the [integration trial guide](docs/integrations.md).

## Quick Start

One command, nothing to configure:

```sh
pnpm --filter @warp/console build   # once, so the console is served too
go run ./cmd/warp start
```

`warp start` runs the whole platform in one process: development sign-in on
loopback, in-memory stores, files on disk, seeded example agents on the offline
provider, and the console at <http://127.0.0.1:8080>. On a terminal, the first
start asks a few questions (its name, model providers, MCP tools, sign-in,
Slack), each with a default Enter accepts, and writes the answers to
`warp.yaml` in your config directory. You name where each key is kept
(`env:NAME`, `keychain:SERVICE`, `op://VAULT/ITEM/FIELD`); warp reads it when it
is used and never stores it.

Everything is configured in that one language: a `System` document for how and
where warp runs (sign-in, stores, bus, files, limits) and a `Platform` document
per workspace (models, tools, agents, Slack, settings). See [the spec](docs/spec.md).
The same file runs on your own infrastructure with no code: point `stores` at
Postgres, `blobs` at S3 and `auth` at your identity provider, and use it with
compose or the Helm chart ([deploy](docs/deploy.md)). Any flag still wins over
the file (`warp start -addr 127.0.0.1:9090`).

### Develop the console

Run the backend in one terminal:

```sh
make dev
```

Run the console in another terminal, from the repository root:

```sh
make web
```

Open <http://127.0.0.1:5173>, complete the first-run workspace form and select an example agent. The backend listens on `127.0.0.1:8080`. Seeded agents use fiber's offline provider, so the preview requires no model key.

To run it in containers, on Kubernetes with Helm, or with Flux, see [deploy](docs/deploy.md).

Development auth grants admin access on loopback. For real sign-in, roles and machine tokens, follow [the security guide](docs/security.md). To use Anthropic, set `ANTHROPIC_API_KEY` before starting the server and configure an agent with provider `anthropic`.

### Capture the console

With a fresh `make dev` process and `make web` running:

```sh
teasr run
```

[`teasr.toml`](teasr.toml) captures the console pages and writes images to [`showcase/`](showcase/). The console opens directly after sign-in. Personal preferences are optional in Profile and Appearance settings.

<p align="center">
  <img src="showcase/runs.png" alt="Seeded offline run history" width="30%"> &nbsp;
  <img src="showcase/analytics.png" alt="Analytics for the seeded offline runs" width="30%">
</p>
<p align="center"><em>Runs &middot; Analytics</em></p>

## Conversations

`POST /api/runs` takes `agent`, `text`, and optionally `after` and `project`.

- `agent` names who answers: an agent available where the conversation is
  filed, or `auto` for `@auto`, which picks among those agents by name,
  description and prompt and hands a message nothing matches to the nearest
  default that names an agent.
- Leave `agent` out and the default the conversation's place inherits answers:
  its project's, then each ancestor's, then the workspace's. A new workspace's
  default is `@auto`. With none set, nothing runs: the answer is 409, asking
  to tag an agent.
- `after` names the turn the message follows. Naming a turn that already has a
  reply starts a branch. Any agent can answer any turn, and is sent the branch
  so far.

## API

The contract is [`api/openapi.yaml`](api/openapi.yaml): every route, its
permission, request and response, and the event stream. Tests fail when it and
the server disagree. [`packages/client`](packages/client) is a TypeScript client
generated from it. See [the API guide](docs/api.md).

| Route | Permission |
|-------|------------|
| `GET /api/me` | Signed in |
| `GET`, `POST /api/agents`, `PUT /api/agents/{name}`, `GET /api/agents/{name}/history` | `agents:read`, `agents:write` |
| `GET`, `POST /api/runs`, `GET /api/runs/{id}`, `/tree`, `/trace` | `runs:read`, `runs:write` |
| `GET /api/threads`, `GET /api/threads/{id}` | `runs:read` |
| `GET /api/pick?q=&project=`, `GET`, `PUT /api/settings`, `PUT /api/agents/{name}/home` | `agents:read`, `agents:write` |
| `GET`, `PUT /api/projects/{name}/agents` | `projects:read`, `projects:write` |
| `GET /api/people`, `GET /api/people/{handle}/photo` | `people:read` |
| `GET`, `POST /api/evals`, `PUT`, `DELETE /api/evals/{name}`, `POST .../run`, `GET .../runs` | `evals:read`, `evals:write` |
| `GET`, `PUT /api/preferences`, `GET`, `PUT`, `DELETE /api/preferences/photo` | Signed in, the caller's own |
| `POST /api/runs/{id}/calls` | `runs:write` |
| `GET /api/events?topic=agent[:name[:run]]` | `runs:read`, server-sent events |
| `GET /api/analytics?window=1h` | `runs:read` |
| `GET /api/journal?agent=&run=&kind=&origin=&q=&window=` | `runs:read` |
| `GET`, `POST /api/schedules`, `DELETE /api/schedules/{name}`, `POST .../pause`, `.../resume`, `.../run` | `schedules:read`, `schedules:write` |
| `GET /api/alerts`, `POST /api/alerts/{id}/ack`, `GET`, `POST`, `DELETE /api/alert-rules` | `alerts:read`, `alerts:write` |
| `GET`, `POST /api/knowledge`, `.../{name}/documents`, `GET /api/knowledge/search?q=` | `knowledge:read`, `knowledge:write` |

## Development

| Task | Command |
|------|---------|
| Set up hooks and modules | `make init` |
| Format, lint and test | `make check` |
| Test with race detection | `make test` |
| Web typecheck and tests | `cd apps/web && pnpm typecheck && pnpm test` |
| Shared adapter typecheck and tests | `cd packages/adapter && pnpm typecheck && pnpm test` |
| Slack app lint, typecheck and tests | `cd apps/slack && pnpm exec biome check && pnpm typecheck && pnpm test` |
| Regenerate the TypeScript client from the spec | `make client` |
| Client typecheck and tests | `make client-check` |
| Lint the API spec | `make spec-lint` |

## Product direction

Warp is a shared place for people and agents to organize projects and tasks, maintain a living knowledge base, and improve how they work together. Agents can be created and hired into scoped project roles. Policies govern capabilities, access, budgets and approvals; connectors bring work in and send updates out. People participate through assignments, questions, approvals and notifications. Shared abilities and agent instructions improve through evaluated, reviewed changes.

This is the direction, not a claim that every workflow exists today. See the [roadmap](docs/roadmap.md) and [implementation status](docs/architecture.md#status).

## Architecture

warp is the top of five layers (warp, fiber, saige, playscape, anchor), each
building on the one below. [The ecosystem](docs/ecosystem.md) says who owns
what and the order the remaining duplication is being removed in.

| Part | Responsibility |
|------|----------------|
| **Warp** | Shared workspace, project orchestration, API, console and presentation |
| [Saige](https://github.com/urmzd/saige) | Agent execution, tools, retrieval and evaluations |
| [Fiber](https://github.com/urmzd/fiber) | Messaging, connectors and request routing |
| [Playscape](https://github.com/urmzd/playscape) | Governed execution environments and workspace isolation |
| [Anchor](https://github.com/urmzd/anchor) | Durable execution, replay and recovery |

The `@warp/*` packages supply web and Slack presentation; `internal/presentation/github` renders GitHub Markdown. They live with the platform in this repository.


An agent-to-agent call creates a run through `pkg/call`; its edge determines access, budget, depth and delegation mode. The platform owns identity, storage and orchestration; warp owns rendering. Read the [architecture and implementation status](docs/architecture.md) before relying on planned persistence or governance features.

## License

[Apache-2.0](LICENSE)
