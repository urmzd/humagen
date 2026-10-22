# The ecosystem

Five repositories, one platform. Each layer builds on the one below and never
redoes its work. Only the top one runs, and only the top one is configured.

```
warp        the platform: one process, API, console, Slack, the spec, deploy
  fiber     agents on a bus: specs and revisions, connectors, routing, the durable turn
    saige   the agent: model providers, the loop, tools, MCP, RAG, memory, evals
      playscape   where a turn runs: sandboxes, workspaces, NGAC access control
        anchor    durability: ledger, queue, worker, replay, Postgres plumbing
```

Rules every layer keeps:

1. **One owner per concept.** A concept is built once, in the lowest layer that
   needs it. A layer above wraps it, never copies it.
2. **Libraries are chosen in code.** Below warp a backend is an interface and
   its constructors. No layer below warp reads a file, an environment variable
   or a URL to decide what to build.
3. **Nothing below warp is a service.** No binary, no listener, no install
   script. A library may return an `http.Handler`; warp decides where it
   listens.
4. **Configuration lives in warp.** The spec ([spec.md](spec.md)) is the only
   configuration language; `warp start` runs it and `warp deploy` puts it
   somewhere.

## Who owns what

Today's state is from an audit of all five repositories (October 2026).
"Fate" is what the other copies become.

| Concept | Owner | Today, elsewhere | Fate |
|---------|-------|------------------|------|
| Durable runs: ledger, queue, worker, replay | anchor | saige `agent/durable/local` (1.5k lines, its own step log and locks) | delete saige's after a file ledger lands in anchor. Playscape's own queue and worker are gone (v0.7.0): it runs tasks on a bounded in-process pool |
| Postgres plumbing: advisory-locked migrations, LISTEN/NOTIFY | anchor (a `pgkit` package in its Postgres adapter module) | eight migrate functions (six in fiber, saige, warp `pkg/state`), three LISTEN hubs | all call `pgkit` |
| Sandboxes, workspaces, access control | playscape | saige `tools/exec` (its own subprocess and Docker code), saige `tools/fs` (a symlink check that can race), warp's file tools (bypass the agent's NGAC grant) | saige runs commands through a playscape executor; warp's file tools honour the grant |
| Model providers, fallback, presets | saige `agent/provider` | fiber `agentrt/provider.go` and warp `internal/app/providers.go` each build adapters | fiber exposes one endpoint built on saige's `provider.Build`; warp's model kinds call it |
| Agent definitions | saige `agent/definition` (Markdown and YAML, skills, subagents) | fiber `agentspec` (flat JSON, copied field by field into `agentrt.Spec`) | agentspec becomes the deployment envelope around a saige definition; warp loads `.md` agents |
| MCP client | saige `agent/mcp` (reconnect, keepalive, OAuth, SSRF guard) | fiber `mcptools` talks to the MCP SDK directly | fiber keeps the pinned-snapshot policy and builds on saige's client |
| SSRF dial guard | saige | three copies (saige `mcp`, saige `fetch`, fiber `attachment/intake`) | one exported guard |
| Retrieval: chunking, embedding, hybrid search | saige `rag` | fiber `pkg/knowledge` (its own chunker, ranker and stores); warp `pkg/knowledge` (its own BM25) | fiber's becomes the tenant-scoped adapter over saige; warp keeps sources, exclusions and variants and stores documents in fiber's |
| Memory | saige `agent/memory` | warp `pkg/memory` (notes injected as raw prompt text) | warp keeps its caps and approval queue on a saige store, injected the guarded way |
| Evals | saige `eval` | warp `pkg/evals` (its own checks and pass rate) | warp keeps suite editing and the runner; scoring, assertions and storage are saige's |
| Bus, connectors, routing, agent registry, threads, the durable turn | fiber | | |
| Files (blobs) | fiber `pkg/blob` | warp `pkg/blob` (adds S3) | S3 moves into fiber, as its own module so the AWS SDK stays out of the core; warp keeps only the choice |
| Agent-authoring tools | fiber `connectors/author` | warp's `list_agents`, `read_agent`, `save_agent` | warp's builder uses fiber's, with its grant filter |
| Observability | one OpenTelemetry seam | playscape `metrics.Recorder` (set to a no-op), saige's two OpenTelemetry shapes, anchor logs only | warp builds the providers and every layer takes the API |
| Configuration, identity, tenancy, console, Slack, deploy | warp | fiber's providers file and `FIBER_*` variables (being removed) | |

## What each one is for

Every repository below warp is a library first. Each may ship a CLI, and a CLI
is for validating that layer on its own: it runs a command, shows what
happened, and exits. Long-running servers, sign-in, configuration and
deployment are warp's.

| Repository | What it does | Who it serves | Its CLI validates |
|------------|--------------|---------------|-------------------|
| **anchor** | Durable execution: a ledger of runs and their records, a queue with leases and fencing, a worker that resumes after a crash, replay | fiber's durable turn and saige's durable agent; anything that must finish once despite restarts | a ledger: list runs, show one run's records and status, replay it (`anchor inspect`, to build) |
| **playscape** | Where work runs and what it may touch: sandboxes, workspaces, NGAC access control, local and Kubernetes runners | fiber, which runs each turn as a playscape task; saige's command and file tools | a command in the sandbox: `playscape run "go test ./..."` runs it under the same policy an agent gets and shows the exit code, output, denials and timeouts |
| **saige** | The agent: model providers and fallback, the loop, tools, MCP, retrieval, memory, evals | fiber, and anyone building an agent in Go | an agent end to end, and first class: chat, evals, retrieval, an MCP server for editors (`saige`, `saige-mcp`) |
| **fiber** | Agents on a bus: specs and revisions, connectors (Slack, GitHub, Jira, cron, webhooks), routing, the durable turn, threads, knowledge, tool providers | warp | an agent spec locally: run it against a message in one process and print its events (`fiber run`, to build) |
| **warp** | The platform: one process with the API, console, Slack app, identity, tenancy, policies, usage; the spec; `warp start` and `warp deploy` | people and their workspaces | is the product |

## Services that should be libraries

| Repository | Ships today | Becomes |
|------------|-------------|---------|
| fiber | nothing (the binary and server are gone) | a small `fiber run` CLI for local tests |
| playscape | since v0.7.0, a library with `playscape run` and an install script; the server, its HTTP API, its queue, the Dockerfile and the service's Kubernetes manifests are gone, and the k8s runner is its own module so importers don't inherit client-go | done |
| saige | `cmd/saige` (a TUI, an HTTP server, ACP, 11.7k lines), `cmd/saige-mcp`, install script, Dockerfile | stays: its CLI is first class. Its reusable hosting code moves into library packages that return `http.Handler`s, so warp can host the same thing |
| warp | `cmd/warp`, and the Slack app as a separate Node process | the one binary; the Slack app is started and stopped by `warp start` and talks only to warp's API |

## The order

Each step leaves every repository building and releasable.

| # | Step | Repos | Size | State |
|---|------|-------|------|-------|
| 1 | Fiber is a library: no binary, no server, no mandatum.v1; conformance suites per store family | fiber | L | in review on the branch |
| 2 | Fiber's backends are chosen in code; its declarative config moves to warp | fiber, warp | M | in progress |
| 3 | Warp's stores on memory or Postgres, chosen by the spec | warp | L | done |
| 4 | One spec, `warp start`, `warp deploy` (local, compose, Kubernetes) | warp | L | done |
| 5 | Retire the last legacy names | all | M | done in warp, saige, anchor, playscape; fiber in progress |
| 6 | Knowledge reaches agent runs (the stated blocker was out of date: fiber's retriever takes the tenant since v0.10) | warp | S | done |
| 7 | `pgkit` in anchor; `COLLATE "C"`; fiber's durable tables use anchor's DDL and the Postgres delta log | anchor, fiber | S | done in anchor v0.4.0 (`pgkit`, the ledger's prefix read, byte-ordered keys with `COLLATE "C"`); fiber's adoption to come |
| 8 | Playscape loses its service and its second queue | playscape | S–M | done, released in playscape v0.7.0: a library with `playscape run` |
| 9 | One SSRF guard, the MCP `$defs` inliner and the fallback names exported from saige | saige | S | done, released in saige v0.41.0: `netguard`, `mcp.InlineDefs` and the exported fallback conditions |
| 10 | Fiber builds providers through saige's `provider.Build`; warp's model kinds call it | fiber, warp | S–M | |
| 11 | S3 moves into fiber's blob store; warp's `pkg/blob` goes | fiber, warp | M | |
| 12 | Fiber's knowledge becomes an adapter over saige's retrieval (needs a stock-Postgres fallback in saige: its store today needs PostgreSQL 18 and the AGPL `pg_search`) | saige, fiber | M–L | |
| 13 | Warp's memory and evals on saige's | warp | M | |
| 14 | Fiber's MCP tools on saige's client | fiber | M | |
| 15 | Commands run through playscape executors; saige's Docker and process-group code goes | playscape, saige | M | |
| 16 | A file ledger in anchor; saige's `durable/local` goes | anchor, saige | M | |
| 17 | One agent definition: agentspec wraps a saige definition | saige, fiber, warp | L | |
| 18 | One OpenTelemetry seam, exporter chosen by warp | all | M | |
| 19 | saige's binaries become library packages | saige | L | owner's call on the developer CLI |

## Limitations, in one place

What is true today, across the stack. Each README links here instead of
keeping its own list.

- **Pre-1.0.** Every library's API may change between minor versions.
- **One writer per database.** Warp's stores keep their indexes in memory and
  write through to Postgres; a second process on the same database is refused.
  Run one replica.
- **Sessions are in memory.** A restart signs everyone out.
- **Spend limits** are enforced at admission and mid-run, but a model call in
  flight when a limit is crossed finishes and is counted.
- **Knowledge** is lexical only in warp (step 12 moves it onto saige's
  hybrid retrieval).
- **Client secrets**, and people's tool credentials where a workspace lets
  them store values, are stored in Postgres in plaintext; a reference's value
  is not stored at all. Prefer references.
- **Per-person credentials for tools** reach MCP tools only, through fiber's
  tool groups, which are told who a run acts for since fiber v0.22.0. The
  Slack and GitHub surfaces' own actions still use their client's token, a run another
  agent called acts for nobody, and a rotated secret behind an unchanged
  reference is not noticed by an open session.
- **Slack and GitHub** are tested against simulated APIs, not live.
- **The container image** is written but not published by releases.
- **Saige's Postgres retrieval store** needs PostgreSQL 18, pgvector and
  ParadeDB `pg_search` (AGPL).
- **Anchor's adapters** are young; durable timers are on its roadmap.
