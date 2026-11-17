## Connect a surface

`humagen create` creates a client in the running platform's store through the
same API as the console. Run `humagen start` first. The command does not edit a
spec file. `humagen create --help` lists the supported kinds.

```sh
humagen create github --app-id 123 --installation 456 \
  --private-key env:HUMAGEN_GITHUB_KEY \
  --webhook-secret env:HUMAGEN_GITHUB_SECRET \
  --repo acme/service --name acme-github

humagen create slack --app-id A0123 --team T0123 \
  --bot-token env:HUMAGEN_SLACK_TOKEN \
  --signing-secret env:HUMAGEN_SLACK_SECRET --name acme-slack
```

The server must have the referenced variables and allow them for the workspace
with `-secret-refs`; see [client credentials](clients.md#which-variables-a-workspace-may-reference).
Client references currently resolve only allowlisted `env:HUMAGEN_*` variables
on the server. The CLI checks `env:`, `keychain:` and `op://` syntax and sends
references unchanged. Server policy decides which it accepts. Raw credential
values are refused by the command.

If the server returns `that secret reference cannot be used by this workspace`,
the CLI prints a one-line hint containing the actual variable names and the
authenticated tenant from `/api/me`. For example, with tenant `default`:

```text
the server does not let this workspace read HUMAGEN_GITHUB_KEY, HUMAGEN_GITHUB_SECRET; start it with -secret-refs "default=HUMAGEN_GITHUB_KEY,HUMAGEN_GITHUB_SECRET" (or set spec.secret_env in the System document); ensure these variables are set on the server
```

Grant those exact names when starting the server:

```sh
humagen serve -secret-refs "default=HUMAGEN_GITHUB_KEY,HUMAGEN_GITHUB_SECRET"
```

Or add the grant to the System document's `spec`:

```yaml
secret_env:
  default: [HUMAGEN_GITHUB_KEY, HUMAGEN_GITHUB_SECRET]
```

The server still requires each variable to be set and non-empty. Its refusal
does not disclose which reference failed or whether it was granted, unset or
empty. The hint therefore names all environment references supplied for this
client. If the principal lookup fails, the hint uses `<tenant>` as a placeholder.

| Flag | Behavior |
| --- | --- |
| `--server URL` | API base URL; defaults to `$HUMAGEN_URL`, then `http://localhost:8080` |
| `--token REF` | API bearer token reference; defaults to `env:HUMAGEN_TOKEN`. An unset default sends no token for dev mode. `--token ''` explicitly disables authentication. An explicit non-empty reference must resolve locally. |
| `--name NAME` | Client name; defaults to `github` or `slack` |
| `--agent NAME` | GitHub defaults to `code-reviewer`, from the Review package in the agent store; Slack leaves the default agent empty for the workspace default |
| `--force` | Update an existing client with the same name and surface through `PUT /api/clients/{id}` |
| `--json` | Print the API's create or update response as JSON, without human output |

GitHub requires `--app-id`, `--installation`, `--private-key REF` and
`--webhook-secret REF`. Repeat `--owner ACCOUNT` for the installation's accounts.
Without `--owner`, the command derives accounts from repeated `--repo owner/name`
values. At least one owner is required. Repositories populate the client's
`github.read_repositories`. `--agent` selects the client default; the agent must
already be available in the workspace.
GitHub app and installation IDs must contain 1 to 20 digits.

Slack requires `--team`, `--bot-token REF` and `--signing-secret REF`, including
updates. Team IDs must match `^[A-Z][A-Z0-9]{2,31}$`, such as `T0123`; `T1`
is too short. Optional `--app-id` identifies the app when several apps share a
team and must match `^A[A-Z0-9]{2,31}$`, such as `A0123`. Identity formats are
validated before any request. These clients use the Events API over HTTP. There is no
`--app-token` flag or Socket Mode token field on this API client.

A duplicate name on the same surface fails with
`client NAME exists; pass --force to update it`. An update replaces the client's
configuration with the supplied fields; omitted credentials keep their previous
values under the API contract. Use the console or API to manage additional client
options. HTTP errors are returned with their status and server response.

After creating a GitHub client, mention `@code-reviewer` on a pull request.
`code-reviewer` comes with the Review package: when the workspace has not
installed it, the command says so first (`Next: install the Review package
from the Store, then mention @code-reviewer on a pull request`). Install it
under Agents, Store in the console, or with
`POST /api/store/packages/review/install`.
For Slack, configure Slack's Events API endpoint and mention the app in Slack.
See the [GitHub](github.md), [Slack](slack.md) and [client](clients.md) references
for app setup, webhook verification and routing.

## Use the agent store

`humagen store` lists, installs and removes the agent store's packages in a
running platform, through the same API as the console's Store page. Run
`humagen start` first. The command only sends the request and prints the
server's answer: the server decides what is installed, what an install adds and
what a removal retires.

```sh
humagen store list
humagen store install review
humagen store remove review
```

| Verb | Request | Behavior |
| --- | --- | --- |
| `list` | `GET /api/store/packages` | One row per package: its name, its current version, whether the workspace has it installed, the agents for which the server offers an update, the agents installing would add, and its title |
| `install NAME` | `POST /api/store/packages/{name}/install` | Adds the package's missing agents and prints the ones added. Installing an installed package adds nothing |
| `remove NAME` | `DELETE /api/store/packages/{name}` | Retires the package's agents and prints the ones retired. An agent changed in the workspace is retired only with `--force` |

`NAME` is a package's name (`humagen/core`) or its last part (`core`). Flags may
come before or after it.

| Flag | Behavior |
| --- | --- |
| `--server URL` | API base URL; defaults to `$HUMAGEN_URL`, then `http://localhost:8080` |
| `--token REF` | API bearer token reference; defaults to `env:HUMAGEN_TOKEN`. An unset default sends no token for dev mode. `--token ''` explicitly disables authentication. An explicit non-empty reference must resolve locally. The token is never printed |
| `--json` | Print the API's response as JSON, unchanged, without human output |
| `--force` | `remove` only: sends `force=true`, which also retires agents changed in the workspace |

```sh
humagen store list --json
humagen store remove core --force --server https://humagen.example.com --token op://vault/humagen/token
```

There is no `humagen store update`: the API has no call that takes a package
update. `list` shows where the server offers one (`update_available`, per
agent), and the API does not report an installed version separate from the
package's current `version`.

Errors go to stderr with exit code 1, with the HTTP status, the server's
response and what to do: 401 and 403 name `--token` and `HUMAGEN_TOKEN` (`list`
needs `agents:read`, `install` and `remove` need `agents:write`), 404 names the
unknown package, 409 on `remove` names `--force`, and a server that cannot be
reached names `--server` and `HUMAGEN_URL`.

## Import sessions, skills and agents

`humagen import` brings what you already have in other assistants into a
running platform, through its API. It adds and never changes or removes: what
the server already has is left as it is, so a second run sends only what is new.

```sh
humagen import sessions
humagen import sessions --since 720h --match humagen
humagen import sessions --from none --export ~/Downloads/conversations.json
humagen import skills
humagen import skills ~/.agents/skills --only ship,pr --scope workspace
humagen import agents --provider anthropic --model claude-sonnet-5-5
```

| Verb | Reads | Sends |
| --- | --- | --- |
| `sessions` | Claude Code sessions (`~/.claude/projects/*/*.jsonl`), Codex sessions (`~/.codex/sessions` and `~/.codex/archived_sessions`, plain or `.zst`), and with `--export` a `conversations.json` from a ChatGPT or claude.ai data export | One knowledge document per session, in sources named `sessions-TOOL-YYYY-MM` by the month the session started. `POST /api/knowledge`, then `POST /api/knowledge/{name}/documents` |
| `skills [DIR]` | Each folder under `DIR` that holds a `SKILL.md`. `DIR` defaults to `~/.agents/skills`, the store [agentspec](https://github.com/urmzd/agentspec) keeps | One draft per skill in the skill library, with the folder's files. `POST /api/library/skills` |
| `agents [DIR]` | Each Markdown agent file in `DIR` with YAML frontmatter, as Claude Code, Copilot and agentspec write them. `DIR` defaults to `~/.agents/agents` | One agent per file: its `name` and `description` from the frontmatter, its text as the system prompt. `POST /api/agents` |

A session's document holds what was asked, what was answered, one line per
tool call (the tool and what it ran or read) and the start of each tool's
result, at most 1,500 bytes of it. An attachment in an export is named, with
the text the export extracted from it; its bytes are not in an export. A
session another session started says which (`Started by`), and a ChatGPT
conversation says how many branches it left behind (`Branches not shown`): the
branch imported is the one last shown. A conversation in an export with nothing
asked in it is counted as skipped. What
looks like a credential (an API key, a token, a private key) is replaced with
`[redacted]` before anything is sent. A session over `--max-bytes` keeps what
was asked, leaves out tool results first and then cuts the answers. The document's `uri` is the session's id
(`claude:ID`, `codex:ID`, `chatgpt:ID`, `claude-ai:ID`), which is how a later
run knows it is there.

A source holds at most 200 documents and 8 MiB, so a busy month continues in
`sessions-TOOL-YYYY-MM-2` and so on. The sources are put in a corpus, titled
Imported sessions and named as `--source` is, because the console's Knowledge
page shows sources by corpus: without one they are counted there and cannot be
opened. A corpus holds 50 sources, so a long history continues in `sessions-2`.
To let an agent search the sessions, give it the corpus under Knowledge. Imported sessions are
references an agent searches and cites; they are not conversations to continue.

A skill is checked here the way the server checks it: `SKILL.md` frontmatter
the parser accepts, at most 64 files, 128 KiB a file and 512 KiB in all. The
skill keeps the `name` in its `SKILL.md`, whatever its folder is called. The
library reads the frontmatter the skill standard names (`name`, `description`,
`license`, `compatibility`, `metadata`, `allowed-tools`). A `SKILL.md` it cannot
read because of other fields, such as one assistant's `arguments` list, is
drafted with only those, and the command names each field it left out. Hidden files are left out. A skill is
drafted with no tools permitted; choose its tools, test it and publish it under
Library, Skills. A scope holds at most 256 skills: past that the command names
the ones it left out.

| Flag | Behavior |
| --- | --- |
| `--server URL`, `--token REF`, `--json` | As for `humagen store` |
| `--dry-run` | Say what would be sent and send nothing. It does not ask the server what is already there |
| `--from LIST` | `sessions`: which tools' files to read: `claude`, `codex`, both (the default) or `none` |
| `--export FILE` | `sessions`: a `conversations.json` from a ChatGPT or claude.ai export; may repeat |
| `--since WHEN` | `sessions`: only sessions started since a date (`2026-10-01`) or an age (`720h`) |
| `--match TEXT` | `sessions`: only sessions whose directory or title contains the text |
| `--source PREFIX` | `sessions`: what the sources' names start with; defaults to `sessions` |
| `--corpus NAME` | `sessions`: the corpus the sources are put in; defaults to the `--source` prefix. `none` puts them in no corpus |
| `--max-bytes N` | `sessions`: the most one document may be; defaults to 49152 |
| `--children` | `sessions`: also add sessions another session started, such as a sub-agent's or a reviewer's |
| `--home DIR` | `sessions`: the home directory the tools keep their sessions under |
| `--scope KIND` | `skills`: `personal` (the default) or `workspace` |
| `--only NAMES` | `skills`: only these folders; `agents`: only these agents. Separated by commas |
| `--provider NAME`, `--model NAME` | `agents`: required. A model provider and its model, or `policy` and a model policy |

An agent file names its model and its tools as another assistant knows them,
and neither names anything on this server. So `agents` needs `--provider` and
`--model`, the model every imported agent runs on, and creates each agent with
no tools: grant what it needs under Agents. The file's `model` and `tools` are
not sent.

`sessions` needs `knowledge:read` and `knowledge:write`; `skills` needs
`projects:read` and `projects:write`; `agents` needs `agents:read` and
`agents:write`. When some sessions, skills or agents are not
sent, the rest still are, each one left out is named with the reason, and the
exit code is 1.

# CLI
