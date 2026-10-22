# Security

How the platform decides who is calling, what they may do, and what it refuses.
Report vulnerabilities as described in [SECURITY.md](../SECURITY.md).

## Callers

| Caller | How it authenticates | What it gets |
|--------|----------------------|--------------|
| A person in a browser | OpenID Connect authorization code flow with PKCE, run by the server. The browser holds an opaque session cookie and never a token. | Roles, from the policy's grants |
| A person through Tailscale Serve | Serve identity headers from a loopback peer at the configured HTTPS host | Roles, from subject or anyone grants |
| A person in a CLI | An access token as `Authorization: Bearer` | Roles, from the policy's grants |
| A machine | A client-credentials access token as `Authorization: Bearer` | Exactly the scopes on the token |

Every caller becomes a `Principal`, and every route asks it one question:
`Can(permission)`.

## Roles and scopes

Roles and who holds them are set by an [authorization policy](authorization.md).
The built-in one declares these four, and the group flags grant them:

| Role | Permissions |
|------|-------------|
| `viewer` | `agents:read`, `runs:read`, `schedules:read`, `alerts:read`, `knowledge:read`, `evals:read`, `billing:read`, `artifacts:read`, `clients:read`, `projects:read`, `people:read` |
| `operator` | viewer, plus `runs:write`, `schedules:write`, `alerts:write`, `evals:write`, `artifacts:write`, `projects:write` |
| `manager` | operator, plus `agents:write`, `knowledge:write`. Writing agents covers their call policies, so a manager is the natural approver of calls between agents |
| `admin` | manager, plus `billing:write`, `clients:write`, `projects:govern` |

Writing knowledge is an admin permission because a document an agent is grounded
in changes what that agent says. Writing clients is one because a client hands
the workspace's agents a credential to act with outside it.

Governing projects is an admin permission because the workspace itself and its
company projects carry instructions and notes sent to every agent answering
under them, and because reusing the name of a deleted project adopts the
conversations still filed under it. `projects:govern` grants nothing on its
own: every project route checks `projects:read` or `projects:write` first, and
govern is asked for on top, for the workspace, a company project, its notes, or
a deleted name. A deleted name's tombstone is never forgotten while
conversations or files are still filed under it, so later deletes cannot clear
the way to adopting them. Its notes are asked the same through every route that changes
them: the project's memory routes and the inbox. The agent memory routes reach
an agent's own notes only, and answer 404 for a project's memory owner
(`project:<name>`, and `project:` for the workspace). Projects have no roles yet, so anyone holding `projects:write`
changes every other project, and every project is visible to everyone holding
`projects:read`.

A machine whose token names its scopes, such as the Slack app or CI, needs
`warp:projects:read` to list projects and `warp:projects:write` to make them:
`runs:*` no longer covers them.

Which agents may run where is a policy the platform enforces, never an
instruction in a prompt. A run is refused at admission when its agent is not
available in the project it is filed in, whatever started it, and an agent
that lives in a project is not available in the workspace itself or in any
project outside its home's subtree. A message that tags no agent goes to the
default its place inherits, and with none nothing runs: nothing routes on its
own. Setting the workspace default changes who answers everyone's untagged
messages, so it needs `projects:govern`; to keep the old split where managers
set it, add `role:manager` as an owner of the workspace once project roles
exist, or grant them `govern on projects`.

The directory (`people:read`, which every built-in role holds) shows each
person's name, handle, roles and picture to everyone in the workspace. No
email address is asked for or shown, and a name that is an address is not
kept as a name. A subject is kept as the identity provider gives it, since it
is the key a person signs in with, and can hold an address: a Tailscale login
does, and so can the subject a Slack people map names. The directory leaves
such a subject out and never searches subjects. A picture is only one a
person uploaded themselves, served by warp: reading the directory sends
nobody else a request. People are recorded when they make a request holding
any permission, when a client maps a Slack person to them, and when the
policy grants a role to their subject, unless tenants come from a claim: a
grant names no tenant, so it puts nobody in a directory they may not belong
to. A machine is not a person. A policy that should not list someone grants
them no role.

A webhook from Slack or GitHub is not a `Principal`. It is authorized by the
signing secret of the [client](clients.md) it is for, and takes its tenant from
that client. No client secret is returned by the API or written to a log.

A client may reference a secret in the server's environment instead of writing
its value. The environment is shared by every tenant, so a reference is read
for a tenant only if the operator listed that variable for that tenant with
`-secret-refs`. A tenant not listed may reference nothing. The check is made
where the secret is read, and a refusal is the same whatever the reason. See
[clients](clients.md#which-variables-a-workspace-may-reference).

A machine has no role. It is issued scopes named `warp:<permission>`, for
example `warp:runs:write`. A scope without the `warp:` prefix, or one that names
no permission, grants nothing.

## Rules the code holds

- **Default deny.** The zero `Principal` can do nothing. An unmapped group, an
  unknown role and an unknown scope each grant nothing. A deny in the policy
  beats every allow.
- **A bad policy stops the server.** One that does not parse or compile is an
  error at start, never at decision time.
- **Tenant is never taken from a request.** It comes from the deployment or a
  verified token claim. A body that carries a `tenant` field is rejected.
- **Another tenant's objects do not exist.** Reads, deletes, calls and stops
  across tenants return 404, never 403, so a response cannot confirm that
  something exists. Each workspace has its own project tree and its own root,
  `~`: a project name another workspace uses is not found, as a filter, a
  parent or a limit's scope.
- **Nothing is filed under a project being deleted.** A schedule, an alert
  rule or a limit is written while its project is held, and a delete is
  refused while any of them points at the project, so a delete never leaves
  one naming a project that is gone. A delete never drops a limit: only
  `billing:write` removes one.
- **A run id carries its tenant.** The runtime derives it from the tenant and
  the tag id together, so a trigger's id, chosen outside, cannot name or join
  another tenant's run. A stop request is honoured only by a run of the tenant
  it was made under, and the platform only makes one for a run the caller's
  tenant holds.
- **A run's trigger id is visible to its workspace.** `tag_id` on a run is the
  id it was accepted with: a Slack or GitHub event or delivery id, a schedule
  occurrence, or for the API `api-<length of subject>-<subject>-<key>`, which
  includes the subject of whoever started it and the `Idempotency-Key` they
  sent. Anyone with `runs:read` in the workspace can read it. Do not put
  secrets in an idempotency key.
- **An ID token is not an API credential.** Bearer tokens must be issued for
  the API audience. A token issued to the web client is refused.
- **Sessions are server-side.** The cookie is a random 256-bit id, `HttpOnly`,
  `SameSite=Lax`, and `Secure` with the `__Host-` prefix whenever the public
  URL is https. Signing out deletes the session on the server.
- **Sign-in is bound to the browser that started it.** State is checked against
  a cookie, the nonce is checked in the ID token, and the code exchange carries
  the PKCE verifier.
- **Redirects stay on this site.** `return_to` must be a local path.
- **Cross-origin writes are refused** before they reach a handler, using Go's
  `http.CrossOriginProtection`.
- **A surface answers only for people it was given.** A Slack app built on the
  API answers a question, or asks for a run, for a Slack person (`act_as`) only
  through a Slack client that names the app's own credential as its `machine`
  and maps that person in `people`. Anyone unmapped is refused (403) and the
  app does not ask. To answer, the mapped person's own grant (what the policy
  grants their subject) must include `runs:write`, as well as the
  credential's. The answer is recorded as the mapped principal, with the
  credential as `Via`: a credential never approves a call in a run it raised
  as itself, whoever it names. Mapping a person is `clients:write`.
- **A person posts only as themselves.** A message in a shared thread is
  the caller's, or, for a credential, the person its client maps through
  `act_as`, who must hold `runs:write` themselves; a credential acting for
  nobody is refused (403). No route takes an author or an agent to speak as,
  and nothing writes an agent's turn but its run. A shared thread is read by
  everyone with `runs:read`: private threads are refused (409) until every
  read path is confined.
- **The machine holds its people's votes.** Whoever holds the credential a
  Slack client names as `machine` can answer every question as any person the
  client maps, within what each may answer, and raise runs as them, which
  use the tools that act as them with their own credentials. The Slack
  app is trusted to say truthfully which Slack user clicked; the platform
  cannot check it. A mapped person's roles are what the policy grants their
  subject: a click carries no groups, so a role held only through a group
  does not count, and such a person cannot answer from Slack. Keep that credential as you would those people's sign-ins,
  map only people who use that Slack workspace, and remove a mapping when
  someone leaves.
- **A tool that acts as a person uses that person's credential and no
  other.** A person-scoped tool provider names no shared credential, so none
  is there to fall back on, and a run for someone who connected nothing gets
  no tools from it. See [Tool credentials](#tool-credentials).
- **Who a run acts for is decided when it is accepted.** It is the signed-in
  person, the person a surface asked for (`act_as`), or the person a Slack
  client maps the connector's Slack user to, written on the run's record. A
  trigger's tag cannot name anyone itself, and nothing a turn says changes
  it.
- **Lineage cannot be forged.** Only the platform's call path mints a tag with
  origin `agent`. A trigger's tag claiming it is dropped.
- **An agent calls only who its spec allows.** The caller's pinned revision
  must list the target in `access.spawn`, then depth, fan-out and budget are
  checked. The zero limits allow no calls.
- **Conversation keys are believed only from the API.** A run is placed in a
  thread, a branch or a project only when the platform's own API raised it. A
  trigger's tag carrying those keys is a conversation of its own.
- **Continuing a conversation checks the tenant.** `after` must name a run in
  the caller's tenant, or the request is a 404.
- **Preferences are the caller's own.** Those routes take no subject, so there
  is no way to name someone else.
- **A profile picture is what its bytes say it is.** Only PNG, JPEG and WebP are
  accepted, by signature, up to 512 KB. It is served with the type the server
  determined, `nosniff`, and a sandboxing content security policy. SVG and HTML
  are refused.
- **Reserved origins cannot be claimed.** A trigger's tag saying it came from
  the API, the platform, an eval, a person's run-now or an agent is dropped.
- **Housekeeping output is untrusted.** A title or a memory note is a model's
  words about text a person supplied. A title is cut to one line of 80
  characters. A note is one line of at most 300 characters, at most three per
  turn, stored as plain text that is shown on the agent and can be removed.
  A housekeeping run never starts another.
- **An agent's memory never displaces a person's note.** When an agent's notes
  are full, only notes the memory agent wrote make room.
- **A proposed memory is not a memory.** What the memory agent writes is held
  as pending and is not sent to any agent until a person approves it, unless
  the workspace turned approval off. This is saige's default for memory writes.
  What it writes for the workspace or a company project waits for someone
  holding `projects:govern` even then: the turn that taught it needed only
  `runs:write`, and turning approval off needs only `agents:write`.
- **Memory refuses personal data.** Every note, from a person or an agent,
  passes saige's memory policy, which rejects text its detector finds personal
  data in.
- **Memory is never drawn from a person's own credential.** The memory agent
  is not asked about a turn whose conversation acted for a person with a
  person-scoped tool ([Tool credentials](#tool-credentials)).
- **Project memory reaches only that project.** A turn is sent a project's
  shared notes only when its conversation is filed in that project.
- **Knowledge stays in its tenant.** A turn is grounded only in the sources
  its agent's pinned revision names, searched in the run's own tenant, so one
  workspace's agent never reads another's documents. A source the workspace no
  longer has grounds nothing. Passages reach the model labelled as reference
  material, not instructions. `TestAnAgentsSourcesGroundItsTurnsInItsOwnTenant`
  runs the same agent spec in two tenants and checks the second never sees
  the first's passage.
- **Evals are bounded.** A suite holds at most 50 cases, one run of a suite is
  open at a time, cases run one after another, and a case is given two minutes.
- **Only trusted origins name a schedule.** A run is tied to a schedule only
  when the cron trigger or the platform's own run-now raised it.
- **Queries are bounded.** Analytics and the journal take a window of at most
  30 days, a capped number of buckets and a capped number of rows.
- **Errors do not leak.** Unrecognized errors are logged and reported without
  detail. The identity provider's error text is never echoed.
- **Secrets come from the environment or a reference**, never from flags.
  A spec names where a credential is kept (`env:`, `keychain:`, `op://`);
  warp reads it when it is used and does not write it down.
- **Every change is logged** with subject, kind, tenant, method and path.
  Denials are logged with the permission that was missing.

## Tool credentials

An MCP tool provider is reached with one of two credentials, as its
`credential_scope` says ([spec](spec.md#tools-that-act-as-you)): its own
shared `credential`, the same for every run, or the credential of the person
the run acts for. fiber reaches a person-scoped provider only with the
credential warp answers for the actor on the run's record, on a session of
that person's own, and never with the shared one: such a provider has none.
fiber redacts every resolved credential, shared or a person's, from tool
results, errors and logs; warp's tests check each person's token is absent
from answers, run events, what the model was sent and the server log.

| The run | Acts for |
|---|---|
| asked in the console, or through the API with a person's sign-in | that person |
| asked by a machine for a person its client maps (`act_as`) | the mapped person |
| asked about with the Slack app's message shortcut | the person who pressed it, when the message is theirs. On anyone else's message, or a bot's, nobody: the app asks as itself, since the message's words are the whole request and were not the presser's. A mention in a channel thread differs: the thread's messages go ahead of the asker's own words, each with who wrote it, and the run acts for the asker, who asked there |
| raised by a Slack client's connector | the one person the team's enabled Slack clients map the Slack user to; none, or two different people, is nobody |
| started by a message in a shared thread: an agent it tags, `@auto`, or the default it inherits | the person who posted the message, or the person a client's credential posted it for (`act_as`), and never another participant. Like a mention in a Slack channel thread, the run is sent the room up to the message, what others said there each with who said it, ahead of the poster's own words, as it reads a tool's result: a request someone else made in the thread is acted on with the poster's credentials only when the poster's own words ask for it, as "go ahead" does. What the run finds is posted in the thread, which everyone who reads the project reads. A failed answer is retried only by the poster, from their inbox, as an answer to the same message; a room offers no Retry or Regenerate of its own. A follow-up asked through the API after a turn in such a thread acts for whoever asks it, and is sent the room the same way |
| Run Now on a schedule | whoever pressed it, when they wrote what it runs: the schedule's last editor, or its creator while nobody has edited it. Anyone with `schedules:write` may change a schedule's words, so anyone else's press acts for nobody, as the schedule's own clock does: one person's words never run with another's credential |
| Retry on a failed turn in the inbox, and Retry or Resume in the Slack app | whoever the turn acted for. A turn that acted for a person is retried only by that person: in the inbox signed in as themselves, where anyone else is not offered Retry (the card says only that person can retry it) and is refused (403), and in the Slack app by a Slack person its client maps to them, asked for with `act_as`, where anyone else, an operator included, is refused. So one person's words never run with another's credential, nor a person's credential on someone else's choice. A turn that acted for nobody is retried for nobody: in the inbox by anyone with `runs:write`, in the Slack app by the person who asked or an operator, and only an operator once a restart has made the app forget who asked. Each person's retry is a run of its own, and pressing again joins it |
| Retry or Regenerate in a console conversation | the person pressing it, as anything they send there, so each is offered only on a turn that acted for them: on anyone else's, or one that acted for nobody, neither is offered, and a failed turn says why instead. A turn recorded before warp said whom a run acts for counts as the person's who asked it from the console or the API, though not a retry warp asked for nobody |
| asked by a machine as itself, a schedule on its own clock, an eval, housekeeping, a call from another agent, GitHub | nobody |

A person connects their own credential at `PUT /api/me/credentials/{tool}`
(Your connections in the console). The route takes no subject, a machine
connects nothing, and another workspace's tool is not found. There are two
kinds, and they are kept differently:

- **A reference is not stored as a secret.** Only the reference is kept: a
  name such as `op://People/ana/issues`. The server reads what it names when a
  run uses it, with the server's own access: its environment, its keychain,
  the 1Password account it signed in to. So a person could name anything the
  server can read, and the operator lists, per tenant, where references may
  start (`person_refs`, `-person-refs`), each start naming the person whose
  references it holds with `{subject}`: `op://People/{subject}/`. A person
  may give only their own, a reference under a start with their subject in
  it, so Ben cannot connect `op://People/ana/issues` and have his runs act
  with Ana's token. This is checked when the reference is given and again
  on every turn that uses it. `{subject}` is followed by a separator a
  subject there cannot contain (`/`, or `_` in an `env:` start, which takes
  only lowercase letters and digits and writes them in capitals, so no two
  subjects are written alike), and no two starts may share a place,
  within a tenant or across two, so no reference is under two people's
  starts. Nothing is listed by default, so nobody may reference anything
  until the operator says where. An `env:` start names a `WARP_` variable
  and never a sign-in secret, as a client's reference must. The binding is
  only as good as the place: keep each person's items under their exact
  subject, in a vault, keychain service or variable prefix that holds
  nothing else, and in a store that tells names apart by case. A reference
  is not read when it is given, so connecting one says nothing about whether
  it names anything, and a refused reference gets one message whatever the
  reason. In a container the server's access is its environment alone, so
  `warp deploy` refuses a `keychain:` or `op://` start for a compose or
  kubernetes target ([Deploying it](spec.md#deploying-it)).
- **A value is stored.** Only where the Platform document sets
  `workspace.person_credentials.values: true`, which is off by default. It is
  kept with the same protections a client's secret has: in the same secrets
  store, apart from the record, never returned by any route, never written to
  a log, and forgotten when it is removed or replaced by a reference. warp
  does not encrypt it: with `-state=postgres` it is in the database as
  written, so guard the database as you would the credentials.

Both are checked again each time a run uses them: a stored value is not used
once the workspace turns values off, and a reference is not used once it
is no longer the person's own under a listed start. A person sees where their credential is sent (the
endpoint of an http provider) before they connect it.

What a person's credential reached is not written into memory. No memory is
read by one person alone: an agent's notes are sent to every run of that
agent, and a project's to every run in it and, as an ancestor's notes, to
every run below it, including runs for other people and runs for nobody. A
turn's answer is built from its tool results and from the conversation so
far, so the memory agent is not asked about a turn when it, or any turn
before it on its branch, acted for a person and was answered by an agent
revision granted a person-scoped provider. This holds whichever agent
answers the later turn, whoever it acts for, and with `memory_auto_approve`
on or off: a proposal waiting in the inbox would show the data to whoever
reviews it. Where that cannot be told, it is assumed: a branch whose
earlier turns the run index no longer holds, a revision or a provider that
cannot be found, and a provider changed since the turn.
`TestWhatAPersonsCredentialReachedIsNotTaughtToMemory` has Ana's turn in a
team project read her issues with her token, with approval off, and checks
that neither her turn nor a tool-less agent's follow-up to it is taught,
that a turn that reached no credential still is, and that Ben's next turn
in the project is not sent her issue. In a shared thread every turn is sent
the room up to its message, which holds what every earlier message's agents
answered, so there every earlier turn of the conversation counts as one
before it, whoever it acted for
(`TestWhatAPersonsCredentialReachedInARoomIsNotTaughtToMemory`).

What a person answers a run steers what its tools are asked next, so a run
that uses the credential of the person it acts for asks that person alone
its replies and choices, and anyone else's answer is refused (403) on the
run, from the inbox, in a shared thread and from Slack. That holds for a
question the agent asks of the project's experts (`asked_of: sme:<topic>`)
too: it goes to the person the run acts for, not to the experts, since an
expert's words would reach that person's tools with that person's
credential (`TestAnExpertDoesNotAnswerWhatARunAsksWithAnotherPersonsCredential`).

## Dev auth

`-auth=dev` makes every request an admin so the console can be previewed
without an identity provider. The server refuses to start in this mode on any
address that is not loopback. Never put it behind a proxy.

It is `warp start`'s default, for a laptop. `warp serve` signs in with OIDC
unless a flag or the spec's System document says otherwise; a System document
that names no `auth.kind` means `dev`, so a server's spec names one.

`-auth=mock` runs a development identity provider in process, so sign-in,
roles and machine tokens can be tried without a real one. Anyone who reaches
it can sign in as any persona, so it is loopback only: the server and the
provider must both listen on loopback, every redirect must be loopback, and
its issuer is a loopback `http` URL, which the verifier accepts for nothing
else. It logs a warning at start and its page says what it is. See
[authorization](authorization.md#sign-in-locally).

## Configure sign-in (Okta)

1. Create an **OIDC web application**. Sign-in redirect URI:
   `<public-url>/auth/callback`. Note the client ID and secret.
2. On the **authorization server** (for example `default`), set the audience
   to a value of your choice, such as `api://warp`.
3. Add a **groups claim** named `groups` to both ID and access tokens, filtered
   to the platform's groups.
4. Create the groups `warp-admins`, `warp-managers`, `warp-operators` and
   `warp-viewers`, and
   assign people.
5. For each machine, create an **API services application** and grant it only
   the `warp:` scopes it needs. Add those scopes to the authorization server.

```sh
export WARP_OIDC_ISSUER="https://<org>.okta.com/oauth2/default"
export WARP_OIDC_CLIENT_ID="<web app client id>"
export WARP_OIDC_CLIENT_SECRET="<from your secret store>"
export WARP_OIDC_AUDIENCE="api://warp"
warp serve -public-url https://warp.example.com
```

Any OpenID Connect provider works the same way. Group names, the groups claim
and a per-caller tenant claim are flags: see `serve -h`.

`-auth=okta` is an alias for `-auth=oidc` and uses the same environment variables.
Opening the console automatically starts sign-in. Warp does not send
`prompt=login` or `max_age`, so an existing Okta session can satisfy the flow
without another password prompt, subject to the organization's Okta policy.
See [Okta session cookies](https://developer.okta.com/docs/guides/session-cookie/main/).

## Tailscale: no application sign-in

Run Warp behind [Tailscale Serve](https://tailscale.com/docs/features/tailscale-serve).
Serve authenticates the tailnet user on every request. No OIDC application or
Warp session cookie is needed.

Create `/etc/warp/tailnet.authz`, granting specific login names:

```text
role viewer { allow read on * }
role admin { allow * on * }
grant viewer to subject "tailscale:alice@example.com"
grant admin to subject "tailscale:operator@example.com"
```

```sh
export WARP_AUTHZ_POLICY=/etc/warp/tailnet.authz
warp serve -auth=tailscale -addr 127.0.0.1:8080 \
  -public-url https://warp.your-tailnet.ts.net -tenant acme \
  -web apps/web/dist
# In another terminal on the same host:
tailscale serve --bg http://127.0.0.1:8080
```

Use the HTTPS hostname reported by Serve as `-public-url`. The server refuses
a non-loopback listener, a non-HTTPS public URL or `-tenant-claim` in this mode.
Tenant always comes from `-tenant`. The policy receives the subject
`tailscale:<login>`; groups, email verification and arbitrary claims are not
inferred from headers. No role is granted by default. A shared-device visitor
also needs an explicit policy grant unless you grant a role to `anyone`.

Trust all local processes on the Serve host: they can forge loopback identity
headers. Never expose the backend through another proxy. Requests from remote
peers, to another host, with bearer credentials, or without a user identity
are refused. Funnel traffic and tagged devices carry no Serve user identity
and cannot sign in. Display names support Serve's RFC 2047 encoding.

Signing out clears Warp cookies but cannot end the device's Tailscale
identity. Reloading the console signs the user in again. Remove the policy
grant or tailnet access to revoke access. This mode does not accept OIDC
machine tokens; use OIDC mode for those deployments.

## Files agents make

- An agent has the file tools only if its spec names them. They act on one
  directory per conversation, opened as an `os.Root`, so no path a model
  writes leaves it: not `..`, not an absolute path, not a symbolic link.
- A published file's type comes from its bytes, not from its name or from
  what the agent said it was.
- A download checks the caller's tenant and `artifacts:read` on every
  request. It is served as an attachment of an opaque type with `nosniff` and
  a sandboxing content security policy. Only plain text, PNG, JPEG, GIF, WebP
  and PDF are ever shown inline, and only when asked. HTML and SVG never are:
  a file written from a stranger's text would otherwise run as this
  application.
- Deleting needs `artifacts:write`. Publishes, downloads, deletes and run
  exports are logged with who and when.
- The platform's own agents cannot be created or revised through the API.

## The inbox

Every inbox route asks only for `runs:read`. What a caller sees and may do is
decided per message:

| Kind | To see | To settle |
|------|--------|-----------|
| Alert, reached limit | `alerts:read` | `alerts:write` to acknowledge, `billing:write` to raise a limit |
| Failed run | `runs:read` | `runs:write`, and to retry a turn that acted for a person, being that person ([Tool credentials](#tool-credentials)) |
| Proposed memory | `agents:read` | `agents:write` for an agent's; `projects:write` for a project's, and `projects:govern` as well for the workspace's and a company project's |
| A question a run waits on (approval, choice, reply) | `runs:read`, and addressed to them | `runs:write`, and the runtime agrees they may answer; a choice or reply in a run that uses the credentials of the person it acts for, only that person |
| A mention (a thread's message that tags them) | `runs:read`, and only the person tagged, or the role an expert entry names | `runs:write`, to reply in the thread as themselves or set it aside |

A message a caller may not see answers 404, not 403, and is left out of their
count. A question is addressed to the people its run's pinned spec lets answer
it (`approvers` or `answerers`, by subject or `role:<name>`), and never to the
person who asked for an approval's run or its tree. A question asked of
`sme:<topic>` goes to the experts its project's chain names for the topic,
narrowed to those `answerers` when it names any. Someone who can see the run
but may not answer is refused with 403 on `answerQuestion`, and nothing is
recorded. See [People in the loop](architecture.md#people-in-the-loop).

Addressing routes a question; it does not keep it confidential. The question,
its prompt and an approval's tool arguments are also on the run
(`GET /api/runs/{id}`, `question`), on its event stream (`lifecycle.parked`,
and `lifecycle.child.parked` on its caller's), and in its manifest, all
readable by anyone in the workspace with `runs:read`. Do not have an agent put
in a question what everyone who can read runs may not see.

Who may not approve: the person whose request raised the run, and the person
who started the tree it is in, by every name they go by: their subject, the
trigger's actor id, and for a run a Slack connector raised, every principal
any Slack client of the run's team maps that Slack user to. The team is the one
the run was accepted from, and the names are pinned when it starts, so a
mapping removed later still counts. The platform checks this itself before
it asks the runtime, because fiber's `AnswerRequest` takes no requester
alias. A GitHub connector's actor is a login no client maps, so a person
approving a call their own GitHub event raised is not detected.

An approval of a call that runs with the credential of the person the run
acts for says so, on the run and in the inbox (`tool.acts_for_person`).
Anyone it is addressed to may approve it as asked or deny it, but only that
person may approve it with edited arguments, as fiber enforces: anyone
else's edit is refused (403). Every approval card says this, in the
console's inbox and on the run, and in the Slack app, and none offers an
edit of such a call.

A reply or a choice steers what a run does next, unlike approving a call as
it was asked. So a run that uses the credentials of the person it acts for
(it acts for a person, and its revision is granted a person-scoped tool
provider, or one that cannot be found) asks that person alone: its reply and
choice questions are addressed to them, and anyone else's answer, on the
run, in the inbox or through a surface's `act_as`, is refused (403) before
the runtime is asked. Otherwise one person's words, or their pick of the
options, would reach another person's tools with that person's credential.
A run that acts for nobody, or reaches only shared credentials, is answered
by whoever `answerers` allows.

The right is checked before saying whether a part is still open. A
change is pushed to the browser as an id only, and only to callers who may see
that kind.

## Not built yet

- Limits are checked under a lock in one process. With `-state=postgres` the
  usage ledger is kept, and a second server on the same database is refused
  at start, so two servers cannot both admit a run at the limit. In memory, a
  restart forgets what was used. Spend is priced from saige's catalog: a model
  it has no price for is counted apart and never as dollars, so a spend limit
  does not bound it (a token limit does).
- Sessions are in memory, so a restart signs everyone out. Schedules are kept
  with `-state=postgres`.
- With `-state=postgres`, client secret values written through the API, and
  people's tool credentials stored as values, are kept in the database as
  written. Guard it as you would the secrets, or use references, which keep
  no value. Values for people are off unless a workspace turns them on.
- Memory written by the memory agent can carry whatever a conversation
  persuaded it to write, into later conversations. It is visible and removable,
  and off by default, but nothing reviews a note before it is used. It is
  never drawn from a conversation that acted for a person with a
  person-scoped tool.
- Dictation uses the browser's speech recognition. In Chrome that sends audio
  to the browser vendor's service, not to this server.
- No OAuth to third parties: nobody signs in to a third party through warp.
  A shared MCP server's credential is a reference the spec names, read when a
  session opens. A person connects a tool with a reference or a token they
  got elsewhere, and warp keeps no refresh token. A person's value is kept in
  the clear in the store client secrets use: there is no encrypted store yet.
- Only MCP tools act as a person. The Slack and GitHub surfaces' own actions
  and deliveries still use their client's token, whoever the run acts for.
- A rotated secret behind an unchanged reference is not noticed by an open
  session until the reference, the secret warp answers or the provider's
  record changes, as fiber documents.
- A run another agent called acts for nobody, so it gets no tools that act as
  a person, even when the person who started the tree has connected them. A
  GitHub run acts for nobody too, since no client maps GitHub logins.
- No token revocation check beyond expiry, and no refresh. A session lasts 8
  hours.
- No rate limiting.
- No local accounts. Deployments can use Tailscale Serve identity or the
  loopback development modes without configuring an OIDC application.
- The audit trail is the server log, not a store.
- A call cannot yet narrow the callee's access to the caller's grant.
- Sign-in has been tested against in-process issuers, not a live Okta org.
