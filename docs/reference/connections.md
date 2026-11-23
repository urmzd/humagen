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

A person links their own GitHub account and Slack account in the console,
under Connected accounts in their settings (`/settings/connections`). A link
records who they are on that surface: the surface's own user id beside their
subject, in their workspace. It holds no credential.

Linking by itself changes nothing about whom a GitHub or Slack user acts for:
a client's `github.people` and `slack.people` maps still decide that
([GitHub](github.md#who-may-ask), [Slack](slack.md#who-can-start-a-run)).
Reading links there is planned and not built.

| Preset | Signs in with | Keeps |
|--------|---------------|-------|
| `github-account` | The GitHub App's user authorization | GitHub's numeric user id, with the login and avatar for display. No token |
| `slack-account` | Sign in with Slack (OpenID Connect, scopes `openid profile`) | Slack's user id and team id, with the name and picture for display. No token |

### The flow

```text
console            Humagen                              GitHub / Slack
Connect  ───────▶  state for this person and browser ─▶ authorize
callback ◀───────  code + state                    ◀─── redirect_url
           ───────▶ exchange the code, read who signed in
                    record (surface, user id) beside the person; keep no token
```

| Step | What holds |
|------|------------|
| Start | `POST /api/connections/{provider}/authorize`, from the person's browser session only: a call with a bearer token, and one an agent or the MCP endpoint dispatched, is refused with 403. The answer is the provider's consent address with a `state` of 32 random bytes and a PKCE challenge (S256). The browser is given an HttpOnly cookie, one per provider, that the consent is bound to. A person has one pending consent per provider, good for 10 minutes; starting again replaces it |
| Return | The provider redirects to `redirect_url`, which is configuration. The console removes `code` and `state` from the address, shows Connections, and posts them to `POST /api/connections/{provider}/callback` |
| Finish | The consent is completed only by the person who started it, in the browser that started it, once, before it expires. Anything else is refused and links nothing. The code is exchanged with the application's client id and secret and the PKCE verifier |
| Identity | One read with the new token: `GET /user` on GitHub, `openid.connect.userInfo` on Slack, of at most 1 MiB. The token is not written anywhere, logged or returned. A GitHub answer that is not a user, and a Slack team that none of the workspace's enabled Slack clients is in, are refused |
| Record | The surface's user id (and on Slack the team id) beside the person's subject, in the person's workspace |

### The rules

| Rule | Behavior |
|------|----------|
| Key | GitHub's numeric user id; Slack's user id with its team id. A login or display name is kept for display only |
| One person per surface user | In a workspace, a second person's link to the same user is refused with 409 until the first is disconnected |
| One account per person | Linking another GitHub account replaces the first. A person has one Slack account per Slack team |
| Workspace | A link belongs to the person's workspace, which comes from their sign-in. Another workspace may link the same user to its own person |
| Disconnect | `DELETE /api/connections/{provider}`, by the person, in their browser. An administrator removes one on the GitHub or Slack app's sheet: `DELETE /api/clients/{id}/accounts/{account}` (`clients:write`, audited) |
| Who sees links | The person sees their own in `GET /api/connections/presets` (`account`). `GET /api/clients/{id}/accounts` (`clients:read`) lists up to 200 on a client's surface; a Slack client lists its own team's. Another workspace's client is not found |
| Machines | A machine credential links nothing |

### Turning it on

Linking is off until all of this is set. Each surface is on by itself.

1. A `connections` block with an `encryption_key`
   ([spec](spec.md#service-connection-authorization)). `redirect_url` is
   `server.public_url` followed by `/connections/callback` unless set.
2. The application's OAuth client:

   | Surface | In the application's settings | Given to Humagen as |
   |---------|-------------------------------|---------------------|
   | GitHub | The GitHub App's **Callback URL** is `redirect_url`. Generate a **client secret**. **Expire user authorization tokens** on. No user permission is needed: only `GET /user` is read | `connections.github` (`client_id`, `client_secret`) where the console installs the app, else `HUMAGEN_GITHUB_CLIENT_ID` and `HUMAGEN_GITHUB_CLIENT_SECRET` |
   | Slack | Under OAuth & Permissions, add `redirect_url` to **Redirect URLs**, and the **User Token Scopes** `openid` and `profile` | `connections.clients.slack` (`client_id`, `client_secret`) where the console installs the app, else `HUMAGEN_SLACK_OAUTH_CLIENT_ID` and `HUMAGEN_SLACK_OAUTH_CLIENT_SECRET` |

3. For Slack, an enabled Slack client of the workspace whose team is the one
   people sign in from.

A surface with no client is not offered: its row is not shown. A client id
from the environment with no secret, or a secret that cannot be read, stops
the server at start.

Tested with stand-ins for GitHub and Slack. A real GitHub or Slack consent
has not been exercised.

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
