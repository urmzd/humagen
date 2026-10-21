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
    projects:               # merged into the tree at every start, parents first
      - {name: Launch, description: Everything for the Q4 launch}
      - name: Field
        title: Field engineering
        kind: team          # company | team | account | a project_kinds name. Left out: team
        instructions: Customer accounts. Never promise dates.
        agents: {default: lead}             # answers untagged messages here and below
        members: [{who: "*", role: viewer}, {who: "role:field-eng", role: member}, {who: "role:field-leads", role: owner}]
      - name: Globex
        parent: Field       # a project in this file. Left out: the top level
        kind: account
        account: {customer: Globex Inc, external_id: "sf:0015g00000ABC", url: "https://crm.example.com/globex"}
        channels:
          - {name: renewals, purpose: Renewal work, default: auto}   # auto: @auto answers untagged messages here
        experts: [{who: "okta|00u1ana", topics: [billing, renewals]}]
        agents: {allow: [globex-helper, lead], default: globex-helper}  # narrows: only these run here and below
        members: [{who: "*", role: viewer}, {who: "okta|00u1ana", role: owner}]   # "*" required until reads are confined

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
  console again. A project's **channels** and **experts** are the file's in
  the same way: a channel the file declares is created or taken over, and
  one that leaves it stays, unmanaged; experts the file sets, even as `[]`,
  are the file's, and experts it leaves out stay the console's.
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
  pinned schemas are its acceptance; anything else stays pending.
- **GitHub** is a client managed by the file, read-only in the API.
- Nothing is deleted because it left the file.

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

`experts` names who answers for some topics, here and below: at most 50, each
`{who, topics}`, where `who` is a person's subject or `role:<name>` and
`topics` are 1 to 8 of lower case letters, digits and dashes. For a topic, the
nearest project that names someone answers, then the projects above it.
Tagging `@sme:<topic>` in a thread notifies them, and an agent's question
asked of `sme:<topic>` is addressed to them.

## Which level a knob lives at

| Concern | Level | Why |
|---------|-------|-----|
| Listen address, public URL | System | belongs to the host |
| Sign-in and the identity provider | System | one per process |
| Stores, bus, files | System | where state lives is the operator's choice |
| Concurrency, depth and fan-out | System | protects the process |
| Variables a workspace may reference | System | the environment is the process's |
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
