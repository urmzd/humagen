#!/bin/sh
# Two humagen replicas on one Postgres, end to end (internal/e2e):
# live updates across replicas, a question answered on the other replica,
# a replica stopped (SIGTERM, drain) and killed (SIGKILL) mid-turn, and no
# event delivered twice. A fake Anthropic model answers; no model is called.
#
#   scripts/two-replica.sh            # Postgres in Docker on 127.0.0.1:55432
#   PG_PORT=55433 scripts/two-replica.sh
#   HUMAGEN_E2E_POSTGRES_URL=postgres://... scripts/two-replica.sh  # your own
#
# Against two running replicas instead (a port-forward to each pod), no
# Docker or build:
#   HUMAGEN_E2E_A_URL=http://127.0.0.1:18081 HUMAGEN_E2E_B_URL=http://127.0.0.1:18082 \
#   HUMAGEN_E2E_AGENT=<agent> HUMAGEN_E2E_LOGIN=<tailnet login> scripts/two-replica.sh
set -eu
cd "$(dirname "$0")/.."
GO=${GO:-go}

if [ -z "${HUMAGEN_E2E_A_URL:-}" ] && [ -z "${HUMAGEN_E2E_POSTGRES_URL:-}" ]; then
	port=${PG_PORT:-55432}
	name=humagen-e2e-pg-$port
	docker run -d --rm --name "$name" -e POSTGRES_PASSWORD=pg -p "127.0.0.1:$port:5432" pgvector/pgvector:pg16 >/dev/null
	trap 'docker rm -f "$name" >/dev/null 2>&1 || true' EXIT INT TERM
	until docker exec "$name" pg_isready -U postgres >/dev/null 2>&1; do sleep 1; done
	export HUMAGEN_E2E_POSTGRES_URL="postgres://postgres:pg@127.0.0.1:$port/postgres?sslmode=disable"
fi

if [ -z "${HUMAGEN_E2E_A_URL:-}" ] && [ -z "${HUMAGEN_E2E_BIN:-}" ]; then
	bin=$(mktemp -d)/humagen
	$GO build -o "$bin" ./cmd/humagen
	export HUMAGEN_E2E_BIN="$bin"
fi

# Not exec: the trap above removes the Postgres container when the test ends.
$GO test ./internal/e2e/ -run TestTwoReplicas -count=1 -v -timeout 10m
