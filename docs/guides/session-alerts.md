# Session alerts

Every new alert belongs to a canonical session. Open alerts raise that session
in the list: Urgent first, Normal next, Low last, then sessions without open
attention. Recency is preserved within each priority. Snoozed and resolved
alerts do not raise a session, and another person's private attention is never
used to sort the reader's list.

`GET /api/alerts` accepts `state`, `priority`, `session`, `mine` and `source`.
The source value matches the alert's `kind`, such as `session`, `approval`,
`clarification`, `connection` or a standing rule's kind. Reads remain confined
to the current tenant and session audience. `POST /api/sessions/{id}/alerts`
creates attention; `PATCH /api/alerts/{id}` resolves, snoozes or reopens it.

Agents need an explicit `alert_create` or `alert_resolve` tool grant and the
initiating person's standing alert-write permission. Creation accepts the
current session ID, priority, title, optional body, action, `resolve_when` and
at most 20 recipients. Recipients must already read the session. Naming someone
never shares a private session. Titles are limited to 500 bytes, details to
10,000 bytes and resolution guidance to 500 bytes. A free-form resolution
condition is guidance, not an executable condition or a grant.

Question and approval alerts resolve only after their authoritative answer is
recorded. A canceled, expired or failed run does not count as an answer, and
reading a session never resolves its alerts. Manually raised alerts require an
explicit resolution, even when the run that raised them completes.

Personal `notifications` settings accept `urgent`, `normal` and `low`, each
with `immediate`, `digest` or `off`. Suggested settings are Immediate, Digest
and Off respectively. Unset settings stay Off; email still requires separate
opt-in and a configured authorized recipient. Quiet hours and current access
are rechecked before delivery. Canonical priority settings replace the legacy
kind setting for session alerts. Older non-session transport rows keep their
kind setting during migration. Internal inbox rows provide compatibility
transport only; reading one does not change canonical attention. Interrupted
transport projection is repaired from the durable alert on a later poll.

Confirmed connection authentication failures create one open reconnect alert
per person and provider, in that person's private Builder session. The notice
contains a fixed service label and reconnect action, never a provider error,
endpoint, token or signing reference. Concurrent processes reuse the alert;
a provider-confirmed reauthorization resolves the matching alert, and a later
failure can raise new attention. A missing
connection or transient network failure does not create reconnect attention.
