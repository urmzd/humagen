# Prompt layers and evaluations

Each admitted run pins shared instructions before execution. The model receives
platform, workspace, project ancestry and agent instructions as a stable system
prefix. Conversation messages and changing notes follow separately. Shared
layers have content-addressed versions, a 16,000-character per-layer bound and
64,000-character aggregate bound. The project Context preview uses the same
composition and reports each version. Retries use the accepted snapshot.
Layer kinds and origins are bounded to 256 Unicode characters. Encoded snapshots
are capped at 400,384 bytes, including JSON escaping and metadata; every accepted pin
can be read back under that same bound. Empty workspace origins remain valid.

A platform file can set `spec.workspace.prompts`:

```yaml
prompts:
  workspace: "Cite the source of factual claims."
  slack_word_budget: 120
```

An empty `platform` value selects the shipped platform instructions. Shared
instruction changes sample up to five cases from every saved agent suite before
saving. Agent changes run all their suite cases. Missing executors, interrupted
cases and incomplete recorded tool calls fail the gate. Checks are deterministic.

The builder exposes `preview_agent_change` and `apply_agent_change`. Preview
retains the proposed definition, before/after scores and a prompt diff. Applying
requires approval. A measured regression additionally requires an explicit reason;
a case that did not finish cannot be overridden. Changes to the base definition,
shared prompt or suite invalidate an older card. The builder can also pause,
resume and retire an agent, and write/run suites.
Connecting alone grants nothing to an agent. A builder change may explicitly
include `person_tools`; its card and approval disclose that grant and the same
eval gate applies. Each initiating person's connection still needs its own
authorization, approved tool list and current host policy. Leaving it out
removes the grant. New agents start without it. Package agents such as the writer
remain customizable; the builder and analyst stay protected, and no built-in
agent can be paused or retired through the builder.

Agent preview cards also carry current connection and listening summaries when
the requesting person can read client configuration in their own private
builder session. The summaries expose no credential values, references or raw
channel IDs. They describe current routing and explicit inbound bindings, never
new grants or live connection health. Hidden project placement is omitted, not
shown as workspace placement; omitted lists mean unknown, while empty lists
mean none. The preview does not change connections or subscriptions.

Shared instructions use `preview_workspace_prompt` and `apply_workspace_prompt`.
The card samples every agent suite and pins the complete suite hashes, agent
versions, project prefixes and previous shared configuration. A different
administrator must approve application; self-approval is refused. Measured
regressions require an explicit reason retained with the card. Incomplete evals
cannot be overridden. Applying changes only the prompt settings and preserves
unrelated settings updated while the review was pending.

`add_recorded_eval_case` copies a shared source run's input and tool outcomes only
after visibility checks. Private-context runs cannot become shared replay cases.
Replayed tools consume recorded results for matching arguments once and never
execute live tools. A new call or changed arguments fails the evaluation. Tool
groups need explicit recorded tool names; missing or truncated traces fail closed.
Indexed retrieval retains the exact assembled prompt and citation provenance
before model input, including an explicit empty result. Recorded cases carry a
digest-bound copy; replay checks its original input and selected sources and
never reads a live retriever. Missing historical snapshots fail closed. Retained
provider excerpts without current per-file authority cannot become shared cases.
Public suite responses omit retrieval bytes and source bindings; editing the same
case preserves its stored snapshot, while a changed input must be captured again.
Suite and result access still require knowledge and original source visibility.
Each new eval run retains its own replay-source access dependencies, atomically
with admission. Removing or replacing a case never changes those dependencies.
HTTP history and builder history recheck current source and original-run access;
inaccessible results are omitted. Historical rows from before provenance was
retained are also omitted because a current suite cannot establish their origin.
New plain runs retain an explicit empty dependency set and remain readable.
Builder responses also enforce the conversation's audience: a person's access to
a private recorded run does not authorize publishing its tool bytes in a shared
builder response, whether through a current case or historical result.
A suite's `every_seconds` enables
leader-driven runs, from one hour to thirty days; zero disables the schedule.
Recorded replay execution checks the initiating person's current configured
policy and original run/source access. A schedule acts for the person who last
saved the suite, not for a workspace or system identity. Missing person
attribution or current grants blocks replay before admission. Each case checks
again before model execution and before retaining its answer, so revocation
stops later cases and withholds output produced during revocation. Cached
sign-in roles do not grant unattended access. Plain manual cases are unchanged.

Completed ordinary runs receive online assessments. Slack checks count words
(default 120, except an explicit request for detail) and reject opening filler.
Run completion and tool failures are checked on every surface. Recognized
review output is also checked deterministically against its declared
format, within 64 KiB. An immutable agent revision explicitly naming a supported
review format requires that same format; ordinary prose then fails. A recognized
format declared by an answer is checked even without an instruction. Unknown
formats are unassessed. These checks validate structure, not live diff
applicability or evidence authorization, which remain delivery checks. Optional
`spec.workspace.eval_limits` fields `max_spend` (micro-USD), `max_duration_ms` and
`require_evidence` add cost, execution-time and linked-evidence checks. Waiting
for a person is excluded from execution time. Zero cost/time limits are disabled.
Health shows only the caller's readable runs. Failed checks on completed runs
raise an alert in a builder session; failed runs keep their normal failure alert.

Setup validation and evaluation runs remain separately marked and do not enter
ordinary conversation history, housekeeping or failure notifications. A
configured agent without a setup wizard has a ready read projection; that
projection grants no setup mutation rights. Feedback marked down with a reason
becomes a candidate input. Promoting it requires explicit expected checks.
