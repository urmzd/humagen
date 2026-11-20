# Connections

`/connections` is the console address for messaging services, personal OAuth
accounts, approved tool lists and knowledge sources. Existing connection routes
remain aliases during the session migration.

## Personal OAuth accounts

Notion, Linear and Granola use their published MCP endpoints. A person chooses
Connect, authorizes access at the service, then reviews and approves the exact
tool list. Connecting an account does not grant its tools to an agent. Agents
still need the personal-tools grant, and workspace/project host ceilings still
apply. Removing an account stops its credentials from being used by admitted runs.

Enable the flow in the System document:

```yaml
spec:
  connections:
    encryption_key: {ref: env:HUMAGEN_CONNECTION_KEY}
    redirect_url: https://humagen.example.com/connections/callback
```

The key is 32 random bytes encoded with standard base64. Keep it outside the
state database and retain it across restarts. Changing it requires people to
reconnect. Without the key, OAuth is unavailable and the console explains why.
No provider or host access is enabled by this configuration alone.

Where a provider does not offer Dynamic Client Registration, configure a
pre-registered client, bound to the expected issuer:

```yaml
spec:
  connections:
    encryption_key: {ref: env:HUMAGEN_CONNECTION_KEY}
    redirect_url: https://humagen.example.com/connections/callback
    clients:
      linear:
        issuer: https://authorization.example.com
        client_id: registered-client-id
        client_secret: {ref: env:HUMAGEN_LINEAR_CLIENT_SECRET}
        scopes: [read]
```

The issuer and scopes above are illustrative. Use the provider's actual
registration. Public clients may omit the secret. Discovery follows protected
resource metadata (RFC 9728) and authorization server metadata (RFC 8414),
requires PKCE S256, and uses DCR (RFC 7591) for public clients when available.
The service resource is included in authorization and code exchange.

State is bound to tenant, person and provider, expires after ten minutes, and is
consumed once before exchanging the code. State, client secrets and rotating
tokens are encrypted with AES-256-GCM; tenant and record key are authenticated
associated data. Tokens refresh a minute before expiry with a persisted lease
so processes do not rotate the same token concurrently. OAuth HTTP requests
refuse redirects and private addresses, including DNS answers to public names.
A supplied HTTP client is a deployment-owned network-policy boundary.

## Existing credentials

Saved Slack and GitHub credentials are explicitly shown as unverified. A refused
connection preserves the entered secrets and places the error next to Connect.
The personal tool form lists permitted hosts and rejects an unlisted host before
making a discovery request. Credential references remain subject-bound and do
not fall back to another person's or the workspace's secret.

An MCP OAuth authorization is not consent to use a native source API. Native
Notion source sync continues to require its own credential scope.

## Custom and workspace connections

Other accepts a public HTTPS MCP endpoint. Protected-resource discovery also
accepts a resource-metadata URL from an unauthenticated `WWW-Authenticate`
challenge, while enforcing exact resource and issuer matching. Metadata,
registration and token requests retain the public-address and redirect guards.
If the service requires a registered application, select **Use a registered
application** and enter its issuer, client ID and optional secret. Discovery must
match that issuer. These values stay encrypted with consent and are reused on
reconnect; refused submissions keep the entered values beside their error.

Personal scope is the default. An administrator may explicitly choose Workspace
for an MCP connection. Consent still belongs to the initiating person; another
administrator cannot finish that callback. Shared credentials use separate
encrypted owner records. Each person must still review the tool list and have
the applicable host and personal-tool grants. Generic endpoints are not granted
native source access.

## Native provider setup

Drive, BigQuery and Slack require registered applications. Their endpoints and scopes are
fixed by the server; MCP tokens are never substituted:

```yaml
spec:
  connections:
    encryption_key: {ref: env:HUMAGEN_CONNECTION_KEY}
    redirect_url: https://humagen.example.com/connections/callback
    clients:
      drive:
        client_id: google-web-client-id
        client_secret: {ref: env:HUMAGEN_GOOGLE_CLIENT_SECRET}
      bigquery:
        client_id: google-web-client-id
        client_secret: {ref: env:HUMAGEN_GOOGLE_CLIENT_SECRET}
      slack:
        client_id: slack-client-id
        client_secret: {ref: env:HUMAGEN_SLACK_CLIENT_SECRET}
        signing_secret: {ref: env:HUMAGEN_SLACK_SIGNING_SECRET}
    github:
      app_id: "12345"
      app_slug: your-humagen-app
      client_id: github-app-client-id
      client_secret: {ref: env:HUMAGEN_GITHUB_CLIENT_SECRET}
      private_key: {ref: env:HUMAGEN_GITHUB_PRIVATE_KEY}
      webhook_secret: {ref: env:HUMAGEN_GITHUB_WEBHOOK_SECRET}
```

Register the exact callback URL in each application. Enable the Drive API and
authorize the read-only Drive scope. The flow requests offline access and consent
so the encrypted refresh token can support later reads. After authorizing Drive,
choose **Use my Google Drive account** on a source. This binds consent to that
source's provider, folder and project. Reconfiguration, reconnecting the account
or revocation requires new source consent. No other person's token is used.

Enable PKCE in the Slack app. Installation uses bot scopes for mentions,
messages, history, files and reactions. Verified team/app identifiers create a
disabled managed client with no agent routes and empty event/action grants.
Configure routing, Events and Actions before enabling it. API requests and file
downloads fetch the current token; refresh failure never falls back to a stale
token. Separate teams retain separate encrypted credentials.

For GitHub, enable **Request user authorization (OAuth) during installation**
and make the shared callback the app's first callback URL. The callback requires
code, state and installation ID. The server verifies user access and the App
installation, then creates a disabled client with empty grants. Its deployment
private key mints short-lived installation tokens. References are hidden from
client responses. **Disable installations** stops local delivery; uninstalling
the app remains a GitHub Settings action.

Provider registration instructions: [Google web-server OAuth](https://developers.google.com/identity/protocols/oauth2/web-server),
[Slack PKCE](https://docs.slack.dev/authentication/using-pkce/),
[GitHub App installation setup](https://docs.github.com/en/apps/creating-github-apps/registering-a-github-app/about-the-setup-url).

## Checking access

**Test access** makes a bounded read-only request. MCP connections discover the
available tool list under current host grants, Drive reads the account metadata
kind, Slack checks authentication against the expected team, and GitHub verifies
installation repository access. Responses include the actual check time and a
sanitized result. Saved credentials stay unverified until the check succeeds;
the result does not claim inbound webhooks or future calls will succeed. A raw
GitHub token cannot verify an App installation's identity.

The latest observation persists across reloads and replicas. It is tied to the
consent or client version that was checked, so reconnecting or changing client
configuration invalidates it. Confirmed credential rejection marks the account
as needing reconnection and raises a private alert for the consenting person;
transient network and server errors do not raise credential-rejection alerts.
Successful renewed authorization resolves that connection's alert.

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

**View Connection** shows reported granted scopes and the configured tools,
source bindings, clients, agents and projects the reader may see. Missing scope
metadata is shown as unknown, not replaced with requested scopes. These mappings
describe current configuration, not evidence of execution. GitHub App permissions
are not OAuth scopes and are not yet projected by this view.

Disconnect removes Humagen's local consent and credentials. Removing consent at
the provider remains a separate action in that service's account settings.

## Evidence and remaining validation

`pkg/connectionauth/manager_test.go` covers identity isolation, one-use and expired
state, encryption at rest, restarted refresh, ciphertext relocation, unsafe
metadata and PKCE requirements. Real provider authorization and token rotation
remain to be validated with user consent; deterministic tests are not evidence
of a successful connection to a live account.

Native and health paths are covered by offline tests; these do not establish a
working provider registration. Live consent, revoked grants, provider token
rotation and workspace sharing have not been exercised against real accounts.
BigQuery uses separate Google `bigquery.readonly` consent, per-request refreshed
credentials and an explicitly saved private connector. A selected dataset that
cannot be listed fails verification. OAuth supports metadata sync only: the
existing SQL preflight uses `jobs.insert`, whose documented scopes exclude this
read-only grant. Authorized ADC/reference connectors retain bounded SQL queries.
See [dataset listing scopes](https://docs.cloud.google.com/bigquery/docs/reference/rest/v2/datasets/list)
and [job submission scopes](https://docs.cloud.google.com/bigquery/docs/reference/rest/v2/jobs/insert).
Browser responsive acceptance remains separate from DOM tests.

The offline HTTP contract test covers presets, authorization, callback,
disconnect, rejected/replayed state, missing authentication and unknown providers.
