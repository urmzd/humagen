# Deploy

warp is one process. `warp serve` holds the API, sign-in, the event stream,
schedules and agent execution, and serves the built console with `-web`. The
console and the Slack app are clients of it. Everything here runs that one
process with its stores beside it.

| Where | What | Files |
|-------|------|-------|
| Your machine, no containers | `warp start`, or `make dev` and `make web` | [README](../README.md#quick-start) |
| Your machine or one host | Docker Compose: warp, Postgres, optional MinIO and Redis | [`deploy/compose`](../deploy/compose) |
| Kubernetes | A Helm chart | [`deploy/helm/warp`](../deploy/helm/warp) |
| Kubernetes, from Git | Flux deploying that chart | [`deploy/flux`](../deploy/flux) |
| Wherever the spec says | `warp deploy`: the spec's `deploy` slot picks `local`, `compose` or `kubernetes`, and `-o DIR` writes what it would apply and stops | [the spec](spec.md#deploying-it) |

## What to know first

- **One spec everywhere.** warp is configured by the YAML documents in
  [the spec](spec.md). A `System` document says how and where it runs:
  listeners, sign-in, stores, bus, files, limits. It belongs to whoever runs
  the process, so here it is the compose file's `config/warp.yaml` or the
  chart's `system` value. A `Platform` document per workspace says what that
  workspace offers: models, tools, agents, Slack and GitHub. `warp serve -f
  PATH` reads them, and flags win over them: every setup below forces only the
  listen address and the console's directory as flags.
- **Secrets go in the environment, never in the spec.** A credential in a
  document is a reference, `{ref: env:NAME}`, read at start. The variable comes
  from `.env` in compose and from the `existingSecret` Secret in Kubernetes. A
  reference to an unset variable stops warp at start.
- **People's own tool credentials are read where warp runs.** A person's
  reference, under a `person_refs` start ([the spec](spec.md#tools-that-act-as-you)),
  is read by the server when a run uses it. A container has no keychain and
  no 1Password session, so there a start is an `env:` one, and each person's
  variable (`WARP_PERSON_ANA_ISSUES`) is set in its environment like any other
  secret. `warp deploy` refuses the others for a container
  ([Deploying it](spec.md#deploying-it)).
- **One replica.** The stores are single-writer: the run index, the usage
  ledger, the admission lock and the schedules assume one process owns them
  (see [Limits at scale](architecture.md#limits-at-scale)). A second warp on
  the same database races the first. The chart refuses `replicaCount` other
  than 1 and upgrades with `Recreate`.
- **Sign-in in a container is OIDC.** `auth.kind` `dev`, `mock` and
  `tailscale` trust loopback and refuse any other listen address, and a
  published container port is not loopback. Use `kind: oidc` with `issuer`,
  `client_id`, `audience` and `client_secret: {ref: env:WARP_OIDC_CLIENT_SECRET}`,
  and register `public_url` plus `/auth/callback` with your identity provider.
  See [security](security.md).
- **Stores.** `stores.kind: postgres` with `url: {ref: env:WARP_POSTGRES_URL}`
  puts every store in Postgres; `memory` loses them on restart. `bus.kind:
  redis` moves the event bus out of the process; `memory` is right for one
  replica. See [Storage](architecture.md#storage) for what a Postgres-only setup
  cannot do. **Published files** go to `blobs.kind: disk` (the workspace
  volume) or `s3`, any S3-compatible bucket.

## The image

The [`Dockerfile`](../Dockerfile) at the root builds the console (`pnpm
install`, then `turbo run build --filter=@warp/console`) and a static `warp`
(`CGO_ENABLED=0`, as `make build`), and ships them on Alpine:

- `warp` at `/usr/local/bin/warp` is the entrypoint, the console is at
  `/usr/share/warp/web`.
- It runs as uid 10001, with `HOME` and `XDG_CACHE_HOME` under the
  `/var/lib/warp` volume, so the workspace (agents' files, and `blobs/` with
  disk storage) persists there.
- The default command is `serve -f /etc/warp -addr 0.0.0.0:8080 -web
  /usr/share/warp/web`: mount a spec at `/etc/warp`. With none, `serve` keeps
  its own defaults: OIDC from the `WARP_OIDC_*` variables, nothing seeded. When
  they are unset warp stops rather than serve without sign-in. A System
  document that names no `auth.kind` means `dev`, which refuses the
  container's address, so the spec names `oidc`.
- A healthcheck calls `/healthz`.

```sh
make image                      # docker build -t warp:local .
docker build --build-arg VERSION=0.9.0 --build-arg COMMIT=$(git rev-parse --short HEAD) -t warp .
```

Releases do not publish an image yet: build it and push it to your registry.
The image carries only `warp`. An MCP server run over stdio needs its own
runtime (node, python), so build an image on top of this one for it, or run
the server over HTTP.

## Docker Compose

[`deploy/compose/compose.yaml`](../deploy/compose/compose.yaml) runs warp and
Postgres 17, with two optional profiles: `s3` (MinIO, and a one-shot job that
creates the bucket) and `redis`. Each service has a healthcheck, and warp waits
for what it depends on.

```sh
cp deploy/compose/.env.example deploy/compose/.env   # the secrets the spec names
$EDITOR deploy/compose/config/warp.yaml              # identity provider, public URL
make up                                              # build and start
make down                                            # stop, keep the volumes
make down V=1                                        # stop and delete the volumes
```

[`config/warp.yaml`](../deploy/compose/config/warp.yaml) holds a System
document (OIDC, Postgres through `env:WARP_POSTGRES_URL`, disk files) and a
`default` Platform document, mounted at `/etc/warp` with any file it names.
compose runs `serve -f /etc/warp -addr 0.0.0.0:8080 -web /usr/share/warp/web`
and passes `.env` to the container, so a variable a reference names only has to
be in `.env`. [`.env.example`](../deploy/compose/.env.example) lists them,
commented. `COMPOSE_PROFILES=s3,redis` turns on the profiles; uncomment the
matching `blobs` or `bus` block in `warp.yaml` as well.

warp is published on `127.0.0.1:8080` by default. To reach it from other
machines, set `WARP_BIND=0.0.0.0` behind TLS, and `public_url` to the address
browsers use.

### Without an identity provider

[`compose.dev.yaml`](../deploy/compose/compose.dev.yaml) mounts
[`dev/warp.yaml`](../deploy/compose/dev/warp.yaml) instead: `auth.kind: dev`,
seeded agents on the offline provider, the same Platform document. warp runs
on the host's network, listening on `127.0.0.1:8080`, with Postgres (and MinIO,
Redis) published on the host's loopback for it. It needs host networking: Linux, or Docker Desktop 4.34+ with
it turned on.

```sh
cd deploy/compose
docker compose -f compose.yaml -f compose.dev.yaml up -d --build
```

Every request is an admin. Do not use it anywhere but your own machine.

## Kubernetes with Helm

[`deploy/helm/warp`](../deploy/helm/warp) deploys one warp: a Deployment, a
Service, an optional Ingress, a ConfigMap, a PersistentVolumeClaim for the
workspace, and a ServiceAccount with no API token. The pod runs as non-root
with a read-only root filesystem and no capabilities. Startup, liveness and
readiness probes call `/healthz`. Every value is documented in
[`values.yaml`](../deploy/helm/warp/values.yaml).

The `system` value (`metadata` and `spec`) and each item of `platforms` are
rendered verbatim, with `api_version` and `kind` added, into one `warp.yaml`
in the ConfigMap. The pod runs `serve -f /etc/warp/warp.yaml -addr=0.0.0.0:8080
-web=/usr/share/warp/web`. Files the spec names, such as `auth.policy_file` or
an agent's `file`, go in `files` and sit beside it as `/etc/warp/NAME`.
Everything else in the values is the chart's own: image, volume, Service,
Ingress, probes, resources. The defaults are OIDC (`issuer`, `client_id` and
`audience` are required) and Postgres, both reading their credentials through
references. Rendering fails for `auth.kind` `dev`, `mock` or `tailscale`, for
missing OIDC values, and for `replicaCount` other than 1.

Secrets never go in values. Create a Secret with the variables the references
name and set `existingSecret`; its keys become the container's environment:

```sh
kubectl create namespace warp
kubectl -n warp create secret generic warp-env \
  --from-literal=WARP_OIDC_CLIENT_SECRET=... \
  --from-literal=WARP_POSTGRES_URL='postgres://warp:...@db:5432/warp'

helm install warp deploy/helm/warp -n warp \
  --set image.repository=registry.example.com/warp --set image.tag=0.9.0 \
  --set system.spec.server.public_url=https://warp.example.com \
  --set system.spec.auth.oidc.issuer=https://login.example.com \
  --set system.spec.auth.oidc.client_id=warp \
  --set system.spec.auth.oidc.audience=warp \
  --set existingSecret=warp-env
```

Models, tools and agents go in a values file under `platforms`, as in the
[Flux example](../deploy/flux/app/values.yaml). Helm merges maps key by key and
replaces lists, so a `platforms` override replaces the default list.

`make chart-check` (`deploy/helm/check.sh`) lints the chart, renders it with
each file in `ci/` and the Flux values, runs `kubeconform` on the manifests
when it is installed, and runs `warp config check` on the `warp.yaml` each
render produces, so the chart cannot render a spec warp refuses. It also checks
the compose stack's two specs.

The chart does not run Postgres, Redis or a bucket: use what your cluster
already has, or an operator. With an ingress, turn off proxy buffering and
raise the read timeout so the event stream is not cut; `values.yaml` shows the
ingress-nginx annotations. `webhooks.enabled` adds the separate listener for
signed Slack and GitHub webhooks, on its own Service port.

## Flux

[`deploy/flux`](../deploy/flux) deploys the chart straight from this repository:

- [`clusters/example/warp.yaml`](../deploy/flux/clusters/example/warp.yaml): a
  `GitRepository` for this repository (narrowed to `deploy/`) and a Flux
  `Kustomization` that applies `deploy/flux/app`. Commit it where your cluster's
  Flux already reconciles.
- [`app/`](../deploy/flux/app): the `warp` namespace and a `HelmRelease` whose
  chart is `./deploy/helm/warp` from that source, rebuilt on every revision. Its
  values come from [`app/values.yaml`](../deploy/flux/app/values.yaml), an
  override example in the spec's shape (`system`, `platforms`), through a generated ConfigMap whose name changes with its
  content, so an edit upgrades the release.

To use your own values, copy `app/` into your fleet repository, edit
`values.yaml` and point the `Kustomization`'s `path` at the copy (with a
`sourceRef` to your fleet repository). Pin `ref` to a release tag rather than
`main`. The `warp-env` Secret is yours to create; with SOPS, commit it encrypted
beside `values.yaml` and turn on the `decryption` block shown in the file. Once
an image and chart are published to a registry, an `OCIRepository` can replace
the `GitRepository`; nothing is published yet.

## What has been checked

The compose files pass `docker compose config`, and both compose specs pass
`warp config check`; the dev spec, with memory stores, was started with `warp
serve` and served the console. `make chart-check` passes: `helm lint --strict`,
`kubeconform -strict` on the renders (the defaults with the required OIDC
values, every option on in
[`ci/full-values.yaml`](../deploy/helm/warp/ci/full-values.yaml), and the Flux
override), and `warp config check` on each rendered `warp.yaml`. The Flux
manifests pass `kubeconform` after `kustomize build`. `config check` does not
resolve references or read the files a spec names, so an unset variable or a
missing file shows only at start. The image has not been built and run here (no
Docker daemon), and nothing has been deployed to a cluster. See the
[status table](architecture.md#status).
