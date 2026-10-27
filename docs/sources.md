# Sources

Library holds manual documents and managed Google Drive or Notion sources.
New Source shows supported providers and formats before configuration. Source
creation saves its selected project and refresh cadence atomically. Select a
provider and mode, enter the folder or page ID, and choose its project.
`~` selects the workspace. Project membership never replaces upstream file
permission. Collection authority cannot grant a reader access to external data.

Save your own allowed credential reference, such as an `op://` reference under
the deployment's `credentials.refs` prefixes. Saving a reference does not verify
access, create an OAuth registration, or grant a provider permission. References
are checked again at use, resolved only at the provider boundary, and never
fall back to another person or a workspace token. The `hsrc-` credential
namespace is reserved for native sources and cannot be used by MCP providers.

- Collected copy (`indexed`) retains an atomic copy. Refresh Copy imports and
  indexes it. A failed fetch, unsupported document, quota response or interrupted
  process preserves the last good copy and its timestamp.
- Live fetches using the reader's credential for every query. No document text
  is persisted in the knowledge store. Runtime use requires an explicit
  `source_search` tool grant and a private conversation with that one person.
- Copy + live query (`hybrid`) retains a copy and queries the provider with the
  current reader's credential. A provider failure never falls back to cached
  external text.

Retained external documents are currently withheld from automatic grounding,
workspace content discovery, document listings and direct store search. This
includes frozen snapshots and the connecting owner: a saved credential or an
earlier import does not establish current file access. Explicit source search
uses a fresh provider query for an unfrozen external collection in every mode.
Configured resource IDs constrain that query; a provider unable to fetch the
selected files fails closed. Query completion rechecks the source configuration
and the reader's saved reference. Runtime queries require the existing
`source_search` grant and a private single-reader conversation.

Saved variants are unavailable for external provider queries, and frozen
external snapshots cannot be searched until access to their retained version
can be verified. Neither falls back to a broader or newer provider read. Copies
and refresh history remain durable for collection bookkeeping. Serving cached
external passages needs a per-file reader authorizer before release. Authored
local documents retain their native project access rules and saved variants.
An explicit named frozen-snapshot query returns an invalid-request error.
Unfiltered global search skips frozen external sources with an Unsupported
warning while preserving results from other authorized sources.

The resource picker reads actual provider inventory with your own reference.
Select the direct children to pin, then Add Selected. Unselected unsupported
files are not fetched. Live sources do not import selections.

Drive imports the selected folder's direct children: Google documents exported
as plain text, plain text, Markdown and CSV files. Notion imports a selected
page's text blocks and supported nested text blocks. Partial pages, unsupported
formats or limits are failures, not successful truncated copies. Imports are
bounded to 200 documents, 8 MiB of text and a 45 second request deadline. Notion
walks at most 100 block lists. The last 20 attempts survive restart. Manual
collections share the same 200 document and 8 MiB limits. Durable content
cursors retain up to 10000 source
names, including deleted names, so recreating a name never reuses an old
assignment revision.

The console distinguishes saved configuration from verified work. Available and
In sync follow actual retained data or successful provider reads. Importing,
Indexing and Syncing record work in progress. A failed refresh raises one
Needs You item per source and cause in Notifications. Retrying it performs the real refresh and
settles the item only after success. It never resumes an assignment by itself.

Managed sources default to a 24 hour freshness stop rule. Assignment checks use
the sources pinned at admission, not a newly edited project selection. Missing, never-imported
or stale required sources require repair and an explicit assignment resume.
Manual sources and frozen snapshots do not expire. Keep As Snapshot freezes
a successful retained copy without claiming a fresh provider read. Replace With
New Version performs a real import while preserving the frozen-copy setting;
a failed replacement keeps the prior version.

Sync Settings permits manual refresh or an explicit interval from one minute to
seven days. A bounded service worker reads one due source at a time. It rechecks
current subject-based server policy grants and ordinary project membership; a
prior sign-in group grant cannot authorize unattended reads. Withdrawing the
interval or changing the source configuration invalidates an already selected
background candidate. Provider failure stops automatic retries until an
operator repairs and explicitly refreshes. Manual and live sources and frozen
snapshots cannot schedule imports. Durable actors do not carry sign-in group
snapshots. External collection does not authorize automatic runtime grounding.

A retained source cannot change its owner, provider, origin or scope. Create a
new source to select another audience or authority. Publication checks reject
missing source metadata and moving retained private source content to a broader
project. Retained external source history cannot be published raw. Turns granted
`source_search` require a reviewed summary rather than raw-history publication
because their content may use personal provider access.

Providers implement `knowledge.Provider`; private builds register one with
`humagen.WithSourceProvider`. Built-in implementations use the official
[Drive list](https://developers.google.com/workspace/drive/api/reference/rest/v3/files/list),
[Drive export](https://developers.google.com/workspace/drive/api/reference/rest/v3/files/export)
and [Notion block children](https://developers.notion.com/reference/get-block-children)
API boundaries. Automated transport fixtures verify those requests, denial,
quota, unsupported responses and credential redaction. No live Google Drive or
Notion account has been validated in this change.

Resource-link rule storage, CRUD, unsaved previews and disabled project-fork
snapshots are implemented. Preview always reports suggestions and filters
hidden destinations before serialization. Ambiguous verified matches cannot
auto-link. Verified connector observations and successful provider inventory/imports persist
bounded suggestions. Link and Ignore require the current version and survive
restart. Automatic relationships recheck current rules and verified metadata;
changed or ambiguous matches stop applying. Ignored resources stay ignored.
Existing surface conversations keep their placement. New accepted links select
a destination without granting the mapped actor any new project permission.

Manual attachments connect a source to a project or one channel for real turn
composition. They do not narrow read permissions or remove explicit agent
grants. Add Selected attaches only after the actual import succeeds. Detach
removes the context relationship while retaining the source and its copy.

Each citation retains the actual query mode, copy timestamp when known and
failed-refresh flag. Native evidence also retains the source authority; the
model cannot supply it. Evidence is written before model input and bound to the
run, source, document and exact passage. Reader projections are network-free.
Only explicit local/manual evidence can be returned through those projections
without an upstream file check. External or unknown evidence and evidence for
a missing source are withheld; an explicit empty citation list clears earlier
reader state. Cached document-exclusion and variant responses cannot disclose
external file titles, URIs or exclusion IDs through a metadata mutation.

These rules do not erase text already retained in historical conversation
messages or tool results. Such text continues to follow its conversation's
access rules. New personal provider queries require the private-reader tool
path, and publication requires an explicit reviewed summary. A future current-
file authorizer must define how historical retained text is handled as well as
how new passages are retrieved.

No successful external action is simulated, and no new external write action
is offered.

The source catalog labels Slack, Snowflake and BigQuery as requiring a
connector. It does not implement those providers. Live MCP queries and permitted
scheduled ingestion have separate authority and retention requirements. The
[conversation-first collection design](design/conversation-workspace.md)
records that follow-on work.

## Proposed native corpora

The next backend delivery will structure knowledge as a stable tenant-scoped
corpus, its runs, corpus-owned files, immutable file versions and version-bound
chunks. Runs attach the exact file version through a membership record; a
refresh does not duplicate the file identity. This is a proposed extension of
the current source lifecycle, not a new API already shipped in this UI pass.

| Current primitive | Proposed corpus contract |
| --- | --- |
| Source and configuration | Stable corpus with provider, selected resources, cadence, policy and published manifest |
| Bounded lifecycle attempt and last-good copy | Corpus run with checkpoints, observed outcomes and atomic complete-manifest publication |
| Document IDs and passages | Stable corpus files, immutable versions, run/file membership and chunks tied to exact pipeline revisions |

The pull planner will distinguish changed content, metadata, permissions and
confirmed deletion using provider IDs, verified cursors/revisions and hashes.
Partial runs cannot imply absence or publish an incomplete latest corpus.
Changed/deleted files invalidate their current chunks, vectors and graph facts;
ACL or membership revocation invalidates reader/result caches independently of
content. Cached passages still require current upstream reader authorization.
Keep the existing Source API compatible and compose Saige synchronization and
retrieval rather than introduce a parallel pipeline. The concrete records,
cache keys and publication rules are in the
[corpus design](design/conversation-workspace.md#proposed-corpus-model).


Source-change watchers read the resident `content_version` cursor. Successful
changes to retained documents, provenance or curation advance it atomically.
Unchanged refreshes, provider ordering, connection edits and failed attempts do
not. Deletion keeps a durable tombstone cursor, and recreating a name starts
after its history. No provider polling is used for this trigger.


The source Used By view includes actual query-time conversation references and
files published by those retrieving runs, up to 50 each within a bounded
200-run evidence window. Files are labelled as publication relationships, not
inferred content dependencies. Every relationship requires source access,
run access and, for files, artifact access; hidden records consume no result
slots and produce no count. These relationships survive evidence/index replay.
The lower source index has no typed skill/memory origin fields. A trusted
`SourceDependencyReader` can project authoritative extra-store origin records
from at most 200 already-visible retrieval runs. It must independently enforce
current skill/memory access and ordinary exact-owner origin access. The lower
layer verifies each private origin run belongs to the supplied evidence set,
bounds rows, accepts only internal links and omits origin IDs from responses.
Without that adapter the UI states both limits explicitly. Matching names in
instruction or note text never creates a graph edge.

## Optional live provider smoke tests

The default Go suite excludes `TestProviderLiveSmoke` through the
`live_integration` build tag. To run it, enable that tag and prepare a separate,
non-sensitive test resource and a read-only provider token. The test
reads provider content into process memory, but does not print it or write to
Drive or Notion. It uses an isolated in-memory Humagen store. Avoid shell
tracing and CI logs that expose environment variables. Do not use real customer
content or production credentials.

For Drive, create a folder containing a supported text file or Google Doc and
share it with the token's identity. Supply the folder ID as `ORIGIN` and the
file ID as `RESOURCE`. For Notion, share a page with the integration and use
its page ID for both values. Use a small page with supported text blocks, and
place a unique single-word marker in the selected document. The test searches
for this marker; it should be a non-sensitive alphanumeric word. Provider
limits and supported formats above apply. The Drive token needs access to list
and read the selected resource; the Notion integration needs read access.

Set the variables for each provider being tested. `CREDENTIAL_REF` uses the
normal `env:`, `keychain:` or `op://` conventions; it is a reference, not a
literal token. For example, with credentials already stored in environment
variables, run one provider at a time:

```sh
export HUMAGEN_SMOKE_DRIVE_CREDENTIAL_REF=env:DRIVE_ACCESS_TOKEN
export HUMAGEN_SMOKE_DRIVE_ORIGIN=your_test_folder_id
export HUMAGEN_SMOKE_DRIVE_RESOURCE=your_test_file_id
export HUMAGEN_SMOKE_DRIVE_MARKER=uniquesmokemarker
HUMAGEN_SOURCE_LIVE_TEST=drive go test -tags=live_integration -race -count=1 -run '^TestProviderLiveSmoke$' -v ./pkg/knowledge
```

For Notion, use the corresponding `HUMAGEN_SMOKE_NOTION_CREDENTIAL_REF`,
`HUMAGEN_SMOKE_NOTION_ORIGIN`, `HUMAGEN_SMOKE_NOTION_RESOURCE`, and
`HUMAGEN_SMOKE_NOTION_MARKER` variables and set
`HUMAGEN_SOURCE_LIVE_TEST=notion`. Set it to `both` only when all eight
variables are configured. Missing or invalid configuration fails an enabled
run rather than silently skipping it. Do not paste credentials or private IDs
into issue reports.

The test discovers and refreshes a selected resource, attempts a read with an
invalid token, and verifies durable last-good data and refresh history. Retained
external search must still fail closed after credentials recover. A fresh
selected-file read builds an ephemeral index for live hits and reader-bound
evidence. It checks unchanged refresh and authentication failure; it does not
change provider content, simulate outages or exercise the full HTTP API, UI or
runtime access-control path. A skipped or fixture-only run is not live
validation. Record live validation only after the opted-in test actually passes.
