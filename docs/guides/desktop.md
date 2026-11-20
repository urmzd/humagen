# Connect a Mac desktop

Humagen can use [Peekaboo's native MCP server](https://peekaboo.sh/MCP.html)
for exact-window screenshots, accessibility inspection, and native controls.
`examples/desktop/humagen.yaml` pins the real `@steipete/peekaboo@4.9.9` server
and twelve reviewed tool schemas. It runs on the Mac as a stdio process, with
explicit foreground authority and no remote-host discovery. Its prompt limits
actions to the named application and window; the granted native tools can
perform foreground input, so use this only on the trusted Mac worker.

## Configure the worker

The worker needs macOS, Node.js 22 or newer, an unlocked local GUI session,
Screen Recording, and Accessibility permission. Synthetic input can require
additional Accessibility authorization. Check the native server's current status:

```sh
npx --yes @steipete/peekaboo@4.9.9 permissions status --no-remote --json
```

The person grants missing permissions in System Settings > Privacy & Security,
to the actual worker/terminal process running Peekaboo, then restarts that worker.
Permissions alone do not unlock a GUI session. A locked Mac returns an explicit
capture error; neither Humagen nor this configuration unlocks it.

The examples explicitly pass `--allow-foreground` to the worker. Peekaboo's
default background-only mode refuses actions such as `AXPress`; foreground
authority is necessary for the fixture's button interaction. This flag authorizes
the worker's foreground actions and does not unlock the Mac or grant TCC access.

Use the `tools` entry in an existing authenticated workspace and grant
`mcp:desktop:write` only to the chosen agent. Its model must support image input
and have a working provider credential. The standalone example reads
`ANTHROPIC_API_KEY` and starts with:

```sh
humagen start -f examples/desktop/humagen.yaml
```

The standalone example uses development sign-in and memory state for trusted
local fixtures. Every client reaching a dev API is an administrator. For real
work, use the authenticated workspace instead. A screen or document can contain
hostile instructions, and the agent's prompt is not an authorization boundary.
The explicit write grant authorizes the tool group; configure Humagen's approval
policy when individual actions must wait for a person.

The `unisolated` sandbox in the local example is deliberate: a Linux container
cannot operate the Mac's desktop. Run this on the person's trusted Mac worker,
not a shared server. `--no-remote` keeps the native target on that worker. No
background failure is retried using global input or another host.

## Observe, act, verify

Call `permissions`, then `see` with the exact application/PID and window ID.
Use its snapshot reference and actionable element IDs. After a change, call
`see` again; an earlier snapshot is not authority to act on a later state.
The selected schemas describe text replacement with `type` and `clear: true`,
and button activation with `action` and `AXPress`. The adapter is intended to
carry returned PNG bytes as it does browser screenshots. These native actions,
the snapshot reference parser and PNG delivery remain unverified while capture
is blocked. The live fixture check must pass before claiming this workflow.

The selected inventory omits `set_value`, whose union-valued schema is not
representable by the current saige parameter schema. It also omits the server's
nested agent, analysis, browser, and clipboard tools. Unsupported schemas are
not weakened or silently accepted. Text entry uses the supported `type` tool.

## Dotfiles, processes and printers

Which host an agent's `shell` reaches depends on the sandbox kind. The
`docker` kind runs it in a container of its own, which cannot see this host's
dotfiles, processes or printers. The `local` kind runs it as host processes
with the server's privileges, unconfined, for development only: its
environment is empty (no `HOME` or other variables), and the shell's own
default search path (`/usr/local/bin`, `/bin`, `/usr/bin` and the like) finds
only commands installed there, so a tool under `/opt/homebrew/bin`, for
example, is not found. `unisolated` refuses the shell. Before #383 this
section pointed to the Claude Code and Codex programs and their allow rules
for host commands; that path is gone, and no shipped profile replaces it with
a confined one.

## Real native acceptance

With the pinned binary installed on the test worker:

```sh
HUMAGEN_DESKTOP_MCP_COMMAND=/absolute/path/to/peekaboo \
  go test -race ./internal/mcptools -run '^TestDesktopLive$' -v
```

This compiles and opens a temporary AppKit fixture, checks the pinned MCP
tool set, targets only that fixture's exact PID/window, enters text, presses
its button, checks the application's independent action receipt, then checks
the changed heading and returned PNG pixels. The fixture is terminated and
removed afterward. `HUMAGEN_DESKTOP_CAPTURE_DIR` optionally retains its final
PNG for review. A locked session or missing permission fails this test rather
than skipping the native actions or claiming coverage.
