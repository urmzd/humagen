# Upgrading to saige v0.45.0

Humagen used to depend on two modules: `github.com/urmzd/orchagent` (`flow`,
`durable` and `sandbox`) and `github.com/urmzd/saige` (the agent SDK).
orchagent has been folded into saige. Humagen now requires only
`github.com/urmzd/saige` v0.45.0.

## Who needs to do anything

| You | What to do |
|-----|------------|
| Run Humagen (an operator) | Nothing. There is nothing to migrate: the same database, the same settings and the same image. Postgres tables keep the `fiber_` prefix, and event type names and envelope fields are unchanged |
| Build Humagen from source, or import it as a Go module | Use Go 1.26.11 or later. `go.mod` requires `github.com/urmzd/saige v0.45.0` and no longer requires `github.com/urmzd/orchagent` |
| Import orchagent packages alongside Humagen | Switch the import paths: `github.com/urmzd/orchagent/flow/...`, `.../durable/...` and `.../sandbox/...` are the same paths under `github.com/urmzd/saige`. The packages that moved elsewhere or were not carried over are in the table in [the spec](../reference/spec.md#since-orchagent-moved-into-saige) |
| Have a branch in flight | Merge main, then run `scripts/rewrite-orchagent-imports.sh` from the repository root. It rewrites the imports that map one to one, prints what it changed and what it left, and exits 1 if an orchagent reference is left. It edits files in place, so commit first |

Humagen never created `fiber_knowledge_documents`, `fiber_knowledge_chunks` or
`fiber_dispatch_runs` itself, and nothing creates them now. A database that
has them keeps them untouched.

## What changes at runtime

- **A batch of waiting tool calls parks the run once.** When one model message
  holds several tool calls that each wait (several approvals, several
  `ask_person` questions, several calls to other agents), the run parks once
  on all of them. Before, it parked once per question, one after the other.
  `run.parked.v1` carries `question`, the first, and `questions`, all of them,
  when there is more than one; `run.resumed.v1` carries `answer` and
  `answers`. Each question is still answered on its own, and the run resumes
  when every one is answered. Calls to other agents in one batch now start
  side by side.
- **Agents never approve a GitHub pull request review.** An `APPROVE` review is
  refused (`github.ErrNoApproval`). An agent comments or requests changes.
- **A run parked before the upgrade may announce its park once more.** A run
  that parked on a question before the upgrade, and is woken after it with the
  question still open, publishes `run.parked.v1` one more time, and never
  again after that. The question keeps its id and its one inbox message, and
  an answer resumes the run as before.
- **A waiting tool call says so on the stream.** A run's events now include
  `run.delta.tool.marker.v1` for a tool call that waits for a decision, such as
  an approval. The question a person answers is still on `run.parked.v1`. A
  reader that passes over types it does not know needs no change.
- **A request says what it refers to.** A message from Slack or GitHub that
  names a person, a channel, an issue, a pull request or a link carries them
  as `references`, and the model is shown them in a `References:` list under
  the text.
- **Delta events have two optional fields.** `run.delta.citation.v1` may carry
  `tool_call_id`, for a source a tool's result cited, and every delta may
  carry `parent`, the path of tool calls an agent that runs as a tool of
  another spoke through. None of Humagen's own tools sets either today.

## Checking an upgrade

`scripts/upgrade-from-release.sh` builds a release from its tag, has it serve
a fresh database and do real work (sessions, finished runs, a run parked on a
question, a schedule), then starts this checkout's binary on the same
database and checks that it carries all of it on. It needs Docker and calls no
model and no outside service.
