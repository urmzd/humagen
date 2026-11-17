#!/bin/sh
set -eu

# Keep the browser worker local. An existing browser is connected only when
# the person explicitly selects extension mode and approves its tab handoff.
mode=${1:-isolated}
output_dir=${HUMAGEN_BROWSER_OUTPUT_DIR:-${XDG_CACHE_HOME:-$HOME/.cache}/humagen/browser}
mkdir -p "$output_dir"
if [ "$#" -gt 0 ]; then shift; fi
case "$mode" in
  isolated) set -- --headless --isolated "$@" ;;
  extension) set -- --extension --background "$@" ;;
  *) printf '%s\n' 'usage: start.sh [isolated|extension] [Playwright MCP options]' >&2; exit 2 ;;
esac
exec npx --yes @playwright/mcp@0.0.85 \
  --host 127.0.0.1 --port 8931 --no-webmcp --image-responses allow \
  --output-dir "$output_dir" "$@"
