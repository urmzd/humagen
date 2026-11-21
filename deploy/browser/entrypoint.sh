#!/bin/sh
set -eu

# Starts the Playwright MCP server with one headless Chromium that every
# client shares: the same page and the same sign-ins, whichever Humagen
# process asks (a server pod's console, a worker pod's run). That is the
# browser docs/guides/browser.md describes, the workspace's and not a
# session's. Its profile is in HUMAGEN_BROWSER_PROFILE_DIR, so it lasts as
# long as the container does, or longer on a volume mounted there. Arguments
# are passed on to the server, after these.
#
# HUMAGEN_BROWSER_ALLOWED_HOSTS  the host and port Humagen reaches this server
#                                at, comma separated, exactly as the endpoint
#                                names it: "humagen-browser:8931". The server
#                                answers a request for any other Host with 403,
#                                so a page cannot reach it by a name of its
#                                own. Unset, only localhost:8931, 127.0.0.1:8931
#                                and [::1]:8931 are served. Set, the list
#                                replaces them.
# HUMAGEN_BROWSER_MCP_PORT       the port, 8931 when unset
# HUMAGEN_BROWSER_PROFILE_DIR    where the browser's profile is kept,
#                                /home/browser/profile when unset
# HUMAGEN_BROWSER_OUTPUT_DIR     where downloads and capture files go

port=${HUMAGEN_BROWSER_MCP_PORT:-8931}
output_dir=${HUMAGEN_BROWSER_OUTPUT_DIR:-/var/lib/humagen-browser}
profile_dir=${HUMAGEN_BROWSER_PROFILE_DIR:-/home/browser/profile}

if [ -n "${HUMAGEN_BROWSER_ALLOWED_HOSTS:-}" ]; then
  set -- --allowed-hosts "$HUMAGEN_BROWSER_ALLOWED_HOSTS" "$@"
fi

# Chromium's own sandbox needs user namespaces, which a container without
# extra privileges does not have. The container is the boundary here: run it
# as this unprivileged user, with no capabilities, and give it no route to
# anything but the internet (docs/guides/browser.md).
exec node /opt/browser/node_modules/@playwright/mcp/cli.js \
  --headless --browser chromium --no-sandbox --shared-browser-context \
  --user-data-dir "$profile_dir" \
  --host 0.0.0.0 --port "$port" \
  --no-webmcp --image-responses allow \
  --output-dir "$output_dir" "$@"
