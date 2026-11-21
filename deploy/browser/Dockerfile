# syntax=docker/dockerfile:1.7
#
# Humagen's browser worker: Chromium and the Playwright MCP server, at the
# version examples/browser pins, in one image. It serves the browser tools an
# agent is granted (docs/guides/browser.md) over MCP streamable HTTP on port
# 8931, at /mcp. Build from the repository root:
#
#   docker build -t humagen-browser deploy/browser
#
# The server has no sign-in of its own. Run it where only Humagen can reach
# it: the Helm chart's browser.enabled does that with a network policy, and
# deploy/compose keeps it on the compose network with no published port.

ARG NODE_VERSION=24

FROM node:${NODE_VERSION}-bookworm-slim

# The Playwright MCP release whose thirteen tool schemas examples/browser
# accepts by hash. A new version changes the hashes: update both together
# (TestBrowserImagePinsTheReviewedVersion).
ARG PLAYWRIGHT_MCP_VERSION=0.0.85

ENV NODE_ENV=production \
    PLAYWRIGHT_BROWSERS_PATH=/opt/browser/browsers \
    HUMAGEN_BROWSER_MCP_PORT=8931 \
    HUMAGEN_BROWSER_OUTPUT_DIR=/var/lib/humagen-browser

WORKDIR /opt/browser

# The server with the Playwright it depends on, then the Chromium build that
# Playwright names and the system libraries Chromium needs. One layer, so
# the package lists and npm's cache are not kept.
RUN npm install --omit=dev --no-audit --no-fund --no-update-notifier \
      "@playwright/mcp@${PLAYWRIGHT_MCP_VERSION}" \
    && node node_modules/playwright/cli.js install --with-deps chromium \
    && rm -rf /var/lib/apt/lists/* /root/.npm /tmp/*

COPY entrypoint.sh /opt/browser/entrypoint.sh

# Not root. /home/browser holds the browser's profile (its sign-ins) and
# /var/lib/humagen-browser its downloads and capture files: mount a volume at
# either to keep it past the container.
RUN useradd --uid 10001 --create-home --shell /usr/sbin/nologin browser \
    && mkdir -p /var/lib/humagen-browser \
    && chown browser:browser /var/lib/humagen-browser \
    && chmod 0555 /opt/browser/entrypoint.sh
USER 10001:10001

EXPOSE 8931
ENTRYPOINT ["/opt/browser/entrypoint.sh"]
