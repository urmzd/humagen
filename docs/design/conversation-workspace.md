# Conversation-first delivery and collections

This is a proposed architecture for follow-on work. The current UI delivery is
tracked in [the task queue](../ui-task-queue.md). A catalog entry, a dependency's
capability or a design example is not a working Humagen integration.

## Stay with the work

The project is the home object for work, scope and permissions. Within that
scope, the conversation is the primary interaction: ask for a result, inspect
its evidence, revise it and review publication in the same place. Specialized
views earn their place by making a concrete review or management task easier.
Library and Operations use the same authoritative resources; they must not
introduce a second execution path or a separate set of access rules.

Skills are immutable packages with files. Memory chains are managed file views
over scoped records. Personal, Agent, Project and Company identify real owners;
folders do not create new permissions. A chat action must preserve its scope
and open an editable native draft before any work runs.

| Capability | Current boundary | Follow-on work |
| --- | --- | --- |
| Conversation delivery | Native runs, SSE, shared presentation and immutable deliverables | More registered generators and inline views over existing result versions |
| Skills and Memory | Scoped file browsers over native stores and actions | Additional managed resource types through those same stores and policies |
| Sources | Manual BM25 collections, native Drive/Notion boundaries and recorded refresh attempts | A permission-aware Saige retrieval adapter and additional collection providers |
| Browser access | No embedded Chrome service | An authenticated, isolated browser workspace and phone interaction |
| Agent protocols | Fiber AgentSpec runtime and `pkg/call` | Explicit A2UI, A2A and AG-UI adapters with conformance evidence |
| Improvement loop | Native eval suites and reviewed publication primitives | Reviewed production fixtures, candidate comparison and human-approved release/rollback |

## Proposed corpus model

Organize managed knowledge as corpora, corpus runs, files and file-version
chunks. A file belongs to the corpus and is attached to each relevant run;
refreshing it does not create a second file identity. These are proposed
contracts for the next backend delivery, not entities already implemented by
the current source store.

| Record | Identity and relationship | Retained facts |
| --- | --- | --- |
| `Corpus` | Stable tenant-scoped ID | Provider/origin, selected resources, schedule, policy, published manifest and freshness |
| `CorpusRun` | Attempt ID belonging to one corpus | Trigger, status, checkpoints, provider cursors, enumeration completeness, staged manifest and observed outcome |
| `CorpusFile` | Stable corpus file ID, unique by provider authority and upstream file ID | Current published version, metadata pointers, deletion state and independent access/revocation revision |
| `FileVersion` | Immutable version belonging to one corpus file | Provider revision/ETag, metadata and content hashes, captured bytes or reference, extraction revision and access observation at collection |
| `RunFile` | Run/file membership referencing the exact observed file version | Planned action, observed change, outcome, error and whether bytes were fetched or reused |
| `Chunk` | Exact file version plus chunker revision and chunk position | Text/hash, offsets and source location |
| Derived embedding or fact | Exact supporting chunks plus pipeline/model revision | Vector or assertion, provenance, index generation and current visibility/tombstone state |

Every record carries tenant confinement. Run membership does not transfer file
ownership or permission. Historical versions and a version's saved access
observation are evidence of what was collected, never current reader grants.
Facts spanning files retain every supporting chunk/source so authorization can
be checked before a fact is returned.

### Pull, publish and invalidate

1. Build a bounded pull plan from verified provider IDs, cursors, revisions,
   ETags and prior hashes. Separate content, metadata, permission and deletion
   observations. Fetch bytes only when the provider evidence or required
   pipeline revision makes them necessary; compute content hashes to confirm
   reuse without substituting hashes for upstream file identity.
2. Persist attempt checkpoints and idempotent `RunFile` outcomes. Partial or
   failed enumeration cannot imply file absence, deletion or a complete new
   corpus. Resume/retry within explicit item, byte, time and attempt bounds.
3. Stage file versions, chunks, vectors and graph updates. Atomically publish
   the complete successful manifest, its provider cursor and index generation.
   On failure, retain the last-good manifest and its actual freshness; a run's
   partial progress is not a successful refresh.
4. Replace current derived descendants when content or pipeline changes.
   Tombstone deleted files and remove their chunks, embeddings and facts from
   current retrieval. Retain immutable historical versions only under the
   configured retention and access rules.

| Change | Required invalidation |
| --- | --- |
| Metadata only | Update observed metadata and filters; invalidate metadata-dependent results without automatically re-embedding unchanged text |
| Content/provider revision | Publish the exact new version; replace affected chunks, embeddings, graph facts and result-cache entries |
| Permissions or revocation | Advance authorization state independently of content; deny affected results and invalidate reader/result caches immediately |
| Confirmed deletion | Tombstone the file and derived descendants in the next complete publication; withhold any separately observed revoked access immediately |
| Chunker, extractor, model or retrieval revision | Rebuild affected derived data or query results under a new generation before publication |

Derived cache keys include tenant, corpus, file version, chunker/extractor,
model and retrieval revisions. Query caches also bind the query and published
index/graph generation. Cached results retain current-reader authorization
requirements and are checked again before use; no cache entry can outlive
revocation merely because its content hash is unchanged.

### Map from the current sources

Keep existing Source APIs compatible while adding the corpus contracts behind
the native source/retriever seams. Today's Source configuration maps toward
`Corpus`, lifecycle attempts toward `CorpusRun`, and current document IDs and
passages toward stable files and version-bound chunks. The current store has
bounded attempts and atomic last-good copies; it does not yet implement the
full run/file/version/manifest graph above. A live query remains a current-reader
operation and does not silently create a shared corpus publication.

## Compose retrieval rather than fork it

Humagen currently retrieves local passages through its own BM25 store. The
pinned Saige v0.41.0 already exposes lexical/vector composition, reciprocal
rank fusion, reranking and incremental source synchronization. Reuse those
interfaces behind the native retriever; Humagen keeps tenant identity, source
configuration, authorization, scheduling and atomic publication. See
[Saige's retrieval configuration](https://github.com/urmzd/saige/blob/v0.41.0/rag/config.go)
and [the ecosystem plan](../ecosystem.md).

The adapter must fix the pipeline to the host's tenant scope, preserve external
file identity independently of content deduplication, and authorize each
passage before it reaches a result or model context. A project grant, content
hash, vector index or graph namespace cannot replace the provider's file check.
Stage partial sync results and publish a complete successful collection
atomically; a failed refresh must retain the last-good version without serving
unauthorized cached content.

Saige's graph interfaces expose fact search and episode provenance. A future
Humagen fact must resolve to readable supporting sources at query time.
Unattributed facts from protected collections are withheld. Entity merging and
cross-document relationships must preserve source authority and deletion
lineage. See [Saige's graph contract](https://github.com/urmzd/saige/blob/v0.41.0/rag/knowledge/types/types.go).

## Slack has two separate paths

Slack's official MCP service uses Streamable HTTP at `https://mcp.slack.com/mcp`,
a registered app and user authorization. It does not support Dynamic Client
Registration. Humagen's planned live-search adapter must retain those app,
scope and reader checks. See [Slack's MCP documentation](https://docs.slack.dev/ai/slack-mcp-server/).

An internal channel collector is a separate proposed connector. Establish its
eligibility, approved channels, scopes, retention and intended use before
collecting data. Record bounded backfills and checkpoints, honor rate limits,
and propagate edits, deletions and membership revocation through documents,
vectors and graph facts. A different API method is not a way around policy.
Slack distinguishes internal organization apps from third-party distribution;
that distinction does not remove authorization obligations. See
[Slack's internal-app guidance](https://slack.dev/data-security-best-practices-for-agentic-slack-apps/)
and [API terms](https://slack.com/terms-of-service/api).

## Website retrieval and a browser workspace

Start website retrieval with explicit URLs, bounded responses and a guarded
HTTP client. Saige's HTTP source accepts an injected client; its default client
does not itself establish Humagen's network policy. Compose it with the guarded
fetch client and the deployment's authorized network policy. See
[the HTTP source](https://github.com/urmzd/saige/blob/v0.41.0/rag/source/http.go)
and [safe HTTP client](https://github.com/urmzd/saige/blob/v0.41.0/tools/fetch/safehttp.go).

Interactive Chrome requires a separate isolated service, authenticated session
ownership, controlled navigation and a view usable on a phone. Tailscale can
provide private reachability; it does not replace Humagen authentication or
provider permissions. The loopback development preview is not that service.

The requested [Cerebras knowledge-base article](https://www.cerebras.ai/blog/how-we-built-our-knowledge-base)
returned HTTP 500 during research. Its body remains unverified, so this design
does not attribute an architecture to it.

## Missing capabilities stay explicit

Define a bounded capability-gap response containing the missing renderer,
generator or protocol, its version, the target surface, the reason it is
unavailable and a supported text, table or file fallback. The conversation
should show the useful fallback and a clear request to build the missing view.

Building a renderer or generator uses the existing child-run path, ordinary
tool grants and a reviewed component registration. Generated code or a model's
claim cannot register a capability or prove a successful outcome. Protocol
adapters must preserve native actor, run, approval and result identities;
native SSE alone is not A2UI, A2A or AG-UI conformance. AgentSpec remains the
current deployment/runtime boundary. Unifying definitions is still the
separate [ecosystem step 17](../ecosystem.md).

## Human-reviewed improvement

A proposed improvement starts with an authorized production case. A person
reviews a redacted fixture, a bounded repair run creates a candidate, and evals
compare it with the retained baseline. Missing measurements remain inconclusive.
Only explicit human publication promotes the candidate, with its evidence and
a rollback version retained. Saige provides comparison and regression gates;
Humagen owns the review and deployment decision. See
[Saige's comparison gate](https://github.com/urmzd/saige/blob/v0.41.0/eval/compare_gate.go).

This loop does not authorize unreviewed model changes, deployments or broader
access. Acceptance needs reproducible fixtures, permission regressions, native
run evidence, measured candidate behavior and an exercised rollback.
