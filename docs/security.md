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
them: the project's memory routes, the inbox, and the memory agent, which
keeps what it learns from a turn unasked only when the workspace chose that
and the person the turn was for may change those notes themselves. The agent memory routes reach
an agent's own notes only, and answer 404 for a project's memory owner
(`project:<name>`, and `project:` for the workspace). Then the caller's role on
the project decides, and only narrows: a viewer reads, a member creates
projects under it and proposes notes, an owner changes it (see
[Project roles](authorization.md#project-roles)). A project the caller has no
role in answers 404, and so does everything that lives in it: its runs,
threads, events, files, notes, schedules, inbox messages, limits and eval
suites, and the agents that live there, each answering exactly as an object
that does not exist.

An agent that lives there is named to a reader who may not read it in one
way only: as the agent that ran a run they may read. A run names the agent
that ran it, which was available where it ran, even one moved since. So does
what is built only from such runs: a conversation's agents, the agents that
joined a thread by answering there, a tag in a thread that started a run, analytics and the analyst's summaries by
agent and their slowest runs, a recap's rows of what was done, and a filter
of runs by agent. That is history the reader may already read, so one page
never hides a name another shows.

Everything that names an agent as configuration, or as a choice going
forward, leaves it out, as if it were not there: a client, a project's lists
of agents, the workspace's settings, an agent's spec (the agents it may call,
the approvals of those calls, and who may approve when every approval is
left out, with the hash of what is shown), an agent's call policy (the
callers it admits and trusts), the definition in a run's manifest, a
channel's default and its history, a thread's default, the agents added to it
by name who never answered there, and the tags in its turns that started
nothing, the lists of
agents available, and a project's history. A default left out is passed over
for the reader, as if it were not set: who answers there, for them, is the
next default along, as it would be were it not set, so neither what they read
nor what their messages do tells it apart from none. A refusal that an agent
is not available somewhere says where it lives only when everyone in the
workspace reads that project, since it reaches whoever reads where it was
made.

A change to any of them keeps what the caller was not shown, in place. A
spec's is kept against the revision the write expects, a call policy's under
its store's lock, and a channel's and a thread's default under theirs, so an
entry another writer adds meanwhile is kept too; a client's and the
settings' are kept against what was read when the write began. What the
caller was not shown counts toward no limit they are told of: a call
policy's lists hold more than 100 callers only with ones already stored, and
a change that adds to a thread leaves at most 50 participants they are
shown, of at most 100 in all. Who may approve a spec's kept approvals is
kept with them when the writer was shown none of its approvals or wrote
none, so it guards them still; a writer shown no approvals who adds one
then reads back the approvers kept, which tells them a hidden approval
exists, never whose. A request that names such an
agent, there or as the agent a run, a call, a schedule or a thread asks,
answers as one naming an agent that does not exist. Moving an agent into a
project is refused while a project's, a channel's or a client's default
would lose it; a thread's default is kept and left out instead. A model
policy such an agent runs on is not removed, and the refusal does not name
the agent. A private thread is read by its participants alone.

A machine whose token names its scopes, such as the Slack app or CI, needs
`humagen:projects:read` to list projects and `humagen:projects:write` to make them:
`runs:*` no longer covers them.

Which agents may run where is a policy the platform enforces, never an
instruction in a prompt. A run is refused at admission when its agent is not
available in the project it is filed in, whatever started it, and an agent
that lives in a project is not available in the workspace itself or in any
project outside its home's subtree. A message posted in the console runs
only the agents it tags or, when it tags none, the agents its conversation
has selected (the best fit, or all of them); with neither it starts nothing,
whatever default is set.
A message a client posts from Slack or GitHub that tags no agent goes to the
default its place inherits, and with none nothing runs: nothing routes on its
own. Setting the workspace default changes who answers everyone's untagged
messages, so it needs `projects:govern`. Owning the workspace is not enough,
since the workspace itself is changed only with govern: to keep the old split
where managers set it, grant them `govern on projects`, or a role that holds it
such as `steward` in [authorization](authorization.md#example).

The directory (`people:read`, which every built-in role holds) shows each
person's name, handle, roles and picture to everyone in the workspace. No
email address is asked for or shown, and a name that is an address is not
kept as a name. A subject is kept as the identity provider gives it, since it
is the key a person signs in with, and can hold an address: a Tailscale login
does, and so can the subject a Slack people map names. The directory leaves
such a subject out and never searches subjects. A picture is only one a
person uploaded themselves, served by humagen: reading the directory sends
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

A machine has no role. It is issued scopes named `humagen:<permission>`, for
example `humagen:runs:write`. A scope without the `humagen:` prefix, or one that names
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
  app does not ask. To answer, to ask for a run or to post in a thread for
  them, the mapped person's own grant (what the policy grants their subject)
  must include `runs:write`, as well as the credential's: a workspace viewer
  starts nothing because a machine relays their words. The answer is recorded as the mapped principal, with the
  credential as `Via`: a credential never approves a call in a run it raised
  as itself, whoever it names. They answer only what they may read, and
  start or post only where they are a member, wherever the machine may read.
  A run a Slack client's own connector raises is held to the same: it acts
  for the person the team maps the Slack user to only if the policy grants
  their subject `runs:write` and they are a member where it is filed, else
  it is not started, and in a Slack channel bound to a project someone the
  team maps to nobody is not answered. Mapping a person is `clients:write`.
- **A person posts only as themselves.** A message in a shared thread is
  the caller's, or, for a credential, the person its client maps through
  `act_as`, who must hold `runs:write` themselves; a credential acting for
  nobody is refused (403). No route takes an author or an agent to speak as,
  and nothing writes an agent's turn but its run. A shared thread is read by
  everyone with `runs:read` who sees its project; a private one by its
  participants alone, and every read path answers 404 for it to anyone else.
  A conversation the console starts is private until it is shared, which
  takes whoever started it (or an owner of its project), never only someone
  tagged into it, and a member of the project it goes to: from
  then on everyone who sees that project reads it, the messages written
  while it was private included. A share never makes a thread private again,
  and a conversation that has runs does not move to another project, so its
  runs stay read where they were filed.
- **The machine holds its people's votes.** Whoever holds the credential a
  Slack client names as `machine` can answer every question as any person the
  client maps, within what each may answer, and raise runs as them, which
  use the tools that act as them with their own credentials. The Slack
  app is trusted to say truthfully which Slack user clicked; the platform
  cannot check it. A mapped person's roles are what the policy grants their
  subject: a click carries no groups, so a role held only through a group
  does not count, and such a person cannot answer, ask or post from Slack. Keep that credential as you would those people's sign-ins,
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
- **A scheduled answer drawn from a person's own credential stays theirs.**
  A Run Now that acted for the person who pressed it, with a person-scoped
  tool, is never posted in its project, held for its owners as a draft, or
  sent to someone else who created the schedule: its answer goes to that
  person's inbox alone, saying where it would have gone.
- **A project's knowledge reaches only that project and below.** A run is
  grounded in the sources its project and the projects above it name, as
  they stood when it was accepted, and never in a sibling's or a
  sub-project's; whatever a caller writes in the tag for them is replaced.
  A source is the workspace's, read with `knowledge:read`, so naming one in
  a project takes `knowledge:read` as well as owning the project, and a
  reader without it is shown no source's name. Naming a source lets the
  runs filed there, and the people they act for, read what it holds through
  their answers, as an agent's own sources do.
- **A skill's instructions reach only the runs a person asked for.** A
  message names skills its project's chain declares, and only the runs that
  message starts are given them, pinned when it is accepted. Admission drops
  the pinned key from every tag but a root run the platform's API raised,
  which only the platform does, so no connector, schedule, caller or calling
  agent puts instructions in a run, and a run an agent calls is not given its
  caller's. The instructions reach the model ahead of the turn, under a line
  that ranks them below the agent's own and says the platform enforces
  limits, approvals and availability whatever they say; the pinned key itself
  is private (`_warp.skills`), so fiber writes it into no prompt and no
  record of one, a run stopped before it starts included. A skill is read by
  whoever reads its project, and a hidden project's skills answer as missing
  ones in every list and completion.
- **A mention kind resolves on the server.** `@<kind>:<name>` is resolved
  as the workspace's Platform document says, which only the file sets;
  completing people reads the directory and takes `people:read`, as the
  directory does.
- **Project memory reaches only that project.** A turn is sent a project's
  shared notes only when its conversation is filed in that project.
- **Knowledge stays in its tenant.** A turn is grounded only in the sources
  its agent's pinned revision names and those its project's chain named when
  the run was accepted, searched in the run's own tenant, so one
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
  humagen reads it when it is used and does not write it down.
- **Every change is logged** with subject, kind, tenant, method and path.
  Denials are logged with the permission that was missing.

## Tool credentials

An MCP tool provider is reached with one of two credentials, as its
`credential_scope` says ([spec](spec.md#tools-that-act-as-you)): its own
shared `credential`, the same for every run, or the credential of the person
the run acts for. fiber reaches a person-scoped provider only with the
credential humagen answers for the actor on the run's record, on a session of
that person's own, and never with the shared one: such a provider has none.
fiber redacts every resolved credential, shared or a person's, from tool
results, errors and logs; humagen's tests check each person's token is absent
from answers, run events, what the model was sent and the server log.

| The run | Acts for |
|---|---|
| asked in the console, or through the API with a person's sign-in | that person |
| asked by a machine for a person its client maps (`act_as`) | the mapped person |
| asked about with the Slack app's message shortcut | the person who pressed it, when the message is theirs. On anyone else's message, or a bot's, nobody: the app asks as itself, since the message's words are the whole request and were not the presser's. Asking as itself acts for whoever the app's own credential is, so where that is a person, as under a dev sign-in (an empty `HUMAGEN_TOKEN` against `-auth=dev`, where every request is `dev`'s), it acts for that person. A mention in a channel thread differs: the thread's messages go ahead of the asker's own words, each with who wrote it, and the run acts for the asker, who asked there |
| raised by a Slack client's connector | the one person the team's enabled Slack clients map the Slack user to; none, or two different people, is nobody |
| started by a message in a shared thread: an agent it tags, `@auto`, or the default it inherits | the person who posted the message, or the person a client's credential posted it for (`act_as`), and never another participant. Like a mention in a Slack channel thread, the run is sent the room up to the message, what others said there each with who said it, ahead of the poster's own words, as it reads a tool's result: a request someone else made in the thread is acted on with the poster's credentials only when the poster's own words ask for it, as "go ahead" does. Each earlier turn is one line, its line breaks made spaces, after who took it: a person by name and by handle, which no one else has, an agent marked as one. The room is fenced off from the poster's words, which follow with who posted them, so no one can write a line that reads as the poster's, nor pass for them by display name, which anyone may share and which may be an agent's (`TestNoOneInTheRoomPutsWordsInThePostersMouth`). What the run finds is posted in the thread, which everyone who reads the project reads. A failed answer is retried only by the poster, from their inbox, as an answer to the same message; a room offers no Retry or Regenerate of its own. A follow-up asked through the API after a turn in such a thread acts for whoever asks it, and is sent the room the same way: each earlier follow-up with who asked it, or as acting for nobody, and its own words with who asks them (`TestAFollowUpInARoomNamesWhoAskedEachTurn`) |
| Run Now on a schedule | whoever pressed it, when they wrote what it runs: whoever last changed its text, or its creator while nobody has, and nobody else has since chosen its agent or its project, whose instructions and notes go ahead of its text. Saving a schedule without changing its text writes none of it, so moving someone else's to another time does not make it yours. Anyone with `schedules:write` may change a schedule's words, so anyone else's press acts for nobody, as the schedule's own clock does: one person's words never run with another's credential, nor on an agent or in a project someone else chose for them. What a project itself says, its instructions and notes, is still written by whoever may change that project, as for every turn filed in it. A schedule edited before humagen kept who wrote it acts for nobody until its text is written again |
| Retry on a failed turn in the inbox, and Retry or Resume in the Slack app | whoever the turn acted for. A turn that acted for a person is retried only by that person: in the inbox signed in as themselves, where anyone else is not offered Retry (the card says only that person can retry it) and is refused (403), and in the Slack app by a Slack person its client maps to them, asked for with `act_as`, where anyone else, an operator included, is refused. So one person's words never run with another's credential, nor a person's credential on someone else's choice. The exception is a turn the Slack app asked as itself, which it asks again the same way, through the app, by the person who asked or an operator: where the app's own credential is a person, as under a dev sign-in, such a turn acted for that person, and its retry acts for them again. A turn that acted for nobody is retried for nobody: in the inbox by anyone with `runs:write`, in the Slack app by the person who asked or an operator, and only an operator once a restart has made the app forget who asked. Each person's retry is a run of its own, and pressing again joins it |
| Retry or Regenerate in a console conversation | the person pressing it, as anything they send there, so each is offered only on a turn that acted for them: on anyone else's, or one that acted for nobody, neither is offered, and a failed turn says why instead. A turn recorded before humagen said whom a run acts for counts as the person's who asked it under their own name from the console or the API, unless its words were someone else's: an earlier turn's at the same point, asked by anyone else, which is what the console's Retry and Regenerate asked then. One a machine asked for a person (the Slack app's `act_as`, its message shortcut included) and a retry humagen asked for nobody count as nobody's |
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
  until the operator says where. An `env:` start names a `HUMAGEN_` variable
  and never a sign-in secret, as a client's reference must. The binding is
  only as good as the place: keep each person's items under their exact
  subject, in a vault, keychain service or variable prefix that holds
  nothing else, and in a store that tells names apart by case. A reference
  is not read when it is given, so connecting one says nothing about whether
  it names anything, and a refused reference gets one message whatever the
  reason. In a container the server's access is its environment alone, so
  `humagen deploy` refuses a `keychain:` or `op://` start for a compose or
  kubernetes target ([Deploying it](spec.md#deploying-it)).
- **A value is stored.** Only where the Platform document sets
  `workspace.person_credentials.values: true`, which is off by default. It is
  kept with the same protections a client's secret has: in the same secrets
  store, apart from the record, never returned by any route, never written to
  a log, and forgotten when it is removed or replaced by a reference. humagen
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

What a person answers a run steers what its tools are asked next, and what
a called run is answered is its caller's tool result. So a run that uses the
credential of the person it acts for, and any run it called, asks that
person alone its replies and choices, and anyone else's answer is refused
(403) on the run, from the inbox, in a shared thread and from Slack. That
holds for a question the agent asks of the project's experts
(`asked_of: sme:<topic>`) too: it goes to the person the run, or the run that
called it, acts for, not to the experts, since an expert's words would
reach that person's tools with that person's credential
(`TestAnExpertDoesNotAnswerWhatARunAsksWithAnotherPersonsCredential`,
`TestAnExpertDoesNotAnswerARunCalledByOneWithAnotherPersonsCredential`).

## Dev auth

`-auth=dev` makes every request an admin so the console can be previewed
without an identity provider. The server refuses to start in this mode on any
address that is not loopback. Never put it behind a proxy.

It is `humagen start`'s default, for a laptop. `humagen serve` signs in with OIDC
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
   to a value of your choice, such as `api://humagen`.
3. Add a **groups claim** named `groups` to both ID and access tokens, filtered
   to the platform's groups.
4. Create the groups `humagen-admins`, `humagen-managers`, `humagen-operators` and
   `humagen-viewers`, and
   assign people.
5. For each machine, create an **API services application** and grant it only
   the `humagen:` scopes it needs. Add those scopes to the authorization server.

```sh
export HUMAGEN_OIDC_ISSUER="https://<org>.okta.com/oauth2/default"
export HUMAGEN_OIDC_CLIENT_ID="<web app client id>"
export HUMAGEN_OIDC_CLIENT_SECRET="<from your secret store>"
export HUMAGEN_OIDC_AUDIENCE="api://humagen"
humagen serve -public-url https://humagen.example.com
```

Any OpenID Connect provider works the same way. Group names, the groups claim
and a per-caller tenant claim are flags: see `serve -h`.

`-auth=okta` is an alias for `-auth=oidc` and uses the same environment variables.
Opening the console automatically starts sign-in. Humagen does not send
`prompt=login` or `max_age`, so an existing Okta session can satisfy the flow
without another password prompt, subject to the organization's Okta policy.
See [Okta session cookies](https://developer.okta.com/docs/guides/session-cookie/main/).

## Tailscale: no application sign-in

Run Humagen behind [Tailscale Serve](https://tailscale.com/docs/features/tailscale-serve).
Serve authenticates the tailnet user on every request. No OIDC application or
Humagen session cookie is needed.

Create `/etc/warp/tailnet.authz`, granting specific login names:

```text
role viewer { allow read on * }
role admin { allow * on * }
grant viewer to subject "tailscale:alice@example.com"
grant admin to subject "tailscale:operator@example.com"
```

```sh
export HUMAGEN_AUTHZ_POLICY=/etc/warp/tailnet.authz
humagen serve -auth=tailscale -addr 127.0.0.1:8080 \
  -public-url https://humagen.your-tailnet.ts.net -tenant acme \
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

Signing out clears Humagen cookies but cannot end the device's Tailscale
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
| Failed run | `runs:read` | `runs:write`, and to retry a turn that acted for a person, being that person ([Tool credentials](#tool-credentials)); retrying a turn filed in a project needs a member of it as well |
| Proposed memory | `agents:read` | `agents:write` for an agent's, and owning its home for one that lives in a project; `projects:write` and owning the project for a project's, and `projects:govern` as well for the workspace's and a company project's |
| Announcement | `projects:read`, and a member of its project or one below it (anyone who reads the project's own inbox) | hiding or snoozing it is the reader's own (`projects:read`); withdrawing it is its author's or an owner's (`projects:write`) |
| A question a run waits on (approval, choice, reply) | `runs:read`, and addressed to them | `runs:write`, and the runtime agrees they may answer; where the answer may reach the credentials of the person a run acts for, a choice or reply only by that person, and an approval with edited arguments or a note only by them (the question names them as `steered_for`) |
| A mention (a thread's message that tags them) | `runs:read`, and only the person tagged, or the role an expert entry names | `runs:write`, to set it aside or to reply in the thread as themselves, which takes seeing the thread's project; a reply that tags anyone takes a member of it, since its tags start the agents and tell the people they name as any message's do |

A message a caller may not see answers 404, not 403, and is left out of their
count, and a notice about it never reaches their event stream. Where a
client's app posted a message (`deliveries`) is shown only to that client's
machine, wherever the message is read, a project's inbox included, and so is
each receipt (`delivered`) in the message's history. A count is added up
from counters, one for each bucket of open messages alike in kind,
addressing and what they are about (and one for each group of messages
snoozed for everyone, alike in the same), and each is asked of the same rule
a message is, so it counts only what the caller may see. What was settled
for everyone is dropped with its record after the workspace's
`inbox_retention` (90 days by default, at least 7); the log line each
decision in the inbox writes (`platform: inbox`) is the operator's, and
stays. An
announcement's audience (`project:<name>`) and a proposed note's
(`owners:<name>`) are decided from the project tree at each read, so a revoked
membership hides past announcements at the next one. Announcements are plain
text; surfaces escape Slack mention syntax before showing them, and at most 20
a project and 200 a workspace are posted a day. A question is addressed to the people its run's pinned spec lets answer
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
console's inbox, on the run and in an agent's approval queue, and in the
Slack app, and none offers an edit of such a call.

A reply or a choice steers what a run does next, unlike approving a call as
it was asked, and so do an approval's note, which the model is shown on a
denial, and its edited arguments, which it reads back with what the call
found, even for a call that reaches only a shared credential. What a called
run is answered steers its caller too: its answer is the caller's tool
result. So where an answer may reach the credentials of the person a run
acts for (the run, or a run that called it, acts for a person and its
revision is granted a person-scoped tool provider, or one that cannot be
found), that person alone says anything to it: its reply and choice
questions are addressed to them, anyone else's answer is refused (403), and
anyone else may approve or deny its calls only as asked, with no edit and no
note (403 with either). Each is checked on the run, in the inbox and through
a surface's `act_as`, before the runtime is asked. The question names that
person (`steered_for`, as it stood when it was asked), so the console's
approval queue offers nobody else a note, and says why. So nothing anyone else
says in answer to such a run, or to a run it called, nor their pick of the
options, reaches the person's tools with their credential. Any other run,
one that acts for nobody, or reaches only shared credentials, and was called
by none that reaches a person's, is answered by whoever `answerers` and
`approvers` allow.

`answerers` still decides who may answer at all: the asking run's own, so
a called agent's for what a called run asks. When it leaves out the person
whose credentials the answer may reach, nobody may answer its reply or
choice: the question is addressed to that person, the runtime refuses them,
and humagen refuses everyone else. The run waits until the question expires (a
day by default) and takes its default, or someone stops it, the same
fail-closed outcome as `approvers` naming only the person who asked, without
`self_approval`. On an agent granted a person-scoped tool provider, and on
every agent it may call, directly or through another, leave `answerers`
unset, or name everyone who uses the agent.

That covers what a run is answered, not what a later turn reads. Each turn
of a conversation in the console or the API is sent the conversation so
far: every earlier turn's words, whoever asked them, and its answer. Anyone
with `runs:write` may add a turn to any conversation in the workspace, and
an earlier turn whose run reached no person's own credential was answered by
whoever its `answerers` and `approvers` allowed, so its answer can carry
someone else's words. When a person continues the conversation with an
agent granted a person-scoped tool, that turn reads all of it with their
credential. They see those turns and answers before they continue, as in a
Slack thread, but unlike there the transcript the agent is sent does not
say who wrote each turn, and an answer drawn from someone else's reply reads
as the agent's own. Read a conversation before continuing it with such an
agent, or start a new one.

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
- No OAuth to third parties: nobody signs in to a third party through humagen.
  A shared MCP server's credential is a reference the spec names, read when a
  session opens. A person connects a tool with a reference or a token they
  got elsewhere, and humagen keeps no refresh token. A person's value is kept in
  the clear in the store client secrets use: there is no encrypted store yet.
- Only MCP tools act as a person. The Slack and GitHub surfaces' own actions
  and deliveries still use their client's token, whoever the run acts for.
- A rotated secret behind an unchanged reference is not noticed by an open
  session until the reference, the secret humagen answers or the provider's
  record changes, as fiber documents.
- A run another agent called acts for nobody, so it gets no tools that act as
  a person, even when the person who started the tree has connected them. A
  GitHub run acts for nobody too, since no client maps GitHub logins.
- A conversation's transcript does not say who wrote what. A turn is sent
  every earlier turn as the user's, whoever asked it, and an answer drawn
  from someone's reply as the agent's own, so a person who continues a
  conversation others added to, or answered, has their person-scoped tools
  read those words with their credential ([The inbox](#the-inbox)).
- No token revocation check beyond expiry, and no refresh. A session lasts 8
  hours.
- No rate limiting.
- No local accounts. Deployments can use Tailscale Serve identity or the
  loopback development modes without configuring an OIDC application.
- The audit trail is the server log, not a store.
- A call cannot yet narrow the callee's access to the caller's grant.
- Sign-in has been tested against in-process issuers, not a live Okta org.
