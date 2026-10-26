# Native console notifications

The notification panel is a delivery projection of a person's authoritative inbox.
Dismissing a notification does not read, resolve or remove its inbox message.

Each event kind has Immediately, Digest or Off. A new person defaults to Off.
Immediate updates appear on the next authenticated console poll, every five
seconds while the console is open. Digests release on the next hourly boundary.
Quiet hours defer both until the configured local quiet period ends, using the
person's IANA time zone. Email is explicitly unavailable.

`GET /api/me/notifications` runs the bounded delivery worker with the current
human Principal. It discovers at most 500 visible unread inbox references per
pass, checking a newest page and advancing a durable backfill cursor. Before
release and every render, it checks current addressing, permissions and `p.Seer`.
It never reconstructs permissions from a directory record or stored sign-in roles.

The durable queue stores message IDs, message-update timestamps, due times,
opaque delivery IDs and acknowledgements. It stores no message body, subject
line, project name or principal claims. Delivery IDs are retained before the
response, so retries and replay return the same ID. A digest groups at most 100
items; a response returns at most 50 visible deliveries. Each person's queue is
capped at 10,000 references and follows workspace inbox retention. Capacity and
persistence failures fail closed, leaving authoritative inbox history intact.

`POST /api/me/notifications/{id}/ack` acknowledges only the caller's currently
visible delivery. A hidden or another person's delivery answers like a missing
one. A pending item loses delivery eligibility when access is revoked, it is
read, it is resolved or its mode is Off.

Console polling evaluates timing only while that person is authenticated. A
digest that became due while the console was closed appears on the next poll.
No background process sends private content with an invented or stale identity.

Private personal-memory context is classified before dispatch in an independent
`context.flags` record and in the immutable private run tag. A failed security
flag write aborts before runtime acceptance. Called and housekeeping runs inherit
the classification. It survives journal expiry and same-ID replay; profile
formatting alone does not receive this classification. Publication uses the
structural marker, never a text search of a prompt.


Personal skill instructions use a generic private-library marker before model
admission. Classification is written through independently of the run journal;
a failed write aborts dispatch. Called and housekeeping runs inherit it, and
same-ID replay retains the original accepted classification. Publication requires
a reviewed summary, without exposing any private skill name or owner.
