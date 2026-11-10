# The Slack app, maintained with the Slack CLI

This directory is a [Slack CLI](https://docs.slack.dev/tools/slack-cli/) project for the
Socket Mode Slack app. It has no manifest file of its own: the CLI's `get-manifest` hook runs
`humagen slack manifest`, so the app Slack sees is generated from the same code that serves it.
`apps/slack/manifest.json` is the same output, kept in step by a test.

## Once per machine

```sh
cd deploy/slack
slack login                                              # if `slack auth list` shows no workspace
slack app link --app "$SLACK_APP_ID" --team "$SLACK_TEAM_ID" --environment local
```

`slack app link` writes `.slack/apps.dev.json` and `.slack/config.json`. Both stay out of git.

## Changing the app

1. Change `slackManifest` in `internal/app/slack.go`.
2. Run `make slack-manifest` to regenerate `apps/slack/manifest.json`.
3. Run `make slack-diff` to compare the generated manifest with the app's live settings.
4. Run `make slack-push` to validate the manifest, update the app and reinstall it. A new scope
   asks you to confirm in the terminal, then to allow it in Slack.

`SLACK_APP_ID` and `SLACK_TEAM_ID` come from your environment, for example from your secret
store in `.envrc.local`. They are never committed.
