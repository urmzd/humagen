# Deploy

Humagen is one process. `humagen serve` holds the API, sign-in, the event stream,
schedules and agent execution, and serves the built console with `-web`. The
console and the Slack app are clients of it. Everything here runs that one
process with its stores beside it.

| Where | What | Files |
|-------|------|-------|
| Your machine, no containers | `humagen start`, or `make dev` and `make web` | [README](https://github.com/urmzd/humagen/blob/main/README.md#quick-start) |
| Your machine, while developing | `make tilt`: API, console with hot reload, Postgres, restarted as you edit | [develop](develop.md) |
| Your machine or one host | Docker Compose: Humagen, Postgres, optional MinIO | [`deploy/compose`](https://github.com/urmzd/humagen/tree/main/deploy/compose) |
| Kubernetes | A Helm chart | [`deploy/helm/humagen`](https://github.com/urmzd/humagen/tree/main/deploy/helm/humagen) |
| Kubernetes, from Git | Flux deploying that chart | [`deploy/flux`](https://github.com/urmzd/humagen/tree/main/deploy/flux) |
| Wherever the spec says | `humagen deploy`: the spec's `deploy` slot picks `local`, `compose` or `kubernetes`, and `-o DIR` writes what it would apply and stops | [the spec](../reference/spec.md#deploying-it) |

## What to know first

- **One spec everywhere.** Humagen is configured by the YAML documents in
  [the spec](../reference/spec.md). A `System` document says how and where it runs:
  listeners, sign-in, stores, bus, files, limits. It belongs to whoever runs
  the process, so here it is the compose file's `config/warp.yaml` or the
  chart's `system` value. A `Platform` document per workspace says what that
  workspace offers: models, tools, agents, Slack and GitHub. `humagen serve -f
  PATH` reads them, and flags win over them: every setup below forces only the
  listen address and the console's directory as flags, and the chart adds
  the drain timings.
- **Secrets go in the environment, never in the spec.** A credential in a
  document is a reference, `{ref: env:NAME}`, read at start. The variable comes
  from `.env` in compose and from the `existingSecret` Secret in Kubernetes. A
  reference to an unset variable stops Humagen at start.
- **People's own tool credentials are read where Humagen runs.** A person's
  reference, under a `person_refs` start ([the spec](../reference/spec.md#tools-that-act-as-you)),
  is read by the server when a run uses it. A container has no keychain and
  no 1Password session, so there a start is an `env:` one, and each person's
  variable (`HUMAGEN_PERSON_ANA_ISSUES`) is set in its environment like any other
  secret. `humagen deploy` refuses the others for a container
  ([Deploying it](../reference/spec.md#deploying-it)).
- **Replicas.** The stores read and write Postgres per request, delivery and
  the observer share work through consumer groups, and the work done once per
  deployment runs on one elected leader, so the server runs as more than one
  replica. More than one needs `stores.kind: postgres` and shared blobs
  (`blobs.kind: s3` or `gcs`); the chart refuses otherwise. It upgrades with a
  rolling update that adds no pod before one is gone. A change to the inbox
  or a conversation made through one replica reaches the live streams every
  replica serves, through Postgres NOTIFY. Every replica answers every Slack
  and GitHub webhook client, and a client changed through one replica reaches
  the others within 2 seconds. A Socket Mode Slack app (`surfaces.slack`) runs
  on one replica at a time, under a lease another replica takes over when it
  stops. Turns scale on their own
  with workers: see [Process roles](#process-roles).
- **Sign-in in a container is OIDC.** `auth.kind` `dev`, `mock` and
  `tailscale` trust loopback and refuse any other listen address, and a
  published container port is not loopback. Use `kind: oidc` with `issuer`,
  `client_id`, `audience` and `client_secret: {ref: env:HUMAGEN_OIDC_CLIENT_SECRET}`,
  and register `public_url` plus `/auth/callback` with your identity provider.
  The exception is `tailscale` in the chart with its Tailscale proxy container
  in the same pod: Humagen listens on the pod's loopback and the proxy is the
  only way in (see [Tailscale sign-in](#tailscale-sign-in)).
  See [security](../architecture/security.md).
- **Stores.** `stores.kind: postgres` with `url: {ref: env:HUMAGEN_POSTGRES_URL}`
  puts every store in Postgres; `memory` loses them on restart. The event bus
  follows the stores: on Postgres it is `bus.kind: postgres`, events in a table
  on the same database with readers woken by LISTEN/NOTIFY, so there is no
  second service to run. `bus.kind: memory` keeps it in the process. See
  [Storage](../architecture/overview.md#storage). **Published files** go to `blobs.kind: disk` (the workspace
  volume), `s3`, any S3-compatible bucket, or `gcs`, Google Cloud Storage
  with Application Default Credentials.
- **Tools need a sandbox driver.** A run's file tools, its shell and its
  stdio MCP servers execute in the driver `sandbox` names
  ([the spec](../reference/spec.md#sandbox)). See
  [Upgrading: choose a sandbox](#upgrading-choose-a-sandbox).

### Process roles

A process runs one role ([the spec](../reference/spec.md#process-roles)):
`all`, the default and what every setup here runs, or the API and the turns
apart, so each scales on its own:

| Pods | Command | Probes |
|------|---------|--------|
| API, behind the load balancer | `humagen serve -f humagen.yaml -role api` (or `server.role: api`) | `/healthz` on `server.addr` |
| Workers, as many as the run queue needs | `humagen worker -f humagen.yaml` | none over HTTP: a worker listens on nothing, so check the process |

Both read the same spec and the same Postgres; runs pass between them through
the durable ledger and queue. A split role on memory stores is refused at start.
Whatever the roles, the processes elect one leader with a Postgres advisory
lock, and only it runs the reconciler, schedules, inbox retention, email
notices, knowledge refresh and catalog crawls. If the leader's database
connection drops, another process takes over within about ten seconds,
without a restart. Each process logs `msg=process role=...` at start and
`platform: leading` when it becomes the leader.

Until the stores read the database per request, `pkg/state` still refuses a
second process on one database, so run one process with `role: all`. The
split is for the step that lifts that limit
([Limits at scale](../architecture/overview.md#limits-at-scale)).

### Upgrading: choose a sandbox

From this release, a System document that names no `sandbox` gets `docker`
on `runsc` (gVisor): each run's tools execute in containers of their own.
The chart's pod and the compose container have no Docker socket, so a deployment that upgrades without choosing keeps
answering, logs `sandbox: docker is unavailable` at start, and every file
tool and stdio MCP server refuses with that reason. Nothing falls back to
the host. Choose one:

| `sandbox.kind` | What it means here |
|----------------|--------------------|
| `unisolated` | Tools run in Humagen's own container with its privileges, as before this release. The `shell` tool is refused. What the chart and the compose stack now set explicitly |
| `docker` | An instance per run. Humagen needs a daemon. Giving it the Docker socket is root on that host, so run it only where the host exists to run sandboxes, never on a shared Kubernetes node. With `host` and `tls_dir`, it drives a daemon on a separate sandbox host over TLS instead, and needs no socket |
| `local` | Host processes in Humagen's container, not isolated. Development only |

A Kubernetes driver that runs each run's instance as its own pod is not
built yet
([status](../architecture/overview.md#status)). `humagen deploy` deploys the
spec you give it: set `sandbox` there for compose and kubernetes targets too.

### Upgrading: runc is refused by default

saige's sandbox (playscape v0.10.0 and later) refuses a runtime that shares the host's kernel. A spec
with `sandbox.docker.runtime: runc` now logs `sandbox: docker is unavailable`
and every tool that needs an instance refuses, until it also sets
`insecure_development_runtime: true` (or the server gets
`-sandbox-insecure-development-runtime`). Set it only on a development
machine without gVisor. Every container also starts with a seccomp profile
stricter than Docker's default, which denies `ptrace`, `mount`, `unshare`,
`bpf`, `keyctl` and similar syscalls a tool has no use for.

### Upgrading: library skills past the earlier bounds

From this release a library skill may hold 256 files, 1 MiB a supporting
file and 2 MiB in all, where it held 64 files, 128 KiB a file and 512 KiB
([managed library](../architecture/managed-library.md#skills-are-immutable-packages)).
The change is forward-only once one skill past the earlier bounds is drafted,
by `humagen import skills` or through the API. A release before this one
cannot read that skill's record, and fails the listing of its whole scope:

- `GET /api/library/skills` for that scope answers 400 `library: invalid`, so
  the console's Library page for that scope and `humagen import skills` into
  it fail.
- Its skills drop out of what a message can complete in that scope. A
  published skill of the scope other than the large one is still used when a
  message names it; the large one cannot be read, used or found by id.

Rolling forward again reads every record as before. This release, and every
one after it, lists the rest of a scope when one record cannot be read, and
names that record under `unreadable`.

### Upgrading: what an imported skill limits

From this release a skill drafted by `humagen import skills` limits the tools
of a run that uses it only as its `SKILL.md` declares in `allowed-tools`
([managed library](../architecture/managed-library.md#skills-are-immutable-packages)).
Before, the import wrote an empty tool list whatever the folder said, which
gave a run no tools at all, `file_read` included, so a skill could not read
its own supporting files.

- **Skills that change**: every revision an import wrote since v0.138.1, the
  first release that marks an import (`via: import`). Such a revision whose
  `SKILL.md` declares no `allowed-tools`, published and named in a message or
  bound as an agent's default, now leaves a run the agent's own tools where
  it left none; one that declares `allowed-tools` limits a run to those of
  them the agent holds. The library answers each with that limit, and the
  next write of its record stores it.
- **Skills that do not change**: a revision imported before v0.138.1 carries
  no import mark, keeps its empty list, and still gives a run no tool. A
  revision saved in the editor, or promoted from one, keeps its list.

Every run whose skills have supporting files is now offered
`skill_file_read`, which reads only those files. To give a skill imported
before v0.138.1 the agent's tools, open it under Library, Skills, clear
"Limit the tools of a run that uses this skill" under Allowed Tools, save,
test and publish the new revision. An import now refuses a `SKILL.md` that
declares more than 128 tools, or a name over 128 characters, and names the
bound. A release before this one reads a revision with more than 32 tools as
`unreadable` ([library skills past the earlier bounds](#upgrading-library-skills-past-the-earlier-bounds)).

### Upgrading: knowledge source ids

From this release each knowledge source has an id
([status](../architecture/overview.md#status)), and a conversation continued
from an imported one pins it. A source kept before this release has none
until a conversation imported into it is continued, which gives it one.

A replica still on an earlier release does not know the id. During a rolling
update, when it writes a source (a document added, replaced, excluded or
removed, a variant, a configuration, a sync), the record it writes has no id.
That fails closed: the source's continuations are seen by no one, an admin
included, and nothing is posted in them, from then on. A source it deletes
and makes again has no id either, and an empty id matches no continuation,
so none is revived. To avoid losing continuations, keep knowledge writes off
older replicas until every replica runs this release. Continuations are
started through `POST /api/knowledge/{name}/documents/{id}/continue`, so a
deployment that has used that route has some to lose.

## The image

The [`Dockerfile`](https://github.com/urmzd/humagen/blob/main/Dockerfile) at the root builds the console (`pnpm
install`, then `turbo run build --filter=@humagen/console`) and a static `humagen`
(`CGO_ENABLED=0`, as `make build`), and ships them on Alpine:

- `humagen` at `/usr/local/bin/humagen` is the entrypoint, the console is at
  `/usr/share/warp/web`.
- It runs as uid 10001, with `HOME` and `XDG_CACHE_HOME` under the
  `/var/lib/warp` volume, so the workspace (agents' files, and `blobs/` with
  disk storage) persists there.
- The default command is `serve -f /etc/warp -addr 0.0.0.0:8080 -web
  /usr/share/warp/web`: mount a spec at `/etc/warp`. With none, `serve` keeps
  its own defaults: OIDC from the `HUMAGEN_OIDC_*` variables, nothing seeded. When
  they are unset Humagen stops rather than serve without sign-in. A System
  document that names no `auth.kind` means `dev`, which refuses the
  container's address, so the spec names `oidc`.
- A healthcheck calls `/healthz`.

```sh
make image                      # docker build -t humagen:local .
docker build --build-arg VERSION=0.9.0 --build-arg COMMIT=$(git rev-parse --short HEAD) -t humagen .
```

Releases do not publish an image yet: build it and push it to your registry.
The image carries only `humagen`. An MCP server run over stdio needs its own
runtime (node, python), so build an image on top of this one for it, or run
the server over HTTP.

## Docker Compose

[`deploy/compose/compose.yaml`](https://github.com/urmzd/humagen/blob/main/deploy/compose/compose.yaml) runs Humagen and
Postgres 17, with one optional profile: `s3` (MinIO, and a one-shot job that
creates the bucket). Each service has a healthcheck, and Humagen waits
for what it depends on.

```sh
cp deploy/compose/.env.example deploy/compose/.env   # the secrets the spec names
$EDITOR deploy/compose/config/warp.yaml              # identity provider, public URL
make up                                              # build and start
make down                                            # stop, keep the volumes
make down V=1                                        # stop and delete the volumes
```

[`config/warp.yaml`](https://github.com/urmzd/humagen/blob/main/deploy/compose/config/warp.yaml) holds a System
document (OIDC, Postgres through `env:HUMAGEN_POSTGRES_URL`, disk files) and a
`default` Platform document, mounted at `/etc/warp` with any file it names.
compose runs `serve -f /etc/warp -addr 0.0.0.0:8080 -web /usr/share/warp/web`
and passes `.env` to the container, so a variable a reference names only has to
be in `.env`. [`.env.example`](https://github.com/urmzd/humagen/blob/main/deploy/compose/.env.example) lists them,
commented. `COMPOSE_PROFILES=s3` turns on the profile; uncomment the
matching `blobs` block in `humagen.yaml` as well.

Humagen is published on `127.0.0.1:8080` by default. To reach it from other
machines, set `HUMAGEN_BIND=0.0.0.0` behind TLS, and `public_url` to the address
browsers use.

### Without an identity provider

[`compose.dev.yaml`](https://github.com/urmzd/humagen/blob/main/deploy/compose/compose.dev.yaml) mounts
[`dev/warp.yaml`](https://github.com/urmzd/humagen/blob/main/deploy/compose/dev/warp.yaml) instead: `auth.kind: dev`,
seeded agents on the offline provider, the same Platform document. Humagen runs
on the host's network, listening on `127.0.0.1:8080`, with Postgres (and MinIO) published on the host's loopback for it. It needs host networking: Linux, or Docker Desktop 4.34+ with
it turned on.

```sh
cd deploy/compose
docker compose -f compose.yaml -f compose.dev.yaml up -d --build
```

Every request is an admin. Do not use it anywhere but your own machine.

A browser for agents is the `browser` profile: see
[the bundled browser image](browser.md#run-the-bundled-browser-image).

## Kubernetes with Helm

[`deploy/helm/humagen`](https://github.com/urmzd/humagen/tree/main/deploy/helm/humagen) deploys one Humagen server: a Deployment, a
Service, an optional Ingress, a ConfigMap, a PersistentVolumeClaim for the
workspace when blobs are on disk, a PodDisruptionBudget, and a ServiceAccount
with no API token. The pod runs as non-root with a read-only root filesystem
and no capabilities. Every value is documented in
[`values.yaml`](https://github.com/urmzd/humagen/blob/main/deploy/helm/humagen/values.yaml).

The `system` value (`metadata` and `spec`) and each item of `platforms` are
rendered verbatim, with `api_version` and `kind` added, into one `humagen.yaml`
in the ConfigMap. The pod runs `serve -f /etc/warp/humagen.yaml -addr=0.0.0.0:8080
-web=/usr/share/warp/web`. Files the spec names, such as `auth.policy_file` or
an agent's `file`, go in `files` and sit beside it as `/etc/warp/NAME`.
Everything else in the values is the chart's own: image, volume, Service,
Ingress, probes, resources. The defaults are OIDC (`issuer`, `client_id` and
`audience` are required) and Postgres, both reading their credentials through
references. Rendering fails for `auth.kind` `dev`, `mock` or `tailscale`, for
missing OIDC values, and for `replicaCount` other than 1.

The bus is left unset in the values: when `stores.kind` is `postgres` the
chart writes `bus: {kind: postgres}`, and with memory stores it stays memory.
A `bus.kind` in the values wins. A release upgraded with `--reuse-values`
keeps the `bus: {kind: memory}` older values set; drop it to move to Postgres.

Secrets never go in values. Create a Secret with the variables the references
name and set `existingSecret`; its keys become the container's environment:

```sh
kubectl create namespace humagen
kubectl -n humagen create secret generic humagen-env \
  --from-literal=HUMAGEN_OIDC_CLIENT_SECRET=... \
  --from-literal=HUMAGEN_POSTGRES_URL='postgres://humagen:...@db:5432/humagen'

helm install humagen deploy/helm/humagen -n humagen \
  --set image.repository=registry.example.com/humagen --set image.tag=0.9.0 \
  --set system.spec.server.public_url=https://humagen.example.com \
  --set system.spec.auth.oidc.issuer=https://login.example.com \
  --set system.spec.auth.oidc.client_id=humagen \
  --set system.spec.auth.oidc.audience=humagen \
  --set existingSecret=humagen-env
```

Models, tools and agents go in a values file under `platforms`, as in the
[Flux example](https://github.com/urmzd/humagen/blob/main/deploy/flux/app/values.yaml). Helm merges maps key by key and
replaces lists, so a `platforms` override replaces the default list.

`make chart-check` (`deploy/helm/check.sh`) lints the chart, renders it with
each file in `ci/` and the Flux values, runs `kubeconform` on the manifests
when it is installed, and runs `humagen config check` on the `humagen.yaml` each
render produces, so the chart cannot render a spec Humagen refuses. It also checks
the compose stack's two specs.

### Probes and shutdown

The liveness probe calls `/healthz` (the process answers). The readiness
probe calls `/readyz`, which answers 200 only when the database and bus
answer, the platform has finished starting and the process is not draining,
so a draining pod leaves the Service's endpoints. The startup probe calls
`/healthz` every 2 seconds for up to two minutes, since migrations run at
start. Under `development.loopback` or tailscale sign-in the kubelet cannot
reach the listener, so each probe runs `wget` inside the pod against its own
path.

On SIGTERM Humagen stops claiming work, lets turns in flight finish for
`shutdown.drainGrace` (25s), releases the leases of the turns still running
so another replica resumes them, then shuts the HTTP server down within
`shutdown.httpTimeout` (10s). The chart passes them as `-drain-grace` and
`-shutdown-timeout`. Before SIGTERM a preStop sleep of
`shutdown.preStopSleepSeconds` (5, the kubelet's sleep action, Kubernetes
1.30 or later; 0 turns it off) lets endpoints be removed first.
`terminationGracePeriodSeconds` (45) must exceed the three together, and
rendering fails when it does not. `/readyz` and the two flags need a release
that has them.

### Blobs and the volume

With `blobs.kind: disk` published files live on the workspace volume, a
ReadWriteOnce claim the chart creates (or `persistence.existingClaim`). With
`s3` or `gcs` the chart creates no claim and the workspace is an `emptyDir`,
scratch, so the pod is not tied to a node; `persistence.existingClaim` is
still used when set. `gcs` takes `{bucket, prefix}` and authenticates with
Application Default Credentials: on GKE, Workload Identity, by annotating the
ServiceAccount with `iam.gke.io/gcp-service-account`. `s3` takes `bucket`,
`region`, `endpoint`, `path_style` and keys; `values.yaml` shows AWS, GCS's
S3-compatible endpoint with an HMAC key, R2 and MinIO.

### Disruption budget and autoscaling

`podDisruptionBudget` is on with `maxUnavailable: 1`. A `minAvailable: 1`
budget on one replica would block every node drain; `maxUnavailable: 1` lets
a drain evict the pod and limits evictions once there are more replicas.

`autoscaling.enabled` adds a HorizontalPodAutoscaler for the server (CPU,
optional memory, and `autoscaling.metrics` appended as written) and the
Deployment then omits `replicas`. `maxReplicas` above 1 needs Postgres stores
and shared blobs, the same rule as `replicaCount`.

### Workers and KEDA

`worker.enabled` adds a second Deployment running `humagen worker -f
/etc/warp/humagen.yaml`: the same image, spec, Secret, volumes and security
context, no Service, the same drain flags and grace period, and its own
resources and disruption budget. A worker claims turns from the Postgres
queue and serves no API. It needs a release with the `humagen worker`
command, Postgres stores and a bucket (`s3` or `gcs`); rendering fails
otherwise. Off by default.

`worker.keda.enabled` adds a KEDA ScaledObject for the worker with a
`postgresql` trigger, reading the connection string from `existingSecret`'s
`HUMAGEN_POSTGRES_URL` through a TriggerAuthentication. With
`worker.keda.connectionFromEnv` set to a variable of the worker container,
such as a passwordless `HUMAGEN_POSTGRES_URL` in `extraEnv` for IAM sign-in
through the Cloud SQL Auth Proxy, KEDA reads that instead and neither the
TriggerAuthentication nor `existingSecret` is needed. Its default query
counts the runs in `fiber_run_queue` that are due to be claimed or held by a
live lease; `targetQueryValue` is the rows per worker, best set to
`execution.concurrency`. It needs KEDA installed in the cluster.

### Tailscale sign-in

`auth.kind: tailscale` trusts the `Tailscale-User-Login` and
`Tailscale-User-Name` headers from a loopback caller. In the chart it needs
`tailscale.proxy.enabled`: a proxy container in the same pod, such as Caddy
built with caddy-tailscale, that joins the tailnet (auth key from a Secret
through `tailscale.proxy.envFrom`) and reverse-proxies to `127.0.0.1:8080`,
where Humagen listens. Its config (a Caddyfile) goes in
`tailscale.proxy.files`, mounted at `/etc/tailscale-proxy`; `values.yaml` and
[`ci/tailscale-values.yaml`](https://github.com/urmzd/humagen/blob/main/deploy/helm/humagen/ci/tailscale-values.yaml)
show one. The proxy is the only way in: the chart refuses an Ingress, and the
Service exposes no Humagen port (only the webhooks port, when on). The
Tailscale Kubernetes operator's ingress proxies to the Service or pod IP, not
the pod's loopback, so it cannot front this listener. Never expose the pod
any other way. `public_url` is the proxy's https tailnet URL, `auth.oidc` is
`null`, and `dev` and `mock` stay refused outside `development.loopback`.

### Infrastructure

The chart does not run Postgres or a bucket: use what your cluster
already has, or an operator. With an ingress, turn off proxy buffering and
raise the read timeout so the event stream is not cut; `values.yaml` shows the
ingress-nginx annotations. `webhooks.enabled` adds the separate listener for
signed Slack and GitHub webhooks, on its own Service port.

The chart was called `warp` before the product was renamed. A release it
installed keeps its Deployment, Service and volume only with
`nameOverride: warp`, and the chart refuses to upgrade one without it;
`humagen deploy` sets it for such a release on every deploy. See
[upgrading-from-warp.md](upgrading-from-warp.md#kubernetes).

### Check two running replicas

Port-forward each server pod to a separate local port, then run the existing
replica check with an agent and an authorized tailnet login:

```sh
HUMAGEN_E2E_A_URL=http://127.0.0.1:18081 \
HUMAGEN_E2E_B_URL=http://127.0.0.1:18082 \
HUMAGEN_E2E_AGENT=lead \
HUMAGEN_E2E_LOGIN=you@example.com \
HUMAGEN_E2E_HOST=platform.withhumagen.com \
scripts/two-replica.sh
```

`HUMAGEN_E2E_HOST` is optional. Set it to the authority in the deployment's
`public_url` (hostname and port when present) when authentication checks the
HTTP Host. The readiness probe, API calls and event streams use this Host
while still connecting to the local port forwards. Without it, each request
uses its URL's host. `HUMAGEN_E2E_LOGIN` supplies `Tailscale-User-Login` for a
pod's trusted loopback connection. The check starts a real model run and
compares both replicas' live and stored events; it does not stop either pod.

`browser.enabled` adds a browser for agents as one more pod, reached only by
the release's own pods: see
[the bundled browser image](browser.md#run-the-bundled-browser-image).

## Flux

[`deploy/flux`](https://github.com/urmzd/humagen/tree/main/deploy/flux) deploys the chart straight from this repository.
Its objects (the namespace, the `GitRepository`, the `Kustomization`, the
`HelmRelease` and the Secret it reads) keep the name `warp` they had before
the product was renamed, because the example `Kustomization` prunes what it no
longer applies:

- [`clusters/example/humagen.yaml`](https://github.com/urmzd/humagen/blob/main/deploy/flux/clusters/example/humagen.yaml): a
  `GitRepository` for this repository (narrowed to `deploy/`) and a Flux
  `Kustomization` that applies `deploy/flux/app`. Commit it where your cluster's
  Flux already reconciles.
- [`app/`](https://github.com/urmzd/humagen/tree/main/deploy/flux/app): the `warp` namespace and a `HelmRelease` named
  `warp` whose chart is `./deploy/helm/humagen` from that source, rebuilt on
  every revision, and upgraded in place when the chart's name changes
  (`chartNameChangeStrategy: InPlaceUpdate`). Its
  values come from [`app/values.yaml`](https://github.com/urmzd/humagen/blob/main/deploy/flux/app/values.yaml), an
  override example in the spec's shape (`system`, `platforms`), through a generated ConfigMap whose name changes with its
  content, so an edit upgrades the release.

To use your own values, copy `app/` into your fleet repository, edit
`values.yaml` and point the `Kustomization`'s `path` at the copy (with a
`sourceRef` to your fleet repository). Pin `ref` to a release tag rather than
`main`. The `warp-env` Secret is yours to create; with SOPS, commit it encrypted
beside `values.yaml` and turn on the `decryption` block shown in the file. Once
an image and chart are published to a registry, an `OCIRepository` can replace
the `GitRepository`; nothing is published yet.

## Local Kubernetes with Tilt

The [`Tiltfile`](https://github.com/urmzd/humagen/blob/main/Tiltfile) at the root runs humagen on a local cluster
through this chart, rebuilding as you edit:

```sh
kind create cluster      # or k3d, minikube, Docker Desktop, Rancher Desktop, OrbStack
tilt up                  # then open http://127.0.0.1:8080
```

It needs Docker, helm, Go and pnpm beside Tilt. The binary and the console
are built on the host, where Go's and pnpm's caches keep a change to seconds,
then copied into a small image ([`deploy/tilt/Dockerfile`](https://github.com/urmzd/humagen/blob/main/deploy/tilt/Dockerfile))
that the chart runs with [`deploy/tilt/values.yaml`](https://github.com/urmzd/humagen/blob/main/deploy/tilt/values.yaml):
development sign-in, memory stores, example agents and the offline model, so
nothing needs a key, a database or an identity provider.

Development sign-in signs every request in as an admin, which the chart
otherwise refuses in a pod. `development.loopback: true` is what lets it
through: humagen listens on the pod's own loopback, which `kubectl
port-forward` (Tilt's forward of port 8080) reaches and nothing else does,
and the probes run `wget` inside the pod. The chart refuses it with an
Ingress. Never set it on a cluster other people use.

Your own overrides go in `deploy/tilt/values.local.yaml`, which git ignores
and the Tiltfile applies last: a `system.spec.branding` block to try a brand,
a model provider with an `existingSecret`, more `platforms`.

## What has been checked

The compose files pass `docker compose config`, and both compose specs pass
`humagen config check`; the dev spec, with memory stores, was started with `humagen
serve` and served the console. `make chart-check` passes: `helm lint --strict`,
`kubeconform -strict` on the renders (the defaults with the required OIDC
values, every option on in
[`ci/full-values.yaml`](https://github.com/urmzd/humagen/blob/main/deploy/helm/humagen/ci/full-values.yaml), the worker, KEDA,
autoscaler, disruption budgets and `gcs` blobs in
[`ci/scale-values.yaml`](https://github.com/urmzd/humagen/blob/main/deploy/helm/humagen/ci/scale-values.yaml), tailscale behind
its proxy in [`ci/tailscale-values.yaml`](https://github.com/urmzd/humagen/blob/main/deploy/helm/humagen/ci/tailscale-values.yaml),
and the Flux and Tilt overrides; KEDA's ScaledObject and TriggerAuthentication
are skipped, having no schema), `humagen config check` on each rendered
`humagen.yaml`, the refusals (grace period too short, unparsable durations,
server autoscaling above one replica, a worker without Postgres or a bucket,
tailscale without its proxy or behind an Ingress), and the shape of each
render (the claim only for disk blobs, the Postgres bus, `/readyz`, the
probes' paths under loopback). The drain, `/readyz` and the worker have not
been run in a cluster. The Flux
manifests pass `kubeconform` after `kustomize build`. `config check` does not
resolve references or read the files a spec names, so an unset variable or a
missing file shows only at start. The image has not been built and run here (no
Docker daemon), and nothing has been deployed to a cluster. The Tilt setup's values render
through `make chart-check`; `tilt up` itself has not been run here. See the
[status table](../architecture/overview.md#status).
