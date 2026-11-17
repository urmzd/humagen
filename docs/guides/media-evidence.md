# Read mixed evidence and transcribe local media

`examples/media-evidence/claude-code.yaml` runs an agent on the signed-in
Claude Code program with a folder of original evidence on this host. It renders
scanned PDF pages, reads image pixels, extracts video frames and uses local
Whisper ASR for English audio. It keeps original files and writes derived
outputs separately, citing original pages and source times.

## Start the analyst

The tested host is an Apple silicon Mac with Claude Code, FFmpeg (`ffprobe`
included), Poppler (`pdftoppm` and `pdftotext`) and uv installed. Sign in to
Claude Code, set `claude-code.dir` to the existing absolute evidence folder,
then run:

```sh
humagen config check -f examples/media-evidence/claude-code.yaml
humagen start -f examples/media-evidence/claude-code.yaml
```

The profile preauthorizes Bash, Read, Write and Glob and supplies an empty
strict MCP configuration. The program runs as you. Humagen does not confine
its shell to the folder, and native tool grants and the native sandbox do not
govern its tools. Its user settings, hooks, plugins and skills still apply.
The prompt's file restriction is an instruction, not an authorization boundary.
Use a trusted local host and development account.

The pinned [mlx-whisper 0.4.3](https://pypi.org/project/mlx-whisper/0.4.3/)
package runs ASR locally and downloads a model on first use. This example uses
[whisper-base.en-mlx](https://huggingface.co/mlx-community/whisper-base.en-mlx).
It does not need an ASR API key. Transcripts and extracted images that the
agent reads enter the Claude conversation; local ASR does not make the whole
agent workflow offline. Other operating systems, languages and models were
not exercised.

## Ask for pages, frames and unknowns

Name every original file and require observations to be separated from claims:

```text
Read all pages of scanned-notes.pdf, inspect illustration.png, and analyze
scene.mp4 and recording.m4a. Preserve their bytes. Render the PDF pages and
read their pixels, not only extracted text. Inspect video frames around the
changes and cite their actual source timestamps, not captions or filenames.
Keep ASR output labelled as ASR. Write evidence.md with original PDF page
numbers, source frame times, audio intervals, contradictions and unknowns.
Does anything establish identity, prove payment or synchronize the clocks?
Say exactly which pages, frames and audio intervals you did not inspect.
```

Inspect metadata before choosing extraction times. A frame's caption can be
wrong. `ffprobe` supplies source frame presentation timestamps; retain those
beside the actual extracted images. A contact sheet helps compare frames,
but it does not establish that every frame was examined.

These files are read on the CLI host, not uploaded through the message API.
Large files therefore use a different path from bounded message uploads. Use
FFmpeg to extract selected frames or audio windows instead of putting a whole
binary file into the model context. For long recordings, request explicit
windows of at most 120 seconds, retain each source offset and reconcile the
coverage. This is a workflow instruction, not an enforced resource limit.

## Retain timestamps and cross-check the original

For English audio, the local tool can preserve segment and word timing:

```sh
uv run --with mlx-whisper==0.4.3 mlx_whisper recording.m4a \
  --model mlx-community/whisper-base.en-mlx --language en \
  --word-timestamps True --temperature 0 \
  --condition-on-previous-text False --output-format all --output-dir asr
```

Keep raw JSON as well as SRT. Ask the agent to cut padded intervals from the
**original recording**, transcribe those clips again and record both local
clip times and source times. For example, a clip beginning at source 2.5 s
maps a local word time of 1.0 s to source 3.5 s. Retain the requested extraction
interval and the actual decoded duration. Codec padding can make them differ.

An extraction interval is exact as requested; ASR speech and word boundaries
are estimates. Keep confidence, repeated words and disagreements in raw output.
A second ASR pass, even with another Whisper size, is correlated evidence.
It is not manual listening or a guarantee that a statement was spoken.

## Reproduce the generated check

The [generated input set](https://raw.githubusercontent.com/urmzd/humagen/3f80e938d748cadb7f2a9e372af0a4f57a54dd1c/docs/assets/session-audit-2026-11-16/media-evidence-inputs/README.md) includes an image-only
three-page PDF, a schematic video, an AI illustration and a short synthetic
speech recording. Nothing in it is a real incident or a user's recording.
Download the four originals and their [hash manifest](https://raw.githubusercontent.com/urmzd/humagen/3f80e938d748cadb7f2a9e372af0a4f57a54dd1c/docs/assets/session-audit-2026-11-16/media-evidence-inputs/sources.json)
into a fresh folder, then use the prompt above.

The large-file check can be recreated from the small schematic video:

```sh
ffmpeg -n -i scene.mp4 -c:v rawvideo -pix_fmt bgr24 large-scene.avi
```

On the tested FFmpeg installation this produced a valid 126,498,214-byte AVI,
720 by 480, 122 frames at 10 fps, 12.2 seconds. Inspect your actual output;
container metadata can vary with the tool version. The agent extracted and
inspected selected frames from that file without loading it into model context.

Observed acceptance:

- All three scanned pages were rendered and read. The agent cited page 2's
  seal correction, page 1's unverified identity and page 3's unconfirmed
  payment. It did not treat a requested deposit as proof of payment.
- Video pixels changed at source PTS 4.2 and 8.2 s, while captions suggested
  4 and 8 s. An independent decode of all 122 frame timestamps and center
  pixels confirmed the agent's observed times. The original was retained,
  including the misleading captions.
- The actual illustration's fox and blue scarf were inspected. It was
  identified as an illustration, not evidence of the fictional incident.
- Full-file ASR and selected original-clip re-transcriptions retained source
  offsets and uncertainty. The tiny model produced a false currency symbol
  and repeated tail words; the base model removed the repetition but gave
  different approximate word boundaries. All raw results were retained.
  One answer table also misstated a clip's last-word end: it said 10.52 s,
  while the raw word JSON ends at 11.24 s. Independently check every reported
  interval against the raw JSON; a fluent summary is not timing proof.
- Original hashes were unchanged. No identity, payment or shared clock was
  inferred from an unrelated image, initials or matching numbers.

## Limits and review

Not checked: real records, noisy or overlapping speech, speaker identity,
other languages, a long recording's complete coverage, every video frame by
visual inspection, manual listening or cloud media. There is no native media
timeline or automatically published evidence report in the console. Open the
actual files and review the cited source passages and intervals yourself.
