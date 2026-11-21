# Slack app

A Socket Mode Slack app that answers through Humagen. It is a client of
Humagen's HTTP API and holds no rendering of its own: a run's events go
through [`@humagen/adapter`](../../packages/adapter), the same adapter the
console uses, into one shared UI turn, and that turn is rendered by
`@humagen/render-slack`. A run reads the same in Slack and in the console.

It is the experience of Humagen's Slack sandbox (`apps/slack-sandbox` in
Humagen), driven by real runs instead of fixtures. For how it relates to the Go
webhook connector, see [Slack](../../docs/reference/slack.md#two-ways-to-answer-in-slack).

## Run it

You need a Slack app created from [`manifest.json`](manifest.json), which
asks for what this app uses and nothing more (or from the sandbox manifest,
`SLACK_SANDBOX_SKIP=workflowStep pnpm sandbox manifest` in Humagen, which lacks
`mpim:history` and `users:read`), installed to one team, and a Humagen server.

```sh
cd apps/slack
pnpm install
cp .envrc.local.example ../../.envrc.local   # fill it in, then direnv allow
pnpm start
```

Run `pnpm install` once from the repository root. The adapter and renderers are local workspace packages.

## What it does

| Entry | Where the answer goes |
|-------|-----------------------|
| A direct message | A thread under that message. Each top-level message starts a conversation. |
| A mention in a channel | The mention's thread. |
| `/agent` | Opens the ask form with a picker of who to ask. |
| `/agent <question>` | A new thread in the channel. `/agent help` lists who can be asked. |
| **Ask the agent** shortcut | The app's messages with you. |
| **Ask agent about this** on a message | That message's thread. On your own message it asks as you; on anyone else's, or a bot's, it asks through the app, for nobody, so their words are never run as you. Under a dev sign-in, where the app's own credential is the person `dev`, asking through the app acts for `dev`. |
| Like or dislike | Opens a comment modal immediately. The rating is saved even if you choose Skip. Dislike also offers a reason. Requires a mapped workspace account. |
| `/skill` or `/agent skills` | Lists this client's configured skills. `/skill name arguments` invokes one; code channels also offer each configured slash command dynamically. |
| `/agent connections` | Opens your private provider status and connect/disconnect controls. Supply an allowed secret reference; keep tokens out of Slack. |
| `/agent canvas RUN_ID` | Publishes a finished answer from this channel as a Slack canvas, shared with the channel (or just you in a DM). |
| A plan, in a code channel | The turn's plan is kept as a checklist on one canvas, attached to the channel as the **Plan** tab, where people may comment and only the app writes. See [The plan tab](#the-plan-tab). |
| Uploaded files and images | A mapped person's triggering message is reread through its client on the server and ingested as agent attachments, including file-only DMs. Unread files are explained to the agent. |
| **Run Details** on a finished turn | Shows you alone, in an ephemeral message, the run's agent, model, tokens, cost and elapsed time. Offered when the app's client maps people; the run is read as you, so you see it only where you may read the run. |
| Stop button | Stops the run, confirms in the thread, sets the session to active. |
| Home tab | For a person the client maps, the shared Home read as them: what needs them, what's new in their projects and their projects, with Ask a Question, Build an Agent and Search Context. Anyone else sees what is waiting on them in Humagen's inbox. |

A turn streams with `chat.startStream`, `appendStream` and `stopStream`, and
the session status follows it: processing while it works, active when it ends.
While it works, one line above the reply box says what it is doing, such as
"Checking recent deploys": the running step's title, else the plan's, else
"Thinking", set with `assistant.threads.setStatus` and cleared when the run
waits on a person or ends. See
[the status line](../../docs/reference/slack.md#the-slack-apps-status-line).
Where native streaming is refused, the turn is posted once and edited. A
message sent while a turn is running waits for it, as in the console.

In a code channel the session is the channel itself. There a turn also keeps
the channel's context bar to what its run says (the project, the agent
answering, and the conversation in the console when `HUMAGEN_PUBLIC_URL` is
set) with `agents.conversations.setProperties`, and the session is named once
after its conversation's title with `agents.sessions.rename`, when Humagen has
one. A run names no repository, branch, pull request or check, so the bar
shows none. An ordinary thread or direct message makes neither call, and a
channel Slack refuses either for is not asked again; the answer never depends
on them. `apps/slack/src/codechannel.ts` also holds the close of a code
channel (post a summary, close the session, archive with the summary shared
back to where the session began), which nothing calls yet: Humagen has no
event that says a session is over, and the app never archives a channel on
its own.

Bot messages, the app's own messages, edits and joins never start a run.
Deliveries from any team but `SLACK_TEAM_ID` are ignored.

## Feedback and files

Feedback stores the run ID, the person's identity, rating, reason, comment and
update time. The prompt and answer stay on the run; retrieve them through
`GET /api/runs/{id}`, and use its thread and message references for conversation
context. These reads enforce the source records' current access permissions.
`GET /api/runs/{id}/feedback` collects up to 200 records per page; pass `after` from the previous response's
`next` to continue. One person has one latest rating per run. Skipping the
modal preserves an existing comment. The store uses the configured state backend,
so use Postgres to keep feedback across server restarts.

File intake requires a configured Slack client whose bot token can read the
message, and `files:read`. The record the server keeps for an app it starts
(`slack-app-<team>`) has no bot token, so an app run that way reads no files:
a message with a file is refused. Bytes live in the server workspace's `attachments`
directory as tenant-scoped, content-addressed blobs. `saige/flow/attachment` bounds file count,
bytes and fetch time, checks content types, and resolves the references for the
model. The app supplies only a message location, never a download URL or digest.
Files from another person's Slack message are refused. Reinstall or reauthorize
the Slack app after applying the updated manifest to grant file and canvas scopes
and register `/skill`.

Skills use the existing client's `slack.commands` list; the agent receives the
configured command and its arguments through the normal run path. A command
whose name is a skill the project declares (see docs/architecture/projects.md, Skills) also
gives the runs it starts that skill's instructions. Changes appear
in a code channel on its next message after the client cache refreshes (30 seconds).
Ordinary Slack channels use `/skill`; Slack's dynamic command API applies only to
code channels. This does not install new agent skills or grant additional tools.

## Code channels

Off by default. With `code_channels: true` on the Slack surface
(`SLACK_CODE_CHANNELS=1` when the app is run by hand), a finished answer in a
thread or a direct message carries a **Continue in a Code Channel** button.
Nothing decides for the person that a request is coding work: the app knows an
agent by its name and description and a run names no repository, so there is no
signal to trust, and a channel is opened only when someone presses.

Pressing it calls `agents.conversations.create` with a session id made from the
thread (`<channel>-<thread ts>`), so a second press, a redelivery or a restart
asks for the same channel and never a second one, and posts
"Continuing in #channel. Follow along there." in the thread. The person who
asked or an operator may press in a channel thread; in a direct message it is
theirs already.

The conversation does not change, only where it is shown. A mention in the code
channel is keyed by the thread the channel was opened from, so it is the next
turn of that same Humagen conversation, and the thread keeps working too. The
link from the channel back to its thread is read from Slack
(`conversations.info`, the channel's `agent_session.origin_link`), not stored.
The first turn in the channel carries what people said in the thread since the
app was last asked there, as [thread history](#thread-history) does. The
channel's stop button stops the turn in flight in it, and Retry pressed there
asks again there.

Turning it on changes the manifest: `humagen slack manifest -f humagen.yaml`
then adds the `code_channels` feature and the `code_channels:manage`,
`channels:read` and `groups:read` scopes. Push the manifest and reinstall the
app, or Slack refuses the call. Every refusal leaves things as they were:

| Slack says | What happens |
|---|---|
| `agents.conversations.create` is refused for a reason that will not pass by itself (a missing scope, a plan or an app without code channels) | The person who pressed is told the thread carries on, nothing is posted, and the button is not offered again until the app restarts |
| It is refused for a reason that may pass (rate limit, a Slack error) | The person is told, and the button stays |
| `conversations.info` is refused | The channel is answered as an ordinary one, as before this setting; after a missing scope Slack is not asked again |
| The thread cannot be read | The first turn in the channel goes without the thread's messages |

Not moved: a thread in a channel bound to a project, and a thread a mapped
person posts in as themselves (that conversation is the platform's, found by
the Slack thread itself). In a code channel a mapped person asks through the
app as themselves, and files attached there are refused, as they are for
anyone who asks through the app. Inbox items of a moved conversation are still
posted in the thread it started in.

## The plan tab

In a Slack code channel, a turn that has a plan (a plan title and its steps)
is written to one canvas per channel and attached as the **Plan** tab with
comment access (`canvases.create`, then `agents.conversations.setView`). Each
step is a checkbox: empty when not started, empty and marked *in progress*
while it runs, ticked when done. One line above the steps says how the work
stands. Later plans replace the canvas in place
(`agents.conversations.setCanvasContent`), so a comment stays on a line that
did not change.

Before the next turn in that channel the app reads the open comments
(`agents.conversations.getCanvas`) and puts the ones the agent has not been
shown ahead of the question, each with who wrote it and the line it is on,
between two lines that mark them as people's comments on the plan. A comment
goes once: it is recorded as shown when the run starts. At most 20 comments
and 4,000 characters go with one turn, oldest first; the rest go with the next.

Two limits today:

- A plan title comes from the shared turn model (`plan.updated`). Humagen's run
  events do not carry one yet, so no run produces a plan tab until the adapter
  maps one.
- Comments reach a run the app starts itself. A person the client maps posts as
  themselves through `POST /api/threads`, which takes only their own words and
  a Slack message to anchor them; comments wait there until a turn can carry
  them.

Nothing here applies outside a code channel. A direct message is never asked
about. Any other channel is asked once (`agents.conversations.listViews`,
before any canvas is made, so an ordinary channel never gets a stray canvas),
and a channel Slack refuses is left alone for an hour. A refusal never costs a
turn: the plan still streams in the thread as before. It needs no scope beyond
`canvases:write`.

## Thread history

A mention in a channel thread carries what people said in that thread since
the app was last asked there: the messages after the previous mention of the
app (or its own post that opened the thread), up to the one asking, read with
`conversations.replies`. Bots, the app's own answers, joins and other events
are left out. Each message goes ahead of the question with who wrote it, by
display name when the app has `users:read`, else as a mention. At most the
newest 30 messages and 6000 characters go with one turn. A direct message is
always addressed to the app, so it reads no history.

The history goes ahead of the asker's own words, and the run acts for the
asker, as any mention does: they asked in that thread, and the agent reads
what others said there, each with who said it, as it reads a tool's result.
So a request someone else made in the thread is acted on with the asker's
credentials only when the asker's own words ask for it, as "go ahead" does.
Asking about someone else's message with the shortcut is different: their
words are the whole request, so it acts for nobody (above).

The run takes only text, so the history travels in the question and shows as
part of it in the console. Reading a thread needs `channels:history`,
`groups:history` or `mpim:history` for the kind of conversation it is in. A
thread Slack will not give is logged and the question goes alone.

## Staying connected

Socket Mode is reopened on a close or a `disconnect`. A connection can also
die without closing, after the machine sleeps or the network drops, so a
watchdog replaces it when nothing has arrived for `SLACK_SOCKET_IDLE_MS`
(default 120000), or when its own timer fires far too late, which means the
process was asleep. The new connection opens before the old one closes.
Failed attempts back off from one second to a minute, with jitter. After
`SLACK_SOCKET_MAX_ATTEMPTS` failures in a row (default 10) the app exits with
status 1, so a supervisor can restart it.

## Branding

`SLACK_BRANDING` names a JSON file that renames and rewords the app without a
fork. Every key is optional, and without the file the app reads as it always has.

| Key | What it changes | Limit |
|-----|-----------------|-------|
| `name` | The ask form's title (`Ask <name>`), and the name in the default disclaimer | 20 characters |
| `tagline` | The title over the suggested prompts | 150 characters |
| `disclaimer` | The line under every answer. `{name}` becomes the agent that answered | 500 characters |
| `prompts` | `{title, message}` pairs offered in the app's messages and each new assistant thread (`assistant.threads.setSuggestedPrompts`) | 4 prompts, 75-character titles |

The file is checked at startup: an unknown key, a wrong type or a value over
its limit stops the app with every problem listed. See
[`branding.example.json`](branding.example.json). Suggested prompts need the
`assistant:write` scope and the `app_home_opened` and `assistant_thread_started`
events.

## Who answers

1. An agent named as `@name` in the message, when it exists and the app may
   reach it, or `@auto`, which picks among the agents available where the
   message lands, when the app may reach any agent. The name is taken out of
   the message.
2. The agent the thread was last asked for by name: a thread started for
   one agent stays that agent's until another is named. A person the client
   maps posts into Humagen's thread, where the platform keeps the same rule;
   for anyone else the app remembers it, for its last 5000 threads, until
   it restarts.
3. The client's default agent, or `HUMAGEN_DEFAULT_AGENT`.
4. When the app may reach any agent, nobody is named: the app sends no agent,
   and the default the workspace's projects set answers. With none set, Humagen
   answers 409 and the app asks the person to tag an agent, naming one it may
   reach.

`HUMAGEN_CLIENT` is the id of the client record this app is. An app that
`humagen serve` starts is given it by the server, and only when the Platform
document maps people (`surfaces.slack.people`): the server keeps the record
`slack-app-<team id in lowercase>` with that map and its own machine, routing
to any agent with no default, so who answers is as it is with no client. With
nobody mapped it is not set. An app run by hand sets it itself, to a client
made through the API. See
[the spec](../../docs/reference/spec.md#whom-the-slack-app-acts-for).

Nothing is picked in the app. With `HUMAGEN_CLIENT` set, the client record's
default agent and `agents` list apply, read again every 30 seconds. A client
that lists agents and names no default answers nothing: the default Humagen would
use is not known here, so it could be one the list leaves out. For the same
reason such a client never sends `auto`, as a tag or as its default, since
`@auto` picks among every agent available where the run lands.

## Picking who to ask

`/agent` alone opens the ask form with a picker of who to ask: the agents
this app may reach by name, the client's default first, each by the name
people call it and its greeting or description. Left unpicked, the default
answers, as for any question. `/agent` with words asks at once, as before,
and `/agent help` lists the agents.

## Following a thread

In a channel thread it answered, the app takes the next reply from the
person who asked it there last as asked, though it does not mention the
app; anyone else's replies start nothing. It reads channel messages for
this alone (`message.channels`, `message.groups`): any other channel
message that does not mention it is dropped unread, unless the client's
own subscriptions name channel messages. Which threads it follows is kept
in memory, for its last 5000, and forgotten on a restart. A bound channel is
never followed.

## Each agent in its own name

One app answers for every agent, and it posts each turn as the agent that
answers it: the answer, its stream, and the status line above the reply box
carry the agent's display name and its picture (`username` and `icon_url`,
which Slack takes on `chat.postMessage`, `chat.startStream` and
`assistant.threads.setStatus` with the `chat:write.customize` scope the
manifest asks for). Slack still marks every one APP and names the app on
hover. Both come from the agent's look, set on its Look tab in the console
(`pkg/agentlooks`), which `GET /api/agents` carries: the picture is the one
the console drew when the look was saved, served at an unguessable address
under the console's public URL, so an agent shows a picture only where the
server has a `public_url`. An agent nobody gave a look is posted as the app,
as it always was. An install from before the scope, or a workspace that
refuses it, has its call made again as the app, and the rest of that turn
is the app's.

Home's Meet your agents lists the agents this app may reach that someone
gave a greeting or starters, each with how to ask it here.

## Identity and permissions

The app authenticates as a machine. Under `tailscale` sign-in, where no
process can sign in from outside, the server that starts the app makes its
credential: a token new at every start, with the scopes below and the subject
`slack-app:<team id>`
([security](../../docs/architecture/security.md#the-slack-app-under-tailscale)).
An app run by hand has no credential in that mode. Least privilege:

| Scope | For |
|-------|-----|
| `humagen:runs:read` | Follow runs, read conversations and the inbox |
| `humagen:runs:write` | Start, continue, retry and stop runs |
| `humagen:agents:read` | Know which agents exist; see proposed memories |
| `humagen:clients:read` | Only with `HUMAGEN_CLIENT` |
| `humagen:projects:read` | Your projects on the Home tab of a person the client maps. Without it the Home leaves them out |

Every button in Slack acts with the app's rights, so each extra scope lets the
people in the loop do more: `agents:write` keeps or discards memories,
`alerts:read` and `alerts:write` show and acknowledge alerts. Only the person
who asked for the run, or someone in `SLACK_OPERATORS`, may settle an inbox
item. When who asked is not known (after a restart), only an operator may. A
failed run's card is the exception: its Retry is offered to everyone and is
decided as Retry in the thread is (below), so the person a turn acted for may
press it after a restart too, and once the turn is asked again the card is
acknowledged. A reached limit is shown, not raised: raise it in the console.

Retry and Resume ask a turn again (always the app's own, never Humagen's, so its
answer shows in the thread) for whoever the turn acted for, as Humagen's record
of the run says, and are checked against the client's agents again. A turn
that acted for a person is theirs to ask again: asked by anyone else it would
run their words with the presser's credentials, or a tool that acts as them
on a choice they never made. So only a Slack person the client maps to them
may ask it again, and it is asked as them (`act_as`); anyone else, an
operator included, is told so and nothing starts. A turn that acted for nobody is asked again for nobody, by the person
who asked or an operator, and after a restart by an operator only. A turn the
app asked as itself is asked again the same way, through the app, even when
the app's own credential is a person (an empty `HUMAGEN_TOKEN` against a dev
sign-in server, where every run acts for `dev`): asked as it was asked, it
acts for the same one. Each person's press is a run of its own, and pressing
again joins it.

A question a run waits on is shown in its thread with Humagen's card and
buttons. A click is answered through Humagen as the Slack person, only when
`HUMAGEN_CLIENT` names a client whose `slack.machine` is this app's
credential and whose `slack.people` maps them; anyone else is told they cannot,
and Humagen is not asked. The same mapping makes the runs a mapped person
asks for theirs. Something Else opens a modal for the words, sent as the
same person. A press on a card whose question is no longer waiting (answered
elsewhere, expired, its run stopped) changes nothing: the person is told and
the card shows what was recorded. See
[clients](../../docs/reference/clients.md#people-a-slack-app-answers-for).

A refusal or error from Humagen is shown in Humagen's failure words; the
platform's own text goes to the log. A Slack write that fails mid-turn stops
the streaming, and the whole turn is posted when it ends, with the session set
off processing. A run that says nothing for 15 minutes (a long tool call, or a
run waiting on other work) is not failed: its stream is closed as it stands,
the session stays processing, the thread is free for the next message, and the
run's record is read less and less often (at most every 3 minutes) until it
ends, when its answer is posted once in the thread. A message sent meanwhile
follows it in the conversation, as Humagen orders any turn sent after one
that is still running. The slow follow does not survive a restart; the stop
button still finds the run.

## What survives a restart

Nothing that matters is kept here. A Slack thread's conversation is the run
started with the key `slack-<team>-<channel>-<thread ts>`, whose id the app
computes the way the runtime does, from the workspace and the trigger id
`api-<length of subject>-<subject>-<key>`, so it is asked of Humagen, not remembered. Going the
other way, from a run to its thread, reads the `tag_id` of the conversation's
first run. A message's run is keyed by its own timestamp, so a
Slack redelivery joins the run it started, and the app asks Humagen before
starting it. The stop button asks Humagen which runs in the thread are still
going.

Two things are kept on disk. The first is the Slack streams this process has open (channel,
message and thread, nothing else), in `SLACK_STREAM_JOURNAL`, by default
`$XDG_STATE_HOME/warp-slack/streams-<team>.json` (`~/.local/state` when
unset). A process that crashes or is killed mid-turn leaves its streams open;
the next one closes each with a note that the answer was cut off and asks
again, and sets the thread's session to active, before it connects. A close
that fails for want of the network is tried again at the next start. One
journal file serves one process.

Beside it, in `plan-canvases-<team>.json`, is which canvas is each code
channel's plan and which of its comments the agent has been shown: Slack ids
only. Without it the next plan takes up the channel's Plan tab again or makes a
new canvas for it, and open comments are shown to the agent once more.

Kept in memory, and lost on restart:

- Who asked for each run. After a restart only operators can settle that run's
  inbox items or retry it for nobody, and the Home of someone the client does
  not map shows them nothing until they ask again. Whom a run acted for is Humagen's record, so the person a turn
  acted for may still retry it, from the thread or its failure card, which
  their retry acknowledges.
- Which inbox messages were posted to a thread. A change to one after a
  restart is not reflected in the thread.
- The queue of messages waiting behind a turn.
- Whether native streaming was refused.

One gap remains: the first message the app answers in a thread it did not
start (a mention deep in a human thread) is keyed by the thread. If Slack
redelivers it after a restart and after a later message in that thread was
answered, it is answered twice.

## Checks

| Task | Command |
|------|---------|
| Format and lint | `pnpm exec biome check` |
| Typecheck | `pnpm typecheck` |
| Tests, with fakes for Slack and Humagen | `pnpm test` |
| Every Block Kit payload the tests produce, through Slack's `blocks.validate` (network, no token) | `pnpm validate` |
| Against a real Humagen server built from this checkout (needs Go, no model) | `pnpm test:server` |

## Project bridges

Bindings use `two_way`, `inbound`, or `outcomes` direction. Inbound only mirrors
mapped human messages with `post_only`; it does not wake agents or write replies,
reactions or approval buttons to Slack. Outcomes only ignores incoming human
messages. Two-way retains explicit agent routing. Bound output contains final
outcomes or decisions, excluding Activity and retained source metadata.

The host refreshes installation subscriptions and bindings at ingress and output
boundaries. Before publishing a run it calls
`GET /api/clients/{id}/delivery?run=...&channel=...`, using its existing machine
credential. The server checks the actual installation identity, current run
placement and read access, direction, action policy and private-context flags.
A refusal or unavailable guard withholds output. Guest approval remains denied.
These paths have automated native-host tests; live Slack delivery is not claimed.
