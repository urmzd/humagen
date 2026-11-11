#!/bin/sh
# An upgrade from an earlier release, end to end (internal/e2e): the release's
# own binary serves a fresh database and does real work (sessions, a finished
# run, a run parked on a question, a schedule, a pending delivery), then this
# checkout's binary starts on the same database and must carry all of it on.
# A fake Anthropic model answers, and both processes are given a proxy that
# refuses every outside connection, so no model and no surface is called.
#
#   scripts/upgrade-from-release.sh            # from v0.117.1, Postgres in Docker on 127.0.0.1:55434
#   scripts/upgrade-from-release.sh v0.117.3   # from another tag or commit
#   PG_PORT=55435 scripts/upgrade-from-release.sh
#   HUMAGEN_E2E_POSTGRES_URL=postgres://... scripts/upgrade-from-release.sh   # your own Postgres
#   HUMAGEN_E2E_OLD_BIN=/path/to/humagen scripts/upgrade-from-release.sh      # a release binary you built
set -eu
cd "$(dirname "$0")/.."
GO=${GO:-go}
from=${1:-v0.117.1}
tmp=$(mktemp -d)
old=""
name=""
cleanup() {
	[ -z "$name" ] || docker rm -f "$name" >/dev/null 2>&1 || true
	[ -z "$old" ] || git worktree remove --force "$old" >/dev/null 2>&1 || true
	rm -rf "$tmp"
}
trap cleanup EXIT INT TERM

if [ -z "${HUMAGEN_E2E_OLD_BIN:-}" ]; then
	# The release is built from its own source, in a worktree of its own.
	old="$tmp/release"
	git worktree add --detach "$old" "$from" >/dev/null
	(cd "$old" && $GO build -o "$tmp/humagen-release" ./cmd/humagen)
	export HUMAGEN_E2E_OLD_BIN="$tmp/humagen-release"
fi

if [ -z "${HUMAGEN_E2E_BIN:-}" ]; then
	$GO build -o "$tmp/humagen" ./cmd/humagen
	export HUMAGEN_E2E_BIN="$tmp/humagen"
fi

if [ -z "${HUMAGEN_E2E_POSTGRES_URL:-}" ]; then
	port=${PG_PORT:-55434}
	name=humagen-upgrade-pg-$port
	docker run -d --rm --name "$name" -e POSTGRES_PASSWORD=pg -p "127.0.0.1:$port:5432" pgvector/pgvector:pg17 >/dev/null
	until docker exec "$name" pg_isready -U postgres >/dev/null 2>&1; do sleep 1; done
	export HUMAGEN_E2E_POSTGRES_URL="postgres://postgres:pg@127.0.0.1:$port/postgres?sslmode=disable"
fi

# Not exec: the trap above removes the container and the worktree.
$GO test ./internal/e2e/ -run TestUpgradeFromRelease -count=1 -v -timeout 10m
