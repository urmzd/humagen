# One entry point: the platform answers by its own name

Status: proposed.

## Goal

A person reaches every agent through one name, the platform's own: `@humagen`
here, `@codex` for a downstream product built on Humagen and called Codex. That
agent reads the ask, chooses the specialists, runs them, and answers once. The
same entry point works in the console, in Slack and on GitHub, and anyone can
still mention a specific agent to skip the routing.

## Context

- **Built:** `code-reviewer` delegates to `security-reviewer`,
  `architect-reviewer` and `qa-reviewer` through `pkg/call` and merges one
  verdict (v0.89.0, verified live on a real model). `pkg/join` chooses an agent
  for a message that names none. Every run has its own sandbox. Platform agents
  are customizable packages (`humagen/core`, `humagen/review`).
- **Built:** signed GitHub ingress at `hooks.platform.withhumagen.com`
  (infra #50, #57): unsigned requests are refused, console paths return 404.
  The GitHub App `humagen` has Contents, Pull requests, Checks and Issues read
  and write, Actions read.
- **Missing:** a single orchestrating agent that answers to the platform's
  name; a rule for messages that arrive while a run is working; progress shown
  inside GitHub; inline review comments; fix pull requests.

## Decision

### 1. The entry agent

- One platform agent per workspace answers to the workspace's name and to each
  client's bot login (`HUMAGEN_GITHUB_LOGIN`, the Slack app's user). It ships in
  a new package `humagen/core` alongside writer, analyst and builder, so a
  workspace may customize it (RFC: agent packages, humagen#218).
- Its name comes from the deployment's branding (`workspace_name`), not a
  hard-coded `humagen`, so a downstream product answers as itself.
- It is an orchestrator: it may spawn every agent available in the place the
  message arrived, and nothing else (default deny still holds per agent). It
  answers simple asks itself and delegates the rest through `pkg/call`.
- `pkg/join` becomes its first step rather than a separate path: one way to
  route.
- Mentioning a specific agent bypasses it.

### 2. Messages while a run works

No command words. Every message is a message, and the entry agent classifies it
against the running work:

| The new message | Result |
|---|---|
| Adds to the ask ("also check the migration") | Queued; handled as a follow-up in the same conversation when the run settles |
| Replaces the ask ("actually only review auth.go") | The running work stops with its context kept (saige's mid-turn stop with a reason) and the new ask starts |
| Stops it ("stop", "cancel", "never mind") | The run stops and reports where it reached |

The classification is a small, cheap model call with a deterministic fallback:
when unsure, queue. Queueing and stop already exist (the composer queue,
`RunStop`); the new piece is the classifier and its event.

**Verbs.** A verb as the first word after the mention skips the classifier and
is exact. Anything else is natural language.

| Verb | Effect |
|---|---|
| `@<entry> queue <message>` | Always queued as a follow-up |
| `@<entry> stop [reason]` | Stops now; the reason is recorded and shown |
| `@<entry> replace <message>` | Stops with context kept, then starts this ask |

### 2b. Channel modes

A channel (Slack, a GitHub thread where it applies) has one listening mode,
stored on the channel record, shown in the console, and changed by a verb or
in the console:

| Mode | Verb | Behavior |
|---|---|---|
| Mention (default) | `@<entry> mention` | Answers only when mentioned |
| Listen | `@<entry> listen` | Answers channel messages that need it without a mention; `pkg/join` decides which need it |
| Hush | `@<entry> hush` | Follows the channel and keeps its context but posts nothing until mentioned or replied to directly, then answers with what it heard |

Hush keeps a bounded rolling context per channel (a window and a size cap,
enforced on the server) and never starts a run on its own. Listen and hush are
default deny per channel: a workspace admin enables the modes for a client
before anyone can switch a channel to them.

### 3. GitHub

- **Triggers:** an `@<entry>` mention in any PR, issue or review thread; and an
  automatic review when a PR opens, leaves draft or receives a push, if the
  connection's setting is on. The setting lives on the GitHub connection in the
  console, can be overridden per repository, and defaults to on. No labels.
- **Progress:** one check run named after the entry agent ("Humagen") on the
  head commit, queued, in progress, then neutral with the verdict. Findings
  never fail the check. One comment per PR, edited in place, shows a row per
  agent working, then the verdict table and a link to the conversation. Edits
  only on state changes, at most one every 5 s, always flushing the last.
- **Findings:** one batched pull request review, never an approval, with
  inline comments on the diff lines and which specialist found each.
- **New pushes:** review only the commits since the last reviewed SHA; a newer
  push supersedes a running review; automatic reviews pause after 5 reviewed
  pushes on one PR until someone asks again.
- **Fixes:** "fix the SQL injection" routes to an agent that changes code in its
  sandbox, then asks a person to approve before pushing a `humagen/<slug>`
  branch from the default branch and opening a draft PR whose body links the
  source and who asked and approved. The entry agent reviews that PR like any
  other. It never marks a PR ready, approves or merges.
- **Guardrails:** a repository ruleset lets the App create and update only
  `humagen/**`; the default branch requires a PR. The server refuses any other
  ref and any `.github/workflows/**` change (the Workflows permission stays
  off).

### 4. Where it lands

| Piece | Repository | New or changed |
|---|---|---|
| Entry agent and its package | humagen `internal/platform/builtin*.go`, `agent_packages` | new agent; name from branding |
| Follow-up classifier and verbs | humagen `internal/platform/threads.go`, `pkg/join` | new event `run.followup` with its renderer and OpenAPI variant; verb parser |
| Channel modes | humagen `pkg/threads` / channel records, Slack connector, console channel settings | mode field (mention, listen, hush), bounded hush context |
| Auto-review setting | humagen `pkg/clients` (`GitHubOptions.AutoReview`), console Connected Accounts | replaces `HUMAGEN_GITHUB_AUTO_LABELS` |
| Check run name and progress rows | orchagent `flow/connectors/github` (`checks.go`), humagen `internal/presentation/github/progress.go` | check named for the entry agent; rows from delegation events |
| Sticky comment | orchagent `guide.go` generalized to `UpdateSticky` | per PR, not per run |
| Inline review | orchagent `api.go` `CreateReview`; humagen `ReviewFinding` gains `path`, `line`, `found_by` | new |
| Fix PRs | humagen `pkg/integrations/github/mutations.go`; orchagent `CreateFixBranch`, `OpenPullRequest` | new, approval-gated |

## Order

1. Entry agent with routing through `pkg/join`, the follow-up classifier and the verbs.
1b. Channel modes: mention, listen, hush.
2. GitHub progress: the check run and the edited-in-place comment.
3. Inline review comments.
4. Fix pull requests behind the approval gate and the rulesets.
5. The auto-review setting in the console, then remove `HUMAGEN_GITHUB_AUTO_LABELS`.

Each step ships on its own and is verified on platform.withhumagen.com.

## Risks

- GitHub secondary rate limits: edits count as writes; throttle as above.
- Comment edit races across replicas: take a lease per PR before editing.
- Size limits: 65,536 characters per comment and per check summary; truncate
  with a link to the conversation.
- Prompt injection through repository text steering a fix: contained by the
  approval gate and the ruleset.
- A misclassified follow-up: the fallback queues, which never loses work.
