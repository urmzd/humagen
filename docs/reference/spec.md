# The Humagen spec

Humagen is configured in one language: versioned YAML documents (JSON works too)
of two kinds, each with one owner.

| Kind | Says | Owned by | Takes effect |
|------|------|----------|--------------|
| `System` | how and where Humagen runs: listeners, sign-in, stores, bus, files, limits, extensions | whoever runs the process: a laptop, compose, Helm values | at start |
| `Platform` | what one workspace offers: its models, tools, agents, Slack and GitHub, settings | that workspace's admins | at start, with the console building on it |

Both can live in one `humagen.yaml`, separated by `---`, or in separate files.
Two commands use it. `humagen start` runs the platform here; `humagen deploy` runs it
where the System document's `deploy` slot says (here, a Docker Compose project,
or a Kubernetes cluster). Both read `-f PATH` (repeatable; a directory reads its
`*.yaml`, `*.yml` and `*.json`). With no `-f` they read `humagen.yaml` from
`HUMAGEN_HOME`, else the user config directory, and `humagen start` runs on defaults,
with example agents, if there is none. `humagen setup` writes one by asking a few
questions.

`humagen serve` is the command for a server: what the image, the chart and the
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
   string where a secret belongs is refused. `env:HUMAGEN_X` reads `HUMAGEN_X`,
   else the old name `WARP_X`, and `env:WARP_X` (the old prefix) reads the same
   variable ([upgrading-from-warp.md](../guides/upgrading-from-warp.md#settings)).
3. **Every replaceable part is a slot: `kind` plus a block named after it.**
   `kind: postgres` reads `postgres: {...}`. A program built on the `humagen` package
   registers more kinds (`humagen.WithKind`), and the file selects one by name
   with no other change. An unknown kind is an error listing the kinds the
   binary has. A choice is a file or an interface, never a fork.
4. **Unknown fields are errors**, with the document and path they are in.

Flags still work and win over the file, so `humagen start -addr 127.0.0.1:9090`
needs no edit. `humagen config check -f humagen.yaml` validates without starting and
lists every `env:` reference the shell it runs in has not set; `humagen config
print` writes the result with every default filled in.

## System

```yaml
api_version: humagen/v1
kind: System
metadata:
  name: humagen                # the deployment's identifier
  display_name: Humagen        # what people see: console title, Slack app name
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
      client_id: humagen
      client_secret: {ref: env:HUMAGEN_OIDC_CLIENT_SECRET}
      audience: humagen
    tenant: {fixed: default}                 # or {claim: org}
    groups: {claim: groups, roles: {admin: [platform-admins], operator: [staff]}}   # admin, manager, operator, viewer
    policy_file: ""                          # an authorization policy (docs/architecture/security.md)

  stores:                   # slot: memory | postgres. Every store family
    kind: postgres
    postgres: {url: {ref: env:HUMAGEN_POSTGRES_URL}}

  bus:                      # slot: memory | postgres. Leave it out and it
    kind: postgres          # follows the stores: postgres on Postgres stores

  blobs:                    # slot: disk | memory | s3. Published files and
                            # memory's content. With memory, memory's content
                            # is kept with the stores instead
    kind: s3
    s3:
      bucket: humagen-files
      region: us-east-1
      endpoint: ""          # set for MinIO or R2
      prefix: ""
      path_style: false
      access_key: {ref: env:HUMAGEN_S3_ACCESS_KEY_ID}       # both left out: the AWS chain
      secret_key: {ref: env:HUMAGEN_S3_SECRET_ACCESS_KEY}

  knowledge:
    embedder:               # slot: none | hashing | openai | vertex. Left out: none, lexical search only
      kind: vertex
      vertex: {project: my-gcp-project, location: us-central1}   # model gemini-embedding-001 and 768 dimensions by default
      # kind: openai
      # openai: {base_url: "http://127.0.0.1:11434/v1", model: nomic-embed-text, dimensions: 768, key: {ref: env:EMBEDDINGS_KEY}}
      # kind: hashing
      # hashing: {dimensions: 256}   # no model: matches words and word stems, not meaning

  execution:
    workspace: ""           # agents' files. Empty is the user cache directory
    concurrency: 4          # a run that calls another agent waits in one place while it runs in another
    max_depth: 4            # 1 to 7: a deeper call would pass the bus's hop limit of 8
    max_fan_out: 16
    builtin: {provider: "", model: ""}       # the platform's own agents

  sandbox:                  # slot: docker | local | unisolated. Where a run's tools execute
    kind: docker            # the default: an instance per run, destroyed when the run ends
    docker:
      runtime: runsc        # gVisor, the default; runc shares the host's kernel and is for trusted tools
      insecure_development_runtime: false   # true accepts runc; without it runc is refused
      image: python:3.13-alpine           # the shell and the file tools; needs sh and sleep
      connectors_image: node:22-alpine    # stdio MCP servers; empty is image
      work_root: ""         # empty is humagen/sandbox in the user cache directory
      host: ""              # a sandbox host's daemon: tcp://, ssh:// or unix://. Empty is the local daemon
      tls_dir: ""           # with a tcp:// host: ca.pem, cert.pem and key.pem
      warm: 1               # instances kept started ahead of runs, per shape
      max: 64               # every instance at once

  egress:                   # slot: none | gate. How an instance reaches the network
    kind: gate              # none, the default: no network at all
    gate:
      listen: 10.0.9.2:3128 # where the gate listens; empty is 127.0.0.1:3128
      address: 10.0.9.2:3128   # the gate as an instance reaches it; empty is listen
      network: humagen-egress  # docker: the --internal network instances join
      allow_non_public: false  # true lets an allowed host resolve to a private address

  rooms:                    # a conversation with many readers (RFC 0004); each default is far above an ordinary one
    coalesce_above: 200     # readers on one server above which each is sent at once only what involves them, the rest as a page
    tail_interval: 3s       # how often a large conversation's page is sent: 500ms to 1m
    max_runs_per_minute: 600   # runs one conversation starts in any minute
    max_runs_per_person: 25    # runs one person has in flight at once

  secret_env:               # variables a workspace's clients may reference, by tenant
    default: [HUMAGEN_SLACK_TOKEN]

  person_refs:              # where each workspace's people may keep their own tool credentials, by tenant
    default: ["env:HUMAGEN_PERSON_{subject}_"]   # how their references may start, {subject} being the person's; none listed is none
    # deploy local (this machine) may also list "op://People/{subject}/" and "keychain:humagen-people-{subject}/"

  branding:                 # the console's look and name; see Branding below
    name: Acme              # the product's name. Left out: metadata.display_name
    logo: {path: ./brand/logo.svg, dark: ./brand/logo-dark.svg, alt: Acme}
    icons: ./brand/icons    # favicon.svg, icon-192.png and the rest, by the console's own names
    manifest: {short_name: Acme, theme_color: "#1a1a2e", background_color: "#fafafa"}
    palette:
      accent: {light: "#c2185b", dark: "#f06292"}
      surface: {light: "#fafafa", dark: "#1a1a2e"}
      text: {light: "#1a1a2e", dark: "#fafafa"}
    fonts:
      ui: "Inter, sans-serif"
      heading: "Georgia, serif"
      files: [{family: Inter, path: ./brand/InterVariable.woff2, weight: "100 900"}]   # served for the lists to name
    email: {subject: "{name} updates", multiple: "{count} updates wait for you in {name}."}

  notifications:            # optional outbound delivery; people must also opt in
    email:
      address: smtp.example.test:587
      from: Humagen <notifications@example.test>
      username: notifications
      password: {ref: env:HUMAGEN_SMTP_PASSWORD}
      tls: starttls         # default; implicit also requires verified TLS
      recipients:
        default:
          "issuer|person-subject": person@example.test

  extensions: []            # process-wide parts a binary registered, each a slot

  deploy:                   # slot: local | compose | kubernetes. Read by humagen deploy
    kind: compose
    compose:
      image: ""             # empty builds one from `build`, a humagen checkout
      postgres: true        # run Postgres beside humagen and point the stores at it
      port: 8080
    # kind: kubernetes
    # kubernetes: {context: prod, namespace: humagen, host: humagen.example.com, image: {repository: ghcr.io/urmzd/humagen, tag: v0.10.0}}
```

Email recipient mappings use the exact authenticated tenant and subject. Delivery
checks current standing permissions and preferences. It sends a readable update
count and an inbox link, with event kinds and email off by default. The explicit
`insecure_loopback` TLS mode accepts only literal loopback addresses without
credentials. See [Email notifications](../guides/email-notifications.md) for quiet hours,
retry behavior and consent records.

### Sandbox

Every run's tools execute in an instance of the run's own (RFC 0004): the
file tools work in its work directory, the `shell` tool runs there, and stdio
MCP servers run in its connectors compartment, which holds their credentials
where code in the workload cannot read them. The agent loop and the model
keys stay in the server. An instance is leased on a run's first tool, never
shared with another run (a run another agent calls gets its own), and
destroyed when the run ends, however it ends. Its work directory starts from
the conversation's files and is written back to them at the end, so a
follow-up turn sees what the last one wrote.

| `sandbox.kind` | Where tools run | Use |
|----------------|-----------------|-----|
| `docker` (default) | Containers with no network, a read-only root, no capabilities, a non-root user, a seccomp profile stricter than Docker's default and resource limits, on `runtime: runsc` (gVisor) unless it says `runc` with `insecure_development_runtime: true` | Production |
| `local` | Host processes, with the server's privileges, in a directory laid out like an instance. The shell works | Development without Docker |
| `unisolated` | This host, as before the sandbox: files in the conversation's directory, stdio servers as host processes. The shell is refused | Development and tests |

The server says at start which one it runs. `local` and `unisolated` are
never chosen for you: name them in the spec or with `-sandbox`. The Docker
daemon must have `runsc` registered for the default. `runc` shares the host's
kernel, so a kernel bug is a way out of every instance: it is refused unless
`insecure_development_runtime` (or `-sandbox-insecure-development-runtime`)
is set, and then the server warns at start. `humagen start`, the `make dev`
targets and the Tilt stack use `docker` with `runc`, the runtime Docker has
on a developer's machine, and set it for you. A `docker` sandbox whose CLI is
missing, or whose runtime is refused, does not stop the server: it logs why,
and every tool that needs an instance refuses with that reason; nothing falls
back to the host. A server in a
container needs the Docker CLI and a daemon it may use (a mounted socket) to
run tools. **Giving Humagen the Docker socket is root on that host**:
whoever can start containers through it can mount the host's filesystem. So
the `docker` driver is for a host that exists to run sandboxes, not a shared
server or a Kubernetes node; the chart and the compose stack set
`unisolated` explicitly for that reason.

`host` points the `docker` sandbox at a daemon on a sandbox host instead,
so Humagen holds no Docker socket on its own machine: whoever takes over
Humagen gets the sandbox host, not the one with the database. A `tcp://`
host needs `tls_dir`, the directory with the daemon's certificate authority
(`ca.pem`) and Humagen's client certificate (`cert.pem`, `key.pem`); a
daemon on plain TCP is refused, because it is root for anyone who reaches
the port. With a host, a run's work directory is a volume on the sandbox
host, `work_root` is unused, and host directories cannot be mounted. The
sandbox host needs Docker with `runsc` registered and nothing else worth
taking. An instance has no network at all unless the
`egress` slot is `gate` ([Egress](#egress)).

Flags override the block: `-sandbox`, `-sandbox-runtime`,
`-sandbox-insecure-development-runtime`, `-sandbox-image`,
`-sandbox-connectors-image`, `-sandbox-work-root`, `-sandbox-host` and
`-sandbox-tls-dir`. A binary can register
another kind with `humagen.WithKind(humagen.Sandbox, name, factory)`, whose
factory returns an `isolation.Leaser` (`fiber/pkg/isolation`).

### Egress

`egress` decides how a sandbox instance reaches the network. Left out, it is
`none`: an instance has a loopback interface and nothing else, so a tool that
calls an API cannot reach it.

`gate` runs playscape's egress gate in the server: an HTTP forward proxy that
is every instance's only way out. Each run's instance is granted the hosts its
`ceilings.egress` allows ([Run settings](#run-settings)), default deny, and
every command in it, the shell and stdio MCP servers alike, gets `HTTP_PROXY`
and `HTTPS_PROXY` with that grant's token. A request for any other host gets
`403` before anything is dialed. HTTPS goes through as a `CONNECT` tunnel, so
the gate sees the host and port, never the content. The grant ends when the
run's instance is destroyed. Addresses that are loopback, private or
link-local are refused whatever name led to them, unless
`allow_non_public` is set.

Every decision, allowed or denied, is written to the workspace's audit log as
`sandbox.egress` on resource `egress_host`, with the run, its agent and
conversation, the method, host and port, and why a request was refused. A
denied request is a failed record.

| Sandbox | What forces the gate |
|---------|----------------------|
| `docker` | The instance joins `network`, which must be created with `--internal` (`docker network create --internal humagen-egress`), so it has no route out. The gate must be reachable on that network at `address`: run the server in a container attached to it, and give the server's IP address on it, since gVisor cannot always resolve names there. A network that is not internal is refused |
| `local` | Nothing: the processes are the server's own, and only the proxy variables send them to the gate. `listen` and `address` on loopback are enough |
| `unisolated` | Nothing runs in an instance, so the gate is not started and the server says so |

Credential injection, where the sandbox holds a placeholder and the gate
swaps in the real value for an allowed host, is playscape's and not wired
here yet.

### Knowledge search

`knowledge.embedder` turns on hybrid search for knowledge sources
([RFC 0005](../rfcs/0005-federated-retrieval-and-catalog.md)). Each passage
gets a vector, computed in the background after it is added, so an upload never
waits on a model. Search then ranks passages both by vector similarity and with
BM25, and fuses the two lists by reciprocal rank fusion. It finds passages that
share no word with the query and still puts an exact identifier first.

| Kind | Block | Credentials |
|------|-------|-------------|
| `none` | none | Lexical only, the default |
| `hashing` | `dimensions` (256 when left out) | None: deterministic, no network. Matches word stems, not meaning |
| `openai` | `base_url`, `model`, `dimensions` (required), `key` | `key`, a reference, for any OpenAI-compatible `/embeddings` endpoint |
| `vertex` | `project` (required), `location`, `model`, `dimensions` | Application Default Credentials: the VM's service account on GCE, `gcloud` credentials locally |

Vectors are stored beside the passages and read back at start. Changing the
model or its dimensions computes them again; until then those passages rank
lexically. If the embedder fails, writes still succeed and search is lexical.
Search sees exactly the documents it would without vectors: the same sources,
exclusions, variants and access checks.

### Catalog connectors

`connectors` in a Platform document are the systems the catalog lists
([RFC 0005](../rfcs/0005-federated-retrieval-and-catalog.md)): tables,
columns and their descriptions, never data. Each is crawled on its `refresh`
cadence (24h when left out), and a crawl replaces its entries whole, so a
dropped table disappears and a failed crawl keeps the entries before it.
Entries are searched like knowledge, with `knowledge.embedder` when one is set.

| Field | Meaning |
|-------|---------|
| `owner` | The person whose credential the connector uses. Required. They see its entries, may publish them, and may query it |
| `project` | Whose members see its entries while the owner publishes them (`POST /api/catalog/connectors/{name}/publish`). Left out, the workspace |
| `refresh` | How often it is crawled, at least `1h` |

| Kind | Block | Credentials |
|------|-------|-------------|
| `bigquery` | `project` (required), `datasets`, `location`, `max_bytes_billed` (1 GiB), `max_rows` (200, at most 10000), `timeout` (60s, at most 10m) | `credential`, a reference to a service account key in JSON; left out, Application Default Credentials: the VM's service account on GCE |

Two tools use the catalog, granted to an agent by name like any tool:

- `catalog_search` finds entries: a published connector's when everyone the
  answer reaches reads its project, and the owner's own only in the owner's
  private conversation.
- `catalog_query` runs one SQL `SELECT` with the connector's credential, so
  only for its owner, in their own private conversation. BigQuery dry-runs it
  first and refuses it before it runs when it is not a single `SELECT`, reads a
  dataset outside `datasets`, or would read more than `max_bytes_billed`. The
  query that runs carries the same cap as BigQuery's maximum bytes billed.

The credential needs `roles/bigquery.metadataViewer` (or `dataViewer`) on the
datasets to crawl them, `roles/bigquery.dataViewer` to read them, and
`roles/bigquery.jobUser` on `project` to run queries. Give it no write role:
the dry run's statement check is the first guard, the credential the last.

### Rooms

`rooms` protects the server from a conversation with a great many readers
(RFC 0004, "A million people in one room"). A conversation's stream
(`GET /api/events?thread=<id>`) is sent every event as it happens while the
conversation has at most `coalesce_above` readers on the server. Past that,
each reader is sent at once only the messages they posted or are named in,
the runs they asked for and the questions put to them, and the rest as one
page every `tail_interval`, which says how much happened; the console then
reads the conversation's newest turns. What one server delivers each second
is then bounded by the events that involve someone plus the readers divided
by the interval, whatever the conversation's message rate.

`max_runs_per_minute` and `max_runs_per_person` are admission limits beside
a workspace's policies: a run past either is refused with 429 and a
`Refusal` whose `limit` is `room_rate` or `person_runs`, and a tag in a
message past them is refused in the message's `refused`. Only a run a person
or a surface started counts: a call between agents is its root's work, and a
schedule's or an eval's run is asked by nobody. With `stores.kind: postgres`
the counts are shared by every replica.

## Platform

One document per workspace. `metadata.name` is its tenant.

```yaml
api_version: humagen/v1
kind: Platform
metadata:
  name: default
  display_name: Acme
spec:
  workspace:                # a field left out is left to the console
    history: {mode: recent, limit: 8000}
    agents: {enable: ["*"], default: "*"}   # the workspace's own: every platform agent, and each decides on untagged messages
    instructions: Acme sells warehouse robots. Answer plainly.   # the workspace's own, sent first
    channels: [{name: announcements, purpose: Company news}]      # the workspace's own; every project has general
    experts: [{who: "role:security", topics: [security]}]         # answer anywhere a nearer project names nobody
    knowledge: [handbook]   # every run in the workspace is grounded in these sources, besides its agent's own
    skills:                 # a message anywhere names one with $summarize, and the agents it starts use it
      - {name: summarize, description: Summarize a thread in five bullets., instructions: "Summarize the conversation so far in five bullets, newest last."}
    mention_kinds:          # @oncall:sre notifies everyone holding sre, besides the built-in @sme: and @skill:
      - {kind: oncall, title: On call, resolves: role}
    members: [{who: "*", role: member}, {who: "role:comms", role: owner}]   # the workspace's own
    project_kinds: [{name: partner, title: Partner, like: account}]          # the workspace's own kinds
    agent_posts: review     # what agents scheduled in the workspace itself post waits for its owners
    run:                    # every run's settings: ceilings bound it, preferences choose within them
      ceilings: {models: ["anthropic/*", "gemini/*"], call_depth: 2, tool_servers: [mcp.acme.example], egress: [api.github.com, "*.pypi.org"]}
      preferences: {model: anthropic/claude-sonnet-4-5}
    projects:               # merged into the tree at every start, parents first
      - {name: Launch, description: Everything for the Q4 launch}
      - name: Field
        title: Field engineering
        kind: team          # company | team | account | a project_kinds name. Left out: team
        instructions: Customer accounts. Never promise dates.
        agents: {default: lead}             # answers untagged messages here and below
        knowledge: [field-playbook]         # runs filed here and below are grounded in it too, nearest first
        skills:                             # $review here and below; a nearer project's of the same name wins
          - {name: review, description: Review a renewal., argument_hint: "[account]", instructions: Check the renewal terms against the contract and list what changed.}
        members: [{who: "*", role: viewer}, {who: "role:field-eng", role: member}, {who: "role:field-leads", role: owner}]
        agent_posts: publish  # what its scheduled agents post goes out at once
        run:                  # tightens the workspace's for runs filed here and below
          ceilings: {tools: [file_read, file_list, "mcp:github"], call_depth: 1}
          preferences: {compaction_tokens: 120000}
        schedules:            # the file's: filed here, paused and run in the console, changed only here
          - {name: field-recap, cron: "41 8 * * 1", agent: humagen-analyst, text: "What changed in Field this week?", deliver: project}
      - name: Globex
        parent: Field       # a project in this file. Left out: the top level
        kind: account
        account: {customer: Globex Inc, external_id: "sf:0015g00000ABC", url: "https://crm.example.com/globex"}
        channels:
          - {name: renewals, purpose: Renewal work, default: "*"}   # everyone decides: each agent available asks itself whether to answer
          - {name: general, slack: {client: slack-acme, channel: C0123GLOBEX}}  # a Slack thread there is a shared thread here
        experts: [{who: "okta|00u1ana", topics: [billing, renewals]}]
        agents: {allow: [globex-helper, lead], default: globex-helper}  # narrows: only these run here and below
        members: [{who: "okta|00u1ana", role: owner}]   # no "*": members only, read by its members alone
    person_credentials: {values: false}   # true lets people store a tool credential's value with humagen
    release_notes: true     # each release's breaking changes and features, posted once in the workspace's inbox from humagen
    inbox_retention: 180d   # how long the inbox keeps what was settled for everyone. Left out: 90d. At least 7d

  models:                   # each a slot: anthropic | openai | google
    - name: anthropic       # what an agent names as its provider
      kind: anthropic
      anthropic: {key: {ref: env:ANTHROPIC_API_KEY}}
    - name: gemini
      kind: google          # Gemini; vertex: {project: P, location: us-central1} for Vertex AI
      google: {key: {ref: env:GOOGLE_API_KEY}}
    - name: local
      kind: openai          # any OpenAI-compatible server, such as Ollama (http://127.0.0.1:11434/v1) or vLLM
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

  connectors:               # each a slot: bigquery. What the catalog lists, and agents query in place
    - name: warehouse
      kind: bigquery
      owner: ana@acme.example   # whose credential it is: they see its entries and may query it
      project: Field        # whose members see its entries once the owner publishes them
      refresh: 24h          # how often it is crawled, at least 1h
      bigquery:
        project: acme-analytics
        datasets: [billing, web]   # the catalog and every query keep to these
        max_bytes_billed: 1073741824   # one query reads at most this; refused before it runs otherwise
        max_rows: 200
        # credential: {ref: env:BQ_KEY}   # a service account key; left out, the VM's service account

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
    - file: agents/reviewer.agent.md   # saige's own format; a .json or .yaml file holds a spec instead
      policy: {answerers: ["role:field-leads"], tagged_only: true}   # what the .agent.md does not say

  surfaces:
    slack:                  # humagen runs the Slack app as its client, over Socket Mode
      enabled: true
      team: T0123456
      bot_token: {ref: env:SLACK_BOT_TOKEN}
      app_token: {ref: env:SLACK_APP_TOKEN}
      operators: [U0123456]
      command: /agent       # `humagen slack manifest -f humagen.yaml` writes the app's manifest
      description: Acme's agents in Slack     # the manifest's; each left out is Humagen's
      background_color: "#1a1a2e"
      command_description: Ask Acme's agents
      command_usage_hint: "[question]"
      humagen_client:          # how the app signs in to humagen's API. Dev sign-in needs none
        token_url: https://login.example.com/oauth2/v1/token
        client_id: humagen-slack
        client_secret: {ref: env:HUMAGEN_SLACK_CLIENT_SECRET}
        scopes: [humagen:agents:read, humagen:runs:read, humagen:runs:write, humagen:billing:read, humagen:projects:read]
        audience: ""        # set when the provider needs one named
      # humagen_token: {ref: env:HUMAGEN_SLACK_API_TOKEN}   # instead: a bearer token that does not expire
    github:
      enabled: false
      owners: [acme]
      token: {ref: env:HUMAGEN_GITHUB_TOKEN}
      webhook_secret: {ref: env:HUMAGEN_GITHUB_WEBHOOK_SECRET}
      login: humagen[bot]      # the app's own login; see github.md, "Who a comment asks"

  branding:                 # this workspace's, over the System document's, once its people sign in
    wordmark: Acme Field
    palette: {accent: "#2e9e8f"}   # one color is both themes'
    welcome: {title: "Welcome to {name}", text: Ask the field team anything. Your drafts stay private until you share them.}
    starters:               # replace the suggestions on an empty conversation; [] shows none
      - {agent: lead, label: Plan the week, text: "Plan this week's customer visits for "}
    copy:                   # the console's own text, by key: humagen config copy-keys lists them
      navigation.projects: Accounts
```

### Branding

`branding` is how the console looks and what it calls itself. Everything left
out is Humagen's own. The System document's is the deployment's, and it alone
covers what is served before anyone signs in; a Platform document's is that
workspace's, field by field over the deployment's, applied once a person of
the workspace signs in.

| Field | What it changes | Where |
|-------|-----------------|-------|
| `name` | The product's name: the tab title, the sidebar's wordmark and the installed app's name, unless each is set itself. Left out, `metadata.display_name` | both |
| `title` | What follows the page's own name in the browser tab, after ` · ` | both |
| `wordmark` | The text beside the logo in the sidebar. `""` shows the logo alone | both |
| `logo` | `{path, dark, alt}`: an `.svg`, `.png`, `.webp` or `.jpg` of at most 512 KiB in place of Humagen's mark. `dark` is the same kind of file drawn in the dark theme; left out, `path` is drawn in both | both |
| `icons` | A directory of files that replace the console's icons by name: `favicon.svg`, `favicon-32.png`, `apple-touch-icon.png`, `icon.svg`, `icon-192.png`, `icon-512.png`. `favicon-dark.svg` and `favicon-dark-32.png` are added for the tab while the device is in its dark theme; an SVG favicon may instead carry its own `prefers-color-scheme` style. One it lacks stays Humagen's; any other name is an error | System |
| `manifest` | The installable app's `name`, `short_name`, `description`, `theme_color` and `background_color` | System |
| `palette` | `accent`, `surface` (the page), `plane` (the sidebar and panels), `text` and `rule` (lines), each `{light, dark}` or one color for both themes | both |
| `fonts` | CSS font-family lists for `ui`, `heading` and `mono` text, and `files`: up to 12 `{family, path, weight, style}` faces (`.woff2`, `.woff`, `.ttf` or `.otf`, at most 2 MiB each) the server serves for those lists to name. A face named without a file must be installed on the device. A workspace's files replace the deployment's | both |
| `welcome` | `{title, text}` of the announcement a workspace's people find first in its inbox. Left out, Humagen's, with the product's name in place of Humagen. It is posted once, so a change reaches only a workspace not yet welcomed | both |
| `starters` | Up to 6 `{agent, label, text}` suggestions on an empty conversation, each shown only where its agent can answer. `[]` shows none | both |
| `copy` | The console's own text by key, such as `navigation.agents: Assistants`. `humagen config copy-keys` lists the keys; an unknown one is an error. A workspace's keys are merged over the deployment's | both |
| `email` | What notification emails say: `subject`, `single` (one update), `multiple` (with `{count}`) and `footer`. `{name}` is the product's name in each. Left out, Humagen's under the product's name | both |

Colors are hex, `#rgb` or `#rrggbb`. Text must reach 4.5:1 on its surface and
the accent 3:1 in each theme (WCAG AA), against Humagen's own for a side left
out; a workspace's palette is checked over the deployment's. The product's own messages in the inbox are shown from its name. The accent is
the console's default: a person still picks their own under Settings, where
Workspace returns to it. Paths are read at start, from the working directory,
like `agents[].file`, so a missing file, an SVG carrying a script, or an icon
under a name the console does not have fails the start rather than a page.

The server writes the deployment's name, theme color and stylesheet into the
console's page and manifest as it serves them, so nothing is rebuilt and the
checkout stays clean. A workspace's reaches the console through
`GET /api/branding`.

Some of a workspace's identity lives outside Humagen. A Slack app's icon and
its bot's picture are set on the app's page at api.slack.com: Slack's
manifest has no field for them. A GitHub App's name and avatar are its
registration's on GitHub, and every review comment and check run carries
them; `surfaces.github.login` only tells Humagen what that login is.

### What the file owns

A Platform document is applied at every start, and is the floor the console
builds on:

- **Agents** in the file are created, or revised to the file's spec. An
  unchanged spec makes no new revision. An edit made in the console to an agent
  the file declares lasts until the next start. `auto` and `sme` are reserved,
  so never an agent's name (`humagen config check` refuses one written inline, the
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
  are the file's, and experts it leaves out stay the console's. The
  **knowledge** sources a project or the workspace names, and the
  **skills** it declares, are the same: the file's when it sets them, even
  as `[]`, and the console's when it leaves them out. A project's **schedules** are the file's whole: created, or taken over when
  the console made one by that name, filed in the project; the console can
  pause, resume and run one, not change or delete it (409), and one that
  leaves the file stays, the console's again. Schedules are applied last,
  once the agents and where they live are in.
- **Agents in projects.** `workspace.agents` and a project's `agents` are
  what it turns on: `enable` adds platform agents here and below (`*` alone
  is every one), `allow`, when set, is the only agents that may run here and
  below, and `default` answers a message that tags no agent (an agent, or
  `*` for everyone decides: each agent available there that is not
  `tagged_only` is asked whether to join, see
  [the dynamic router](../architecture/projects.md#the-dynamic-router)). `auto` still works
  and reads as `*`, with a load note asking to write `"*"`. Each list names agents in the file or the platform's
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

### Agent files

An agent is a `spec` inline, or a `file`. A file ending in `.agent.md` is a
saige definition: YAML frontmatter (`apiVersion: saige/v1`, `name`,
`version`, `model.use: provider/model`, `tools`, `subagents`,
`approval.ask`), then the system prompt. Any other file holds a spec as JSON
or YAML.

```markdown
---
apiVersion: saige/v1
name: reviewer
version: 1.0.0
description: Reviews changes.
model:
  use: anthropic/YOUR_MODEL
---
Review the change and list what is risky.
```

`policy` goes beside an `.agent.md` file, and only there. It holds what
saige's format does not: `approvers`, `self_approval`, `answerers`,
`tagged_only`, `workspace_areas`, `mcp_write` (the MCP servers whose write
tools it may call), `knowledge` and `fallback_on`. Left out, it grants
nothing beyond the file. `subscriptions` is refused: the platform decides
what wakes an agent. saige checks the file at start, then the platform's
own rules apply as to any spec. The file's `version` is not kept: the store
numbers each change, and a run pins the version, digest and policy it
started with.

### The Slack app

`surfaces.slack` runs the Slack app (`apps/slack`) beside the server as a
client of its API, restarting it when it exits. It needs Node.js on `PATH` and
`apps/slack` with its dependencies installed (`pnpm install`), found under the
working directory, beside the binary, or at `HUMAGEN_SLACK_APP_DIR`. The Humagen image
carries neither, so there the surface is logged as off and the server runs
without it.

The app is given its tokens, its workspace and Humagen's address, plus `PATH`,
`HOME`, `TMPDIR`, `TZ`, `XDG_STATE_HOME`, the `NODE_OPTIONS`,
`NODE_EXTRA_CA_CERTS`, `NODE_USE_ENV_PROXY` and proxy variables, and
`SLACK_BRANDING` and `SLACK_SOCKET_*`, when they are set. Nothing else of
Humagen's environment reaches it.

`humagen slack manifest -f humagen.yaml` writes the app's manifest from the
surface: its name (`app_name`, else the workspace's or deployment's
`display_name`), `command`, `description`, `background_color`,
`command_description` and `command_usage_hint`, each Humagen's when left out.
What the app says in Slack itself (its tagline, disclaimer and suggested
prompts) is the `SLACK_BRANDING` file's. The app's icon and its bot's
picture are set on the app's page at api.slack.com.

It calls Humagen's API with a credential for the server's sign-in. Under `dev` it
needs none. Under `oidc` or `mock` it needs `humagen_client`: a machine client of
the identity provider, whose access tokens the app gets with client credentials
and renews as they expire, with the scopes [the API](api.md#authenticating-a-machine)
lists for a Slack surface. `humagen_token` is a bearer token given as it is, for a
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
| `knowledge` | the console's | at most 8 knowledge sources, each named once: lower case letters, digits and dashes, starting with a letter or digit, at most 63. Runs filed here and below are grounded in them, see below |
| `skills` | the console's | at most 32 skills, each `{name, description, argument_hint, instructions}`: a name of lower case letters and digits in words joined by single dashes, starting with a letter, at most 64, once; a description on one line of at most 1,024 characters; an optional argument hint of at most 100; instructions of at most 8,000. A message posted here and below names one with `$<name>`, see below |
| `schedules` | none | agents scheduled here, see below |
| `members` | the console's | at most 100 `{who, role}`: `who` is `*` (everyone who can see the parent), `role:<name>` (the holders of a policy role) or a subject; `role` is `viewer`, `member` or `owner`. An owner owns everything below. Until reads are confined to members, the list has an entry for `*` of at least `viewer`, a company project's of at least `member`, and a project is never more open than its parent. A new project the file leaves them out of has everyone as a member (a viewer of an account) and `projects:govern` as its owner |

`workspace.members` are the workspace's own in the same way: everyone (`*`)
is at least a member of it, and `projects:govern` owns it whatever they say.
`workspace.project_kinds` are at most 16 kinds of the workspace's own, each
`{name, title, like}`: a name of lower case letters, digits and dashes that is
not a built-in kind, a title of at most 40 characters, and `like` either
`team` or `account`. Behaviors stay Humagen's; names are the workspace's. A kind
a project still has cannot be removed: a start that would fails, naming the
projects.

`workspace.instructions`, at most 4,000 characters, are sent first, then each
project's down to the one a conversation is filed in. A later one wins, and a
turn is sent at most 8,000 characters of them: the farthest are left out
first, and the turn says which.

`knowledge` names knowledge sources every run filed in the project and below
it is grounded in, besides its agent's own `sources`. A run is given its
project's, then each project's above it, `workspace.knowledge` last, nearest
first, each once, at most 16; never a sibling's or a sub-project's. They are
fixed when the run is accepted, so a change made while it works, or a restart,
does not change what it is grounded in. Sources are filled in the console,
so the file cannot check that the workspace has them: one it does not have
grounds nothing until it does. Through the API, naming one takes an owner of
the project who holds `knowledge:read`.

`skills` are named instructions a person can tell an agent to use: writing
`$<name>`, or `@skill:<name>`, in a message has every run the message starts
given the skill's instructions ahead of the turn, below the project's and the
agent's own. A message posted in a project can name its project's skills, then
each project's above it, `workspace.skills` last, nearest first, each name
once; never a sibling's or a sub-project's. A skill nobody on the way declares
refuses the message. A message a person posts in the console, as themselves
and not posted only, that names a skill and tags no agent asks the agents
whether to join, and each that joins is told to use it; one
a client posts is answered by the client's default with the skill. The skills
are fixed when a run is accepted, so a change made while it works, or a
restart, does not change what it was told. A skill's name and description are
ones a SKILL.md takes. A Slack client's command whose name is a skill's uses
that skill. Through the API, declaring one takes an owner of the project.

`workspace.mention_kinds` are at most 16 kinds of `@<kind>:<name>` besides the
built-in `sme` (experts) and `skill` (skills), each `{kind, title, resolves}`:
a kind of lower case letters, digits and dashes, starting with a letter, at
most 32, that is not `sme`, `skill` or `auto`, once; a title of at most 40
characters, as the console shows it; and `resolves`, what its items are:
`experts` (`@<kind>:<topic>` is `@sme:<topic>`), `skills` (`@<kind>:<name>` is
`$<name>`), `role` (everyone holding the policy role `<name>` is notified,
except in a private thread, where the tag is refused) or `people` (the person
whose handle is `<name>`). Left out, a workspace has the built-in kinds only.
Only the file declares them; the console completes each kind's items as a
person types.

`humagen config check` refuses a mistake with its path, such as
`workspace.projects[2].parent: "Feild" is not a project in this file`. A
parent must be in the same file, because one made only in the console cannot
be checked before start. A workspace has at most 500 projects, and the start
checks the file again against the projects the console made. A file that
names another kind or parent for a project that already exists changes it as
the console's move would, under the same rules: a move into its own subtree,
past four levels, or that breaks a kind or openness rule fails the start.

### Run settings

`run` on `workspace` or on a project sets what every run filed there and
below may do, and what it prefers. A person adds their own level through
`PUT /api/me/context`. Each setting is one of two kinds:

| Setting | Kind | Left out everywhere | Rule |
|---------|------|---------------------|------|
| `ceilings.tools` | ceiling | narrows nothing | at most 64 tool names, as an agent's `tools` writes them (`mcp:<name>` for a provider's tools). A tool the agent grants that is not listed is taken away for the run. `[]` allows none |
| `ceilings.models` | ceiling | narrows nothing | at most 64 `provider/model`, or `provider/*` for all of a provider's. The run's model and fallbacks outside it are dropped, and a run with none left is refused |
| `ceilings.call_depth` | ceiling | the deployment's depth | 0 to 16: how deep a run's calls to other agents may go. 0 calls none. It never deepens the deployment's own limit |
| `ceilings.tool_servers` | ceiling | none | at most 64 hosts, in lower case, a person's own tool servers may be on. Default deny: only `workspace.run` opens it, and a project or a person only narrows it |
| `ceilings.egress` | ceiling | none | at most 64 hosts, in lower case, a run's sandbox may reach through the egress gate, on ports 80 and 443: an exact name, or `*.domain` for its subdomains (not the domain itself). Default deny: only `workspace.run` opens it, and a project or a person only narrows it. It takes effect only with the `egress` slot set to `gate` ([Egress](#egress)) |
| `preferences.model` | preference | the agent's model | `provider/model`, or `policy/<name>` for one of the workspace's model policies, used in place of the agent's own. One the ceilings do not allow is passed over |
| `preferences.compaction_tokens` | preference | never compacts | 1,024 to 2,000,000: the input size, in tokens, past which a run's history is summarized before its next model call |

A **ceiling** is held by the strictest level: the workspace, each project from
the root down to the run's, then the person. A level below can only tighten
it. A **preference** is taken from the nearest level that sets one the
ceilings allow: the person, the run's project, each project above it, the
workspace, then the default.

The settings are resolved once, when a run is admitted, together with who it
acts for (whoever posted the message that started it, in a shared thread
too), that person's tool servers on the allowed hosts and their memory
volume. The result is pinned to the run with a digest, as its agent's
definition is: a retry or a replay sees exactly what the first attempt saw,
and a change made while the run works reaches only the runs after it. A run
another agent calls inherits its caller's context and can only narrow it.
A run acting for a person may call only agents that person may read.

A person's own tool servers are kept as an https address and a credential
reference, never a secret, and the reference must be under the person's own
start (System `person_refs`). They are pinned to the run but not yet
connected: they wait for the sandbox's connector compartment
([RFC 0004](../rfcs/0004-isolated-runs-and-person-context.md)).

### Channels and experts

Every project, the workspace included, has a `general` channel, and at most
20 channels in all. `channels` on `workspace` or on a project declares more,
or declares `general` to set its purpose:

| Field | Left out | Rule |
|-------|----------|------|
| `name` | required | lower case letters, digits and dashes, starting with a letter or digit, at most 40, once per project, never changed |
| `title` | the name | one line, at most 80 characters |
| `purpose` | none | one line, at most 200 characters, sent to every agent answering in the channel |
| `default` | the project's | the agent that answers a message that tags none, or `"*"` for everyone decides (`auto` reads as `*`). One that cannot answer in the project answers nothing, and the message says why |
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
`schema_hash` is the hash of that schema (`humagen config check` prints the
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
([security](../architecture/security.md#the-inbox)).

Who a run acts for is decided when it is accepted, never by anything said in
it:

| The run | Acts for |
|---|---|
| asked in the console, or through the API with a person's sign-in | that person |
| asked by the Slack app built on the API for a Slack person its client maps (`act_as`) | the mapped person, never the app. A message the app posts for them into the Humagen thread their Slack thread is (with its `surface`) is theirs, and so is every run it starts |
| asked about with that app's message shortcut, on someone else's message | nobody: the app asks as itself, since those words are the whole request. Under a dev sign-in, where the app's own credential is the person `dev`, asking as itself acts for `dev`. In a channel bound to a project the app refuses it, since only a person's own words are posted there |
| raised by a Slack client's own connector | the one person the team's Slack clients map that Slack user to; none, or two different people, is nobody. It is started only if that person may start it where it is filed: the policy grants their subject `runs:write` and they are a member of the project. In a channel bound to a project it is not started for nobody |
| started by a message in a shared thread (a tagged agent, an agent that joined, or the thread's default) | the person who posted it, never another participant, though it is sent what the others said in the room ahead of the poster's words; its retry is the poster's alone, from their inbox |
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
  `env:` start, `env:HUMAGEN_PERSON_{subject}_`, and in a `keychain:` start it
  is in the service, `keychain:humagen-people-{subject}/`). No two starts may
  share a place, within a workspace or across two, so give each workspace
  its own. A subject is written as it is, except in an `env:` start, which
  writes it in capitals (`ana` gives `env:HUMAGEN_PERSON_ANA_ISSUES`). A
  subject with a character a reference cannot hold there (an `env:` start
  takes lowercase letters and digits; the others take letters, digits, `.`,
  `_`, `@` and `-`) can give no reference under it, only a value. Keep
  each person's items under their exact subject, in a place that holds
  nothing else, and in a store that tells names apart by case. An `env:`
  start names a `HUMAGEN_` variable, never a sign-in secret. A workspace with no
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

Memory is files ([RFC 0004](../rfcs/0004-isolated-runs-and-person-context.md#memory)):
an agent's notes, a project's and a person's own are each a versioned tree,
whose files an agent with the `memory_search` tool can search. Nothing in the
spec configures it beyond `blobs`.

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
| `text` | required | what the agent is asked, at most 8,000 characters. Each firing is a `cron.tick.v1` event carrying it, on the channel `cron://<workspace>/<name>` |
| `description` | none | what it is for, at most 500 characters; the agent is not shown it |
| `deliver` | the run | `project` posts each answer in the project as an announcement from the agent, its first line the subject, or as a draft for the project's owners under `agent_posts: review`. An answer of NONE posts nothing. A file has nobody to send an answer to, so `inbox` is refused |

A recap is such a schedule: `humagen-analyst`, a Monday morning, "What changed in
Field this week?", `deliver: project`. Its `project_changes` tool reads only
the project and the projects below it.

### Release notes

`workspace.release_notes: true` (off by default) posts what the release of
Humagen this process runs brings in the workspace's own inbox, as an
announcement from humagen: "What's new in Humagen 0.16.0", then the release's
breaking changes and its features, its Breaking and Features groups in the
changelog the binary carries (`CHANGELOG.md`), and the link to the full
changelog. The release tool files every breaking change under Breaking, a
breaking feature included, so a breaking change of any type is posted.
Fixes are not posted. It reaches everyone who reads the workspace, shows in
What's new and in the workspace's inbox like any announcement there, and
each reader hides it for themselves.

Each release is posted once, at the start that first runs it: a restart, a
later process on the same database, a withdrawal, or retention dropping it
never posts it again.
With memory stores every start is a new inbox, so it is posted at each
start. A release of fixes alone posts nothing, and so does a build that is
not a release: `dev`, a local build or a pseudo-version. The release is the
version a release build stamps (`-ldflags "-X main.version=v0.16.0"`, as the
Dockerfile does), else the version Go records for Humagen's module
(`go install github.com/urmzd/humagen/cmd/humagen@latest`, or a program that
imports Humagen at that version). It counts against the workspace's 20
announcements a day; a start past them posts it at the next start. The
Slack app connects after the start and catches up when it does, so it posts
them in a channel bound to the workspace's general, once it was bound
before that start, where a reply under
them starts a thread seeded with them, from Humagen ([Slack](slack.md)).

### Inbox retention

`workspace.inbox_retention` is how long the workspace's inbox keeps what was
settled for everyone: a message resolved, dismissed or withdrawn. It is whole
days, such as `90d`, or a duration, such as `2160h`; left out, it is 90
days, and it is at least `7d` and at most 100 years (`humagen config check`
refuses less or more). At start and every hour after, Humagen drops each
message settled longer ago than that, with everything recorded about it:
its outcomes, who read or hid it, and where a client posted it. What is open or snoozed is never dropped, nor a
question a run still waits on, and an announcement stays while it is open
for anyone, however many readers hid their own copy. Message ids are never
issued again, and release notes dropped this way are not posted again.

The badge and `Count.updates` use counters maintained as inbox changes
apply and rebuilt on replay. Open messages share buckets by kind,
addressing and source. Each reader's read and hidden marks reduce those
counts. The same read rule checks each bucket, so counts reveal only what
the reader may see. Snoozes whose time has passed are added separately;
messages snoozed for everyone are grouped as buckets are, and the read rule
checks each group once.
Settled messages are never scanned to answer a count.

Once a withdrawn announcement is dropped, a reply under its Slack post
starts a thread as any other in the channel, and a thread started from it
no longer shows it. The log lines each inbox decision writes
(`platform: inbox`) are the operator's, and retention does not touch them.

## Which level a knob lives at

| Concern | Level | Why |
|---------|-------|-----|
| Listen address, public URL | System | belongs to the host |
| Sign-in and the identity provider | System | one per process |
| Stores, bus, files | System | where state lives is the operator's choice |
| Concurrency, depth and fan-out | System | protects the process |
| Where a run's tools execute (`sandbox`) | System | the isolation boundary is the host's |
| A conversation's readers, run rate and runs per person (`rooms`) | System | protects the process |
| Variables a workspace may reference | System | the environment is the process's |
| Where people may keep their own tool credentials (`person_refs`) | System | a reference is read with the process's own access |
| Whether people may store a tool credential's value (`workspace.person_credentials`) | Platform | it is the workspace's people's data |
| Release notes in the workspace's inbox (`workspace.release_notes`) | Platform | it is the workspace's people's news |
| How long the inbox keeps what was settled (`workspace.inbox_retention`) | Platform | it is the workspace's people's record |
| Names people see | both | the deployment's, then each workspace's |
| Branding: name, logo, palette and fonts | both | the deployment's, then each workspace's |
| Branding: icons and the installable app | System | served before anyone signs in |
| Models and model policies | Platform | what the workspace's agents run on |
| MCP tools and pinned schemas | Platform | what its agents may touch |
| Agents | Platform | the workspace's own |
| Slack and GitHub | Platform | its identity on a surface |
| History | Platform | how it behaves |
| Project tree, kinds, instructions, channels, experts, knowledge sources, skills, mention kinds and which agents are available where | Platform | the workspace's own |
| Spend and run limits | Platform, in the console | versioned, scoped policies (`pkg/policy`) |
| Ceilings and preferences of every run (`workspace.run`, a project's `run`) | Platform, then each person | the strictest ceiling and the nearest preference win |

## Deploying it

`humagen deploy` takes the same documents anywhere, so nobody writes compose files
or Helm values by hand:

| `deploy.kind` | What `humagen deploy` does |
|---------------|-------------------------|
| `local` (the default) | `humagen start` on this machine |
| `compose` | writes a Docker Compose project (Humagen, and Postgres when asked) and brings it up. Sign-in that only answers on loopback runs on the host's network, so it stays on this machine |
| `kubernetes` | installs the Helm chart Humagen carries, with the spec as its values, into the cluster the block names |

`humagen deploy -o DIR` writes the project or the chart and values and stops, for
review or for GitOps (the Flux example in `deploy/flux` applies the same
chart). A secret never lands in a written file: an `env:` reference is passed
from the deploying shell's environment, and a `keychain:` or `op://` one is
read on this machine and handed to the target as a variable the written spec
references. The `kubernetes` block's `values` are merged over the chart
values Humagen derives, but may not set `system` or `platforms`: those are the
spec's documents, whose references `humagen deploy` carries and checks, so set
them there.

That is for the spec's own references. A person's reference
([Tools that act as you](#tools-that-act-as-you)) is read where Humagen runs,
when a run uses it, so there is nothing to read or pass when deploying:

- A `keychain:` or `op://` start in `person_refs` names this machine's
  keychain or the 1Password account it signed in to, which a container cannot
  reach. `humagen deploy` refuses one for `compose` and `kubernetes`, naming the
  entry. For those targets list an `env:` start instead, let people store
  their credential with Humagen (`workspace.person_credentials.values: true`), or
  deploy `local`.
- An `env:` start names one variable per person and tool
  (`env:HUMAGEN_PERSON_{subject}_` holds Ana's `HUMAGEN_PERSON_ANA_ISSUES`), which
  nobody knows when deploying, so `humagen deploy` does not pass them, and says
  so in what it prints and in the file it writes. Set each in the target:
  for `compose` in the Humagen service's environment, for example in a
  `compose.override.yaml` beside the `compose.yaml` it writes, and for
  `kubernetes` in the pod's, for example as `extraEnv` in the block's
  `values`, from a Secret of your own.

## Extending it

| Slot | Built in | A registered kind returns |
|------|----------|---------------------------|
| `system.blobs` | disk, memory, s3 | a `blob.Store` |
| `system.bus` | memory, postgres | a `bus.Bus` |
| `system.extensions[]` | none | something with `Run(ctx) error`, run beside the server |
| `platform.models[]` | anthropic, openai, google | a `native.ProviderFunc` |
| `platform.tools[]` | mcp | a `runtime.Catalog` |

`humagen.WithKind(slot, name, factory)` registers one. The factory is given its
block as JSON and decodes it strictly itself, so its fields get the same
unknown-field errors as the built-ins'. Sign-in and stores are built in only,
for now.

`humagen.WithRuntime(rt)` runs every turn on your own `turn.Runtime`
(`github.com/urmzd/humagen/fiber/pkg/turn`) in place of the native saige
turn. It is handed the pinned definition and the triggering event and streams
back catalog event bodies. It runs inside the same playscape sandbox and on
the same durable runs; it brings its own models and tools, so the spec's
`models` and `WithModelProvider` do not reach it.

### Since fiber moved into this repository

fiber now lives in this module, at `fiber/`, and a program built on the
`humagen` package imports it from `github.com/urmzd/humagen/fiber/...`. It
drops `github.com/urmzd/fiber` from its `go.mod`. This is a breaking change for
such a program, not for a file:

| Before | Now |
|--------|-----|
| `github.com/urmzd/fiber/pkg/agentrt` | `github.com/urmzd/humagen/fiber/pkg/turn/native` (`native.ProviderFunc`, `native.Option`) |
| `envelope.Tag`, `envelope.Event` | one `event.Envelope`, with a typed body per event type |
| a kind (`delta.text`) and a payload | a type (`run.delta.text.v1`) and its body |
| `router`, `registry`, `agentspec`, topics | `egress`, `definition` over `pkg/agents`, channels per tenant |
| a cron schedule's `Meta` and `Routes` | its `Context`; where a run answers is its channel |

Nothing a file says changes, and stored data is read as it was written: a run
recorded before keeps its trigger, and a schedule keeps its context
([upgrading-from-warp.md](../guides/upgrading-from-warp.md)). The API sends a run's
events as the union too: each has a `type` and its `data` inline
([API contract](api.md#events)).


## Managed source records

Managed source configuration is durable API state, documented in [sources](../architecture/sources.md)
and `KnowledgeSourceConfig` in the OpenAPI contract. `mode` selects indexed, live
or hybrid behavior. `provider` selects a registered `knowledge.Provider`, with
Drive and Notion built in. `origin` is the selected resource ID. `project` is
explicit for external sources, with `~` selecting the workspace. The connecting
owner is the authenticated person. `stop_after_seconds` defaults to 86400 and
accepts 60 through 2592000. No source record contains a credential. Personal
references follow the existing deployment `credentials.refs` allowlist.
