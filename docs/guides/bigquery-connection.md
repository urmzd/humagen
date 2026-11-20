# Configure and verify a BigQuery connection

Open **Connections**, choose **BigQuery**, and connect your Google account.
This obtains dedicated BigQuery read-only consent, separate from Drive or tool
server authorization. Choose an explicit dataset list, bounded query settings
and metadata refresh interval, then choose **Save Connection**. The server
assigns ownership to the signed-in person and saves a private, unverified
connector. Saving does not create cloud resources or read BigQuery data.

Choose **Verify Metadata** on the saved connector. This calls the owner-only
crawl API. The result displays the actual catalog entry count and last
successful crawl time. A failed check preserves those previous successful
values and prevents publishing through this flow until a new check succeeds.
Reloading cannot turn a failed check into success. A selected dataset that
cannot be listed fails the check instead of reporting an empty success.

Verification lists table and column metadata. It does not query rows or prove
permission to execute SQL. **Publish Metadata** is a separate action that
shares metadata with the connector's configured project, or the workspace
when no project is set. **Withdraw Metadata** removes that publication. Neither
action grants access to data rows. Only a connector's owner with knowledge
management permission sees these controls.

Google account connections expose metadata only. The current SDK uses
`jobs.insert` for SQL execution and dry runs, which the dedicated read-only
OAuth scope does not authorize. Such connectors do not expose a live-query
capability. Authorized deployment credentials retain the existing bounded,
read-only query path; the console does not request broader Google consent.

After reconnecting Google, choose **Use Reconnected Account** on an existing
OAuth connector to explicitly bind the renewed consent to its saved datasets.
Then verify metadata again. Disconnecting or reconnecting invalidates prior
bindings. Tokens stay in the encrypted connection store and are refreshed and
checked per request; they are never copied into catalog definitions.

## Deployment and access boundaries

Managed definitions persist in the catalog's existing state store and survive
restarts. Their names and quota slots are reserved atomically, with no overwrite
of an existing connector. A saved definition is immutable. This version creates
connections and renews account consent; editing or deleting a managed definition
through the console is not implemented.

Administrators may use Application Default Credentials only within the Google
project and dataset scope of an existing deployment-declared ADC connector in
their tenant. A credential reference is accepted only under the current
person's allowed reference prefixes. Both grants are checked again when used.
Raw service-account keys and access tokens are not accepted. Static deployment
definitions keep their existing lifecycle and cannot take over a saved name.

The form's bounds are 1–50 explicitly named datasets, 1–1024 GiB per query,
1–10,000 result rows, 1–600 seconds per query and 1–720 hours between metadata
refreshes. Backend query validation and authorization still apply.

Evidence: `pkg/catalog/managed_test.go` covers atomic name reservation and
restart; `internal/platform/bigquery_setup_test.go` covers persistence, scope
denial, dedicated owner consent and explicit reconnect. Console pure/DOM tests
cover bounded input, failed save and verification, retained state, separate
publication, authorization and read-only access. These are local tests, not
live BigQuery or browser visual validation.
