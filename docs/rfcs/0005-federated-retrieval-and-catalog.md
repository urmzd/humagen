# Federated retrieval and the catalog

Status: proposed. This document changes no behavior. It describes the target
design and the order of the changes that reach it.

## Goal

An agent can find and use the right information wherever it lives (Google
Drive, OneDrive, GCS, S3, Snowflake, BigQuery, Notion, Slack, uploads, its own
files) without Humagen copying every system into its own index. Search has to
hold at the size of a company's data, and stay inside each reader's own access.

## Current boundary

- **Retrieval.** `pkg/knowledge` is what runs are grounded in. A source is cut
  into passages of about 800 bytes and ranked with BM25 only: lexical, no
  embeddings. Its logic and passages live in memory, written through
  `pkg/state` ([status](../architecture/overview.md#status)).
- **Hybrid search exists, unused.** `fiber/pkg/knowledge` has an `Embedder`
  interface (a deterministic `Hashing` embedder and an OpenAI-compatible `HTTP`
  one), a dense arm, reciprocal rank fusion and a pgvector backend. The
  platform does not use it.
- **Providers.** Manual upload (UTF-8 text, Markdown, CSV, JSON), Google Drive
  and Notion, as a collected copy, a live query or both
  ([sources](../architecture/sources.md)). Retained external text is withheld
  until the current reader's upstream access is verified; live queries use the
  reader's own credential through the `source_search` tool.
- **Warehouses and chat.** Snowflake, BigQuery and Slack are reachable only as
  MCP tools an agent is granted. Nothing records what tables or channels exist,
  so an agent must already know where to look.

## Decision

Three rules decide where each kind of information is searched:

| Information | Where it is searched | Humagen stores |
|-------------|----------------------|----------------|
| Content in an external system (files, pages, rows, messages) | **Live**, in that system, with the reader's own credential: its search API, its SQL | Nothing, unless an owner opts into a collected copy |
| What exists in those systems (tables, columns, folders, file titles) | **The catalog**, Humagen's own index | Metadata only, never row data or file text |
| Content Humagen owns (uploads, files agents write, opted-in copies) | **Hybrid** index in `pkg/knowledge` | Passages and their vectors |

The catalog is what makes live access work at scale. An agent asks the catalog
"where is churn by region", gets back `analytics.billing.churn_monthly` with its
columns and their descriptions, then queries that table live. It never needs a
copy of the warehouse, and it never guesses a table name.

## Hybrid retrieval in `pkg/knowledge`

`pkg/knowledge` keeps its API, lifecycle, variants and access rules. It gains a
dense arm beside BM25, composed from `fiber/pkg/knowledge` rather than
reimplemented:

- **Embedder slot.** A System spec field `knowledge.embedder`, a slot:
  `none` (the default, today's behavior), `hashing`, `openai` (any
  OpenAI-compatible `/embeddings` endpoint) or `vertex` (Vertex AI, with
  Application Default Credentials). The Vertex kind defaults to
  `gemini-embedding-001` at 768 dimensions.
- **Vectors are a derived index.** A passage's vector is written under its own
  key beside the passage, tagged with the embedder's model. They are computed
  off the write path by a bounded background indexer, so an upload never waits
  on a model and a model outage never fails a write. At start, passages whose
  vector is missing or from another model are queued again.
- **Fusion.** Search embeds the query (bounded by a short timeout, outside the
  store lock), ranks the selected passages by cosine similarity, and fuses that
  list with BM25 by reciprocal rank fusion, as `fiber/pkg/knowledge` does. A
  passage without a vector still ranks lexically. If the query cannot be
  embedded, search is lexical, and the hit says so.
- **Same access rules.** Fusion runs over exactly the passages the lexical arm
  may see: the same tenant, sources, exclusions, variants and upstream-read
  checks. A vector is never served on its own.
- **Limits.** Vectors are compared in memory, exhaustively, like the passages
  are today. At 768 dimensions in float32 that is about 3 KB a passage, so the
  current caps (`MaxImportDocuments`, `MaxImportBytes`) bound it. Moving to the
  pgvector backend is the step after `pkg/state` stops loading whole
  namespaces ([limits at scale](../architecture/overview.md#limits-at-scale)).

## Connectors

A connector is one kind in a spec slot, declaring what it can do:

| Capability | Meaning | Example |
|------------|---------|---------|
| `live` | Search or read in place with the reader's credential | Drive search, a BigQuery query |
| `collect` | Fetch selected resources into a collected copy | Drive folder, Notion page, S3 prefix |
| `catalog` | List what exists, with schema and descriptions | BigQuery `INFORMATION_SCHEMA`, Snowflake `SHOW`, GCS object listing |

The existing `Provider` (`Fetch`) becomes the `collect` capability. Drive and
Notion keep working unchanged. New kinds, in order of value: BigQuery and
Snowflake (`catalog`, `live`), GCS and S3 (`collect`, `catalog`), OneDrive
(`live`, `collect`). A system that already has a good MCP server can be a
connector whose `live` capability is that server's tools, so Humagen does not
reimplement a client it can compose.

Credentials follow [sources](../architecture/sources.md): a reference, resolved
at the provider boundary, never a workspace fallback for a person's read. A
connector may also name a workspace credential (`env:`, a service account)
for `catalog` only, when an owner chooses to publish metadata (below).

## The catalog

`pkg/catalog` is a tenant-scoped store of entries, each one an object in a
connected system:

| Field | Example |
|-------|---------|
| `system`, `kind` | `bigquery`, `table` |
| `path` | `analytics.billing.churn_monthly` |
| `title`, `description` | from the system, else empty |
| `schema` | columns with names, types, modes and descriptions |
| `stats` | row count, size, last modified, when the system reports them |
| `connector`, `seen_at` | which connector listed it, and when |

- **Filled by a crawl.** A connector's `catalog` capability is run on demand or
  on the source's refresh cadence. A crawl replaces its connector's entries
  atomically, so a deleted table disappears and a partial crawl publishes
  nothing.
- **Searched hybrid.** Each entry is rendered to one short text (path, title,
  description, column names and descriptions) and indexed lexically and with
  the configured embedder, the same way `pkg/knowledge` does.
- **Used by agents through a tool.** `catalog_search`, granted explicitly like
  `source_search`, returns entries with their schema. The agent then queries
  the system live through its connector or MCP tool. The console gets a
  Catalog view to browse and search the same entries.
- **Never data.** Entries hold structure and descriptions. Sample values are
  off, and an owner can turn them on per connector.

### Who sees an entry

Table and column names can themselves be sensitive. An entry is visible to:

1. the person whose credential crawled it, by default;
2. the members of the connector's project, only when its owner publishes the
   connector's metadata. Publishing is explicit, recorded with who and when,
   and can be withdrawn, which hides the entries at once.

Seeing an entry never grants reading the object. A live query still runs with
the reader's own credential and fails if they lack access.

## What gets vectors

| Content | Vectors |
|---------|---------|
| Uploaded documents and pasted text | Yes, through the embedder slot |
| Files agents write (artifacts) | Yes, opt-in per project, indexed as a native corpus |
| Collected copies of external content | Yes, when an owner chose a copy |
| Live external content | No: searched in place |
| Catalog entries | Yes: one vector per entry, metadata only |

PDF and Office extraction for uploads is a separate change; it adds formats
to the same pipeline.

## Alternatives

- **Ingest everything into one vector store.** Rejected: it duplicates data
  Humagen should not hold, goes stale between syncs, multiplies cost, and
  needs every system's permissions mirrored to be safe. The existing rule that
  collection authority never grants external access already rules out serving
  most of it.
- **MCP tools only, no catalog.** Rejected as the whole answer: an agent cannot
  find what it does not know exists, and listing a warehouse in every turn
  costs tokens and time. The catalog is the index MCP lacks.
- **pgvector now.** Deferred: the platform's stores load whole namespaces into
  memory today, so the in-memory dense arm matches how every other store works.
  The vector index is derived and rebuildable, so moving it later loses
  nothing.

## Changes

1. **Hybrid search.** The embedder slot, the Vertex embedder, the derived
   vector index with its background indexer, and fusion in `pkg/knowledge`.
   Spec reference and status table updated.
2. **Catalog core.** `pkg/catalog` with hybrid search, the `catalog_search`
   tool, `GET /api/catalog`, and the publish and withdraw rules.
3. **Warehouse connectors.** BigQuery and Snowflake with `catalog` and `live`.
4. **Storage connectors.** GCS and S3 (`collect`, `catalog`), then OneDrive.
5. **Console.** The New Source catalog groups connectors by capability, and a
   Catalog view browses and searches entries.
6. **Agent files.** Opt-in indexing of artifacts as a native corpus.

Each change is its own PR and keeps the previous behavior when its settings
are left out.

## Open questions

- Whether Vertex's task types (`RETRIEVAL_QUERY` and `RETRIEVAL_DOCUMENT`)
  should become part of the `Embedder` interface, which today does not tell a
  query from a passage.
- How often a published catalog should be recrawled by default, and whether a
  connector that supports change feeds should push instead.
- Whether column-level descriptions written in Humagen (a data dictionary)
  should be stored as catalog annotations that survive a recrawl.
