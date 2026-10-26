# Rename verification

Verification of the install and upgrade paths after the install-safety follow-up
merged in [PR #54](https://github.com/urmzd/humagen/pull/54). The checkout under
test is `dad280a`, release `v0.21.1`; the last release before the rename is
`v0.20.2`.

## Published module

From an empty module cache and an empty build cache,
`go install github.com/urmzd/humagen/cmd/humagen@v0.21.1` passed on macOS arm64
with Go 1.27.1. `go version -m` identifies the installed module as
`github.com/urmzd/humagen v0.21.1`. Running `humagen version` succeeded and
printed `dev (none, unknown)`: a plain Go install does not supply the release
linker flags. Installation did not require a checkout or local replacements.

## Container

The unmodified root Dockerfile built successfully for Linux arm64 with its
default Go 1.26.10 and Node 24 base images. The resulting image is
`humagen:codex-verification`, digest
`sha256:e0c912a52157601f87a28e342eef973c0a66af8cd126379cc7909594d8e2be00`.
Its configured user is `10001:10001`.

Both `docker run --rm humagen:codex-verification version` and the same image
with `--entrypoint /usr/local/bin/warp` exited successfully. The second
invocation printed the expected compatibility note:

```text
humagen: the command is humagen now; warp still runs it
```

Both printed `dev (none, unknown)` for the version because this build used
the Dockerfile's default build arguments. Initial downloads were slow and
pnpm retried several tarballs, but the build completed without source changes.

## Slack

`pnpm install --frozen-lockfile` and `pnpm validate:slack` passed with Node
24.15.0 and pnpm 11.6.0, with network access to Slack's public
`blocks.validate` endpoint. Every sandbox fixture was accepted, including the
three parts of `longTurn` (Slack returned warnings for those parts).

The app's live payload validation passed all 74 tests, with zero failures and
zero skips. This checks Block Kit acceptance; it does not install an app into
a Slack workspace or exercise an actual Socket Mode connection.

## Kubernetes upgrade

The documented Helm upgrade passed on kind 0.33.0 / Kubernetes 1.37 with
Helm 4.3.0, using the actual `v0.20.2` chart and Go binary for the baseline
and the new root Dockerfile image for the upgrade. The default Postgres
store stayed configured through the old `WARP_POSTGRES_URL` Secret key.

Revision 2 retained the Deployment UID, PVC UID and marker contents, Secret
UID and bytes, and container name `warp`. The new pod became ready and
`/healthz` returned `ok`. This verifies an in-place Deployment update rather
than a reinstall. The script records the before/after JSON and logs under
its `VERIFY_ARTIFACTS` directory.

The separate Flux upgrade passed with Flux 2.9.6, helm-controller 1.6.5 and
source-controller 1.9.6. Its GitRepository first fetched `v0.20.2`, then
commit `39f7f48`; the HelmRelease switched from `./deploy/helm/warp` to
`./deploy/helm/humagen` with `chartNameChangeStrategy: InPlaceUpdate` and
`nameOverride: warp`. The Deployment, PVC and Secret UIDs remained the same,
the Secret bytes and volume marker survived, the container stayed named
`warp`, and the new pod became ready with `/healthz` returning `ok`.

The original checks used the default Postgres slot. The memory-store
regression below was subsequently fixed and both store variants were checked again. An earlier
attempt was interrupted when the host's Colima VM stopped; the completed
checks above were rerun after the existing VM and isolated cluster recovered.

## Memory-store reuse-values regression

[Issue #62](https://github.com/urmzd/humagen/issues/62) records a live Helm
4.3.0 failure for an existing memory-store release. The old release is healthy
with an override containing `stores.kind: memory` and `stores.postgres: null`.
Its rendered spec omits the null key. An upgrade with `--reuse-values`
preserves `postgres: null` in the new spec, and the pod exits with:

```text
humagen: /etc/warp/humagen.yaml: document 1 (System warp): spec: unknown field "postgres"
```

The failing atomic upgrade rolled back. A regular `helm template` render drops
the null and therefore did not expose this live upgrade behavior.

The chart now copies the System values and omits only null store entries.
Configured inactive blocks, including empty maps and `false`, still reach
the unchanged strict spec decoder and are refused. Postgres without a database
reference is still refused. The regression injects retained values after
Helm's normal coalescing and covers both store kinds and custom null blocks;
zero execution limits, false settings and empty lists stay intact. CI runs
these checks through `make chart-check` with Helm 4.3.0.

On October 25, 2026, both the default Postgres and memory variants passed a
fresh kind upgrade from the actual `v0.20.2` chart and binary to the fixed
chart with the current `v0.25.1` Go source. Each used the documented
`--reuse-values`, `nameOverride` and image flags. No extra store override was
passed at upgrade. The Deployment, PVC and Secret UIDs, Secret bytes, volume
marker and container name stayed unchanged, and both pods became ready with
`/healthz` returning `ok`. The memory release's stored Helm values still
contain `postgres: null`, while its generated spec contains only
`stores: {kind: memory}`. The runtime was built from this branch and layered
onto the verified Alpine image; console assets were unchanged. Helm 3 and
old-to-old upgrades have not been checked.

## Reproduction

Use an empty directory for the module and build caches when checking the
published module:

```sh
verify_dir=$(mktemp -d)
GOMODCACHE="$verify_dir/mod" GOCACHE="$verify_dir/cache" \
  GOBIN="$verify_dir/bin" \
  go install github.com/urmzd/humagen/cmd/humagen@v0.21.1
"$verify_dir/bin/humagen" version
```

Build and check both command names from the repository root:

```sh
docker build -t humagen:codex-verification .
docker run --rm humagen:codex-verification version
docker run --rm --entrypoint /usr/local/bin/warp \
  humagen:codex-verification version
```

The isolated cluster upgrade checks are in `scripts/verify-upgrade.sh`. They
need kind, kubectl, Helm, Flux 2.9 or later, Python 3 and yq 4. The script
uses its own kubeconfig, test namespaces and dummy Secret, and compares the
Deployment, claim and Secret UIDs, Secret bytes, a volume marker, container
name and readiness across both upgrades. It deletes only a cluster it
created itself, unless `KEEP_CLUSTER=1` is set.

The old runtime tested here uses the real `v0.20.2` Go binary on the new
image's shared Alpine runtime and console assets. No published container
image exists for either release. To reproduce that old runtime on arm64:

```sh
git worktree add --detach .worktrees/install-pre-rename v0.20.2
old_image_dir=$(mktemp -d)
(
  cd .worktrees/install-pre-rename
  CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
    go build -trimpath -o "$old_image_dir/warp" ./cmd/warp
)
cat > "$old_image_dir/Dockerfile" <<'DOCKERFILE'
FROM humagen:codex-verification
USER root
RUN rm -f /usr/local/bin/warp
COPY warp /usr/local/bin/warp
USER 10001:10001
ENTRYPOINT ["/usr/local/bin/warp"]
DOCKERFILE
docker build -t humagen:codex-pre-rename "$old_image_dir"
docker pull postgres:16-alpine
OLD_CHECKOUT="$PWD/.worktrees/install-pre-rename" \
  OLD_IMAGE=humagen:codex-pre-rename NEW_IMAGE=humagen:codex-verification \
  scripts/verify-upgrade.sh
```

The fixture keeps the chart's default Postgres store, with an isolated
Postgres 16 Deployment and Service in each test namespace and an old-name
database reference in the preserved Secret. Its test-only OIDC client uses
Google's public discovery issuer. No sign-in or real credentials are involved.
Both rendered default specs are checked by their actual runtime images before
installation. The script also installs a separate memory release and reuses
its null backend values through an upgrade. Set `VERIFY_FLUX=0` to check both
local chart upgrades before its branch is available to the Flux source.
