# Managed BigQuery connections

`GET /api/catalog/connectors/bigquery/setup` reports the caller's available
credential modes and existing OAuth connector names. `POST
/api/catalog/connectors/bigquery` saves a private, unverified connector owned by
the authenticated person. Saving performs local validation and consent binding;
it does not call Google or publish metadata.

A definition names one Google project and 1–50 explicit datasets. Limits are
1–1,099,511,627,776 bytes billed, 1–10,000 rows, 1–600 seconds and 1–720 hours
between refreshes. The credential mode defaults to dedicated BigQuery OAuth.
ADC and reference modes additionally require client-management permission and
are limited to the deployment's permitted project/datasets or the caller's own
reference prefixes. Raw keys and access tokens are not accepted.

Use the existing owner-only crawl route to verify metadata. A selected dataset
that cannot be listed fails verification. A failed crawl retains the last
successful metadata. Publishing and withdrawing metadata remain separate owner
actions and never grant access to data rows.

Dedicated OAuth is metadata-only. It cannot use the SDK's SQL execution or dry
run path. Authorized ADC/reference connectors retain the existing bounded query
path. Every operation rechecks current ownership, permission and binding;
credentials are resolved when used and are not embedded in catalog definitions.

After renewing Google consent, `POST /api/catalog/connectors/{name}/reconnect`
explicitly binds it to the same immutable owned definition. Then crawl again.
Managed definitions persist across restarts, reserve their names atomically and
cannot be overwritten by static deployment definitions. Managed definition
editing or deletion is not exposed by this API.

Evidence: the setup HTTP contract and platform race tests cover authorization,
limits, owner consent and reconnect. The managed-name reservation and three
cross-process catalog tests pass against both memory and actual PostgreSQL.
These tests use fake provider boundaries and do not establish live Google access.
