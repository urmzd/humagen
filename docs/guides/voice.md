# Voice notes

Talk to Humagen from wherever you are. A voice note is transcribed before the
run it starts, so an agent reads what you said, and the recording stays with
the message. It is off until a workspace admin sets up a voice provider, or
the System spec names a speech kind.

## A workspace voice provider

An admin (anyone who may manage the workspace's clients, `clients:write`)
sets one up in **Workspace settings › Voice**:

1. Choose **Groq** or **OpenAI**.
2. Paste the provider's API key, or give an `env:HUMAGEN_*` reference this
   server grants the workspace (`-secret-refs`). The key is never shown again.
3. Optionally pick the voice, the models and the prices.
4. Press **Try Voice**: the provider speaks a sample and hears it back.

With a provider set up, the console's microphone and voice notes from every
channel are heard by it, and voice mode is on. **Turn Off** forgets the
provider and its key.

What each call costs is counted in the workspace's usage, toward its spend
limits, from the provider's published price unless the admin set another:

| Kind | Hears | Speaks | Defaults |
|------|-------|--------|----------|
| `groq` | $0.04 an hour of audio, at least 10 seconds a request | $22 a million characters | `whisper-large-v3-turbo`, `canopylabs/orpheus-v1-english`, voice `autumn` |
| `openai` | $0.006 a minute of audio | $15 a million characters | `whisper-1`, `tts-1`, voice `alloy` |

A transcription whose length the provider did not say is counted as an
unpriced call, never as free. The API: `GET`, `PUT` and `DELETE
/api/voice/provider`, and `POST /api/voice/provider/check`.

## Voice mode in a session

With a workspace voice provider set up, a session's composer has a voice
mode button beside the microphone. In voice mode you talk with the session's
agent: what you say is heard by the provider and sent as an ordinary message,
marked spoken, so the agent (and the entry agent's routing) answers it as it
would a typed one, with its own model, tools and approvals. The answer is
written in the session as usual and spoken a few sentences at a time.

| Call | What it does |
|------|--------------|
| `POST /api/sessions/{id}/voice/transcriptions` | Hears a recording (its own bytes, at most 10 MiB) and answers its words. Nothing is kept |
| `POST /api/messages/{id}` with `"spoken": true` on the message (or `POST /api/sessions/{id}/messages` with `"spoken": true`) | Sends the words as any message is sent, so it gets the same one answer; `spoken` only marks the message |
| `POST /api/sessions/{id}/voice/speech` with `{"run", "from"}` | Speaks the next few sentences of that run's answer, from offset `from` (0, then each answer's `next`) |
| `PATCH /api/sessions/{id}/messages/{mid}` with `{"text"}` | Puts right a spoken message's words, for its author only, so a misheard word is right in every later turn; the runs it already started are not changed |

What is spoken is read on the server from the answer of a run of that
session, never sent by the client, so voice mode cannot speak anything else
on the workspace's account. It is plain words: no Markdown, a link by its
text, an address as "a link", a table left out, a code block as "The code is
in the session." While the answer is still being written and has no whole
sentence after `from`, the answer is `pending` and nothing is spent.

Each call is counted in the usage ledger, toward the session's project, its
agent and the workspace, and on the session's own `voice` usage (seconds
heard, characters spoken, cost). When a spend limit over any of them is
reached, the calls answer 402 and nothing is sent to the provider. Both take
`speech:write` and a session the caller can see.

## Spoken answers in Slack

A voice clip sent to the app (a direct message, or a thread the app takes
part in) arrives on its run as an audio attachment with no words. When the
run's turn starts, the clip is heard with the workspace's voice provider and
its words go into what the agent reads, marked `[Voice note, english]`, as a
voice note posted through the API is. The words are kept with the run, so a
retried or resumed turn neither hears nor pays for the clip again, and the run
is remembered as answering a spoken message.

When that run completes, the answer is written in the thread as usual, and a
completion step then speaks it and uploads it as an audio clip in the same
thread ("Spoken answer"). It runs once a run, after the run has ended; when
it cannot speak (no voice provider, a spend limit, no words, Slack refusing
the upload) it logs why and leaves the written answer. The Slack app needs the
`files:write` scope (in `humagen slack manifest` and the Slack connection's
OAuth scopes); an installation from before it was added reinstalls to grant it.

`POST /api/runs/{id}/speech` makes the same clip for any caller: the answer
of a run that has ended, as plain words, at most 1,200 characters, a longer
answer cut at a sentence and ending "The rest is in the thread." It speaks
only the run's own words, takes `speech:write` on a run the caller can see,
and is counted like voice mode's calls. It answers 409 while the run works.

Slack huddles are out of reach: Slack's public API has no supported way for
an app to join a huddle or to hear or speak its audio (it offers huddle
status events, and the Calls API for linking a call hosted elsewhere).

## A deployment speech kind

Groq transcribes with Whisper. Set `GROQ_API_KEY` in the server's
environment and add a speech slot to the System document:

```yaml
api_version: humagen/v1
kind: System
metadata:
  name: humagen
spec:
  speech:
    kind: groq
```

That is all: the key reference defaults to `env:GROQ_API_KEY`, the model to
`whisper-large-v3-turbo`, the size cap to 25 MiB and one request to 60
seconds. Every field is in
[the spec reference](../reference/spec.md#voice-notes). The same key can run
agents on Groq's models: add a model of `kind: groq` to a Platform document
and name it as an agent's provider.

```yaml
api_version: humagen/v1
kind: Platform
metadata:
  name: default
spec:
  models:
    - name: groq
      kind: groq
```

## Where you can send one

| From | How | What the agent reads |
|------|-----|----------------------|
| Slack | Record an audio clip in a thread the app answers, or attach an audio file | The message's text, then `[Voice note, english] what you said`. With a workspace voice provider the agent reads the clip's words, and the answer comes back as text and as an audio clip in the thread |
| The console | Press the microphone in the message box, speak, press it again | The words come back into the box, marked the same way, for you to read, change and send |
| WhatsApp, iMessage, SMS | Not yet: the messaging connectors in `urmzd/saige` will carry a voice note as an audio attachment and call the same hook | The same |

A message has at most four voice notes transcribed. A note that is too long,
has no words in it, or reaches a provider that fails or does not answer in
time is said to be so in the message (`[Voice note: clip.m4a could not be
transcribed]`), and the message still posts. Without a voice provider or a
speech kind a voice note is still attached, and the console's microphone
falls back to the browser's own dictation where the browser has it.

## Who can use it

Recording a voice note in the console takes `speech:write`, which operators,
managers and admins hold; `GET /api/voice` says whether it is on. A voice
note posted from Slack is transcribed under the poster's own right to post.
The console's recording is sent to the workspace's voice provider (or the
deployment's speech kind) and not kept by Humagen; a voice note from a
channel is kept as the attachment it arrived as, like any other file.

## For a connector

The hook is `(*platform.Platform).TranscribeVoiceNotes(ctx, tenant, text,
attachments)`. A connector stores the audio as an attachment, as it does any
file, and passes the message's text and attachments; it gets back the text
with each transcript after it. It knows nothing of the channel, so WhatsApp,
iMessage and SMS use it as Slack does. A new speech provider is a kind in the
`speech` slot: `humagen.WithKind(humagen.Speech, ...)` returning a
`speech.Transcriber`.
