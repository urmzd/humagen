.PHONY: all init build test integration lint fmt check start dev dev-live dev-signin web tilt screens up down chart-check image client client-check spec-lint oag-install

BIN := humagen

# The generator that writes packages/client from api/openapi.yaml, and the one
# version of it whose output is committed. CI regenerates with the same version
# and fails on any difference.
OAG ?= oag
OAG_VERSION := 0.20.6
# sha256 of each v$(OAG_VERSION) release asset, computed from the downloads.
# oag publishes no checksums (urmzd/oag#37), so they are pinned here and the
# download is refused on any mismatch.
OAG_SHA256_x86_64-unknown-linux-musl := 61ab4f13f49a32191ea3783ab05708cfd571192b7ea4c6f61420f4c53b10ae3e
OAG_SHA256_aarch64-unknown-linux-musl := 4e4610685960dd7d2885b11d24be5a6e70e39f61e9096fa72f93d522ce13fafe
OAG_SHA256_aarch64-apple-darwin := 347a29cde8f0a6e5be042ac8af2d17980c55a663e996deef3424a0337c7803a5
OAG_SHA256_x86_64-apple-darwin := f4a94758b6b3c4b955973349efde46ff1f5e41b19da2e133391f76d86ee1d701
OAG_DIR ?= $(HOME)/.local/bin
REDOCLY := npx --yes @redocly/cli@2.63.0

all: check

init:
	git config core.hooksPath .githooks
	go mod download && go mod tidy

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/$(BIN) ./cmd/$(BIN)

test:
	go test ./... -race

# fiber's backend conformance suites against Postgres (pgvector) and Redis
# from the compose integration profile. Without these variables they skip.
integration:
	$(COMPOSE) --profile integration up -d --wait fiber-postgres redis
	FIBER_POSTGRES_TEST_URL=postgres://fiber:fiber@localhost:$${FIBER_POSTGRES_PORT:-5433}/fiber?sslmode=disable \
	FIBER_REDIS_ADDR=localhost:$${FIBER_REDIS_PORT:-6379} \
	go test -race -count=1 ./fiber/...

lint:
	golangci-lint run

fmt:
	gofmt -w .

check: fmt lint test

# The whole platform with zero configuration: console, seeded agents, and
# the first-run questions on a terminal.
start: build
	pnpm install && pnpm --filter @humagen/console build
	./bin/$(BIN) start

# Local preview: no sign-in, loopback only, two example agents.
dev: build
	./bin/$(BIN) serve -auth=dev -seed -sandbox-runtime=runc -sandbox-insecure-development-runtime -builtin-provider offline -builtin-model offline

# Local preview on real models: Anthropic, then OpenAI, then Gemini, each
# with its key from the shell (examples/dev/humagen.yaml). Use with `make web`.
dev-live: build
	./bin/$(BIN) serve -auth=dev -sandbox-runtime=runc -sandbox-insecure-development-runtime -f examples/dev/humagen.yaml -builtin-provider anthropic -builtin-model claude-haiku-5-5

# Local preview with sign-in: a development identity provider on loopback
# (127.0.0.1:8081) and personas to pick from, no passwords. Use with `make web`
# and open http://127.0.0.1:5173/auth/login.
dev-signin: build
	./bin/$(BIN) serve -auth=mock -seed -sandbox-runtime=runc -sandbox-insecure-development-runtime -public-url http://127.0.0.1:5173 -builtin-provider offline -builtin-model offline

# The web console, proxying /api and /auth to the server from `make dev`.
web:
	pnpm install
	pnpm --filter @humagen/console dev

# The API, the console with hot reload and Postgres in Docker under Tilt, each
# restarted only when its own sources change (docs/guides/develop.md). Stop
# with `tilt down -f dev.Tiltfile`; the database is kept.
tilt:
	tilt up -f dev.Tiltfile

# Every console screen at phone (390x844) and desktop (1280x800) width, from a
# humagen seeded with example data, with the layout checks that fail the run
# (scripts/screens). Uses an installed Chromium or Chrome and downloads none.
# Screenshots and index.html, which shows them side by side, go to .screens/.
screens: build
	pnpm install --frozen-lockfile
	pnpm --filter @humagen/console build
	pnpm --filter @humagen/screens screens

# The container image, from the Dockerfile at the root.
IMAGE ?= humagen:local
image:
	docker build -t $(IMAGE) .

# humagen, Postgres and the profiles in deploy/compose/.env (COMPOSE_PROFILES), built
# from this checkout. Copy deploy/compose/.env.example to .env first.
COMPOSE := docker compose --project-directory deploy/compose -f deploy/compose/compose.yaml
up:
	$(COMPOSE) up -d --build

# Stops the stack and keeps its volumes. `make down V=1` removes them too.
down:
	$(COMPOSE) down $(if $(V),-v)

# Lint and render the Helm chart, and run `humagen config check` on the spec each
# render produces, and on the compose stack's. Needs helm; kubeconform is used
# when it is on PATH.
chart-check:
	deploy/helm/check.sh
	go run ./cmd/humagen config check -f deploy/compose/config
	go run ./cmd/humagen config check -f deploy/compose/dev

# Regenerate the TypeScript client from the API contract. The output under
# packages/client/src/generated is committed.
client:
	@$(OAG) version | grep -qx 'oag v$(OAG_VERSION)' || { \
		echo "make client needs oag v$(OAG_VERSION), found: $$($(OAG) version 2>&1 | head -1)"; \
		echo "install it with: make oag-install, then put $(OAG_DIR) on PATH"; \
		exit 1; }
	cd packages/client && $(OAG) generate

# Build, typecheck and test the client package.
client-check:
	pnpm install --frozen-lockfile
	pnpm welcome-copy:check
	pnpm --filter @humagen/client typecheck
	pnpm --filter @humagen/client test

# Lint the API contract.
spec-lint:
	$(REDOCLY) lint api/openapi.yaml --config api/redocly.yaml

# Download the pinned oag release for this machine into $(OAG_DIR), checking
# its sha256 before it is made executable.
oag-install:
	@case "$$(uname -s)-$$(uname -m)" in \
		Linux-x86_64) target=x86_64-unknown-linux-musl ;; \
		Linux-aarch64) target=aarch64-unknown-linux-musl ;; \
		Darwin-arm64) target=aarch64-apple-darwin ;; \
		Darwin-x86_64) target=x86_64-apple-darwin ;; \
		*) echo "no pinned oag build for $$(uname -s)-$$(uname -m)"; exit 1 ;; \
	esac; \
	case "$$target" in \
		x86_64-unknown-linux-musl) want=$(OAG_SHA256_x86_64-unknown-linux-musl) ;; \
		aarch64-unknown-linux-musl) want=$(OAG_SHA256_aarch64-unknown-linux-musl) ;; \
		aarch64-apple-darwin) want=$(OAG_SHA256_aarch64-apple-darwin) ;; \
		x86_64-apple-darwin) want=$(OAG_SHA256_x86_64-apple-darwin) ;; \
	esac; \
	tmp=$$(mktemp); \
	curl -fsSL -o "$$tmp" "https://github.com/urmzd/oag/releases/download/v$(OAG_VERSION)/oag-$$target" || { rm -f "$$tmp"; exit 1; }; \
	got=$$( (command -v sha256sum >/dev/null && sha256sum "$$tmp" || shasum -a 256 "$$tmp") | cut -d' ' -f1); \
	if [ "$$got" != "$$want" ]; then echo "oag-$$target: sha256 $$got, want $$want"; rm -f "$$tmp"; exit 1; fi; \
	mkdir -p "$(OAG_DIR)" && mv "$$tmp" "$(OAG_DIR)/oag" && chmod +x "$(OAG_DIR)/oag"; \
	echo "installed oag v$(OAG_VERSION) ($$target, sha256 verified) to $(OAG_DIR)/oag"
