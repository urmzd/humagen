# Handoffs and threads: who answers, and where their work shows

Status: proposed. The console work it describes is partly built on a branch
(`feat/agent-call-items`); the server work is not started.

## Goal

A person picks the agent that answers, or lets the entry agent answer. From
there every other agent is reached by a tool call, in one of three ways:

- **Subagent.** A small context of its own. It does one job and reports back.
  The caller still owns the answer.
- **Handoff.** The answering agent changes. The new agent has its own system
  prompt and tools, carries the conversation so far, and answers in the
  caller's place. It stays the answering agent until it hands back.
- **Delegate.** The caller carries on. The other agent answers later, as a
  response of its own.

A session holds several lines of this work at once: different people's
messages, and work an agent handed on. Each line is a thread, shown as its own
flat run of messages and as one line in the prompt while it works.

## Context

### What Humagen has

- **One call primitive.** `pkg/call` defines three modes: `subagent` (the
  caller parks, the answer returns to it), `handoff` (the caller ends, the
  child answers wherever the caller would have, inheriting its source, routes
  and trigger context) and `delegate` (the caller keeps working, the answer
  arrives later).
- **One tool.** The `agent` tool, `{agent, title, brief, background?}`, is the
  one way a run calls an agent (humagen#318). It starts a `subagent` call, or
  a `delegate` call with `background: true`. Nothing starts a `handoff` call.
- **A prepared input that can carry messages.** `call.Prepared` holds
  `Messages`, and the executor turns all but the last into the child's
  history. A `handoff` child is not told "you are a helper"; every other child
  is. The `agent` tool sends one message, the brief.
- **Who answers a message.** The one agent a message mentions. For several
  mentions, the entry agent, with the platform making one `agent` call per
  mention before the first model step. For no mention, the nearest default for
  the place, else the entry agent (`--entry-agent`, `humagen` by default).
  Nothing remembers who answered last.
- **A handoff record.** `operations.Handoff` records owner, goal, completion
  condition, deadline and a bounded chain alongside an agent call, and
  escalates a cycle or more than three hops. It is bookkeeping about a call,
  not a change of who answers. RFC 0002 covers the cross-project case.
- **One activity view.** On the branch, the console's activity is one tree per
  turn, rooted at the answering agent, with each called agent as a collapsed
  item that opens to what it did. The prompt lists every thread at work.

### What others do

Checked 2026-11-11 against each project's documentation.

| | Subagent | Handoff | Who answers the next turn |
|---|---|---|---|
| OpenAI Agents SDK | `Agent.as_tool()`: the manager keeps control and owns the answer | A tool call named `transfer_to_<agent>`. The new agent sees the whole history by default; an `input_filter` trims it, and an opt-in setting compacts it to a summary | The application reuses `last_agent` |
| LangChain | Stateless, starts fresh each call, results flow back through the main agent | A tool call that changes the active agent; state persists across turns | The agent handed to |
| Claude Agent SDK | The `Agent` tool: fresh context holding only the prompt string; only the final message returns. Also background runs, resume, and fork, which copies the conversation | None | Not applicable |

Two things follow. The industry's two primitives are the two described under
Goal, and a handoff is a tool call everywhere it exists. No project above
models several people and several threads working in one session; that part
is ours to design.

## Decision

### 1. One tool, three modes

The `agent` tool gains `mode`, one of `subagent` (default), `delegate` and
`handoff`. `background: true` stays as a stored spelling of `mode: "delegate"`
and is read in `internal/legacy/tools.go` with the other earlier names.

A handoff passes every check a subagent call passes: the caller's spawn grant,
the callee's call policy, the action policy for `call_agents`, depth, fan-out
and budget. A spec may grant `subagent` without granting `handoff`: handing
over the answer is a larger permission than asking for help.

### 2. What a handoff carries

- **History.** The thread's messages so far, as the caller saw them, become
  the child's history through `call.Prepared.Messages`. The caller's `brief`
  is the last message: why it is handing over and what is left to do.
- **Not the caller's instructions.** The child runs under its own system
  prompt, tools, model and budget. The caller's system prompt and private
  tool results it did not put in the thread are not carried.
- **A bounded size.** Past a limit, the oldest messages are replaced by a
  summary the platform writes, and the handoff says it was summarized. The
  limit is the callee's context budget, not a fixed number.
- **Person context stays the person's.** What a handoff may carry follows the
  isolation rules of RFC 0004: nothing reaches the child that the person the
  run acts for could not read.

### 3. The answering agent of a thread

A thread remembers its answering agent. A handoff sets it to the callee. The
next message in that thread that names nobody is answered by that agent, not
by the entry agent. A mention still overrides it for that message. The agent
gives the thread back by handing off to the agent it came from, or a person
resets it from the thread's header.

The caller's run ends when the handoff is accepted, with a result that says to
whom it handed over. The callee's run is the thread's next turn, not a child
that reports back.

### 4. Threads in a session

- A thread starts when a person posts a message that starts a run, and when a
  delegate call is accepted. A handoff continues the thread it happened in.
- Each thread has one answering agent at a time and one flat run of messages.
  Subagent calls never become threads: they are items inside a turn.
- Several threads may work at once. A person's message goes to the thread
  they are replying in, else it starts a thread.
- The existing choice "continue this agent's session or start fresh"
  (`sessionPick` in the composer) is the same choice, said per thread.

### 5. What the console shows

| Thing | In the message list | In the activity tree | In the prompt |
|---|---|---|---|
| Subagent call | Nothing of its own | A collapsed item under its caller; opens to what it did | Counted on its thread's line |
| Delegate call | Its answer, when it comes, as its own response | An item marked Delegated | Its own line while it works |
| Handoff | A line: "lead handed over to researcher", then the callee's turns | The caller's tree ends at the handoff item; the callee's turn is the next root | The thread's line changes agent |

Built on the branch: the tree as the only activity view, called agents as
items, the prompt's working lines with a Stop each, and the Delegated mark.
Not built: the handoff line, the thread header's answering agent, and threads
as separate flat runs.

## Alternatives

- **A second tool, `transfer_to_<agent>`, per agent.** This is OpenAI's shape.
  Rejected: humagen#318 removed per-agent tools so that one definition carries
  the approvals and the policy category. A mode on that tool keeps one place.
- **Carry nothing but the brief.** Simple and safe, and it is what a subagent
  gets. Rejected for handoffs: the callee would ask the person to repeat
  themselves, which is the failure a handoff exists to avoid.
- **Always carry the full history.** Rejected: a long thread would exceed a
  smaller callee's context, and the cost of every handoff would grow with the
  thread. A summary past the callee's budget bounds both.
- **Keep the entry agent as the answerer and let it call the specialist each
  turn.** This works today. Rejected as the only path: every turn pays for the
  entry agent's step, and the specialist never holds the conversation.

## Changes

1. Public Humagen: `mode` on the `agent` tool, with `background` read as
   `delegate`; a grant that separates handoff from subagent.
2. Public Humagen: the handoff's prepared input, built from the thread's
   messages, with the summary rule and the RFC 0004 isolation check.
3. Public Humagen: a thread's answering agent, set by a handoff, read when a
   message names nobody, reset by a person.
4. Console: the handoff line, the thread header's answering agent, and the
   prompt line following the thread across a handoff.
5. Slack and GitHub: the same thread rule, so a handoff in a Slack thread
   changes who answers there.

## Open questions

- Whether a handoff needs a person's approval by default, or only when the
  callee's call policy or the action policy says so.
- Whether the callee may refuse a handoff, and what the caller does then.
- How a handoff counts against depth: it ends the caller, so a chain of
  handoffs is not nested, but `operations.Handoff` already stops at three
  hops and a cycle.
- Whose budget lease the callee draws on once the caller has ended.
- Whether the answering agent is remembered per thread or per person in a
  thread, when several people share one.
- Whether a fork (a subagent that starts from a copy of the conversation, as
  in the Claude Agent SDK) is a fourth mode or a handoff that reports back.
