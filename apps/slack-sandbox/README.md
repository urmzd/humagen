# Slack sandbox

Test Humagen in the real Slack client, not a lookalike. The same fixtures that Storybook renders on the web get posted, streamed, and clicked in a sandbox workspace.

## Setup (once)

1. Generate the manifest. Scopes and events come from `src/manifest.ts`, one entry per capability, so the manifest can't drift from the code:

   ```sh
   pnpm sandbox manifest apps/slack-sandbox/slack-manifest.json
   ```

   The command prints a link that opens Slack's "Create app from manifest" flow, already filled in.

   A free workspace rejects custom workflow functions. Set `SLACK_SANDBOX_SKIP` to a comma-separated list of capability names (the keys of `CAPABILITIES` in `src/manifest.ts`) to leave them out. An unknown name is an error. The committed `slack-manifest.json` is the free-workspace variant, generated with:

   ```sh
   SLACK_SANDBOX_SKIP=workflowStep pnpm sandbox manifest apps/slack-sandbox/slack-manifest.json
   ```

   It has no **Ask the agent** workflow step and is not org-ready. Run the command without the variable on a paid workspace to get the full manifest.

2. Install the app to the sandbox workspace and invite it to the test channel (`/invite @humagen`).
3. Create an app-level token with `connections:write` (Basic Information → App-Level Tokens). Socket Mode uses it, so no public URL is needed.
4. Copy `.envrc.local.example` to the repo root as `.envrc.local`, fill it in, and run `direnv allow`.

## Updating the app with the Slack CLI

Once the app exists, the [Slack CLI](https://docs.slack.dev/tools/slack-cli/) can push manifest changes, so you don't have to paste JSON into the app settings. `.slack/hooks.json` tells the CLI to read `slack-manifest.json`. Run these from `apps/slack-sandbox`:

```sh
slack app link                                # once: connect this directory to your existing app
slack manifest validate                       # check the local manifest
slack manifest diff                           # compare it with the app's current settings
slack manifest sync --manifest-source=local   # apply the local manifest to the app
```

`slack app link` writes your app, team and user ids to `.slack/apps.dev.json` and `.slack/config.json`. Both are gitignored: don't commit them. Regenerate `slack-manifest.json` before `validate`, and read the `diff` before you `sync`, because `sync` changes the live app.

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
| Global shortcut | **Ask Humagen** in the shortcuts menu | The app's messages with you |
| Message shortcut | **Ask Humagen about this** on any message | That message's thread |
| Slash command | `/agent <question>` or `/agent help` | A new thread in the channel |
| Workflow step | **Ask the agent** in Workflow Builder | A new thread in the chosen channel |

The workflow step finishes when the turn does, so a turn that waits on an approval keeps the workflow waiting. Its outputs are `status` (`done`, `denied`, `stopped`) and `answer`. The shortcuts and the command need the `commands` scope, and the step needs an org-ready app on a paid workspace: regenerate the manifest without `SLACK_SANDBOX_SKIP` and reinstall.

The test app is Humagen's Slack app, so the manifest names it Humagen (`@humagen`). Opening its Messages tab sets the suggested prompts. Opening its Home tab publishes the `home` fixture (`Home` from `@humagen/components`): a greeting, Ask a Question, Build an Agent and Search Knowledge, notifications (inbox items and open alerts, each with its console address written out), who answers your direct messages, connections, and a line about your access. Ask a Question opens the ask form; picking who answers republishes the tab; the other buttons open a modal that says what a host would do.

Fixture names match the exports in `@humagen/core/fixtures` and the Storybook docs.
