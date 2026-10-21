# syntax=docker/dockerfile:1.7
#
# The warp platform: one process that serves the API, sign-in, the event
# stream and the built web console. Build from the repository root:
#
#   docker build -t warp .
#
# Then run it with real sign-in (dev and mock auth listen only on loopback,
# so they cannot be reached through a published container port):
#
#   docker run -p 8080:8080 -v warp-data:/var/lib/warp \
#     -e WARP_OIDC_ISSUER=... -e WARP_OIDC_CLIENT_ID=... \
#     -e WARP_OIDC_CLIENT_SECRET=... -e WARP_OIDC_AUDIENCE=... \
#     warp serve -addr 0.0.0.0:8080 -web /usr/share/warp/web \
#       -public-url https://warp.example.com
#
# See docs/deploy.md.

ARG GO_VERSION=1.26.9
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
RUN pnpm exec turbo run build --filter=@warp/console

# 2. The Go binary, static, the way `make build` builds it.
FROM golang:${GO_VERSION}-alpine AS go
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY pkg ./pkg
COPY warpapp ./warpapp
COPY api ./api
ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath \
      -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
      -o /out/warp ./cmd/warp

# 3. The runtime: a small Alpine image, so the healthcheck has wget and an
# operator has a shell. Nothing else is installed: stdio MCP servers that
# need node or python must come from an image built on this one.
FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata \
 && addgroup -S -g 10001 warp \
 && adduser -S -D -H -u 10001 -G warp -h /var/lib/warp warp \
 && mkdir -p /var/lib/warp /etc/warp \
 && chown warp:warp /var/lib/warp
COPY --from=go /out/warp /usr/local/bin/warp
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

ENTRYPOINT ["/usr/local/bin/warp"]
# The default is OIDC sign-in; it needs the WARP_OIDC_* variables.
CMD ["serve", "-addr", "0.0.0.0:8080", "-web", "/usr/share/warp/web"]
