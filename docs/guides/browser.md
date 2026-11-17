# Connect a browser

Humagen uses the official [Playwright MCP server](https://github.com/microsoft/playwright-mcp)
for browser navigation, tabs, forms, and screenshots. The checked-in configuration
pins version 0.0.85 and thirteen tool schemas. Tools whose schema changes stay
unavailable until reviewed. This path needs Node.js 18 or newer and Chrome or
another browser supported by Playwright on the browser worker.

## Run a local browser worker

From the repository root, start an isolated browser in one terminal:

```sh
sh examples/browser/start.sh isolated
```

Downloads and capture files are retained under
`${XDG_CACHE_HOME:-$HOME/.cache}/humagen/browser` on that worker. Set
`HUMAGEN_BROWSER_OUTPUT_DIR` before starting it to choose another directory.
Download action results name the saved file; reading that file from another
host needs an explicitly connected file service or shared mount.

In another terminal, make your model provider credential available through the
reference in `examples/browser/humagen.yaml`, then start Humagen:

```sh
humagen start -f examples/browser/humagen.yaml
```

Open `http://127.0.0.1:8080` and message `@browser`. The example uses development
sign-in and memory state, so run it on your own computer. To use an existing
workspace instead, copy its `tools` entry into your Platform document and grant
`mcp:browser:write` to the chosen agent. Its model must support image input for
screenshots. Browser operations are classified as writes because even navigation
can change an authenticated service. The grant allows those operations; use
the workspace's approval policy where each operation needs a person's review.

**Development sign-in is only for trusted local fixtures.** Every client that
can reach that API is an administrator, including this browser worker. An
untrusted page can instruct an agent to navigate to the Humagen API, read its
private records, and leak them through a later URL. Before researching public
pages or connecting signed-in tabs, use an existing workspace with real OIDC
sign-in, or replace this example's `auth: dev` with the
[OIDC configuration](../reference/spec.md#system). Keep the browser's profile
separate from the profile signed in to Humagen. A loopback bind or a system-prompt
instruction does not authenticate the browser; Playwright origin filters also
do not provide a complete network security boundary.

The MCP endpoint is `http://127.0.0.1:8931/mcp`, reached by the Humagen server.
For a remote deployment, keep the browser on its own trusted worker and use an
authenticated tunnel or service. Exposing this unauthenticated loopback server
on a public interface is not part of this configuration.

## Use existing signed-in tabs

Install the official [Playwright browser extension](https://playwright.dev/mcp/configuration/browser-extension)
in the Chrome or Edge profile whose tabs you want to use. Start the worker in
extension mode instead of isolated mode:

```sh
sh examples/browser/start.sh extension
```

Approve the extension's browser/tab connection when prompted. The same Humagen
tool configuration then operates that connected browser. Login, SSO, MFA, and
the extension's tab handoff remain with the person. No existing profile is copied
or silently opened. Until that connection is approved, signed-in portal work is
blocked even if isolated-browser tests pass. A CDP endpoint explicitly opened
by the person is another server-supported connection, using `--cdp-endpoint`.

## Read pages and inspect pixels

After navigation or interaction, call `browser_snapshot` without `filename`.
In this pinned version, automatic snapshots are links to files on the browser
worker; they do not supply the tree to Humagen. Use the latest snapshot's
reference as `target` for click/type/form operations. For a screenshot, call
`browser_take_screenshot` with `scale: css`; the typed MCP image result carries
the actual bytes into a model that supports images. A saved path alone does not
prove that the model saw the screenshot.

## Run acceptance against the real connector

Start the isolated worker, then run:

```sh
HUMAGEN_BROWSER_MCP_URL=http://127.0.0.1:8931/mcp \
  HUMAGEN_BROWSER_OUTPUT_DIR="${XDG_CACHE_HOME:-$HOME/.cache}/humagen/browser" \
  go test -race ./internal/mcptools -run '^TestBrowserLive$' -v
```

This serves a temporary local page, navigates through the actual MCP adapter,
reads its accessible tree, clicks a real button by reference, verifies the changed
heading, downloads a selected CSV and checks its saved bytes, and checks the
decoded PNG's size and known background pixels. It uses
the checked-in accepted schemas rather than accepting whatever tools the server
offers. It needs no model credential and does not touch personal browser state.
An image-capable model reading the screenshot and a person connecting an
authenticated tab are additional end-to-end checks, not implied by this test.
