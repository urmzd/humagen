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

## Run the bundled browser image

`deploy/browser` builds one image with Chromium and the same Playwright MCP
release, so a deployment needs no Node or Chrome on a worker:

```sh
docker build -t humagen-browser deploy/browser
```

It serves MCP at `/mcp` on port 8931, headless, as a user with no privileges.
Run it where only Humagen reaches it. It has no sign-in of its own, and it
offers more tools than the thirteen Humagen accepts: whatever can reach its
port can call any of them, with no grant and no audit record.

| Where | How |
|-------|-----|
| Kubernetes | `browser.enabled: true` in the Helm chart, with `browser.image` naming the image you pushed. The chart adds the pod, a Service, a network policy and the `browser` tool server in one workspace's spec, with the thirteen tools accepted |
| Docker Compose | `docker compose -f compose.yaml -f compose.browser.yaml up -d --build` in `deploy/compose`. Add the tool server from `examples/browser/humagen.yaml` to your Platform document with `endpoint: http://browser:8931/mcp`. Not with the development overlay, whose server is on the host's network |

Then grant an agent `mcp:browser:write`. Humagen and the browser may start in
either order: a tool server that is not up yet is asked again until it
answers, and its tools are accepted then.

What the image and the chart hold to:

- **One browser for every Humagen process.** A server pod's Browser tab and a
  worker pod's run share the sign-ins and the tabs. A browser started with
  `--isolated`, as the local worker above is, gives each process its own,
  which is right for one process and wrong for several. No client may close
  the shared browser.
- **Each connection has its own current tab.** An agent that opens a second
  tab moves only itself there. The console's Browser tab, on another
  connection, stays on the tab it was on, and has no action to change tab.
  While everything stays in one tab, every connection sees the same page.
- **It lasts as long as its container.** The server closes the browser when
  its last client leaves, so the image keeps a client of its own connected
  (`anchor.mjs`), and never closes an idle browser. That client asks the
  browser for its tabs every half minute and connects again if it gets no
  answer. A restart of every Humagen pod then leaves the page and the
  sign-ins where they were. A
  restart of the browser's own pod loses both: the chart keeps the profile in
  the pod and has no volume for it. With `docker run` or Compose, a volume at
  `/home/browser` keeps the profile.
- **One workspace.** It is one set of sign-ins, so the chart gives it to one
  workspace: `browser.workspace` names it when there are several.
- **The name it is reached by.** The server answers a request that names the
  host and port in `HUMAGEN_BROWSER_ALLOWED_HOSTS`, exactly as the endpoint
  names them (`browser:8931`), and answers one that names another with 403. A
  health check that sends a request is refused the same way: check the port.
- **No way into the cluster, by a list of what to refuse.** A page an agent
  opens is not trusted. The chart's network policy lets only this release's
  server and worker pods reach the browser. It lets the browser ask the
  cluster's DNS for names (`browser.networkPolicy.dns`: the pods of
  `kube-system`, and the node-local and metadata resolvers, on port 53; name
  your own where DNS is elsewhere, such as OpenShift's `openshift-dns` on
  5353), and reach the internet on ports 80 and 443, but
  for the private, link-local, shared and reserved ranges and the addresses
  clouds serve metadata on. If your cluster's pods, services or nodes have
  addresses outside those ranges, add them to
  `browser.networkPolicy.blockedCIDRs`, or the browser reaches the cluster.
  IPv6 is not let out. All of this needs a network plugin that enforces
  NetworkPolicy. The pod holds no service account token.
- **Under Compose, a network of its own, and nothing refused.** The browser
  shares a network with the server only, not with Postgres or MinIO. The
  server's API is within its reach. Compose has no egress policy: a page the
  browser opens reaches whatever the host reaches, such as the host's LAN,
  its published ports and, on a cloud VM, the metadata address
  `169.254.169.254` and the credentials it serves. Use Compose on a machine
  with nothing to reach, or block those addresses in the host's firewall.
- **Bounded files.** Every snapshot and screenshot writes a file. The oldest
  go when the output directory passes `HUMAGEN_BROWSER_OUTPUT_MAX_BYTES`,
  256 MiB unless set. The browser also keeps a temporary copy of each
  download until it closes, which here is never, so the image removes those
  when they are ten minutes old. The chart bounds the pod's disk at 2Gi: a
  pod that passes it is evicted, and its sign-ins go with it. One download
  larger than that still does it.
- **Chromium runs without its own sandbox.** A container without extra
  privileges has no user namespaces for it. The container is the boundary:
  read-only, no capabilities, no privilege escalation. A renderer exploit
  holds the pod. In the chart, set `browser.runtimeClassName` to a sandboxed
  runtime such as gVisor to put a kernel between the pod and the node.

To check an image, start it and run the acceptance below with
`HUMAGEN_BROWSER_SHARED=1`, which adds two checks: that two clients share one
browser, and that the page outlives every client. On Linux, put the container
on the host's network, as CI does, so the browser reaches the page the test
serves:

```sh
out="$HOME/.cache/humagen/browser-image-test" && mkdir -p "$out" && chmod 777 "$out"
docker run -d --name humagen-browser --network host --read-only --cap-drop ALL \
  --security-opt no-new-privileges \
  --tmpfs /tmp --tmpfs /home/browser:uid=10001,gid=10001 \
  -v "$out:/var/lib/humagen-browser" humagen-browser
HUMAGEN_BROWSER_MCP_URL=http://127.0.0.1:8931/mcp HUMAGEN_BROWSER_OUTPUT_DIR="$out" \
  HUMAGEN_BROWSER_SHARED=1 go test -race ./internal/mcptools -run '^TestBrowser' -v
```

On a Mac the container is in a virtual machine and has no host network.
Publish the port, tell the image the address you reach it at, and name this
host as the container sees it (`host.docker.internal`, or
`host.lima.internal` under Colima):

```sh
docker run -d --name humagen-browser --read-only --cap-drop ALL \
  --security-opt no-new-privileges \
  --tmpfs /tmp --tmpfs /home/browser:uid=10001,gid=10001 \
  -e HUMAGEN_BROWSER_ALLOWED_HOSTS=127.0.0.1:18931 \
  -v "$out:/var/lib/humagen-browser" -p 127.0.0.1:18931:8931 humagen-browser
HUMAGEN_BROWSER_MCP_URL=http://127.0.0.1:18931/mcp HUMAGEN_BROWSER_OUTPUT_DIR="$out" \
  HUMAGEN_BROWSER_SHARED=1 HUMAGEN_BROWSER_FIXTURE_HOST=host.docker.internal \
  go test -race ./internal/mcptools -run '^TestBrowser' -v
```

CI builds the image and runs this on every change to it.

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

## Look at the browser and act on it from the console

A session whose agent used the browser has a Browser tab in its workbench, and
a card under each answer that used it, which opens the tab. The tab lists what
the agent did, step by step, with the page each step left the browser on.

A person who may post in the session can act on that same browser from the
tab: take a screenshot, go to an address, go back, and click or type into
something on the page. Pick the element on the screenshot or in the list under
it, then press Click or Type. The screenshot is taken again after each action.
This is the way to sign in for the agent, pass a prompt it cannot, or put the
browser on the right page before asking for more.

Each thing done this way is listed above the composer and goes with the
person's next message, so the agent reads where the browser was left. What was
typed is never in that list: it says how many characters, and into what.
Nothing is sent to the agent before the person sends a message.

The tab calls `POST /api/sessions/{id}/browser` (`runs:write`). It reaches the
browser through the provider the session's agent holds a write grant on, on
the connection the agent's own runs use, and only with the tools an admin
accepted. It is refused when:

- the session's agent holds no `mcp:<name>:write` grant on a provider whose
  accepted tools include `browser_take_screenshot`;
- that provider is reached over stdio, or with each person's own credential;
- the caller may not post in the session, or is not a person;
- the action policy for the agent, the project or the person says `ask` or
  `never` for `other` or `external_messages`. A request for approval is the
  agent's to make, so ask the agent instead;
- the address is not `http` or `https`, or the target is not an element
  reference from the page's own tree.

Every action but a screenshot is in the audit log as `session.browser.<action>`
with the address, the element and the key. Text typed is recorded as a count.

The browser is the workspace's, not the session's: a screenshot shows whatever
that browser shows now, whichever session last used it. That is the same
browser any agent holding the grant would be shown. Keep one provider per
group of people who may see each other's pages.

What is not there:

- Screenshots the agent took are not kept. A tool's result reaches a run's
  events as text, so the tab has the agent's steps and no picture of them. The
  pictures in the tab are the ones taken from it, kept in the browser tab's
  memory and gone on reload.
- No live video. The page is drawn again after each action and on Refresh.
- No click by coordinates, drag, hover, file upload or dialog answer by hand.
  The pinned tool list has no coordinate tools, and the rest are left to the
  agent.
- A session whose agent calls another agent that holds the grant has the
  steps and the card, and no control: the session's own agent holds no browser.

## Read pages and inspect pixels

After navigation or interaction, call `browser_snapshot` without `filename`.
In this pinned version, automatic snapshots are links to files on the browser
worker; they do not supply the tree to Humagen. Use the latest snapshot's
reference as `target` for click/type/form operations. For a screenshot, call
`browser_take_screenshot` without `filename` and with `scale: css`; the typed MCP image result carries
the actual bytes into a model that supports images. A saved path alone does not
prove that the model saw the screenshot. In this pinned version, specifying
`filename` returns a saved-file reference instead of inline image bytes.

## Compare an uploaded screenshot with a changed page

With `examples/browser/humagen.yaml`, first attach the original PNG to a message
for the browser agent and ask it to inspect only that image. For example:

```text
Read only the attached screenshot's pixels. Describe the visible heading and
the unlabelled panels' colors from left to right. Do not use tools, open a
browser or change the page yet. Say what you cannot see.
```

After reviewing that answer, authorize the specific connected tab and action
in a follow-up in the same conversation:

```text
Use only my authorized tab at EXACT_ORIGIN_AND_PATH. Inspect its current
snapshot, capture its pixels, and perform APPROVED_ACTION. Capture the changed
page again without filename and with scale css. Compare its visible heading
and panels with the original uploaded screenshot in this conversation. Do not
infer visual changes from a successful click or snapshot text. Preserve the
original image and report what is outside the captured viewport.
```

Replace the tab address and action with the actual authorized target. The
first message's own instruction keeps the agent from opening the browser for
its first answer; the helper below checks that it made no tool call. Browser/tab access still has the authorization limits described above.
Color names are visual estimates; this is not a pixel-diff regression service.

### Check the comparison

The fixture helper below can exercise both steps through the session message
API. Add `HUMAGEN_BROWSER_COMPARE_UPLOAD=1` to its environment and use a fresh
capture directory. It uploads the original PNG through `POST /api/sessions`,
checks the random visible record and colors with zero tool calls, and posts a
follow-up to that same session. The approved fixture action really changes
yellow/purple panels to green/orange. The forwarding observer must see PNG
image blocks both before and after that action at the requested dimensions.
Each case retains the upload hash, first answer, follow-up answer and events
alongside the before/after page captures. It also retains the existing tab and
download-byte checks.

A real model and connector passed all four size/theme cases on a generated
local fixture before #383, through the removed Claude Code profile. The helper
now runs the native profile and has not been rerun since. This does not cover
a real user's screenshot, every pixel of a long page, a third-party portal or
this two-step workflow through the console.

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


## Downloads

For an existing CDP browser, downloads follow Chrome's own download settings,
not the MCP output directory. The native agent has no tool that reads the
host's download folder: open a downloaded file yourself, or attach it to a
message for the agent. Do not silently change someone's personal browser
preferences. Only authorize the selected download.

## Test the signed-in fixture end to end

The opt-in helper runs a real Humagen server on `examples/browser/humagen.yaml`,
the pinned MCP worker, an independent headless Chrome process, and the model
that profile names. Install the worker in a separate prefix, build Humagen and
set the profile's provider key (`ANTHROPIC_API_KEY`), then run from the
repository root:

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
Linux `chromium` command. The helper requires Node.js and installed Chrome. It
creates only owned local fixture processes and state, and stops those
processes when finished; it does not touch a personal profile. It writes its
own copy of the profile with a unique fixture workspace, its own ports and a
forwarding observer in front of the worker, and verifies the workspace's
identity before submitting any work. The worker starts before Humagen, since
the server accepts the profile's pinned tools when it starts.

The helper logs into a local fixture once, keeps the same authenticated Chrome
target across all four runs (390x844 and 1280x800, light and dark), and checks
the requested viewport and theme. Each run must read two unlabelled panel
colours from returned screenshot pixels, perform the authorized button action
and download the selected CSV; the run's events must show
`browser__browser_tabs` and `browser__browser_take_screenshot`. The helper
itself checks the changed heading, exactly one download and its exact bytes:
the native agent has no tool that reads the download folder, so it no longer
reads the CSV. The forwarding observer checks the actual MCP response: a
nonempty PNG image block at the requested pixel dimensions, with its byte
count and SHA256. MCP images and snapshots go to a separate artifacts
directory, and each verified CSV is moved out of the download directory before
the next case. Paired PNGs, run and tool events and wire image receipts are
written to the capture directory. Open every before/after pair before approval.
Disconnecting the fixture's CDP client resets its display emulation, so the
helper keeps that connection attached while the MCP worker operates.

Before #383 the helper ran the removed Claude Code profile and passed all
four cases then. Moving it to the native profile dropped three assertions:

- **The agent read the downloaded file.** That program's file tools read the
  CSV in the download folder, and the helper required the record in that
  read's result. A native agent has no tool that reads a host folder, so the
  helper now checks the file's bytes itself.
- **A read outside the download folder was refused.** That checked the
  program's restricted mode, which the native profile does not have.
- **The tab survived a restart of the MCP worker.** The worker was started
  fresh for each case; it now starts once, before Humagen, because the server
  accepts the pinned tools at start.

**Gap:** the helper's byte check proves that the agent's click downloaded
exactly the selected record. It does not show the agent reading or checking
what it downloaded. A native agent sees a downloaded file only when a person
attaches it to a message. The native version has not been run yet. Neither
version establishes a third-party portal login, SSO/MFA, extension handoff,
cross-host download mount, or an authorization boundary around a tab.
