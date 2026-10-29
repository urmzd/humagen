#!/usr/bin/env bash
# What humagen_dev (deploy/tilt/dev/Tiltfile) is running and whether it can:
# tool and humagen versions, the addresses, where state is kept, the spec
# checked with `humagen config check`, and the credentials that are not set.
# Tilt reruns it whenever a spec file changes.
#
# Exits non-zero when the spec does not check, or when a credential is missing
# and HUMAGEN_DEV_STRICT is 1, so the API waits for a fix instead of failing
# its start. Everything else is reported, never fatal.
#
# Read from the environment (the Tiltfile sets them):
#   HUMAGEN_DEV_BIN          the binary just built
#   HUMAGEN_DEV_ROOT         the humagen checkout the console runs from
#   HUMAGEN_DEV_GO_DIR       the Go module the binary is built from
#   HUMAGEN_DEV_SPECS        spec files and directories, one per line
#   HUMAGEN_DEV_CREDENTIALS  environment variables the project needs, space separated
#   HUMAGEN_DEV_STATE        where state is kept, in words
#   HUMAGEN_DEV_URLS         name=url pairs, one per line
#   HUMAGEN_DEV_STRICT       1 makes a missing credential fatal
set -uo pipefail

status=0
section() { printf '\n%s\n' "$1"; }
row() { printf '  %-12s %s\n' "$1" "$2"; }
# The first line a command prints, or why there is none.
first_line() {
	command -v "$1" >/dev/null 2>&1 || { echo "not installed"; return; }
	local out
	out=$("$@" 2>/dev/null | head -n1)
	echo "${out:-not answering}"
}

section "Versions"
row go "$(first_line go version | sed 's/^go version //')"
row node "$(first_line node --version)"
row pnpm "$(first_line pnpm --version)"
row docker "$(first_line docker version --format '{{.Server.Version}}')"
row tilt "$(first_line tilt version)"
row humagen "$(first_line "$HUMAGEN_DEV_BIN" version)"
console_version=$(sed -n 's/^  "version": "\(.*\)",$/\1/p' "$HUMAGEN_DEV_ROOT/apps/web/package.json" 2>/dev/null | head -n1)
row console "${console_version:-unknown} ($HUMAGEN_DEV_ROOT)"

# A project built on humagen pins it in go.mod; its console comes from the
# checkout Tilt loaded this from. Different versions are worth knowing.
module=$(cd "$HUMAGEN_DEV_GO_DIR" && go list -m -f '{{if .Replace}}{{.Replace.Path}} (replace){{else}}{{.Version}}{{end}}' github.com/urmzd/humagen 2>/dev/null || true)
if [ -n "$module" ]; then
	row module "github.com/urmzd/humagen $module"
	case "$module" in
	*replace*) ;;
	"v$console_version" | "$console_version") ;;
	*) echo "  warning: the binary is built on humagen $module but the console is $console_version; set ref (or humagen_dir) to match go.mod" ;;
	esac
fi

section "Addresses"
while IFS='=' read -r name url; do
	[ -n "$name" ] && row "$name" "$url"
done <<<"${HUMAGEN_DEV_URLS:-}"

section "State"
row kept "${HUMAGEN_DEV_STATE:-memory, lost on restart}"

section "Spec"
specs=()
while IFS= read -r p; do
	[ -n "$p" ] && specs+=(-f "$p")
done <<<"${HUMAGEN_DEV_SPECS:-}"
if [ ${#specs[@]} -eq 0 ]; then
	echo "  none: the seeded example agents on the offline model"
elif out=$("$HUMAGEN_DEV_BIN" config check "${specs[@]}" 2>&1); then
	sed 's/^/  /' <<<"$out"
else
	sed 's/^/  /' <<<"$out"
	echo "  The spec does not check: the API waits until it does."
	status=1
fi

section "Credentials"
missing=()
for name in ${HUMAGEN_DEV_CREDENTIALS:-}; do
	if [ -n "${!name:-}" ]; then
		row "$name" set
	else
		row "$name" "NOT SET"
		missing+=("$name")
	fi
done
if [ -z "${HUMAGEN_DEV_CREDENTIALS:-}" ]; then
	echo "  none declared (the spec check above names any reference that is unset)"
elif [ ${#missing[@]} -gt 0 ]; then
	echo "  Set them in the shell that runs tilt, or in the env_file the Tiltfile names."
	if [ "${HUMAGEN_DEV_STRICT:-0}" = 1 ]; then
		status=1
	fi
fi

exit $status
