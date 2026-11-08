# Voice notes

Talk to Humagen from wherever you are. A voice note is transcribed before the
run it starts, so an agent reads what you said, and the recording stays with
the message. It is off until the System spec names a speech kind.

## Turn it on

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
| Slack | Record an audio clip in a thread the app answers, or attach an audio file | The message's text, then `[Voice note, english] what you said` |
| The console | Press the microphone in the message box, speak, press it again | The words come back into the box, marked the same way, for you to read, change and send |
| WhatsApp, iMessage, SMS | Not yet: the messaging connectors in `urmzd/saige` will carry a voice note as an audio attachment and call the same hook | The same |

A message has at most four voice notes transcribed. A note that is too long,
has no words in it, or reaches a provider that fails or does not answer in
time is said to be so in the message (`[Voice note: clip.m4a could not be
transcribed]`), and the message still posts. Without a speech kind a voice
note is still attached, and the console's microphone falls back to the
browser's own dictation where the browser has it.

## Who can use it

Recording a voice note in the console takes `speech:write`, which operators,
managers and admins hold; `GET /api/voice` says whether it is on. A voice
note posted from Slack is transcribed under the poster's own right to post.
The console's recording is sent to Groq and not kept by Humagen; a voice note
from a channel is kept as the attachment it arrived as, like any other file.

## For a connector

The hook is `(*platform.Platform).TranscribeVoiceNotes(ctx, tenant, text,
attachments)`. A connector stores the audio as an attachment, as it does any
file, and passes the message's text and attachments; it gets back the text
with each transcript after it. It knows nothing of the channel, so WhatsApp,
iMessage and SMS use it as Slack does. A new speech provider is a kind in the
`speech` slot: `humagen.WithKind(humagen.Speech, ...)` returning a
`speech.Transcriber`.
