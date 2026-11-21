# Reusable environments and private session credentials

Status: proposed. No credential prompt, masked browser operation or reusable
environment described here is implemented by this RFC.

## Goal

A person creates a named environment in a workspace, selects it in sessions
across projects and supplies only the access each session needs. A native
workspace environment is a first-class choice: using this feature does not
require a custom image. An environment is a reusable definition, not a running
machine, a grant to a project, or a container of shared personal secrets.

The default credential scope is **this session**. A person may explicitly bind
one of their credentials to one project for their own future sessions. Reuse of
a definition in another project never carries that project binding with it.
Workspace operators still control isolation, permitted images, tools, egress
and credential storage. No choice in an environment widens those ceilings.

## What exists

- [RFC 0004](0004-isolated-runs-and-person-context.md) separates a run's
  agent, project, person and sandbox, and requires permission intersection and
  clean isolation. `internal/platform/isolation.go` currently leases an
  instance per turn and releases it at the turn's end; `runFiles` copies the
  conversation's scratch files between instances. A reusable definition or
  session credential must not be mistaken for a session-long sandbox.
- `pkg/personctx` keeps a person's MCP definitions and approved tool schemas.
  `internal/platform/credentials.go` resolves person-scoped provider access
  for the run's recorded actor, and `pkg/credentials` keeps references or
  write-only values per tenant, person and provider. The existing value store
  can keep plaintext in Postgres; it does not establish encryption at rest.
- `internal/mcptools/secret.go` masks a credential when formatted and redacts
  its known value from MCP text, errors and logs. This is not a general
  browser, terminal, screenshot or arbitrary transformed-output secrecy boundary.
- [PR #475](https://github.com/urmzd/humagen/pull/475), merged, added session
  setup, an environment choice and a session panel.
  [PR #486](https://github.com/urmzd/humagen/pull/486), still open, proposes
  workspace images pinned by digest and an image picker. It is active work, not
  a prerequisite that has landed. #486's browser is an HTTP tool server outside
  the image. Neither PR supplies this contract.
- `internal/platform/questions.go` and the runtime answer path persist ordinary
  questions and answers. They must never receive a secret as answer text.

## Definition and scopes

A workspace member with permission to create environments can create, name,
edit and delete one. It has a tenant, stable ID, owner, revision and bounded
metadata. It names the deployment's native/default runtime, or an image that
workspace policy permits; optional connector/tool references; and required
credential descriptors such as a name, purpose and permitted destination. It
contains no credential value, browser profile, live process, project data or
unrestricted environment variables. Workspace policy decides who may use or
administer a shared definition. The owner may reuse it in any project they can
access in that workspace; another member may use it only when shared and
otherwise authorized. Tenant is always derived from the principal, and another
tenant's ID returns 404.

The session pins the chosen definition revision and a resolved image digest
where applicable. Later edits affect new sessions, not an existing session's
meaning. On each use, access is checked again against current workspace and
project membership, provider grants, tool schemas, egress ceilings, credential
ownership and revocations. A snapshot is not a continuing authorization. An
unavailable or revoked dependency stops the affected action with a clear reason;
it does not silently substitute a different environment or another person's
credential. Existing sessions without a selection use the deployment default.

A secret binding has exactly one owner and one scope:

| Scope | Key and lifetime | Availability |
|---|---|---|
| Session, default | tenant, session, acting person and binding; deleted at explicit session end or deletion, with a bounded expiry for abandoned sessions | That person's authorized tools in that session only |
| My project, explicit | tenant, project, acting person and binding; persists until revoked, membership is lost or retention expires | That person's authorized sessions in that project only |

The UI must say the actual expiry and whether a value is kept; a session
finishing a single turn is not session end. Archive alone must not be described
as deletion. The operator defines a bounded maximum session retention and a
cleanup job; expire no later than that deadline even after worker or server
failure. Revocation prevents future use immediately, including of any browser
state the binding produced, and deletes the value and binding, including on
project membership removal, session deletion and owner removal. Cleanup must be
idempotent and work across replicas. A project binding is an explicit choice,
never the default, and must be separately revocable.

For shared roots and sessions, project files and other shared resources retain
their existing access rules. A connector or vault action acts only as the
recorded person whose message or run requested it, never as the root creator,
environment owner, another participant, or a machine with no mapped person.
A called run is treated two ways today. For a workspace provider with
person-scoped credentials it acts for nobody
([security, not built yet](../architecture/security.md#not-built-yet); the
actor rules in `internal/platform/credentials.go`). For a person's own tool
servers it does not: a called run's context keeps the caller's person and tool
servers (`Narrow` in `pkg/runctx`), and `personalToolGroup` in
`internal/platform/person_tools.go` connects with that person's credential when
the called agent holds `mcp:person_tools:write`, without checking whether the
run was called. This RFC does not build on that second path.

The rule for a binding is the stricter one. A called run does not use its
caller's person for a connector or vault action, so a binding needed inside a
called run fails closed with a reason and creates no request. Letting a called
run use a person's binding is out of scope and needs its own rule: consent from
that person for each call, naming the called agent, tool and destination. The
two existing paths must follow one rule before bindings ship; step 1 settles
which, and step 4 tests the called-run case. `ActorOf`, `personOf`,
`ownCredentialOf`, `steeredFor` and `personCredential` in
`internal/platform/credentials.go` are the existing identity and steering
boundaries to extend and verify. A shared thread must not make a private
credential or its use available to other readers.

## Missing credentials and use

Before a supported tool uses a required binding, the trusted host resolves the
actor, session, project, exact tool and destination and checks all grants. If
no usable binding exists, park the action and create a **metadata-only**
request addressed to the acting person: what is needed, the tool and destination,
the requesting session and available scopes. Nobody without that identity can
supply or inspect the request. A machine, a called run or any other unmapped
actor fails closed with a reason, rather than asking the room or borrowing an
identity.

The value is submitted to a separate authenticated write-only endpoint over a
protected connection, bound server-side to the pending request, actor, session,
destination and expiry. The response, ordinary question answer, event, audit
record and run ledger contain only opaque IDs and status. The value never goes
into a model prompt or tool arguments, URL, logs, terminal history, workspace
files or a normal question. Check request ownership and current authorization
again at submission and immediately before use. Reject stale, answered, expired,
cancelled, mismatched and revoked requests. The secret store must support
bounded writes, atomic one-time binding, idempotent retries, deletion and
multi-replica recovery; a durable record cannot claim success before the value
is available.

A person's durable binding is never kept in plain text. The backend encrypts
values at rest with a key held outside the database, or keeps them in an
external vault. The current value store does neither: a stored value is in the
database as written ([security](../architecture/security.md)). Writing that
limit down does not make it acceptable for a binding. Until such a backend
exists, a durable binding stays behind `workspace.person_credentials.values`,
which is off by default, and a session binding is held only for the session.

On success, resume the parked action once. A retry must not run it twice.
Decline, cancellation, expiry, a failed store write, lost authorization or an
unavailable worker leave the action stopped and give the owner a recoverable
status. Never fall back to a different credential. Audit may record who supplied
or revoked a binding, scope, request ID and outcome, but no value or sensitive
reference. Rotate and revoke access without revealing prior values.

## Trusted injection and browser boundary

Only a trusted connector or execution gateway may resolve a binding, at the last
possible moment and only for the authorized tool and destination. Reuse and
extend the `internal/mcptools` secret wrapper and redaction where applicable;
do not expose a value to the agent's general shell or process environment. The
existing `runInstances` lease, shell, file copy-back and browser MCP do not
currently provide this guarantee. Any saige sandbox or gateway capability
needed here belongs upstream, composed through its interface rather than a
parallel runtime.

Browser form filling needs a distinct trusted operation. The model may request
a binding by opaque identifier and a specific permitted origin, frame and form
field; it cannot pass or retrieve the value. The trusted worker verifies the
current page, redirect, origin, frame, binding, actor and scope at execution,
then fills the field without sending the value through the model-facing tool
call. Snapshots, accessibility trees, screenshots, recordings, traces, error
messages and tool results must mask the field and prevent a secret-bearing
response from crossing the boundary. Disable or fail closed for any browser
worker that cannot enforce those protections, including after navigation or
frame changes. Exact-string redaction is defense in depth, not proof: a page,
script or destination receives the value and may transform or exfiltrate it.
The interface must identify the receiving destination and never promise that
the destination itself cannot see the value.

A fill also leaves signed-in state in the browser: cookies, storage and open
pages. That state is a use of the binding and has the binding's owner and
scope. The existing browser is the workspace's, not the session's
([browser guide](../guides/browser.md)): any agent holding the provider's grant
reaches the same pages, and so does a person acting by hand through
`internal/platform/browser_view.go`. A masked fill therefore runs only in a
browser context isolated to the binding's owner and the session that used it,
which no other actor, session or by-hand view can reach. A project binding is
filled again in each session rather than carrying signed-in state between them.
Destroy the context and its storage at session end, expiry and revocation. A
worker that cannot isolate and destroy the context fails closed.

## Interface and lifecycle

Use the session setup and panel patterns #475 put on main, and #486's image
picker once it settles.
The environment selector lists the native default and authorized named
environments, not images as if they were environments. A management view creates,
edits and shares a definition and shows its revision, runtime and required
connector names without values. The session panel shows the selected revision,
current session bindings, each connector's actual availability and scope, and
an owner-only revoke action. It must not imply that the definition grants access.

A missing credential appears as a private waiting state with the tool,
destination and scope, a masked input, **Supply and resume**, and **Decline**.
Session scope is selected by default; project reuse requires an explicit choice.
Show submitting, waiting, resumed, declined, expired, revoked and error states.
Keep entered values out of rendered transcript and browser storage; clear the
input on completion or cancellation. Only the owner sees the prompt; other
participants see a neutral waiting state without private details. The UI must
remain useful by keyboard and at phone width, and use the existing shared
controls and design tokens. The screen is a proposal until its trusted path is
implemented and visually verified.

## Sequence and acceptance

1. Reconcile #475's session selection, now on main, and #486's pinned image API
   with this definition; keep native/default available. Put a called run's use
   of a person's own tool servers and of a binding under one rule. Add versioned, tenant-scoped
   definitions and principal-authorized selection, OpenAPI and generated client.
   Test cross-tenant access, sharing, project membership, pinned revisions,
   revocation, legacy sessions and image policy.
2. Add private binding metadata and a secret backend that encrypts values at
   rest with a key outside the database, or uses an external vault, with
   explicit retention, deletion and replica-safe atomic operations. Refuse a
   durable binding when the backend offers neither and
   `workspace.person_credentials.values` is off. Test session expiry, explicit
   project reuse, owner isolation, removal, concurrent writes, crash recovery,
   stale requests and revocation. Exercise both memory and configured durable
   stores; do not expose secrets through read APIs.
3. Add a metadata-only parked request and dedicated write-only supply route,
   with recorded actor and exactly-once resume. Check the route's permission in
   `internal/httpapi/server.go`, derive tenant from the principal, and register
   any new event's sensitivity, renderer and OpenAPI mapping. Test denial,
   cancellation, replay, retries and shared-thread privacy.
4. Add scoped connector injection and a separately verified trusted browser
   operation, including any saige changes. Test a canary secret against model
   input and output, tool arguments and results, logs, errors, audit, ledger,
   API responses, terminal, files, DOM/accessibility snapshots, screenshots,
   recordings and traces; include echo, encoded/transformed output, redirect,
   wrong origin, unauthorized actor and called run cases. Test that another
   actor, another session, by-hand browser access and a use after session end,
   expiry or revocation cannot reach the signed-in state a fill leaves. If a
   boundary cannot enforce the invariant, reject the operation instead of
   declaring it safe.
5. Build the owner-only interface and exercise the full waiting, supply,
   decline, revoke and failure flow in light and dark at desktop and phone
   widths. Capture matched before/after screenshots and inspect them; document
   the browser and vault configurations actually tested.

These steps are a proposed contract, not evidence that the existing runtime,
credential store or browser already enforces the guarantees.
