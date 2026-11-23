# Use Humagen on GitHub

An agent reviews pull requests and answers on issues, as a GitHub App you
own. One command registers the app, installs it on your repositories and
connects it to Humagen.

## Before you start

- Humagen is running, and you can reach its API. `humagen start` on this
  machine is enough.
- You may register a GitHub App on your account or organization.
- GitHub can reach Humagen. See [a public address](#a-public-address).

## A public address

GitHub tells the app about pull requests and comments by sending events to a
URL. That URL must be public and use `https`, and it must reach
`/connectors/github` on Humagen's webhook listener.

Start Humagen with a separate listener for webhooks, then expose only that
port:

```sh
humagen start -webhook-addr=127.0.0.1:8383
```

Forward port 8383 with the tunnel or reverse proxy you already use. Do not
forward the port the console and API use. The address you pass to the install
command is the public one, for example
`https://hooks.example.com/connectors/github`.

## Install

```sh
humagen install github --webhook-url https://hooks.example.com/connectors/github
```

Your browser opens GitHub twice:

1. **Register the app.** GitHub shows the app's name, which you may change,
   and its permissions. Select **Create GitHub App**.
2. **Install it.** Choose the account, then all repositories or only the
   ones you select. Select **Install**.

Return to the terminal. It prints the app, the account and the agent that
answers. You copy nothing: GitHub gives the app's private key and webhook
secret to the command, which sends them to your Humagen server. They are not
printed, shown in the browser or written to a file on this machine.

To register the app under an organization:

```sh
humagen install github --org acme --webhook-url https://hooks.example.com/connectors/github
```

## Use it

Open a pull request in a repository the app is installed on. The default
agent, `code-reviewer`, reviews it when it is ready for review. To ask a
question, write `@humagen` in a comment, and name an agent right after it to
ask that agent: `@humagen ask code-reviewer to look at the retry path`. Add the
`humagen` GitHub account as a collaborator so GitHub offers the name as you
type ([who a comment asks](../reference/github.md#who-a-comment-asks)).

`code-reviewer` comes from the Review package. If the command says to install
it, open **Store** in the console and install **Review**.

## Options

| Flag | Use |
|---|---|
| `--webhook-url URL` | Required. The public address of `/connectors/github`. |
| `--org LOGIN` | Register the app under an organization. |
| `--app-name NAME` | The app's name on GitHub. Names are unique across GitHub, so the default is Humagen and a few random letters. |
| `--agent NAME` | The agent that answers. The default is `code-reviewer`. |
| `--repo OWNER/NAME` | A repository that another surface, such as Slack, may read. Repeat it for more. |
| `--name NAME` | The client's name in Humagen. The default is `github`. |
| `--force` | Replace the client that has this name. |
| `--server URL`, `--token REF` | Which Humagen to connect, and how to sign in to it. |
| `--no-browser` | Print the link instead of opening it. |

## The app's look

GitHub gives a new app a generated icon. To set a logo and its background
color, open the app's page (the terminal prints it), select **App settings**
and scroll to **Display information**.

## If something goes wrong

| What you see | What to do |
|---|---|
| `--webhook-url is required` | Give the public `https` address. See [a public address](#a-public-address). |
| `client github exists` | Pass `--force` to replace it, or `--name` to add a second one. |
| `timed out waiting for GitHub` | Run the command again and finish both pages. If an app was registered, delete it first. |
| `the app ... is registered and installed, but Humagen did not take it` | Delete the app on GitHub, in its **Advanced** settings, fix the error shown, and run again. The key from the first try is not kept. |
| The app does not answer | Check the app's **Advanced** page on GitHub for recent deliveries. A failed delivery means the webhook URL does not reach Humagen. |

To remove it, delete its client in the console, then
uninstall or delete the app on GitHub.

See the [GitHub reference](../reference/github.md) for reviews, fixes and
checks, and [Slack](slack.md) to add the next place you work.
