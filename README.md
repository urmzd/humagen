<p align="center">
  <h1 align="center">agent-ui</h1>
  <p align="center">
    One agent UI model, rendered to the web and to Slack Block Kit from the same React components.
    <br /><br />
    <a href="#installation">Install</a>
    &middot;
    <a href="https://github.com/urmzd/agent-ui/issues">Report Bug</a>
    &middot;
    <a href="docs/surfaces.md">Surface Map</a>
  </p>
</p>

<p align="center">
  <a href="https://github.com/urmzd/agent-ui/actions/workflows/ci.yml"><img src="https://github.com/urmzd/agent-ui/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  &nbsp;
  <a href="LICENSE"><img src="https://img.shields.io/github/license/urmzd/agent-ui" alt="License"></a>
</p>

## Features

- **One model.** Turns, tasks, approvals, clarifications, failures, artifacts, inbox items and session state are defined once in `@agent-ui/core`, with a pure event reducer that any runtime can feed.
- **One component, two renderers.** Shared components are built only from primitives. `render-web` draws them in the DOM, and `render-slack` serializes the same React tree to Block Kit through a custom reconciler.
- **Slack done properly.** The Slack renderer enforces limits, supplies fallback text and time tokens, streams with `chat.startStream`, `appendStream` and `stopStream`, and maps statuses to what Slack accepts. Every fixture is checked against Slack's `blocks.validate`.
- **Human-in-the-loop.** Approval gates, batch approvals, Ask, Always Allow and clarifications, with decisions that update in place. Every gate settles exactly once and never defaults to yes.
- **Web-only extras where Slack has its own UI.** The prompt bar with a message queue, the thinking orb, a statusline for context, compaction and budget, the turn rail and artifact previews.
- **Real Slack sandbox.** Generate the app manifest, then post fixtures, stream turns and click real buttons over Socket Mode.
- **Themeable.** A neutral black base with one swappable accent (purple by default; blue, yellow and teal presets, plus a hue shift), in light or dark.

## Installation

### Prerequisites

| Requirement | Version |
|-------------|---------|
| Node | 22+ |
| pnpm | 11+ |

The packages are private and not published yet. Work from the workspace:

```sh
git clone git@github.com:urmzd/agent-ui.git
cd agent-ui
pnpm install
```

## Quick Start

```sh
pnpm storybook           # web components at http://localhost:6006
pnpm sandbox validate    # check every fixture with Slack's blocks.validate (no token needed)
```

<!-- fsrc src="examples/basic/render.tsx" fence="auto" -->
```
import { TurnView } from "@agent-ui/components";
import { replay } from "@agent-ui/core";
import { renderToSlack } from "@agent-ui/render-slack";
import { WebSurface } from "@agent-ui/render-web";

// One stream of events from any runtime...
const turn = replay([
  { type: "turn.started", turnId: "t1", agent: { name: "Assistant" } },
  { type: "task.upserted", task: { id: "s", title: "Searched runbooks", status: "complete" } },
  { type: "text.delta", text: "Roll back deploy `#4812`, then watch the 5xx rate." },
  { type: "turn.completed" },
]);

// ...one component, rendered to the web...
export const Web = () => (
  <WebSurface onAction={(action) => console.log(action.id, action.payload)}>
    <TurnView turn={turn} />
  </WebSurface>
);

// ...and to Slack Block Kit, ready for chat.postMessage.
export const slack = renderToSlack(<TurnView turn={turn} />);
```
<!-- /fsrc -->

## Usage

### Web

Wrap shared components in `WebSurface`. Clicks arrive as the same `UiAction` that a Slack interaction decodes to, so one handler serves both surfaces.

### Slack

`renderToSlack(element, { placement })` returns `{ blocks, text, warnings }`, where placement is `message`, `modal` or `home`. For streaming:

1. Pass `streamStart` to `chat.startStream`.
2. Send each `streamUpdate(prev, next)` result with `chat.appendStream`.
3. Finish with `chat.stopStream`, using `renderToSlack(<TurnView streamed />)` for the final blocks.

`lifecycle(turn)` returns the reaction for the user's message and the agent session status. `decodeSlackAction` turns any `block_actions` payload back into a `UiAction`.

### Sandbox

| Command | Does |
|---------|------|
| `pnpm sandbox manifest` | Generate the Slack app manifest from the capabilities agent-ui uses |
| `pnpm sandbox validate` | `blocks.validate` every fixture |
| `pnpm sandbox post <name\|all>` | Post fixtures to your test channel |
| `pnpm sandbox stream [turn]` | Stream a turn for real |
| `pnpm sandbox upload` | Upload a real file and post its artifact card |
| `pnpm sandbox serve` | Socket Mode: chat with the bot and click real buttons |

See [apps/slack-sandbox/README.md](apps/slack-sandbox/README.md) for setup.

## Architecture

`core` (model, events, queue, session, inbox) feeds `components` (shared, primitives only). The primitives are implemented by `render-web` (DOM) and by `render-slack` (Block Kit). `web` holds the components that only exist on the web.

See [docs/surfaces.md](docs/surfaces.md) for every concept mapped to both surfaces, and for the open questions.

## License

[Apache-2.0](LICENSE)
