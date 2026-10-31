# Deploy

Humagen is one process. `humagen serve` holds the API, sign-in, the event stream,
schedules and agent execution, and serves the built console with `-web`. The
console and the Slack app are clients of it. Everything here runs that one
process with its stores beside it.

| Where | What | Files |
|-------|------|-------|
| Your machine, no containers | `humagen start`, or `make dev` and `make web` | [README](../../README.md#quick-start) |
| Your machine, while developing | `make tilt`: API, console with hot reload, Postgres, restarted as you edit | [develop](develop.md) |
| Your machine or one host | Docker Compose: Humagen, Postgres, optional MinIO | [`deploy/compose`](../../deploy/compose) |
| Kubernetes | A Helm chart | [`deploy/helm/humagen`](../../deploy/helm/humagen) |
| Kubernetes, from Git | Flux deploying that chart | [`deploy/flux`](../../deploy/flux) |
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
- **One replica.** The stores are single-writer: the run index, the
  schedules and the rest assume one process owns them (see
  [Limits at scale](../architecture/overview.md#limits-at-scale)). Admission
  and spend counts are shared on Postgres, but that alone does not make a
  second process safe, and `pkg/state` refuses one at start. The chart
  refuses `replicaCount` other than 1 and upgrades with `Recreate`.
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

playscape v0.10.0 refuses a runtime that shares the host's kernel. A spec
with `sandbox.docker.runtime: runc` now logs `sandbox: docker is unavailable`
and every tool that needs an instance refuses, until it also sets
`insecure_development_runtime: true` (or the server gets
`-sandbox-insecure-development-runtime`). Set it only on a development
machine without gVisor. Every container also starts with a seccomp profile
stricter than Docker's default, which denies `ptrace`, `mount`, `unshare`,
`bpf`, `keyctl` and similar syscalls a tool has no use for.

## The image

The [`Dockerfile`](../../Dockerfile) at the root builds the console (`pnpm
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

[`deploy/compose/compose.yaml`](../../deploy/compose/compose.yaml) runs Humagen and
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

[`config/warp.yaml`](../../deploy/compose/config/warp.yaml) holds a System
document (OIDC, Postgres through `env:HUMAGEN_POSTGRES_URL`, disk files) and a
`default` Platform document, mounted at `/etc/warp` with any file it names.
compose runs `serve -f /etc/warp -addr 0.0.0.0:8080 -web /usr/share/warp/web`
and passes `.env` to the container, so a variable a reference names only has to
be in `.env`. [`.env.example`](../../deploy/compose/.env.example) lists them,
commented. `COMPOSE_PROFILES=s3` turns on the profile; uncomment the
matching `blobs` block in `humagen.yaml` as well.

Humagen is published on `127.0.0.1:8080` by default. To reach it from other
machines, set `HUMAGEN_BIND=0.0.0.0` behind TLS, and `public_url` to the address
browsers use.

### Without an identity provider

[`compose.dev.yaml`](../../deploy/compose/compose.dev.yaml) mounts
[`dev/warp.yaml`](../../deploy/compose/dev/warp.yaml) instead: `auth.kind: dev`,
seeded agents on the offline provider, the same Platform document. Humagen runs
on the host's network, listening on `127.0.0.1:8080`, with Postgres (and MinIO) published on the host's loopback for it. It needs host networking: Linux, or Docker Desktop 4.34+ with
it turned on.

```sh
cd deploy/compose
docker compose -f compose.yaml -f compose.dev.yaml up -d --build
```

Every request is an admin. Do not use it anywhere but your own machine.

## Kubernetes with Helm

[`deploy/helm/humagen`](../../deploy/helm/humagen) deploys one Humagen server: a Deployment, a
Service, an optional Ingress, a ConfigMap, a PersistentVolumeClaim for the
workspace when blobs are on disk, a PodDisruptionBudget, and a ServiceAccount
with no API token. The pod runs as non-root with a read-only root filesystem
and no capabilities. Every value is documented in
[`values.yaml`](../../deploy/helm/humagen/values.yaml).

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
[Flux example](../../deploy/flux/app/values.yaml). Helm merges maps key by key and
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
Deployment then omits `replicas`. While the stores are single-writer the
chart refuses `maxReplicas` above 1, the same rule as `replicaCount`.

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
`HUMAGEN_POSTGRES_URL` through a TriggerAuthentication. Its default query
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
[`ci/tailscale-values.yaml`](../../deploy/helm/humagen/ci/tailscale-values.yaml)
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

## Flux

[`deploy/flux`](../../deploy/flux) deploys the chart straight from this repository.
Its objects (the namespace, the `GitRepository`, the `Kustomization`, the
`HelmRelease` and the Secret it reads) keep the name `warp` they had before
the product was renamed, because the example `Kustomization` prunes what it no
longer applies:

- [`clusters/example/humagen.yaml`](../../deploy/flux/clusters/example/humagen.yaml): a
  `GitRepository` for this repository (narrowed to `deploy/`) and a Flux
  `Kustomization` that applies `deploy/flux/app`. Commit it where your cluster's
  Flux already reconciles.
- [`app/`](../../deploy/flux/app): the `warp` namespace and a `HelmRelease` named
  `warp` whose chart is `./deploy/helm/humagen` from that source, rebuilt on
  every revision, and upgraded in place when the chart's name changes
  (`chartNameChangeStrategy: InPlaceUpdate`). Its
  values come from [`app/values.yaml`](../../deploy/flux/app/values.yaml), an
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

The [`Tiltfile`](../../Tiltfile) at the root runs humagen on a local cluster
through this chart, rebuilding as you edit:

```sh
kind create cluster      # or k3d, minikube, Docker Desktop, Rancher Desktop, OrbStack
tilt up                  # then open http://127.0.0.1:8080
```

It needs Docker, helm, Go and pnpm beside Tilt. The binary and the console
are built on the host, where Go's and pnpm's caches keep a change to seconds,
then copied into a small image ([`deploy/tilt/Dockerfile`](../../deploy/tilt/Dockerfile))
that the chart runs with [`deploy/tilt/values.yaml`](../../deploy/tilt/values.yaml):
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
[`ci/full-values.yaml`](../../deploy/helm/humagen/ci/full-values.yaml), the worker, KEDA,
autoscaler, disruption budgets and `gcs` blobs in
[`ci/scale-values.yaml`](../../deploy/helm/humagen/ci/scale-values.yaml), tailscale behind
its proxy in [`ci/tailscale-values.yaml`](../../deploy/helm/humagen/ci/tailscale-values.yaml),
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
