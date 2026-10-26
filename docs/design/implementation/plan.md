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

| Layer | Branch | Pull request | Owner |
| --- | --- | --- | --- |
| Badge containment and shared message actions | codex/agent-badge-layout | [#75](https://github.com/urmzd/humagen/pull/75), base main | root |
| Plan, audit and source foundations | codex/artifact-ui-completion | [#76](https://github.com/urmzd/humagen/pull/76), base #75 | root, audit core from library_personal |
| Company and authoritative placement | codex/company-placement | [#78](https://github.com/urmzd/humagen/pull/78), base #76 | company_placement |
| Agent operations | codex/agent-operations | [#79](https://github.com/urmzd/humagen/pull/79), base #78 | agent_operations |
| Sources and integrations | codex/source-lifecycle | [#80](https://github.com/urmzd/humagen/pull/80), base #79 | source_lifecycle |
| Clean Library and personal review layer | codex/library-personal-review | [#81](https://github.com/urmzd/humagen/pull/81), base #80 | library_personal |
| Conversation interactions | codex/conversation-actions | [#82](https://github.com/urmzd/humagen/pull/82), base #81 | conversation_actions |
| Usage and Activity | codex/workspace-observability | [#83](https://github.com/urmzd/humagen/pull/83), base #82 | root |
| Native SMTP notifications | codex/notification-email | [#84](https://github.com/urmzd/humagen/pull/84), base #83 | root |
| Outcomes and integrated workspace | codex/workspace-outcomes | Upper PR publication pending, intended base #84 | workspace_outcomes |

Branches are integrated through no-ff merges, never rebase/reset/force-push. Each PR targets its immediate parent; root publishes after integrating the lower layer. Shared API/navigation changes are reconciled and verified before promotion.

## Current implementation and acceptance status

Reconciled October 26 against the tested native union: runtime `902641d` with source-learning adapter `8f43384`, native document browser helper `9d5df97`, repeated-agent action-row regression `1cce847`. Every requirement below has its own status and checked evidence in `requirements.json`; the traceability table uses the same values.

A checked task means its native implementation and relevant automated checks passed. It does not certify full visual parity, final remote CI or external-account authorization. Live-account pending states remain explicit even when the native implementation task is checked.

The full Go race run at `a0d7059` passed 47 tested packages and 2,380 test/subtest records, with zero failures and zero test skips using isolated Postgres and S3. After `902641d`, focused platform/HTTP/Library race checks pass for skills, source dependencies, outcome learning, response contracts and private confinement on memory and Postgres. The final full rerun remains separate. The final Library/upper console suite passes 566 tests without skips and types pass. Adapter 58 tests, client 18 tests, generated-client no-diff, console build, OpenAPI lint, Biome and golangci-lint passed at the recorded checkpoints. Four actual phone/desktop native DOCX/XLSX journeys passed: editable original bytes, PDF export, exact-version requested-change draft, new v2 and unchanged v1 hash.

The P2 native follow-ups have now landed: the bounded source dependency graph and typed derived-skill/reviewed-memory reader (`59baeef`, `8f43384`, REQ-028), exact-version skill Used By and publication readiness (`3272306`, REQ-034), and native Needs Review roster/filter plus Context links (`902641d`, REQ-023). Relevant native/contract and upper rendered regressions pass; this does not claim final screenshot acceptance for those controls.

Final canonical responsive, keyboard/touch and dark-mode acceptance, eight showcase captures, final stacked CI and external authorization/delivery checks remain open (REQ-001, REQ-004, REQ-060, REQ-064). The 484 static control mappings and supplemental dynamic-control table are traceability evidence, not a full-parity certificate. Root will record final capture outcomes and honest authorization limits separately.

## Implementation steps

### Phase 1: Foundation and review

- [ ] T001 [Plan:1.1] Complete requirement traceability and native verification. Native implementation/evidence in `docs/design/implementation/plan.md`, `docs/design/implementation/control-audit.md`, `docs/design/implementation/control-audit.json`. [REQ-001]; owner root. Source: `docs/design/humagen-control-plane/gap-analysis.md`.
- [x] T002 [Plan:1.2] Badge outlines contain labels at narrow widths and enlarged text. Native implementation/evidence in `apps/web/src/ui.tsx`, `apps/web/src/Agents.tsx`, `scripts/screens/run.ts`. [REQ-002]; owner root. Source: `user supplied badge screenshot`.
- [x] T003 [Plan:1.3] Copy and Reply share one consistent accessible action row. Native implementation/evidence in `packages/web/src/AgentMessage.tsx`, `apps/web/src/Room.tsx`, `packages/web/src/MessageActionRow.tsx`, `apps/web/src/room.dom.test.tsx`. [REQ-003]; owner workspace_outcomes. Source: `user supplied message-actions screenshot`.
- [ ] T004 [Plan:1.4] Prototype controls become real authenticated state transitions. Native implementation/evidence in `api/openapi.yaml`, `internal/httpapi/server.go`, `internal/httpapi/openapi_test.go`. [REQ-004]; owner all. Source: `docs/design/humagen-workspace/README.md#try-the-interaction-loop`.
- [x] T005 [Plan:1.5] Preserve agent identity, stable colors and compatibility history. Native implementation/evidence in `internal/platform/builtin.go`, `pkg/agentcolors/store.go`, `pkg/agentcolors`, `design.md`. [REQ-005]; owner root. Source: `AGENTS.md; design.md#built-in-handles-and-slack-conversation-navigation`.
- [x] T006 [Plan:1.6] Durable bounded safe audit attempts underpin domain mutations. Native implementation/evidence in `pkg/audit`, `internal/platform/audit.go`, `pkg/audit/audit_test.go`, `internal/platform/audit_test.go`. [REQ-006]; owner library_personal. Source: `docs/design/humagen-control-plane/spec.md#control-plane-operations`.

### Phase 2: Company and authoritative placement

- [x] T007 [Plan:2.1] Company is the root and every agent has an explicit home. Native implementation/evidence in `pkg/projects/homes.go`, `internal/platform/agents.go`, `internal/httpapi/personal_address_test.go`. [REQ-007]; owner company_placement. Source: `docs/company-workspace.md#one-company-project`.
- [x] T008 [Plan:2.2] Idempotent durable personal project provisioning for human members. Native implementation/evidence in `pkg/projects`, `pkg/projects/personal_test.go`, `internal/httpapi/personal_address_test.go`. [REQ-008]; owner company_placement. Source: `docs/company-workspace.md#a-personal-project-for-each-person`.
- [x] T009 [Plan:2.3] Exact-owner personal read/write confinement, governance recorded. Native implementation/evidence in `internal/platform/sees.go`, `pkg/projects/roles.go`, `internal/platform/personal_context_test.go`, `internal/httpapi/confinement_test.go`. [REQ-009]; owner company_placement. Source: `docs/company-workspace.md#a-personal-project-for-each-person`.
- [x] T010 [Plan:2.4] Versioned current placement separate from immutable accounting. Native implementation/evidence in `pkg/threads`, `pkg/runs`, `internal/platform/threads.go`, `internal/platform/placement_projection_test.go`. [REQ-010]; owner company_placement. Source: `docs/company-workspace.md#moving-work-into-a-public-project`.
- [x] T011 [Plan:2.5] Publish completed private work with audience preview and race safety. Native implementation/evidence in `apps/web/src/ConversationShell.tsx`, `internal/platform/threads.go`, `internal/platform/publication_race_test.go`, `internal/platform/summary_publication.go`. [REQ-011]; owner company_placement. Source: `docs/company-workspace.md#moving-work-into-a-public-project`.
- [x] T012 [Plan:2.6] One retained announcement banner above the greeting. Native implementation/evidence in `internal/platform/announce.go`, `internal/platform/welcome.go`, `apps/web/src/HomePage.tsx`. [REQ-012]; owner company_placement. Source: `docs/company-workspace.md#an-announcement-above-the-greeting`.
- [x] T013 [Plan:2.7] Permission-filtered Find Help and safe retirement of new SME routing. Native implementation/evidence in `pkg/pick`, `internal/platform/threads.go`, `apps/web/src/Chat.tsx`, `internal/platform/find_help.go`. [REQ-013]; owner company_placement. Source: `docs/company-workspace.md#find-the-project-and-agent-for-the-work`.

### Phase 3: Delegation and agent operations

- [x] T014 [Plan:3.1] Durable assignment goal, owner, completion criteria and outcomes. Native implementation/evidence in `pkg/operations`, `internal/platform/assignments.go`, `apps/web/src/Assignments.tsx`, `pkg/operations/operations_test.go`. [REQ-014]; owner agent_operations. Source: `docs/conversation-workspace.md#follow-through`.
- [x] T015 [Plan:3.2] Standing work and triggers reuse scheduler, Invoke and pkg/call. Native implementation/evidence in `pkg/call`, `internal/platform/assignments.go`, `internal/platform/assignment_cron_recovery_test.go`, `internal/platform/assignment_sources_test.go`. [REQ-015]; owner agent_operations. Source: `docs/design/humagen-control-plane/spec.md#agents-and-how-they-talk-to-each-other`.
- [x] T016 [Plan:3.3] Inherited tighten-only Allow, Ask, Never action policy. Native implementation/evidence in `pkg/operations`, `internal/platform/action_policy_view_test.go`, `apps/web/src/ActionPolicies.tsx`. [REQ-016]; owner agent_operations. Source: `docs/design/humagen-control-plane/spec.md#agents-and-how-they-talk-to-each-other`.
- [x] T017 [Plan:3.4] Named managers, escalation, reminders, budgets and stop rules. Native implementation/evidence in `pkg/operations`, `internal/platform/hooks.go`, `internal/platform/operation_reminders_runtime_test.go`, `internal/platform/operation_reminders_test.go`. [REQ-017]; owner agent_operations. Source: `docs/design/humagen-control-plane/spec.md#agents-and-how-they-talk-to-each-other`.
- [x] T018 [Plan:3.5] Typed handoff contracts, deadlines, states, cycles and hop limits. Native implementation/evidence in `pkg/operations`, `pkg/call`, `internal/platform/handoffs.go`, `pkg/operations/invocation_test.go`. [REQ-018]; owner agent_operations. Source: `docs/design/humagen-control-plane/spec.md#agents-and-how-they-talk-to-each-other`.
- [x] T019 [Plan:3.6] Pause agent, pause all and enforce control at every admission path. Native implementation/evidence in `pkg/operations`, `internal/platform/agents.go`, `apps/web/src/AgentControls.tsx`, `internal/platform/operations_internal_test.go`. [REQ-019]; owner agent_operations. Source: `docs/design/humagen-control-plane/gap-analysis.md#not-built`.
- [x] T020 [Plan:3.7] Real console Stop control follows terminal ledger state. Native implementation/evidence in `apps/web/src/Room.tsx`, `packages/web/src/AgentMessage.tsx`, `apps/web/src/RunStop.tsx`, `apps/web/src/room.dom.test.tsx`. [REQ-020]; owner workspace_outcomes. Source: `docs/design/humagen-control-plane/gap-analysis.md#not-built`.
- [x] T021 [Plan:3.8] Bounded Agent Network volumes, waiting time and blocked work. Native implementation/evidence in `apps/web/src/AgentNetwork.tsx`, `internal/httpapi/operations_test.go`. [REQ-021]; owner agent_operations. Source: `docs/design/humagen-control-plane/spec.md#agents-and-how-they-talk-to-each-other`.
- [x] T022 [Plan:3.9] Needs you deduplicates one cause and records its owner. Native implementation/evidence in `pkg/inbox`, `internal/platform/inbox.go`, `pkg/inbox/reminders_test.go`, `internal/platform/operations_internal_test.go`. [REQ-022]; owner agent_operations. Source: `docs/design/humagen-control-plane/spec.md#control-plane-operations`.
- [x] T023 [Plan:3.10] Roster exposes working, waiting, watching, paused, failed and last run. Native implementation/evidence in `apps/web/src/Agents.tsx`, `apps/web/src/AgentControls.tsx`, `internal/platform/operations.go`, `apps/web/src/AgentOperations.tsx`. [REQ-023]; owner agent_operations. Source: `docs/design/humagen-control-plane/spec.md#agents-and-how-they-talk-to-each-other`.

### Phase 4: Sources and integrations

- [x] T024 [Plan:4.1] Source origin, owner, access mode and freshness are real and visible. Native implementation/evidence in `pkg/knowledge`, `apps/web/src/conversation-sources.dom.test.tsx`, `apps/web/src/SourceLifecycle.tsx`. [REQ-024]; owner source_lifecycle. Source: `docs/design/humagen-control-plane/spec.md#context-provenance-and-the-sync-lifecycle`.
- [x] T025 [Plan:4.2] Durable source lifecycle, bounded attempts and last good copy. Native implementation/evidence in `pkg/knowledge`, `internal/platform/source_lifecycle.go`, `pkg/knowledge/lifecycle_test.go`, `internal/platform/source_lifecycle_test.go`. [REQ-025]; owner source_lifecycle. Source: `docs/design/humagen-control-plane/spec.md#context-provenance-and-the-sync-lifecycle`.
- [x] T026 [Plan:4.3] Stale failures raise one cause and pause dependent work by stop rule. Native implementation/evidence in `internal/platform/source_lifecycle.go`, `pkg/inbox`, `pkg/operations`, `internal/platform/source_assignment.go`. [REQ-026]; owner source_lifecycle. Source: `docs/design/humagen-control-plane/spec.md#context-provenance-and-the-sync-lifecycle`.
- [x] T027 [Plan:4.4] Indexed, live and hybrid honor the reader on every request. Native implementation/evidence in `pkg/knowledge`, `internal/platform/knowledge.go`, `pkg/credentials`, `internal/platform/source_grounding_test.go`. [REQ-027]; owner source_lifecycle. Source: `docs/design/humagen-control-plane/spec.md#context-access-modes`.
- [x] T028 [Plan:4.5] Source dependency graph answers Used by without hidden names/counts. Native implementation/evidence in `pkg/knowledge`, `internal/platform/source_attachments.go`, `internal/platform/source_rules.go`. [REQ-028]; owner source_lifecycle. Source: `docs/design/humagen-control-plane/spec.md#system-foundations-and-phasing`.
- [x] T029 [Plan:4.6] GitHub resources link to projects with explicit people mapping. Native implementation/evidence in `pkg/clients`, `internal/platform/surfaces.go`, `apps/web/src/Clients.tsx`, `internal/platform/source_suggestions.go`. [REQ-029]; owner source_lifecycle. Source: `docs/design/humagen-control-plane/spec.md#integrations`.
- [x] T030 [Plan:4.7] Slack binding directions and guest approval boundaries. Native implementation/evidence in `pkg/clients`, `pkg/projects/channels.go`, `apps/web/src/Clients.tsx`, `internal/platform/bridge_delivery_test.go`. [REQ-030]; owner source_lifecycle. Source: `docs/design/humagen-control-plane/spec.md#integrations`.
- [x] T031 [Plan:4.8] Drive and Notion provider paths use real API boundaries and references. Native implementation/evidence in `pkg/knowledge`, `pkg/knowledge/provider_test.go`, `internal/platform/source_refresh_test.go`. [REQ-031]; owner source_lifecycle. Source: `docs/design/humagen-control-plane/spec.md#integrations`.
- [x] T032 [Plan:4.9] Connect Resources Links Access Events Actions report real state. Native implementation/evidence in `apps/web/src/Clients.tsx`, `apps/web/src/ClientConnect.tsx`, `apps/web/src/Integrations.tsx`, `apps/web/src/integrations.dom.test.tsx`. [REQ-032]; owner source_lifecycle. Source: `docs/design/humagen-control-plane/spec.md#integrations`.
- [x] T033 [Plan:4.10] Suggest, auto-link and off rules are bounded, previewable and auditable. Native implementation/evidence in `pkg/clients`, `pkg/sourcelinks/suggestions_test.go`, `internal/platform/source_rules_test.go`, `apps/web/src/integrations.dom.test.tsx`. [REQ-033]; owner source_lifecycle. Source: `docs/design/humagen-control-plane/spec.md#integrations`.

### Phase 5: Library, personal settings and audit

- [x] T034 [Plan:5.1] Versioned scoped skill drafts, tests, publish and promotion. Native implementation/evidence in `internal/platform/skills.go`, `pkg/library/skills_test.go`, `internal/platform/skill_library_test.go`, `apps/web/src/SkillsLibrary.tsx`. [REQ-034]; owner library_personal. Source: `docs/design/humagen-control-plane/spec.md#skills-and-memory`.
- [x] T035 [Plan:5.2] Skill and new-agent dry runs force actions to Ask. Native implementation/evidence in `internal/platform/agent_setup_test.go`, `internal/platform/skill_library_test.go`. [REQ-035]; owner library_personal. Source: `docs/design/humagen-control-plane/spec.md#core-flows`.
- [x] T036 [Plan:5.3] Memory scope, kind, provenance, review, expiry, usage and audited forget. Native implementation/evidence in `pkg/memory`, `apps/web/src/MemoryLibrary.tsx`, `internal/platform/personal_context_test.go`. [REQ-036]; owner library_personal. Source: `docs/design/humagen-control-plane/spec.md#skills-and-memory`.
- [x] T037 [Plan:5.4] Personal memory stays out of projects without explicit permission. Native implementation/evidence in `pkg/memory`, `internal/platform/knowledge.go`, `internal/platform/personal_context_test.go`, `internal/platform/agent_skill_defaults_test.go`. [REQ-037]; owner library_personal. Source: `docs/design/humagen-control-plane/spec.md#skills-and-memory`.
- [x] T038 [Plan:5.5] Personal profile and briefing style are persisted and used. Native implementation/evidence in `pkg/prefs`, `apps/web/src/Settings.tsx`, `internal/httpapi/personal_profile_test.go`, `pkg/prefs/personal_test.go`. [REQ-038]; owner library_personal. Source: `docs/design/humagen-control-plane/spec.md#user-level-configuration-you`.
- [x] T039 [Plan:5.6] Notification kind, digest and quiet-hour settings drive dispatch. Native implementation/evidence in `pkg/prefs`, `internal/platform/notifications.go`, `internal/platform/notifications_test.go`, `internal/platform/email_notifications_test.go`. [REQ-039]; owner library_personal. Source: `docs/design/humagen-control-plane/spec.md#user-level-configuration-you`.
- [x] T040 [Plan:5.7] Personal agents expose ownership, pause and project promotion. Native implementation/evidence in `pkg/prefs`, `internal/platform/agents.go`, `apps/web/src/PersonalAgents.tsx`, `pkg/projects/personal_test.go`. [REQ-040]; owner library_personal. Source: `docs/design/humagen-control-plane/spec.md#user-level-configuration-you`.
- [x] T041 [Plan:5.8] Connected accounts expose actual scopes, use and disconnect. Native implementation/evidence in `pkg/credentials`, `apps/web/src/Connections.tsx`, `internal/platform/credentials_test.go`, `internal/httpapi/credentials_test.go`. [REQ-041]; owner library_personal. Source: `docs/design/humagen-control-plane/spec.md#user-level-configuration-you`.
- [x] T042 [Plan:5.9] Cross-domain audit view is bounded, confined, durable and retained. Native implementation/evidence in `pkg/audit`, `internal/httpapi/audit.go`, `apps/web/src/Audit.tsx`, `internal/httpapi/audit_test.go`. [REQ-042]; owner library_personal. Source: `docs/design/humagen-control-plane/spec.md#control-plane-operations`.

### Phase 6: Conversation and project interaction

- [x] T043 [Plan:6.1] Saved fork from message preserves bounded read-only history and lineage. Native implementation/evidence in `pkg/threads`, `internal/platform/forks.go`, `apps/web/src/ForkConversation.tsx`, `internal/platform/forks_test.go`. [REQ-043]; owner conversation_actions. Source: `docs/design/humagen-control-plane/spec.md#forking`.
- [x] T044 [Plan:6.2] Bring back result posts a reviewed result and never executes pending actions. Native implementation/evidence in `internal/platform/forks.go`, `apps/web/src/ForkConversation.tsx`, `internal/platform/forks_test.go`. [REQ-044]; owner conversation_actions. Source: `docs/design/humagen-control-plane/spec.md#forking`.
- [x] T045 [Plan:6.3] Project fork copies chosen structure/config safely, rules disabled. Native implementation/evidence in `pkg/projects`, `internal/platform/project_forks.go`, `internal/platform/project_forks_test.go`, `pkg/projectforks/projectforks_test.go`. [REQ-045]; owner conversation_actions. Source: `docs/design/humagen-control-plane/spec.md#forking`.
- [x] T046 [Plan:6.4] People shows actual audience, roles, removal and safe invitations. Native implementation/evidence in `apps/web/src/ConversationShell.tsx`, `internal/platform/threads.go`, `apps/web/src/ConversationAccess.tsx`, `apps/web/src/conversation-sources.dom.test.tsx`. [REQ-046]; owner conversation_actions. Source: `docs/design/humagen-control-plane/gap-analysis.md#partial`.
- [x] T047 [Plan:6.5] Quick switcher returns bounded readable destinations only. Native implementation/evidence in `internal/httpapi/discovery.go`, `apps/web/src/QuickSwitcher.tsx`, `internal/platform/discovery_test.go`. [REQ-047]; owner conversation_actions. Source: `docs/design/humagen-control-plane/spec.md#hierarchy-initiatives-projects-and-sub-projects`.
- [x] T048 [Plan:6.6] Initiatives roll up status, risks, decisions, usage and needs safely. Native implementation/evidence in `pkg/projects`, `internal/platform/projects.go`, `apps/web/src/Projects.tsx`, `internal/platform/project_rollup_test.go`. [REQ-048]; owner conversation_actions. Source: `docs/design/humagen-control-plane/spec.md#hierarchy-initiatives-projects-and-sub-projects`.
- [x] T049 [Plan:6.7] Create project blank, template, fork or under initiative. Native implementation/evidence in `apps/web/src/Projects.tsx`, `internal/platform/project_forks.go`, `pkg/projects/templates_test.go`, `apps/web/src/ForkProject.tsx`. [REQ-049]; owner conversation_actions. Source: `docs/design/humagen-control-plane/spec.md#hierarchy-initiatives-projects-and-sub-projects`.

### Phase 7: Outcomes, native deliverables and full workspace presentation

- [x] T050 [Plan:7.1] Outcomes lead with recommendation, decisions, deliverables and next action. Native implementation/evidence in `packages/core/src/types.ts`, `packages/components/src/components.tsx`, `packages/web/src/AgentMessage.tsx`, `internal/platform/outcome_test.go`. [REQ-050]; owner workspace_outcomes. Source: `docs/design/humagen-control-plane/spec.md#summary-and-principles`.
- [x] T051 [Plan:7.2] Redirect records revised intent separately from approval. Native implementation/evidence in `packages/components/src/components.tsx`, `apps/web/src/Chat.tsx`, `apps/web/src/Room.tsx`, `apps/web/src/workspace.dom.test.tsx`. [REQ-051]; owner workspace_outcomes. Source: `docs/design/humagen-workspace/index.html`.
- [x] T052 [Plan:7.3] Completed work collapses; blocked work exposes result/input/output on demand. Native implementation/evidence in `packages/web/src/AgentMessage.tsx`, `packages/components/src/components.tsx`, `apps/web/src/Activity.tsx`, `apps/web/src/activity.dom.test.tsx`. [REQ-052]; owner workspace_outcomes. Source: `docs/conversation-workspace.md#five-interactions`.
- [x] T053 [Plan:7.4] Versioned native previews keep real bytes, provenance and parser bounds. Native implementation/evidence in `apps/web/src/Deliverables.tsx`, `apps/web/src/artifactPreview.ts`, `pkg/nativefiles/tables_test.go`, `apps/web/src/artifactPreview.test.ts`. [REQ-053]; owner workspace_outcomes. Source: `docs/conversation-workspace.md#versioned-deliverables`.
- [x] T054 [Plan:7.5] Native deliverables are real editable files, not prototype exports. Native implementation/evidence in `pkg/artifacts`, `internal/platform/sandbox.go`, `apps/web/src/Deliverables.tsx`, `internal/platform/sandbox_test.go`. [REQ-054]; owner workspace_outcomes. Source: `docs/design/humagen-workspace/README.md#try-the-interaction-loop`.
- [x] T055 [Plan:7.6] Revision requests retain immutable version/slide context and unsent draft. Native implementation/evidence in `apps/web/src/Deliverables.tsx`, `apps/web/src/Room.tsx`, `scripts/screens/workspace.ts`, `apps/web/src/workspace.dom.test.tsx`. [REQ-055]; owner workspace_outcomes. Source: `docs/conversation-workspace.md#versioned-deliverables`.
- [x] T056 [Plan:7.7] Accepted outputs promote to knowledge/reviewed memory with audience checks. Native implementation/evidence in `pkg/knowledge`, `pkg/memory`, `internal/platform/outcome_learning_race_test.go`, `internal/platform/outcome_learning_test.go`. [REQ-056]; owner workspace_outcomes. Source: `docs/conversation-workspace.md#follow-through`.
- [x] T057 [Plan:7.8] Agent and human timestamps use recorded instants, hover/focus/tap and timezone. Native implementation/evidence in `packages/web/src/AgentMessage.tsx`, `packages/adapter/src/fold.ts`, `apps/web/src/Room.tsx`, `apps/web/src/MessageTime.tsx`. [REQ-057]; owner workspace_outcomes. Source: `design.md#des-004-date-and-time-on-hover-or-focus`.
- [x] T058 [Plan:7.9] Composer is compact, mobile safe, textual and permission aware. Native implementation/evidence in `apps/web/src/Chat.tsx`, `apps/web/src/Room.tsx`, `packages/core/src/queue.test.ts`, `apps/web/src/chat.dom.test.tsx`. [REQ-058]; owner workspace_outcomes. Source: `docs/design/humagen-control-plane/gap-analysis.md#partial`.
- [x] T059 [Plan:7.10] Projects Library Operations You navigation exposes working routed pages. Native implementation/evidence in `apps/web/src/Sidebar.tsx`, `apps/web/src/App.tsx`, `apps/web/src/route.ts`, `apps/web/src/route.test.ts`. [REQ-059]; owner workspace_outcomes. Source: `docs/design/humagen-control-plane/spec.md#mental-model-and-information-architecture`.
- [ ] T060 [Plan:7.11] Phone/tablet/desktop, dark mode, keyboard and touch complete all journeys. Native implementation/evidence in `scripts/screens/run.ts`, `apps/web/src/styles.css`, `scripts/screens/workspace.ts`, `scripts/screens/library.ts`. [REQ-060]; owner workspace_outcomes. Source: `docs/design/humagen-workspace/README.md`.
- [x] T061 [Plan:7.12] Saved-turn citations retain provenance and truthful indexed/live labels. Native implementation/evidence in `pkg/runs`, `packages/adapter/src/run.ts`, `packages/web/src/AgentMessage.tsx`, `internal/platform/source_provenance_test.go`. [REQ-061]; owner workspace_outcomes. Source: `docs/design/humagen-control-plane/gap-analysis.md#partial`.
- [x] T062 [Plan:7.13] Presentation, notes, versions and downloads preserve immutable selection. Native implementation/evidence in `apps/web/src/Deliverables.tsx`, `packages/web/src/ArtifactPreview.tsx`, `apps/web/src/workspace.dom.test.tsx`, `scripts/screens/native.ts`. [REQ-062]; owner workspace_outcomes. Source: `docs/design/humagen-workspace/README.md#try-the-interaction-loop`.
- [x] T063 [Plan:7.14] Identity, scope chips and punctuation follow repo accessibility/copy rules. Native implementation/evidence in `packages/components/src/copy.ts`, `packages/core/src/types.ts`, `apps/web/src/ui.tsx`, `apps/web/src/Plus.tsx`. [REQ-063]; owner workspace_outcomes. Source: `AGENTS.md; docs/design/humagen-control-plane/spec.md#objects-scopes-and-identity`.
- [ ] T064 [Plan:7.15] Every shipped capability has automated and honest live validation evidence. Native implementation/evidence in `docs/design/implementation/plan.md`, `docs/architecture.md`, `scripts/screens/run.ts`, `docs/design/implementation/control-audit.md`. [REQ-064]; owner root. Source: `design.md#implementation-and-review-evidence`.

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
| REQ-001 | Complete requirement traceability and native verification | 1.1 | root | pending_final_acceptance: `docs/design/implementation/control-audit.md`; `docs/design/implementation/control-audit.json` |
| REQ-002 | Badge outlines contain labels at narrow widths and enlarged text | 1.2 | root | implemented_native_checks_passed: `apps/web/src/ui.tsx`; `scripts/screens/run.ts` |
| REQ-003 | Copy and Reply share one consistent accessible action row | 1.3 | workspace_outcomes | implemented_native_checks_passed: `packages/web/src/MessageActionRow.tsx`; `apps/web/src/room.dom.test.tsx` |
| REQ-004 | Prototype controls become real authenticated state transitions | 1.4 | all | pending_final_acceptance: `api/openapi.yaml`; `internal/httpapi/openapi_test.go` |
| REQ-005 | Preserve agent identity, stable colors and compatibility history | 1.5 | root | implemented_native_checks_passed: `pkg/agentcolors`; `design.md` |
| REQ-006 | Durable bounded safe audit attempts underpin domain mutations | 1.6 | library_personal | implemented_native_checks_passed: `pkg/audit/audit_test.go`; `internal/platform/audit_test.go` |
| REQ-007 | Company is the root and every agent has an explicit home | 2.1 | company_placement | implemented_native_checks_passed: `pkg/projects/homes.go`; `internal/httpapi/personal_address_test.go` |
| REQ-008 | Idempotent durable personal project provisioning for human members | 2.2 | company_placement | implemented_native_checks_passed: `pkg/projects/personal_test.go`; `internal/httpapi/personal_address_test.go` |
| REQ-009 | Exact-owner personal read/write confinement, governance recorded | 2.3 | company_placement | implemented_native_checks_passed: `internal/platform/personal_context_test.go`; `internal/httpapi/confinement_test.go` |
| REQ-010 | Versioned current placement separate from immutable accounting | 2.4 | company_placement | implemented_native_checks_passed: `internal/platform/placement_projection_test.go`; `internal/platform/placement.go` |
| REQ-011 | Publish completed private work with audience preview and race safety | 2.5 | company_placement | implemented_native_checks_passed: `internal/platform/publication_race_test.go`; `internal/platform/summary_publication.go` |
| REQ-012 | One retained announcement banner above the greeting | 2.6 | company_placement | implemented_native_checks_passed: `internal/platform/welcome.go`; `apps/web/src/HomePage.tsx` |
| REQ-013 | Permission-filtered Find Help and safe retirement of new SME routing | 2.7 | company_placement | implemented_native_checks_passed: `internal/platform/find_help.go`; `apps/web/src/FindHelp.tsx` |
| REQ-014 | Durable assignment goal, owner, completion criteria and outcomes | 3.1 | agent_operations | implemented_native_checks_passed: `pkg/operations/operations_test.go`; `internal/platform/assignment_deadline_runtime_test.go` |
| REQ-015 | Standing work and triggers reuse scheduler, Invoke and pkg/call | 3.2 | agent_operations | implemented_native_checks_passed: `internal/platform/assignment_cron_recovery_test.go`; `internal/platform/assignment_sources_test.go` |
| REQ-016 | Inherited tighten-only Allow, Ask, Never action policy | 3.3 | agent_operations | implemented_native_checks_passed: `internal/platform/action_policy_view_test.go`; `apps/web/src/ActionPolicies.tsx` |
| REQ-017 | Named managers, escalation, reminders, budgets and stop rules | 3.4 | agent_operations | implemented_native_checks_passed: `internal/platform/operation_reminders_runtime_test.go`; `internal/platform/operation_reminders_test.go` |
| REQ-018 | Typed handoff contracts, deadlines, states, cycles and hop limits | 3.5 | agent_operations | implemented_native_checks_passed: `internal/platform/handoffs.go`; `pkg/operations/invocation_test.go` |
| REQ-019 | Pause agent, pause all and enforce control at every admission path | 3.6 | agent_operations | implemented_native_checks_passed: `internal/platform/operations_internal_test.go`; `apps/web/src/AgentControls.tsx` |
| REQ-020 | Real console Stop control follows terminal ledger state | 3.7 | workspace_outcomes | implemented_native_checks_passed: `apps/web/src/RunStop.tsx`; `apps/web/src/room.dom.test.tsx` |
| REQ-021 | Bounded Agent Network volumes, waiting time and blocked work | 3.8 | agent_operations | implemented_native_checks_passed: `apps/web/src/AgentNetwork.tsx`; `internal/httpapi/operations_test.go` |
| REQ-022 | Needs you deduplicates one cause and records its owner | 3.9 | agent_operations | implemented_native_checks_passed: `pkg/inbox/reminders_test.go`; `internal/platform/operations_internal_test.go` |
| REQ-023 | Roster exposes working, waiting, watching, paused, failed and last run | 3.10 | agent_operations | implemented_native_checks_passed: `internal/platform/operations.go`; `internal/platform/agent_skill_defaults_test.go` |
| REQ-024 | Source origin, owner, access mode and freshness are real and visible | 4.1 | source_lifecycle | implemented_native_checks_passed: `apps/web/src/conversation-sources.dom.test.tsx`; `apps/web/src/SourceLifecycle.tsx` |
| REQ-025 | Durable source lifecycle, bounded attempts and last good copy | 4.2 | source_lifecycle | implemented_native_checks_passed: `pkg/knowledge/lifecycle_test.go`; `internal/platform/source_lifecycle_test.go` |
| REQ-026 | Stale failures raise one cause and pause dependent work by stop rule | 4.3 | source_lifecycle | implemented_native_checks_passed: `internal/platform/source_assignment.go`; `internal/platform/source_operations_test.go` |
| REQ-027 | Indexed, live and hybrid honor the reader on every request | 4.4 | source_lifecycle | implemented_native_checks_passed: `internal/platform/source_grounding_test.go`; `internal/platform/source_privacy_test.go` |
| REQ-028 | Source dependency graph answers Used by without hidden names/counts | 4.5 | source_lifecycle | implemented_native_checks_passed: `internal/platform/source_dependency_reader.go`; `internal/platform/source_dependencies_test.go` |
| REQ-029 | GitHub resources link to projects with explicit people mapping | 4.6 | source_lifecycle | implemented_live_authorization_pending: `internal/platform/source_suggestions.go`; `pkg/integrations/github/runtime_test.go` |
| REQ-030 | Slack binding directions and guest approval boundaries | 4.7 | source_lifecycle | implemented_live_authorization_pending: `internal/platform/bridge_delivery_test.go`; `pkg/projects/bridge_direction_test.go` |
| REQ-031 | Drive and Notion provider paths use real API boundaries and references | 4.8 | source_lifecycle | implemented_live_authorization_pending: `pkg/knowledge/provider_test.go`; `internal/platform/source_refresh_test.go` |
| REQ-032 | Connect Resources Links Access Events Actions report real state | 4.9 | source_lifecycle | implemented_native_checks_passed: `apps/web/src/Integrations.tsx`; `apps/web/src/integrations.dom.test.tsx` |
| REQ-033 | Suggest, auto-link and off rules are bounded, previewable and auditable | 4.10 | source_lifecycle | implemented_native_checks_passed: `pkg/sourcelinks/suggestions_test.go`; `internal/platform/source_rules_test.go` |
| REQ-034 | Versioned scoped skill drafts, tests, publish and promotion | 5.1 | library_personal | implemented_native_checks_passed: `internal/platform/skill_usage.go`; `internal/httpapi/skill_usage_test.go` |
| REQ-035 | Skill and new-agent dry runs force actions to Ask | 5.2 | library_personal | implemented_native_checks_passed: `internal/platform/agent_setup_test.go`; `internal/platform/skill_library_test.go` |
| REQ-036 | Memory scope, kind, provenance, review, expiry, usage and audited forget | 5.3 | library_personal | implemented_native_checks_passed: `pkg/memory`; `apps/web/src/MemoryLibrary.tsx` |
| REQ-037 | Personal memory stays out of projects without explicit permission | 5.4 | library_personal | implemented_native_checks_passed: `internal/platform/personal_context_test.go`; `internal/platform/agent_skill_defaults_test.go` |
| REQ-038 | Personal profile and briefing style are persisted and used | 5.5 | library_personal | implemented_native_checks_passed: `internal/httpapi/personal_profile_test.go`; `pkg/prefs/personal_test.go` |
| REQ-039 | Notification kind, digest and quiet-hour settings drive dispatch | 5.6 | library_personal | implemented_native_checks_passed: `internal/platform/notifications_test.go`; `internal/platform/email_notifications_test.go` |
| REQ-040 | Personal agents expose ownership, pause and project promotion | 5.7 | library_personal | implemented_native_checks_passed: `apps/web/src/PersonalAgents.tsx`; `pkg/projects/personal_test.go` |
| REQ-041 | Connected accounts expose actual scopes, use and disconnect | 5.8 | library_personal | implemented_native_checks_passed: `internal/platform/credentials_test.go`; `internal/httpapi/credentials_test.go` |
| REQ-042 | Cross-domain audit view is bounded, confined, durable and retained | 5.9 | library_personal | implemented_native_checks_passed: `internal/httpapi/audit_test.go`; `internal/platform/audit_client_visibility_test.go` |
| REQ-043 | Saved fork from message preserves bounded read-only history and lineage | 6.1 | conversation_actions | implemented_native_checks_passed: `internal/platform/forks_test.go`; `pkg/threads/forks_test.go` |
| REQ-044 | Bring back result posts a reviewed result and never executes pending actions | 6.2 | conversation_actions | implemented_native_checks_passed: `internal/platform/forks_test.go`; `apps/web/src/ForkConversation.tsx` |
| REQ-045 | Project fork copies chosen structure/config safely, rules disabled | 6.3 | conversation_actions | implemented_native_checks_passed: `internal/platform/project_forks_test.go`; `pkg/projectforks/projectforks_test.go` |
| REQ-046 | People shows actual audience, roles, removal and safe invitations | 6.4 | conversation_actions | implemented_native_checks_passed: `apps/web/src/ConversationAccess.tsx`; `apps/web/src/conversation-sources.dom.test.tsx` |
| REQ-047 | Quick switcher returns bounded readable destinations only | 6.5 | conversation_actions | implemented_native_checks_passed: `internal/platform/discovery_test.go`; `apps/web/src/QuickSwitcher.tsx` |
| REQ-048 | Initiatives roll up status, risks, decisions, usage and needs safely | 6.6 | conversation_actions | implemented_native_checks_passed: `internal/platform/project_rollup_test.go`; `apps/web/src/ProjectRollup.tsx` |
| REQ-049 | Create project blank, template, fork or under initiative | 6.7 | conversation_actions | implemented_native_checks_passed: `pkg/projects/templates_test.go`; `apps/web/src/Projects.tsx` |
| REQ-050 | Outcomes lead with recommendation, decisions, deliverables and next action | 7.1 | workspace_outcomes | implemented_native_checks_passed: `internal/platform/outcome_test.go`; `packages/web/src/AgentMessage.tsx` |
| REQ-051 | Redirect records revised intent separately from approval | 7.2 | workspace_outcomes | implemented_native_checks_passed: `apps/web/src/Chat.tsx`; `apps/web/src/Room.tsx` |
| REQ-052 | Completed work collapses; blocked work exposes result/input/output on demand | 7.3 | workspace_outcomes | implemented_native_checks_passed: `packages/web/src/AgentMessage.tsx`; `apps/web/src/Activity.tsx` |
| REQ-053 | Versioned native previews keep real bytes, provenance and parser bounds | 7.4 | workspace_outcomes | implemented_native_checks_passed: `pkg/nativefiles/tables_test.go`; `apps/web/src/artifactPreview.test.ts` |
| REQ-054 | Native deliverables are real editable files, not prototype exports | 7.5 | workspace_outcomes | implemented_native_checks_passed: `internal/platform/sandbox_test.go`; `pkg/nativefiles/nativefiles_test.go` |
| REQ-055 | Revision requests retain immutable version/slide context and unsent draft | 7.6 | workspace_outcomes | implemented_native_checks_passed: `scripts/screens/workspace.ts`; `apps/web/src/workspace.dom.test.tsx` |
| REQ-056 | Accepted outputs promote to knowledge/reviewed memory with audience checks | 7.7 | workspace_outcomes | implemented_native_checks_passed: `internal/platform/outcome_learning_race_test.go`; `internal/platform/outcome_learning_test.go` |
| REQ-057 | Agent and human timestamps use recorded instants, hover/focus/tap and timezone | 7.8 | workspace_outcomes | implemented_native_checks_passed: `apps/web/src/MessageTime.tsx`; `apps/web/src/room.dom.test.tsx` |
| REQ-058 | Composer is compact, mobile safe, textual and permission aware | 7.9 | workspace_outcomes | implemented_native_checks_passed: `packages/core/src/queue.test.ts`; `apps/web/src/chat.dom.test.tsx` |
| REQ-059 | Projects Library Operations You navigation exposes working routed pages | 7.10 | workspace_outcomes | implemented_native_checks_passed: `apps/web/src/App.tsx`; `apps/web/src/Sidebar.tsx` |
| REQ-060 | Phone/tablet/desktop, dark mode, keyboard and touch complete all journeys | 7.11 | workspace_outcomes | pending_final_responsive_validation: `scripts/screens/run.ts`; `scripts/screens/workspace.ts` |
| REQ-061 | Saved-turn citations retain provenance and truthful indexed/live labels | 7.12 | workspace_outcomes | implemented_native_checks_passed: `internal/platform/source_provenance_test.go`; `pkg/runs/citations_test.go` |
| REQ-062 | Presentation, notes, versions and downloads preserve immutable selection | 7.13 | workspace_outcomes | implemented_native_checks_passed: `apps/web/src/Deliverables.tsx`; `apps/web/src/workspace.dom.test.tsx` |
| REQ-063 | Identity, scope chips and punctuation follow repo accessibility/copy rules | 7.14 | workspace_outcomes | implemented_native_checks_passed: `packages/components/src/copy.ts`; `apps/web/src/Plus.tsx` |
| REQ-064 | Every shipped capability has automated and honest live validation evidence | 7.15 | root | pending_final_live_ci_validation: `docs/design/implementation/control-audit.md`; `docs/design/implementation/checkpoints/local-browser-validation.md` |
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

PR #75 contains the message action row, badge containment, recorded agent timestamps, and ledger-confirmed Stop Turn. All 164 phone/desktop layouts, workspace typechecks/tests, console build and real-server Slack tests passed. The stacked audit foundation passed golangci-lint v2, the full Go race suite with real Postgres/S3 fixtures, spec lint, generated-client no-diff and client checks. This is historical lower-layer evidence; the current per-requirement statuses above supersede the original pending list.

Structured full-suite evidence on the shared foundation: 39 packages passed with Postgres and S3, zero failed packages and zero skipped tests. Concurrent main changes retain DES-006 for sidebar scrolling and DES-007 for badge containment.
