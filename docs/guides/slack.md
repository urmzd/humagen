# Use Humagen in Slack

Message your agents from any channel, thread or direct message. One command
creates the Slack app and sets Humagen up to answer through it.

## Before you start

- Humagen is installed and `humagen start` runs on this machine.
- You may add apps to your Slack workspace.

The app connects out to Slack over Socket Mode, so this machine needs no
public address.

## Install

```sh
humagen install slack
```

Your browser opens Slack's own page with the app already described: its name,
permissions and events come from Humagen's manifest. Then:

1. Pick your workspace, then **Next** and **Create**.
2. **Install to Workspace**, then **Allow**.
3. Open **OAuth & Permissions** and copy the **Bot User OAuth Token**.
   Paste it into the terminal.
4. Open **Basic Information**, find **App-Level Tokens**, and generate one
   with the scope `connections:write`. Paste it into the terminal.

Slack does not hand a token to another program, so these two values are the
only things you copy. The terminal does not show them as you paste.

The command asks Slack to confirm each token, keeps both in your operating
system's keychain, and writes where they are into `humagen.yaml`. The file
holds references such as `keychain:humagen/slack-bot-T0123`, never the tokens.

## Use it

```sh
humagen start
```

In Slack, open a direct message with the app, or invite it to a channel with
`/invite @Humagen` and mention it. It answers in the thread.

## Say who is who

Out of the box the app answers everyone and acts for nobody: a run asked for
in Slack is the app's, not yours. To make your Slack messages yours in
Humagen (your runs, your Home tab, your answers to an agent's questions), map
each Slack user id to the subject that person signs in to Humagen with, under
`surfaces.slack` in `humagen.yaml`:

```yaml
people:
  U0123ABCD: "tailscale:you@github"
```

A Slack user id is in a person's Slack profile, under **More**, then **Copy
member ID**. A subject under `tailscale` sign-in is `tailscale:` followed by
the person's Tailscale login. Under `tailscale` sign-in that is all: run
`humagen start` again. Under any other
sign-in also write `machine`, the subject the app's own credential signs in
as, and give that credential `humagen:clients:read`. Anyone you do not map
stays nobody. See [the spec](../reference/spec.md#whom-the-slack-app-acts-for).

## Options

| Flag | Use |
|---|---|
| `--name NAME` | The app's name in Slack. The default is the name in your spec, else Humagen. |
| `--store env` | Do not use the keychain. You export `SLACK_BOT_TOKEN` and `SLACK_APP_TOKEN` yourself where Humagen runs. |
| `--no-browser` | Print the link instead of opening it, for a machine with no browser. |
| `-o PATH` | The spec file to write. |

## The app's look

An app made from a manifest has Slack's default icon. To change the icon or
its background color, open the app at <https://api.slack.com/apps>, then
**Basic Information** and **Display Information**. The color can also be set
before you install, as `surfaces.slack.background_color` in the spec.

## If something goes wrong

| What you see | What to do |
|---|---|
| `the bot token was refused by Slack` | Copy the token again from **OAuth & Permissions**. It starts with `xoxb-`. Install the app to the workspace first. |
| `the app-level token was refused by Slack` | Generate a new one with the scope `connections:write`. It starts with `xapp-`. |
| `the keychain could not be written` | Run again with `--store env`. |
| `under tailscale sign-in the server gives the Slack app it starts its own credential` | Remove `humagen_client` and `humagen_token` from `surfaces.slack`. Under `tailscale` sign-in Humagen makes the app's credential itself. |
| `under tailscale sign-in the server makes the Slack app's credential and knows its subject` | Remove `machine` from `surfaces.slack`. |
| `people need machine` | Under a sign-in other than `tailscale`, write `machine` beside `people`: the subject the app's credential signs in as. |
| `slack app's client from the file not applied, so the app acts for nobody` in the log | Another client already has the id `slack-app-<team>`. Remove or rename that client, then start again. |
| The app does not answer | Check that `humagen start` is running and that the app is in the channel. |

To remove the app, delete it at <https://api.slack.com/apps> and set
`surfaces.slack.enabled` to `false`.

See the [Slack reference](../reference/slack.md) for what the app can do, and
[GitHub](github.md) to add the next place you work.
