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
blocked even if isolated-browser tests pass. For a CDP endpoint explicitly opened by the person, use:

```sh
HUMAGEN_BROWSER_CDP_ENDPOINT=http://127.0.0.1:9222 \
  sh examples/browser/start.sh cdp
```

Do not expose that endpoint publicly or copy a personal browser profile.
`HUMAGEN_BROWSER_MCP_PORT` changes the worker port; update the configured MCP
URL to match. Connecting an existing browser grants access to its context; the
prompt asking for a particular tab and origin is not an authorization boundary.

## Read pages and inspect pixels

After navigation or interaction, call `browser_snapshot` without `filename`.
In this pinned version, automatic snapshots are links to files on the browser
worker; they do not supply the tree to Humagen. Use the latest snapshot's
reference as `target` for click/type/form operations. For a screenshot, call
`browser_take_screenshot` without `filename` and with `scale: css`; the typed MCP image result carries
the actual bytes into a model that supports images. A saved path alone does not
prove that the model saw the screenshot. In this pinned version, specifying
`filename` returns a saved-file reference instead of inline image bytes.

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

For visual review, set `HUMAGEN_BROWSER_CAPTURE_DIR` to retain `before.png` and
`after.png` from the same page, and `HUMAGEN_BROWSER_VIEWPORT` to `390x844` or
`1280x800`. Run a worker with `browser.contextOptions.colorScheme` set to `light`
and another with `dark` in its Playwright JSON config (`--config PATH`);
`HUMAGEN_BROWSER_EXPECT_THEME` checks that the page actually uses the chosen
appearance. Open both images in every pair before approval. These are synthetic
fixture pages, not evidence of a signed-in portal.


## Use an existing Claude Code account

`examples/browser/claude-code.yaml` uses the signed-in Claude Code account and
connects its browser MCP client to the same loopback worker. Set its
`claude-code.dir` to an existing dedicated download directory, then start it with:

```sh
humagen start -f examples/browser/claude-code.yaml
```

No separate model API key is required. This profile allows twelve named browser
tools and built-in Read/Glob for that directory, and supplies one strict MCP
configuration. It requires a Claude Code version with `--restricted` (tested
with 2.1.326). That mode confines built-in file tools to the working directories
and ignores user, project and local settings; managed settings still apply.
The acceptance actually attempts an outside-directory Read and verifies refusal.
This does not establish that every other customization is disabled. External
program permissions govern these tools, not Humagen's native sandbox or MCP
grants. The connected browser's tabs and origins are not confined by this file
control. Use real workspace sign-in for an authenticated browser, as above.

For an existing CDP browser, downloads follow Chrome's own download settings,
not the MCP output directory. Explicitly select the download folder and provide
that folder to the agent. For this example, configure the owned browser to save
into `<claude-code.dir>`. Put only authorized downloads in that dedicated folder:
all files inside it are readable, and selection among them remains an instruction.
Keep browser profiles, MCP capture output and unrelated files outside it.
The CLI must run on the same host or have an
explicit shared mount; a remote path alone cannot be read. Do not silently change
someone's personal browser preferences. Only authorize the selected download.

## Test the signed-in fixture end to end

The opt-in helper runs a real Humagen server, the pinned MCP worker, an independent
headless Chrome process, and the signed-in Claude Code model. Install the worker
in a separate prefix and build Humagen, then run from the repository root:

```sh
npm install --prefix /tmp/humagen-browser-tools @playwright/mcp@0.0.85
go build -o /tmp/humagen-browser ./cmd/humagen
HUMAGEN_BROWSER_LIVE=1 \
HUMAGEN_BROWSER_TOOL_DIR=/tmp/humagen-browser-tools \
HUMAGEN_BROWSER_BINARY=/tmp/humagen-browser \
HUMAGEN_BROWSER_CAPTURE_DIR=/tmp/humagen-browser-evidence \
node examples/browser/connected-acceptance.mjs
```

Set `HUMAGEN_BROWSER_CHROME` if Chrome is not at the standard macOS path or the
Linux `chromium` command. The helper requires Node.js, installed Chrome, and a
working Claude Code sign-in supporting `--restricted`. It creates only owned local fixture processes and
state, and stops those processes when finished; it does not touch a personal
profile. It uses a unique fixture workspace and verifies its identity before
submitting any work.

The actual acceptance passed at 390x844 and 1280x800 in light and dark. It logs
into a local fixture once, retains the same authenticated Chrome target across
all four model runs, and verifies the requested viewport and theme. Each run
reads two unlabelled panel colours from returned screenshot pixels, performs the
authorized button action, downloads the selected CSV, and reads it through the
CLI. Independent checks verify the changed heading, exactly one download,
record identity and exact file bytes. A loopback forwarding observer also checks
the actual MCP response delivered to the CLI: a nonempty PNG image block at the
requested pixel dimensions, with retained byte count and SHA256. A saved-file
link in the CLI's text journal is not used as proof of inline image delivery.
The helper keeps MCP images/snapshots in a separate artifacts directory and
archives each verified CSV outside the CLI directory before the next case; that
directory must be empty at each case's start. A separate harmless outside-file
Read must be refused by the real program. Paired PNGs, retained run/tool events
and wire image receipts are written to the capture directory. Open every before/after pair before
approval. Disconnecting the fixture's CDP client resets its display emulation,
so the helper keeps that connection attached while the MCP worker operates.

This is real connector and model evidence on a controlled local authenticated
fixture. It does not establish a third-party portal login, SSO/MFA, extension
handoff, cross-host download mount, or an authorization boundary around a tab.
Those remain separate checks with the person and target service.
