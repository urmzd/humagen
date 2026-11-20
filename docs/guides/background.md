# Background work

Ask an agent to keep at something while you do other things. It works in the
background, one run at a time, and tells you in your inbox when it is done.
Your notification settings decide whether that also reaches the console or
email.

| Kind | Ask it | Each run | Ends when |
|------|--------|----------|-----------|
| Watch | "Tell me when the release workflow on main passes" | A check every interval, in a private session kept for the watch | A check answers anything but `NONE` |
| Goal | "Keep going until every test passes" | The agent's next turn in the same conversation, as soon as the last ends | A turn ends with `GOAL MET`, or `GOAL BLOCKED: why` |
| Task | "Run the full migration dry run and tell me what breaks" | One run in a private session of its own, which you can open while it works | The run ends |

Each kind also ends when its time or runs run out, or after three failed runs
in a row (a task after one). You are told how it ended. Work that someone
stopped tells nobody.

## Grant the tools

An agent has these tools only when its revision names them. The builder may
propose them like any other grantable tool.

| Tool | What it does |
|------|--------------|
| `notify` | Sends the person the run works for a message now. Use it at the end of long work they asked to hear about. |
| `watch_start` | Starts a watch: `condition`, `how` to check it, `every` and `for`. |
| `goal_start` | Sets a goal on this conversation: `goal`, `done_when`, `turns` and `for`. The turn that sets it is the first. |
| `task_start` | Starts a task: `ask`, complete enough to do without the conversation. |
| `background_list` | Lists the person's watches, goals and tasks. |
| `background_stop` | Stops one of the person's own. |

```yaml
tools: [github_workflow_status, shell, notify, watch_start, goal_start, task_start, background_list, background_stop]
```

## How it runs

1. In a conversation, the agent calls a start tool. The work belongs to the
   person the run works for, and is filed in the conversation's project.
2. A run's end settles it. A goal's next turn starts at once; a watch's next
   check starts `every` after the last.
3. The leader process looks every 30 seconds for runs that are due, work that
   ran out, and runs that ended while no process saw them.
4. When it ends, the person gets one inbox message. A watch or a goal links to
   the conversation that asked; a task links to its own session.

## Limits

| Limit | Value |
|-------|-------|
| Watch `every` | 5m to 24h, default 15m |
| Watch `for` | up to 7d, default 24h; at most 200 checks |
| Goal `turns` | 1 to 25, default 10 |
| Goal `for` | up to 24h, default 4h |
| Task | one run, within 24h |
| Still running | 20 per person, 500 per workspace |
| Ended work kept | 30 days |

Spans read as `15m`, `2h` or `3d`.

## Who may do what

- Every run acts for the person who asked, with their access. A run is refused
  if they can no longer work in the project, and three refusals end the work.
- Evaluation runs, housekeeping runs and runs that act for nobody cannot start
  background work or send notices.
- Background work does not start more background work.
- A person can stop only their own. Another person's reads as not found.
- Each run is an ordinary run: it counts toward the project's budgets and
  limits, and its tool calls follow the same action policy and approvals.
