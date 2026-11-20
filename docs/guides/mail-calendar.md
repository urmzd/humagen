# Read selected mail and approve a calendar change

`examples/mail-calendar/humagen.yaml` runs a native agent, `mail-calendar`,
on the person's own Gmail and Google Calendar connections. These are Google's
Workspace tool servers, which a person connects in **Settings › Connections**
with the `gmail` and `google-calendar` presets
([connections](../reference/connections.md)). The agent is granted
`person_tools`: the tools of the connections that person approved, and
nothing else. No mail send, invitation or event deletion is asked of it.

Before #383 this example used the Codex program's connected apps for Gmail and
Contacts and the Claude Code program's Calendar tools. Those are gone. There is
no Google Contacts preset, so the contacts reader is gone too.

## Configure and start

Google registers no OAuth client for a caller, so the deployment needs one:
create an OAuth client of type Web application in a Google Cloud project,
enable the Gmail and Calendar MCP APIs, add the profile's `redirect_url` as an
authorized redirect URI, and put its client ID in the profile. The redirect
URL is where the console that starts the sign-in is served, followed by
`/connections/callback`; the profile assumes the server serves the built
console at `http://localhost:8080`.

The server does not start without these two, besides a provider key
(`ANTHROPIC_API_KEY`, `OPENAI_API_KEY` or `GOOGLE_API_KEY`):

| Variable | What it is |
|----------|------------|
| `HUMAGEN_CONNECTION_KEY` | A base64-encoded 32-byte key that encrypts consent. Keep it across restarts |
| `HUMAGEN_GOOGLE_CLIENT_SECRET` | The OAuth client's secret |

```sh
humagen config check -f examples/mail-calendar/humagen.yaml
humagen start -f examples/mail-calendar/humagen.yaml
```

The workspace's `run.ceilings.tool_servers` lists `gmailmcp.googleapis.com`
and `calendarmcp.googleapis.com`. Those are the only hosts a person's own tool
server may be on here: the other Google presets are listed as unavailable and
say which hosts are allowed, and Other MCP, which the presets list still
offers, is refused for any other host. Then connect Gmail and
Google Calendar in **Settings › Connections**, sign in with Google, review the
tools each server offers, and approve the list.

## Permission boundaries

- Every tool that its server does not mark read-only waits for the person's
  approval in Humagen before it runs. Reads do not.
- Access is what the person consented to: `gmail.modify` for Gmail, and
  `calendar.events` with `calendar.readonly` for Calendar. The prompt selects
  the message and the event; it does not narrow that access.
- Opened mail enters the model provider's context.
- The profile uses development sign-in and memory state, for a trusted local
  trial: every client reaching it is an administrator.

## Select the message and attachment

```text
Search only EXACT_QUERY, with at most one result, in my connected Gmail.
Confirm it is EXPECTED_MESSAGE_ID before reading it. Open only its selected
ATTACHMENT_NAME. Cite actual identifiers and content, not only a file name.
Do not send or modify mail, read unrelated messages or use Calendar.
```

Whether an attachment's content can be read depends on the tools Google's
Gmail server offers; the agent is told to say when none can. A saved-file
reference alone does not prove the agent read the bytes.

## Approve one exact calendar action

Supply the calendar, title, start and end with an IANA time zone, and whether
to create or update. For an update also give the event ID, the expected
current values and only the fields to change:

```text
Prepare, but do not save, this change in calendar EXACT_CALENDAR_ID:
update event EXACT_EVENT_ID from EXPECTED_OLD_VALUES to APPROVED_NEW_VALUES.
Keep every other field and event unchanged. Show the proposal for approval.
```

When the write tool runs, Humagen asks for approval of that call with its
arguments. After it, the agent reads the event back. Reading before an update
detects an earlier change but cannot prevent a race between the read and the
write; do not treat it as an atomic guarded update.

## What was checked, and what was not

The profile passes `humagen config check` with no variables set, and starts
and stops with throwaway values for the two variables above and no provider
key. On that server, `GET /api/connections/presets` listed `gmail` and
`google-calendar` as available and the other Google presets as blocked by the
host ceiling.

The presets were checked against Google's published server metadata only.
Signing in to a real Google account through them, the tools those servers
offer, reading a message or attachment, and any calendar write have not been
exercised. The earlier checks on 17 November 2026 ran on the removed Codex and
Claude Code paths and say nothing about this one. Contacts, Outlook and another
account are not covered.
