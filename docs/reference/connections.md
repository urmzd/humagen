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
Native response-token rejection hardening is a separate following layer.
