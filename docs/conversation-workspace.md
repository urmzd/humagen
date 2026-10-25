# Conversation workspace

A project is the home for conversations, agents, context and the work they
produce. Conversations remain the place to ask, clarify intent, review an
action and use a result. The web console now brings those interactions into
one page instead of sending people between run history and file downloads.

The [interactive design and visual review](design/humagen-workspace/README.md)
describe the broader direction. The implementation in this change uses the
platform's existing threads, calls, questions, approvals and artifact storage.
It does not introduce another agent execution path.

## The conversation page

Each conversation uses `Page` with a centered title. Its project sits on the
left; Context, People & Access and Conversation Settings sit on the right.
On a phone the title takes its own centered row. Activity and Deliverables
open from the settings control. The sidebar groups conversations by project
and shows the agents that took part beneath each conversation title.

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
