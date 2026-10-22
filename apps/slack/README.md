# Slack app

A Socket Mode Slack app that answers through warp. It is a client of
warp's HTTP API and holds no rendering of its own: a run's events go
through [`@warp/adapter`](../../packages/adapter), the same adapter the
console uses, into one shared UI turn, and that turn is rendered by
`@warp/render-slack`. A run reads the same in Slack and in the console.

It is the experience of warp's Slack sandbox (`apps/slack-sandbox` in
warp), driven by real runs instead of fixtures. For how it relates to the Go
webhook connector, see [Slack](../../docs/slack.md#two-ways-to-answer-in-slack).

## Run it

You need a Slack app created from [`manifest.json`](manifest.json), which
asks for what this app uses and nothing more (or from the sandbox manifest,
`SLACK_SANDBOX_SKIP=workflowStep pnpm sandbox manifest` in warp, which lacks
`mpim:history` and `users:read`), installed to one team, and a warp server.

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
| `/agent <question>` | A new thread in the channel. `/agent help` lists who can be asked. |
| **Ask the agent** shortcut | The app's messages with you. |
| **Ask agent about this** on a message | That message's thread. On your own message it asks as you; on anyone else's, or a bot's, it asks through the app, for nobody, so their words are never run as you. |
| Stop button | Stops the run, confirms in the thread, sets the session to active. |
| Home tab | What is waiting on you in warp's inbox. |

A turn streams with `chat.startStream`, `appendStream` and `stopStream`, and
the session status follows it: processing while it works, active when it ends.
Where native streaming is refused, the turn is posted once and edited. A
message sent while a turn is running waits for it, as in the console.

Bot messages, the app's own messages, edits and joins never start a run.
Deliveries from any team but `SLACK_TEAM_ID` are ignored.

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
   reach it. The name is taken out of the message.
2. The client's default agent, or `WARP_DEFAULT_AGENT`.
3. Warp's picker, when the app may reach any agent.

With `WARP_CLIENT` set, the client record's default agent and `agents`
list apply, read again every 30 seconds. A client that lists agents and names
no default answers nothing, because the picker does not know the list.

## Identity and permissions

The app authenticates as a machine. Least privilege:

| Scope | For |
|-------|-----|
| `warp:runs:read` | Follow runs, read conversations and the inbox |
| `warp:runs:write` | Start, continue, retry and stop runs |
| `warp:agents:read` | Know which agents exist; see proposed memories |
| `warp:clients:read` | Only with `WARP_CLIENT` |

Every button in Slack acts with the app's rights, so each extra scope lets the
people in the loop do more: `agents:write` keeps or discards memories,
`alerts:read` and `alerts:write` show and acknowledge alerts. Only the person
who asked for the run, or someone in `SLACK_OPERATORS`, may settle an inbox
item. When who asked is not known (after a restart), only an operator may. A
reached limit is shown, not raised: raise it in the console.

Retry and Resume ask a turn again (always the app's own, never warp's, so its
answer shows in the thread) for whoever the turn acted for, as warp's record
of the run says, and are checked against the client's agents again. A turn
that acted for a person used that person's own tool credentials, so only a
Slack person the client maps to them may ask it again, and it is asked as
them (`act_as`); anyone else, an operator included, is told so and nothing
starts. A turn that acted for nobody is asked again for nobody, by the person
who asked or an operator, and after a restart by an operator only. Each
person's press is a run of its own, and pressing again joins it.

A question a run waits on is shown in its thread with warp's card and
buttons. A click is answered through warp as the Slack person, only when
`WARP_CLIENT` names a client whose `slack.machine` is this app's
credential and whose `slack.people` maps them; anyone else is told they cannot,
and warp is not asked. The same mapping makes the runs a mapped person
asks for theirs. A reply is typed in the console for now. See
[clients](../../docs/clients.md#people-a-slack-app-answers-for).

A refusal or error from warp is shown in warp's failure words; the
platform's own text goes to the log. A Slack write that fails mid-turn stops
the streaming, and the whole turn is posted when it ends, with the session set
off processing. A run that says nothing for 15 minutes (a long tool call, or a
run waiting on other work) is not failed: its stream is closed as it stands,
the session stays processing, the thread is free for the next message, and the
run's record is read less and less often (at most every 3 minutes) until it
ends, when its answer is posted once in the thread. A message sent meanwhile
follows it in the conversation, as warp orders any turn sent after one
that is still running. The slow follow does not survive a restart; the stop
button still finds the run.

## What survives a restart

Nothing that matters is kept here. A Slack thread's conversation is the run
started with the key `slack-<team>-<channel>-<thread ts>`, whose id the app
computes the way the runtime does, from the workspace and the trigger id
`api-<length of subject>-<subject>-<key>`, so it is asked of warp, not remembered. Going the
other way, from a run to its thread, reads the `tag_id` of the conversation's
first run. A message's run is keyed by its own timestamp, so a
Slack redelivery joins the run it started, and the app asks warp before
starting it. The stop button asks warp which runs in the thread are still
going.

One thing is kept on disk: the Slack streams this process has open (channel,
message and thread, nothing else), in `SLACK_STREAM_JOURNAL`, by default
`$XDG_STATE_HOME/warp-slack/streams-<team>.json` (`~/.local/state` when
unset). A process that crashes or is killed mid-turn leaves its streams open;
the next one closes each with a note that the answer was cut off and asks
again, and sets the thread's session to active, before it connects. A close
that fails for want of the network is tried again at the next start. One
journal file serves one process.

Kept in memory, and lost on restart:

- Who asked for each run. After a restart only operators can settle that run's
  inbox items or retry it for nobody, and Home shows a person nothing until
  they ask again. Whom a run acted for is warp's record, so the person a turn
  acted for may still retry it.
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
| Tests, with fakes for Slack and warp | `pnpm test` |
| Every Block Kit payload the tests produce, through Slack's `blocks.validate` (network, no token) | `pnpm validate` |
| Against a real warp server built from this checkout (needs Go, no model) | `pnpm test:server` |
