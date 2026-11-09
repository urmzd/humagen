# Agent calls

A task has one answering agent. That agent brings in other agents with the
`agent` tool: `{agent, title, brief}`. The title is a few imperative words (at
most 60 characters) shown on the call's card; the brief is everything the agent
is told. Each call runs the agent as a child run of the caller that carries the
call's id (`call_id`, `htu_<run>_<n>`), so its work streams under the call, and
its answer is the call's result. The caller owns the user-facing answer.
Parallel agents are parallel calls.

`Access.Spawn` grants the tool: an agent that may call others has it without
listing it. Stopping the caller stops the child, the child's spend counts
toward the caller, and a call the callee's policy holds waits for a person. A
revision that still names `call_agent`, the tool's earlier form, is read as
granting the agent tool, and an approval it required is required of the agent
tool. A client of the humagen MCP makes no agent call of its own: it posts a
message (`send_messages`), which starts a run.

## Constructing input

A program that holds a run calls helpers with a constructed conversation and a
return format through `POST /api/runs/{id}/calls`. Helpers receive exactly that
conversation and return material for the caller to use. A helper's full result
stays available in execution history.

```json
{
  "agent": "researcher",
  "messages": [
    {"role": "user", "content": [{"type": "text", "text": "Example question"}]},
    {"role": "assistant", "content": [{"type": "text", "text": "Example summary"}]},
    {"role": "user", "content": [
      {"type": "conversation_range", "thread": "thread-id", "from": "first-run-id", "through": "last-run-id"},
      {"type": "artifact", "id": "artifact-id", "version": 2},
      {"type": "run_result", "run": "completed-run-id"},
      {"type": "text", "text": "Assess the approach. Focus on unresolved risks."}
    ]}
  ],
  "response_format": {"type": "summary", "max_words": 200},
  "mode": "subagent"
}
```

Messages are ordered. Context, examples and the request all use this one list.
Only `user` and `assistant` roles are accepted, and the list ends with a user
message. Supplied assistant messages are examples or selected history. They
cannot impersonate tool results or system instructions. No conversation,
source connector credentials, or parent memory is inherited implicitly.
The helper keeps its own pinned definition and its own access grants.

Content is a closed union. `text` carries inline text; `conversation_range`
selects inclusive run endpoints on one branch of the named conversation;
`artifact` requires an exact ID and version and supplies UTF-8 text;
`run_result` supplies a completed run's answer. Foreign-tenant references are
reported as missing. Unavailable, running, oversized or non-text references
fail the call before a helper starts. References currently follow the existing
workspace visibility model: there is no finer per-project or per-run read ACL.

Inputs allow at most 64 messages, 128 parts and 64,000 serialized bytes.
Resolved text has its own 64,000-byte bound. A conversation range contains at
most 64 turns. Nothing is silently truncated or summarized during resolution.

## Return contracts

```ts
type ResponseFormat =
  | { type: "text"; max_chars?: number }
  | { type: "summary"; max_words?: number }
  | { type: "json_schema"; schema: JsonSchema };
```

The discriminator selects both a host-generated instruction and validation.
Fields from another variant, unknown fields and unknown types are errors.
`text` defaults to 16,000 characters and permits at most 64,000. `summary`
defaults to 200 whitespace-separated words and permits at most 2,000. JSON
results must consist of one JSON value, with no surrounding prose or fences.
Schemas use saige's compiled JSON Schema validator; unsupported keywords fail
before execution instead of being ignored.

fiber's current provider boundary has a system channel, without a separate
developer channel. The host appends the bounded return contract to the helper's
system instructions, explicitly subject to its pinned rules. The caller cannot
supply arbitrary privileged instruction text. JSON schemas are identified as
data. Tools, spawn grants, budgets and tenant scope remain enforced outside
the model regardless of what an instruction says.

A summary instruction guides relevance; validation checks its word limit,
not its quality. Contracted answer text is withheld from the public event stream
until validation succeeds. Other execution events remain live. An invalid
result becomes a terminal helper failure and a tool error for the waiting
caller. It is never silently clipped, repaired or automatically regenerated.
Actions already performed by a helper are not undone by an invalid result.

## Execution and ownership

| Mode | Calling agent | Result |
|------|---------------|--------|
| `subagent`, the default | Waits for its helper | Receives validated text as the tool result |
| `delegate` | Continues immediately | Receives the run reference; the validated result later appears in the inbox and execution history |

Both modes go through `Platform.Call` and `pkg/call`. The caller's pinned spawn
allowlist, depth, fan-out, budget and admission policy apply. Waiting calls keep
one execution slot available so a chain of helpers cannot occupy every slot
while waiting. Attached work stops with its caller; delegated work is independent.
A return format never changes these lifecycle or delivery rules.

The agent tool offers a run only the agents available where its calls are filed:
one that lives elsewhere would be refused, and the run's readers may not read
it. An agent that lives in a project a reader may not read is left out of the
spawn list and the call policy lists they read. Naming one when they write
either is refused as naming an agent that does not exist, a call to one
through the calls endpoint is refused as one the spawn allowlist does not
name, and saving what they read keeps what they were not shown (see
[security](security.md)).

The low-level API's existing `handoff` mode is not exposed by the agent tool.
A real handoff needs explicit answer ownership transfer and caller termination;
selecting a return format does neither.

Before acceptance the platform resolves references and stores both their
provenance and the exact resolved messages in the child tag. The run's `input`
field exposes that snapshot to the existing run-detail and execution-history
views. Retries execute from the snapshot rather than reading the references
again. A repeated call ID joins its accepted child even after references are
removed or the fan-out allowance has been consumed. Reusing a call ID with
different input is an error. Tool call IDs also correlate parallel helper
steps with their runs, independent of start order.

`Access.Spawn` makes the unified tool available automatically. Per-target
`tag_<agent>` tools are replaced in Humagen. The explicitly configured
`delegate_task` tool and the calls HTTP endpoint retain their legacy `text`
shorthand, normalized to a user message and a text return contract. The HTTP
endpoint also accepts the structured contract under `input`.

## Runtime dependency

fiber is `orchagent/flow`, pinned with the rest of orchagent in `go.mod`
(`github.com/urmzd/orchagent`). It carries:
- the call extension;
- model fallbacks with `fallback_on`, and per-call approvals through
  `native.Approving` (`github.com/urmzd/orchagent/flow/turn/native`);
- `mcp:<provider>[:write]` grants from the tool provider store;
- unavailable tools and tool groups dropped with a warning rather than
  failing the run.

`make test` covers fiber and Humagen together.
