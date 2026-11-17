# Read selected mail and approve a calendar change

`examples/mail-calendar/humagen.yaml` uses existing signed-in Codex apps for
Gmail and Contacts reads, and Claude Code's native Google Calendar tools for
an explicitly approved event action. No mail send, invitation, contact write
or event deletion is part of this example. These are local CLI account
connections, not native Humagen Gmail or calendar adapters.

## Connect and start

Sign in to Codex and Claude Code on this host and connect the intended accounts
in those programs. Set both model directories to an existing absolute work
folder, then start:

```sh
humagen config check -f examples/mail-calendar/humagen.yaml
humagen start -f examples/mail-calendar/humagen.yaml
```

Codex enables `apps`, ignores user config and uses a read-only local filesystem.
Claude Code exposes only ToolSearch as a built-in tool and permits the exact
native Calendar names in the profile. Its user settings, hooks and account
connections still load. Neither model configuration confines cloud access to
one account object: explicit target selection and approval remain necessary.
Opened content enters the model providers and consumes account usage.

## Select the message and attachment

Choose `account-reader` and give a narrow search or exact message ID, intended
account and attachment name. For example:

```text
Search only EXACT_QUERY, with at most one result, in my connected Gmail.
Confirm it is EXPECTED_MESSAGE_ID before reading full MIME. Open only its
selected ATTACHMENT_NAME using the attachment ID from that message. Cite
actual identifiers and content, not only a file name. Do not send or modify
mail, read unrelated messages or use Calendar.
```

The Codex app path uses `search_email_ids` or `search_emails`, `read_email` and
`read_attachment`. Select the returned attachment by its actual message and
attachment ID. Its supported rendering and size depend on the tool and file
format; a saved-file reference alone does not prove the agent read the bytes.
For an unsupported attachment, download only that selected file to the work
folder and use the [local media path](media-evidence.md) for its actual format.

## Read a selected contact

Choose `contacts-reader` and give an exact name/query or selected resource ID.
The connected app exposes `search_contacts` and `read_contact`. Read only the
selected person after resolving an ambiguous match. This connector exposes
no create/update/delete tool here. Contact writes need a write-capable People
or Contacts MCP connection and an exact authorized change; this example does
not invent that capability.

## Approve one exact calendar action

Choose `calendar-writer` and supply the calendar ID, title, start and end with
an IANA time zone, and whether to create or update. For an update also provide
the event ID, expected current values and only the fields to change. Review
the concrete proposal before approving it. Invitations, guests, notifications
and reminders need their own explicit authorization.

```text
Prepare, but do not save, this change in calendar EXACT_CALENDAR_ID:
update event EXACT_EVENT_ID from EXPECTED_OLD_VALUES to APPROVED_NEW_VALUES.
Keep every other field and event unchanged. Show the proposal for approval.
```

The native tool prefix is `mcp__claude_ai_Google_Calendar__`; discover the exact
`get_event`, `list_events`, `create_event` and `update_event` names. After the
approved action, read it back and compare its ID, calendar and fields. After
an uncertain create response, check only the selected time range before retrying
to avoid duplicates. The discovered update schema has no expected-version or
etag field. Read-before-update detects an earlier change but cannot prevent a
race between read and write; do not claim an atomic guarded update.

## What was checked

On 17 November 2026, these existing connectors ran through real Humagen servers
and signed-in programs, with non-personal fixture content only:

- One unsent Gmail draft with no recipients was created, with the generated
  `req026-fixture.csv`. Nothing was sent. Codex searched only its exact subject,
  confirmed the selected message, read full MIME, then opened the actual
  127-byte attachment. All five original CSV fields matched the local fixture;
  the attachment renderer also showed an added index column, not a source
  column. The draft remains available for peer review.
- Claude Code's native attachment tool also opened those bytes, but its
  `get_message` call returned a permission error. The cause is unconfirmed;
  the working message path uses the connected Codex Gmail app instead.
- One exact nonexistent Contacts audit marker returned zero matches. No real
  contact was opened or modified. No contact write tool was exposed.
- Calendar tool schemas and primary-calendar metadata were discovered. No
  event was created or changed. A concrete private historical fixture proposal
  awaits explicit approval, so the calendar-write acceptance is still blocked.

No original personal message or appointment was used. Contacts writes, Outlook,
another account and the console flow were not checked. The full requirement
is not closed by the working mail/attachment path: the remaining asks are an
exact approved Calendar action and, for contact writes, a write-capable connector
plus the selected contact and change.
