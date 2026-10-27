# Humagen task queue

Updated 2026-10-26. This queue separates the current delivery from follow-on
platform work. Status describes the working branch; it is not a claim that the
deployed site has been updated. Final verification and merge remain separate.

## Current delivery, in execution order

| Order | Task | State | Done when |
| --- | --- | --- | --- |
| 1 | Consolidate Needs You and notifications | Implemented; 30 routes, 43 focused DOM checks and 20 browser states pass | One Notifications sidebar link opens a single requests-and-updates feed. One Filter panel combines status and type. Alert Rules is the only companion tab. The badge keeps one destination; native links and permissions still work. |
| 2 | Finish the navigation and layout cleanup | Implemented; final browser/CI verification pending | New sits above Search Work; workspace and operations have appropriate separators; related tools use hubs; pin writes preserve later operations, rollback and focus. |
| 3 | Finish compact projects, suggestions and announcements | Implemented, visual review pending | Useful project metadata is visible, descriptions expand, suggestions stay put while typing, and the announcement has an accessible eye-with-slash hide action. |
| 4 | Verify Skills and Memory as managed file browsers | Implemented, final regression review pending | Folder import, editing, immutable versions, downloads, scoped memory actions and private chat links work through the existing APIs. Dirty drafts survive navigation. |
| 5 | Verify typography, settings, Audit and transitions | Automated checks pass; final browser/CI verification pending | Plex fonts and red/yellow/green presets load; shared settings rows persist values; Audit calendar submits valid local-to-ISO ranges; drawers preserve focus and reduced motion. |
| 6 | Finish source creation and access safeguards | Automated checks pass; final browser/CI verification pending | Providers/formats are accurate; cadence is saved with the source; selected provider reads and retained projections preserve upstream authority without cached fallback. |
| 7 | Reconcile design, API and architecture documentation | Documentation reconciled; final evidence pending | Generated clients match the contract; docs reflect the Notifications consolidation and source access behavior; implemented features are distinguished from proposed integrations. |
| 8 | Run integrated verification | Automated checks pass; browser matrix and remote CI pending | Complete native narrow/desktop, light/dark, focus and reduced-motion journeys; pass remote CI for the final revision. |
| 9 | Publish and merge this delivery | Queued | Commit the reviewed changes, create the PR on the consolidated base, resolve CI failures and merge the exact passing revision. Refresh the local main checkout. |

### Execution owners

| Work | Owner |
| --- | --- |
| Retained source evidence and metadata authorization | `source_access_fixes` |
| Selected provider queries, variants and frozen snapshots | `source_query_fixes` |
| Notifications consolidation and frontend regression assertions | `notification_consolidation` |
| Native browser journeys and screenshot review | `browser_verification` |
| Design, architecture and task-queue reconciliation | `design_docs_reconcile` |
| Integration, final checks, PR and merge | Primary agent |

### Source-access work before merge

- [x] Withhold retained external citation excerpts without current file
  authorization, including unknown proofs and deleted-source fallbacks. Retain
  authority provenance when new evidence is recorded. Document the separate
  limitation of text already retained in historical conversations.
- [x] Make fresh provider queries respect selected resource IDs through
  `FetchSelected`. Explicitly handle saved variants and frozen snapshots rather
  than silently searching a broader or newer collection.
- [x] Prevent document-exclusion and saved-variant responses from returning
  retained external file metadata without authorization.
- [x] Add regression coverage for revocation, restart, selected files and
  collection failure, then review every affected public read path.

### Consolidation verification

- [x] Keep one Notifications destination regardless of the actionable badge count.
- [x] Assert one Notifications feed with permission-aware Alert Rules, including
  machine identities that can use the feed without human delivery polling.
- [ ] Check native deep links, modified-click navigation, badge updates and
  mobile/sidebar separators in the final browser build.

## Follow-on platform queue

These items require implementation beyond the current UI delivery. Research or
a provider tile does not mean the capability is available.

| Order | Task | Next concrete step |
| --- | --- | --- |
| 10 | Native corpora, runs, files and retrieval | Implement the proposed Corpus/CorpusRun/CorpusFile/FileVersion/RunFile/chunk contracts, bounded pull planner, independent ACL invalidation and atomic last-good manifest. Keep Source APIs compatible and compose Saige synchronization/retrieval. |
| 11 | Slack live search and permitted channel collection | Keep user-authorized MCP search separate from approved internal channel ingestion. Specify scopes, policy eligibility, rate limits, edits, deletions, retention and membership revocation before adding an index. |
| 12 | Vector and knowledge-graph augmentation | Compose the existing Saige index and graph interfaces behind the native retriever. Require readable source attribution for every passage and fact; never treat a graph namespace as an access grant. |
| 13 | Website retrieval and a browser workspace | Start with explicit URL retrieval and safe network boundaries. Scope the authenticated browser service, phone interaction and private Tailscale access separately from the local development preview. |
| 14 | Conversation-first generation and protocol adapters | Define a bounded missing-capability response and text/file fallback. Add reviewed renderer and generator registrations, then explicit A2UI, A2A and AG-UI adapters through existing run and call boundaries. |
| 15 | Human-reviewed improvement loop | Turn reviewed production cases into eval fixtures, compare bounded repair drafts, and require human publication with rollback. Keep AgentSpec and the existing native runtime as the execution boundary. |

## Evidence and limits

- All 15 workspace typechecks and nine workspace test tasks pass, including
  637 console, 18 client, 193 Slack, 58 adapter, eight screen-helper and 46 core
  tests. These results supersede the earlier stale-assertion failures.
- The full Go suite passes with `-race`; Go lint reports zero issues. Biome,
  API lint and production/Storybook builds pass. API lint retains five existing
  ignored findings. No live external-provider trial is claimed.
- Focused source access, selected-file queries, settings/calendar and pin
  concurrency/recovery regressions pass. PostgreSQL live cases were not enabled
  for the source worker's local run.
- Current 320px light calendar/preferences/source checks and completed dark
  phone states pass. The final browser matrix and remote CI are still running;
  deployment and merge remain unverified.
- The requested Cerebras article returned HTTP 500 during research. Its body has
  not been verified and is not being used as implementation evidence.
- The proposed corpus schema, run/file membership, update planning, invalidation
  and cache rules are recorded in [the next-backend design](design/conversation-workspace.md#proposed-corpus-model).
  Items 10 through 15 remain planned and are not part of the current UI backend.
- See [managed library behavior](managed-library.md), [design requirements](../design.md)
  and [architecture status](architecture.md#status) for the implementation contract.
