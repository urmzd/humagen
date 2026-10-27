# Managed Library and web foundation

The console keeps React, Vite and Tailwind 4. Standard shadcn Radix controls live
in `packages/render-web/src/ui`, below the existing surface-neutral primitive
contract. Legacy button and field helpers consume that foundation. Web-only
resource layouts live in `packages/web`; their hosts own identity, API calls and
persistence. Slack continues using its own renderer.

Humagen's existing semantic colors also supply shadcn's tokens. The shared theme
owns the neutral dark palette and the `Figtree Variable` font name. The console
and Storybook load the same font files. Compact controls, regular controls and
coarse-pointer targets use explicit sizes. Reduced motion remains supported.
`components.json` records the component boundary; upstream source and its license
are recorded in `packages/render-web/THIRD_PARTY.md` and `SHADCN_LICENSE`.

## Navigation and reading order

Library has one sidebar destination and Sources, Skills, Memory and Deliverables
tabs. Agents groups its directory, network, assignments and controls. Operations
groups context health, integrations, policies, schedules, usage, alert rules and
audit. Needs You remains a direct action queue. Permission predicates are shared
between sidebar destinations and hub tabs. Existing native addresses continue to
work; selections and files have copied-link routes.

Resource browsers use a scope rail when their available width permits it. Narrow
layouts use a scope drawer. Content follows in reading order. A scope change
cannot discard a dirty Skills draft or a Memory correction. The existing
inspector dock still reflows desktop content and uses native modal focus on
narrow screens. Global search remains a centered modal.

## Skills are immutable packages

A skill folder contains `SKILL.md` and supporting files such as `references/`,
`scripts/` and `assets/`. The [Agent Skills format](https://agentskills.io/specification)
is parsed and snapshotted by saige. Humagen retains its existing skill invocation
name rule: a lowercase letter first, then lowercase letters or digits separated
by single hyphens. This keeps amounts such as `$5` out of the skill grammar.

The editor imports one folder, preserves binary resources, previews text and
Markdown, edits source, adds or removes supporting files and downloads a ZIP.
`SKILL.md` is authoritative for description and instructions. Every save creates
a new immutable draft with optimistic version checks. Old instruction-only
revisions appear as a generated `SKILL.md`; merely opening one does not rewrite
it. Supporting files are omitted from list responses and loaded when a skill is
opened. Binary content is downloaded rather than shown as editable text.

Limits are enforced by the server: 64 files, 128 KiB per file and 512 KiB in total
after base64 decoding. Main instructions allow 32000 characters. Relative paths
must be canonical and unique. The in-memory archive adapts saved bytes to saige's
filesystem interface; no uploaded path is opened on the host.

Existing test, publish, promotion and agent-binding workflows remain. A package
never grants tools through frontmatter. `allowed-tools` can only narrow the
explicit tool selection, which is intersected with agent grants. The admitted
run pins the selected revision, hash and resource manifest. Supporting text can
be read through the existing gated `file_read` tool with `skill:<name>/<path>`.
A later draft cannot change that read. Binary content, unselected packages,
missing files and cross-tenant references are refused. Reading a script does not
execute it. Private packages retain the existing publication classification.

## Memory is a managed projection

A chain folder identifies a real Personal, Company, Project or Agent owner.
Each retained note has a stable Markdown-file address over the native memory
record. Correction, review, expiry, promotion and audited forgetting use their
existing APIs. Original provenance remains projected through current access
rules. These are managed resources over the authoritative stores, not host
filesystem directories or invented relationships.

`GET /api/library/memory` returns authorized folder metadata. Each kind checks
its native read permission before counts are read. Personal ownership is exact,
including against governors and machine credentials. Queries return at most 50
project and 50 agent folders, two Personal/Company folders and one exact selected
owner. Agent discovery scans at most 1000 directory entries. The selected owner
is resolved separately, so searching the rail or reaching a later owner through
a deep link does not erase the current chain. Promotion capability and eligible
destinations are explicit; filtering the rail does not replace that destination
catalog.

Chat With Memory opens an editable private conversation draft through the normal
conversation route. The URL contains a generic question and placement, never
remembered text. Personal chat requires the person's own personal project.
The existing compiler supplies approved, unexpired memory according to current
scope and ReadContext policy. This includes inherited project and selected-agent
context; it is not a strict folder-only reader. No separate model loop is added.

Management states describe actual configuration. Personal learning requires
opt-in and private human turns. Eligible named-project turns produce proposals;
company/root turns can teach the answering agent. Company memory itself is
maintained through reviewed additions and promotions. Missing, offline or
out-of-scope housekeeping agents are shown as unavailable. Configuration is not
displayed as proof that a housekeeping run is currently executing.

## Design and delivery references

The foundation follows the [shadcn manual integration](https://ui.shadcn.com/docs/installation/manual)
and current [AI interface patterns](https://ui.shadcn.com/docs/ai-elements).
The original [AI Elements site](https://elements.ai-sdk.dev/) now directs new work
toward shadcn. [Aceternity's timeline](https://ui.aceternity.com/components/timeline)
and [file upload](https://ui.aceternity.com/components/file-upload) informed
interaction exploration. Humagen keeps restrained surfaces and explicit
navigation rather than adopting decorative animation or hover-only expansion.

The Controls and ResourceWorkspace stories demonstrate the shared foundation.
`scripts/screens/library.ts` exercises native folder import, saved resources,
download, source editing, memory scopes, entry provenance and correction drawers.
Context Health has one actionable empty state and shows details only after
selection. The policy editor uses compact rows, descriptions, inherited limits,
an explicit scope label and a bounded save action.

Browser fixtures use an isolated local server. The Context Health empty response
is simulated explicitly; populated source states and library mutations use the
native API. These checks do not establish live external-provider behavior.
