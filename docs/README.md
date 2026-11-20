# Start with Humagen

A **project** groups people, agents and their shared work. A **session** holds a conversation. A **run** records one agent’s work on a request, including its status and result.

## Render your first review

Start with a small example you can run locally. It turns three sample findings into a GitHub review with a severity, a suggested fix and evidence for each finding. You need Git and Go at the version listed in [go.mod](https://github.com/urmzd/humagen/blob/main/go.mod).

```sh
git clone https://github.com/urmzd/humagen.git
cd humagen
go run ./examples/github-review
```

Look for **1 error**, **1 warning** and **1 info** on the first line. The example renders fixture data locally and needs no GitHub token or model credentials. Compare the report with the [example source](https://github.com/urmzd/humagen/blob/main/examples/github-review/main.go) and [expected output](https://github.com/urmzd/humagen/blob/main/examples/github-review/preview.md).

## Start a session

From the same checkout, install Node.js 24 and the pnpm version in [package.json](https://github.com/urmzd/humagen/blob/main/package.json). Build the console and start the local workspace.

```sh
pnpm install --frozen-lockfile
pnpm --filter @humagen/console build
go run ./cmd/humagen start
```

1. Follow the first-start questions in the terminal. Open <http://127.0.0.1:8080>.
2. In **Settings → Model providers**, enable a provider and configure a model the entry agent can use. This step needs access to the chosen model.
3. Open a new session and send `@humagen What can you help me do in this workspace?`. Open **Activity** in the session to inspect the run.

The default local workspace keeps conversations in memory. To retain work across restarts, use the [deployment guide](guides/deploy.md). To use your agents where you work, follow the [Slack](guides/slack.md) and [GitHub](guides/github.md) guides.

## Work with other agents

The [development configuration](https://github.com/urmzd/humagen/blob/main/examples/dev/humagen.yaml) defines a lead, a researcher and a writer. The lead’s `access.spawn` list names the two agents it can call. Read [how agent calls work](architecture/agent-calls.md) alongside the file, then use the [configuration reference](reference/spec.md) to change the models and permissions.

Browse the [documentation index](catalog.md) for workflow guides, architecture, reference material and design records.
