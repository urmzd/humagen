# Connection authorization backend

The connection API supports personal MCP OAuth, explicit workspace MCP consent,
Drive source binding, native Slack installation and verified GitHub App
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
unavailable. Optional `clients` entries configure registered MCP, Drive, BigQuery
or Slack applications; optional `github` configures the App ID, slug, OAuth
client and deployment credential references. Provider callbacks must match the
configured redirect URL.

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

Drive authorization must be explicitly bound to the source before use. The
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
