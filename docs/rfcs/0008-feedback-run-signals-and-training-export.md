# Feedback, run signals and training export

Status: proposed. This document changes no behavior. It describes the target
design and the order of the changes that reach it.

## Goal

People rate runs, and those ratings make agents better. A rating is asked for
when it is cheap to give and useful to have. A rated run can become an
improvement proposal or an eval case, after a person reviews it. Every run
carries a small set of signals (cost, latency, tool errors, retries, rating),
so a workspace can see what good runs have in common. An owner can export
rated runs as training data, with the raters' consent and with private text
removed.

## Current boundary

- **Ratings.** `pkg/feedback` keeps one record per person per run: `up` or
  `down`, a short reason and a comment. It reads and writes per request, so
  any number of processes agree. The console and Slack open the same modal.
  `GET /api/runs/{id}/feedback` pages a run's ratings and never shows a
  subject.
- **When people are asked.** Never. The like and dislike buttons sit under
  every finished answer and wait. Most runs end unrated, and the ones that are
  rated lean to the extremes.
- **What a run reports.** The run index holds status, spend, usage and
  duration (`pkg/runs`). Tool calls and their errors are events in the journal
  (`run.delta.tool.done.v1`). Attempts are on the lifecycle events. Nothing
  joins them with the ratings, so a question like "do runs with tool errors
  get rated down" has no answer.
- **Evals.** `pkg/evals` suites are written by hand. Checks are deterministic.
  Nothing turns a run people disliked into a case.
- **Improvement.** RFC 0003 describes a human-reviewed improvement loop, and
  outcomes already reach memory only through a reviewed learning write
  (`runs.GuardLearning`). Ratings do not feed either.
- **Export.** None. Run text leaves Humagen only through the API, one run at
  a time.

## Decision

Three rules hold across the whole design:

| Rule | Meaning |
|------|---------|
| **Signals are derived** | A run's signals are a pure fold of its events and its ratings. They are rebuildable, never the record of truth, and never decide whether a run is over: the ledger does. |
| **A person promotes** | No rating changes an agent, a suite or a dataset on its own. A rated run becomes a proposal, and a person accepts it. |
| **Consent is per rater, per use** | A rating given in the product is not consent to train. Export takes only runs whose requester and rater agreed to that use, and removes what the redaction rules name. |

## Asking at the right moment

A prompt for a rating is a small card under the answer, in the console and in
Slack. It is asked, never required, and it never animates.

- **When.** After a run completes and the person has read it: the answer was
  shown for a few seconds, or they replied, copied it or opened its files. A
  run that failed or was stopped is not prompted; its signals already say how
  it went.
- **Which runs.** A per-workspace sample rate (`feedback.ask.rate`, default
  `0`, so nothing changes until an owner turns it on). Some runs are always
  asked: the first runs of a new agent revision, runs whose signals are
  unusual (a tool error, a retry, cost well above the agent's median), and
  runs in a suite's shadow set (below).
- **How often.** At most once per person per conversation per hour, and at
  most `feedback.ask.daily` times a day (default 3). A person can turn it off
  in their own settings (`pkg/prefs`). An ignored prompt is not asked again.
- **What it asks.** Up or down, then one reason from a short list the
  workspace sets (wrong, incomplete, slow, unsafe, other), then an optional
  comment. This is the existing record; only the moment is new.

Being asked is recorded, so the share of asked runs that were rated is known,
and rated runs can be weighted against the ones never asked.

## From ratings to proposals and cases

A down rating with a reason, or a run several people rated, opens a
**proposal** in the agent's review queue. A proposal never edits anything.

| Proposal | What a reviewer accepts | Where it goes |
|----------|-------------------------|---------------|
| **Eval case** | The run's input, and checks drafted from the rating (for "wrong": a `contains` of the reviewer's expected answer; for "slow": a `within_ms`). The reviewer edits the checks. | A case in an existing `pkg/evals` suite, or a new suite |
| **Instruction change** | A diff to the agent's instructions or skills, drafted by a bounded repair run as RFC 0003 describes | A new agent revision, published only by a person, with its evals run against the old one |
| **Memory note** | A note from the outcome, through the reviewed learning write that exists today | The agent's memory |

Checks stay deterministic. A drafted case is a pure function of what the
reviewer approved. If a workspace wants a model to draft checks, the suite
says so, as AGENTS.md requires.

A run's text in a proposal is shown only to people who could already read the
run. Accepting a case copies only the input and the checks, never the
answer, so a suite does not hold private output.

## Run signals

A **signals record** is one row per run:

| Field | From |
|-------|------|
| `spend`, `model_calls`, `tokens`, `unpriced_calls` | The runtime's final usage, or the per-call usage events until it arrives |
| `latency_ms` | From the first event to the last ending event |
| `waiting_ms` | Time parked on a person, so a slow approver is not a slow agent |
| `tool_calls`, `tool_errors` | `run.delta.tool.done.v1`, an error being a non-empty `error` |
| `attempts`, `retries` | The highest attempt on a lifecycle event, and that less one |
| `up`, `down`, `rating` | The run's ratings: `up`, `down`, `mixed` or none |

It is computed by a fold over the run's events and its feedback records. It
holds no text. It is kept per run beside the run index and recomputed when a
rating changes or when the run ends. Being derived, it can be rebuilt from
the journal and `pkg/feedback` at any time.

### Reward analytics

Run analytics (`GET /api/analytics`) gains, per agent and per revision:

- the rated share, the up share, and the up share among asked runs only;
- median and p95 of spend and latency, split by rating;
- tool error and retry rates, split by rating;
- the reasons given, counted.

The Monitor overview shows them next to the existing outcome charts. Every query
is bounded by a window, a row cap and a bucket cap, like the summary today.
Revisions are compared only when both have enough rated runs; below that the
page says the difference is not yet known instead of drawing it.

## Training export

An owner exports rated runs as a dataset file for preference training. Two
formats:

| Format | One line is | Built from |
|--------|-------------|------------|
| **KTO** | `{prompt, completion, label}`, label `true` for up and `false` for down | Every rated run with a clear rating |
| **DPO** | `{prompt, chosen, rejected}` | Two runs that answer the same point in a conversation (branches, same `prev`) where one was rated up and the other down |

`prompt` is the conversation up to the run, in the chat message shape of the
agent's model. Tool calls and their results are included as messages when the
owner asks for them, and left out by default.

### Consent

- A workspace setting turns export on. It is off by default.
- Each person has a training consent in their own settings, off by default,
  and it can be withdrawn. A run is exported only when its requester and every
  rater counted for it have consent on at export time.
- A run in a private conversation, a run that used private context
  (`Run.UsesPrivateContext`), or a run that read external retained text is
  never exported.
- Each export is recorded with who ran it, when, the filters and the count of
  runs, in the audit log. The file itself is not kept.

### Redaction

Before a line is written, its text passes the workspace's redaction rules:

- built in: email addresses, phone numbers, credential-shaped strings
  (provider key prefixes, JWTs, long hex and base64 runs), and every handle and
  display name in the tenant's directory, replaced with stable placeholders
  per line;
- workspace rules: literal terms and regular expressions;
- tool results are dropped whole unless the tool is on an allow list.

A line that still matches a credential pattern after redaction is dropped,
and the export reports how many were. Redaction is deterministic and tested
with fixtures, so the same run always exports the same line.

## Alternatives

- **Thumbs only, as today.** Rejected: unprompted ratings are few and skewed,
  and with nothing joined to them they cannot answer why a run went badly.
- **A model as the judge of every run.** Rejected as the default: it costs a
  call per run, it drifts, and AGENTS.md keeps evals deterministic. A
  workspace can still add a judged suite, labeled as one.
- **Store signals on the run record.** Rejected: the run index is still loaded
  whole at start, and ratings are already per request. A derived record keeps
  the run index from growing and can move to per-request storage on its own.
- **Export raw journals.** Rejected: it would carry text nobody consented to
  share, and every consumer would redo the pairing and redaction.

## Changes

1. **Signals record.** `pkg/signals`: the record and a pure fold over a run's
   events and its feedback records, with tests. No wiring.
2. **Signals store and summary.** Keep a record per run, recompute it on run
   end and on rating, add the reward fields to `GET /api/analytics` and
   `api/openapi.yaml`.
3. **Asking.** The sample rate and caps in the spec, the person's opt out, the
   prompt card in the console and in Slack, and the asked record.
4. **Proposals.** The review queue, eval case drafts into `pkg/evals`, and the
   link to the existing learning write.
5. **Consent and redaction.** The workspace setting, the person's consent, the
   redaction rules and their fixtures.
6. **Export.** KTO and DPO export through the API and the console, recorded in
   the audit log.
7. **Monitor.** The reward analytics on the Monitor overview.

Each change is its own PR and keeps the previous behavior when its settings
are left out.

## Open questions

- Whether a rater's consent should be asked once in settings or at the moment
  of rating, and whether a requester's consent should cover runs others rated.
- Whether DPO pairs should also be built from an edited answer (the person's
  correction as `chosen`), which needs a way to record corrections.
- How many rated runs a revision needs before Monitor compares it,
  and whether that threshold is a setting.
- Whether the redaction rules belong to the workspace only or can also be set
  per project.
