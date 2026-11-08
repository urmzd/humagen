# Console visual evidence

`make screens` builds a fresh local server and console, preserves development
sign-in and the deterministic local model, and captures the canonical session
interface. No live OAuth connection or model provider is used.

The `Console screens (light)` and `Console screens (dark)` CI jobs each capture
20 focused states at 390, 480, 768, 1024 and 1280 pixels wide: the seven navigation
entries, composer and session menus, feedback, all four Details sections, an
actual parked approval and a review v2 answer. A child session, alert and
scratchpad files are created through the real session APIs. Details assertions
check modal behavior below 1024 and composer reflow beside the desktop pane.

Artifacts are named `console-screens-light` and `console-screens-dark`. Each
contains its theme directory with:

- `index.html`: labeled side-by-side images, including full-height captures.
- `report.json`: screen, path, viewport, image and structural layout problems.
- `metadata.json`: tested source SHA, PR head SHA, expected states and sizes,
  captured count, completion and pass flags.
- `<WIDTH>x<HEIGHT>/<state>.png`, plus optional `.full.png`.
- A `.failure.png` and `.failure.json` when a scripted interaction fails.

Reports update after every state. A partial artifact is diagnostic evidence;
acceptance requires `complete: true`, `passed: true`, the expected count for all
five widths in both themes, and visual inspection of the final tested commit.
The checks measure overflow, text overlap, targets, gutters and field text size.
They do not replace visual review or live-provider validation.

The runner accepts `SCREENS_VIEWPORTS`, `SCREENS_COLOR_SCHEME`, `SCREENS_OUT` and
`SCREENS_ONLY`. Filtered captures record their smaller expected set, so they
must not be described as full acceptance. `SCREENS_SUITE=legacy` retains the
historical screen and showcase fixtures; it is not final session-UI evidence.

Offline script validation, without launching a browser:

```sh
pnpm --filter @humagen/screens typecheck
pnpm --filter @humagen/screens test
```
