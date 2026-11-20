# Read mixed evidence

`examples/media-evidence/humagen.yaml` runs a native agent, `analyst`, on the
files a person attaches to a message: scanned PDFs, photos and images, and
audio a model can hear. It separates what it saw from what a document claims
and from what it infers, and can write its findings to `evidence.md`.

Before #383 this example ran on the Claude Code program with a folder of
evidence on the host, rendered PDF pages and video frames with its shell, and
transcribed audio locally with Whisper. That path is gone. This guide says
what is left.

## What reaches the model

The model reads what is attached to a message: at most five files and 25 MiB
in all. Nothing is read from a folder on the host.

| Attachment | Reaches the model |
|------------|-------------------|
| PDF | As the document itself, with its pages (at most 20 MiB) |
| PNG, JPEG, GIF, WebP | As the image itself (at most 4 MiB each) |
| MP3 or WAV | Heard (at most 20 MiB and 30 minutes) |
| M4A (`audio/mp4`) | **Not as audio.** saige's model catalog lists only `audio/mpeg` and `audio/wav` as audio Gemini takes, so the model gets a note that the file could not be read |
| MP4 video | Listed in the catalog as a type Gemini takes (at most 20 MiB and 10 minutes); not exercised |

The table is for this profile's agent, which runs on Gemini
(`gemini-pro-latest`, with `gemini-flash-latest` as its one fallback). Both
names are matched in saige's catalog by their Gemini family, not as listed
models. Anthropic and OpenAI models read PDFs and images and hear no audio.

One run sends at most 24 MiB of media in all. A file a model cannot take
reaches it as a note saying so, not as its content. A fallback chain takes
only the media types every model in it takes, so the agent has no fallback to
Anthropic or OpenAI: one such model in the chain would turn every recording
and video into a note. `TestMediaEvidenceAgentHearsAudio` builds this agent's
chain and checks that a WAV reaches it as audio.

## What is gone, and what is left for transcription

- **Local transcription is gone.** The Whisper transcription (`mlx-whisper`)
  ran in the Claude Code program's shell on the host. Humagen has no local
  transcription.
- **A model cannot look at what it extracts itself.** `file_read` and the
  shell return text only, so a page or frame an agent renders with a tool
  never reaches the model as an image. This profile grants neither; its
  `unisolated` sandbox refuses the shell anyway.
- **Times a model gave for speech were unreliable** in the earlier live
  check described below. Treat any time
  it gives as an estimate and check it against the original recording.
- **The one transcription path left is a cloud speech provider.** Uncomment
  `speech: {kind: groq}` in the profile and set `GROQ_API_KEY`, or set up
  **Workspace settings › Voice** ([voice notes](voice.md)). Then every audio
  attachment on a message, M4A included, and every MP4 or WebM video, is
  transcribed before the run by that provider's model (Whisper by default)
  and put into the message's text, marked `[Voice note, <language>]`. It
  keeps the words only: no word or segment timestamps. At most four
  recordings a message are heard, the message with its transcripts is cut at
  8,000 characters, and the provider's own limit applies (25 MiB by default).
  The audio is sent to that provider. This path was not part of the live
  check described below.

## Start the analyst

Set `GOOGLE_API_KEY` in the shell. Then:

```sh
humagen config check -f examples/media-evidence/humagen.yaml
humagen start -f examples/media-evidence/humagen.yaml
```

The profile uses development sign-in and memory state, for a trusted local
trial. Every client reaching it is an administrator.

## Ask for pages, images and unknowns

Attach the files to the message with **Attach** in the composer, or as
`uploads` on `POST /api/sessions` ([attachments](attachments.md)). Name every
file and ask for observations kept apart from claims:

```text
Read all pages of scanned-notes.pdf, inspect illustration.png and listen to
recording.wav. Cite original PDF page numbers. Give audio times only as your
estimate. Keep any [Voice note] transcript labelled as a transcript. Write
evidence.md with page numbers, approximate audio intervals, contradictions
and unknowns. Does anything establish identity, prove payment or synchronize
the clocks? Say exactly which pages and audio intervals you did not inspect.
```

## What was checked, and what was not

**This profile has not been run with a live model.** What is known of it
comes from code: its agent's model chain, built with the real provider
construction and fake keys, takes PDF, PNG, JPEG, GIF, WebP, MP3, WAV and MP4
natively and not M4A, and a generated WAV resolves to native audio for it
(`TestExampleAgentsReadTheMediaTheirGuidesSay`,
`TestMediaEvidenceAgentHearsAudio`). Whether Gemini answers well about them
is not shown.

Separately, the live re-run of the acceptance matrix at `main` after #383
used an agent of its own, on a Gemini model and not this profile. There an
agent read PDF pages and images given as uploads and heard a WAV and an MP3;
an M4A did not reach the model as audio; and the times the model gave for
speech were unreliable. The first version of this profile, which fell back to
Anthropic and OpenAI, could not have heard either recording.

The [generated input set](https://raw.githubusercontent.com/urmzd/humagen/3f80e938d748cadb7f2a9e372af0a4f57a54dd1c/docs/assets/session-audit-2026-11-16/media-evidence-inputs/README.md)
from the earlier check, with its [hash manifest](https://raw.githubusercontent.com/urmzd/humagen/3f80e938d748cadb7f2a9e372af0a4f57a54dd1c/docs/assets/session-audit-2026-11-16/media-evidence-inputs/sources.json),
holds an image-only PDF, a schematic video, an illustration and a short
synthetic speech recording. Nothing in it is a real incident or a person's
recording. Convert the recording to WAV or MP3 before attaching it for a model
to hear.

Not checked: video, the cloud speech path above, real records, noisy or
overlapping speech, speaker identity, other languages and long recordings.
There is no media timeline or evidence report in the console. Open the
original files and check the cited pages and intervals yourself.
