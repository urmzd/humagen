# Documentation

| Document | Kind | Purpose |
|----------|------|---------|
| [Architecture](architecture/overview.md) | Architecture | Plan of record: what Humagen composes and the status table of what exists |
| [The ecosystem](architecture/ecosystem.md) | Architecture | The layers (Humagen and saige: its agent, flow, sandbox, durable) and the rules each keeps |
| [One model, two surfaces](architecture/surfaces.md) | Architecture | How each agent concept reaches the web and Slack, and the open gaps |
| [Projects](architecture/projects.md) | Architecture | Design of record for nested projects, policies, agents, inboxes and shared threads |
| [Company, personal work and finding help](architecture/company-workspace.md) | Architecture | The Company root project, personal projects, publishing work and Find Help |
| [Conversation workspace](architecture/conversation-workspace.md) | Architecture | The conversation page, activity and deliverables in the console |
| [Agent calls](architecture/agent-calls.md) | Architecture | How an answering agent calls other agents with the `agent` tool |
| [Agent operations](architecture/agent-operations.md) | Architecture | Agent controls, action policies, assignments and handoff contracts |
| [Security](architecture/security.md) | Architecture | Callers, roles, rules the code holds, credentials, inbox and break-glass reads |
| [Sources](architecture/sources.md) | Architecture | Library sources from Google Drive, OneDrive and Notion, credentials and access |
| [Library and personal settings](architecture/library-personal.md) | Architecture | Skills, Memory, Audit, Profile, Notifications, Privacy and Personal Agents |
| [Managed Library and web foundation](architecture/managed-library.md) | Architecture | The console's shadcn foundation, theme, fonts and Library folder browsers |
| [Native console notifications](architecture/native-notifications.md) | Architecture | The Notifications page, badge and delivery preferences |
| [Usage and Activity](architecture/usage-and-activity.md) | Architecture | How Usage intervals and Activity classifications are computed |
| [Egress guard](architecture/egress.md) | Architecture | How a refused write to Slack or GitHub is stopped, paused or backed off instead of repeated |
| [Typed learning dependencies](architecture/learning-dependencies.md) | Architecture | How skills and reviewed memory link back to the runs that produced them |
| [Develop with Tilt](guides/develop.md) | Guide | One `tilt up` for the API, the console with hot reload and Postgres, reusable by projects built on Humagen |
| [Deploy](guides/deploy.md) | Guide | Run Humagen locally, with Compose, Helm, Flux or `humagen deploy` |
| [Generate and revise a raster image](guides/image-generation.md) | Guide | Use Codex's account-backed image tool, inspect real pixels and retain local image revisions |
| [GitHub and Slack trial](guides/integrations.md) | Guide | Set up a local trial with a GitHub installation and a Slack app |
| [Read and organize a local folder](guides/local-files.md) | Guide | An agent on Claude Code surveys a folder on this host in place, plans, and applies only what a person approves |
| [Edit a document and audit a workbook](guides/office-files.md) | Guide | An agent on Claude Code edits a .docx in place and audits .xlsx formulas and revisions in a folder on this host |
| [Work out a refund path](guides/travel-refund.md) | Guide | Two agents on Claude Code: one reads a booking and current policy pages and cannot write, one writes the approved request and nothing else |
| [Build a sourced chronology from records](guides/records-research.md) | Guide | An agent on Claude Code reads a folder of records, cites and quotes each dated event, lists unknowns, and opens current official pages |
| [Reconcile account exports](guides/finance-exports.md) | Guide | An agent on Claude Code runs a shipped script over authorized exports, reports what does not reconcile, and drafts a plan whose amounts are all sourced |
| [Connect a Mac desktop](guides/desktop.md) | Guide | Pinned native MCP tools, permissions, targeting instructions, and an opt-in fixture check |
| [Connect a browser](guides/browser.md) | Guide | Pinned Playwright MCP configuration, existing-tab connection, and real browser acceptance |
| [Read selected mail and approve a calendar change](guides/mail-calendar.md) | Guide | Native account-tool profile, exact-target approval, and the limits of the checked read-only preflight |
| [Read mixed evidence and transcribe local media](guides/media-evidence.md) | Guide | Inspect original pages and frames, preserve ASR source timestamps and report uncertainty |
| [Compare cloud document revisions and approve an edit](guides/cloud-documents.md) | Guide | Existing Codex history and native Docs guarded-write connectors, with real private-fixture checks and missing-history limits |
| [Email notifications](guides/email-notifications.md) | Guide | Configure the outbound email notification relay |
| [Upgrading from the old name](guides/upgrading-from-warp.md) | Guide | What changed in the rename and how an operator moves to the new names |
| [Upgrading to saige v0.45.0](guides/upgrading-to-saige.md) | Guide | What changes for operators and Go programs now that orchagent is folded into saige |
| [Names](guides/renaming.md) | Guide | The ecosystem's current names and the retired ones |
| [Rename verification](guides/rename-verification.md) | Guide | Verification of the install and upgrade paths after the rename |
| [The Humagen spec](reference/spec.md) | Reference | The System and Platform configuration documents |
| [API contract](reference/api.md) | Reference | The OpenAPI contract, events, commands and the generated client |
| [Authorization](reference/authorization.md) | Reference | The policy language: grammar, roles and grants |
| [Clients](reference/clients.md) | Reference | The client record for Slack apps and GitHub installations |
| [Slack agent delivery](reference/slack.md) | Reference | The Slack webhook connector and Slack app: configuration and behavior |
| [GitHub agent delivery](reference/github.md) | Reference | The GitHub connector: mentions, labels, reviews and checks |
| [Design requirements](reference/design-requirements.md) | Reference | Console design requirements (DES-NNN) and their implementation evidence |
| [Workspace design principles](reference/workspace-principles.md) | Reference | Principles for the console, with the reference each one applies |
| [Design reference research](reference/design-reference-research.md) | Reference | Products and people chosen as references for the working interface |
| [Board design rules](reference/board-design-rules.md) | Reference | Format, tokens, identity and navigation rules for the design boards |
| [Control plane gap analysis](reference/control-plane-gap-analysis.md) | Reference | RFC 0001 checked against the code: built, partial, not built, diverges |
| [Console UI review](reference/ui-review.md) | Reference | Console UI review findings, patterns and how to check a UI change |
| [RFC 0001: Agentic workspace and control plane](rfcs/0001-agentic-workspace-control-plane.md) | RFC, Mostly built | Product and design spec for delegated agent work; design notes in [assets/control-plane](assets/control-plane/README.md) |
| [RFC 0002: Cross-project handoffs](rfcs/0002-cross-project-handoffs.md) | RFC, Proposed | A staged design for handoffs that cross project boundaries |
| [RFC 0003: Conversation-first delivery](rfcs/0003-conversation-first-delivery.md) | RFC, Partly built | Conversation-first delivery and collections for follow-on work |
| [RFC 0004: Isolated runs and person context](rfcs/0004-isolated-runs-and-person-context.md) | RFC, Mostly built | One agent for many people in parallel: pinned person context, isolated sandboxes, and the path to a million people in one room |
| [RFC 0005: Federated retrieval and the catalog](rfcs/0005-federated-retrieval-and-catalog.md) | RFC, Mostly built | Live search in external systems, a metadata catalog, and hybrid search for what Humagen owns |
| [RFC 0006: Multiple replicas, fiber folded in, and a durable event bus](rfcs/0006-replicas-fiber-and-durable-bus.md) | RFC, Partly built | fiber's fold completed, the standalone repository deprecated, and the Postgres bus with an outbox and consumer groups for many replicas |
| [RFC 0008: Feedback, run signals and training export](rfcs/0008-feedback-run-signals-and-training-export.md) | RFC, Partly built | Asking for ratings at the right moment, per-run signals and reward analytics, and consented, redacted KTO and DPO export |
| [Roadmap](plans/roadmap.md) | Plan | Product direction by area and the next delivery boundary |
| [Task queue](plans/2026-10-ui-task-queue.md) | Plan | Current UI delivery and follow-on platform work, in order |
| [Workspace implementation plan](plans/2026-10-workspace-implementation.md) | Plan | Tasks and requirement traceability for the workspace and control plane |
| [Implementation constraints](plans/2026-10-workspace-implementation-research.md) | Plan | Constraints resolved before implementing the workspace plan |
| [Prototype control audit](plans/2026-10-workspace-control-audit.md) | Plan | Acceptance status of every interactive control in the prototypes |
| [Final native acceptance](plans/2026-10-workspace-final-acceptance.md) | Plan | Recorded checks for the implemented workspace and control plane |
| [Local browser validation](plans/2026-10-workspace-local-browser-validation.md) | Plan | Incremental browser evidence from the persistent local preview |
| [Application validation](plans/2026-10-workspace-application-validation.md) | Plan | Local validation of the workspace prototype's application work |
| [ecosystem.md](ecosystem.md), [github.md](github.md), [slack.md](slack.md), [upgrading-from-warp.md](upgrading-from-warp.md) | Stub | Old paths that other repositories link; each points to its new home |

Design assets: [assets/control-plane](assets/control-plane/README.md) (design notes; the clickable boards were removed) and [assets/workspace-prototype](assets/workspace-prototype/README.md) (interactive prototype and sample files). Requirement and audit records for the implementation plan are in `assets/workspace-implementation/`.
