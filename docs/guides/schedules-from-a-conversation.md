# Schedules from a conversation

Ask an agent, in a conversation or a Slack thread, to set up recurring work:
"check Plaud every 15 minutes and post in this channel when there is a new
recording". The agent proposes a schedule, you approve it on a card, the agent
tells you when it runs next, and you can run it once now to see it work.

## The flow

1. You ask. The agent calls `create_schedule` with a name, a cron, what each
   run checks, and where it posts.
2. An approval card appears for you: **Create a schedule: plaud-new, every 15
   minutes, posts in this Slack channel (C0PLAUD) only when something fires**.
   Its fields say what it checks, how often, which agent, where it posts, and
   when it would next run, in your own time zone.
3. You approve. The schedule exists, filed in the conversation's project and
   owned by you. The tool answers with its next runs in UTC (`next_run`,
   RFC 3339 such as `2026-11-20T14:15:00Z`), and the agent confirms the time.
4. The agent offers to run it once now. `run_schedule` is approved the same
   way (**Run plaud-new once now**), and its run acts for you.

Each run answers `NONE` when it finds nothing: only other answers are posted.

## Grant the tools

An agent has these tools only when its revision names them. The builder may
propose them like any other grantable tool.

| Tool | What it does | Card |
|------|--------------|------|
| `create_schedule` | Makes a schedule: `name`, `cron`, `check`, and optionally `agent` (defaults to the asking agent), `description` and where it posts (`post`, `connection`, `to`). | Create a schedule: name, how often, where it posts |
| `update_schedule` | Changes one of the conversation's project's schedules. Every field is given as it should now be. | Change the schedule name, how often, where it posts |
| `pause_schedule` | Pauses one. | Pause the schedule name |
| `resume_schedule` | Resumes one, and answers with its next run. | Resume the schedule name |
| `run_schedule` | Runs one once now. | Run name once now |

```yaml
tools: [web_fetch, create_schedule, update_schedule, pause_schedule, resume_schedule, run_schedule]
```

Deleting a schedule stays in the console and the API.

## Where it posts

| `post` | Answers go |
|--------|------------|
| left out, or `session` | Nowhere: each answer stays in its own session |
| `here` | The Slack channel the conversation is in, through the Slack connection it came in by. Refused outside a Slack thread |
| `slack` | A Slack channel (`to`, an id such as `C0123ABC`) through a Slack connection (`connection`) |
| `github` | A comment on an issue (`to`, `owner/repo#12`) through a GitHub connection |
| `inbox` | Your inbox |

A cron is five fields, read in UTC: `*/15 * * * *` is every 15 minutes,
`0 9 * * 1-5` weekdays at 09:00 UTC.

## Who may do what

- Every call goes through the API's own route (the same as
  `POST /api/schedules`, `PUT /api/schedules/{name}`, `.../pause`,
  `.../resume` and `.../run`) as the person the run works for, with their
  roles. The agent can do nothing they could not: without `schedules:write`, a
  call is refused, and a connection, agent or project they cannot see is not
  found.
- Every call waits for that person's approval, whatever the action policy
  allows, for up to a day. They may approve their own. In a conversation set
  to Run, the approval agent may decide it in their place where the policy
  asks; where the policy allows, the person still decides. A policy of
  `never` for `other` refuses every call.
- A schedule made from a conversation is filed in its project and owned by the
  person (`created_by`), so Run Now from it acts for them. A conversation in no
  project makes none.
- Only the schedules filed in the conversation's project can be changed,
  paused, resumed or run from it. Any other reads as not found.
- Evaluation runs, housekeeping runs, runs that act for nobody, background work
  and scheduled runs cannot make or run schedules.
