# Managed Library and web foundation

The console keeps React, Vite and Tailwind 4. Standard shadcn Radix controls live
in `packages/render-web/src/ui`, below the existing surface-neutral primitive
contract. Legacy button and field helpers consume that foundation. Web-only
resource layouts live in `packages/web`; their hosts own identity, API calls and
persistence. Slack continues using its own renderer.

Humagen's existing semantic colors also supply shadcn's tokens. The shared theme
owns the neutral dark palette. Reading and headings use `IBM Plex Sans Variable`;
file editors and technical text use `IBM Plex Mono`. Both are self-hosted through
the shared font entry, with their licenses retained in the renderer package.
The console loads these files. Compact controls, regular
controls and coarse-pointer targets use explicit sizes. Reduced motion remains
supported.
The default interaction accent is red, with real yellow and green presets.
Saved purple, blue and teal choices remain valid. Neutral light and charcoal
surfaces retain semantic gold attention and green success states; agent
identity hues do not change. Glass and a thin static color streak are limited
to chrome. Content and popovers keep opaque readable surfaces with restrained
borders, bevels and shadows.
`components.json` records the component boundary; upstream source and its license
are recorded in `packages/render-web/THIRD_PARTY.md` and `SHADCN_LICENSE`.

## Navigation and reading order

Library has one sidebar destination and Sources, Skills, Memory and Deliverables
tabs. Agents groups its directory, network, assignments and controls. Operations
groups context health, integrations, policies, schedules, usage and audit.
Notifications has one sidebar destination and one requests-and-updates feed,
with Alert Rules as its permission-aware companion tab. One Filter popover
combines status and type; the default shows all messages. The badge counts
actionable work and always opens Notifications. Native `/inbox` links remain
aliases for the same feed.
The sidebar starts with + New and Search Work, then separates workspace links
from operations. Permission predicates are shared between sidebar destinations
and hub tabs. Existing native addresses continue to work; selections and files
have copied-link routes.

Conversation pins update optimistically while preference writes are serialized.
Each confirmed result replays later pending operations; one refused operation
rolls back only itself. Pinning, unpinning and failure preserve a usable focus
target, and an active older conversation remains visible after unpinning.

Resource browsers use a scope rail when their available width permits it. Narrow
layouts use a scope drawer. Content follows in reading order. A scope change
cannot discard a dirty Skills draft or a Memory correction. The existing
inspector dock still reflows desktop content and uses native modal focus on
narrow screens. Global search remains a centered modal.

Project rows show compact activity and named agent pills. Descriptions and
additional agents sit under Details. Home suggestions stay visible while a
draft is edited; choosing a different project changes their context without
discarding the draft. Announcements have one Open Announcement action and an
always-visible, accessibly named eye-with-slash Hide action.

Profile, Notifications, Privacy and workspace administration share compact
`SettingsFormRow` controls with labels, icons and hints. Privacy uses switch
rows, one save action and a direct Memory link. Preferences describe internal
deployment as ownership of the deployment's data and infrastructure; this
does not change the permissions of a connected provider.

Audit uses the shared Radix/react-day-picker range calendar with explicit local
date/time inputs. The host converts submitted local values to ISO timestamps
and validates a positive range of at most 31 days. Compact event rows reveal
retained details on demand; Refresh, Export and resource links use shared icon
actions. These controls retain native audit permissions and bounded queries.

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

`humagen import skills` writes through the same draft route. A folder the
library lacks becomes a skill's first revision; a folder that differs from
the newest revision becomes one more draft after it, never a published
revision and never a change to a revision already there. Every answer
carries `package_hash`, the snapshot hash over each file's path and content
that a run is pinned with, so a changed supporting file, a rename or a
deletion is told from an identical folder without reading the files. A
revision an import wrote records `via: import`. The import follows only a
newest revision that carries it: one saved in the editor or promoted is
reported as a conflict and left alone unless the person passes `--force`,
and a draft sent after another writer's revision is refused by the
optimistic version check. See the
[CLI reference](../reference/cli.md#import-sessions-skills-and-agents).

Limits are enforced by the server, after base64 decoding: 256 files, 1 MiB per
supporting file, 128 KiB for `SKILL.md` and 2 MiB in total. A supporting file
has room for the schema and source files skills ship; `SKILL.md` keeps the
smaller bound because its body is what a model is asked. The revisions of one
skill hold at most 16 MiB of file bytes together, which is what 32 revisions of
the earlier 512 KiB package came to. A skill's revisions are one stored record
and one history answer, which also hold each file's path and each revision's
text, so they are larger than the file bytes: at worst, with every path and
text field at its bound in characters JSON escapes, a record is about 41.8 MiB
(about 32.9 MiB under the earlier 64-file bound), and a history answer a few
KiB more. A record of real files is near its file bytes in base64. The
arithmetic is at `library.MaxSkillBytes`.

A skill that holds 32 revisions, or 16 MiB of files, takes no more: a draft
past either is refused and names the bound. A skill's revisions are never
removed, and there is no way to free room in a full skill. To go on, save the
skill under another name in its `SKILL.md`, or in another scope, and bind
agents to that skill.

`POST /api/library/skills` takes a body as large as a full package in base64
and 1 MiB besides. Main instructions allow 32000 characters. Relative paths
must be canonical and unique. The in-memory archive adapts saved bytes to saige's
filesystem interface; no uploaded path is opened on the host.

A run started with `POST /api/runs` names skills as a message does: `$name`,
`@skill:name` or `/name` in its text pins the published revision the person
may use where the conversation is filed, and a name that is no such skill
refuses the run with the words that refuse the message. A draft, another
project's skill or another person's own skill is never used.

Existing test, publish, promotion and agent-binding workflows remain. A package
never grants tools through frontmatter. `allowed-tools` can only narrow the
explicit tool selection, which is intersected with agent grants. The admitted
run pins the selected revision, hash and resource manifest. Supporting text can
be read through the existing gated `file_read` tool with `skill:<name>/<path>`.
What `file_read` returns enters the model's context, so it reads a supporting
file of at most 128 KiB: a larger one is listed with its size and the words
"over the 131072 bytes file_read returns", and reading it is refused. A
supporting file's bytes are never put into the turn's text. The turn's text
lists at most 63 supporting files of a skill, as many as a package of the
earlier 64 files held, so the list costs a model no more than it did. The
files `SKILL.md`'s instructions name by their exact path come first, then the
rest by path; a listed file over 128 KiB is marked as above. Past 63 the text
says how many are listed and how many are not, and tells a native model that
`file_read` reads any path the instructions name. `file_read` reads any file
of the pinned package, listed or not, and the directory placed for a provider
whose program reads host files (below) holds every file.

A skill record this release cannot read, such as one a later release wrote
past these bounds, fails only itself: `GET /api/library/skills` lists the
rest of its scope and names it under `unreadable`, it is left out of the
skills a message can name, and every read of it, and a draft after it, is
refused.
A later draft cannot change that read. Binary content, unselected packages,
missing files and cross-tenant references are refused. Reading a script does not
execute it. Private packages retain the existing publication classification.

A provider a program adds may run its turn in a program of its own on the
host, which is offered no platform tool, so `file_read` cannot serve it. No
provider Humagen ships does. For a turn on one, the same pinned package, read in the run's tenant and checked
against the pinned hash, is written to a directory of that turn's own on the
host (`pkg/skillfiles`), and the turn's text names each supporting file by its
absolute path there instead of a `skill:` path. Paths pass the package's own
path validation and are written through a directory handle that follows
nothing out of it. The directory holds every supporting file of the pinned
revision, binary ones included, read only, and is removed when the turn
returns. A pin that names another tenant's package, another version, or a
file outside its package refuses the turn, as does a directory that cannot
be written. A provider declares that its program reads host files
(`skillfiles.HostReader`); every other model keeps `file_read`. The files
are placed when any model of the turn's chain, the one it starts on or a
fallback, is such a provider, and a chain that also holds a native model is
told both the `skill:` paths and the host paths.

The provider lets its program read only the directory placed for the run its
turn belongs to (`skillfiles.Granted`). The run and tenant are the ones the
runtime names on the turn's context, set from the run's record when the turn
starts. Nothing in the turn's text is read for it, so a path a person types,
another run's included, grants nothing.

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

## Sources and current access

New Source distinguishes working native paths from integrations that still need
a connector. Text collections, Google Drive and Notion use existing source APIs;
Snowflake, BigQuery and Slack collection are not implemented by adding their
catalog rows. MCP tool queries remain separate from collected source copies.
Source configuration includes its on-demand or scheduled refresh cadence.

Collection history is durable, but collection authority never grants a reader
access to external files. Provider queries use the reader's current reference
and the configured selected resource IDs. Retained external passages, file
listings and citation projections are withheld pending a per-file authorizer.
Frozen snapshots and saved external variants cannot silently become live
queries. See [source behavior and limits](sources.md).

## Design and delivery references

The remaining delivery and follow-on platform work is tracked in the
[task queue](../plans/2026-10-ui-task-queue.md).
The [conversation-first design](../rfcs/0003-conversation-first-delivery.md) records the
planned collection, browser, protocol and human-reviewed improvement seams.

The foundation follows the [shadcn manual integration](https://ui.shadcn.com/docs/installation/manual)
and current [AI interface patterns](https://ui.shadcn.com/docs/ai-elements).
The original [AI Elements site](https://elements.ai-sdk.dev/) now directs new work
toward shadcn. [Aceternity's timeline](https://ui.aceternity.com/components/timeline)
and [file upload](https://ui.aceternity.com/components/file-upload) informed
interaction exploration. Humagen keeps restrained surfaces and explicit
navigation rather than adopting decorative animation or hover-only expansion.

The Controls and ResourceWorkspace stories demonstrate the shared foundation.
Tailwind motion states cover native drawers, the search modal, nested composer
panes and Radix folders. Retained outgoing shells are inert, reopening cancels
removal, and reduced motion removes travel and removal delays.
`scripts/screens/library.ts` exercises native folder import, saved resources,
download, source editing, memory scopes, entry provenance and correction drawers.
Context Health has one actionable empty state and shows details only after
selection. The policy editor uses compact rows, descriptions, inherited limits,
an explicit scope label and a bounded save action.

Browser fixtures use an isolated local server. The Context Health empty response
is simulated explicitly; populated source states and library mutations use the
native API. These checks do not establish live external-provider behavior.

Agent Skills packages preserve Fiber AgentSpec registration, setup, revisions
and runtime. Unifying AgentSpec with saige definitions and Markdown agent loading
remains the separate work recorded in [the ecosystem plan](ecosystem.md).
