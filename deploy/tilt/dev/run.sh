#!/usr/bin/env bash
# Starts one process for humagen_dev (deploy/tilt/dev/Tiltfile):
#
#   run.sh [--port N]... [--wait HOST:PORT]... -- COMMAND [ARG]...
#
# Each --port must be free: one still held after a few seconds stops the start
# with the process holding it named, rather than a bind error later. Each
# --wait must accept a connection within a minute (the database). Then the
# script becomes COMMAND (exec), so the signal Tilt sends on a restart or on
# shutdown reaches the process itself and no child is left holding a port.
set -euo pipefail

ports=()
waits=()
while [ $# -gt 0 ]; do
	case "$1" in
	--port) ports+=("$2"); shift 2 ;;
	--wait) waits+=("$2"); shift 2 ;;
	--) shift; break ;;
	*) echo "run.sh: unknown argument $1" >&2; exit 2 ;;
	esac
done
if [ $# -eq 0 ]; then
	echo "run.sh: no command after --" >&2
	exit 2
fi

# Something accepts connections on 127.0.0.1:$1.
listening() {
	(exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null
}

for port in "${ports[@]}"; do
	# A restart's old process may take a moment to let go.
	for _ in 1 2 3 4 5 6 7 8 9 10; do
		listening "$port" || continue 2
		sleep 0.5
	done
	echo "humagen dev: port $port is already in use." >&2
	if command -v lsof >/dev/null 2>&1; then
		lsof -nP -iTCP:"$port" -sTCP:LISTEN >&2 || true
	fi
	echo "Stop that process, or pick another port in the Tiltfile." >&2
	exit 1
done

for target in "${waits[@]}"; do
	host=${target%:*}
	port=${target##*:}
	ok=
	for _ in $(seq 1 120); do
		if (exec 3<>"/dev/tcp/$host/$port") 2>/dev/null; then
			ok=1
			break
		fi
		sleep 0.5
	done
	if [ -z "$ok" ]; then
		echo "humagen dev: nothing answered on $host:$port within a minute." >&2
		exit 1
	fi
done

exec "$@"
