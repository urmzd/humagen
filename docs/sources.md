# Sources

Context holds manual documents and managed Google Drive or Notion sources.
Create a source, open Configure Source, select a provider and mode, enter the
folder or page ID, and explicitly choose its project. `~` selects the workspace.
An indexed copy is shared with readers of that project. The connecting person
must be allowed to share the selected resource with that audience.

Save your own allowed credential reference, such as an `op://` reference under
the deployment's `credentials.refs` prefixes. Saving a reference does not verify
access, create an OAuth registration, or grant a provider permission. References
are checked again at use, resolved only at the provider boundary, and never
fall back to another person or a workspace token. The `hsrc-` credential
namespace is reserved for native sources and cannot be used by MCP providers.

- Indexed retains an atomic copy. Refresh Copy imports and indexes it. A failed
  fetch, unsupported document, quota response or interrupted process preserves
  the last good copy and its timestamp.
- Live fetches using the reader's credential for every query. No document text
  is persisted in the knowledge store. Runtime use requires an explicit
  `source_search` tool grant and a private conversation with that one person.
- Hybrid keeps the indexed copy and searches live resources whose stable IDs
  are not already pinned in that copy. A live failure does not remove indexed
  grounding. Search responses identify the unavailable live portion.

Drive imports the selected folder's direct children: Google documents exported
as plain text, plain text, Markdown and CSV files. Notion imports a selected
page's text blocks and supported nested text blocks. Partial pages, unsupported
formats or limits are failures, not successful truncated copies. Imports are
bounded to 200 documents, 8 MiB of text and a 45 second request deadline. Notion
walks at most 100 block lists. The last 20 attempts survive restart. Manual collections share the same 200
document and 8 MiB limits. Durable content cursors retain up to 10000 source
names, including deleted names, so recreating a name never reuses an old
assignment revision.

The console distinguishes saved configuration from verified work. Available and
In sync follow actual retained data or successful provider reads. Importing,
Indexing and Syncing record work in progress. A failed refresh raises one
Needs-you item per source and cause. Retrying it performs the real refresh and
settles the item only after success. It never resumes an assignment by itself.

Managed sources default to a 24 hour freshness stop rule. Assignment checks use
the sources pinned at admission, not a newly edited project selection. Missing, never-imported
or stale required sources require repair and an explicit assignment resume.
Manual sources do not expire. Scoped runtime grounding currently requires the
exact connecting owner with a current subject-based project role. Durable
actors do not carry sign-in group snapshots, so other readers fail closed.

A retained source cannot change its owner, provider, origin or scope. Create a
new source to select another audience or authority. Publication checks reject
missing source metadata and moving retained private source content to a broader
project. Turns granted `source_search` require a reviewed summary rather than
raw-history publication because their content may use personal provider access.

Providers implement `knowledge.Provider`; private builds register one with
`humagen.WithSourceProvider`. Built-in implementations use the official
[Drive list](https://developers.google.com/workspace/drive/api/reference/rest/v3/files/list),
[Drive export](https://developers.google.com/workspace/drive/api/reference/rest/v3/files/export)
and [Notion block children](https://developers.notion.com/reference/get-block-children)
API boundaries. Automated transport fixtures verify those requests, denial,
quota, unsupported responses and credential redaction. No live Google Drive or
Notion account has been validated in this change.

Resource-link rule storage and disabled project-fork snapshots are implemented.
Rule management, resource discovery, connector application and the integration
configuration flow remain under implementation. No successful external action
is simulated, and no new external write action is offered.


Source-change watchers read the resident `content_version` cursor. Successful
changes to retained documents, provenance or curation advance it atomically.
Unchanged refreshes, provider ordering, connection edits and failed attempts do
not. Deletion keeps a durable tombstone cursor, and recreating a name starts
after its history. No provider polling is used for this trigger.
