# Slack agent delivery

## Two ways to answer in Slack

| | Webhook connector | Slack app |
|---|---|---|
| Where | Go, in `humagen serve` (`pkg/integrations/slack`) | TypeScript, `apps/slack`, a separate process |
| How Slack reaches it | Signed webhooks to a public URL | Socket Mode, no public URL |
| How it reaches Humagen | In process | The HTTP API, as a machine |
| A run looks like | One plain message per run, edited with `chat.update` | Humagen components: native streaming, plans, tool steps, failures with Retry, files |
| Agent session status and the stop button | Yes | Yes |
| Code channel slash commands | Yes | No |
| `/agent`, shortcuts, Home tab | No | Yes, from Humagen's sandbox manifest |
| Inbox items about a run | No | In the run's thread, and on Home |
| Several apps per workspace, as [clients](clients.md) | Yes: tenant, limits and allow-list come from the client | Reads one client's default agent and allow-list, in full as the client's `slack.machine`, though they name an agent that lives in a project the machine may not read. A mapped person's messages are the client's (threads with a surface, below); anyone else's runs are not yet recorded as the client's |
| Channels bound to projects | Yes: a run in a bound Slack channel is filed in the channel it is bound to, and an untagged one answered by that channel's chain, else in the client's home | Yes: a mapped person's Slack thread is a Humagen thread filed by the binding, someone the client maps to nobody is refused in a bound channel, and announcements reach a bound general channel, caught up on after a restart or a reconnect, where a reply that mentions the app starts a thread seeded with the announcement |
| Mentions of a person | No | In their direct messages with the app, with Open Thread and Reply, unless they are in the Slack thread already (they wrote there or were mentioned there with `<@U…>`). Experts named by a role are each told |
| Who asked | Recorded on the run, and the run acts for the person the team's clients map the Slack user to | Recorded as the person, and acts for them, when the app's client maps them (`act_as`): their message is theirs in the Humagen thread, and so are the runs it starts. Else as the app, acting for nobody. Tools that act as a person use the person's own credential |
| Questions a run waits on | No | In the run's thread, answered for the person the client maps. See below |
| Tokens and cost | No | No. See [usage on a turn](clients.md#usage-on-a-turn) |

Use the connector for code channels, for several apps per workspace, and where
a public URL is fine. Use the Slack app for the console's experience in Slack,
or where nothing may listen on a public address. Do not point both at the same
Slack app: both would answer.

The rest of this page describes the connector. The app is described in its
[README](../../apps/slack/README.md).

## What the API lacks for the Slack app

The app works around each of these and says where in its README.

| Missing | Effect today | Proposal |
|---------|--------------|----------|
| `actor` on `POST /api/runs` for anyone | Runs are recorded as the Slack person only when the app's client maps them (`act_as`, see [clients](clients.md#people-a-slack-app-answers-for)); anyone else's run is the app's | Built for mapped people. A Slack person nobody mapped stays unnamed on purpose |
| `client` on `POST /api/runs`, or a machine bound to a client | The client's run limits and allow-list are not applied by the platform, except that `*` for a person the client acts for (`act_as`) asks only the client's agents whether to join. The app sends `*` only for a client whose list is `*` | Bind a machine credential to one client (a `client` claim, or a `client_id` on the token mapped in the client record) so the platform places its runs as it places webhook runs |
| `metadata` on `POST /api/runs` | The Slack team, channel and thread a run answers are not on the run; the app derives them from the run id instead | Accept a bounded `metadata` map (string keys and values, a few hundred bytes) and return it on the run, so a surface can find its own place without encoding it in the idempotency key |
| A router preview that knows a client's allow-list | A client with a list and no default answers nothing, and a default of `*` is not sent for a client with a list: the platform would ask agents past it | `GET /api/join?client=` and the same rule in `POST /api/runs` |
| The run a retry started, in the resolve response | Retry from Slack starts its own run and acknowledges the inbox part | Return `{message, run}` from `POST /api/inbox/{id}/parts/{part}/resolve` |
| Feedback with comments | Built: like and dislike open a comment modal and save the rating even when skipped | `PUT /api/runs/{id}/feedback` stores a run reference, rating, reason and comment; the run provides context; `GET` collects paginated records |
| A modal to type a reply to a question | Built for a question in the inbox and a mention in a person's direct messages. Something Else on a run's own card still points to the console | Open the reply modal from the run's card too |

### Questions a run waits on

The Slack app shows a run's question in the run's thread: the streamed message
is closed with Humagen's approval or choice card, so its buttons can be pressed.
A click answers through Humagen (`answerQuestion`) as the person the app's
client maps the Slack user to; a Slack user it does not map is told in Humagen's
words that they cannot, and Humagen is not asked. Humagen decides the rest:
who may answer is the run's spec's to say, and a refusal is shown with Humagen's
`noPermission` copy. The card settles in place from the run's own events,
whoever answered, and the answer continues in a new message. A question's inbox
message is not posted in the thread again. Configure the client with the app's
credential and the people it answers for:

```json
"slack": {"machine": "<subject of the app's machine credential>", "people": {"U01ANA": "ana@example.com"}}
```

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

For a person the client maps, the Slack app posts each message into the Humagen
thread its Slack thread is, as that person (`POST /api/threads` with
`surface {client, channel, ts, message, app_user}` and `act_as`). Only the
client's `slack.machine` may, and the platform decides where the thread goes:

| Slack | Humagen |
|-------|------|
| A thread in a bound channel | A shared thread in the bound channel |
| A thread in an unbound channel | A shared thread in the client's home |
| A direct message, or the assistant pane | A private thread in the client's home |

The platform reads Slack's text in the one tag grammar: the app's own mention
is not a tag, a bot in `client.bots` is its agent, a person in `slack.people`
is their handle, and anyone else stays plain and is refused. When the app is
mentioned in a thread, the people's messages since it was last asked there are
posted first, each as its person and posted only, so the room in Humagen reads as
it did in Slack. Each message is posted once by its ts: a redelivered event
answers `repeated` and starts nothing again. Outside a bound channel the
client's default answers an untagged message; the client's allow-list holds
everywhere, and for a default of `*` only the agents on a client's list are
asked to join. `@auto` tags nobody. The machine reads every thread on its
own surface, wherever it is filed, but a person it posts or answers for is held to where they may
read: someone who may not read a project's runs answers no question there,
even on a card in a channel bound to it. A bound channel is a project's
channel, so only people the client maps post there: anyone else who mentions
the app in one is told to ask an admin to connect their Slack account, and
Humagen is not asked. Elsewhere they ask through the app as before. When Humagen
cannot say which channels are bound, the app answers nobody it would ask as
itself: the turn is refused as Humagen being unavailable, and the next one reads
the bindings again.

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
the announcement. Slack delivers a message in a channel thread to the app
only when it mentions the app: the manifest subscribes to `app_mention`, and
to `message.im` for direct messages alone, and adding `message.channels`
would widen what the app reads. So the reply that mentions the app is the
one that reaches Humagen, and the replies before it that did not are posted
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
client maps to nobody is told to connect their Slack account, as anywhere in
a bound channel. A thread is seeded only where the Slack channel's binding
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
(`HUMAGEN_PUBLIC_URL`). Without one, what needs them has Review, which sends the
message to their direct messages with the app, where it can be answered, and
What's new and Your projects are shown to read. A press on what Review sent is
the person's own, done for them through the client even where the app may read
the message too, and Retry asks the run again in its own thread. Neither it
nor a mention's message offers Snooze or Dismiss: Humagen does those only as the
caller, so in Slack they would be the app's. Ask a Question and Build an Agent
open a form, and the words are posted as the person in their direct messages
with the app, as the shortcut's are; Build an Agent tags `@humagen-builder`, and
is offered only where the client may reach it. Search Context opens the
console. The tab shows no access or connections, which the app cannot know for
the person. Their projects are left out when they cannot be read: the app's
machine needs `humagen:projects:read` for them, and the person `projects:read`.
Someone the client maps to nobody sees the mailbox, as before: an operator the
whole inbox the app may read, anyone else what they may settle.

The webhook connector files its runs by the same binding: a run in a bound
Slack channel is in the channel it is bound to, anything else in the client's
home, whatever the payload said. A mention that names no agent is answered as
it would be in a thread there: in a bound channel by the bound channel's
default and then its project's, never the client's; in the home by the
client's default first.

The Slack connector accepts signed mentions, direct messages and code channel
slash commands. Responses use one message per run, edited as events arrive.

## App definition

[`examples/slack/manifest.json`](../../examples/slack/manifest.json) declares
the app: an agent view, a bot user, code channels, and the scopes and events the
connector uses. Replace `YOUR_HOST` with the public address of `humagen serve`,
then create the app from the manifest at <https://api.slack.com/apps>.

| Manifest entry | Why the connector needs it |
|----------------|----------------------------|
| `app_mentions:read`, `app_mention` | A mention in a channel raises a run. |
| `im:history`, `message.im` | A direct message raises a run. |
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
posts nothing. Anyone who can see the button may press it: the session, not a
console role, is the authority. If the runtime cannot carry the request, the
press is answered with an error so Slack redelivers it, and nothing is posted.

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
restart, or after the client is changed, the button still settles the status
but finds no run to stop.

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
  [Slack app](../../apps/slack/README.md) streams them.
- Command registration, lifecycle replay cursors and the runs a session holds
  are bounded, in memory and per client, like the sink's message handles. A
  restart forgets them, and so does changing the client.

Code channels and agent sessions are new Slack surfaces. This connector is
tested against the documented contract through the in-memory fake and has not
yet been exercised against a live workspace.
