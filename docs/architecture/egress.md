# Egress guard

How a client's writes to Slack and GitHub are kept from repeating a write the
surface refuses. The code is `pkg/egressguard`, the provider rules are
`pkg/integrations/github/refusal.go` and `pkg/integrations/slack/refusal.go`,
and the wiring is `internal/platform/egress_guard.go`.

## The defect it contains

A run owns one message on its surface: a GitHub comment, a reaction, a check,
a Slack message. saige's connector commits each event of the run to that
message's delivery state, marks it pending, and writes it. When the write
fails the message stays pending, and the connector's `Flush` writes every
pending message again. The platform calls `Flush` for every client once a
second, on every replica.

So a write the surface refuses for good was sent again every second for as
long as its row was pending. The row is in the database (`fiber_sink_state`,
`pending = true`), so a restart continued it. A message that was never created
has no handle, and the connector creates a message without a handle on every
event, so each event of the run tried the same create too, four times each
through egress's own retries. On GitHub that volume reaches the secondary rate
limit for content creation, which also answers 403, so the writes kept failing
after the missing permission was granted.

The connector cannot tell a refusal from an outage: it sees an error. The
guard is where that is decided.

## What the guard does

Every call a client's connector makes goes through the guard's HTTP
transport, and the connector keeps its messages in a delivery state the guard
wraps. The provider's classifier says what each answer means.

| Answer | Meaning | What follows |
|--------|---------|--------------|
| GitHub 401, 403 that is not a rate limit, 404, 410, 422 and other 3xx and 4xx on a write. Slack `ok:false` with a code such as `not_in_channel`, `missing_scope`, `token_revoked` | Permanent | Delivery of that message stops. It is recorded, taken off the pending list, logged once and raised in the inbox once per run and client. Later events for it are dropped and it is never written again, by any replica, after any restart |
| GitHub 403 or 429 with `retry-after`, or with `x-ratelimit-remaining: 0` and `x-ratelimit-reset`, or naming a rate limit. Slack 429 or `ratelimited` | Rate limited | Every write of the client is paused until the time given. With no time given the pause is one minute, doubling for each rate limit in a row, up to an hour. Messages stay pending and are written when the pause ends |
| 5xx, 408, 409, a network error, a Slack code the list does not know, any refused read | Transient | The message waits 2s, 4s, 8s and so on up to 5 minutes, with jitter, and is stopped after 20 failures in a row. The client's writes also wait, from a second up to a minute |
| Five refused writes in a row | Circuit open | Every write of the client is paused for a minute. Then one write probes: accepted, the circuit closes; refused, it stays open for twice as long, up to 15 minutes |

While a client's writes are held, its `Flush` is not called, the wrapped
store lists nothing pending and the transport sends no write. Reads are not
held. The numbers are `egressguard.Policy`; `platform.Config.EgressPolicy`
overrides them.

A stopped message is one failure, shown where a failed run is shown: an inbox
message of kind `failure` on the run, whose text is the refusal. The run itself
is unchanged, and its answer is in its session. A held client is reported in
its connection health (`ConnectionHealth`, unhealthy with the reason and the
time writes resume) and in the log.

Log lines, each with `connector`, `tenant` and `client`:

- `egress: write refused, delivery of this message has stopped` (error)
- `egress: rate limited, every write of this client is paused` (warn)
- `egress: circuit open, every write of this client is paused` (error)
- `egress: write failed, backing off` (warn)
- `egress: writes resumed` (info)

## What a refusal says

A refusal keeps the status, GitHub's `message` and `documentation_url` (read
from a JSON body, capped at 300 and 200 bytes, the URL only on a GitHub
documentation host), `x-github-request-id`, `retry-after` and the
`x-ratelimit-*` headers. For Slack it keeps the error code. Nothing else of a
response is read into a log, an error or a record: a body that is not a JSON
object contributes nothing, and anything shaped like a credential is cut out
of the message. The response body handed back to the connector is rebuilt
from those fields, so the connector's own errors carry them and nothing more.

## Limits

- The guard is per client and per process. Each replica learns of a rate
  limit or opens its circuit from its own refused write. A stopped message is
  shared: it is in the state store.
- Two replicas that hold the same pending message at the same moment may each
  send it once before either records the refusal.
- A stopped message stays stopped. Granting the permission delivers the next
  run, not the stopped one.
- The guard sees HTTP. A client whose API the embedding program supplies
  (`Config.SlackAPI`, `Config.GitHubAPI`) or a `Connection` it built is not
  guarded.
- The pending list is read 256 messages at a time, oldest first. Messages that
  are backing off are skipped after they are read, so a page of them delays
  newer ones until they are delivered or stopped.

## What the runtime should do

The guard exists because the connector's delivery engine
(`saige/flow/connectors/internal/sink`) has one policy for every error:
stay pending, retry on the next event and the next flush. These are the
changes that belong there, in saige. Each one retires a part of the guard.

1. Let a surface say what an error means. `Surface.Create` and `Update` return
   a plain error; the engine should accept a typed one (permanent, retry
   after a time, transient) and the HTTP client (`internal/httpjson`) should
   produce it from the status and headers, which it has and discards.
2. Stop a message on a permanent error: clear `pending`, keep the reason in
   its saved state, return an error egress treats as permanent, and expose
   the reason to the host. Today `write` leaves `dirty` set for every error
   (`sink.go`, `write`).
3. Keep a next-attempt time and an attempt count in the saved state, and have
   `Pending` return only messages that are due. Today `Flush` writes every
   pending message on every call (`sink.go`, `Flush`), and `due` returns true
   for every event of a message with no handle.
4. Give `Flush` a per-connector pause the surface can set from a rate limit,
   so nothing is read or locked while it lasts.
5. Bound a pending message's life: a message that has been pending longer
   than a host-set age is stopped, as egress already drops events older than
   its maximum age.
6. Do not copy a provider's response body into an error. Keep named fields.

`TestTheRuntimeConnectorAloneRetriesARefusedWriteOnEveryFlush` in
`pkg/integrations/github` pins the engine's present behavior and fails when it
changes.
