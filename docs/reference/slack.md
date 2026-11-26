# Slack agent delivery

## Two ways to answer in Slack

| | Webhook connector | Slack app |
|---|---|---|
| Where | Go, in `humagen serve` (`pkg/integrations/slack`) | TypeScript, `apps/slack`, a separate process |
| How Slack reaches it | Signed webhooks to a public URL | Socket Mode, no public URL |
| How it reaches Humagen | In process | The HTTP API, as a machine. Under `tailscale` sign-in the server that starts it makes its credential ([security](../architecture/security.md#the-slack-app-under-tailscale)) |
| A run looks like | One plain message per run, edited with `chat.update` | Humagen components: native streaming, plans, tool steps, failures with Retry, files. See [steps, command output and run details](#steps-command-output-and-run-details) |
| Agent session status and the stop button | Yes | Yes |
| Code channel slash commands | Yes | Yes: each command in the client's `slack.commands`, registered with `agents.conversations.setCommands` when a message is accepted in the channel. Ordinary channels use `/skill` |
| `/agent`, shortcuts, Home tab | No | Yes, from Humagen's sandbox manifest |
| Inbox items about a run | No | In the run's thread, and on Home |
| Several apps per workspace, as [clients](clients.md) | Yes: tenant, limits and allow-list come from the client | Reads one client's default agent and allow-list, in full as the client's `slack.machine`, though they name an agent that lives in a project the machine may not read. A mapped person's messages are the client's (threads with a surface, below). Nobody else starts a run (see [who can start a run](#who-can-start-a-run)) |
| Channels bound to projects | Yes: a run in a bound Slack channel is filed in the channel it is bound to, and an untagged one answered by that channel's chain, else in the client's home | Yes: a mapped person's Slack thread is a Humagen thread filed by the binding, and announcements reach a bound general channel, caught up on after a restart or a reconnect, where a reply that mentions the app starts a thread seeded with the announcement |
| Mentions of a person | No | In their direct messages with the app, with Open Thread and Reply, unless they are in the Slack thread already (they wrote there or were mentioned there with `<@U…>`). Experts named by a role are each told |
| Background work and `notify` | No | A reply in the app's Slack thread the work was asked from, naming the person, with its one line and Open Session; else in their direct messages. See [background results](#background-results) |
| Who asked | Recorded on the run, and the run acts for the person the team's clients map the Slack user to. Only someone they map asks at all (see [the webhook connector](#who-can-start-and-stop-a-run-on-the-webhook-connector)) | Recorded as the person, and acts for them (`act_as`): their message is theirs in the Humagen thread, and so are the runs it starts. Only someone the app's client maps asks at all. Someone else's words they ask about are asked for nobody, at their asking (`asked_by`). Tools that act as a person use the person's own credential |
| Questions a run waits on | No | In the run's thread, answered for the person the client maps. See below |
| Tokens and cost | No | Never on a turn. A finished turn offers **Run Details** to a person the client maps, answered to them alone. See [steps, command output and run details](#steps-command-output-and-run-details) |

Use the connector for several apps per workspace, and where a public URL is
fine. Use the Slack app for the console's experience in Slack,
or where nothing may listen on a public address. Do not point both at the same
Slack app: both would answer.

The app acts for a person only as a client whose `slack.machine` is its
credential and whose `slack.people` maps them. An app the server starts
(`surfaces.slack`) gets that client from the Platform document: `people`, and
under a sign-in other than `tailscale` also `machine`, make the server keep
the record `slack-app-<team>` and start the app with `HUMAGEN_CLIENT` naming
it. With no `people` there is no record and no `HUMAGEN_CLIENT`, and the app
answers nobody. That record has no bot token, so files attached in Slack are
not read through it. See
[the spec](spec.md#whom-the-slack-app-acts-for).

## Who can start a run

Only a Slack person the app's client maps. Anyone who can message the app can
reach it: a new member of the workspace, a guest, or someone from another
company in a Slack Connect channel. So the app refuses everyone else, and so
does the server.

A person may also [link their Slack account](connections.md#accounts-a-person-links)
in the console. A link records their Slack user and team ids beside their
subject. It is not read here yet: a linked person the client does not map
starts nothing.

| Entry | A mapped person | Anyone else |
|---|---|---|
| A direct message, a mention in any channel, a reply that mentions the app | Asks as themselves (`act_as`) | Told privately, nothing posted or run |
| A reply that mentions nobody, in a session the app listens to | Offered to the agent as theirs | Dropped, and not answered |
| `/agent`, `/skill`, a skill command, the shortcuts and their forms | Asks as themselves | Told privately; `/agent connections` still says how to be connected |
| A button: Approve, Retry, Resume, Stop, an answer, a mode, a move, a rating | Acts as themselves | Told privately, nothing read or done |
| **Ask agent about this** on someone else's message | Asked for nobody, at their asking (`asked_by`) | Told privately |

A person is mapped when the client the app is (`HUMAGEN_CLIENT`) names the
app's credential as its `slack.machine` and its `slack.people` maps their
Slack user id. A delivery that names another team for the sender (Slack
Connect) is nobody, whatever the map says. An app with no client answers
nobody. There is no setting that opens the app to everyone.

The reply says the person's Slack account is not connected and to ask a
workspace admin to connect it. It is ephemeral where the person wrote, the
command's own reply for a command, and a direct message from the app where
there is no channel (the Home tab, a global shortcut). One person is told at
most once in ten minutes, and the app tells at most twenty people a minute,
so the reply cannot be used to flood a channel or Slack's rate limits.

The server holds the app's credential to the same rule, whatever the app
sends, so an old or forged app starts nothing either. `POST /api/runs` from a
credential a Slack client names as its `slack.machine` (or the `slack-app:`
credential the server makes) needs `act_as` or `asked_by` naming a person the
client maps who holds `runs:write`; without one it answers 403. A run at a
person's asking acts for nobody and records them as `requested_by`. The same
credential makes no call (`POST /api/runs/{id}/calls`), runs no assignment
(`POST /api/assignments/{id}/run`), tests no skill and asks no failed turn
again through the inbox. A stop that names no Slack user stops nothing. Runs the server starts itself
(schedules, automations, housekeeping) are not affected.

### Who can start and stop a run on the webhook connector

The webhook connector follows the same rule: it starts and continues runs
only for a person the client's team maps, writing from the client's own
Slack team.

| Entry | A mapped person | Anyone else |
|---|---|---|
| A mention, a direct message, a message with a file | Starts a run that acts for them | Acknowledged to Slack, nothing read further, posted or run |
| A reply in a thread the client answered in | Continues with the agent that answered | Acknowledged, nothing run |
| A slash command (`/connectors/slack/commands`) | Starts a run that acts for them | Told privately that their Slack account is not connected, nothing run |
| A button or shortcut, where a program serves the connector's interactivity path (`humagen serve` does not) | Read as before (the connector acts on none today) | Acknowledged, not read |
| The stop button, or a raised-hand reaction on an answer | Stops the run | Stops the run only if they asked for it |

A person is mapped when an enabled Slack client of the workspace serving
the client's team maps their Slack user id in `slack.people` (see
[clients](clients.md#people-a-slack-app-answers-for)); the map may be on
another client of the same team than the one the message reached. A user two
clients map to different people is nobody. The sender's team is the one
Slack names for them (`user_team`, then the event's `team`, then the
delivery's `team_id`; for a command its `team_id`; for a button the user's
`team_id`, then the payload's team), and it must be the client's `identity.team`: a mapped user id
from another team, or a delivery that names no team or names one with
something that is not a string, is nobody; a verified delivery the connector
cannot read is acknowledged and goes no further. A client
that names no team, and a connector an embedding program passes as a
`Connection` (which has no client record, so no team and no people), start
nothing. The default client made from the `HUMAGEN_SLACK_*` variables has no
people of its own and is not changed through the API: its team's people come
from another Slack client record of the same team, such as the one a Platform
document's `surfaces.slack.people` keeps. A bot's post never starts a run,
and an app's bot user is asked about like any other user id.

The connector decides after it verifies the request's signature and before
it hands the request on, for every message and mention it would raise,
whatever its subtype, and for every command and button. So for someone it
does not map no file is fetched, no reaction is added, nothing is posted and
nothing is kept about the channel. A plain message in a channel, or a reply
in a thread the client did not answer in, asks for nothing and nobody is
looked up for it. The platform decides again when it files the run, with the
sender's own team as the connector read it: the mapped person needs
`runs:write` from the workspace's policy and membership where the run is
filed, as before. The not-connected reply to a command is ephemeral, sent
to one person at most once in ten minutes and to at most twenty people a
minute. There is no setting that opens the connector to everyone.

A run is stopped from Slack by a person the client's team maps, or by the
Slack user who asked for it, who keeps that though the map no longer names
them. A press or a reaction from anyone else is acknowledged and changes
nothing: the run carries on, nothing is posted and the session's status
stays, in a session with no run known too. `Stopped by @user.` is posted
only for a run that was asked to stop: a run answering in the session that
the client did not raise is not stopped from Slack, and a press there posts
nothing and leaves the status, whoever pressed. See
[stopping a run](#stopping-a-run).

The rest of this page describes the connector. The app is described in its
[README](https://github.com/urmzd/humagen/blob/main/apps/slack/README.md).

## What the API lacks for the Slack app

The app works around each of these and says where in its README.

| Missing | Effect today | Proposal |
|---------|--------------|----------|
| `actor` on `POST /api/runs` for anyone | Runs are recorded as the Slack person the app's client maps (`act_as`, see [clients](clients.md#people-a-slack-app-answers-for)), and nobody else asks | Built for mapped people. A Slack person nobody mapped starts nothing |
| `client` on `POST /api/runs`, or a machine bound to a client | The client's run limits and allow-list are not applied by the platform, except that `*` for a person the client acts for (`act_as`) asks only the client's agents whether to join. The app sends `*` only for a client whose list is `*` | Bind a machine credential to one client (a `client` claim, or a `client_id` on the token mapped in the client record) so the platform places its runs as it places webhook runs |
| `metadata` on `POST /api/runs` | The Slack team, channel and thread a run answers are not on the run; the app derives them from the run id instead | Accept a bounded `metadata` map (string keys and values, a few hundred bytes) and return it on the run, so a surface can find its own place without encoding it in the idempotency key |
| A router preview that knows a client's allow-list | A client with a list and no default answers nothing, and a default of `*` is not sent for a client with a list: the platform would ask agents past it | `GET /api/join?client=` and the same rule in `POST /api/runs` |
| The run a retry started, in the resolve response | Retry from Slack starts its own run and acknowledges the inbox part | Return `{message, run}` from `POST /api/inbox/{id}/parts/{part}/resolve` |
| Feedback with comments | Built: like and dislike open a comment modal and save the rating even when skipped | `PUT /api/runs/{id}/feedback` stores a run reference, rating, reason and comment; the run provides context; `GET` collects paginated records |
| A modal to type a reply to a question | Built for a question in the inbox, a mention in a person's direct messages, and Something Else on a run's own card in its thread | None |
| A consequence and a recommended mark on a question's options | Not shown: `Question.options` is a list of labels | Options as objects with a label, an optional one-line consequence and an optional recommended flag |
| The approval agent's fields and references on the run's question | The card in the run's thread says what the agent decided and why: an allow or deny from the run's answer, and any judgement from the inbox while the app follows the run. A card the app no longer follows (after a restart, or a run that went quiet) shows only the reason from the answer | Carry `judgement` on the run's question as the inbox part does |

### Steps, command output and run details

What a turn's work looks like in Slack depends on how the turn was written.

| | While the turn is going | Once it has ended |
|---|---|---|
| Streamed natively | Slack's own task cards, one per tool call, from `chat.appendStream` | The same cards stay. The final blocks add what is below |
| Posted and edited (streaming refused, a bridged outcome, a run that ended while the app was away) | One line per run of tool calls, in order. The last says what is running now | One line that counts the work by kind: `Ran 3 commands (1 failed), read 2 files, updated 4 files (+120 -18), ran 2 searches` |

The count uses the kinds every tool line uses (`lineKind` in
`packages/components/src/timeline.ts`, and `fileEdits` for files written), in
the order each kind first came. A turn that made one call still says what it
was pointed at (`Read docker.go`). A call to another agent keeps its own row
either way, and a turn with a plan keeps its plan.

**Command output.** The platform's `shell` tool answers with `exit <code>` and
then the end of the command's output, for a command that failed as for one that
passed, so a non-zero exit arrives as a completed call
(`run.delta.tool.done.v1`). The app reads the command from the call's arguments
and the exit code from that first line (`commandResult` in
`packages/core/src/command.ts`):

- A command that passed stays a count. Its output is not shown.
- A command that failed, and that no later call ran again with success, is
  shown under the answer with its command, `exit <code>`, how long it took and
  the last 40 lines of its output. This holds for a streamed turn too.
- The newest three are shown. Earlier ones are counted in a line above them.
- The exit code is shown only for a failure. A command tool that reports no
  exit line is shown as failed only when the call itself failed, with no code.
- The working directory is not shown: the stream does not carry it.

A bridged outcome carries no steps, so it shows none of this.

**Run details.** A turn never shows tokens or cost, since a whole channel reads
it. A finished, failed or stopped turn offers **Run Details** instead, when the
app's client names the app as its `slack.machine` and maps at least one person.
A click answers the person who clicked, alone, in an ephemeral message in the
thread:

| Shown | From `GET /api/runs/{id}` |
|-------|---------------------------|
| Agent | `agent` |
| Model | `usage.context.model`, the model of the run's latest call |
| Input tokens, and how many came from cache | `usage.total.prompt_tokens`, `usage.total.cached_prompt_tokens` |
| Output tokens | `usage.total.completion_tokens` |
| Cost | `usage.total.cost`, in its own currency. "At least" when `unpriced_calls` is not zero or some calls were priced in another currency. "Not priced" when there are tokens and no cost, never zero |
| Elapsed | `updated_at` less `created_at`, for a run that has ended. It includes time the run waited on a person |

`total` counts the run's own model calls and the attached runs it waited on. A
figure the run did not record has no row.

The run is read as the person who clicked (`act_as_client` and `act_as_user`),
never as the app's machine, so Humagen decides: a person sees a run's usage in
Slack only where they may read the run. A person who may not is told they have
no access, with no figures. A Slack user the client maps to nobody is told
their account is not connected, and Humagen is not asked. A bridged
outcome does not offer the button.

### Questions a run waits on

The Slack app shows a run's question in the run's thread: the streamed message
is closed with Humagen's approval or choice card, so its buttons can be pressed.
A click answers through Humagen (`answerQuestion`) as the person the app's
client maps the Slack user to; a Slack user it does not map is told their
account is not connected, and Humagen is not asked. Humagen decides the rest:
who may answer is the run's spec's to say, and a refusal is shown with Humagen's
`noPermission` copy. The card settles in place from the run's own events,
whoever answered, and the answer continues in a new message. A question's inbox
message is not posted in the thread again. Configure the client with the app's
credential and the people it answers for:

```json
"slack": {"machine": "<subject of the app's machine credential>", "people": {"U01ANA": "ana@example.com"}}
```

Under `tailscale` sign-in the app's subject is `slack-app:<team id>`, such as
`slack-app:T0123ABCD`, and a mapped person is `tailscale:<login>`.

**How the cards are drawn.** In a message, the app uses Slack's own blocks
(`packages/render-slack/src/native.tsx`):

| What waits | Block | Buttons |
|------------|-------|---------|
| One approval | `card`: "Approval needed" as its title; the whole action, its preview and the approval agent's line as its body; who may act or the record as its subtext | Approve, Ask and Decline on the card. Edit, Always Allow and **Set this thread to Auto** in a row under it |
| Several approvals | A line, then a `carousel` with one card for each | Each card decides its own call. A menu with **Set this thread to Auto** under the carousel |
| A question | `container`: "Input needed" as its title, the whole question first | One button for each option, I'm Not Sure, and a field for an answer of the reader's own that sends on Enter. A question with no options keeps Something Else, which opens the reply form |
| An inbox alert | `card`: the service and its state as its title, its status as its subtitle, what is wrong as its body | Acknowledge and Snooze on the card, Open Rule under it |

Slack shows a card's or a container's title on one line, so the title only
names the kind of card, and what is being approved or asked is in the body,
whole. A card holds as many button labels as it shows uncut (24 characters,
measured in Block Kit Builder), so Ask a Question reads **Ask** there and is
read aloud in full. A decided card stays in place with its record and no buttons. A modal and
the Home tab keep the sections and buttons. A text longer than a card holds
is never cut: that card is drawn as sections. When Slack refuses a message's
blocks as `invalid_blocks`, the app sends the same message again as sections,
and then as its text alone (`apps/slack/src/fallback.ts`).

The carousel offers nothing that acts on all its approvals at once: the app
has no handler for Approve All, Approve Selected or Cancel All yet. The
checked-list layout a modal or a refused carousel shows still has them.

**Ask a Question** on an approval opens a form for one question. Sent, the
question is said in the thread (who asked, about which action, and what)
and asked of the agent that made the call, as a message typed in the
console while a run waits is: by a mapped person as their own message in the
conversation (`POST /api/threads` with `act_as`), by anyone else through the
app, as a mention there would be. It is answered beside the call, not behind
it: the call's card keeps waiting with its buttons, and the answer streams
below it. Deciding stays with Approve and Decline. In a bound channel only a
mapped person may ask.

**The approval agent.** A conversation has two approval modes, as in the
console: **Ask**, where a person approves every gated action, and **Auto**,
where the approval agent decides gated actions, answers what the conversation
already settles, and asks a person when unsure. In Slack, an approval waiting
in a run's thread has a menu after its buttons with one row, **Set this
thread to Auto**, where the app's client maps people. It sets
the conversation the thread is (`PUT /api/threads/{id}/approval` with
`act_as`), with the same checks as the console's toggle, made as the person
who pressed: Humagen decides whether they may, and its refusal is said to
them alone. A Slack user the client maps to nobody is told their account is
not connected, and Humagen is not asked. The change is said in the thread, naming who made it:
the approval agent approves and answers from the next action on, as that
person could, and an action already waiting still waits for a person. Under
it, **Switch to Manual** sets the conversation back, and that change is said the
same way, with **Switch to Auto** under it.

A call the approval agent decided settles its card with what it decided and
why, as the inbox shows it (`Approval agent: Allowed: "<reason>"` beside who
set the conversation to Auto). What it said about a call it handed to a
person is put on the card while it waits, from the inbox, when the app is
following the run.

The webhook connector shows a parked run as a suspended session and does not
take answers.

### Coworking: channels, threads and mentions

A project's owners bind a Humagen channel to a Slack channel the app's bot is
already in (`PUT /api/projects/{name}/channels/{channel}` with
`slack: {client, channel}`, or `slack:` on the channel in the Platform
document). Binding one, or moving a binding, also takes `clients:write`: a
binding files every new thread in that Slack channel in the project, so which
channels a project answers in is the client's admins' to decide. An owner
keeps or removes a binding an admin made. One Slack channel is bound to one channel, a direct message never,
and archiving a channel unbinds it. `GET /api/clients/{id}/bindings` lists a
client's. A client's `home` (a project and one of its channels) is where
everything else is filed.

#### When the app listens

The Slack app listens on a mention, and after one. Nothing else is heard.

| In Slack | What the app does |
|---|---|
| A direct message | Answers. Every message there is addressed to it |
| A message that mentions the app, anywhere in it | Answers that message |
| A mention in a channel thread's first message | Answers, and once the answer is in, the thread is **listened to** (`listening` on the thread). A refused mention, a failed run, or one still quiet or waiting on someone starts no listening |
| A later reply in a listened thread that mentions nobody, from the person whose mention the app answered most recently there | Reads it and asks the agent whether it calls for it (`POST /api/threads` with `listen`). The agent answers, or the message is kept as what was said and nothing shows in Slack |
| A mention in a reply of a thread that did not begin with one | Answers once, with what was said in the thread since it was last asked. The thread is not listened to |
| A reply that mentions nobody in a thread that is not listened to, or from anyone else in a listened one | Not heard, and not kept (409). A late answer to an older mention does not hand the thread back |
| A reply that mentions nobody, which Humagen could not take (an error, or no answer from it) | Nothing shows in Slack: no notice, no reaction, no status. It is logged. A mention or a direct message that fails is told why |
| A reply that tags only other people, a user group, or the whole channel (`@here`, `@channel`, `@everyone`) | Not offered: it is addressed to them. One that also mentions the app or names an agent is asked |
| A channel message that mentions nobody, outside a thread | Not heard. In a Slack channel bound to a project it is recorded there, as every message in a bound channel is |
| A message from the app itself, another bot, an edit or a join | Never starts anything |

The whole message is sent, as the person wrote it. The app's own mention tags
nobody: one that opens the message is the address and is dropped, with the
colon or comma after it, and anywhere else it reads as the app's name, so
"could @Humagen take a look" asks "could Humagen take a look".

#### Naming an agent

`@Humagen`, the app's bot user, is the one trigger, as `@humagen` is on
[GitHub](github.md#who-a-comment-asks). Agents are not Slack users, so Slack
cannot offer their names. When the message opens with the app's mention, type
the agent's name right after it, after `ask`, with `:` or `,` after it, or as
`@name`:

| Message | Who answers |
|---|---|
| `@Humagen what changed?` | The default: the thread's agent, the client's, the bound channel's, else the entry agent |
| `@Humagen ask research-bot why is the cache cold?` | research-bot, which reads `why is the cache cold?` |
| `@Humagen research-bot: why is the cache cold?` | research-bot |
| `@Humagen @research-bot why is the cache cold?` | research-bot |
| `@Humagen docs are stale` | The default: a bare first word is part of the question |

The first word names an agent only when it is one the client may route to.
Only a message that opens with the app's own mention is read this way. In a
direct message, a thread reply, or a message that opens with someone else
(`@Ana research-bot: ...`), the first word is part of the message. A plain
`@name` of an agent anywhere in a message still tags it, as before. The
connector and the Slack app read the name the same way.

Only a person the client maps is listened to. The agent's answer to "does this
call for me?" is the one a message that names no agent is routed by
([overview](../architecture/overview.md)): a no, a failure and no agent to ask
all leave the message unanswered.

A thread is filed where its Slack channel is bound, else in the client's
`home`. A client with no `home` files in the workspace's root, in no project,
so no project lists its sessions: give it one (`PUT /api/clients/{id}`, or the
client's page in the console). A person's own project is not a place a client
can file in: a machine never reads one, so the app could not show the answer.

For a person the client maps, the Slack app posts each message into the Humagen
thread its Slack thread is, as that person (`POST /api/threads` with
`surface {client, channel, ts, message, app_user}` and `act_as`). Only the
client's `slack.machine` may, and the platform decides where the thread goes:

| Slack | Humagen |
|-------|------|
| A thread in a bound channel | A shared thread in the bound channel |
| A thread in an unbound channel | A shared thread in the client's home |
| A direct message, or the assistant pane | A private thread in the client's home |

Outside a bound channel the Slack thread is a root session (`POST /api/sessions`
with `surface`), filed in the client's home. Its audience follows the Slack
conversation the app reports, and a thread that reports none is private to the
person asking. The answer goes back to the same Slack conversation in every
case: a public channel, a private channel, a group direct message or a direct
message. Before each write the app asks `GET /api/clients/{id}/delivery` with
the channel and the thread's `ts`. The server allows a private run in a Slack
channel only in the Slack thread that started the run's session: the session,
or the root of its session tree, has that Slack thread as its surface on this
client. It answers in no other thread, and a request without `ts` is refused.
Only the client's machine starts a session with a surface, so a private
session started anywhere else, such as in the console, is never posted in a
channel. When the server refuses, the app posts nothing and logs it once for
the run: `run <id> not delivered to <channel>: <reason>`.

A session in that tree belongs to the Slack thread, even where the console
shows it as private. This includes a child an agent hands work to and a child
a person opens under it in the console. Its answers are posted in the Slack
thread. So text or a file a person adds to such a session in the console can be
quoted there, in front of everyone in that Slack conversation. No session in
the tree is the person's own private conversation, so tools that use a person's
own credential or show their connection status and consent links
(`list_connections`, `connect_connection`, `catalog_query`, a person's own
`catalog_search` entries) refuse in all of them. Ask for those in a private
console session of your own. Corpora are read for the Slack thread's
audience in every session of the tree, and no session of the tree is published
or moved into a project.

The platform reads Slack's text in the one tag grammar: the app's own mention
is not a tag, a bot in `client.bots` is its agent, a person in `slack.people`
is their handle, and anyone else stays plain and is refused. When the app is
mentioned in a thread, the people's messages since it was last asked there are
posted first, each as its person and posted only, so the room in Humagen reads as
it did in Slack. Each message is posted once by its ts: a redelivered event
answers `repeated` and starts nothing again. A channel thread whose first
message mentioned the app is listened to once the app has answered there
([When the app listens](#when-the-app-listens)): a reply that mentions nobody,
from the person whose mention the app answered most recently there, is offered
to the agent, and replies from anyone else start nothing, so people talking to
each other in the thread are never answered. Only the newest answered mention
moves whom the thread listens to; the listener's unmentioned replies never do.
A bound channel whose client's own subscriptions name channel messages has
them mirrored as before, and none is offered. A thread stays with the agent it was asked for: the one agent a
message tags, once it answers, becomes the thread's selected agent, and the
thread's selected agents answer its untagged messages ahead of any default,
until a message tags another (RFC 0012). Outside a bound channel the
client's default answers an untagged message in a thread nobody tagged; the client's allow-list holds
everywhere, and for a default of `*` only the agents on a client's list are
asked to join. `@auto` tags nobody. The machine reads every thread on its
own surface, wherever it is filed, but a person it posts or answers for is held to where they may
read: someone who may not read a project's runs answers no question there,
even on a card in a channel bound to it. A bound channel is a project's
channel, so only people the client maps post there, as everywhere (see
[who can start a run](#who-can-start-a-run)), and a mapped person asking
about someone else's message there is told to ask in their own words. When
Humagen cannot say which channels are bound, the app answers nobody it would
ask for nobody: the turn is refused as Humagen being unavailable, and the
next one reads the bindings again.

An announcement of a project whose general channel is bound is posted there,
top-level, once; the app records where (`PUT /api/inbox/{id}/deliveries/{client}`),
and that receipt is the record of the post, so an edit, a withdrawal or a
restart updates that post instead of posting again. Once the announcement
is withdrawn the post says only "This announcement was withdrawn.", and the
app records the same post again, so the receipt says the post shows the
withdrawal. A draft never reaches a channel, and a post offers nothing that
would set it aside for one reader: it is the channel's. The app's machine
is told of a bound project's announcements wherever it may see the project,
though it is only a viewer there. A members-only project's reach Slack only
where its owners name the machine among its members: binding its general
channel alone sends nothing.

The event stream does not replay what it carried while the app was not
listening, so each time the stream opens, when the app starts and after
every reconnect, the app catches up. For each project whose general channel
is bound to its client it reads the project's newest 50 announcements,
withdrawn ones too, as its machine reads them (`GET /api/projects/{name}/inbox`,
where the machine is shown its own client's receipts and nobody else any),
and compares them with its receipts:

- a post whose announcement changed since its receipt says the post showed
  it (`updated_at` after the receipt's `at`), as when it was withdrawn, is
  edited, and its receipt recorded again;
- an open announcement it has no receipt for is posted, oldest first, when it
  was announced in the last 24 hours and no earlier than the Slack channel
  was bound there (`bound_at` on `GET /api/clients/{id}/bindings`, kept while
  the binding holds and moved when it is made again or moved).

So the release notes Humagen posts at start (`workspace.release_notes`), before
the app connects, reach a channel bound to the workspace's general, once it
was bound before that start. The bounds keep a reconnect to one read a
bound project, and keep from the channel what is no longer news: a post a
day late reads as new, and what was announced before the channel was bound,
or while it was unbound, is not posted at all. What falls outside them
stays in Humagen's inbox and on the Home tab. A change is caught up on only in
the newest 50: an older announcement edited or withdrawn while the app was
not listening keeps its post as it was, withdrawn words included, until a
later notice about it. Only bound projects are caught up on: once a Slack
channel is unbound, or bound to another project, its old posts are left as
they are.

The app catches up, and posts announcements at all, only when its client
names the app's own credential as its machine (`slack.machine`): Humagen shows
the client's receipts to that credential alone, so any other would post
everything again. It logs that it skipped the catch-up and why. A catch-up
that could not read Humagen, or could not bring a post up to date because Humagen
was away, is tried again after 5 seconds, then twice as long each time up to
5 minutes, until one finishes. What would answer the same next time is
logged and skipped, not tried again until the next catch-up: a bound project
the machine may not read (403 or 404), such as a members-only one that does
not name it, a receipt Humagen refuses the machine the same way, and a post
Slack refuses to edit, such as one deleted there. None holds up another. A
notice that comes during a catch-up never posts twice: the app looks at each
announcement one look at a time, reads it afresh each time, receipt and
all, and posts only what has none. A notice posts an announcement with no
receipt only as a catch-up would: open, from the last day, and announced
after the binding. Humagen dates a receipt when it takes it, so after each
receipt the app reads the announcement once more and shows a change made
meanwhile, which no catch-up would see. A post whose receipt Humagen did not
take, as when it went away just then, is recorded at the next look rather
than posted again, while the app keeps running.

A reply in an announcement's Slack thread starts a shared thread seeded with
the announcement. The manifest subscribes to `message.channels` and
`message.groups` so the app can listen in a thread that began with a mention
([When the app listens](#when-the-app-listens)), and the app drops every other channel message that does not mention it unread,
unless the client's own subscriptions name channel messages. So the reply
that mentions the app is the one that reaches Humagen, and the replies before it that did not are posted
ahead of it as the room, each as its person and posted only. The app posts
them as for any Slack thread, with the post's surface: the platform finds
the announcement by the receipt the app recorded at that channel and ts, and
starts the thread in the bound channel with it. Its turns start with the
announcement, from the person who posted it, for a recap the agent the
schedule ran, or for release notes Humagen itself, never from whoever replied;
then comes the reply, the person's
own message, read in the one grammar and answered by the channel's chain as
any message there. What the announcement says tags nobody. A later reply
finds the same thread by its surface. The thread reads the announcement as
it stands, so once it is withdrawn the thread says only that. Someone the
client maps to nobody is told privately that their account is not connected,
as anywhere. A thread is seeded only where the Slack channel's binding
files it in the announcement's own project, so only those who see that
project read it. There a reply under an announcement that was withdrawn
before anyone replied starts nothing, and the app says so (Humagen answers
410); a draft never reaches a channel. After the Slack channel is bound to
another project, or unbound, a reply under an old post starts a thread where
it is now filed, as any other, open or withdrawn: nothing of the
announcement comes with it, and nobody there is told what became of it. So
does a reply under an announcement withdrawn longer ago than the
workspace's inbox retention (`workspace.inbox_retention`, 90 days by
default), which Humagen has dropped, receipt and all.
Once a thread is seeded, the announcement is part of it and of what its runs
are sent, so it is read as they are, with `runs:read`, and not as the inbox
reads it, with `projects:read`.

A mention of a mapped person, or a question addressed to them, reaches the
app's event stream with `for {client, user}`. The app reads it for them
(`act_as_client` and `act_as_user` on `GET /api/inbox/{id}`) and posts it to
their direct messages with Open Thread and Reply, unless it was raised in a
Slack thread of the app's own, where Slack told them already. Reply opens a
modal; sending it posts in the thread as the person (`act_as` on
`POST /api/threads/{id}/messages`), which settles their mention, or answers
the question.

### Background results

A result that [background work](../guides/background.md) sent when it ended
on its own (a watch that fired or ran out, a goal or a task), or that `notify`
sent, reaches the event stream of a client's machine with `for {client,
user}` when the client maps the person it is for. No other result does. When
the work was asked in a Slack thread of a Slack client, only that client hears
of it, and the notice also carries `reply_in {channel, ts, session}`: the app
reads the message for the person, as above, and replies once in that thread
with `<@user>`, the message's subject made unable to ping or link, and Open
Session, which opens `session` (the watch's checks or the task's run) or else
the conversation. The details are never posted there, and the reply is never
edited. The server leaves `reply_in` out, and the app sends the whole message
to the person's direct messages instead, when the work was asked anywhere
else, when the person may no longer read that conversation, or when a run
behind it (the one that asked, or the last check or turn) read private
context, as [a schedule's answer is kept for its creator](spec.md). A
background result is sent to their direct messages even about a Slack thread
they are in: nothing there pinged them. Work a person stopped tells nobody.

The Home tab of a mapped person is the shared Home, everything on it read for
them the same way: their open inbox and announcements (`GET /api/inbox`) and
their projects (`GET /api/projects`, both with `act_as_client` and
`act_as_user`), so it holds only what that person may read. It shows at most
five messages that need them, newest first, with the rest counted as Humagen
counts what may need them (`open`): an agent's draft waiting on them as an
owner is one of them, and a posted announcement is not. They are read with
`needs=true`, which leaves posted announcements out before the page is cut, so
however much news is newer the rows and their count agree. What's new holds
five announcements of their projects, read with `news=true` so an agent's
draft never crowds them off the page, unread first, posted ones only. Your
projects holds the projects that name them, which Humagen marks `mine`: they are
a member or an owner there through an entry for their subject or a
`role:<name>` they hold, on the project itself or as an owner of a project
above it. A project they see only through `*`, or only through
`projects:govern`, is not theirs. At most eight, where something happened most
recently first and then in the tree's order, archived ones left out, and the
rest counted in "N more in Projects", which links the console's Projects. Each
opens its page in the console when the app has the console's address
(`HUMAGEN_PUBLIC_URL`). An address on the app's own machine, such as
`localhost` or `127.0.0.1`, counts as none: nobody else in Slack can open it,
so the app says so at start and posts no console links. Without one, what needs them has Review, which sends the
message to their direct messages with the app, where it can be answered, and
What's new and Your projects are shown to read. A press on what Review sent is
the person's own, done for them through the client even where the app may read
the message too, and Retry asks the run again in its own thread. Neither it
nor a mention's message offers Snooze or Dismiss: Humagen does those only as the
caller, so in Slack they would be the app's. Ask a Question and Build an Agent
open a form, and the words are posted as the person in their direct messages
with the app, as the shortcut's are; Build an Agent tags `@humagen`, and
is offered only where the client may reach it. Search Context opens the
console. The tab shows no access or connections, which the app cannot know for
the person. Their projects are left out when they cannot be read: the app's
machine needs `humagen:projects:read` for them, and the person `projects:read`.
Someone the client maps to nobody sees the mailbox: an operator the whole inbox
the app may read, anyone else nothing, as they asked for nothing. They act on
none of it: a press is told their account is not connected.

The webhook connector files its runs by the same binding: a run in a bound
Slack channel is in the channel it is bound to, anything else in the client's
home, whatever the payload said. A mention that names no agent is answered as
it would be in a thread there: in a bound channel by the bound channel's
default and then its project's, never the client's; in the home by the
client's default first.

The Slack connector accepts signed mentions, direct messages and code channel
slash commands. Responses use one message per run, edited as events arrive.

## App definition

[`examples/slack/manifest.json`](https://github.com/urmzd/humagen/blob/main/examples/slack/manifest.json) declares
the app: an agent view, a bot user, code channels, and the scopes and events the
connector uses. Replace `YOUR_HOST` with the public address of `humagen serve`,
then create the app from the manifest at <https://api.slack.com/apps>.

| Manifest entry | Why the connector needs it |
|----------------|----------------------------|
| `app_mentions:read`, `app_mention` | A mention in a channel raises a run. |
| `im:history`, `message.im` | A direct message raises a run. |
| `channels:history`, `groups:history`, `mpim:history`, `message.channels`, `message.groups`, `message.mpim` | The replies in a thread that began with a mention, which the app listens to. |
| `chat:write` | Post and edit the run's message; report session status. |
| `reactions:write` | The `slack_add_reaction` action. |
| `agent_view`, `assistant:write` | Declares the app an agent, which sessions require. |
| `agent_session_stopped` | Shows Slack's stop button on a working session and delivers the press. |
| `code_channels`, `code_channels:manage` | Register and receive slash commands. |

Slack sends events to `/connectors/slack` and command invocations to
`/connectors/slack/commands`. Both are verified with the same signing secret
before anything is parsed.

## Configuration

Each Slack app is a [client](clients.md): its team and app id, its secrets, the
agents it may reach, and the options on this page. A workspace may have several,
and every app posts to the same two URLs. The request goes to the client whose
signing secret verifies it.

For a single app, these variables create the default client, `default-slack`:

| Variable | Meaning |
|----------|---------|
| `HUMAGEN_SLACK_TOKEN` | Bot token (`xoxb-`). Required with the signing secret. |
| `HUMAGEN_SLACK_SIGNING_SECRET` | Authenticates every inbound request. |
| `HUMAGEN_SLACK_TEAM` | Team id the client serves. A request for another team is a 404. |
| `HUMAGEN_SLACK_APP` | App id. Optional for one app, required of each when a team has several. |
| `HUMAGEN_SLACK_BOTS` | `U08BOT=docs-bot` pairs: which agent a bot user id stands for. |
| `HUMAGEN_SLACK_DEFAULT_AGENT` | Answers a mention or command that named no agent. |
| `HUMAGEN_SLACK_SESSIONS` | `true` reports run lifecycle as session status. |
| `HUMAGEN_SLACK_COMMANDS` | JSON array of commands to offer in code channels. |

```sh
export HUMAGEN_SLACK_SESSIONS=true
export HUMAGEN_SLACK_COMMANDS='[{"name":"create-pr","description":"Open a pull request","argument_hint":"[title]"}]'
```

Malformed values fail startup. So do commands Slack would refuse: more than 10,
a name over 31 characters, a leading slash, a missing description, a duplicate.
The same values on a client created through the API are refused with a 400.

| Variable | Client field |
|----------|--------------|
| `HUMAGEN_SLACK_TOKEN`, `HUMAGEN_SLACK_SIGNING_SECRET` | `credentials.token`, `credentials.signing_secret` |
| `HUMAGEN_SLACK_TEAM`, `HUMAGEN_SLACK_APP` | `identity.team`, `identity.app_id` |
| `HUMAGEN_SLACK_BOTS` | `bots` |
| `HUMAGEN_SLACK_DEFAULT_AGENT` | `default_agent` |
| `HUMAGEN_SLACK_SESSIONS`, `HUMAGEN_SLACK_COMMANDS` | `slack.sessions`, `slack.commands` |

The default client may route to any agent. One created through the API routes
to its default agent and its `agents` list, and to nothing else.

## Session status

With sessions on, each lifecycle event calls `agents.sessions.setStatus`:

| Run event | Session status | What Slack shows |
|-----------|----------------|------------------|
| accepted, started, resumed | `processing` | The "Working…" indicator. |
| parked | `suspended` | Waiting on the user. |
| completed, failed, stopped | `active` | Ready for the next prompt. |

A failed or stopped run reports `active` because its error, or its partial
answer marked stopped, is rendered in the run's own message and the thread
remains usable. The connector never reports `closed`.

A session is a thread in an ordinary channel and the whole channel in a code
channel. The trigger cannot tell the two apart, so the sink sends the thread
first and repeats without it when Slack answers `thread_ts_not_allowed`.

### The Slack app's status line

The [Slack app](https://github.com/urmzd/humagen/blob/main/apps/slack/README.md)
also shows one line above the reply box while a run works, as other Slack
agents do: "Checking recent deploys", "Testing Plaud alerts". It is the title
of the step running now, else the plan's title, else "Thinking"
(`lifecycle` in `packages/render-slack/src/lifecycle.ts`), on one line of at
most 50 characters, cut with an ellipsis. Slack documents no length of its
own for it.

`agents.sessions.setStatus` takes only the session state, so the line is set
with `assistant.threads.setStatus` (`channel_id`, `thread_ts`, `status`),
which needs the `assistant:write` scope the app's manifest already has
(`statusLine` in the sandbox's capabilities):

| When | Call |
|------|------|
| The run starts, or resumes after a question | The line, at once, after the session is set to `processing`. |
| The step changes | The new line, only when it differs, and no sooner than 2.5 seconds after the last one. The latest line waiting is sent when the time is up, whether or not anything else happens. |
| The run waits on a person, completes, fails or stops | `status: ""`, at once and before the session status, so no held line follows it. |

A code channel's session has no thread, so it gets no line. A thread that
refuses one (for example where Slack keeps the status to assistant threads)
is logged and not asked again during that turn; the answer never depends on
it. The connector sets no line.

## Stopping a run

Slack shows a stop button on a `processing` session because the manifest
subscribes to `agent_session_stopped`. A press is handled in the order Slack
asks for:

1. Every run answering in that session is asked to stop through the runtime,
   with the reason `Stopped by slack:<user id>`. Runs attached to it are asked
   too; runs it delegated carry on. A stop on a code channel covers every run
   in the channel.
2. `Stopped by @user.` is posted in the session.
3. The session is set to `active`.

The model call in flight is cancelled. The run ends with `run.stopped.v1`,
and fiber's sink edits the run's message to keep what the agent had said,
marked stopped with the reason underneath. From the press until that event,
nothing the run still publishes sets the session back to working, and the
`active` it reports on ending is where the session stays. A run whose turn
finished before the request reached it ends completed, as usual.

A second press, or Slack redelivering the first, finds no run left to stop and
posts nothing. A press that stops none of the session's runs posts nothing
and leaves the status. A person the client's team maps may press it, and so may the
Slack user who asked for the run; a press from anyone else is acknowledged,
stops nothing, posts nothing and leaves the session's status as it is (see
[who can start and stop a run](#who-can-start-and-stop-a-run-on-the-webhook-connector)).
If the runtime cannot carry the request, the press is answered with an error
so Slack redelivers it, and nothing is posted.

A follow-up mention in the thread is answered by a new run. Slack mentions do
not carry the thread's history in Humagen today, so that run is not sent the
stopped turn. A conversation in the console is: see
[Stopping a run](../architecture/overview.md#stopping-a-run). Carrying a Slack thread's
history needs fiber's thread journal (`runtime.WithThreads`), which records a
stopped turn with its partial answer and a fixed marker.

A press reaches only the runs its own client raised. Two apps answering in one
thread each hold their own sessions, and another workspace's app in the same
team cannot stop a run it did not raise.

Which runs a session holds is remembered in memory, per client. After a
restart, or after the client is changed, the button finds no run to stop and
settles the status, for a person the client's team maps.

Check the manifest with the Slack CLI from `examples/slack`:

```sh
slack manifest validate --team <team id>
```

## Slash commands

Commands are registered with `agents.conversations.setCommands` the first time a
run is accepted in a channel. Slack answers `not_agent_channel` for an ordinary
channel; that outcome is remembered, so it costs one call per channel per
process.

An invocation becomes a tag whose text is what a human would have typed:
`/create-pr Fix the flaky login test`. The agent is the one named in the text
with `@name`, otherwise the default agent. The answer is a top-level message in
the code channel. With no agent configured, the invoker gets an ephemeral
message saying so.

## Errors

Slack reports failures as `ok: false` with a code. For status and command
calls Humagen logs permanent refusal codes as warnings and still treats the event as
delivered: the answer is already on the surface, and retrying a refusal would
never succeed. Transport failures and transient Slack server/rate-limit errors
are returned, so the router retries them with its bounded backoff.

| Code | Usual cause |
|------|-------------|
| `feature_disabled` | Agent sessions are not enabled for the workspace. |
| `missing_scope` | The installed app predates a scope in the manifest. Reinstall. |
| `thread_ts_required` | A top-level route in an ordinary channel has no session. |
| `no_actions_url` | The manifest has no `slash_command_url`. |
| `colliding_with_builtin` | A command shares a name with a Slack command. |
| `too_many_commands` | Other agents in the channel already use the 10 slots. |
| `not_authorized` | The app is not the channel's agent bot. |

## Remaining work

- **Creating code channels.** The connector answers in code channels that
  already exist. It does not call `agents.conversations.create`, set context bar
  items, publish views or archive.
- **Unmentioned messages in a code channel.** Only mentions and commands raise
  runs there; `message.session` is not handled.
- **Session titles and renames** are left to Slack and its users.
- **Native streaming.** The connector edits answers with `chat.update`. The
  [Slack app](https://github.com/urmzd/humagen/blob/main/apps/slack/README.md) streams them.
- Command registration, lifecycle replay cursors and the runs a session holds
  are bounded, in memory and per client, like the sink's message handles. A
  restart forgets them, and so does changing the client.

Code channels and agent sessions are new Slack surfaces. This connector is
tested against the documented contract through the in-memory fake and has not
yet been exercised against a live workspace.

## Durable native session feedback

Managed webhook installations show lifecycle with reactions on the original
thread message: eyes while working, a check on success, x on failure, raising
hand when waiting and raised hand when stopped. The app needs reactions write
permission. A reaction failure is retried independently of delivering the answer.
The answer retains native user/channel mentions and HTTPS permalinks.

A plain reply in a thread with a persisted agent answer continues its agent's
session. A standalone thanks in that known thread receives a heart reaction
without starting a model turn. Bot messages and unknown unmentioned channel
threads do not start work.

Signed thumbs-up/down reactions on delivered agent answers save the same
`run.feedback.v1` assessment as console feedback, including reaction removal.
A raised-hand reaction stops the known run for whoever may stop it: a person
the client's team maps, or the Slack user who asked for the run. Anyone else's
is acknowledged and stops nothing. Subscribe to `reaction_added` and `reaction_removed`, and include
them in an installation's explicit event capabilities when those are configured.
Delivery handles and native event IDs persist across connector rebuilds.

Cross-surface `slack_post_message` actions require an explicit destination channel
in `slack.action_channels`. This is an exact channel-ID list; project bindings,
installation capabilities and runtime action policy still constrain the write.
