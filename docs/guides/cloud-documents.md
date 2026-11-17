# Compare provider revisions and approve a guarded document edit

`examples/cloud-documents/humagen.yaml` uses existing signed-in account
connections on this host. The `history` agent runs on Codex apps to read actual
Drive revision text. The `documents` agent runs on Claude Code's native Google
Docs tools to apply an approved edit with the expected Docs revision. No new
API key is needed when those two programs already have the intended account
connections. No existing document is selected by the example.

## Connect and start

Sign in to Codex and Claude Code on this host and connect the intended Drive
and Docs accounts in those programs. Set both model directories to one existing
absolute work folder, then start:

```sh
humagen config check -f examples/cloud-documents/humagen.yaml
humagen start -f examples/cloud-documents/humagen.yaml
```

The example enables Codex's `apps` feature and ignores its user config; its
local filesystem sandbox is read-only. Claude Code exposes only ToolSearch as
a built-in tool and allows the exact native Docs read/update names in the
file. Its user settings, hooks and account connections still load. Existing
account tools and connected apps are not confined to one document by this
configuration: exact resource selection and approval remain necessary. Opened
content enters the programs' model providers and consumes their account usage.

## Read the requested history

Select `history` and provide the exact Google Doc ID and requested provider
revision IDs. For example:

```text
For Google Doc EXACT_DOCUMENT_ID, list every page of provider revisions and
fetch actual text of requested revisions REVISION_IDS. Compare the text with
the current native document and give its Docs write revision token. Report
missing revisions and unread content. Do not change anything or read another
resource.
```

It uses the app tools `list_file_revisions`, `fetch_file_revision` and
`get_document`. Drive history IDs and Docs write tokens are different values.
A list of revision metadata, a cached local copy or the current document is
not historical content. Provider history can coalesce: an ID seen earlier
may later return 404, and the workflow must report that content as unknown.
It cannot reconstruct a missing provider revision.

## Approve an edit to the same document

Read the comparison, then select `documents`. Supply the exact document ID,
change and expected current Docs write token. The agents do not hand off
automatically; the person passes the selected comparison and approves the
scoped change. For example:

```text
Approved for EXACT_DOCUMENT_ID: replace only OLD_TEXT with NEW_TEXT, using
writeControl.requiredRevisionId EXACT_DOCS_WRITE_TOKEN. Preserve the original
ID, tabs, formatting and all unrelated content. Stop on a stale token or
permission error; do not retry with a fresh token. Read back the changed area
and retained controls once.
```

The exact native tool names are `mcp__claude_ai_Google_Docs__read_doc` and
`mcp__claude_ai_Google_Docs__update_doc`. Discover those names explicitly.
The update takes top-level `writeControl.requiredRevisionId` and native
`requests`. Use the required token to fail on a concurrent change;
`targetRevisionId` allows a merge and is a different operation. Read all
requested tabs before editing their structure. Native range indexes are
UTF-16 code units and requests in a batch execute in order.

## Checked on an approved private fixture

On 17 November 2026, the person explicitly approved one new private folder and
one non-personal native Google Doc. It had four plain-text control lines; no
existing document, sharing permission or other account object was changed.

- Humagen's Claude Code path read the native tab and write token, changed one
  exact marker with that required token, then read it back at the same ID.
- The final combined profile changed only Value: baseline to Value: approved
  with the exact required token, preserving the marker, control line, ID and tab.
  Its history agent then fetched all currently listed revisions 1 and 4.
- An actual stale-token write returned a provider revision-mismatch error.
  It was not retried, and the control text remained unchanged.
- Humagen's Codex-apps path listed the actual provider history and fetched
  revisions 1 and 3: the former was empty apart from a byte-order mark, and
  the latter contained the four current lines. Revision 2 had appeared in
  an earlier list, then was coalesced out; its fetch returned 404. Its old
  contents were reported as unknown, rather than replaced with a snapshot.

The supplied document is a generated fixture in a real connected account.
Paired provider-exported PDF pages were opened around the final Value edit;
the earlier marker edit was not captured as a rendered before/after pair, and
the native Google Docs web interface was not opened.

This does not establish a real user's document, complex formatting, every
historical revision or OneDrive support. Preserve snapshots separately if
exhaustive history is required. A new account still needs its own connection
and consent.

## Connector limits observed

Claude Code's native Drive `download_file_content` exposed a revisionId
argument but actually refused historical downloads with "not yet supported."
The working history path therefore uses Codex's connected Drive app.
Codex's noninteractive cloud write attempt was refused by its approval policy;
the example retains its read-only role and uses the existing permitted native
Docs path for approved guarded writes. It does not disable approval review or
broaden local permissions. A similarly named Claude Docs connector was not
in the allow list and was refused; the explicit Google_Docs tool names avoid
that ambiguous discovery path.
