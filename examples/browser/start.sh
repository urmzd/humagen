#!/bin/sh
set -eu

# Keep the browser worker local. An existing browser is connected only when
# the person selects extension mode and approves its tab handoff, or supplies
# an explicitly authorized CDP endpoint.
mode=${1:-isolated}
port=${HUMAGEN_BROWSER_MCP_PORT:-8931}
output_dir=${HUMAGEN_BROWSER_OUTPUT_DIR:-${XDG_CACHE_HOME:-$HOME/.cache}/humagen/browser}
mkdir -p "$output_dir"
if [ "$#" -gt 0 ]; then shift; fi
case "$mode" in
  isolated) set -- --headless --isolated "$@" ;;
  extension) set -- --extension --background "$@" ;;
  cdp)
    : "${HUMAGEN_BROWSER_CDP_ENDPOINT:?set the explicitly authorized CDP endpoint}"
    set -- --cdp-endpoint "$HUMAGEN_BROWSER_CDP_ENDPOINT" "$@"
    ;;
  *) printf '%s\n' 'usage: start.sh [isolated|extension|cdp] [Playwright MCP options]' >&2; exit 2 ;;
esac
exec npx --yes @playwright/mcp@0.0.85 \
  --host 127.0.0.1 --port "$port" --no-webmcp --image-responses allow \
  --output-dir "$output_dir" "$@"
