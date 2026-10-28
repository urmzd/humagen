# API contract

The backend is Go. The surfaces that call it (the web console, a Slack app,
later a GitHub app) may be TypeScript. They share one contract:
[`api/openapi.yaml`](../api/openapi.yaml), an OpenAPI 3.2 description of every
route `internal/httpapi` serves. The TypeScript client in
[`packages/client`](../packages/client) is generated from it.

## How the contract stays true

The spec is written by hand, from the code. The tests in
[`internal/httpapi/openapi_test.go`](../internal/httpapi/openapi_test.go) run
with `go test ./...` and fail when the two disagree:

| Test | Fails when |
|------|------------|
| `TestSpecListsExactlyTheRegisteredRoutes` | A route is registered and not in the spec, or the reverse. An operation has no `operationId` or tag, or its `x-permission` is not the permission `Server.Handler` names for the route. Routes are read with `go/types`, so any call of `(*http.ServeMux).Handle` or `HandleFunc` counts, whatever its receiver is called, and one the reader cannot resolve to a constant pattern and permission fails the test. |
| `TestSpecPermissionsAreTheOnesTheServerEnforces` | The running server enforces a different permission than `x-permission` says. |
| `TestSpecSchemasHaveTheFieldsTheGoTypesMarshal` | For the named Go types it lists (about 75), the type marshals a field the schema lacks, or the schema has one the type lacks, or `required` disagrees with `omitempty`. The event union is not the catalog: `EventType` is not exactly `event.Types()`, or `RunEvent`'s `type` mapping lacks a type, has one more, or maps a type to a variant whose `data` is not the body that type decodes to. `TestEventUnionParityCatchesAMissingVariant` proves a missing mapping entry fails. |
| `TestResponsesMatchTheSpec`, `TestEventStreamMatchesTheSpec` | A real response, including 4xx and the event stream, does not validate against its schema, or answers with a status the spec does not list. Every JSON operation must be exercised. |
| `TestResponsesMatchTheSpec` | A documented status code is never produced by a test, unless it is in `unexercised` with the reason. `401`, `403`, `413` and `500` are covered once for every route. |

Validation in these tests is stricter than the published spec: every object
schema refuses properties it does not name. So a key added to a response built
from a map or an inline struct (`getMe`, `getStorage`, a refusal, a manifest)
fails as soon as a test sees it. What is not guaranteed: a property that is
optional in the spec and that no test makes the server send can drift without
notice, and so can a field of a response shape no test reaches.

So a change to a route, a handler's answer or a type's JSON fields is a change
to `api/openapi.yaml` in the same commit, followed by `make client`. A new
event type registered in fiber's catalog needs its variant in `RunEvent` and
`EventType` too.

## Events

Every event is one envelope (`EventEnvelope`: `id`, `source`, `type`,
`subject`, `channel`, `time`, `hops`, and on run events `tenant`, `run`,
`seq`, `agent`, `version`, `digest`) with `data`, the body for its `type`,
as inline JSON. `type` is the only discriminator and carries the version,
such as `run.completed.v1`. `RunEvent` is the union, one variant per type in
the catalog. A type not in its mapping is an `UnknownEvent`: a reader passes
over it and never guesses its shape. The API sends run events only (`run.*`).

`GET /api/events` takes `channel` (a channel prefix), `type` (type prefixes,
repeated) and `run`, and `from` to resume. With none it sends every run event
the caller may read, `run.usage` and `run.context` included. The workspace is
always the caller's. The old `topic` still works and cannot be combined with
the others: `agent:<name>:<run>` is read as `run=<run>`
(`internal/legacy.Topic`). `inbox=true` sends inbox and thread notices alone,
with no run event, and takes no other filter. `GET /api/journal` filters by
`type`, repeated for more than one; the old `kind` is still read when no
`type` is given.

Validation uses [`santhosh-tekuri/jsonschema`](https://github.com/santhosh-tekuri/jsonschema),
a JSON Schema 2020-12 validator with no dependencies of its own, which is the
schema dialect of OpenAPI 3.1 and 3.2. The spec is read with `go.yaml.in/yaml/v4`,
which was already in the module graph.

## Commands

| Task | Command |
|------|---------|
| Regenerate the client after changing the spec | `make client` |
| Build, typecheck and test the client | `make client-check` |
| Lint the spec with Redocly | `make spec-lint` |
| Check the spec against the server | `go test ./internal/httpapi` |

`make client` needs oag at the version pinned in the `Makefile`.
`make oag-install` downloads that release for this machine into
`~/.local/bin` (or `OAG_DIR`) and refuses it unless its sha256 matches the one
pinned beside the version, because oag publishes no checksums of its own.

CI installs oag the same way, regenerates the client and fails if the result
differs from what is committed.

## The generated client

`packages/client` is `@humagen/client`: private, ESM, typed, no runtime
dependencies, for Node 22.12 or later and browsers. Consumers in this repository
depend on it with `"@humagen/client": "link:../../packages/client"` and run
`pnpm build` in `packages/client` first, because the package exports its built
`dist/`.

| Path | What it is |
|------|------------|
| `packages/client/oag.yaml` | The generator's configuration. |
| `packages/client/.oag/packs/node-client` | oag's `node-client` template pack, copied unchanged from oag v0.20.6. Without it oag fetches the pack from oag's main branch. |
| `packages/client/src/generated/` | oag's output. Committed, never edited. |
| `packages/client/src/humagen.ts` | The hand-written layer over it. |

The generated output is committed, as fiber commits `gen/`: a change to the
contract shows up as a reviewable diff in the client, consumers need no
generator to build, and CI can prove the two match.

One method per operation, named by its `operationId`. Path parameters come
first, then the body, then query and header parameters in the order the spec
lists them, then `RequestOptions` (`signal`, `headers`, `timeout`, `retry`).
Each also has a `...Raw` form that returns the status and headers.

The layer in `humagen.ts` adds what the generator does not do:

- `createClient({ baseUrl, token, credentials })`: `token` is a string or a
  function that returns one, sent as a bearer. `credentials: "include"` uses a
  browser's session cookie instead. Retries are off unless asked for.
- `events({ channel, type, run, from })`: the event stream as typed items,
  told apart by `item`: `event` carries a `RunEvent`, `inbox` an
  `InboxNotice`. A `RunEvent` is an envelope whose `data` is the inline JSON
  body for its `type`, so a `switch` on `event.type` narrows `event.data`.
  Leaving the loop by `break`, `return` or a throw, or the stream ending,
  closes the connection.
- `runFilter(run, type?)`: the filter for one run's events.
- `endsRun(event)`: whether an event ends its run on the stream:
  `run.completed.v1`, `run.stopped.v1`, or `run.failed.v1` with
  `data.terminal`.
- `runEvents(run)`: one run's events, to the one that completes it, stops it
  or fails it for good. If the stream drops first, it reads the run's record
  and resumes after the last `seq`, with backoff. It never ends quietly before
  the run's last event: it throws `StreamEndedError` with `reason` `stopped`
  (the record says stopped and its `run.stopped.v1` was no longer on the
  stream), `ended` (the record says completed or failed and no ending event
  was found) or `gave-up` (it could not resume). The last read of a run that
  has ended waits at most `resume.drainMs` (2 s by default) for that event.
- `failureOf(error)`: the status and the API's error body from any error the
  client throws, and `refusal` when a limit refused a run.
- `getArtifactContent` and `getPreferencesPhoto` return the file's bytes. The
  generated versions read the body as text.

## Authenticating a machine

A machine sends an OIDC access token issued for the API audience by
client credentials: `Authorization: Bearer <token>`. It has no role. It may do
exactly what the `humagen:<permission>` scopes on its token allow, and its
workspace comes from the token's tenant claim. Each operation's `x-permission`
names the scope it needs. A Slack surface needs at least
`humagen:agents:read`, `humagen:runs:read`, `humagen:runs:write` and
`humagen:billing:read`, plus the write scope for each inbox action it offers.
The Slack app's Home lists a mapped person's projects, which takes
`humagen:projects:read` too. An app run as a client (`HUMAGEN_CLIENT`) reads its
client and its bindings, which takes `humagen:clients:read`, and catches up on
each bound project's announcements in the project's inbox, which takes
`humagen:projects:read`: without it, release notes, and announcements made
while the app was away, never reach Slack.
See [security](security.md) for setting up the identity provider.

Under `make dev` the server runs with development auth: every request is the
same admin, and no token is needed.

## Example

Start a run under the caller's own idempotency key, stream its answer, and
stop it if it takes longer than a minute:

```ts
import { createClient, failureOf, StreamEndedError } from "@humagen/client";

const client = createClient({ baseUrl: "http://127.0.0.1:8080", token: process.env.HUMAGEN_TOKEN });
const run = await client.createRun({ agent: "helper", text: "Say hello" }, "slack-event-Ev01");
const giveUp = AbortSignal.timeout(60_000);
try {
  for await (const event of client.runEvents(run, {}, { signal: giveUp })) {
    if (event.type === "run.delta.text.v1") process.stdout.write(event.data.text);
    if (event.type === "run.action.taken.v1") console.log(`\n${event.data.summary ?? ""}`);
  }
} catch (error) {
  if (giveUp.aborted) await client.stopRun(run.id);
  else if (error instanceof StreamEndedError) console.log(`\nended: ${error.reason}`);
  else throw new Error(failureOf(error)?.message ?? String(error));
}
```

`runEvents` returns after the event that completes the run, or fails it for
good, and closes its connection however the loop is left. It also yields
`run.usage.v1`, one per model call, and `run.usage.child.v1`; a reader that
does not count usage passes over them, and `getRun` has the run's total in
`usage`. A type the client does not know (`UnknownEvent`) is passed over the
same way. A dropped stream is
resumed. A run stopped elsewhere ends it with `StreamEndedError` and `reason`
`stopped`. `getRun` is the authority on whether a run is over. A refusal by a
limit throws from `createRun`: `failureOf(error).refusal` names the limit, the
policy and when the count resets.
