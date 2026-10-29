# Documentation

| Document | Kind | Purpose |
|----------|------|---------|
| [Architecture](architecture/overview.md) | Architecture | Plan of record: what Humagen composes and the status table of what exists |
| [The ecosystem](architecture/ecosystem.md) | Architecture | The layers (Humagen, fiber, saige, playscape, anchor) and the rules each keeps |
| [One model, two surfaces](architecture/surfaces.md) | Architecture | How each agent concept reaches the web and Slack, and the open gaps |
| [Projects](architecture/projects.md) | Architecture | Design of record for nested projects, policies, agents, inboxes and shared threads |
| [Company, personal work and finding help](architecture/company-workspace.md) | Architecture | The Company root project, personal projects, publishing work and Find Help |
| [Conversation workspace](architecture/conversation-workspace.md) | Architecture | The conversation page, activity and deliverables in the console |
| [Agent calls](architecture/agent-calls.md) | Architecture | How an answering agent calls helpers with `call_agent` |
| [Agent operations](architecture/agent-operations.md) | Architecture | Agent controls, action policies, assignments and handoff contracts |
| [Security](architecture/security.md) | Architecture | Callers, roles, rules the code holds, credentials, inbox and break-glass reads |
| [Sources](architecture/sources.md) | Architecture | Library sources from Google Drive and Notion, credentials and access |
| [Library and personal settings](architecture/library-personal.md) | Architecture | Skills, Memory, Audit, Profile, Notifications, Privacy and Personal Agents |
| [Managed Library and web foundation](architecture/managed-library.md) | Architecture | The console's shadcn foundation, theme, fonts and Library folder browsers |
| [Native console notifications](architecture/native-notifications.md) | Architecture | The Notifications page, badge and delivery preferences |
| [Usage and Activity](architecture/usage-and-activity.md) | Architecture | How Usage intervals and Activity classifications are computed |
| [Typed learning dependencies](architecture/learning-dependencies.md) | Architecture | How skills and reviewed memory link back to the runs that produced them |
| [Develop with Tilt](guides/develop.md) | Guide | One `tilt up` for the API, the console with hot reload and Postgres, reusable by projects built on Humagen |
| [Deploy](guides/deploy.md) | Guide | Run Humagen locally, with Compose, Helm, Flux or `humagen deploy` |
| [GitHub and Slack trial](guides/integrations.md) | Guide | Set up a local trial with a GitHub installation and a Slack app |
| [Email notifications](guides/email-notifications.md) | Guide | Configure the outbound email notification relay |
| [Upgrading from the old name](guides/upgrading-from-warp.md) | Guide | What changed in the rename and how an operator moves to the new names |
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
| [RFC 0001: Agentic workspace and control plane](rfcs/0001-agentic-workspace-control-plane.md) | RFC, Prepared | Product and design spec for delegated agent work; boards in [assets/control-plane](assets/control-plane/README.md) |
| [RFC 0002: Cross-project handoffs](rfcs/0002-cross-project-handoffs.md) | RFC, Proposed | A staged design for handoffs that cross project boundaries |
| [RFC 0003: Conversation-first delivery](rfcs/0003-conversation-first-delivery.md) | RFC, Proposed | Conversation-first delivery and collections for follow-on work |
| [Roadmap](plans/roadmap.md) | Plan | Product direction by area and the next delivery boundary |
| [Task queue](plans/2026-10-ui-task-queue.md) | Plan | Current UI delivery and follow-on platform work, in order |
| [Workspace implementation plan](plans/2026-10-workspace-implementation.md) | Plan | Tasks and requirement traceability for the workspace and control plane |
| [Implementation constraints](plans/2026-10-workspace-implementation-research.md) | Plan | Constraints resolved before implementing the workspace plan |
| [Prototype control audit](plans/2026-10-workspace-control-audit.md) | Plan | Acceptance status of every interactive control in the prototypes |
| [Final native acceptance](plans/2026-10-workspace-final-acceptance.md) | Plan | Recorded checks for the implemented workspace and control plane |
| [Local browser validation](plans/2026-10-workspace-local-browser-validation.md) | Plan | Incremental browser evidence from the persistent local preview |
| [Application validation](plans/2026-10-workspace-application-validation.md) | Plan | Local validation of the workspace prototype's application work |
| [ecosystem.md](ecosystem.md), [github.md](github.md), [slack.md](slack.md), [upgrading-from-warp.md](upgrading-from-warp.md) | Stub | Old paths that other repositories link; each points to its new home |

Design assets: [assets/control-plane](assets/control-plane/README.md) (clickable boards, published to GitHub Pages) and [assets/workspace-prototype](assets/workspace-prototype/README.md) (interactive prototype and sample files). Requirement and audit records for the implementation plan are in `assets/workspace-implementation/`.
