# Read a cloud document and approve an edit

`examples/cloud-documents/humagen.yaml` runs a native agent, `documents`, on
the person's own Google Docs and Google Drive connections. These are Google's
Workspace tool servers, which a person connects in **Settings › Connections**
with the `google-docs` and `google-drive` presets
([connections](../reference/connections.md)). The agent is granted
`person_tools`: the tools of the connections that person approved, and
nothing else.

Before #383 this example read Drive revision text through the Codex program's
connected apps and applied an edit guarded by the expected Docs revision
through the Claude Code program's Docs tools. Both are gone. Whether Google's
servers offer revision history, or a write that fails when the document
changed since a given revision, is what their tool lists say when the person
connects. The agent is told to list its tools, say whether either exists, and
report a revision it cannot read as unknown instead of substituting the
current text.

## Configure and start

Set up the Google OAuth client as in
[the mail guide](mail-calendar.md#configure-and-start), with the Docs and
Drive MCP APIs enabled, and put its client ID in the profile. The server does
not start without `HUMAGEN_CONNECTION_KEY` and `HUMAGEN_GOOGLE_CLIENT_SECRET`,
besides a provider key (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY` or
`GOOGLE_API_KEY`):

```sh
humagen config check -f examples/cloud-documents/humagen.yaml
humagen start -f examples/cloud-documents/humagen.yaml
```

The workspace's `run.ceilings.tool_servers` lists `docsmcp.googleapis.com`
and `drivemcp.googleapis.com`. Those are the only hosts a person's own tool
server may be on here: the other Google presets are listed as unavailable, and
Other MCP, which the presets list still offers, is refused for any other host.
Connect Google Docs and Google Drive in **Settings › Connections**, sign in,
review the tools each server offers, and approve the list. Each person
consents to `documents` with `drive.readonly` for Docs, and to `drive` for
Drive. Every tool its server does not mark read-only waits for the person's
approval in Humagen before it runs.

## Read, then approve one edit

Select the exact document by its identifier, and ask first for what the tools
can and cannot do:

```text
For Google Doc EXACT_DOCUMENT_ID, list the tools you have and say whether any
reads revision history or makes a write fail when the document changed. Read
the current document. If a revision tool exists, read revisions REVISION_IDS
and compare them; report any you cannot read as unknown. Do not change
anything.
```

Then approve one scoped change in a follow-up:

```text
Approved for EXACT_DOCUMENT_ID: replace only OLD_TEXT with NEW_TEXT. If a
revision-guarded write exists, use it with revision EXACT_REVISION and stop on
a mismatch; if none exists, tell me before writing. Preserve the identifier
and all unrelated content. Read back the changed range once.
```

Humagen asks for approval of the write call with its arguments before it
runs. A list of revision metadata, a cached copy or the current document is
not historical content, and provider history can coalesce revisions.

## What was checked, and what was not

The profile passes `humagen config check` with no variables set, and starts
and stops with throwaway values for the two variables above and no provider
key. On that server, `GET /api/connections/presets` listed `google-docs` and
`google-drive` as available and the other Google presets as blocked by the
host ceiling.

The presets were checked against Google's published server metadata only.
Signing in to a real Google account through them, which tools those servers
offer, reading a revision and any edit have not been exercised. The earlier
private-fixture checks of 17 November 2026 ran on the removed Codex and Claude
Code paths and say nothing about this one. OneDrive is not covered.
