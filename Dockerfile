# syntax=docker/dockerfile:1.7
#
# The Humagen platform: one process that serves the API, sign-in, the event
# stream and the built web console. Build from the repository root:
#
#   docker build -t humagen .
#
# Then run it with a spec (docs/reference/spec.md) in /etc/warp. Its System document
# must sign in with OIDC: dev, mock and tailscale listen only on loopback, so
# they cannot be reached through a published container port. Credentials are
# references in the spec, read from the environment:
#
#   docker run -p 8080:8080 -v warp-data:/var/lib/warp \
#     -v "$PWD/config:/etc/warp:ro" \
#     -e HUMAGEN_OIDC_CLIENT_SECRET=... -e HUMAGEN_POSTGRES_URL=... humagen
#
# The paths in the image (/var/lib/warp, /etc/warp, /usr/share/warp/web) and
# the uid keep the names they had before the product was renamed, so volumes
# and specs written for those paths keep working. See docs/guides/deploy.md and
# docs/guides/upgrading-from-warp.md.

# At least the go directive in go.mod: the golang image sets GOTOOLCHAIN=local,
# so it cannot fetch a newer toolchain.
ARG GO_VERSION=1.26.10
ARG NODE_VERSION=24

# 1. The web console: a Vite build inside the pnpm workspace.
FROM node:${NODE_VERSION}-alpine AS web
WORKDIR /src
RUN corepack enable
# Manifests first, so the dependency layer is reused until they change.
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml turbo.json tsconfig.base.json tsconfig.json ./
COPY apps ./apps
COPY packages ./packages
COPY examples/ui ./examples/ui
RUN --mount=type=cache,id=pnpm,target=/root/.local/share/pnpm/store \
    pnpm install --frozen-lockfile
RUN pnpm exec turbo run build --filter=@humagen/console

# 2. The Go binary, static, the way `make build` builds it.
FROM golang:${GO_VERSION}-alpine AS go
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY pkg ./pkg
COPY *.go ./
# The changelog the root package embeds, for workspace.release_notes.
COPY CHANGELOG.md ./
COPY api ./api
# The Helm chart `humagen deploy` carries, embedded by deploy/chart.go.
COPY deploy/chart.go ./deploy/
COPY deploy/helm/humagen ./deploy/helm/humagen
ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath \
      -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
      -o /out/humagen ./cmd/humagen

# 3. The runtime: a small Alpine image, so the healthcheck has wget and an
# operator has a shell. Nothing else is installed: stdio MCP servers that
# need node or python must come from an image built on this one.
FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata \
 && addgroup -S -g 10001 humagen \
 && adduser -S -D -H -u 10001 -G humagen -h /var/lib/warp humagen \
 && mkdir -p /var/lib/warp /etc/warp \
 && chown humagen:humagen /var/lib/warp
COPY --from=go /out/humagen /usr/local/bin/humagen
# The command's old name still runs it, with a note, for custom entrypoints
# and `docker exec ... warp` written before the rename.
RUN ln -s humagen /usr/local/bin/warp
COPY --from=web /src/apps/web/dist /usr/share/warp/web

# The workspace (agents' files and, with -storage=disk, published files'
# bytes) lives under the user cache directory, which is pointed into the
# volume. A disk store defaults to blobs/ inside the workspace.
ENV HOME=/var/lib/warp \
    XDG_CACHE_HOME=/var/lib/warp/cache
VOLUME ["/var/lib/warp"]
WORKDIR /var/lib/warp
USER 10001:10001
EXPOSE 8080

HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null || exit 1

ENTRYPOINT ["/usr/local/bin/humagen"]
# The spec is mounted at /etc/warp; the listen address and the console are the
# image's, as flags, which win over the spec.
CMD ["serve", "-f", "/etc/warp", "-addr", "0.0.0.0:8080", "-web", "/usr/share/warp/web"]
