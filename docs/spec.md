# The warp spec

warp is configured in one language: versioned YAML documents (JSON works too)
of two kinds, each with one owner.

| Kind | Says | Owned by | Takes effect |
|------|------|----------|--------------|
| `System` | how and where warp runs: listeners, sign-in, stores, bus, files, limits, extensions | whoever runs the process: a laptop, compose, Helm values | at start |
| `Platform` | what one workspace offers: its models, tools, agents, Slack and GitHub, settings | that workspace's admins | at start, with the console building on it |

Both can live in one `warp.yaml`, separated by `---`, or in separate files.
Two commands use it. `warp start` runs the platform here; `warp deploy` runs it
where the System document's `deploy` slot says (here, a Docker Compose project,
or a Kubernetes cluster). Both read `-f PATH` (repeatable; a directory reads its
`*.yaml`, `*.yml` and `*.json`). With no `-f` they read `warp.yaml` from
`WARP_HOME`, else the user config directory, and `warp start` runs on defaults,
with example agents, if there is none. `warp setup` writes one by asking a few
questions.

`warp serve` is the command for a server: what the image, the chart and the
compose project run. It reads only the files `-f` names, seeds nothing, never
asks, and signs in with `oidc` unless a flag or a System document says
otherwise. A System document sets sign-in like any other field, so one that
leaves `auth` out is `dev`, which answers only on loopback; Platform documents
alone leave `serve` on `oidc`.

Four rules hold for every field:

1. **Every field has a working default.** No file is a running platform on a
   laptop: development sign-in on loopback, memory stores, files on disk.
2. **A secret is a reference, never a value.** A credential is written
   `{ref: env:NAME}`, `{ref: keychain:SERVICE/ACCOUNT}` or
   `{ref: op://VAULT/ITEM/FIELD}`, read when it is used and never stored. A bare
   string where a secret belongs is refused.
3. **Every replaceable part is a slot: `kind` plus a block named after it.**
   `kind: postgres` reads `postgres: {...}`. A program built on the `warp` package
   registers more kinds (`warp.WithKind`), and the file selects one by name
   with no other change. An unknown kind is an error listing the kinds the
   binary has. A choice is a file or an interface, never a fork.
4. **Unknown fields are errors**, with the document and path they are in.

Flags still work and win over the file, so `warp start -addr 127.0.0.1:9090`
needs no edit. `warp config check -f warp.yaml` validates without starting and
lists every `env:` reference the shell it runs in has not set; `warp config
print` writes the result with every default filled in.

## System

```yaml
api_version: warp/v1
kind: System
metadata:
  name: warp                # the deployment's identifier
  display_name: Warp        # what people see: console title, Slack app name
spec:
  server:
    addr: 127.0.0.1:8080
    public_url: http://localhost:8080
    webhook_addr: ""        # a second listener that serves only signed webhooks
    console: {enabled: true, dir: auto}      # auto finds a built apps/web

  auth:                     # slot: dev | tailscale | oidc | mock
    kind: oidc
    oidc:
      issuer: https://login.example.com
      client_id: warp
      client_secret: {ref: env:WARP_OIDC_CLIENT_SECRET}
      audience: warp
    tenant: {fixed: default}                 # or {claim: org}
    groups: {claim: groups, roles: {admin: [platform-admins], operator: [staff]}}   # admin, manager, operator, viewer
    policy_file: ""                          # an authorization policy (docs/security.md)

  stores:                   # slot: memory | postgres. Every store family
    kind: postgres
    postgres: {url: {ref: env:WARP_POSTGRES_URL}}

  bus:                      # slot: memory | redis
    kind: memory

  blobs:                    # slot: disk | memory | s3
    kind: s3
    s3:
      bucket: warp-files
      region: us-east-1
      endpoint: ""          # set for MinIO or R2
      prefix: ""
      path_style: false
      access_key: {ref: env:WARP_S3_ACCESS_KEY_ID}       # both left out: the AWS chain
      secret_key: {ref: env:WARP_S3_SECRET_ACCESS_KEY}

  execution:
    workspace: ""           # agents' files. Empty is the user cache directory
    concurrency: 1
    max_depth: 4
    max_fan_out: 16
    builtin: {provider: "", model: ""}       # the platform's own agents

  secret_env:               # variables a workspace's clients may reference, by tenant
    default: [WARP_SLACK_TOKEN]

  person_refs:              # where each workspace's people may keep their own tool credentials, by tenant
    default: ["env:WARP_PERSON_{subject}_"]   # how their references may start, {subject} being the person's; none listed is none
    # deploy local (this machine) may also list "op://People/{subject}/" and "keychain:warp-people-{subject}/"

  extensions: []            # process-wide parts a binary registered, each a slot

  deploy:                   # slot: local | compose | kubernetes. Read by warp deploy
    kind: compose
    compose:
      image: ""             # empty builds one from `build`, a warp checkout
      postgres: true        # run Postgres beside warp and point the stores at it
      port: 8080
    # kind: kubernetes
    # kubernetes: {context: prod, namespace: warp, host: warp.example.com, image: {repository: ghcr.io/urmzd/warp, tag: v0.10.0}}
```

## Platform

One document per workspace. `metadata.name` is its tenant.

```yaml
api_version: warp/v1
kind: Platform
metadata:
  name: default
  display_name: Acme
spec:
  workspace:                # a field left out is left to the console
    history: {mode: recent, limit: 8000}
    agents: {enable: ["*"], default: auto}   # the workspace's own: every platform agent, and @auto for untagged messages
    instructions: Acme sells warehouse robots. Answer plainly.   # the workspace's own, sent first
    channels: [{name: announcements, purpose: Company news}]      # the workspace's own; every project has general
    experts: [{who: "role:security", topics: [security]}]         # answer anywhere a nearer project names nobody
    members: [{who: "*", role: member}, {who: "role:comms", role: owner}]   # the workspace's own
    project_kinds: [{name: partner, title: Partner, like: account}]          # the workspace's own kinds
    agent_posts: review     # what agents scheduled in the workspace itself post waits for its owners
    projects:               # merged into the tree at every start, parents first
      - {name: Launch, description: Everything for the Q4 launch}
      - name: Field
        title: Field engineering
        kind: team          # company | team | account | a project_kinds name. Left out: team
        instructions: Customer accounts. Never promise dates.
        agents: {default: lead}             # answers untagged messages here and below
        members: [{who: "*", role: viewer}, {who: "role:field-eng", role: member}, {who: "role:field-leads", role: owner}]
        agent_posts: publish  # what its scheduled agents post goes out at once
        schedules:            # the file's: filed here, paused and run in the console, changed only here
          - {name: field-recap, cron: "41 8 * * 1", agent: warp-analyst, text: "What changed in Field this week?", deliver: project}
      - name: Globex
        parent: Field       # a project in this file. Left out: the top level
        kind: account
        account: {customer: Globex Inc, external_id: "sf:0015g00000ABC", url: "https://crm.example.com/globex"}
        channels:
          - {name: renewals, purpose: Renewal work, default: auto}   # auto: @auto answers untagged messages here
          - {name: general, slack: {client: slack-acme, channel: C0123GLOBEX}}  # a Slack thread there is a shared thread here
        experts: [{who: "okta|00u1ana", topics: [billing, renewals]}]
        agents: {allow: [globex-helper, lead], default: globex-helper}  # narrows: only these run here and below
        members: [{who: "okta|00u1ana", role: owner}]   # no "*": members only, read by its members alone
    person_credentials: {values: false}   # true lets people store a tool credential's value with warp
    release_notes: true     # each release's breaking changes and features, posted once in the workspace's inbox from warp

  models:                   # each a slot: anthropic | openai | ollama
    - name: anthropic       # what an agent names as its provider
      kind: anthropic
      anthropic: {key: {ref: env:ANTHROPIC_API_KEY}}
    - name: local
      kind: openai          # any OpenAI-compatible server
      openai: {base_url: "http://127.0.0.1:8000/v1"}

  model_policies:
    - name: default
      models: [{provider: anthropic, model: YOUR_MODEL}, {provider: local, model: YOUR_LOCAL_MODEL}]
      fallback_on: [unavailable, rate_limit]

  tools:                    # each a slot: mcp
    - name: docs            # granted to an agent as mcp:docs or mcp:docs:write
      kind: mcp
      mcp:
        transport: http
        endpoint: https://mcp.example.com
        credential: {ref: env:DOCS_MCP_TOKEN}
        accept:             # pinned by schema hash; new or changed tools stay pending
          - {name: search, schema_hash: 9f2c4e, class: read}
    - name: issues          # granted as mcp:issues; each person reaches it as themselves
      kind: mcp
      mcp:
        transport: http
        endpoint: https://issues.example.com/mcp
        credential_scope: person   # shared (the default) or person. A person-scoped tool names no credential
        accept:             # not reached at start, so each tool carries the form its server offers
          - name: search
            description: Search issues
            input_schema: {type: object, properties: {q: {type: string}}}
            schema_hash: 21877437383c4aa0e8fc28bbe3abc1d801bac704010bcdd62a93e30dafd20ab6
            class: read

  agents:
    - spec:
        name: lead
        provider: policy
        model: default
        system_prompt: You coordinate work.
        access: {spawn: [helper]}
    - spec: {name: helper, provider: local, model: YOUR_LOCAL_MODEL, system_prompt: Do one task well.}
    - project: Globex       # lives in Globex: available there and below, and nowhere else
      spec: {name: globex-helper, provider: local, model: YOUR_LOCAL_MODEL, system_prompt: You know the Globex account.}

  surfaces:
    slack:                  # warp runs the Slack app as its client, over Socket Mode
      enabled: true
      team: T0123456
      bot_token: {ref: env:SLACK_BOT_TOKEN}
      app_token: {ref: env:SLACK_APP_TOKEN}
      operators: [U0123456]
      command: /agent       # `warp slack manifest -f warp.yaml` writes the app's manifest
      warp_client:          # how the app signs in to warp's API. Dev sign-in needs none
        token_url: https://login.example.com/oauth2/v1/token
        client_id: warp-slack
        client_secret: {ref: env:WARP_SLACK_CLIENT_SECRET}
        scopes: [warp:agents:read, warp:runs:read, warp:runs:write, warp:billing:read]
        audience: ""        # set when the provider needs one named
      # warp_token: {ref: env:WARP_SLACK_API_TOKEN}   # instead: a bearer token that does not expire
    github:
      enabled: false
      owners: [acme]
      token: {ref: env:WARP_GITHUB_TOKEN}
      webhook_secret: {ref: env:WARP_GITHUB_WEBHOOK_SECRET}
      login: warp[bot]      # the app's own login; not live yet, see github.md, "Who a comment asks"
```

### What the file owns

A Platform document is applied at every start, and is the floor the console
builds on:

- **Agents** in the file are created, or revised to the file's spec. An
  unchanged spec makes no new revision. An edit made in the console to an agent
  the file declares lasts until the next start. `auto` and `sme` are tags, so
  never an agent's name (`warp config check` refuses one written inline, the
  start one read from a file), and a new agent named like a handle a person
  in the workspace goes by fails the start with "a person goes by @dana", as
  the API answers 409.
- **Workspace** fields that are set are applied; fields left out stay as the
  console left them.
- **Projects** in the file are created, or taken over when the console made
  one by that name, parents first, and are managed by the file. A field the
  file sets is the file's, and the console cannot change it (409); a field
  it leaves out stays the console's. `workspace.instructions` are the
  workspace's own in the same way. An unchanged entry makes no new version.
  A project made in the console survives every start, and one that leaves
  the file stays, no longer managed, so it can be changed or deleted in the
  console again. A project's **channels** and **experts** are the file's in
  the same way: a channel the file declares is created or taken over, and
  one that leaves it stays, unmanaged; experts the file sets, even as `[]`,
  are the file's, and experts it leaves out stay the console's. A
  project's **schedules** are the file's whole: created, or taken over when
  the console made one by that name, filed in the project; the console can
  pause, resume and run one, not change or delete it (409), and one that
  leaves the file stays, the console's again. Schedules are applied last,
  once the agents and where they live are in.
- **Agents in projects.** `workspace.agents` and a project's `agents` are
  what it turns on: `enable` adds platform agents here and below (`*` alone
  is every one), `allow`, when set, is the only agents that may run here and
  below, and `default` answers a message that tags no agent (an agent, or
  `auto` for `@auto`). Each list names agents in the file or the platform's
  own. Set, the three are the file's as a whole, and the console cannot
  change them. An agent's `project` is where it lives: available there and
  below, and nowhere else, so `*` never reaches it and enabling it adds
  nothing. A named default must be available where it is set. They are
  applied in this order: the workspace, the projects, the agents, then
  where each agent lives. A home that leaves the file stays.
  `workspace.baseline` is gone: a file that still sets it fails with
  "baseline is now workspace.agents.default".
- **Models and tools** exist only while the file declares them. A tool's
  pinned schemas are its acceptance; anything else stays pending. A tool's
  `credential_scope` says whose credential reaches it: see
  [Tools that act as you](#tools-that-act-as-you).
- **GitHub** is a client managed by the file, `file-github`, read-only in
  the API. The one an earlier start kept is changed to what the file says, so
  a field added later, such as `login`, takes effect at the next start.
  `enabled: false` turns it off: it stays the file's and answers no webhook
  until the file turns it on again. A section that leaves the file leaves the
  client as it is, still answering. An unchanged section makes no new
  version. A client of that id made in the console is left as it is.
- Nothing is deleted because it left the file.

### The Slack app

`surfaces.slack` runs the Slack app (`apps/slack`) beside the server as a
client of its API, restarting it when it exits. It needs Node.js on `PATH` and
`apps/slack` with its dependencies installed (`pnpm install`), found under the
working directory, beside the binary, or at `WARP_SLACK_APP_DIR`. The warp image
carries neither, so there the surface is logged as off and the server runs
without it.

The app is given its tokens, its workspace and warp's address, plus `PATH`,
`HOME`, `TMPDIR`, `TZ`, `XDG_STATE_HOME`, the `NODE_OPTIONS`,
`NODE_EXTRA_CA_CERTS`, `NODE_USE_ENV_PROXY` and proxy variables, and
`SLACK_BRANDING` and `SLACK_SOCKET_*`, when they are set. Nothing else of
warp's environment reaches it.

It calls warp's API with a credential for the server's sign-in. Under `dev` it
needs none. Under `oidc` or `mock` it needs `warp_client`: a machine client of
the identity provider, whose access tokens the app gets with client credentials
and renews as they expire, with the scopes [the API](api.md#authenticating-a-machine)
lists for a Slack surface. `warp_token` is a bearer token given as it is, for a
token that does not expire; set one or the other. A process cannot sign in
through Tailscale Serve, so `tailscale` sign-in refuses a Slack surface. A spec
that needs a credential and has none is refused when it is checked, and so is a
start whose `-auth` flag needs one the file does not give.

Model names are shared across the workspaces in one process: two workspaces
may declare the same model, but not two different ones under one name.

### Projects

The workspace is the root of a tree of projects, written `~` in the
console's addresses. Each entry of `workspace.projects` is one project:

| Field | Left out | Rule |
|-------|----------|------|
| `name` | required | the identity, never changed: letters, digits, spaces, dashes and underscores, starting with a letter or digit, at most 63, unique ignoring case |
| `title` | the name | what people see: one line, at most 80 characters |
| `description` | none | at most 500 characters |
| `kind` | `team` | `company`, `team`, `account` or a name in `workspace.project_kinds`, which behaves as the kind it is `like`. A company project sits under the workspace or another company project, and there are at most 10. An account never sits under another account |
| `parent` | the workspace | another project in this file (`~` is the workspace), at most 4 levels below the workspace |
| `account` | none | `{customer, external_id, url}`, on an account project only. `url` is https |
| `instructions` | none | at most 4,000 characters, sent to every agent answering here and below |
| `agent_posts` | the console's | what an agent scheduled here posts: `publish` posts it at once, `review` holds it as a draft until an owner publishes or discards it. A project that leaves both out reviews at the workspace and in company projects, which everyone reads, and publishes in team and account projects. `workspace.agent_posts` is the workspace's own |
| `schedules` | none | agents scheduled here, see below |
| `members` | the console's | at most 100 `{who, role}`: `who` is `*` (everyone who can see the parent), `role:<name>` (the holders of a policy role) or a subject; `role` is `viewer`, `member` or `owner`. An owner owns everything below. Until reads are confined to members, the list has an entry for `*` of at least `viewer`, a company project's of at least `member`, and a project is never more open than its parent. A new project the file leaves them out of has everyone as a member (a viewer of an account) and `projects:govern` as its owner |

`workspace.members` are the workspace's own in the same way: everyone (`*`)
is at least a member of it, and `projects:govern` owns it whatever they say.
`workspace.project_kinds` are at most 16 kinds of the workspace's own, each
`{name, title, like}`: a name of lower case letters, digits and dashes that is
not a built-in kind, a title of at most 40 characters, and `like` either
`team` or `account`. Behaviors stay warp's; names are the workspace's. A kind
a project still has cannot be removed: a start that would fails, naming the
projects.

`workspace.instructions`, at most 4,000 characters, are sent first, then each
project's down to the one a conversation is filed in. A later one wins, and a
turn is sent at most 8,000 characters of them: the farthest are left out
first, and the turn says which.

`warp config check` refuses a mistake with its path, such as
`workspace.projects[2].parent: "Feild" is not a project in this file`. A
parent must be in the same file, because one made only in the console cannot
be checked before start. A workspace has at most 500 projects, and the start
checks the file again against the projects the console made. A file that
names another kind or parent for a project that already exists changes it as
the console's move would, under the same rules: a move into its own subtree,
past four levels, or that breaks a kind or openness rule fails the start.

### Channels and experts

Every project, the workspace included, has a `general` channel, and at most
20 channels in all. `channels` on `workspace` or on a project declares more,
or declares `general` to set its purpose:

| Field | Left out | Rule |
|-------|----------|------|
| `name` | required | lower case letters, digits and dashes, starting with a letter or digit, at most 40, once per project, never changed |
| `title` | the name | one line, at most 80 characters |
| `purpose` | none | one line, at most 200 characters, sent to every agent answering in the channel |
| `default` | the project's | the agent that answers a message that tags none, or `auto`. One that cannot answer in the project answers nothing, and the message says why |
| `slack` | unbound | `{client, channel}`: a Slack client's id and a Slack channel's (`C...` or `G...`, never a direct message) whose threads are shared threads here. One Slack channel is bound once in the file. The client need not exist yet: until it does the binding binds nothing. The app's bot must already be in the channel |

`experts` names who answers for some topics, here and below: at most 50, each
`{who, topics}`, where `who` is a person's subject or `role:<name>` and
`topics` are 1 to 8 of lower case letters, digits and dashes. For a topic, the
nearest project that names someone answers, then the projects above it.
Tagging `@sme:<topic>` in a thread notifies them, and an agent's question
asked of `sme:<topic>` is addressed to them.

### Tools that act as you

An MCP tool is reached with one of two credentials, as its
`credential_scope` says:

| `credential_scope` | Reached with | When there is none |
|---|---|---|
| `shared` (the default) | its `credential`, the same for every run | nobody's run gets its tools |
| `person` | the credential of the person the run acts for, which each person connects for themselves in the console (Your connections) or with `PUT /api/me/credentials/{tool}` | that person's run gets no tools from it, and the agent is told they have not connected one |

A person-scoped tool names no `credential`, and one that does is refused: a
person with no credential of their own is never given a shared one. Its
server is not reached at start, since there is no credential to reach it
with, so each tool in `accept` carries its `description` and `input_schema`
as the server offers them, and is accepted in that form when its
`schema_hash` is the hash of that schema (`warp config check` prints the
hash it computed when they differ). A stdio one needs `credential_as`.

A run that acts for someone, of an agent granted a person-scoped tool, asks
that person alone what it, or a run it calls, asks with `ask_person`, even
of the project's experts (`asked_of: sme:<topic>`), and takes an edit or a
note on an approval only from them, so nobody else's answer reaches their
tools. An agent's `answerers` still apply, and a called agent's own decide
who answers what it asks: when they leave that person out, nobody may
answer, and the run waits until the question expires or is stopped. Leave
`answerers` unset on an agent granted such a tool and on every agent it may
call, directly or through another, or name everyone who uses it.

That covers what a run is answered. A later turn is sent the conversation
so far, whoever asked or answered its earlier turns, and the transcript
does not say who: an earlier turn's answer drawn from someone else's reply
reads as the agent's own. Read a conversation before continuing it with an
agent granted such a tool, or start a new one
([security](security.md#the-inbox)).

Who a run acts for is decided when it is accepted, never by anything said in
it:

| The run | Acts for |
|---|---|
| asked in the console, or through the API with a person's sign-in | that person |
| asked by the Slack app built on the API for a Slack person its client maps (`act_as`) | the mapped person, never the app. A message the app posts for them into the warp thread their Slack thread is (with its `surface`) is theirs, and so is every run it starts |
| asked about with that app's message shortcut, on someone else's message | nobody: the app asks as itself, since those words are the whole request. Under a dev sign-in, where the app's own credential is the person `dev`, asking as itself acts for `dev`. In a channel bound to a project the app refuses it, since only a person's own words are posted there |
| raised by a Slack client's own connector | the one person the team's Slack clients map that Slack user to; none, or two different people, is nobody. It is started only if that person may start it where it is filed: the policy grants their subject `runs:write` and they are a member of the project. In a channel bound to a project it is not started for nobody |
| started by a message in a shared thread (a tagged agent, `@auto` or the thread's default) | the person who posted it, never another participant, though it is sent what the others said in the room ahead of the poster's words; its retry is the poster's alone, from their inbox |
| Run Now on a schedule | whoever pressed it, when they wrote what it runs (whoever last changed its text, or its creator while nobody has, unless someone else has since chosen its agent or its project, whose instructions and notes go ahead of the text); anyone else's press acts for nobody. When the answer may hold what their own credentials reached, `deliver` does not post it in the project, hold it for the owners or send it to someone else who created the schedule: it goes to them alone, saying why |
| Retry on a failed turn in the inbox, and Retry or Resume in the Slack app | whoever the turn acted for. A turn that acted for a person is retried only by that person, as themselves, and anyone else, an operator included, is refused, except a turn the Slack app asked as itself, which it asks again the same way, by the person who asked or an operator: under a dev sign-in, one that acted for `dev`. A turn that acted for nobody is retried for nobody: in the inbox by anyone who may start runs, in the Slack app by the person who asked or an operator |
| Retry or Regenerate in a console conversation | the person pressing it, as anything they send there, so each is offered only on a turn that acted for them |
| asked by a machine as itself, a schedule on its own clock, an eval, housekeeping, or another agent's call | nobody |
| raised by GitHub | nobody: no client maps GitHub logins to people yet |

A person connects a credential in one of two ways:

- **A reference** (`env:`, `keychain:`, `op://`), which must be the
  person's own under one of the starts the operator lists for the workspace
  in `person_refs`. Only the reference is stored. The server reads it when a
  run uses it, with its own access: its environment, its keychain, the
  1Password account it signed in to. So every start names the person whose
  references it holds with `{subject}`, their sign-in subject, and a person
  may give only a reference under a start with their own subject in it:
  under `op://People/{subject}/`, Ana (subject `ana`) may give
  `op://People/ana/issues` and nobody else may. `{subject}` comes once,
  after a place of the operator's own, and is followed by `/` (by `_` in an
  `env:` start, `env:WARP_PERSON_{subject}_`, and in a `keychain:` start it
  is in the service, `keychain:warp-people-{subject}/`). No two starts may
  share a place, within a workspace or across two, so give each workspace
  its own. A subject is written as it is, except in an `env:` start, which
  writes it in capitals (`ana` gives `env:WARP_PERSON_ANA_ISSUES`). A
  subject with a character a reference cannot hold there (an `env:` start
  takes lowercase letters and digits; the others take letters, digits, `.`,
  `_`, `@` and `-`) can give no reference under it, only a value. Keep
  each person's items under their exact subject, in a place that holds
  nothing else, and in a store that tells names apart by case. An `env:`
  start names a `WARP_` variable, never a sign-in secret. A workspace with no
  entry may reference nothing.
- **A value**, only when the Platform document sets
  `workspace.person_credentials.values: true`. The value is stored, in the
  same secrets store client secrets are kept in: never returned and never
  logged, and with `stores: {kind: postgres}` kept in the database as
  written. Guard the database as you would the credentials, or keep values
  off and use references.

Both are checked again on every run, so a value is not used once values are
turned off, and a reference is not used once it is no longer the person's
own under a listed start.

What a person's tools found is not remembered. Memory is shared: an agent's
notes are sent to every run of that agent, and a project's to every run in
it and below it, whoever the run acts for. So the memory agent is not asked
about a turn whose conversation, up to and including it, has a turn that
acted for a person and was answered by an agent granted a person-scoped
tool, whichever agent answers the turn and whether or not approval is on.

### Schedules

A project's `schedules` run an agent on a cron, filed in the project, so
each run is counted under every limit on its chain and is sent the
project's instructions and notes:

| Field | Left out | Rule |
|-------|----------|------|
| `name` | required | letters, digits, dashes and underscores, at most 63, unique across the deployment: every run it raises carries it |
| `cron` | required | a 5-field cron expression, in UTC |
| `agent` | required | an agent in the file or one of the platform's own, available in the project |
| `text` | required | what the agent is asked, at most 8,000 characters |
| `description` | none | what it is for, at most 500 characters; the agent is not shown it |
| `deliver` | the run | `project` posts each answer in the project as an announcement from the agent, its first line the subject, or as a draft for the project's owners under `agent_posts: review`. An answer of NONE posts nothing. A file has nobody to send an answer to, so `inbox` is refused |

A recap is such a schedule: `warp-analyst`, a Monday morning, "What changed in
Field this week?", `deliver: project`. Its `project_changes` tool reads only
the project and the projects below it.

### Release notes

`workspace.release_notes: true` (off by default) posts what the release of
warp this process runs brings in the workspace's own inbox, as an
announcement from warp: "What's new in warp 0.16.0", then the release's
breaking changes and its features, its Breaking and Features groups in the
changelog the binary carries (`CHANGELOG.md`), and the link to the full
changelog. The release tool files every breaking change under Breaking, a
breaking feature included, so a breaking change of any type is posted.
Fixes are not posted. It reaches everyone who reads the workspace, shows in
What's new and in the workspace's inbox like any announcement there, and
each reader hides it for themselves.

Each release is posted once, at the start that first runs it: a restart, a
later process on the same database, or a withdrawal never posts it again.
With memory stores every start is a new inbox, so it is posted at each
start. A release of fixes alone posts nothing, and so does a build that is
not a release: `dev`, a local build or a pseudo-version. The release is the
version a release build stamps (`-ldflags "-X main.version=v0.16.0"`, as the
Dockerfile does), else the version Go records for warp's module
(`go install github.com/urmzd/warp/cmd/warp@v0.16.0`, or a program that
imports warp at that version). It counts against the workspace's 20
announcements a day; a start past them posts it at the next start. The
Slack app, which connects after the start, does not post it in a channel
bound to the workspace's general.

## Which level a knob lives at

| Concern | Level | Why |
|---------|-------|-----|
| Listen address, public URL | System | belongs to the host |
| Sign-in and the identity provider | System | one per process |
| Stores, bus, files | System | where state lives is the operator's choice |
| Concurrency, depth and fan-out | System | protects the process |
| Variables a workspace may reference | System | the environment is the process's |
| Where people may keep their own tool credentials (`person_refs`) | System | a reference is read with the process's own access |
| Whether people may store a tool credential's value (`workspace.person_credentials`) | Platform | it is the workspace's people's data |
| Release notes in the workspace's inbox (`workspace.release_notes`) | Platform | it is the workspace's people's news |
| Names people see | both | the deployment's, then each workspace's |
| Models and model policies | Platform | what the workspace's agents run on |
| MCP tools and pinned schemas | Platform | what its agents may touch |
| Agents | Platform | the workspace's own |
| Slack and GitHub | Platform | its identity on a surface |
| History | Platform | how it behaves |
| Project tree, kinds, instructions, channels, experts and which agents are available where | Platform | the workspace's own |
| Spend and run limits | Platform, in the console | versioned, scoped policies (`pkg/policy`) |

## Deploying it

`warp deploy` takes the same documents anywhere, so nobody writes compose files
or Helm values by hand:

| `deploy.kind` | What `warp deploy` does |
|---------------|-------------------------|
| `local` (the default) | `warp start` on this machine |
| `compose` | writes a Docker Compose project (warp, and Postgres when asked) and brings it up. Sign-in that only answers on loopback runs on the host's network, so it stays on this machine |
| `kubernetes` | installs the Helm chart warp carries, with the spec as its values, into the cluster the block names |

`warp deploy -o DIR` writes the project or the chart and values and stops, for
review or for GitOps (the Flux example in `deploy/flux` applies the same
chart). A secret never lands in a written file: an `env:` reference is passed
from the deploying shell's environment, and a `keychain:` or `op://` one is
read on this machine and handed to the target as a variable the written spec
references. The `kubernetes` block's `values` are merged over the chart
values warp derives, but may not set `system` or `platforms`: those are the
spec's documents, whose references `warp deploy` carries and checks, so set
them there.

That is for the spec's own references. A person's reference
([Tools that act as you](#tools-that-act-as-you)) is read where warp runs,
when a run uses it, so there is nothing to read or pass when deploying:

- A `keychain:` or `op://` start in `person_refs` names this machine's
  keychain or the 1Password account it signed in to, which a container cannot
  reach. `warp deploy` refuses one for `compose` and `kubernetes`, naming the
  entry. For those targets list an `env:` start instead, let people store
  their credential with warp (`workspace.person_credentials.values: true`), or
  deploy `local`.
- An `env:` start names one variable per person and tool
  (`env:WARP_PERSON_{subject}_` holds Ana's `WARP_PERSON_ANA_ISSUES`), which
  nobody knows when deploying, so `warp deploy` does not pass them, and says
  so in what it prints and in the file it writes. Set each in the target:
  for `compose` in the warp service's environment, for example in a
  `compose.override.yaml` beside the `compose.yaml` it writes, and for
  `kubernetes` in the pod's, for example as `extraEnv` in the block's
  `values`, from a Secret of your own.

## Extending it

| Slot | Built in | A registered kind returns |
|------|----------|---------------------------|
| `system.blobs` | disk, memory, s3 | a `blob.Store` |
| `system.bus` | memory, redis | a `bus.Bus` |
| `system.extensions[]` | none | something with `Run(ctx) error`, run beside the server |
| `platform.models[]` | anthropic, openai, ollama | an `agentrt.ProviderFunc` |
| `platform.tools[]` | mcp | a `runtime.Catalog` |

`warp.WithKind(slot, name, factory)` registers one. The factory is given its
block as JSON and decodes it strictly itself, so its fields get the same
unknown-field errors as the built-ins'. Sign-in and stores are built in only,
for now.
