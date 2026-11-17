# Generate and revise a raster image

`examples/image-generation/codex.yaml` uses the signed-in Codex program on
this host to generate a real image, inspect its pixels and revise that image.
The tested path uses Codex's built-in image tool through the existing account;
it does not need a separate image API key. Availability depends on the installed
Codex version and account. Humagen does not implement an image service itself.

## Start the artist

Sign in to Codex on the host and check its installed capability:

```sh
codex --version
codex features list
```

The acceptance below used Codex CLI 0.167.0, reporting
`image_generation stable true` and `view_image stable true`. Check those
features on your installation before relying on this example. If the actual
tool is unavailable, report the missing capability; do not substitute a drawn
placeholder or claim that a file was generated.

Set `codex.dir` to an existing, absolute output folder, then start:

```sh
humagen config check -f examples/image-generation/codex.yaml
humagen start -f examples/image-generation/codex.yaml
```

The profile enables `image_generation`, uses Codex's default model and its
`workspace-write` sandbox. Humagen's native tool grants and sandbox do not
govern the program's tools. Codex's own account, settings, skills, hooks and
tool connections may apply; they are not audited by this profile. The prompt's
folder restriction is an instruction, not an authorization boundary. This
development-auth profile is for a trusted local trial.

## Generate, inspect, then edit

Message `artist`:

```text
Generate a square editorial illustration: one orange fox wearing a blue
scarf, seated on a snowy hill beneath a pale sky. No text. Use the native
image tool, save the actual PNG as fox-v1.png in your working directory,
inspect it with view_image, and report what you see and the full file path.
Do not draw a substitute with code.
```

Open the actual output and read its pixels. Record its hash before editing:

```sh
shasum -a 256 /absolute/path/to/image-outputs/fox-v1.png
```

Continue the same conversation with an edit grounded in the inspected image:

```text
Edit the actual fox-v1.png with the native image tool. Change only the blue
scarf to warm red. Keep the fox, pose, snowy landscape, composition and
illustration style. Save fox-v2.png, leave fox-v1.png unchanged, inspect the
result with view_image and report the observed color and unintended changes.
Do not recolor it using code.
```

Use the API's `after` field with the preceding run ID if submitting the
follow-up through `POST /api/runs`. Open both files at the same size and compare
them yourself. A model's inspection statement is not independent visual proof.
Recheck the first file's hash, and retain both images.

The built-in tool initially saves its output in Codex's generated-image folder.
The agent must copy the actual output into the requested directory and report
that final path. A path in an answer does not publish the image as a Humagen
artifact or make it downloadable from another host.

## Observed acceptance and limits

A real Humagen run generated a 1254 by 1254 PNG through Codex CLI 0.167.0.
The model inspected the actual image; a follow-up used it as the image tool's
reference, changed the blue scarf to red and inspected the result. Both files
were independently opened: the orange fox, scarf and snowy composition were
visible. Minor fur and knit texture changes accompanied the scarf edit. The
original PNG's SHA256 stayed unchanged. The saved Codex trace confirms real
image-tool and image-view calls, rather than a shell-drawn substitute.

Not checked: exact pixel preservation outside the edited region, transparency,
logo typography, multiple references, batch generation or other Codex versions
and accounts. This is a generated fixture, not a real brand asset. Tool-specific
image generation and viewing events are not currently rendered in Humagen's
run journal; the local files and Codex trace supply that evidence. Automatic
console publication and cross-host delivery are not covered.
