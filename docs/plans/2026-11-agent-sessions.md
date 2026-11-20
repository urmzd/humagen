# Agent sessions implementation

Source: user-provided Humagen Agent Sessions plan dated 2026-11-07,
https://claude.ai/artifact/7hf38WZbyapQtuHG9Pp8xE.
The complete plan, including all 78 findings and the live validation matrix,
is preserved in [the source snapshot](2026-11-agent-sessions-source.txt).

## Scope and evidence

Completion requires the full source plan. Rows below are work in progress,
not claims of completion. Existing APIs remain compatible for one release.

| Requirement | Owner | Evidence needed | Current state |
| --- | --- | --- | --- |
| Session identity, continued/labeled/fresh children | orch_sessions | Memory/Postgres/restart and concurrency tests | Implementing |
| App.Session, public verbs, API cleanup | orch_sessions | Public API examples and race tests | Implementing |
| Five tools, durable listen, governed budgets/depth | orch_sessions + humagen_sessions | Replay, no occupied worker, denied calls/read tests | Implementing |
| One-agent session API, SSE resume, tree/links/people | humagen_sessions | Endpoint confinement, contract and reconnect tests | Implementing |
| Platform composition through flow.New | humagen_sessions | Admission, budgets and runtime regression suite | Implementing |
| Native delivery, reactions, stop and feedback | orch_surfaces | Connector unit tests plus live matrix | Implementing |
| OAuth discovery/DCR/PKCE/encryption/refresh | humagen_connections + orch_surfaces | Security regressions and live provider authorization | Implementing |
| Review v2, native PR reviews, suggestions, provenance | root + orch_surfaces | Parser, diff validation, renderer and native review tests | Implementing |
| Per-finding feedback | humagen_sessions + humagen_console | Separate vote persistence and finding validation | Implementing |
| Platform/workspace/project/agent prompt layers | workspace_instructions | Bounded composition, immutable retry pins and preview tests | Implementing |
| Online checks, builder changes and eval gates | workspace_instructions | Deterministic checks, regression rejection, approval tests | Implementing |
| Session alerts and tools | root | State/tenant/restart race tests, automatic lifecycle and UI | Domain lifecycle tests pass; integration pending |
| Seven-entry console, Details, shared rows | humagen_console | Route, accessibility, responsive and runtime checks | Implementing |
| Immediate fixes, all source findings C/T/N/S | humagen_console + connections + evals | Finding-by-finding regression verification | Implementing |
| Live Slack/GitHub/provider/builder/alerts matrix | root | Saved native live evidence for every row | Not run |

## Workspaces

- Humagen: `.worktrees/agent-sessions`, branch `feat/agent-sessions`, based on v0.90.0.
- orchagent: `.worktrees/agent-sessions`, branch `feat/agent-sessions`, based on v0.4.0.
- Local integration uses `/tmp/humagen-agent-sessions.work`; no absolute replace is committed.
- Go tests use `-race`, with writable cache `/tmp/humagen-agent-sessions-gocache`.

## Verification recorded so far

- `go test -race ./pkg/alerts`: passed, including concurrent processes, cross-tenant
  denial, legacy defaults, invalid snoozes, restart/expiry and resolution undo.
- `go test -race ./internal/presentation/github`: existing formatter tests passed.
  New v2-specific tests and full surface checks remain required.
- `pnpm install --frozen-lockfile`: completed in the Humagen worktree.
- Browser automation reported `The browser access host is unavailable` before
  implementation. No visual verification is claimed.
