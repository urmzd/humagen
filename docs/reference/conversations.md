# Entry agent and follow-ups

The deployment entry agent is `humagen` by default. Set
`execution.entry_agent: codex` for a deployment named Codex. It is a versioned
agent in `humagen/core`, installed with the configured built-in provider/model.
It answers simple asks itself and delegates specialist work through `call_agent`
or offered `tag_<agent>` tools, including code reviews to `code-reviewer`. It
waits for results and returns one merged answer naming the contributors.

Open routing prefers the available entry agent. Explicit agent mentions and
explicit conversation selections still apply. Without the entry, `pkg/join`
remains the fallback. Surface routing and project availability remain enforced.
The entry's runtime spawn list contains only other agents available in its
project; the call path checks availability again. Customizations retain explicit
spawn grants. No wildcard grant is added to ordinary agents.

Messages posted during work are classified with the router's resolved model:
queue adds a follow-up, replace stops the prior work and then starts the new ask,
and stop requests no new work. Failed, malformed or uncertain classification
queues. With no router model configured, every follow-up queues.

The decision, reason and affected run IDs are stored on the thread message.
No new event is registered: that catalog belongs to orchagent. Runtime stop
preserves the partial answer and context. Queue and replacement dispatch wait
for terminal run records, through the existing durable completion pipeline,
and recheck the person's current project access. The console shows a quiet
queue, replacement or stop notice beside the message.
