# Slack sandbox

Test vultus in the real Slack client, not a lookalike. The same fixtures that Storybook renders on the web get posted, streamed, and clicked in a sandbox workspace.

## Setup (once)

1. Generate the manifest. Scopes and events come from `src/manifest.ts`, one entry per capability, so the manifest can't drift from the code:

   ```sh
   pnpm sandbox manifest apps/slack-sandbox/slack-manifest.json
   ```

   The command prints a link that opens Slack's "Create app from manifest" flow, already filled in.

2. Install the app to the sandbox workspace and invite it to the test channel (`/invite @vultus-sandbox`).
3. Create an app-level token with `connections:write` (Basic Information → App-Level Tokens). Socket Mode uses it, so no public URL is needed.
4. Copy `.envrc.local.example` to the repo root as `.envrc.local`, fill it in, and run `direnv allow`.

## Commands

| Command | What it does | Needs |
|---|---|---|
| `pnpm sandbox list` | List postable fixtures | nothing |
| `pnpm sandbox validate` | Run every fixture through `blocks.validate` | nothing (public method) |
| `pnpm sandbox post <name\|all>` | Post fixtures as a threaded catalog | bot token, channel |
| `pnpm sandbox stream [turn]` | Stream a turn for real: reactions, session status, `startStream` → `appendStream` → `stopStream` | bot token, channel, test user |
| `pnpm sandbox serve` | Socket Mode: talk to the bot, click every button, and start turns from shortcuts, `/agent` and workflows | bot and app tokens |

In `serve`, keywords choose the scenario. Say `help` to the bot for the full list, which is the `SCENARIOS` table in `src/fixtures.tsx`: approvals (a skill, a prompt change, a file, a rollback, a batch), a clarification, files, references, failures and runtime notices. `status` posts a system alert and resolves it in place. Anything else gives a streamed answer with a plan and citations.

Every button is live. Approving runs the follow-up for that tool. Edit, Ask and Something Else open a form.

Besides a DM or an @mention, a turn can start from:

| Entry | How | Answer lands |
|-------|-----|--------------|
| Global shortcut | **Ask the agent** in the shortcuts menu | The app's messages with you |
| Message shortcut | **Ask agent about this** on any message | That message's thread |
| Slash command | `/agent <question>` or `/agent help` | A new thread in the channel |
| Workflow step | **Ask the agent** in Workflow Builder | A new thread in the chosen channel |

The workflow step finishes when the turn does, so a turn that waits on an approval keeps the workflow waiting. Its outputs are `status` (`done`, `denied`, `stopped`) and `answer`. The shortcuts and the command need the `commands` scope, and the step needs an org-ready app: regenerate the manifest and reinstall.

Opening the agent's Messages tab sets the suggested prompts. Opening its Home tab publishes what is waiting on you.

Fixture names match the exports in `@vultus/core/fixtures` and the Storybook docs.
