# Corpora and the workspace graph: managed knowledge, search, tools and ownership

Status: proposed. Builds on RFC 0005 (federated retrieval and the catalog),
which it does not change.

## Goal

People create a corpus, a named container such as "HR policies", that holds
the sources one useful body of knowledge comes from: a Drive folder, a Notion
teamspace, a Snowflake schema, uploaded PDFs. A corpus decides how each source
stays fresh, which ways of searching are combined, which tools it gives agents,
what its entities and relations mean (its ontology), and how its quality is
checked. Agents are granted corpora, not raw sources, so they search better
and only where they should.

Above the corpora sits one workspace graph: people, teams, projects, systems,
topics and documents, linked across corpora where allowed. It knows who
wrote and owns each document, which documents are stale or contradicted, and
how systems depend on each other. A built-in maintenance agent keeps it
aligned with the sources, and people approve what matters.

The same machinery serves work and personal life: a team's HR policies, a
warehouse of metrics, and one person's private sessions, notes and records,
each searched under its own access, so an agent can help a person with the
documents they are entitled to and nothing more.

## Context

- **Built.** `pkg/knowledge` keeps sources as named collections of passages,
  ranked by BM25, with access rules per reader. RFC 0005 adds connector
  capabilities (`live`, `collect`, `catalog`), the embedder slot, hybrid
  fusion and the catalog. saige's `rag` already has keyword and vector
  retrievers, fusion, a reranker, parent retrieval, HyDE, a knowledge graph
  with extraction, a graph retriever scoped by group, and search, lookup,
  update and delete tools.
- **Missing.** A container above sources; a search recipe per container;
  tools scoped to a container's contents; an ontology; freshness targets;
  retrieval evaluation; deduplication across sources; a graph that spans
  sources, with ownership and staleness; an agent that maintains it.

## Decision

### 1. A corpus and its members

| Field | Meaning |
|---|---|
| `name`, `description`, `tags` | Shown to people and to the router that picks a corpus |
| `owner`, `readers` | Who manages it, and who may be granted it (default deny) |
| `members` | Sources it draws on, each `indexed` or `referenced` (below) |
| `recipe` | How it is searched (section 2) |
| `ontology` | Types it adds to the workspace graph (section 4), optional |
| `graph` | `shared` or `isolated`, and whether the semantic layer is on (sections 4 and 5) |
| `freshness` | Staleness target per member (section 9) |
| `evals` | Its golden set and thresholds (section 10) |
| `sensitivity` | Its default label and ceiling (section 7) |

A member is a reference to a connection's resource, never a copy of the
connection. One source may sit in several corpora. Every source belongs to at
least one corpus, so knowledge has one concept for people: the corpus.

| Member kind | RFC 0005 capability | Sync does | Searched by |
|---|---|---|---|
| **Indexed** | `collect` | Fetch, parse, chunk, embed, and optionally extract graph facts | The corpus's own indexes |
| **Referenced** | `live` and `catalog` | Records a pointer: title, owner, schema, a change cursor. No content is copied | The source's own API, live, at query time, with the reader's credential |

Referenced is the default where the source searches well natively (a
warehouse, Notion). A sync of a referenced member is a reference point: it
keeps the pointer, metadata and cursor current so a live query knows where to
look, and it can run as often as wanted because it copies nothing.

### 2. Search: a recipe of strategies

A recipe names the strategies a query fans out to, how results merge, and
limits. Offline strategies run against the corpus's indexes; online
strategies run live against referenced members.

| Strategy | Mode | Notes |
|---|---|---|
| `lexical` | offline | BM25 over passages (today's behavior) |
| `vector` | offline | The embedder slot; needs vectors for indexed members |
| `hybrid` | offline | Reciprocal rank fusion of lexical and vector |
| `rerank` | offline | A cross-encoder or model reranker over the top candidates |
| `parent` | offline | Expands a passage hit to its section |
| `hyde` | offline | Embeds a hypothetical answer; opt-in, costs a model call |
| `graph` | offline | Documents, facts and paths from the workspace graph, limited to this corpus or its shared links (sections 4 and 5) |
| `live` | online | Each referenced member's native search or query, scoped to the member |
| `catalog` | online | Finds which table or folder holds an answer, then `live` reads it |

Results from every strategy merge into one ranked list, are deduplicated by
document (section 8), and cite their member and document. A strategy that
times out or fails drops out and the result says so; it never fails the whole
search. Defaults: `hybrid` plus `rerank` for indexed members, `live` for
referenced ones.

### 3. Tools a corpus defines

Granting an agent a corpus grants its tools. Names are prefixed with the
corpus name.

| Tool | Scope |
|---|---|
| `<corpus>_search` | The recipe. The default tool agents use |
| `<corpus>_lookup` | Fetch a cited document or passage by id |
| `<corpus>_graph` | Graph search: entities, relations, owners, paths, starting from this corpus |
| `graph_search` | Workspace-wide graph search across every corpus the agent is granted, for system-level questions |
| Source-native tools | Generated from referenced members and limited to their contents: `<corpus>_sql` reads only the corpus's tables, read-only; `<corpus>_notion_search` only its pages; `<corpus>_drive_read` only its folders |

Source-native tools reuse the connection's existing client or MCP server
(RFC 0005 connectors) with a scope filter applied before the call, never a
second client. Every tool checks the reader's own access to each hit; a corpus
grant never widens what a person can read.

### 4. One workspace graph

There is one knowledge graph per workspace, and it is a top-level entity.
Corpora are slices of it, not separate graphs. A corpus contributes nodes and
edges, and every node and edge records which corpora it came from. Shared
entities connect corpora naturally: the person who wrote the HR leave policy
is the same node as the person who owns the payroll schema.

**Built-in types.** Every workspace graph has a core ontology that corpora
extend and never replace:

| Entity | Comes from |
|---|---|
| Person | The identity directory and source authorship (author, editor, assignee) |
| Team | The identity directory and source groups |
| Project | Humagen projects and source workspaces (a repository, a teamspace) |
| System | Services, tables and schemas found in catalog metadata and code |
| Topic | Extracted from content; a subject a set of documents is about |
| Document | Every indexed or referenced file, page, table or thread |

| Relation | Meaning |
|---|---|
| `authored`, `edited` | A person wrote or changed a document, with when |
| `owns` | A person or team is responsible for a document, topic or system |
| `about`, `mentions` | A document covers a topic, or names an entity |
| `supersedes`, `contradicts` | A newer document replaces or disagrees with an older one |
| `depends_on`, `part_of` | Structure between systems, projects and teams |

**Cross-corpus edges.** A corpus chooses `shared` (its nodes may link to
nodes in other corpora through shared entities) or `isolated` (its nodes link
only within it, for sensitive material such as investigations). Built-in
Person, Team and Project nodes are always shared so people resolve once. An
edge never reveals a node the reader cannot see: traversal filters every hop
by the reader's access, so a path through a hidden corpus is cut, not shown.

**Corpus ontology.** A corpus extends the core with its own types:

| Mode | How it is defined | Use |
|---|---|---|
| **Declared** | A JSON Schema document: entity types with properties and required fields, relation types with source and target types and cardinality, and passage metadata fields | Regulated or well-understood domains: HR, finance |
| **Inferred** | The maintenance agent (section 5) samples the corpus and proposes types, kept *proposed* until an owner accepts them | New or messy corpora |
| **Hybrid** | A declared core plus accepted inferred extensions | The usual case |

- **Versioned.** The core and each corpus ontology are versioned records. A
  run pins the versions it read, as runs pin agent definitions.
- **Validated.** Extracted facts are checked against the ontology. A fact
  that does not fit goes to a review queue rather than into the graph.
- **Re-extraction.** A version change re-extracts only what it affects,
  incrementally and in the background.
- **Metadata for vectors.** Passage metadata fields (document type,
  department, effective date, jurisdiction) become filters on `vector`,
  `hybrid` and `lexical`, so "the current parental leave policy in Canada"
  filters before it ranks.

### 5. Building and maintaining the graph

**Two layers, cheapest first.**

| Layer | Source | Cost | Gives |
|---|---|---|---|
| **Structural** | Source metadata: author, editors, timestamps, folder, links, schema, version history | No model calls. On for every corpus | Documents, people, ownership, staleness, file-level search |
| **Semantic** | Content extraction against the ontology | Model calls. Opt-in per corpus, estimated before enabling, metered | Topics, mentions, systems, contradictions, domain entities |

Referenced members contribute only to the structural layer; their content is
never copied.

**Ownership.** Who owns a document or topic is inferred from authorship,
edit share and recency, then shown as *suggested*. The person can confirm it,
hand it to someone else, or decline it; a confirmed owner wins over
inference. When an owner leaves the workspace, their documents become
unowned and their team lead is asked to reassign. Owners receive their
documents' staleness and contradiction alerts.

**Staleness.** Each document gets a staleness score, shown on its node and in
search results:

| Signal | Effect |
|---|---|
| Age against its type's half-life | A policy ages slower than a status update; half-lives are ontology properties |
| A newer document `supersedes` or `contradicts` it | Marked stale, linked to the newer one |
| Its owner left or declined it | Marked unowned |
| Its source changed after the last sync | Marked out of date until re-synced |

Search ranks stale documents lower and labels them, never hides them.

**The maintenance agent.** A built-in agent keeps the graph aligned with the
sources on a schedule and after each sync. It:

- merges duplicate entities (declared identity keys first, then name and
  properties with a confidence score);
- flags contradictions between documents and asks the owners which is current;
- proposes new types from recurring unfitted facts;
- suggests updates to owners of stale documents, with the evidence;
- drops facts whose only supporting document was deleted.

Every change it makes is a proposal with provenance (the documents and the
reasoning). Low-risk changes apply on their own and stay reversible: a merge
above the confidence threshold, a staleness mark, a fact retired with its
source. Anything else waits for a person: low-confidence merges, new types,
ownership changes, and anything touching a Person node. Proposals land as
alerts in the owner's session (RFC 0009), and the agent's runs are audited
like any other agent's.

**Provenance.** Every node and edge records the documents, members and
corpora it came from, so a graph answer cites sources like any other hit,
and deleting a source removes what only it supported.

**Health.** On the workspace graph page and each corpus page: nodes and
edges by type, orphan and conflicting facts, validation failures, unowned
and stale documents, open proposals, and the maintenance agent's last run.

**Retrieval at three levels.**

| Level | Answers | Tool |
|---|---|---|
| Passage | "What does the leave policy say about adoption?" | `<corpus>_search` |
| Document | "Which docs about payroll are current, and who owns them?" | `<corpus>_search` with document results, filtered by owner, topic, system, staleness |
| System | "What depends on the billing schema, and who should I ask?" | `<corpus>_graph`, or `graph_search` across every corpus the agent is granted |

**Scoped storage.** saige's group id is the workspace; corpus membership is a
property on each node and edge, and `graphretriever` filters by the corpora
the reader and agent are granted.

### 6. Files, references and records

A corpus holds three kinds of things, and each is handled differently.

| Kind | Examples | Stored | Extracted | Searched by |
|---|---|---|---|---|
| **File** | PDFs, Docs, Notion pages, slides, transcripts | Passages and vectors (indexed member) | Metadata fields, entities and facts per the ontology, tables inside documents as rows | The recipe |
| **Reference** | A dashboard, a URL, a Drive folder kept live, a ticket | Pointer, title, owner, change cursor | Structural graph only | `live`, at the source |
| **Record** | Snowflake and BigQuery tables, views, metrics | Schema, column descriptions, tags, row counts. **Never rows** | Table, column and metric nodes; joins as `depends_on`; owners from the warehouse | `<corpus>_sql`, read-only, live, with the reader's credential |

- **Rows stay in the warehouse.** Answers that need data run a query at the
  source under the reader's identity, so warehouse row access and masking
  policies apply unchanged. The result is cited as the query plus the table
  and the time it ran, not copied into the corpus.
- **Documents yield structure.** A policy PDF gives effective date,
  jurisdiction and the roles it covers; a contract gives parties, terms and
  renewal date; an offer letter gives a person, role and start date. These
  become metadata filters and graph facts, linked back to the passage.
- **Documents link to records.** When a document names a metric or table
  ("headcount by department"), the graph links them, so an agent can read the
  definition in the doc and the current number from the warehouse.

### 7. Sensitivity, private sessions and personal context

**Labels.** Every document, column and graph node carries a sensitivity
label: `public`, `internal`, `confidential`, or `restricted` (pay, health,
performance, investigations, personal records). Labels come from the source
first (Drive labels, Snowflake and BigQuery tags and policy tags), then from
a classifier that proposes labels for owner review. A corpus has a default
label and a ceiling.

**Sensitive agents.** An agent declares the highest label it may read. An HR
agent declares `restricted`; granting it a restricted corpus needs a
workspace admin and a stated purpose. A run that reads restricted content:

- is a **private session** by default, visible only to the person it serves
  and the people they add;
- takes the highest label it touched (taint), which follows its transcript,
  artifacts and memory;
- cannot post its content to a Slack channel or a GitHub thread, only a
  pointer back to the private session;
- never writes what it read into shared agent memory.

**Self access.** A person may always read records about themselves, even in
a restricted corpus, through an agent that serves them. The platform passes
the person's identity to the source (Snowflake row access policies, BigQuery
row-level security) or applies a `subject = reader` filter on indexed
documents. So "how much parental leave do I have left?" reads their own
balance and the policy that governs it, and nothing about anyone else.

**Access is visible.** Reads of restricted content are audited with the
agent, session and purpose. A person can see who and which agents read
records about them.

**Sessions in the graph.** Sessions are Document nodes too (RFC 0009 message
history): who took part, which documents they cited, which topics and
systems they touched. A private session is visible only to its
participants, so "what did I decide about the offer last week?" finds it for
them and for no one else. Shared sessions surface as related context for a
project or topic, under the same access rules.

**Personal corpora.** A person can make corpora from their own connections
(their mail, calendar, notes, their own Drive). A personal corpus is owned by
that person, is `isolated`, can be granted only to sessions they run, and is
never visible to admins' search. Personal and work context use the same
machinery, so one assistant can help with "prepare for my 1:1" by combining
the person's calendar, their private notes, the team's project docs and the
relevant metrics, each under its own access.

### 8. Consolidation

- **One document, many origins.** The same content reached through two
  members is stored once, keyed by a content hash, with every origin kept for
  citations and access checks.
- **Versions, not duplicates.** A re-sync of a changed document replaces its
  passages and graph facts and keeps the prior version for the audit window.
- **Graph merges** are the maintenance agent's job (section 5).

### 9. Freshness

| Field | Meaning |
|---|---|
| `mode` | `schedule`, `push` (the source's webhook), or `on_demand` |
| `target` | How stale a member may get, for example 24 h |
| `cursor` | The source's change token or ETag, so a sync reprocesses only changes |

Freshness is about syncs: is the copy current with the source? Staleness
(section 5) is about content: is the document itself still true?

Each member shows Fresh, Stale (since when), Syncing or Failed; the corpus
rolls them up. A referenced member is Live, with the last successful probe. A
failed sync or a missed target raises an alert in the corpus owner's session
(RFC 0009 alerts), with Retry.

### 10. Validation

| | Offline | Online |
|---|---|---|
| When | On a schedule, and before a recipe, member or ontology change is saved | As each search runs |
| Measures | A golden set per corpus (questions with expected documents or facts): recall at k, precision, citation accuracy, graph fact accuracy | Empty results, low top scores, strategy timeouts, thumbs down on answers that cited the corpus, citations never opened |
| Result | A score per version on the change card; a regression blocks the change without an override | Corpus health; an alert when it degrades |

Golden cases come from real use: a thumbs down with a reason on an answer that
cited the corpus becomes a candidate case. Checks are deterministic unless a
case says it is judged by a model, which is labelled. This reuses the agent
eval machinery (`pkg/evals`).

### 11. Console

Agents › Knowledge lists corpora as cards: name, members as tags, document
count, freshness, a Shared or Isolated pill, a Semantic pill when that layer
is on, and the agents granted it. ⋯
holds Edit, Sync now and Delete. New corpus asks for a name, members (from
Connections › Data sources, or uploads), and which agents may use it; recipe,
ontology and evals have sensible defaults and live on the corpus page.

The corpus page has tabs: Documents (browse, remove, re-sync), Members
(freshness per member), Ontology (types, proposed changes to accept, JSON
Schema view), Graph (this corpus's slice: health and an explorer), Evals
(golden set, scores), and Access (readers and granted agents), with a Try a
search box that runs the same tool an agent would. Documents show owner and
staleness as pills, with Confirm owner and Hand off in ⋯.

Agents › Knowledge also has a Graph view of the whole workspace graph:
health, open proposals from the maintenance agent, and an explorer that
starts from a person, topic or system. Each person's profile has "What the
graph says about you": documents attributed to them, topics they own, and
inferred expertise, each with Correct and Remove.

## Where it lands

| Piece | Repository |
|---|---|
| `Corpus`, `Member`, `Recipe`, `Ontology` records on `pkg/state` | humagen `pkg/knowledge` |
| Core ontology, structural layer, ownership and staleness scoring | humagen `pkg/knowledge` |
| Labels, taint, private sessions, self-access filter, access log | humagen `internal/platform` and `pkg/knowledge`, labels from RFC 0005 connectors |
| Warehouse records: schema sync, `<corpus>_sql` under the reader's identity | humagen connectors (RFC 0005), Snowflake and BigQuery first |
| Maintenance agent and its proposals | humagen built-in agent, proposals as RFC 0009 alerts |
| Sync scheduler, cursors, freshness and alerts | humagen `internal/platform` |
| Recipe executor and merge | humagen `pkg/knowledge`, composing saige `rag` retrievers, fusion and reranker |
| Federated retriever interface for `live` strategies | saige `rag` (new), connectors from RFC 0005 |
| Semantic extraction, entity resolution, provenance, corpus filter on traversal | saige `rag/knowledge/graph` (group is the workspace; corpus membership is a new node and edge property) |
| Corpus tool builder and agent grant `knowledge.corpora` | humagen `internal/platform`, spec field in `pkg/spec` |
| Retrieval evals | humagen `pkg/evals` |
| API `/api/corpora` and the console pages | humagen `api/openapi.yaml`, `apps/web` |

## Order

1. Corpus records, members (indexed only), `lexical` plus `hybrid`, the
   `<corpus>_search` and `_lookup` tools, the agent grant, the Knowledge cards.
2. Referenced members with `live` and `catalog` strategies and source-native
   tools (Snowflake and BigQuery first, then Notion and Drive).
3. Freshness: modes, cursors, targets, alerts.
4. Sensitivity: labels from sources, agent ceilings, private sessions and
   taint, self access, the access log. This lands before any restricted
   corpus (HR) is offered.
5. Offline and online retrieval evals and the change card gate.
6. Structural graph: core ontology, Document, Person and Team nodes from
   metadata, ownership suggestions and confirmation, staleness scores,
   document-level search, `<corpus>_graph`. No model calls.
7. Maintenance agent with proposals and auto-apply rules; health; the
   "what the graph says about you" page.
8. Sessions as graph nodes, personal corpora.
9. Corpus ontologies (declared JSON Schema, then inferred, then hybrid),
   metadata filters on search, and the semantic layer: topics, mentions,
   contradictions, `graph_search` across corpora.

Each step ships on its own and is verified on platform.withhumagen.com.

## Risks

- **Cost.** Graph extraction and HyDE spend model tokens. Opt-in per corpus,
  estimated before enabling, metered on the ledger.
- **Leaks through metadata.** Table names and ontology properties can be
  sensitive. Catalog visibility rules from RFC 0005 apply to referenced
  members and graph nodes built from metadata.
- **Restricted data in the wrong place.** Taint follows every output of a
  run that read restricted content, and only pointers leave private
  sessions. Warehouse rows are never copied.
- **People data.** Inferred ownership and expertise describe employees.
  Person changes always need approval, people see and correct what the graph
  says about them, and the workspace can turn expertise inference off.
- **Agent overreach.** The maintenance agent only auto-applies reversible,
  low-risk changes; everything else is a proposal, and its runs are audited.
- **Leaks across corpora.** Traversal filters every hop by the reader's
  access, and isolated corpora never link out.
- **Ontology drift.** Inferred types can sprawl. They stay proposed until an
  owner accepts them, and validation keeps unfit facts out of the graph.
- **Live latency.** Online strategies run under a per-strategy timeout and
  drop out rather than slowing every search.
- **Stale answers.** Freshness targets and alerts make staleness visible
  instead of silent.
