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
- **Slack is a row on the bot's own settings,** "Set up". Devin's docs say
  to give a Bot its own Slack app so the team can @-mention it or DM it, with
  a Who Can Message setting deciding who may DM it and whose thread replies it
  reads. The main Devin stays one `@Devin` app.
- **Work is listed by session and by who is waiting.** "Devin is waiting for
  your input" is the most common notification.

### How others put bots in Slack (November 2026)

| | Claude Tag | Devin Bots | Grok Team Bots |
|---|---|---|---|
| Slack identity | One `@Claude` per workspace. Admins make "separate Claude identities for different uses" by scope (organization, workspace, private channel), each inheriting the permissions and memory of the one above | `@Devin` is one app; each Bot may get its own Slack app, to be @-mentioned and DM'd by name | A bot is created, published, then "Bring to your team's Slack", which a Slack admin approves. A third-party guide says the bot's name is its handle; xAI's docs do not say |
| Personas | None. Behaviour differs by channel: channel instructions, channel memory, a Configure link in each reply's footer, which also names the model | Each Bot is its own named bot with a mark | Each bot is its own named bot |
| Threads | "Once mentioned in a thread, Claude follows the rest of that thread and may reply without another mention." Anyone in the channel can steer. Reads the channel's history and pinned items | Not checked: the docs host does not resolve from here | Not checked: the docs host does not resolve from here |
| Progress | A working indicator with Stop in channel threads; a checklist edited in place for longer work | | |
| Whose access | A channel uses the channel's connections set by an admin and acts as Claude's own accounts; a 1:1 DM uses the person's own connectors and acts as them | | Each teammate links their Slack account once |

What this says for Humagen:

- **Nobody does one app with personas.** Claude Tag keeps one identity and
  varies it by place; Devin and Grok give a named bot a Slack app of its own.
  One app that posts as each agent sits between them: the single install of
  Claude Tag, and a face per agent like Devin's and Grok's, without a real
  Slack user per agent.
- **Place is a persona that costs nothing.** Claude Tag's scope model is
  Humagen's channel binding: a Slack channel bound to a project whose
  default is `barbara` makes Barbara the one who answers there. `#perf-reports`
  can be Barbara's room without a second app.
- **Following a thread after one mention is now expected.** Claude Tag does it
  and lets anyone in the channel steer.
- **The footer is where a reply says who and how.** Claude Tag names the model
  and links to Configure there; Humagen's disclaimer line can name the agent and
  link to its page.

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

There is one Slack app per workspace, named for the workspace (its branding
name, "Humagen" by default). It has no voice of its own: everything it says
for a turn is said as the agent doing the work. One install, one thing for an
admin to approve, one name to mention.

#### How a message wears an agent

Slack lets an app with `chat:write.customize` set `username` and `icon_url`
per message, and since May 2026 `chat.startStream` and
`assistant.threads.setStatus` take the same authorship arguments. `apps/slack`
calls those methods directly, so every place a turn shows can carry the
agent:

| What shows | Who it shows as |
|------------|-----------------|
| The streamed answer, its plan and its task rows | The agent answering |
| The status line above the reply box ("is checking the rollout...") | The agent doing that step, so it changes face when the lead calls a specialist |
| A question or an approval a run waits on | The agent that is waiting: "Barbara needs your approval" |
| Background results and `notify` | The agent whose work it was |
| The person's DM about a mention, Run Details | The agent named in it |
| The reaction on the person's message (👀 ✅) | The app. A reaction has no author to change |
| App Home, the app's DM header, its sidebar icon | The app, as the workspace |

Slack still marks every one of these APP and, on hover, names the app, so a
persona is never mistaken for a person. To keep it that way a
`display_name` may not be a member's name in the workspace's directory, and
is unique among its agents.

#### Who answers

- **The lead, by default.** A message that names no agent (a mention in a
  channel, a DM, the assistant pane) goes to the workspace's entry agent and
  shows as the lead. It answers simple asks itself and brings specialists in
  with the `agent` tool. **One ask gets one answer**: a specialist the lead
  calls shows in the status line and as a task row with its own face, but the
  answer is the lead's, crediting who did what. Posting each specialist's
  part as its own message would turn one question into three notifications.
- **Several agents, when the person asks several.** `@app @rex @barbara ...`
  gets one reply from each, each as itself, because the person asked each.
- **One agent, when a person wants that.** A thread started for one agent is
  that agent's: it answers every reply in the thread until the person names
  the lead or another agent, or it hands back. A handoff shows as a context
  line, "Barbara handed this to Rex", and Rex answers from there. Today a
  mention picks the agent for one message only; the thread must remember it.

#### Following a thread without a mention

In a channel the app hears only messages that mention it. Making people type
`@Humagen` on every reply to an agent they are already talking to is the
largest papercut in the flow, so the app also subscribes to
`message.channels` and `message.groups`, and takes an unmentioned message only
in a thread it has answered in, from a person the client maps. Anything else
it hears that way it drops unread.

#### Walkthrough

1. **A channel ask.** Dana writes `@Humagen why did last night's deploy fail?`
   👀 lands on her message. Above the reply box: *Lead is reading the deploy
   logs*, then *Rex is checking the rollout* with Rex's face. The answer streams
   as **Lead**: what failed, "Rex checked the rollout", and the fix. Dana
   replies `can you roll it back?` with no mention; the lead answers, and asks
   for approval on a card that says *Lead needs your approval*.
2. **Going to one agent.** Dana writes `@Humagen @barbara refresh the perf
   report`. Barbara answers as **Barbara**. Every reply in that thread now goes
   to Barbara. `@lead what else is open?` in the same thread brings the lead
   back, and the thread is the lead's again.
3. **Finding someone.** Sam does not know who does what. `/ask` opens a modal:
   the lead first, then each agent with its face, name and greeting. Picking
   Barbara opens a DM thread that is Barbara's, with her starters as suggested
   prompts.
4. **The DM and the assistant pane.** A DM thread with no agent named is the
   lead's. App Home shows the lead's greeting and starters, then the other
   agents as cards, each with Talk To, which opens a DM thread that is that
   agent's.
5. **Later.** Work Barbara was asked to do in the background reports back in
   the thread that asked, as Barbara.

#### What one shared app cannot do

- `@Barbara` does not autocomplete: the agents are not Slack users. `/ask`,
  App Home, and typing the agent's handle after the app's mention are how
  people reach them.
- Slack's sidebar, App Home and DM list show one app. Several 1:1
  conversations with different agents are threads in one DM, each headed by
  its agent's first message.

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
3. **Slack.** `chat:write.customize` and the agent's identity on every post,
   stream and status line, threads that stay with the agent they
   were started for and are followed without a mention, `/ask` with the picker, the lead and the agents on App
   Home.
4. **GitHub and notifications.** The persona header and the waiting lines.

Each phase updates `docs/architecture/surfaces.md` and the status table.

## Open questions

1. Does `chat.update` on a customized message keep its `username` and
   `icon_url`, or must every update send them again? Check in
   `apps/slack-sandbox`.
2. Does a thread that is one agent's stay that agent's in the console too,
   so a Slack thread and its Humagen thread agree on who answers next?
3. Should a person be able to turn following off for a channel, so the app
   reads nothing there that does not mention it?
4. Does the Home tab's one result come from the agent's definition (which
   metric it reports) or is it always sessions this week?
5. For an agent installed from the store, are `identity` fields part of the
   package, so updating it can change the face people know?
6. Devin and Grok give a named bot its own Slack app. If one agent ever needs
   a real Slack user (autocomplete, its own DM, its own sidebar entry), is that
   a client with that agent as its default, added later, or never?
