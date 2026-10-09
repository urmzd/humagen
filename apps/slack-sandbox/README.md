# Slack sandbox

Test agent-ui in the real Slack client, not a lookalike. The same fixtures that Storybook renders on the web get posted, streamed, and clicked in a sandbox workspace.

## Setup (once)

1. Generate the manifest. Scopes and events come from `src/manifest.ts`, one entry per capability, so the manifest can't drift from the code:

   ```sh
   pnpm sandbox manifest apps/slack-sandbox/slack-manifest.json
   ```

   The command prints a link that opens Slack's "Create app from manifest" flow, already filled in.

2. Install the app to the sandbox workspace and invite it to the test channel (`/invite @agent-ui-sandbox`).
3. Create an app-level token with `connections:write` (Basic Information → App-Level Tokens). Socket Mode uses it, so no public URL is needed.
4. Copy `.envrc.local.example` to the repo root as `.envrc.local`, fill it in, and run `direnv allow`.

## Commands

| Command | What it does | Needs |
|---|---|---|
| `pnpm sandbox list` | List postable fixtures | nothing |
| `pnpm sandbox validate` | Run every fixture through `blocks.validate` | nothing (public method) |
| `pnpm sandbox post <name\|all>` | Post fixtures as a threaded catalog | bot token, channel |
| `pnpm sandbox stream [turn]` | Stream a turn for real: reactions, session status, `startStream` → `appendStream` → `stopStream` | bot token, channel, test user |
| `pnpm sandbox serve` | Socket Mode: DM or @mention the bot, then click Approve, Cancel, Retry, or the feedback buttons | bot and app tokens |

In `serve`, keywords choose the scenario: *deploy/rollback* gives an approval gate, *fail* gives a failure alert, *code/test* gives a coding agent, and anything else gives a streamed answer with a plan and citations. Opening the agent's Messages tab sets the suggested prompts.

Fixture names match the exports in `@agent-ui/core/fixtures` and the Storybook docs.
