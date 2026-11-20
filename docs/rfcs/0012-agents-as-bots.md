# Agents as bots: one identity on every surface

Status: proposed. Nothing here is built.

## Goal

Every agent in a workspace is a coworker of its own: it has a name, a face,
something it is for, and a place where people find it. A person who meets
`@release` in the console, in a Slack thread and in a pull request comment
recognises the same bot each time, and can tell it apart from `@triage`
without reading a footer.

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
presentation: one app process serving several clients, and a way to post as
an agent.

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
- **Slack is per bot.** Slack is a row on the bot's own settings, "Set up",
  not a workspace-wide install the bot happens to be reachable through.
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

### 2. In Slack, one install and every agent posts as itself

The default is one Slack app per workspace that posts as the agent:
`chat:write.customize` added to the manifest, and every message the app posts
for a turn carries `username: display_name` and `icon_url: mark`. A thread
where `@lead` asked `@researcher` and `@writer` then reads as three bots, each
with its own face, which is what the console already shows.

Reaching an agent stays a tag (`@app release` or `@app @release`), which the
platform already reads. Two additions make it findable:

- **`/ask`** (or the existing `/agent`) opens a modal whose first field is an
  agent picker showing each agent's mark, display name and greeting, then the
  composer. It is how a person discovers who is there.
- **App Home lists the agents** the client routes to as cards (mark, name,
  greeting, starters), each starter a button that opens a DM thread with that
  agent already tagged.

What the shared app cannot do, and must not be papered over:

- A customized message still comes from the app's bot user: hovering the name
  shows the app, and `@Barbara` does not autocomplete.
- The assistant pane, `agents.sessions.*` and native streaming
  (`chat.startStream`) belong to the app. Whether streamed messages honour
  `username` and `icon_url` must be checked in `apps/slack-sandbox` before this
  is built. If they do not, a streamed turn opens with a `context` block of the
  agent's mark and display name, and the finished message is edited to carry
  them where Slack allows.

### 3. An agent may have its own Slack app

For an agent people should DM, mention by name and find in the sidebar, its
settings offer **Give it its own Slack app**. That is a client like any other,
with `default_agent` set to the agent, `agents` set to it alone, and `bots`
mapping the new bot user to it. The console:

1. Generates the app's manifest from the agent: name, `display_name`,
   description, background color from its hue, and the scopes the shared app
   has. The mark is offered as a PNG to upload, since a manifest cannot set an
   app icon.
2. Links to Slack's create-from-manifest page, then asks for the token and
   signing secret on the same screen, which it writes and never reads back.
3. Shows the row as connected once the first event arrives.

`apps/slack` must then serve several clients in one process: one Socket Mode
connection per client, the routing and branding of each, one event stream.
Today it reads one `HUMAGEN_CLIENT`. A per-agent app is the only way to get a
real Slack user per agent, so this is worth the cost, but it is the opt-in,
not the default: each app is an install a Slack admin approves, and twenty of
them clutter a workspace.

A rule for the two together: an agent with its own app answers only as that
app. A tag for it through the shared app is answered with a pointer to its
own ("Ask @Barbara directly"), so a person is never answered by the same
agent under two identities in one channel.

### 4. An agent's page opens on its home

`/agents/{name}` opens on a new **Home** tab, and its settings regroup into
the order the screenshots use, which is also the order a person needs them:

| Tab | Holds |
|-----|-------|
| Home | Mark, display name, greeting, starters, a composer ("Ask Barbara to ..."), its recent sessions, and who is waiting on it. One result in its header, worked out from its runs (sessions this week, or pull requests merged for an agent that opens them). |
| Instructions | Today's Prompt, and the greeting and starters. |
| Model | Unchanged. |
| Access | Who may use it (sharing), which agents it may call, and **where it works**: the workspace Slack app, its own Slack app, GitHub, schedules. |
| Context | Unchanged, with Memory folded in. |
| Quality | Evals and Rules. |
| History | Revisions and the queue. |

This follows the console's own rule that a page is headed by its tabs. Home
is the only tab a person who merely uses the agent needs; the rest are for its
owners and are hidden from someone who may not change it.

### 5. New Agent starts from a goal, then a face

`AgentSetup` becomes three steps:

1. **Goal.** Search a list of outcomes: the store's packages and the
   workspace's templates (`agentTemplates`), plus Start From Scratch. Picking
   one fills the prompt, tools and starters. The list is the store, not a
   second catalogue.
2. **Look.** Handle and display name, then the mark: a grid of shapes from
   regenerated seeds, the free hues, an optional glyph, with the live preview
   and the template's description beside it.
3. **Review & Test.** Today's last step, plus "Where it works", with the
   workspace Slack app on by default and its own Slack app offered.

Work & Permissions moves out of the wizard into the agent's Access tab, where
it is edited after creation: as the screenshot says, "You can set this Bot's
behavior after creating it". Defaults stay deny, so a new agent that skips it
can do nothing it was not granted.

### 6. Everywhere else, name the agent

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
3. **Slack, shared app.** `chat:write.customize`, persona on every post,
   `/ask` with the picker, agents on App Home. Verify streaming first.
4. **Slack, own app.** Multi-client `apps/slack`, the manifest generator, the
   connect flow on Access.
5. **GitHub and notifications.** The persona header and the waiting lines.

Each phase updates `docs/architecture/surfaces.md` and the status table.

## Open questions

1. Does `chat.startStream` take `username` and `icon_url`? This decides
   whether a streamed turn is fully in persona or opens with a context line.
2. Should a Slack admin be able to forbid per-agent apps for a workspace,
   keeping everything behind the shared one?
3. Is `display_name` unique in a workspace? Two "Barbara"s in one Slack
   thread would be confusing; the handle already is unique.
4. Does the Home tab's one result come from the agent's definition (which
   metric it reports) or is it always sessions this week?
5. For an agent installed from the store, are `identity` fields part of the
   package, so updating it can change the face people know?
