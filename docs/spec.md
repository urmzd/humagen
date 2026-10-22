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
    default: ["op://People/{subject}/", "keychain:warp-people-{subject}/"]   # how their references may start, {subject} being the person's; none listed is none

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
    baseline: lead
    history: {mode: recent, limit: 8000}
    instructions: Acme sells warehouse robots. Answer plainly.   # the workspace's own, sent first
    projects:               # merged into the tree at every start, parents first
      - {name: Launch, description: Everything for the Q4 launch}
      - name: Field
        title: Field engineering
        kind: team          # company | team | account. Left out: team
        instructions: Customer accounts. Never promise dates.
      - name: Globex
        parent: Field       # a project in this file. Left out: the top level
        kind: account
        account: {customer: Globex Inc, external_id: "sf:0015g00000ABC", url: "https://crm.example.com/globex"}
    person_credentials: {values: false}   # true lets people store a tool credential's value with warp

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
```

### What the file owns

A Platform document is applied at every start, and is the floor the console
builds on:

- **Agents** in the file are created, or revised to the file's spec. An
  unchanged spec makes no new revision. An edit made in the console to an agent
  the file declares lasts until the next start.
- **Workspace** fields that are set are applied; fields left out stay as the
  console left them.
- **Projects** in the file are created, or taken over when the console made
  one by that name, parents first, and are managed by the file. A field the
  file sets is the file's, and the console cannot change it (409); a field
  it leaves out stays the console's. `workspace.instructions` are the
  workspace's own in the same way. An unchanged entry makes no new version.
  A project made in the console survives every start, and one that leaves
  the file stays, no longer managed, so it can be changed or deleted in the
  console again.
- **Models and tools** exist only while the file declares them. A tool's
  pinned schemas are its acceptance; anything else stays pending. A tool's
  `credential_scope` says whose credential reaches it: see
  [Tools that act as you](#tools-that-act-as-you).
- **GitHub** is a client managed by the file, read-only in the API.
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
| `kind` | `team` | `company`, `team` or `account`. A company project sits under the workspace or another company project, and there are at most 10. An account never sits under another account |
| `parent` | the workspace | another project in this file (`~` is the workspace), at most 4 levels below the workspace |
| `account` | none | `{customer, external_id, url}`, on an account project only. `url` is https |
| `instructions` | none | at most 4,000 characters, sent to every agent answering here and below |

`workspace.instructions`, at most 4,000 characters, are sent first, then each
project's down to the one a conversation is filed in. A later one wins, and a
turn is sent at most 8,000 characters of them: the farthest are left out
first, and the turn says which.

`warp config check` refuses a mistake with its path, such as
`workspace.projects[2].parent: "Feild" is not a project in this file`. A
parent must be in the same file, because one made only in the console cannot
be checked before start. A workspace has at most 100 projects, and the start
checks the file again against the projects the console made. A project's kind
and parent are fixed when it is created, so a file that names another kind or
parent for a project that already exists fails the start.

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

Who a run acts for is decided when it is accepted, never by anything said in
it:

| The run | Acts for |
|---|---|
| asked in the console, or through the API with a person's sign-in | that person |
| asked by the Slack app built on the API for a Slack person its client maps (`act_as`) | the mapped person, never the app |
| raised by a Slack client's own connector | the one person the team's Slack clients map that Slack user to; none, or two different people, is nobody |
| Run Now on a schedule | whoever pressed it |
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
| Names people see | both | the deployment's, then each workspace's |
| Models and model policies | Platform | what the workspace's agents run on |
| MCP tools and pinned schemas | Platform | what its agents may touch |
| Agents | Platform | the workspace's own |
| Slack and GitHub | Platform | its identity on a surface |
| Baseline, history | Platform | how it behaves |
| Project tree, kinds and instructions | Platform | the workspace's own |
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
references.

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
