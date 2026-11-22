# Connection authorization backend

The connection API supports personal MCP OAuth, explicit workspace MCP consent,
Drive and OneDrive source binding, native Slack installation and verified GitHub App
installation. Connecting does not grant tools, agent routing or write access.
Current host ceilings, reviewed tool lists, project visibility and permissions
continue to apply.

Enable encrypted consent in a System document:

```yaml
spec:
  connections:
    encryption_key: {ref: env:HUMAGEN_CONNECTION_KEY}
    redirect_url: https://humagen.example.com/connections/callback
```

The referenced key is a base64-encoded 32-byte value. Keep it outside the state
database and retain it across restarts. Missing configuration leaves OAuth
unavailable. Optional `clients` entries configure registered MCP, Google
Workspace, Drive, OneDrive, BigQuery or Slack applications; optional `github` configures the App ID, slug, OAuth
client and deployment credential references. Provider callbacks must match the
configured redirect URL. `redirect_url` may be left out: it is then
`server.public_url` followed by `/connections/callback`, held to the same
rule: HTTPS, or plain HTTP only on a loopback address.

## Accounts a person links

| Preset | Signs in with | Keeps |
|--------|---------------|-------|
| `github-account` | The GitHub App's user authorization: client id and secret from `github`, else `HUMAGEN_GITHUB_CLIENT_ID` and `HUMAGEN_GITHUB_CLIENT_SECRET` | The GitHub user's numeric id, login and avatar. No token |
| `slack-account` | Sign in with Slack (`openid profile`): client id and secret from `clients.slack`, else `HUMAGEN_SLACK_CLIENT_ID` and `HUMAGEN_SLACK_CLIENT_SECRET` | The Slack user and team ids, name and picture. No token |

They use the same `authorize`, `callback` and `DELETE` routes as every
connection, and the preset's `account` field says who the person linked. See
[GitHub](github.md#linked-accounts) and [Slack](slack.md#linked-accounts).

## Tool servers a person connects

Each of these is a vendor's own MCP server. A person connects it in Settings,
Connections, signs in with the vendor, reviews the tools it offers and
approves the list. An agent then uses them when it is granted `person_tools`
and the workspace's `tool_servers` ceiling allows the server's host.

| Preset | Server | Sign-in |
|--------|--------|---------|
| `notion` | `https://mcp.notion.com/mcp` | The server registers Humagen itself |
| `linear` | `https://mcp.linear.app/mcp` | The server registers Humagen itself |
| `granola` | `https://mcp.granola.ai/mcp` | The server registers Humagen itself |
| `atlassian` | `https://mcp.atlassian.com/v2/mcp`: Jira, Confluence, Jira Service Management and the Teamwork Graph | The server registers Humagen itself. An Atlassian administrator may have to allow the deployment's domain in the Rovo MCP server settings |
| `gmail`, `google-calendar`, `google-drive`, `google-docs`, `google-sheets`, `google-slides` | `https://<product>mcp.googleapis.com/mcp/v1`, Google's Workspace servers, in Developer Preview | One Google application of the deployment's, below |

Google registers no client for a caller, so the six Google presets share one
OAuth client you create in a Google Cloud project, of type Web application,
with `redirect_url` as its authorized redirect URI and the Gmail, Calendar,
Drive, Docs, Sheets and Slides MCP APIs enabled:

```yaml
spec:
  connections:
    encryption_key: {ref: env:HUMAGEN_CONNECTION_KEY}
    redirect_url: https://humagen.example.com/connections/callback
    clients:
      google:
        client_id: 1234567890-abc.apps.googleusercontent.com
        client_secret: {ref: env:HUMAGEN_GOOGLE_CLIENT_SECRET}
```

Its issuer and scopes are fixed. Each person consents per product, to that
product's own scope: `gmail.modify`; `calendar.events` and
`calendar.readonly`; `drive`; and for Docs, Sheets and Slides the editor's
scope with `drive.readonly`. Without the `google` client the six presets are
listed as unavailable and say so. This `google` client is separate from
`drive` and `bigquery`, which read a knowledge source and a catalog and take
clients of their own.

The presets were checked against each server's published metadata. Signing
in to a real Atlassian or Google account through them has not been exercised.

`GET /api/connections/presets` reports availability and the current person's
consent. Authorize and callback routes bind one-use state to tenant, person and
provider. Custom MCP registration also binds the issuer and exact resource.
Workspace callbacks require the initiating administrator. Credentials and state
are encrypted; credentials are never returned. Native installs create disabled
clients with empty grants, and their deployment credential references are hidden
from client responses.

`GET /api/connections/{provider}` shows only readable mappings and reported
scopes. Unknown scopes remain unknown. The check routes perform bounded read-only
probes and retain a timestamped, sanitized result tied to the checked consent or
client revision. A successful probe does not establish webhook delivery or
future access. Disconnect removes local consent; provider-side revocation is a
separate action.

Drive and OneDrive authorization must be explicitly bound to the source before use. The
binding covers its provider, origin and project. Source reconfiguration or
renewed consent requires a new binding. Personal tools refresh credentials at
use and recheck the approved resource and tool list.

Offline provider and HTTP contract tests cover consent isolation, replay,
unsafe discovery, managed installation, health, reference redaction and API
shapes. Live provider registration and consent are not part of that evidence.

Native Slack API calls also recognize confirmed authentication errors inside
HTTP 200 responses. A rejection is tied to both the token sent and its consent
revision; a delayed response cannot reject renewed consent or a refreshed token.
GitHub App token-mint HTTP 401 failures use a private, verified installation
receipt to find the current consenting person. Reconnecting replaces that
receipt, and disabling installations invalidates it and pending callbacks.
Legacy installations without this receipt do not guess a recipient from the
client creator. Network errors, HTTP 5xx and ordinary HTTP 403 permission refusals
do not create reconnect alerts. These boundaries are tested with fake providers;
live provider behavior has not been exercised.

Provider error references: [Slack authentication errors](https://docs.slack.dev/reference/methods/auth.test/)
and [GitHub installation access tokens](https://docs.github.com/en/rest/apps/apps#create-an-installation-access-token-for-an-app).
