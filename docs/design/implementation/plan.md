# Implementation plan: complete Humagen workspace and control plane

**Baseline:** main `edc4f00`, October 25, 2026. Both checked-in design sets are approved by the user.

## Summary

Implement the real console and its backing system, preserving one runtime path and default-deny read confinement. Prototype records are design evidence only. This plan is not a claim that the listed gaps are implemented. Each row completes only with native behavior and recorded tests.

## Technical context

Go module composition of fiber, saige, playscape and anchor; React/TypeScript workspace and shared web/Slack presentation. Existing state.Store-backed memory/Postgres stores, OpenAPI contract and generated TypeScript client remain authoritative. Preserve the framework, Page layout, AgentMark identity, native credential references and existing budget accounting.

## Constitution check

Project constitution is `AGENTS.md` (there is no separate constitution.md in this repository). Every new record is tenant-scoped and bounded; each route checks one permission then p.Sees; narrower scope cannot grant access; records replay on memory and Postgres; secrets never enter response DTOs or audit snapshots. Historical accounting is immutable even when current audience placement changes.

## Applied guidelines

Use the existing Go/React stack and style-brand shared web reference, repo copy.ts and primitives. No framework migration or duplicate execution path is planned. New wire fields change api/openapi.yaml before make client. No generated client hand edits.

## Stack and ownership

| Layer | Branch | Owner |
| --- | --- | --- |
| Badge containment | codex/agent-badge-layout | root |
| Plan and audit foundation | codex/artifact-ui-completion | root, audit core from library_personal |
| Company and placement | codex/company-placement | company_placement |
| Agent operations | codex/agent-operations | agent_operations |
| Sources and integrations | codex/source-lifecycle | source_lifecycle |
| Library and personal layer | codex/library-personal | library_personal |
| Conversation interactions | codex/conversation-actions | conversation_actions |
| Outcomes and integrated workspace | codex/workspace-outcomes | workspace_outcomes |

Branches are integrated through no-ff merges, never rebase/reset/force-push. Each PR targets its immediate parent; root publishes after integrating the lower layer. Shared API/navigation changes are reconciled and verified before promotion.

## Implementation steps

### Phase 1: Foundation and review

- [ ] T001 [Plan:1.1] Complete requirement traceability and native verification. Implement and verify in `docs/design/implementation/plan.md`. [REQ-001]; owner root. Source: `docs/design/humagen-control-plane/gap-analysis.md`.
- [x] T002 [Plan:1.2] Badge outlines contain labels at narrow widths and enlarged text. Implement and verify in `apps/web/src/ui.tsx`, `apps/web/src/Agents.tsx`, `scripts/screens/run.ts`. [REQ-002]; owner root. Source: `user supplied badge screenshot`.
- [x] T003 [Plan:1.3] Copy and Reply share one consistent accessible action row. Implement and verify in `packages/web/src/AgentMessage.tsx`, `apps/web/src/Room.tsx`. [REQ-003]; owner workspace_outcomes. Source: `user supplied message-actions screenshot`.
- [ ] T004 [Plan:1.4] Prototype controls become real authenticated state transitions. Implement and verify in `api/openapi.yaml`, `internal/httpapi/server.go`. [REQ-004]; owner all. Source: `docs/design/humagen-workspace/README.md#try-the-interaction-loop`.
- [x] T005 [Plan:1.5] Preserve agent identity, stable colors and compatibility history. Implement and verify in `internal/platform/builtin.go`, `pkg/agentcolors/store.go`. [REQ-005]; owner root. Source: `AGENTS.md; design.md#built-in-handles-and-slack-conversation-navigation`.
- [x] T006 [Plan:1.6] Durable bounded safe audit attempts underpin domain mutations. Implement and verify in `pkg/audit`, `internal/platform/audit.go`. [REQ-006]; owner library_personal. Source: `docs/design/humagen-control-plane/spec.md#control-plane-operations`.

### Phase 2: Company and authoritative placement

- [ ] T007 [Plan:2.1] Company is the root and every agent has an explicit home. Implement and verify in `pkg/projects/homes.go`, `internal/platform/agents.go`. [REQ-007]; owner company_placement. Source: `docs/company-workspace.md#one-company-project`.
- [ ] T008 [Plan:2.2] Idempotent durable personal project provisioning for human members. Implement and verify in `pkg/projects`, `internal/platform/personal_projects.go`. [REQ-008]; owner company_placement. Source: `docs/company-workspace.md#a-personal-project-for-each-person`.
- [ ] T009 [Plan:2.3] Exact-owner personal read/write confinement, governance recorded. Implement and verify in `internal/platform/sees.go`, `pkg/projects/roles.go`. [REQ-009]; owner company_placement. Source: `docs/company-workspace.md#a-personal-project-for-each-person`.
- [ ] T010 [Plan:2.4] Versioned current placement separate from immutable accounting. Implement and verify in `pkg/threads`, `pkg/runs`, `internal/platform/threads.go`. [REQ-010]; owner company_placement. Source: `docs/company-workspace.md#moving-work-into-a-public-project`.
- [ ] T011 [Plan:2.5] Publish completed private work with audience preview and race safety. Implement and verify in `apps/web/src/ConversationShell.tsx`, `internal/platform/threads.go`. [REQ-011]; owner company_placement. Source: `docs/company-workspace.md#moving-work-into-a-public-project`.
- [ ] T012 [Plan:2.6] One retained announcement banner above the greeting. Implement and verify in `internal/platform/announce.go`, `apps/web/src/Home.tsx`. [REQ-012]; owner company_placement. Source: `docs/company-workspace.md#an-announcement-above-the-greeting`.
- [ ] T013 [Plan:2.7] Permission-filtered Find Help and safe retirement of new SME routing. Implement and verify in `pkg/pick`, `internal/platform/threads.go`, `apps/web/src/Chat.tsx`. [REQ-013]; owner company_placement. Source: `docs/company-workspace.md#find-the-project-and-agent-for-the-work`.

### Phase 3: Delegation and agent operations

- [ ] T014 [Plan:3.1] Durable assignment goal, owner, completion criteria and outcomes. Implement and verify in `pkg/operations`, `internal/platform/assignments.go`, `apps/web/src/Assignments.tsx`. [REQ-014]; owner agent_operations. Source: `docs/conversation-workspace.md#follow-through`.
- [ ] T015 [Plan:3.2] Standing work and triggers reuse scheduler, Invoke and pkg/call. Implement and verify in `pkg/schedules`, `pkg/call`, `internal/platform/assignments.go`. [REQ-015]; owner agent_operations. Source: `docs/design/humagen-control-plane/spec.md#agents-and-how-they-talk-to-each-other`.
- [ ] T016 [Plan:3.3] Inherited tighten-only Allow, Ask, Never action policy. Implement and verify in `pkg/operations`, `internal/platform/action_policy.go`. [REQ-016]; owner agent_operations. Source: `docs/design/humagen-control-plane/spec.md#agents-and-how-they-talk-to-each-other`.
- [ ] T017 [Plan:3.4] Named managers, escalation, reminders, budgets and stop rules. Implement and verify in `pkg/operations`, `internal/platform/hooks.go`. [REQ-017]; owner agent_operations. Source: `docs/design/humagen-control-plane/spec.md#agents-and-how-they-talk-to-each-other`.
- [ ] T018 [Plan:3.5] Typed handoff contracts, deadlines, states, cycles and hop limits. Implement and verify in `pkg/operations`, `pkg/call`, `internal/platform/handoffs.go`. [REQ-018]; owner agent_operations. Source: `docs/design/humagen-control-plane/spec.md#agents-and-how-they-talk-to-each-other`.
- [ ] T019 [Plan:3.6] Pause agent, pause all and enforce control at every admission path. Implement and verify in `pkg/operations`, `internal/platform/agents.go`, `apps/web/src/AgentControls.tsx`. [REQ-019]; owner agent_operations. Source: `docs/design/humagen-control-plane/gap-analysis.md#not-built`.
- [x] T020 [Plan:3.7] Real console Stop control follows terminal ledger state. Implement and verify in `apps/web/src/Room.tsx`, `packages/web/src/AgentMessage.tsx`. [REQ-020]; owner workspace_outcomes. Source: `docs/design/humagen-control-plane/gap-analysis.md#not-built`.
- [ ] T021 [Plan:3.8] Bounded Agent Network volumes, waiting time and blocked work. Implement and verify in `internal/platform/agent_network.go`, `apps/web/src/AgentNetwork.tsx`. [REQ-021]; owner agent_operations. Source: `docs/design/humagen-control-plane/spec.md#agents-and-how-they-talk-to-each-other`.
- [ ] T022 [Plan:3.9] Needs you deduplicates one cause and records its owner. Implement and verify in `pkg/inbox`, `internal/platform/inbox.go`. [REQ-022]; owner agent_operations. Source: `docs/design/humagen-control-plane/spec.md#control-plane-operations`.
- [ ] T023 [Plan:3.10] Roster exposes working, waiting, watching, paused, failed and last run. Implement and verify in `apps/web/src/Agents.tsx`, `apps/web/src/AgentControls.tsx`. [REQ-023]; owner agent_operations. Source: `docs/design/humagen-control-plane/spec.md#agents-and-how-they-talk-to-each-other`.

### Phase 4: Sources and integrations

- [ ] T024 [Plan:4.1] Source origin, owner, access mode and freshness are real and visible. Implement and verify in `pkg/knowledge`, `apps/web/src/Context.tsx`. [REQ-024]; owner source_lifecycle. Source: `docs/design/humagen-control-plane/spec.md#context-provenance-and-the-sync-lifecycle`.
- [ ] T025 [Plan:4.2] Durable source lifecycle, bounded attempts and last good copy. Implement and verify in `pkg/knowledge`, `internal/platform/source_lifecycle.go`. [REQ-025]; owner source_lifecycle. Source: `docs/design/humagen-control-plane/spec.md#context-provenance-and-the-sync-lifecycle`.
- [ ] T026 [Plan:4.3] Stale failures raise one cause and pause dependent work by stop rule. Implement and verify in `internal/platform/source_lifecycle.go`, `pkg/inbox`, `pkg/operations`. [REQ-026]; owner source_lifecycle. Source: `docs/design/humagen-control-plane/spec.md#context-provenance-and-the-sync-lifecycle`.
- [ ] T027 [Plan:4.4] Indexed, live and hybrid honor the reader on every request. Implement and verify in `pkg/knowledge`, `internal/platform/knowledge.go`, `pkg/credentials`. [REQ-027]; owner source_lifecycle. Source: `docs/design/humagen-control-plane/spec.md#context-access-modes`.
- [ ] T028 [Plan:4.5] Source dependency graph answers Used by without hidden names/counts. Implement and verify in `pkg/knowledge`, `internal/platform/dependencies.go`. [REQ-028]; owner source_lifecycle. Source: `docs/design/humagen-control-plane/spec.md#system-foundations-and-phasing`.
- [ ] T029 [Plan:4.6] GitHub resources link to projects with explicit people mapping. Implement and verify in `pkg/clients`, `internal/platform/surfaces.go`, `apps/web/src/Clients.tsx`. [REQ-029]; owner source_lifecycle. Source: `docs/design/humagen-control-plane/spec.md#integrations`.
- [ ] T030 [Plan:4.7] Slack binding directions and guest approval boundaries. Implement and verify in `pkg/clients`, `pkg/projects/channels.go`, `apps/web/src/Clients.tsx`. [REQ-030]; owner source_lifecycle. Source: `docs/design/humagen-control-plane/spec.md#integrations`.
- [ ] T031 [Plan:4.8] Drive and Notion provider paths use real API boundaries and references. Implement and verify in `pkg/knowledge`, `internal/platform/source_providers.go`. [REQ-031]; owner source_lifecycle. Source: `docs/design/humagen-control-plane/spec.md#integrations`.
- [ ] T032 [Plan:4.9] Connect Resources Links Access Events Actions report real state. Implement and verify in `apps/web/src/Clients.tsx`, `apps/web/src/ClientConnect.tsx`. [REQ-032]; owner source_lifecycle. Source: `docs/design/humagen-control-plane/spec.md#integrations`.
- [ ] T033 [Plan:4.10] Suggest, auto-link and off rules are bounded, previewable and auditable. Implement and verify in `pkg/clients`, `internal/platform/link_rules.go`. [REQ-033]; owner source_lifecycle. Source: `docs/design/humagen-control-plane/spec.md#integrations`.

### Phase 5: Library, personal settings and audit

- [ ] T034 [Plan:5.1] Versioned scoped skill drafts, tests, publish and promotion. Implement and verify in `pkg/skills`, `internal/platform/skills.go`, `apps/web/src/Skills.tsx`. [REQ-034]; owner library_personal. Source: `docs/design/humagen-control-plane/spec.md#skills-and-memory`.
- [ ] T035 [Plan:5.2] Skill and new-agent dry runs force actions to Ask. Implement and verify in `pkg/skills`, `internal/platform/action_policy.go`. [REQ-035]; owner library_personal. Source: `docs/design/humagen-control-plane/spec.md#core-flows`.
- [ ] T036 [Plan:5.3] Memory scope, kind, provenance, review, expiry, usage and audited forget. Implement and verify in `pkg/memory`, `apps/web/src/Memory.tsx`. [REQ-036]; owner library_personal. Source: `docs/design/humagen-control-plane/spec.md#skills-and-memory`.
- [ ] T037 [Plan:5.4] Personal memory stays out of projects without explicit permission. Implement and verify in `pkg/memory`, `internal/platform/knowledge.go`. [REQ-037]; owner library_personal. Source: `docs/design/humagen-control-plane/spec.md#skills-and-memory`.
- [ ] T038 [Plan:5.5] Personal profile and briefing style are persisted and used. Implement and verify in `pkg/prefs`, `apps/web/src/Settings.tsx`. [REQ-038]; owner library_personal. Source: `docs/design/humagen-control-plane/spec.md#user-level-configuration-you`.
- [ ] T039 [Plan:5.6] Notification kind, digest and quiet-hour settings drive dispatch. Implement and verify in `pkg/prefs`, `internal/platform/notifications.go`. [REQ-039]; owner library_personal. Source: `docs/design/humagen-control-plane/spec.md#user-level-configuration-you`.
- [ ] T040 [Plan:5.7] Personal agents expose ownership, pause and project promotion. Implement and verify in `pkg/prefs`, `internal/platform/agents.go`, `apps/web/src/Skills.tsx`. [REQ-040]; owner library_personal. Source: `docs/design/humagen-control-plane/spec.md#user-level-configuration-you`.
- [ ] T041 [Plan:5.8] Connected accounts expose actual scopes, use and disconnect. Implement and verify in `pkg/credentials`, `apps/web/src/Connections.tsx`. [REQ-041]; owner library_personal. Source: `docs/design/humagen-control-plane/spec.md#user-level-configuration-you`.
- [ ] T042 [Plan:5.9] Cross-domain audit view is bounded, confined, durable and retained. Implement and verify in `pkg/audit`, `internal/httpapi/audit.go`, `apps/web/src/Audit.tsx`. [REQ-042]; owner library_personal. Source: `docs/design/humagen-control-plane/spec.md#control-plane-operations`.

### Phase 6: Conversation and project interaction

- [ ] T043 [Plan:6.1] Saved fork from message preserves bounded read-only history and lineage. Implement and verify in `pkg/threads`, `internal/platform/forks.go`, `apps/web/src/ForkConversation.tsx`. [REQ-043]; owner conversation_actions. Source: `docs/design/humagen-control-plane/spec.md#forking`.
- [ ] T044 [Plan:6.2] Bring back result posts a reviewed result and never executes pending actions. Implement and verify in `internal/platform/forks.go`, `apps/web/src/ForkConversation.tsx`. [REQ-044]; owner conversation_actions. Source: `docs/design/humagen-control-plane/spec.md#forking`.
- [ ] T045 [Plan:6.3] Project fork copies chosen structure/config safely, rules disabled. Implement and verify in `pkg/projects`, `internal/platform/project_forks.go`. [REQ-045]; owner conversation_actions. Source: `docs/design/humagen-control-plane/spec.md#forking`.
- [ ] T046 [Plan:6.4] People shows actual audience, roles, removal and safe invitations. Implement and verify in `apps/web/src/ConversationShell.tsx`, `internal/platform/threads.go`. [REQ-046]; owner conversation_actions. Source: `docs/design/humagen-control-plane/gap-analysis.md#partial`.
- [ ] T047 [Plan:6.5] Quick switcher returns bounded readable destinations only. Implement and verify in `internal/httpapi/discovery.go`, `apps/web/src/QuickSwitcher.tsx`. [REQ-047]; owner conversation_actions. Source: `docs/design/humagen-control-plane/spec.md#hierarchy-initiatives-projects-and-sub-projects`.
- [ ] T048 [Plan:6.6] Initiatives roll up status, risks, decisions, usage and needs safely. Implement and verify in `pkg/projects`, `internal/platform/projects.go`, `apps/web/src/Projects.tsx`. [REQ-048]; owner conversation_actions. Source: `docs/design/humagen-control-plane/spec.md#hierarchy-initiatives-projects-and-sub-projects`.
- [ ] T049 [Plan:6.7] Create project blank, template, fork or under initiative. Implement and verify in `apps/web/src/Projects.tsx`, `internal/platform/project_forks.go`. [REQ-049]; owner conversation_actions. Source: `docs/design/humagen-control-plane/spec.md#hierarchy-initiatives-projects-and-sub-projects`.

### Phase 7: Outcomes, native deliverables and full workspace presentation

- [ ] T050 [Plan:7.1] Outcomes lead with recommendation, decisions, deliverables and next action. Implement and verify in `packages/core/src/types.ts`, `packages/components/src/components.tsx`, `packages/web/src/AgentMessage.tsx`. [REQ-050]; owner workspace_outcomes. Source: `docs/design/humagen-control-plane/spec.md#summary-and-principles`.
- [ ] T051 [Plan:7.2] Redirect records revised intent separately from approval. Implement and verify in `internal/platform/outcomes.go`, `packages/components/src/components.tsx`. [REQ-051]; owner workspace_outcomes. Source: `docs/design/humagen-workspace/index.html`.
- [ ] T052 [Plan:7.3] Completed work collapses; blocked work exposes result/input/output on demand. Implement and verify in `packages/web/src/AgentMessage.tsx`, `packages/components/src/components.tsx`. [REQ-052]; owner workspace_outcomes. Source: `docs/conversation-workspace.md#five-interactions`.
- [ ] T053 [Plan:7.4] Versioned native previews keep real bytes, provenance and parser bounds. Implement and verify in `apps/web/src/Deliverables.tsx`, `apps/web/src/artifactPreview.ts`. [REQ-053]; owner workspace_outcomes. Source: `docs/conversation-workspace.md#versioned-deliverables`.
- [ ] T054 [Plan:7.5] Native deliverables are real editable files, not prototype exports. Implement and verify in `pkg/artifacts`, `internal/platform/sandbox.go`, `apps/web/src/Deliverables.tsx`. [REQ-054]; owner workspace_outcomes. Source: `docs/design/humagen-workspace/README.md#try-the-interaction-loop`.
- [ ] T055 [Plan:7.6] Revision requests retain immutable version/slide context and unsent draft. Implement and verify in `apps/web/src/Deliverables.tsx`, `apps/web/src/Room.tsx`. [REQ-055]; owner workspace_outcomes. Source: `docs/conversation-workspace.md#versioned-deliverables`.
- [ ] T056 [Plan:7.7] Accepted outputs promote to knowledge/reviewed memory with audience checks. Implement and verify in `internal/platform/artifacts.go`, `pkg/knowledge`, `pkg/memory`. [REQ-056]; owner workspace_outcomes. Source: `docs/conversation-workspace.md#follow-through`.
- [x] T057 [Plan:7.8] Agent and human timestamps use recorded instants, hover/focus/tap and timezone. Implement and verify in `packages/web/src/AgentMessage.tsx`, `packages/adapter/src/fold.ts`, `apps/web/src/Room.tsx`. [REQ-057]; owner workspace_outcomes. Source: `design.md#des-004-date-and-time-on-hover-or-focus`.
- [ ] T058 [Plan:7.9] Composer is compact, mobile safe, textual and permission aware. Implement and verify in `apps/web/src/Chat.tsx`, `apps/web/src/Room.tsx`. [REQ-058]; owner workspace_outcomes. Source: `docs/design/humagen-control-plane/gap-analysis.md#partial`.
- [ ] T059 [Plan:7.10] Projects Library Operations You navigation exposes working routed pages. Implement and verify in `apps/web/src/Sidebar.tsx`, `apps/web/src/App.tsx`, `apps/web/src/route.ts`. [REQ-059]; owner workspace_outcomes. Source: `docs/design/humagen-control-plane/spec.md#mental-model-and-information-architecture`.
- [ ] T060 [Plan:7.11] Phone/tablet/desktop, dark mode, keyboard and touch complete all journeys. Implement and verify in `scripts/screens/run.ts`, `apps/web/src/styles.css`. [REQ-060]; owner workspace_outcomes. Source: `docs/design/humagen-workspace/README.md`.
- [ ] T061 [Plan:7.12] Saved-turn citations retain provenance and truthful indexed/live labels. Implement and verify in `pkg/runs`, `packages/adapter/src/run.ts`, `packages/web/src/AgentMessage.tsx`. [REQ-061]; owner workspace_outcomes. Source: `docs/design/humagen-control-plane/gap-analysis.md#partial`.
- [ ] T062 [Plan:7.13] Presentation, notes, versions and downloads preserve immutable selection. Implement and verify in `apps/web/src/Deliverables.tsx`, `packages/web/src/ArtifactPreview.tsx`. [REQ-062]; owner workspace_outcomes. Source: `docs/design/humagen-workspace/README.md#try-the-interaction-loop`.
- [ ] T063 [Plan:7.14] Identity, scope chips and punctuation follow repo accessibility/copy rules. Implement and verify in `packages/components/src/copy.ts`, `packages/core/src/types.ts`, `apps/web/src/ui.tsx`. [REQ-063]; owner workspace_outcomes. Source: `AGENTS.md; docs/design/humagen-control-plane/spec.md#objects-scopes-and-identity`.
- [ ] T064 [Plan:7.15] Every shipped capability has automated and honest live validation evidence. Implement and verify in `docs/design/implementation/plan.md`, `docs/architecture.md`, `scripts/screens/run.ts`. [REQ-064]; owner root. Source: `design.md#implementation-and-review-evidence`.

## Project structure

Existing packages retain their ownership. New domain records belong in their named pkg packages, composition in internal/platform, HTTP in internal/httpapi, shared presentation in packages/core/components/web, and console hosts in apps/web. Prototype HTML remains a design reference.

## Testing strategy

- Mixed SPA, HTTP API and Slack/GitHub surfaces. Critical journeys: private work to reviewed public outcome; source change to retained last-good context and one decision; durable assignment through handoff/pause/restart; fork and bring-back without action replay; native deliverable revision/presentation; human and agent message actions.
- Regressions fail first. Memory/Postgres replay, cross-tenant/private confinement, bounded aggregates, admission races and denied actions are required for backend work.
- Browser validation uses the real seeded server and existing scripts/screens at phone and desktop, with explicit dark/enlarged-text and relevant 320/768/1440 scenarios. Test fixtures are labelled, and are not live external-delivery evidence.
- Shared local fixtures are isolated loopback Postgres/S3. Each test uses its own schema/prefix. Root owns fixture cleanup and the user preview on 8080.
- Local checks: gofmt, golangci-lint v2, Go race suite with Postgres and no test skips, frozen install, Biome, typechecks, JS tests, real-server Slack tests, console build, spec lint, unchanged client generation, client checks, screens and Slack validation.
- External providers require configured credentials and explicit installation scopes. Missing authorization is shown honestly. Do not create external registrations or send external messages merely to make a prototype control appear successful.

## Requirement mapping

| Requirement | Source requirement | Plan item | Owner | Implementation evidence |
| --- | --- | --- | --- | --- |
| REQ-001 | Complete requirement traceability and native verification | 1.1 | root | Pending native checks in `docs/design/implementation/plan.md` |
| REQ-002 | Badge outlines contain labels at narrow widths and enlarged text | 1.2 | root | Pending native checks in `apps/web/src/ui.tsx` |
| REQ-003 | Copy and Reply share one consistent accessible action row | 1.3 | workspace_outcomes | Pending native checks in `packages/web/src/AgentMessage.tsx` |
| REQ-004 | Prototype controls become real authenticated state transitions | 1.4 | all | Pending native checks in `api/openapi.yaml` |
| REQ-005 | Preserve agent identity, stable colors and compatibility history | 1.5 | root | Pending native checks in `internal/platform/builtin.go` |
| REQ-006 | Durable bounded safe audit attempts underpin domain mutations | 1.6 | library_personal | Pending native checks in `pkg/audit` |
| REQ-007 | Company is the root and every agent has an explicit home | 2.1 | company_placement | Pending native checks in `pkg/projects/homes.go` |
| REQ-008 | Idempotent durable personal project provisioning for human members | 2.2 | company_placement | Pending native checks in `pkg/projects` |
| REQ-009 | Exact-owner personal read/write confinement, governance recorded | 2.3 | company_placement | Pending native checks in `internal/platform/sees.go` |
| REQ-010 | Versioned current placement separate from immutable accounting | 2.4 | company_placement | Pending native checks in `pkg/threads` |
| REQ-011 | Publish completed private work with audience preview and race safety | 2.5 | company_placement | Pending native checks in `apps/web/src/ConversationShell.tsx` |
| REQ-012 | One retained announcement banner above the greeting | 2.6 | company_placement | Pending native checks in `internal/platform/announce.go` |
| REQ-013 | Permission-filtered Find Help and safe retirement of new SME routing | 2.7 | company_placement | Pending native checks in `pkg/pick` |
| REQ-014 | Durable assignment goal, owner, completion criteria and outcomes | 3.1 | agent_operations | Pending native checks in `pkg/operations` |
| REQ-015 | Standing work and triggers reuse scheduler, Invoke and pkg/call | 3.2 | agent_operations | Pending native checks in `pkg/schedules` |
| REQ-016 | Inherited tighten-only Allow, Ask, Never action policy | 3.3 | agent_operations | Pending native checks in `pkg/operations` |
| REQ-017 | Named managers, escalation, reminders, budgets and stop rules | 3.4 | agent_operations | Pending native checks in `pkg/operations` |
| REQ-018 | Typed handoff contracts, deadlines, states, cycles and hop limits | 3.5 | agent_operations | Pending native checks in `pkg/operations` |
| REQ-019 | Pause agent, pause all and enforce control at every admission path | 3.6 | agent_operations | Pending native checks in `pkg/operations` |
| REQ-020 | Real console Stop control follows terminal ledger state | 3.7 | workspace_outcomes | Pending native checks in `apps/web/src/Room.tsx` |
| REQ-021 | Bounded Agent Network volumes, waiting time and blocked work | 3.8 | agent_operations | Pending native checks in `internal/platform/agent_network.go` |
| REQ-022 | Needs you deduplicates one cause and records its owner | 3.9 | agent_operations | Pending native checks in `pkg/inbox` |
| REQ-023 | Roster exposes working, waiting, watching, paused, failed and last run | 3.10 | agent_operations | Pending native checks in `apps/web/src/Agents.tsx` |
| REQ-024 | Source origin, owner, access mode and freshness are real and visible | 4.1 | source_lifecycle | Pending native checks in `pkg/knowledge` |
| REQ-025 | Durable source lifecycle, bounded attempts and last good copy | 4.2 | source_lifecycle | Pending native checks in `pkg/knowledge` |
| REQ-026 | Stale failures raise one cause and pause dependent work by stop rule | 4.3 | source_lifecycle | Pending native checks in `internal/platform/source_lifecycle.go` |
| REQ-027 | Indexed, live and hybrid honor the reader on every request | 4.4 | source_lifecycle | Pending native checks in `pkg/knowledge` |
| REQ-028 | Source dependency graph answers Used by without hidden names/counts | 4.5 | source_lifecycle | Pending native checks in `pkg/knowledge` |
| REQ-029 | GitHub resources link to projects with explicit people mapping | 4.6 | source_lifecycle | Pending native checks in `pkg/clients` |
| REQ-030 | Slack binding directions and guest approval boundaries | 4.7 | source_lifecycle | Pending native checks in `pkg/clients` |
| REQ-031 | Drive and Notion provider paths use real API boundaries and references | 4.8 | source_lifecycle | Pending native checks in `pkg/knowledge` |
| REQ-032 | Connect Resources Links Access Events Actions report real state | 4.9 | source_lifecycle | Pending native checks in `apps/web/src/Clients.tsx` |
| REQ-033 | Suggest, auto-link and off rules are bounded, previewable and auditable | 4.10 | source_lifecycle | Pending native checks in `pkg/clients` |
| REQ-034 | Versioned scoped skill drafts, tests, publish and promotion | 5.1 | library_personal | Pending native checks in `pkg/skills` |
| REQ-035 | Skill and new-agent dry runs force actions to Ask | 5.2 | library_personal | Pending native checks in `pkg/skills` |
| REQ-036 | Memory scope, kind, provenance, review, expiry, usage and audited forget | 5.3 | library_personal | Pending native checks in `pkg/memory` |
| REQ-037 | Personal memory stays out of projects without explicit permission | 5.4 | library_personal | Pending native checks in `pkg/memory` |
| REQ-038 | Personal profile and briefing style are persisted and used | 5.5 | library_personal | Pending native checks in `pkg/prefs` |
| REQ-039 | Notification kind, digest and quiet-hour settings drive dispatch | 5.6 | library_personal | Pending native checks in `pkg/prefs` |
| REQ-040 | Personal agents expose ownership, pause and project promotion | 5.7 | library_personal | Pending native checks in `pkg/prefs` |
| REQ-041 | Connected accounts expose actual scopes, use and disconnect | 5.8 | library_personal | Pending native checks in `pkg/credentials` |
| REQ-042 | Cross-domain audit view is bounded, confined, durable and retained | 5.9 | library_personal | Pending native checks in `pkg/audit` |
| REQ-043 | Saved fork from message preserves bounded read-only history and lineage | 6.1 | conversation_actions | Pending native checks in `pkg/threads` |
| REQ-044 | Bring back result posts a reviewed result and never executes pending actions | 6.2 | conversation_actions | Pending native checks in `internal/platform/forks.go` |
| REQ-045 | Project fork copies chosen structure/config safely, rules disabled | 6.3 | conversation_actions | Pending native checks in `pkg/projects` |
| REQ-046 | People shows actual audience, roles, removal and safe invitations | 6.4 | conversation_actions | Pending native checks in `apps/web/src/ConversationShell.tsx` |
| REQ-047 | Quick switcher returns bounded readable destinations only | 6.5 | conversation_actions | Pending native checks in `internal/httpapi/discovery.go` |
| REQ-048 | Initiatives roll up status, risks, decisions, usage and needs safely | 6.6 | conversation_actions | Pending native checks in `pkg/projects` |
| REQ-049 | Create project blank, template, fork or under initiative | 6.7 | conversation_actions | Pending native checks in `apps/web/src/Projects.tsx` |
| REQ-050 | Outcomes lead with recommendation, decisions, deliverables and next action | 7.1 | workspace_outcomes | Pending native checks in `packages/core/src/types.ts` |
| REQ-051 | Redirect records revised intent separately from approval | 7.2 | workspace_outcomes | Pending native checks in `internal/platform/outcomes.go` |
| REQ-052 | Completed work collapses; blocked work exposes result/input/output on demand | 7.3 | workspace_outcomes | Pending native checks in `packages/web/src/AgentMessage.tsx` |
| REQ-053 | Versioned native previews keep real bytes, provenance and parser bounds | 7.4 | workspace_outcomes | Pending native checks in `apps/web/src/Deliverables.tsx` |
| REQ-054 | Native deliverables are real editable files, not prototype exports | 7.5 | workspace_outcomes | Pending native checks in `pkg/artifacts` |
| REQ-055 | Revision requests retain immutable version/slide context and unsent draft | 7.6 | workspace_outcomes | Pending native checks in `apps/web/src/Deliverables.tsx` |
| REQ-056 | Accepted outputs promote to knowledge/reviewed memory with audience checks | 7.7 | workspace_outcomes | Pending native checks in `internal/platform/artifacts.go` |
| REQ-057 | Agent and human timestamps use recorded instants, hover/focus/tap and timezone | 7.8 | workspace_outcomes | Pending native checks in `packages/web/src/AgentMessage.tsx` |
| REQ-058 | Composer is compact, mobile safe, textual and permission aware | 7.9 | workspace_outcomes | Pending native checks in `apps/web/src/Chat.tsx` |
| REQ-059 | Projects Library Operations You navigation exposes working routed pages | 7.10 | workspace_outcomes | Pending native checks in `apps/web/src/Sidebar.tsx` |
| REQ-060 | Phone/tablet/desktop, dark mode, keyboard and touch complete all journeys | 7.11 | workspace_outcomes | Pending native checks in `scripts/screens/run.ts` |
| REQ-061 | Saved-turn citations retain provenance and truthful indexed/live labels | 7.12 | workspace_outcomes | Pending native checks in `pkg/runs` |
| REQ-062 | Presentation, notes, versions and downloads preserve immutable selection | 7.13 | workspace_outcomes | Pending native checks in `apps/web/src/Deliverables.tsx` |
| REQ-063 | Identity, scope chips and punctuation follow repo accessibility/copy rules | 7.14 | workspace_outcomes | Pending native checks in `packages/components/src/copy.ts` |
| REQ-064 | Every shipped capability has automated and honest live validation evidence | 7.15 | root | Pending native checks in `docs/design/implementation/plan.md` |
| DES-001 | Real integration results | 4.8, 7.15 | source_lifecycle, root | Live checks recorded separately from configured credentials |
| DES-002 | No decorative metadata dots | 7.14 | workspace_outcomes | Shared copy/render checks |
| DES-003 | Native persisted fork | 6.1, 6.2 | conversation_actions | Fork lineage and isolation regressions |
| DES-004 | Recorded full message times | 7.8 | workspace_outcomes | Human/agent tooltip interaction and layout checks |
| DES-005 | Discoverable honest implementation record | 1.1 | root | This plan and architecture status |
| DES-006 | Independent sidebar scrolling | 7.11, 7.12 | workspace_outcomes | Existing Chromium wheel regression retained |
| DES-007 | Badge containment | 1.2 | root | Dark enlarged-text browser regression |

## Completion rule

Requirement mapping coverage is not implementation completeness. Root updates each evidence entry only after reviewing real code, its contract, failure/permission cases, persistence where applicable, and relevant browser state. The old gap analysis is a baseline to recheck, not a completion certificate.

## Verified foundation checkpoint

PR #75 contains the message action row, badge containment, recorded agent timestamps, and ledger-confirmed Stop Turn. All 164 phone/desktop layouts, workspace typechecks/tests, console build and real-server Slack tests passed. The stacked audit foundation passed golangci-lint v2, the full Go race suite with real Postgres/S3 fixtures, spec lint, generated-client no-diff and client checks. Remaining requirements stay pending until their native behavior and evidence land.

Structured full-suite evidence on the shared foundation: 39 packages passed with Postgres and S3, zero failed packages and zero skipped tests. Concurrent main changes retain DES-006 for sidebar scrolling and DES-007 for badge containment.
