# Read selected mail and approve a calendar change

`examples/mail-calendar/claude-code.yaml` uses the signed-in Claude Code
program's existing native Gmail and Google Calendar connections. Humagen runs
two agents: `account-reader` has selected read tools; `calendar-writer` has
calendar read/create/update tools. Neither has a mail send, label or delete
tool in its allow list. This is a configured local CLI path, not a native
Humagen Gmail or calendar adapter.

## Connect and start

Sign in to Claude Code on this host and connect the intended Gmail and Google
Calendar accounts there. In the example, replace both `claude-code.dir` values
with an existing dedicated download folder, then run:

```sh
humagen config check -f examples/mail-calendar/claude-code.yaml
humagen start -f examples/mail-calendar/claude-code.yaml
```

The names in the example were discovered from the installed account tools.
`ToolSearch` loads their schemas before use. Do not add `--strict-mcp-config`
with an empty configuration: that would hide the native connections this path
needs. The local program's user settings, hooks and connected servers still
load. The allow list is not an account-object authorization boundary; the
person must trust the local program and authorize exact targets. It uses
account usage and sends opened content to its model provider.

## Select the message and attachment

Give `account-reader` a narrow search or an exact message ID, the intended
account, and the attachment name or ID. Ask it to open only that message and
attachment, cite their identifiers, and distinguish attachment metadata from
bytes it actually opened. If more than one result matches, select one before
reading further. For example:

```text
In my connected Gmail account, find only the message matching EXACT_QUERY.
Show the matching identifiers so I can select one. Do not open attachments,
modify mail or read unrelated messages.
```

After selecting the message, explicitly request the attachment. The discovered
`download_gmail_attachment` schema accepts a message ID and attachment ID,
index or filename metadata, and states a 12 MB limit. Its returned file shape
and attachment rendering have not been exercised here. A returned file name
alone does not establish that the agent read its contents. If a file needs
another reader, download only the selected attachment to the dedicated folder
and use the [local media path](media-evidence.md) for the actual file format.
Contacts, Outlook and another person's account were not checked.

## Approve one exact calendar action

Give `calendar-writer` the calendar ID, title, start and end with an IANA time
zone, and whether to create or update. For an update, also give the event ID,
the expected current values and only the fields to change. Ask for the proposed
values first. Approve that exact proposal before it writes. Invitations and
attendees require their own explicit authorization.

```text
Prepare, but do not save, this change in calendar EXACT_CALENDAR_ID:
update event EXACT_EVENT_ID from EXPECTED_OLD_VALUES to APPROVED_NEW_VALUES.
Keep every other field and event unchanged. Show the proposal for approval.
```

After the approved action, read the event back and compare its ID, calendar,
start/end and fields with the proposal. After an uncertain create response,
read the selected time range before retrying so a duplicate is not created.
The discovered update tool has no expected-version or etag argument. A
read-before-update can detect an earlier change but cannot prevent a race
between that read and the write. Do not claim an atomic guarded update.

## What was checked

On 17 November 2026, the shipped profile, with only directory and port changed,
ran through a real Humagen server and signed-in program. ToolSearch discovered
the exact Gmail search/read/attachment and Calendar list/read/create/update
tools. One deliberately nonexistent, non-personal `rfc822msgid:` audit-marker
query was sent to `search_threads` with page size 1. The provider call returned
an empty object without an error; the trace contains only ToolSearch and that
one search. This does not establish message contents, attachment access, the
account identity or calendar access. No mail or event was changed.

The full acceptance is blocked until the person selects an exact message and
attachment and authorizes one exact calendar change in a connected account.
An example configuration and discovered tool names are not proof of a working
end-to-end account operation. Missing connections, consent errors, unsupported
attachments and an ambiguous target must be reported instead of substituted.
