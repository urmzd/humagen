# Agents as bots: one identity on every surface

Status: proposed. Nothing here is built.

## Goal

Every agent in a workspace is a coworker of its own: it has a name, a face,
something it is for, and a place where people find it. A person who meets
`@release` in the console, in a Slack thread and in a pull request comment
recognises the same bot each time, and can tell it apart from `@triage`
without reading a footer.

There is one Slack app, and it takes on the identity of whichever agent is
answering. People talk to the workspace's lead (the entry agent) by default,
and to one agent directly when they want to.

Today the console comes close and Slack does not. This RFC sets out the
experience on both, the choices behind it, and the smallest changes to the
model that get there.

## Context

### What a person sees now

**In the console.** An agent is a small bot in its color (`AgentMark`): the
shape comes from its name, the hue is reserved per workspace
(`/api/agent-hues`), and it hops while it works. That identity is good and
consistent. But the agent's page (`/agents/{name}`) is a configuration form:
it opens on Prompt, then Model, Access, Queue, Context, Memory, Evals, Rules
and Revisions. There is no place that answers "who is this, what can it do for
me, what has it done lately, and how do I ask it". New Agent
(`AgentSetup`) asks for Role & Scope, then Work & Permissions, then Review &
Test: it starts from a prompt and tools, not from the outcome the person wants,
and never asks how the agent should look.

**In Slack.** There is one bot. A Slack app is a [client](../reference/clients.md),
and the rich app (`apps/slack`) reads one client (`HUMAGEN_CLIENT`) and posts
every answer as that app's bot user. Which agent answered shows only in text:
the `{name}` in the branding disclaimer, a handoff line, a task row. The
manifest has no `chat:write.customize`, so no message carries an agent's name
or mark. A person reaches a particular agent by typing `@app @release ...`;
Slack's mention autocomplete offers only the app.

The model already allows more than this. A workspace may hold several Slack
clients, each with its own `default_agent` and `agents`, and
`client.bots` maps a Slack bot user id to the agent it stands for, so the
platform already reads `<@U08BOT>` as `@release`. What is missing is the
presentation: a way for the one app to post as the agent that answered, and
a thread that stays with the agent it was started for.

### What the reference products do

The screenshots that prompted this (a hosted coding-agent product) show the
shape people now expect:

- **Creating a bot starts from a goal.** "What's your goal?" lists outcomes
  (Software Factory, Product Engineer, Anomaly Watcher, Code Reaper) before
  anything about prompts. Then "Customize your Bot": pick a body, a color, or
  an icon, with a regenerate button, and the role's one-paragraph description
  beside a live preview.
- **A bot has a home.** Opening it shows its mark and name, a greeting in its
  own voice ("I can keep customer performance reports current..."), and "Put
  this Bot to work": three concrete jobs, each naming the repository it acts on.
  The composer says "Ask Barbara to ...".
- **A bot has its own settings,** grouped by Access (Sharing, Slack), Tools
  (Connections, Automations, Secrets, Outposts) and Behavior (Memory, Usage
  limits, Pull requests, Analytics), headed by its mark, its name and one
  result ("0 PRs merged").
- **One product bot that takes on each bot's personality.** The bots are
  reached through one product, not installed one by one.
- **Work is listed by session and by who is waiting.** "Devin is waiting for
  your input" is the most common notification.

## Decisions

### 1. An agent's identity is part of its definition

Add `identity` to `AgentSpec`. Every field is optional and has today's
behaviour as its default, so no stored agent changes.

| Field | Meaning | Default |
|-------|---------|---------|
| `display_name` | What people call it in prose and in Slack: "Barbara". The handle stays `name`. | `name` |
| `mark.seed` | What the bot's shape is drawn from. Regenerate picks a new seed. | `name` |
| `mark.hue` | One of the workspace palette's hues, reserved like today's. Choosing one already reserved is refused. | the reserved hue |
| `mark.glyph` | An icon drawn on the bot's screen (a bug, a chart, a gear), from a fixed set. | none |
| `greeting` | One or two sentences in the agent's voice, shown on its home and in Slack's App Home. | its `description` |
| `starters` | Up to three jobs: a title, one line, and the message it sends. Optionally a project or repository. | none |

No uploaded image. The console's rule holds: an agent is a small bot in its
color, so no person's mark looks like an agent and no agent looks like a
person. Glyph and color give the range people want (the screenshot's
"Icons" tab) without breaking that.

The mark is drawn once, by `bot-avatars`, on every surface. Slack and GitHub
need an image, so the server renders one:
`GET /api/agents/{name}/mark.png?size=…`, cached by a hash of the identity,
readable by whoever may read the agent. Slack fetches `icon_url` without a
credential, so the Slack app gets a signed URL that expires with the cache
entry, not a public route that would list agent names.

### 2. In Slack, one app that takes on whoever is answering

There is one Slack app per workspace, and it has no personality of its own:
every message it posts for a turn is posted as the agent that answered it,
with `username: display_name` and `icon_url: mark` (`chat:write.customize`
added to the manifest). There are no per-agent Slack apps. One install, one
thing for an admin to approve, one name to mention.

Two ways to talk to it:

- **The lead, by default.** A message to the app that names no agent (a
  mention in a channel, a DM, the assistant pane) goes to the workspace's
  entry agent, posted as the entry agent's identity. It answers simple asks
  itself and brings specialists in with the `agent` tool, as it does in the
  console. When a specialist's result is shown on its own (a mention of
  several agents, a handoff), that part posts as the specialist, so a thread
  where the lead asked `@researcher` and `@writer` reads as three faces, not
  one bot quoting two.
- **One agent, when a person wants that.** A thread started for one agent is
  that agent's: it answers every untagged reply in the thread until the
  person tags the lead or another agent, or it hands back. Today a mention
  picks the answering agent for one message only, and the next untagged reply
  goes back to the default; the thread should remember instead. A person
  starts one by tagging the agent (`@app @barbara ...`, or the agent's
  display name first), from `/ask`, or from App Home.

Finding the agents:

- **`/ask`** opens a modal whose first field is an agent picker, the lead
  first, then each agent the client routes to with its mark, display name and
  greeting.
- **App Home** shows the lead's greeting and starters at the top, then the
  other agents as cards (mark, name, greeting, starters). Each card's Talk To
  button opens a DM thread that is that agent's.

What one shared app cannot do, and must not be papered over:

- Every message still comes from the app's bot user: hovering the name shows
  the app, and `@Barbara` does not autocomplete. The picker and App Home are
  how agents are found.
- The assistant pane, `agents.sessions.*` and native streaming
  (`chat.startStream`) belong to the app. Whether streamed messages honour
  `username` and `icon_url` must be checked in `apps/slack-sandbox` before this
  is built. If they do not, a streamed turn opens with a `context` block of the
  agent's mark and display name.
- The App Home tab, the app's DM and its icon in the sidebar are the app's,
  not any agent's. They should read as the workspace (its branding name),
  since the lead speaks for it.

### 3. An agent's page opens on its home

`/agents/{name}` opens on a new **Home** tab, and its settings regroup into
the order the screenshots use, which is also the order a person needs them:

| Tab | Holds |
|-----|-------|
| Home | Mark, display name, greeting, starters, a composer ("Ask Barbara to ..."), its recent sessions, and who is waiting on it. One result in its header, worked out from its runs (sessions this week, or pull requests merged for an agent that opens them). |
| Instructions | Today's Prompt, and the greeting and starters. |
| Model | Unchanged. |
| Access | Who may use it (sharing), which agents it may call, and **where it works**: whether the Slack app and GitHub client route to it, and its schedules. |
| Context | Unchanged, with Memory folded in. |
| Quality | Evals and Rules. |
| History | Revisions and the queue. |

This follows the console's own rule that a page is headed by its tabs. Home
is the only tab a person who merely uses the agent needs; the rest are for its
owners and are hidden from someone who may not change it.

### 4. New Agent starts from a goal, then a face

`AgentSetup` becomes three steps:

1. **Goal.** Search a list of outcomes: the store's packages and the
   workspace's templates (`agentTemplates`), plus Start From Scratch. Picking
   one fills the prompt, tools and starters. The list is the store, not a
   second catalogue.
2. **Look.** Handle and display name, then the mark: a grid of shapes from
   regenerated seeds, the free hues, an optional glyph, with the live preview
   and the template's description beside it.
3. **Review & Test.** Today's last step, plus "Where it works": whether the
   Slack app and GitHub client may route to it.

Work & Permissions moves out of the wizard into the agent's Access tab, where
it is edited after creation: as the screenshot says, "You can set this Bot's
behavior after creating it". Defaults stay deny, so a new agent that skips it
can do nothing it was not granted.

### 5. Everywhere else, name the agent

Small changes that make each agent read as itself:

- The composer's placeholder names the picked agent.
- Inbox items and notifications that wait on a person lead with the agent's
  mark and say "Barbara is waiting for your input", not the run's title alone.
- The sessions list can group by agent, and the agent's Home lists its own.
- GitHub comments from an agent open with its mark and display name, since
  the GitHub App, like the shared Slack app, posts as one login.

## Phasing

1. **Identity.** `identity` on `AgentSpec` and `api/openapi.yaml`, the mark
   renderer and its route, display name and glyph in `AgentMark`. Nothing
   visible changes for agents that set none.
2. **Console.** The Home tab, the regrouped tabs, the new setup steps.
3. **Slack.** `chat:write.customize` and the answering agent's identity on
   every post (verify streaming first), threads that stay with the agent they
   were started for, `/ask` with the picker, the lead and the agents on App
   Home.
4. **GitHub and notifications.** The persona header and the waiting lines.

Each phase updates `docs/architecture/surfaces.md` and the status table.

## Open questions

1. Does `chat.startStream` take `username` and `icon_url`? This decides
   whether a streamed turn is fully in persona or opens with a context line.
2. Does a thread that is one agent's stay that agent's in the console too,
   so a Slack thread and its Humagen thread agree on who answers next?
3. Is `display_name` unique in a workspace? Two "Barbara"s in one Slack
   thread would be confusing; the handle already is unique.
4. Does the Home tab's one result come from the agent's definition (which
   metric it reports) or is it always sessions this week?
5. For an agent installed from the store, are `identity` fields part of the
   package, so updating it can change the face people know?
