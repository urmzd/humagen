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

## What to know first

- **One replica.** The stores are single-writer: the run index, the usage
  ledger, the admission lock and the schedules assume one process owns them
  (see [Limits at scale](architecture.md#limits-at-scale)). A second warp on
  the same database races the first. The chart refuses `replicaCount` other
  than 1 and upgrades with `Recreate`.
- **Sign-in in a container is OIDC.** `-auth=dev`, `-auth=mock` and
  `-auth=tailscale` trust loopback and refuse any other listen address, and a
  published container port is not loopback. Use `-auth=oidc` with the
  `WARP_OIDC_ISSUER`, `WARP_OIDC_CLIENT_ID`, `WARP_OIDC_CLIENT_SECRET` and
  `WARP_OIDC_AUDIENCE` variables, and register `PUBLIC_URL/auth/callback` with
  your identity provider. See [security](security.md).
- **Stores.** `WARP_POSTGRES_URL` puts every store in Postgres; unset, they are
  in memory and a restart loses them. `WARP_REDIS_URL` moves the event bus out
  of the process; unset, it is in process, which is right for one replica. See
  [Storage](architecture.md#storage) for what a Postgres-only setup cannot do.
- **Published files** go to disk (`-storage=disk`, the workspace volume) or any
  S3-compatible bucket (`-storage=s3` and the `-s3-*` flags, with
  `WARP_S3_ACCESS_KEY_ID` and `WARP_S3_SECRET_ACCESS_KEY`, or the AWS default
  chain when both are unset).
- **Providers and settings** are files: `-providers FILE` for model and MCP tool
  providers, `-settings FILE` for platform settings. Credentials stay in the
  environment and are referred to from those files, never written in them.
- **Credentials come from the environment**, never from flags: a flag ends up in
  shell history and `ps`.

## The image

The [`Dockerfile`](../Dockerfile) at the root builds the console (`pnpm
install`, then `turbo run build --filter=@warp/console`) and a static `warp`
(`CGO_ENABLED=0`, as `make build`), and ships them on Alpine:

- `warp` at `/usr/local/bin/warp` is the entrypoint, the console is at
  `/usr/share/warp/web`.
- It runs as uid 10001, with `HOME` and `XDG_CACHE_HOME` under the
  `/var/lib/warp` volume, so the workspace (agents' files, and `blobs/` with
  disk storage) persists there.
- The default command is `serve -addr 0.0.0.0:8080 -web /usr/share/warp/web`,
  which signs in with OIDC.
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
cp deploy/compose/.env.example deploy/compose/.env   # fill in the OIDC values
make up                                              # build and start
make down                                            # stop, keep the volumes
make down V=1                                        # stop and delete the volumes
```

Every knob is in [`.env.example`](../deploy/compose/.env.example), commented.
`COMPOSE_PROFILES=s3,redis` there turns on the profiles. The `s3` profile also
needs `WARP_STORAGE=s3`, and the `redis` profile
`WARP_REDIS_URL=redis://redis:6379/0`. Files
in `deploy/compose/config` are mounted at `/etc/warp`; pass them with
`WARP_EXTRA_ARGS`, for example
`-providers /etc/warp/providers.yaml -settings /etc/warp/settings.yaml`.

warp is published on `127.0.0.1:8080` by default. To reach it from other
machines, set `WARP_BIND=0.0.0.0` behind TLS, and `WARP_PUBLIC_URL` to the
address browsers use.

### Without an identity provider

[`compose.dev.yaml`](../deploy/compose/compose.dev.yaml) runs warp with
`-auth=dev`, `-seed` and the offline provider on the host's network, listening
on `127.0.0.1:8080`, and publishes Postgres (and MinIO, Redis) on the host's
loopback for it. It needs host networking: Linux, or Docker Desktop 4.34+ with
it turned on.

```sh
cd deploy/compose
docker compose -f compose.yaml -f compose.dev.yaml up -d --build
```

Every request is an admin. Do not use it anywhere but your own machine.

## Kubernetes with Helm

[`deploy/helm/warp`](../deploy/helm/warp) deploys one warp: a Deployment, a
Service, an optional Ingress, a ConfigMap for the providers, settings, policy
and agent spec files (mounted at `/etc/warp` and passed with their flags), a
PersistentVolumeClaim for the workspace, and a ServiceAccount with no API token.
The pod runs as non-root with a read-only root filesystem and no capabilities.
Startup, liveness and readiness probes call `/healthz`. Every value is
documented in [`values.yaml`](../deploy/helm/warp/values.yaml).

Secrets never go in values. Create a Secret and name it with `existingSecret`;
its keys become the container's environment:

```sh
kubectl create namespace warp
kubectl -n warp create secret generic warp-env \
  --from-literal=WARP_OIDC_CLIENT_SECRET=... \
  --from-literal=WARP_POSTGRES_URL='postgres://warp:...@db:5432/warp' \
  --from-literal=ANTHROPIC_API_KEY=...

helm install warp deploy/helm/warp -n warp \
  --set image.repository=registry.example.com/warp --set image.tag=0.9.0 \
  --set auth.publicURL=https://warp.example.com \
  --set auth.oidc.issuer=https://login.example.com \
  --set auth.oidc.clientID=warp --set auth.oidc.audience=warp \
  --set existingSecret=warp-env
```

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
  override example, through a generated ConfigMap whose name changes with its
  content, so an edit upgrades the release.

To use your own values, copy `app/` into your fleet repository, edit
`values.yaml` and point the `Kustomization`'s `path` at the copy (with a
`sourceRef` to your fleet repository). Pin `ref` to a release tag rather than
`main`. The `warp-env` Secret is yours to create; with SOPS, commit it encrypted
beside `values.yaml` and turn on the `decryption` block shown in the file. Once
an image and chart are published to a registry, an `OCIRepository` can replace
the `GitRepository`; nothing is published yet.

## What has been checked

The compose files pass `docker compose config`. The chart passes `helm lint`,
and its rendered output (the defaults with the required OIDC values, every
option on in
[`ci/full-values.yaml`](../deploy/helm/warp/ci/full-values.yaml), and the Flux
override) passes `kubeconform -strict`, as do the Flux manifests after
`kustomize build`. The image's build steps were run outside Docker. The image
has not been built and run, and nothing has been deployed to a cluster. See the
[status table](architecture.md#status).
