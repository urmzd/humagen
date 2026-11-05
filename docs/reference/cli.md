# CLI

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
| `--agent NAME` | GitHub defaults to `code-reviewer`; Slack leaves the default agent empty for the workspace default |
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
For Slack, configure Slack's Events API endpoint and mention the app in Slack.
See the [GitHub](github.md), [Slack](slack.md) and [client](clients.md) references
for app setup, webhook verification and routing.
