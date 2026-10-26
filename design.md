# Humagen design requirements

This file tracks the console requirements agreed in the design review on
October 25, 2026. Read it before changing presentation or integrations. Keep
requirement IDs stable and update implementation evidence in the same change
that satisfies a requirement.

The published [design boards](https://urmzd.github.io/humagen/) are interaction
references. Publishing a board does not implement its behavior in the console.
The detailed [control-plane spec](docs/design/humagen-control-plane/spec.md),
[board rules](docs/design/humagen-control-plane/design-rules.md), and
[gap analysis](docs/design/humagen-control-plane/gap-analysis.md) provide the
larger design. The gap analysis records its own review revision; recheck code
before treating it as current. Repository rules in [AGENTS.md](AGENTS.md) still
govern implementation, including shared presentation and agent identity.

## Requirements

| ID | Requirement | Status |
| --- | --- | --- |
| DES-001 | Integration controls use the existing system and report real results | Slack/GitHub configuration form implemented; automated checks pass; live delivery pending |
| DES-002 | Use spacing instead of dots between words in UI metadata | All boards and touched console metadata implemented and checked; audit other console screens when changing them |
| DES-003 | Reveal the message Fork action on hover and keyboard focus | Board interaction checked in Chromium; native conversation-fork API pending |
| DES-004 | Reveal full message date and time on hover and keyboard focus | Human messages and announcements implemented, DOM-tested and browser-checked; agent-message timestamps pending |
| OPS-001 | Durable assignments, scoped autonomy, agent pause and recorded coordination | Domain stores, admission/tool guards, OpenAPI routes and console components implemented. Restart tests cover memory and Postgres; UI DOM tests cover saved and refused actions. Global navigation, screenshots and source-health integration belong to the following stack layers. |
| DES-005 | Keep this requirements record discoverable and accurate | Documented; AGENTS.md references this file |
| DES-006 | Keep sidebar and main content scrolling independent | Implemented; Chromium wheel regression covers phone and desktop |
| DES-007 | Agent badge text remains contained at narrow widths and enlarged text sizes | Fixed; dark phone/desktop containment regression passes |

### DES-001: Working integrations

- Build on the existing client, connector, credentials, project-binding, and
  generated API client paths. Keep API calls and identity in surface hosts and
  the platform. Use the existing tenant and permission checks.
- The console must display saved connection state, expose only actions backed
  by a supported API, show in-progress requests and actionable failures, and
  refresh the saved state after a successful mutation. A successful local UI
  update alone is not a successful connection.
- Preserve credential references and secret storage conventions. Never return
  saved credentials to the browser or include secrets in logs or error text.
- Existing GitHub and Slack connector behavior is described in
  [integrations.md](docs/integrations.md) and [clients.md](docs/clients.md).
  Automatic resource linking, Drive, Notion, live search, and other board
  features require their own implementation evidence before being offered as
  working capabilities.
- Source lifecycle foundation: bounded durable refresh attempts, last-good
  copies, typed failures, stale stop rules and publication access predicates
  are implemented. Native Drive/Notion boundaries, personal reference resolution,
  scoped lifecycle APIs, source dependency views and connection controls are
  implemented. Automated transport tests, memory/Postgres replay, HTTP contract
  tests, source integration DOM states and phone/desktop source-state captures
  pass. Atomic scoped creation, real resource selection, frozen-copy replacement,
  sync settings, current-policy scheduled refresh and confined unsaved rule
  previews are implemented. Live provider accounts, verified connector rule
  application and durable link suggestions remain pending. See [sources](docs/sources.md) for supported formats and limits.
- Per-installation event subscriptions and actual registered native actions now
  have versioned saved controls. They tighten current adapter and action-policy
  grants and are rechecked at execution. Slack bridge directions are durable:
  inbound mirrors mapped human messages without waking an agent; outcomes sends
  permitted final outcomes and decisions; two-way supports both. Activity and
  retained source metadata are withheld, and private-context delivery is refused
  by the server. Native-host, contract, confinement and memory/Postgres replay
  tests exercise these paths. Live external delivery remains unverified.
- Acceptance: exercise each newly wired control through its real host/API
  boundary, including a saved success, a visible failure, and denied access.
  Distinguish automated or simulated API checks from a live external-service
  trial. Record which services and actions were actually verified.

### DES-002: No dots between words

- Remove middle-dot or bullet punctuation used solely to separate titles,
  statuses, timestamps, source modes, counts, or other adjacent metadata.
  Use layout gaps and clear labels while retaining readable text order.
- The review screenshot's evidence examples should read as separate spaced
  fields: `Supplier ETA` `live 9:42`, `Launch plan v3` `snapshot Oct 18`, and
  `Retail calendar` `live 9:30`.
- A semantic status indicator beside text, such as the green live-source
  indicator, may remain. This requirement concerns separators between words;
  it does not remove punctuation inside content, dates, or code.
- Acceptance: evidence pills and message metadata have no decorative text
  separators; labels remain understandable at narrow widths and when read by
  assistive technology. This requirement replaces separator examples in older
  boards where they conflict.

### DES-003: Fork on hover or focus

- Working interpretation of the review note "fork/date time hovering": show
  the message-level Fork action when its message is hovered or keyboard focus
  is within the message. Keep the control available on touch devices through
  a visible action or an accessible message menu.
- Use a native button for operations and a link for navigation, with an
  accessible name, visible focus, and adequate target size. Pointer movement
  from the message to the action must not hide
  the action. Do not depend on a tooltip to make the action usable.
- A conversation fork must preserve history through the chosen message as
  read-only context and retain lineage to the original conversation, subject
  to the control-plane spec's access and policy rules. Regenerating a sibling
  answer is a different operation and must not be labelled as a completed fork.
- Acceptance: pointer, keyboard, and touch can find and invoke the action;
  unsupported or denied operations cannot appear successful. A functional fork
  is verified against the saved new conversation and its lineage.

### DES-004: Date and time on hover or focus

- Under the same working interpretation, reveal the message's full date and
  time on hover or keyboard focus without crowding its resting presentation.
  Provide a touch-accessible equivalent.
- Use the recorded message timestamp, not a fabricated or latest-render time.
  Make the date and timezone unambiguous and retain machine-readable time
  semantics. Assistive technology must have access to the same information.
- Acceptance: the displayed detail matches the stored timestamp, works across
  date boundaries, and remains available without a mouse. Showing or hiding
  detail must not make the action target move away from the pointer.

### DES-006: Independent sidebar scrolling

- The console fills the dynamic viewport. Main content scrolls inside its own
  column without moving the sidebar or the document.
- Navigation and conversations scroll inside the sidebar only when that area
  is scrolled. Reaching its boundary must not scroll the main content.
- Keep the sidebar header and account controls visible while its lists scroll,
  including in the phone drawer.
- Evidence: `App.tsx`, `Sidebar.tsx`, and `styles.css` constrain the shell and
  contain sidebar overscroll. `scripts/screens/sidebar-scroll.ts`, run through
  the `sidebar-scroll` screen fixture, checks real wheel input, long lists,
  fixed header/account geometry and independent content scroll in Chromium at
  phone and desktop widths. Long content is inserted only for the layout test.

## Review and implementation evidence

The approved implementation pass covers supported Slack and GitHub workspace
connection controls through the existing clients API, removal of separator dots
in the boards and touched console metadata, accessible native human-message
timestamps, and Fork hover/focus behavior in the board. Native conversation
forking remains pending because its backend operation is not implemented.
Creating a configured client is not evidence that external credentials work or
that a live Slack or GitHub delivery has succeeded.
For GitHub default-agent routing, configure the app's own bot login so native
mentions reach that agent. The form requires the login when a default agent
is supplied; naming an agent explicitly remains a separate routing path.

| Requirement | Implementation and evidence | Remaining work |
| --- | --- | --- |
| DES-001 | `apps/web/src/ClientConnect.tsx`, `Clients.tsx`, and `api.ts` use the existing `POST /api/clients` contract. Settings DOM tests cover Slack values, GitHub references and routing, server refusal, permission gating, secret clearing, and default-deny reach. Reproducible phone/desktop captures cover Slack and GitHub forms, pending requests and server refusal; new copy lives in `packages/components/src/copy.ts`. | Supply installed-app credentials and a reachable signed webhook listener, then observe real replies. OAuth installation and GitHub token refresh remain separate backend work. |
| DES-002 | All 26 board HTML files render in Chromium without separator dots. Main evidence fields wrap without horizontal overflow at 390px. `ConversationShell.tsx`, `Room.tsx`, `Clients.tsx`, and web `AgentMessage.tsx` use gaps or readable phrases. | Older untouched console and shared presentation copy needs review as it changes. |
| DES-003 | `boards/ForkConversation.html` and `assets/boards.css` reveal actions on hover/focus, retain touch availability, and navigate to the existing prototype panel. Chromium checked hover, focus, target focus, and stable message dimensions. | A native fork needs a saved conversation operation, lineage, permissions, routes, and isolation tests. The board action is a prototype. |
| DES-004 | Human messages and announcements in `Room.tsx` use recorded timestamps with full local date, time, and timezone on hover, focus, and tap. Room DOM tests cover the recorded instant, accessible description, and dismissal. A Chromium phone regression reproduced tooltip overflow before the fix; phone and desktop captures verify placement through the existing panel-gutter helper. | Carry real recorded timestamps through agent-message presentation. |
| DES-005 | Root `AGENTS.md` links this record and requires keeping it current. | Update evidence with each subsequent implementation. |

Validation for this pass: console production build, workspace typechecks and
JavaScript tests, phone and desktop screen checks, and `git diff --check`. DOM
tests and connection screen fixtures use simulated API responses; the board and
console layout checks use local Chromium. Neither establishes live external delivery.

The screenshot supplied in this review is the source for DES-002. The hover
behavior in DES-003 and DES-004 is an explicit working interpretation of the
review note and can be refined here as the interaction is reviewed.

[ChatGPT](https://chatgpt.com/) and [Claude](https://claude.ai/) may be consulted
as interaction references when accessible. No inspection of those sites is
claimed by this document, and they are not integration providers or a new
dependency of the console.

For each implementation, record the relevant requirement ID, code paths,
automated checks, manual interaction checks, and any remaining external setup.
Mark a requirement verified only when its acceptance evidence is recorded.
Keep [docs/architecture.md](docs/architecture.md#status) current when a capability
lands. The documentation entry for DES-005 does not imply DES-001 through
DES-004 have been implemented or live-validated.

### Built-in handles and Slack conversation navigation

New agent choices and starter actions use `humagen-analyst`, `humagen-writer`
and `humagen-builder`. Compatibility registry definitions preserve saved
revisions, old schedules and existing grants. The `humagen-builtins` screen
checks all three names at phone and desktop widths.

A Slack task card exposes Open Conversation when the host has configured
`HUMAGEN_PUBLIC_URL`. It points to the run's conversation, including a later
run in an existing conversation, and remains attached throughout streaming
and settlement. Adapter and Slack rendering regressions cover the link.

### DES-007: Agent badge containment

Agent names and status badges wrap as whole items, and badge height follows
its text. Long scope labels truncate within the badge without losing their
accessible text. The `agent-status-badges` browser state makes the analyst the
workspace default and checks both badges in dark mode with enlarged text.
The containment assertion reproduces the reported failure before the fix.

### Library subsystem implementation evidence

- `pkg/audit`, `internal/platform/audit_view.go`: durable pending/succeeded/failed change attempts,
  bounded redacted snapshots, retained legacy adapters and exact-owner personal filtering.
- `pkg/memory`, `internal/httpapi/personal_memory.go`: private personal owner namespace,
  reviewed kind/provenance, expiry at composition, immediate audited forget and persisted use counts.
- `pkg/prefs/personal.go`: persisted briefing, notification delivery policy, midnight/DST quiet
  hours and privacy choices. Native delivery is implemented in `pkg/notifications` and the authenticated console worker, with restart/dedupe, current-reader confinement, hourly digests, quiet hours and projection-only acknowledgements. Email remains unavailable.
- `apps/web/src/Audit.tsx`, `MemoryLibrary.tsx`, `PersonalPreferences.tsx`: standalone views for
  shared navigation integration. Versioned skill draft/test/publish/promotion workflows use real configured agents and the existing pinned runtime, with explicit tool intersections and forced Ask on tests.
