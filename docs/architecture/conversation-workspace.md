# Conversation workspace

A project is the home for conversations, agents, context and the work they
produce. Conversations remain the place to ask, clarify intent, review an
action and use a result. The web console now brings those interactions into
one page instead of sending people between run history and file downloads.

The [interactive design and visual review](../assets/workspace-prototype/README.md)
describe the broader direction. The implementation in this change uses the
platform's existing threads, calls, questions, approvals and artifact storage.
It does not introduce another agent execution path.

## The conversation page

Each conversation uses `Page` with a centered title. Its project sits on the
left; who is in it (Share or People & Access) and Conversation Settings sit
on the right, as compact icon controls with one 4px gap. Share keeps its label
on a wide conversation. Conversation Settings holds the conversation's own
items: Workspace, Context and Activity. On a phone the title takes its own
centered row. The sidebar groups conversations by project and shows the
agents that took part beneath each conversation title.

Activity is the execution detail of this conversation: its runs, calls to
other agents, tool steps and usage. A selected run has its own URL. The
conversation continues to use the thread record as its current placement and
the run ledger as the authority for execution state.

## Five interactions

| Interaction | What the person sees | What an action does |
|---|---|---|
| Message | An author, readable content, and reply and copy actions | Asking an agent or replying prepares the composer; it starts nothing until the person sends |
| Work update | A compact summary of completed steps and steps needing attention | Expanding it reveals the existing shared task components, readable results and optional technical input and output |
| Clarification | A question, mutually exclusive choices and a separate submit control | Selecting an option prepares an answer; submitting sends the existing clarification action |
| Approval | The existing action-specific approval with its scope and preview | Only the approval action authorizes that side effect |
| Deliverable | A saved document, presentation, table or other file with its author, version and provenance | Open, download, select another saved version or prepare a revision request |

A clarification never turns into approval for a launch date, a message send
or another side effect. A completed tool group starts collapsed. A group
waiting for approval or containing an error opens so its next action stays
visible. Waiting on a person does not animate.

Tool execution duration starts at the executing event, excluding the time
spent waiting for approval. Technical input is bounded in the UI model. A
tool's own summary, message or text is shown before structured output; the
original data remains available in Technical details and Copy As.

## Versioned deliverables

The Deliverables inspector lists the artifacts saved in the current
conversation. Metadata, versions and original downloads use the existing
authenticated artifact routes. An immutable artifact record identifies the
selected version. A different conversation's file cannot be presented as
this conversation's result, even when the reader can read both files.

The native preview supports:

- DOCX text and Markdown documents.
- PPTX slide text, ordered through the deck's relationships, with speaker notes.
- CSV tables, including quoted commas, newlines and escaped quotes.
- The first XLSX worksheet, including shared strings, empty columns and cached values.
- Plain text, code, supported images and the existing inline PDF endpoint.

Office previews show content, not the original application's complete layout
or media. The original download preserves those. Preview inputs are limited
to 8 MB, Office XML to 2 MB per part and 16 MB in total, decks to 80 slides,
and tables to 150 rows and 40 columns. A shortened view says so. XML entities
and outside Office relationships are not followed. HTML remains source text;
macros and formulas are not executed.

Present opens the same slide content in a native modal. Arrow keys move
between slides and Escape returns to the inspector. Version, provenance and
slide selections have addresses in `route.ts`.

Request Changes prepares a message naming the artifact's immutable record,
version and, when available, the selected slide. It keeps the person's
unsent words. Sending uses the existing conversation endpoint. A revision
request does not rewrite the stored file; a later agent publication produces
another stored version. Downloading and presenting do not publish to an
outside system.

## Source provenance

Context reads the knowledge sources available through the project's existing
knowledge API. Each source expands into its imported documents with their
origin URI, actual import timestamp and search state. The current store is
indexed, import-based knowledge. The UI does not call it live or invent a
successful connector sync.

The broader design distinguishes indexed copies, live search under a
person's own access and hybrid sources. Connector state, import attempts,
health, source ownership and retry state need first-class backend records
before those modes can be represented as operating integrations.

## Sharing and placement

The project's sharing action uses the existing authorization and concurrency
rules. Its inspector has separate header, scrollable body and footer rows so
required choices cannot sit behind the buttons. Sharing remains an explicit
action by the conversation's starter or an authorized participating owner.

The existing restriction on moving a conversation after runs have been
filed remains enforced by the server. Removing that guard is not a UI change.
The authoritative placement resolver, immutable accounting attribution,
replay and admission-race work is specified in
[Company, personal work and finding help](company-workspace.md). The
interactive design demonstrates that future flow; the app does not claim
that it is implemented.

## Follow-through

The next system work builds on these interactions:

1. Durable assignments tie together a goal, owner, completion criteria,
   trigger, handoffs and outcomes across multiple runs.
2. Versioned conversation placement lets completed private work move into a
   project while preserving accounting, source access and audit history.
3. Source lifecycle records expose connection, import, indexing and live
   query state, with one cause and owner for a blocked dependency.
4. Accepted outputs can be promoted into project knowledge and reviewed
   memory. Publication, Slack delivery and external actions keep their own
   approvals and audience controls.

This allows agents to continue delegated work between human messages and
return when a decision is needed, with usable results and their evidence
kept in the project.


## Authored outcomes and native deliverables

An agent granted `record_outcome` can retain a recommendation, decision or
update, its decisions, and its next action on the run. These fields describe
its result. They grant no approval and are never evidence that an external
action ran. Tool results, calls and the run ledger retain that responsibility.
The shared surface shows the result before expandable work. Redirect prepares
an editable message to the same agent; submitting that message uses the normal
conversation call path. Pending questions and action approvals remain separate.

Citations are retained on the run, up to 64 passages with bounded labels,
titles, addresses and excerpts. Reading a completed run therefore retains its
evidence after older journal events expire. The run's normal read rule applies.

`file_write` with `format: native` accepts a complete JSON revision model:
`title`, `paragraphs`, `slides` (title, body, notes and sources), `rows`, and
`sources`. It creates editable DOCX, PPTX or XLSX files, or a real PDF, in the
existing conversation sandbox. It does not publish them. `publish_artifact`
keeps the ordinary immutable version, hash, provenance and storage limits.
Native Office files retain their revision model, and `file_read` returns it
for precise revisions. `artifact:<id>@<version>` reads only that exact saved
version in the authoritative run's current conversation. A different tenant,
conversation or version answers the same refusal. Scratch-file replacement
never changes published bytes.

The workbench's PDF download renders this exact saved native model through
`GET /api/artifacts/{id}/pdf`, behind `artifacts:read` and the artifact read
rule. It creates no new version and runs no agent. PDF uses Windows-1252 text
and up to 100 pages; unsupported characters and overfull slides are refused
rather than cut or replaced. The original Office download retains Unicode.
Independent validation opened, edited and re-saved generated documents with
python-docx, presentations with python-pptx (including notes), and spreadsheets
with openpyxl. PDF tests use the independent dslipak reader.

A reviewed shared artifact can be added to a manual indexed knowledge source
in the same current project. The inspector requires acceptance of the whole
file, including all notes and source text. It reads the exact immutable version,
not the newest scratch file. Private outputs, broader destinations, external
provider snapshots and oversized text are refused. `AddForScope` guards source
identity and audience under the insertion lock, and an audit attempt records
the copy. Only an opaque artifact/version reference enters provenance metadata.

## The Workspace panel

A conversation is a session with a sandbox of its own, and each agent run in
it works in a nested sandbox (RFC 0004, isolated runs). **Workspace** is the one
place for what the session holds: a pane beside the conversation with a tab
for each open item. It opens from Conversation Settings and from an artifact
clicked in the conversation. On a desktop it is the third pane (sidebar,
conversation, Workspace); **Expand** fills the page with it and **Show Two
Tabs** puts the active tab beside the one before it. On a phone it is a
full-screen sheet with the same tabs.

**+** opens a list (Files, Scratchpads, Artifacts, Sandboxes, Memories), and an
item chosen from a list opens in a tab of its own. A tab closes with its X;
closing the last closes the pane. The open tabs, the active one and the
Expand and split toggles are the pane's address, so a copied link reopens the
same pane: `/conversations/{id}/workspace?tabs=files,file%3Asrc%252Fa.ts&active=file%3Asrc%252Fa.ts&full=1&split=1`
(`apps/web/src/pane.ts`; at most 12 tabs, the oldest closing first). An item
tab is one of:

| Tab | What it shows |
|---|---|
| `sandboxes`, `sandbox:<run>` | The session sandbox (its files, size, retention and whether runs are isolated) and a nested entry per agent run, newest first and indented under its caller: its mounts (`/work` from the session, and the memory trees in `/project` and `/me`), the files it left and the copyback changes refused because someone changed the file first |
| `files`, `file:<path>` | The session's files, editor and change review below |
| `scratchpads`, `scratchpad:<id>` | Named notes beside the conversation |
| `memories`, `memory:<run>` | Read only, by scope: the memory trees the conversation draws on, what the session's runs used and wrote, and each message's run |
| `artifacts`, `artifact:<id>` | The conversation's saved, versioned artifacts with the Deliverables preview, versions, provenance, Present and Request Changes |

A host can add tab kinds it renders itself (`issue`, `pull` and `review` are
reserved) through `Workspace`'s `extraTab` and `extraPicks`. Addresses from
before tabs (`/workspace/sandboxes/{run}`, `/workspace?file=`) open the one
tab they named.

`GET /api/threads/{id}/sandboxes` answers Sandboxes, behind `runs:read` and
the conversation's audience, at most 50 runs. What a run left is recorded on
the run (`Run.sandbox`, at most 64 files and 16 refusals): when an isolated
run's instance is copied back, and, unisolated, when its `file_write` lands in
the session's files. Files keeps its unsaved drafts while another section is
shown or the panel is closed.

## Save as Artifact and diffs

**Save as Artifact** keeps an agent's answer or a person's message (from its
message actions), a scratchpad (from its tab) or a file (from Files) as a
versioned artifact of the conversation, then opens it in a Workspace tab.
`POST /api/threads/{id}/artifacts` (`artifacts:write`, the conversation's
audience) takes `{source: run | message | scratchpad, id, title?}` and goes
through the same publication files take: storage limits, provenance and the
inbox notice. Repeating the same source, title and content returns the existing
artifact, including after a restart or concurrent saves from different
processes. Changing scratchpad content makes the next version under that title.
Deleting a saved artifact permits a new save with a fresh identifier. A saved answer records its run; the person who saved it is its
creator. A structured review (`humagen.github.review.v1`) is kept as its JSON,
`<agent> review.review.json`, so its findings stay addressable by file and
line.

Diffs use one component, `ChangeReview`: side by side on a wide screen and one
column on a phone, with line comments that prepare a message. A file's
**Changes** compares its saved and edited state. An artifact's **Versions**
offers **Compare** on each other text version, at
`?artifact_tab=versions&compare=<id>`, older on the left. `ChangeReview` takes
`pins`, notes under the lines they cite; `reviewPins` (apps/web/src/review-pins.ts)
reads them from a saved review's findings (`path:line` or a permalink's
`#L<line>`), and Deliverables passes them to its comparison, so a review view
can show findings in place.

## Memories by scope

`GET /api/threads/{id}/memory` (`runs:read`, the conversation's audience)
answers three scopes:

- **Conversation**: the memory trees it draws on: its project, then each
  ancestor (read only), and every tree mounted for one of its runs.
- **Session**: by tree, across its runs: how many runs had it, the versions
  installed, the versions written and the proposals made.
- **Message**: each run's trees, the version installed and what it kept
  (committed version, proposals, changes not kept), and the notes drawn from it
  (`By` is the run).

What a run used and wrote is recorded on the run (`Run.memory`, at most 16
trees and 16 proposals each) when its instance is leased (`InstallMemory`) and
released (`FinishMemory`). Unisolated, nothing is installed and the view says
memory reached the run through its context. A tree the reader may not read is
counted in `hidden`, never named; another person's own memory is never named.
Nothing here changes memory: each tree links to the Library's Memory or the
agent's Memory tab.

## Scratchpads

A conversation holds up to 50 scratchpads (`pkg/scratchpads`), each a name of
1 to 80 printable characters, unique in the conversation in any case, and up
to 256 KiB of UTF-8 text. They are kept per request on `pkg/state`, tenant
first, under `/api/threads/{id}/scratchpads`: listing and reading need
`runs:read` and the conversation's audience; creating, renaming, editing and
deleting need `runs:write` and permission to post, in an open conversation,
and name the version read (a stale one is a conflict that keeps the draft).
Unlike files, a run in flight does not hold them.

Agents in the conversation read a scratchpad with `file_read` and the path
`scratch:<name>`, beside `artifact:` and `skill:`, and `file_list` names them.
They are not files in the session sandbox: the database keeps them past the
sandbox's seven-day retention, and a run's copyback can never overwrite a
person's note. An agent cannot change a scratchpad; it writes files.

## Files, scratchpads and change review

Workspace's **Files** section is a workbench beside the conversation.
File, view and conversation selections have copied addresses under
`/conversations/{id}/workspace`. The file browser, open tabs, text editor and
review are web presentation components; the host performs authenticated API
requests and prepares conversation messages.

People and agents use the same bounded conversation scratch directory. Create
files, import UTF-8 code or patches, and keep Markdown scratchpads there. The
editor provides line numbers, indentation, find, wrapping, selected-code
feedback and a visible Save command with Ctrl/Command+S. Drafts stay in the mounted conversation when its
drawer closes. Closing or reloading the page can lose unsaved drafts. Scratch
files are host-local and expire after seven untouched days; publish a deliverable
for a retained, versioned result.

A save names the content hash it read. A stale hash returns a conflict and
preserves the browser draft. File mutations require permission to write runs
and to post in the current conversation. Publication additionally requires
permission to write artifacts. Archived and read-only conversations refuse
writes. Active or waiting runs temporarily prevent human mutations; admission
and copyback guards prevent overwrites. Reads and writes check the current
conversation audience and tenant before opening its directory.

The Changes view compares an unsaved edit with its saved base, or renders an
imported unified `.diff` or `.patch` with file summaries and old/new line
numbers. A line comment prepares a message with its file, side, line, source revision
and a bounded excerpt of the reviewed content, including unsaved edits. The
person reviews and sends that message through the normal composer. Reviewing
a patch does not apply it or post a GitHub review. HTML previews display static
content in an isolated frame with scripts, navigation and network access
blocked. Markdown previews use the existing renderer.

Publish saves the exact selected revision through the existing artifact
storage. **Edit a Copy** reads an immutable text artifact into a new workspace
file. The original version remains unchanged; publishing the edited copy
creates a new deliverable. Native Office and PDF generation continue to use
the existing agent file tools and immutable artifact pipeline.

The interaction references are [Codex's in-thread diffs and comments](https://openai.com/index/introducing-the-codex-app/),
[Claude's editable artifacts beside the conversation](https://support.claude.com/en/articles/17153992-what-are-artifacts-and-how-do-i-use-them),
[Devin's familiar editor controls](https://docs.devin.ai/collaborate-with-devin/vscode-profiles),
and [Cursor's file review and selected-code feedback](https://cursor.com/help/ai-features/agent).
Humagen keeps its existing theme, agent identity, permissions and call path.
