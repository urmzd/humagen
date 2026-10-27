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

The [workspace principles](docs/design/workspace-principles.md) apply the
[designer and portfolio research](docs/design/reference-research.md): Harvey II
for project structure, Codex for human control, Linear for restraint, and Dust
for testing working interactions. Storybook previews use the console's own
components. These references inform Humagen's identity rather than replacing it.
The [task queue](docs/ui-task-queue.md) tracks the active delivery. The
[conversation-first design](docs/design/conversation-workspace.md) separates
planned collections, browser, protocol and improvement work from this UI pass.

## Requirements

| ID | Requirement | Status |
| --- | --- | --- |
| DES-001 | Integration controls use the existing system and report real results | Slack/GitHub configuration form implemented; automated checks pass; live delivery pending |
| DES-002 | Use spacing instead of dots between words in UI metadata | All boards and touched console metadata implemented and checked; audit other console screens when changing them |
| DES-003 | Reveal the message Fork action on hover and keyboard focus | Native saved-fork API, immutable lineage, source checks and inspector built; shared action wiring pending |
| DES-004 | Reveal full message date and time on hover and keyboard focus | Human messages and announcements implemented, DOM-tested and browser-checked; agent-message timestamps pending |
| OPS-001 | Durable assignments, scoped autonomy, agent pause and recorded coordination | Domain stores, admission/tool guards, OpenAPI routes and console components implemented. Restart tests cover memory and Postgres; UI DOM tests cover saved and refused actions, inherited policy provenance, effective pause, typed contract inputs and reviewed deadlines. Native reminders survive restart on the same confined inbox item. Global navigation, screenshots and source-health integration belong to the following stack layers. |
| DES-005 | Keep this requirements record discoverable and accurate | Documented; AGENTS.md references this file |
| DES-006 | Keep sidebar and main content scrolling independent | Implemented; Chromium wheel regression covers phone and desktop |
| DES-007 | Agent badge text remains contained at narrow widths and enlarged text sizes | Fixed; dark phone/desktop containment regression passes |
| DES-008 | Agent/project discovery belongs in the new-conversation picker | Implemented; search and selection preserve drafts/files and cannot submit work |
| DES-009 | Use consistent action icons and readable inbox recovery groups | Implemented; semantic shared icons, touch targets, collapsed companion diagnostics and native recovery captures |
| DES-010 | Notifications consolidates actionable work and delivered updates | Implemented; one Notifications feed, one status/type filter panel and a permission-aware Alert Rules companion tab |
| DES-011 | Conversation drawers reflow desktop content, with a compact header and consistent controls | Implemented; shared inspector dock, mobile native dialogs and responsive browser regressions |
| DES-012 | Workspace search is a centered modal with indexed content and native destinations | Implemented; permission-confined content indexes, API contract, replay and search interaction tests |
| DES-013 | Project selection is directly editable before starting work; every starter suggestion remains available | Implemented; stacked project glyph, searchable drawer, draft-preserving selection and wrapping starters |
| DES-014 | Composer context choices use named categories and scoped subpanes | Implemented; Agents, Projects and Skills preserve the draft, and channel choices stay inside their project |
| DES-015 | Shared web controls use one semantic theme, font and responsive sizing | Implemented; standard shadcn Radix controls, renderer adapters and foundation stories |
| DES-016 | Skills packages and managed Memory chains have browsable, scoped files | Implemented; immutable packages, folder import/editor, native memory review and private contextual chat |
| DES-017 | Related destinations use tabs and compact operational screens | Implemented; permission-aware Library, Agents and Operations hubs, bounded policy rows and actionable source health |
| DES-018 | Motion and metadata make navigation changes easy to follow | Implemented; Tailwind drawer/menu states, retained inert exits, reduced-motion behavior, named project pills and + New above search |
| DES-019 | Project work has readable states, scoped filters and honest recovery | Implemented; automated delivery checks pass; final browser/CI verification pending |
| DES-020 | Workspace previews reuse console controls, fonts and consolidated navigation | Implemented; automated delivery checks pass; final browser/CI verification pending |
| DES-021 | Sidebar conversations show agents and people; projects and conversations can be pinned independently | Implemented; saved preferences, access checks, restart and DOM regressions, and responsive sidebar captures |
| DES-022 | Conversation actions support rename, archive and admin-reviewed deletion | Included in the consolidated branch; bounded API, durable lifecycle and review UI checks |

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
timestamps, and Fork hover/focus behavior in the board. Native conversation forking now has a persisted operation, bounded passive history,
source-chain confinement, selected sources applied to real turns, and an explicit
bring-back action that posts without executing tags or tools. Shared message action
wiring remains tracked separately.
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

### DES-008: Contextual agent and project discovery

Offer Find an Agent or Project inside the new-conversation composer's picker.
Home keeps its message composer and actual updates. Searching matches readable
projects and available agents through the existing bounded API. Choosing a
match updates the current selection, preserving the draft and attached files;
an empty draft receives the search description. Search Enter does not send a
message. Escape returns to the picker before closing it. Existing conversations
keep their fixed project and have no global discovery action.

The composer DOM regressions cover search, selection, attachment preservation,
explicit submission, empty-draft seeding and keyboard focus. The native
`workspace-find-help` and `workspace-find-help-selection` screen states exercise
matching and selection without creating a conversation.


### DES-009: Action vocabulary and inbox recovery

Message actions use the shared 24px-grid icon set, with a reply arrow for Reply
and semantic glyphs beside Fork, Bring Back, skill, memory and Redirect labels.
Footer icons share a 16px size and coarse-pointer targets are at least 44px.
Existing callbacks, action names and keyboard behavior are preserved.

An inbox failure groups its callout, recovery actions and reference using the
shared stack layout. Alert diagnostics that accompany an actionable failure
sit under Technical Details; standalone alert bodies remain visible. Titles,
metadata and About links wrap with separate spacing for each section. Render
regressions cover the failing layout and native `inbox-alert-recovery` states
capture collapsed and expanded diagnostics on phone and desktop.


### DES-010: Top-level notifications

Notifications has one bell navigation entry and a single feed of requests and
updates. There is no Needs You or Updates tab or separate list section. The feed
starts with all messages; one Filter popover combines status (All, Open,
Resolved) and type, with an active-filter count and Reset. Search stays in the
same toolbar. Filtering preserves the selected reading pane and server paging.
Alert Rules is the only companion tab, visible with `alerts:read`, and uses the
existing native rule editor without another Overview/Rules strip.

Both `/notifications` and existing `/inbox` links open the same feed. Message,
alert and kind selections remain addressable. The navigation badge counts
actionable work and always opens Notifications. Machine readers with `runs:read`
use the same feed without human delivery polling. Notification preferences
remain at `/you/notifications`.

The host retains one authenticated delivery reader. Projection acknowledgements
remain separate from resolving the original message and never add duplicate
feed rows. Reader changes clear stale delivery rows; polling and acknowledgement
failures retain recovery. A delivery-status failure leaves the feed usable.
The Workspace/Notifications Storybook stories exercise the real feed, combined
filters, empty/failure states, phone layout and Alert Rules navigation.
Validation: 30 route/navigation checks and 43 focused DOM checks pass. Both
production builds and console/Storybook typechecks pass. The Notifications browser
journey passes 20 light/dark states at 320, 390, 768 and 1440 pixels, including the
combined filter panel, Alert Rules, errors, empty states, message dismissal and a
selected reader retained outside the filtered list. Evidence is in
`.screens/notifications/checks.json`; reproduce with
`STORYBOOK_URL=http://127.0.0.1:6007 pnpm --filter @humagen/screens exec node --experimental-strip-types notifications.ts`.
The concurrent full console run reports 636/637 passing; its remaining failure is
`the tree at depth 4 indents one level at 390px, and deeper rows say where they sit`
(`ok(indents.length > 0)`) in the other session's project layout work.

### DES-011: Docked conversation workspace

Context, People & Access, Activity, Share and Deliverables use the shared
inspector dock. At 1024px and above the drawer occupies its own flex column,
reflowing the conversation and composer. At smaller widths it is a native modal
edge drawer with contained focus. Closing returns focus and restores the main
column. The header has one left-aligned title, project/channel metadata and a
compact action row. Share owns project placement; More contains Activity and
Deliverables. Control geometry uses the existing shared icon family.

Dark console surfaces use neutral charcoal planes, IBM Plex Sans for reading and headings, with
IBM Plex Mono for editors and technical text. Agent hues and focus use the existing
accent tokens. The `conversation-drawer-reflow` browser state asserts geometry
and desktop/mobile modality. Existing DES-010 removes the boxed Notifications
disclosure and retains the canonical bell destination.

### DES-012: Indexed workspace search

Search Workspace opens a centered native modal from navigation or Cmd/Ctrl+K.
Filters cover projects, agents, conversations, messages, files and sources.
Results keep native links, contextual excerpts and keyboard navigation; Escape
restores the opener. Pending requests clear previous results, and failures
show an explicit error. Search never starts an agent.

`GET /api/discovery?content=true` adds saved human messages and original agent
requests/answers, published file text and authored local source passages. Current
permissions, project placement and private/fork access confine every result.
Message/run search covers the past year, at most 1000 matching candidates per
kind; the modal returns 20 results. File text is capped at 65536 characters at
publication. Older files retain title, description and existing preview lookup
until republished. Source search uses the existing BM25 index, at most 50
readable local sources. Retained external source passages are withheld pending
current-reader file authorization. Private prepared context, reasoning and tool
payloads are excluded. Live providers are not queried on each keystroke. This
does not rewrite previously retained conversation text; its existing access
rules continue to apply.

Indexes derive from authoritative records and rebuild after restart. Go tests
cover indexing, replay, tenant confinement, current file versions and deletion;
platform tests exercise readable and hidden content. Console DOM tests exercise
native result destinations and request failures. The `workspace-search-modal`
browser state checks centering, keyboard traversal and focus return.

### DES-013: Project selection and starter suggestions

Projects use a folder with a small second edge underneath. The composer footer
opens a searchable project drawer directly, including when Personal is selected.
Personal and Company remain explicit homes; selecting another project resets
only the channel. Draft text and attachments stay in the mounted composer.
Existing conversations retain their fixed placement and Share review.

Starter suggestions wrap with consistent marks and touch targets. They stay
visible while the person edits a draft and change context only when the project
changes. No action is hidden at phone widths. A suggestion fills an editable draft and adds its agent
while retaining the project; sending remains explicit. DOM regressions cover
Personal selection, preserved drafts and project-aware starters. The
`home-project-picker` screen exercises the complete selection flow and checks
all three suggestion buttons fit within the phone viewport.

Validation for DES-011 through DES-013: production build; 15 workspace
typechecks; 593 console tests; complete text-index, thread, run and artifact
store suites with `-race`; focused platform access/projection/replay and HTTP
contract tests; shared Slack validation (79 tests). Dark Chromium captures
passed 18 phone/desktop states and 12 states at 320, 390 and 1280px, including
project switching, all starter suggestions, search focus return and drawer
reflow. Local captures live under `.screens/ui-cleanup` and
`.screens/project-selection` in the cleanup worktree. No live deployment is
claimed. This layer builds on main after PR #91 and retains the merged stack's action,
notification, native-file, source and fork-lock changes.


### DES-014: Hierarchical composer context

The plus menu has consistent root actions for files, Agents, Projects and
Skills. Each category opens a named pane with Back and predictable keyboard
focus. Agent search and discovery stay in Agents. Project choices include
Personal, Company and readable projects, with the existing project lock for
started conversations. A lone internal General channel does not appear as a
loose control. Real channel choices use full-width selected rows within their
project context.

Skills come from the existing project-scoped completion resolver. Choosing one
prepends its normal skill tag to the current draft, preserving text and files,
and sends nothing. A project change immediately withholds old skill rows and
completion options while the new project loads; refused reads offer no skill.
DOM and native browser states cover root categories, nested choices, keyboard
Back/Escape, draft and file preservation, and explicit message submission.

Validation: 607 console tests passed; all 292 phone/desktop screen captures
passed, plus 15 dark captures at 320, 768 and 1440px. The eight showcase images
were refreshed from the reproducible screen fixtures.

### DES-015: Shared web component foundation

Keep React, Vite and Tailwind. Standard shadcn Radix controls belong in
`packages/render-web/src/ui`, with one semantic theme and the actual bundled
IBM Plex Sans Variable font for reading and headings. IBM Plex Mono serves
file editors and technical text. Both fonts are self-hosted with their licenses.
Neutral light and charcoal surfaces use red as the default interaction accent,
with real yellow and green presets. Existing purple, blue and teal preferences
remain valid. Gold attention, green success and agent identity colors retain
their separate meanings. Glass and a thin static color streak belong to chrome;
content and popovers keep opaque readable surfaces with restrained bevel/shadow.
Existing button and field helpers share the same variants and sizing.
Web-only resource browsers remain controlled presentation components;
the shared surface-neutral contracts and Slack renderer remain intact.

Acceptance: production and Storybook builds, workspace typechecks, standard
format/lint checks, dense shared controls, correct loaded font, keyboard focus,
reduced motion and phone/desktop reflow. The Controls and ResourceWorkspace
stories provide a repeatable design review surface.

### DES-016: Skills packages and managed Memory chains

Skills is a folder manager with import, file navigation, preview/source editing,
new immutable drafts and ZIP download. SKILL.md and resource bytes use the native
library revision store and saige's parser/snapshot. Existing testing, publication,
promotion and agent bindings remain. Frontmatter grants no tools; supporting
files are read only from the exact package pinned to a run.

Memory is organized by real authorized owners. Retained entries have file
addresses, projected provenance and native review/correction/forget operations.
Management status reflects actual configuration. Chat opens a private native
conversation draft without putting note text in the URL. Pending and expired
memory remain excluded from automatic context. Searching a scope rail preserves
the current chain and any correction draft. Dirty Skills drafts require Save or
explicit Discard before changing scope.

Acceptance: package bounds, malformed paths, immutable versions, replay, tenant
and personal boundaries, current promotion capabilities, separate failure/empty
states, native folder import/save/download, scoped deep links, draft preservation
and private chat placement. Details and limits: [managed Library](docs/managed-library.md).

### DES-017: Consolidated navigation and compact operational screens

Library is one sidebar link with Sources, Skills, Memory and Deliverables tabs.
Agents groups directory, network, assignments and controls. Operations groups
context health, integrations, action policies, schedules, usage and
audit. Notifications combines requests and updates in one feed, with Alert Rules
as its only companion tab and one status/type filter panel.
Native routes remain usable, and permissions control destinations and tabs
through one shared definition. Search and a single status/type Filter popover
replace the long inbox category strip; local search covers loaded updates and states when
older pages need to be loaded.

Action policy uses a readable width, compact labeled rows, descriptions,
inherited/effective values and one aligned save action. Context Health replaces
zero-filled dashboards with one source action when empty; populated views have
aligned filters, a compact status strip and details after selection. Development
fixture disclaimers are kept out of product copy.

Profile, Notifications, Privacy and workspace administration share compact
`SettingsFormRow` controls. Privacy uses icons and labeled switches with hints,
a direct Memory link and one aligned save action. Audit uses a shared
Radix/react-day-picker calendar with local date/time inputs, host conversion to
ISO timestamps and positive-range validation capped at 31 days. Compact event
rows expand retained details; resource, Refresh and Export actions carry icons.
Announcements use a compact card, one Open
Announcement action and a labeled eye-with-slash Hide button with a busy guard.

Acceptance: route and permission regressions, existing operational mutations,
phone/desktop navigation, policy and Context Health captures, narrow reflow and
keyboard selection. Automated delivery checks pass; final browser/CI verification for DES-015
through DES-018 remains pending. Current evidence is recorded below.

### DES-018: Motion, visibility and named project metadata

Native drawers and the search modal enter and exit with short Tailwind transitions.
Outgoing content is inert and hidden from assistive technology immediately; its
visual shell survives only long enough to finish leaving. Reopening cancels
pending removal. Desktop drawer width animates while the panel keeps its width,
so the conversation reflows without scaling text. Nested composer panes have
directional reveals and Radix folders expand and collapse. Reduced motion removes
travel and delays. Waiting states remain still.

Project rows show up to two named agent pills and useful nonzero activity.
Descriptions and additional agents expand under Details; long names wrap.
The sidebar starts with + New, followed by Search Work, with appropriate
workspace/operations separators. Home's project control opens a drawer using
its project icon and name, with no dropdown caret.

Acceptance: rapid drawer close/reopen, immediate focus return, nested keyboard
navigation, reduced motion, desktop dock reflow and narrow project metadata.

Conversation pin writes are serialized while the display updates optimistically.
Confirmed saves replay later pending operations; a refused save rolls back only
its own operation. Focus recovery and visibility of the active older
conversation are preserved after unpinning. Focused concurrency and recovery
regressions cover these cases.

### Source access in the current delivery

New Source distinguishes native text, Drive and Notion paths from providers
that need a connector. Configuration saves refresh cadence with project scope.
Live query, collected copy and copy plus live query describe acquisition,
not a broader reader grant. Selected resources constrain fresh provider reads.
Frozen snapshots and external saved variants fail closed rather than query a
broader or newer collection. External retained content and per-file metadata
remain withheld without current-file authorization; provider failures never
fall back to retained external text.

New evidence records native source authority. Network-free reader projections
withhold external, unknown and missing-source citations pending an authorizer.
This does not erase historical conversation text. See [sources.md](docs/sources.md)
for that limitation and the separate reviewed-summary publication path. Source
access and final integration checks remain in the active task queue until their
regressions pass.


### DES-019: Readable project work

The overview uses `apps/web/src/ProjectWork.tsx` for the real paged conversation
records. Rows keep agent, title, last activity and explicit state together.
All, In progress, Needs attention and Finished filters describe the loaded
rows only; when an older page exists, that boundary is stated beside the list.
Failure counts are not described as approval counts or outcomes awaiting review.
Metadata uses gaps rather than separator dots. On phones, status moves below
the title and filters wrap. Supporting notes and schedules share a quiet plane.

`ProjectTasks` in `Projects.tsx` owns the API requests. Initial read failures
offer Try Again instead of appearing as an empty project. Pagination failures
retain the current rows and offer See More again. Responses from a previous
scope cannot overwrite the current list. DOM tests cover filtering and native
conversation selection, failure/retry and obsolete-scope responses.

### DES-020: Shared workspace design previews

The newer consolidated Library, Agents, Notifications and Operations hubs remain
owned by the shared permission-aware navigation definition. This pass does not
restore the older expanded sidebar. The console and Storybook share the native
navigation item renderer and the same semantic controls and fonts.

The [workspace principles](docs/design/workspace-principles.md) and
[reference research](docs/design/reference-research.md) record the adopted
direction and public portfolio credits. `Workspace/Project work` in Storybook
renders production `ProjectWork`, navigation, `Page` and `Tabs` components with
isolated fixtures, the shared renderer font entry and console CSS. Its
workbench mounts the console's own `Sidebar` (New, Search Work and Notifications
above the grouped sections, pinned projects and conversations, conversations
grouped by project with agent and people participants), the phone
`DrawerButton`, and the project header with Lead, breadcrumb, More menu and
every project tab. The sidebar's one conversation-list request is answered from
the story fixtures.
Overview, light, phone, empty, loading, read failure, paging failure, filter/open
and navigation stories exercise the design. Story actions are local previews,
not saved workspace mutations.

Integration evidence: all 619 console tests pass on the committed managed-library
base. The active branch's project-work filter, read-retry and stale-scope tests
also pass, as do console and Storybook typechecks, both builds and 20 light/dark
Storybook browser states with the newer shared typography and controls.

Current delivery checks pass: all 15 workspace typechecks; all nine workspace
test tasks, including 647 console, 18 client, 193 Slack, 58 adapter, eight screen
helper and 46 core tests; the full Go suite with `-race`; Go lint with zero
issues; Biome; API lint with five existing ignored findings; and production
and Storybook builds. Earlier focused calendar/preferences and source-access
regressions also pass. All 74 focused native browser captures pass with zero
layout findings across dark phone/desktop and light 320px/desktop. The complete
CI matrix and merge status are tracked in [PR #99](https://github.com/urmzd/humagen/pull/99).
Live provider-account trials and a production deployment are not claimed here.

```sh
pnpm storybook
pnpm storybook:check
pnpm storybook:capture
```

The capture command uses [teasr.storybook.toml](teasr.storybook.toml). The checker
accepts `STORYBOOK_URL` and `CHROMIUM`. Story actions are isolated fixtures,
not saved workspace changes or live provider execution.

### DES-021: Conversation participants and independent pins

Conversation rows show a bot glyph and the existing colored `AgentMark`s beside
agent names, and circular people avatars with an overflow count. Full participant
lists remain available through accessible labels and titles. Duplicate identities
are shown once; people-only and agent-only conversations keep their own identity.

Project headings and individual conversations each have a pin control. A
project's actions menu also offers Pin Project, including before it has any
recent conversations. Both
appear under Pinned and save independently through personal preferences. The
`pinned_projects` API field holds up to 50 project addresses, most recently pinned
first; the workspace is `~`. New pins require project read access. Existing
unreadable shortcuts stay hidden without blocking changes to other pins. Pin
controls remain visible when pinned, on keyboard focus and on touch pointers.
Save failures restore the previous list and report the failure beside its row.

Evidence: `Sidebar.tsx`, `Projects.tsx`, `preferences.ts`, `pkg/prefs`, the preferences HTTP route
and the regenerated client contract. DOM tests cover participants, pin/unpin,
reopening, concurrent saves and refusal. Store tests cover bounded private lists,
memory restart, older records and write failure. HTTP tests cover unreadable
projects, revoked access and independent conversation pins. The
`sidebar-participants` browser fixture checks save/reload, independent unpinning,
participant bounds and touch targets at 320, 390 and 1280px in dark mode. Its
multi-person roster is a simulated list response; pin persistence uses the native
API. The open drawer uses its own bounds checks rather than page-content gutters.


### DES-022: Conversation lifecycle in the consolidated workspace

Conversation actions offer Pin, Rename, Archive and Delete. Existing participant
identity, project pins and keyboard focus are retained when rows move or disappear.
An archive is hidden from conversation lists and becomes eligible for deletion
after 30 days. Delete requests immediate eligibility; neither action removes
messages before admin approval. Settings > Deletion queue offers Restore and
Approve Deletion, with approval disabled until due. The API requires workspace
governance and confines every row and mutation to the current reader.

The server returns at most 100 queued conversations, due ones first, and the
console refreshes the batch after each review. Approval atomically removes stored
messages and search entries, retaining a hidden tombstone. Run journals retain
their existing history; this feature does not erase journal retention.

Evidence: `pkg/threads/archive.go`, `internal/platform/thread_archive.go`, the
archive/deletion HTTP routes, `DeletionQueue.tsx` and sidebar actions. Store
regressions cover restart and tombstones; API regressions cover admin authority,
eligibility, queue bounds and batch refill. DOM regressions cover explicit
approval, disabled early approval, restoration, refusal and the combined menu.
