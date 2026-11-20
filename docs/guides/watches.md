# Watch and notify

Ask an agent "tell me when the release workflow on main passes" and it keeps
checking in the background. When it happens, you get a message in your inbox,
and your notification settings decide whether it also reaches the console or
email.

## Grant the tools

An agent has these tools only when its revision names them. The builder may
propose them like any other grantable tool.

| Tool | What it does |
|------|--------------|
| `notify` | Sends the person the run works for a message now. Use it at the end of long work they asked to hear about. |
| `watch_start` | Starts a watch: a condition, how to check it, how often (`every`) and for how long (`for`). |
| `watch_list` | Lists the person's watches that are still watching. |
| `watch_stop` | Stops one of the person's watches. |

```yaml
tools: [github_workflow_status, notify, watch_start, watch_list, watch_stop]
```

## How a watch runs

1. In a conversation, the agent calls `watch_start`. The watch belongs to the
   person the run works for, and is filed in the conversation's project.
2. The leader process looks for due watches every 30 seconds. Each check is a
   run of the same agent, for the same person, in a private conversation kept
   for that watch's checks (`watch://<id>`). Each check sees what the earlier
   ones found, and none of them appear in the conversation that asked.
3. A check answers `NONE` while the condition does not hold. The next check
   starts `every` after this one ends.
4. Any other answer means it happened. The watch ends, and the answer goes to
   the person's inbox, linked to the conversation that asked.
5. A watch that runs out of time, or whose checks fail three times in a row,
   ends and tells the person so. A watch someone stopped tells nobody.

## Limits

| Limit | Value |
|-------|-------|
| `every` | 5m to 24h, default 15m |
| `for` | up to 7d, default 24h |
| Checks per watch | 200 |
| Watches still watching | 20 per person, 500 per workspace |
| Ended watches kept | 30 days |

Spans read as `15m`, `2h` or `3d`.

## Who may do what

- A watch acts for the person who asked, with their access. Each check is
  refused if they can no longer work in the watch's project, and after three
  refusals the watch ends.
- Evaluation runs, housekeeping runs and runs that act for nobody cannot start
  watches or send notices.
- A check cannot start another watch.
- A person can stop only their own watches. Another person's reads as not
  found.
- Each check is an ordinary run: it counts toward the project's budgets and
  limits, and its tool calls follow the same action policy and approvals.
