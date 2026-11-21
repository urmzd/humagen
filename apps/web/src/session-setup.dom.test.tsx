import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { useState } from "react";
import type { ConversationSandboxes } from "../../../packages/client/src/generated/types.ts";
import { coworking } from "./api.ts";
import { stubFetch } from "./dom.ts";
import { useHumanActivity } from "./humanActivity.tsx";
import {
  DEFAULT_SETUP,
  type SessionOptions,
  type SessionSetup,
  SessionSetupPickers,
} from "./SessionPickers.tsx";
import { EnvironmentSection } from "./SessionSections.tsx";
import { type SessionView, SessionViews, useUnavailableViews } from "./SessionViews.tsx";
import { summarize } from "./sessionSummary.ts";
import { type TerminalApi, WorkbenchTerminal } from "./WorkbenchTerminal.tsx";

const words = defaultCopy.sessionSetup;
const panel = defaultCopy.sessionPanel;

afterEach(() => cleanup());

const options: SessionOptions = {
  environments: [
    { name: "auto", label: "Auto", isolated: true, terminal: true },
    { name: "docker", label: "Docker sandbox", isolated: true, terminal: true },
  ],
  repos: ["acme/billing", "acme/web"],
};

function Pickers({
  given = options,
  seen,
}: {
  given?: SessionOptions | null;
  seen: SessionSetup[];
}) {
  const [value, setValue] = useState<SessionSetup>(DEFAULT_SETUP);
  return (
    <SessionSetupPickers
      options={given}
      value={value}
      onChange={(next) => {
        seen.push(next);
        setValue(next);
      }}
    />
  );
}

test("a new session's environment is Auto until another is picked from the deployment's", async () => {
  const seen: SessionSetup[] = [];
  render(<Pickers seen={seen} />);
  const trigger = screen.getByRole("button", { name: `${words.environment}: ${words.auto}` });
  fireEvent.click(trigger);
  const menu = await screen.findByRole("menu", { name: words.environment });
  const rows = within(menu).getAllByRole("menuitemradio");
  assert.deepEqual(
    rows.map((r) => r.getAttribute("data-environment")),
    ["auto", "docker"],
  );
  assert.equal(rows[0]?.getAttribute("aria-checked"), "true");
  assert.match(rows[1]?.textContent ?? "", /Isolated sandbox · Terminal/);
  fireEvent.click(rows[1] as HTMLElement);
  assert.deepEqual(seen.at(-1), { environment: "docker", repos: [] });
  assert.ok(screen.getByRole("button", { name: `${words.environment}: Docker sandbox` }));
});

test("repositories are picked from those connected, any number, and said on the control", async () => {
  const seen: SessionSetup[] = [];
  render(<Pickers seen={seen} />);
  fireEvent.click(screen.getByRole("button", { name: `${words.repoLabel}: ${words.none}` }));
  const menu = await screen.findByRole("menu", { name: words.repoLabel });
  fireEvent.click(within(menu).getByRole("menuitemcheckbox", { name: /acme\/billing/ }));
  fireEvent.click(within(menu).getByRole("menuitemcheckbox", { name: /acme\/web/ }));
  assert.deepEqual(seen.at(-1)?.repos, ["acme/billing", "acme/web"]);
  // The menu stays open for more; a second press takes one off.
  fireEvent.click(within(menu).getByRole("menuitemcheckbox", { name: /acme\/web/ }));
  assert.deepEqual(seen.at(-1)?.repos, ["acme/billing"]);
  assert.equal(
    within(menu)
      .getByRole("menuitemcheckbox", { name: /acme\/billing/ })
      .getAttribute("aria-checked"),
    "true",
  );
});

test("with no repository connected the menu says so and leads to Connections; unread options draw nothing", async () => {
  render(<Pickers seen={[]} given={{ ...options, repos: [] }} />);
  fireEvent.click(screen.getByRole("button", { name: `${words.repoLabel}: ${words.none}` }));
  const menu = await screen.findByRole("menu", { name: words.repoLabel });
  assert.match(menu.textContent ?? "", new RegExp(words.noRepos));
  assert.ok(within(menu).getByRole("link", { name: words.connectGitHub }));
  cleanup();
  render(<Pickers seen={[]} given={null} />);
  assert.equal(screen.queryByTestId("session-setup"), null);
});

test("a private session is started with its environment and repositories, and Auto is not sent", async () => {
  const bodies: unknown[] = [];
  const net = stubFetch({
    "POST /api/sessions": (body) => {
      bodies.push(body);
      return { id: "s_1", thread: "s_1", messages: [], status: "running" };
    },
  });
  await coworking
    .start({
      project: "",
      channel: "general",
      text: "Fix the rounding",
      visibility: "private",
      environment: "docker",
      repos: ["acme/billing"],
    })
    .catch(() => undefined);
  await coworking
    .start({
      project: "",
      channel: "general",
      text: "Hi",
      visibility: "private",
      environment: "auto",
      repos: [],
    })
    .catch(() => undefined);
  assert.equal((bodies[0] as { environment?: string }).environment, "docker");
  assert.deepEqual((bodies[0] as { repos?: string[] }).repos, ["acme/billing"]);
  assert.equal("environment" in (bodies[1] as object), false);
  assert.equal("repos" in (bodies[1] as object), false);
  net.restore();
});

test("the panel's Environment shows the repositories and environment chosen for the session", async () => {
  const net = stubFetch({
    "GET /api/threads/s_set/sandboxes": () => ({
      session: {
        isolated: true,
        files: 1,
        bytes: 1,
        retention_days: 7,
        writable: true,
        commands: true,
      },
      runs: [
        {
          run: "r",
          agent: "a",
          depth: 0,
          status: "completed",
          created_at: "",
          isolated: true,
          mounts: [],
          files: [],
        },
      ],
      more: false,
    }),
  });
  render(
    <ul>
      <EnvironmentSection
        thread="s_set"
        summary={summarize([], {})}
        pulls={[]}
        facts={{}}
        open={{ tab: () => {} }}
        chosen={{ environment: "docker", repos: ["acme/billing"] }}
      />
    </ul>,
  );
  await waitFor(() =>
    assert.match(
      screen.getByTestId("panel-sandbox").textContent ?? "",
      /Chosen environment: docker/,
    ),
  );
  assert.equal(
    screen.getByTestId("panel-repo").getAttribute("href"),
    "https://github.com/acme/billing",
  );
  net.restore();
});

function Header({
  isSession = true,
  api,
}: {
  isSession?: boolean;
  api: Parameters<typeof useUnavailableViews>[2];
}) {
  const off = useUnavailableViews("s_hdr", isSession, api);
  const [opened, setOpened] = useState<SessionView[]>([]);
  return (
    <>
      <SessionViews disabled={off} onOpen={(v) => setOpened((o) => [...o, v])} onClose={() => {}} />
      <p data-testid="opened">{opened.join(",")}</p>
    </>
  );
}

test("Terminal and Browser are always in the header, and say why when they cannot open", async () => {
  render(
    <Header
      api={{
        sandboxes: async () => ({ session: { isolated: false, commands: false } }),
        browser: async () => ({ available: false, reason: "no provider" }),
      }}
    />,
  );
  const terminal = screen.getByRole("button", { name: panel.view.terminal });
  const browser = screen.getByRole("button", { name: panel.view.browser });
  await waitFor(() => assert.equal(terminal.getAttribute("aria-disabled"), "true"));
  assert.equal(terminal.getAttribute("title"), panel.unavailable.terminalHost);
  await waitFor(() => assert.equal(browser.getAttribute("title"), panel.unavailable.browser));
  fireEvent.click(terminal);
  fireEvent.click(browser);
  assert.equal(screen.getByTestId("opened").textContent, "");
});

test("a sandbox that cannot be reached leaves the Terminal unable to open; a live one opens it", async () => {
  render(
    <Header
      api={{
        sandboxes: async () => {
          throw new Error("503");
        },
        browser: async () => ({ available: true, providers: ["playwright"] }),
      }}
    />,
  );
  const terminal = screen.getByRole("button", { name: panel.view.terminal });
  await waitFor(() =>
    assert.equal(terminal.getAttribute("title"), panel.unavailable.terminalUnreachable),
  );
  const browser = screen.getByRole("button", { name: panel.view.browser });
  fireEvent.click(browser);
  assert.equal(screen.getByTestId("opened").textContent, "browser");
  cleanup();
  render(
    <Header
      api={{
        sandboxes: async () => ({ session: { isolated: true, commands: true } }),
        browser: async () => ({ available: true }),
      }}
    />,
  );
  const live = screen.getByRole("button", { name: panel.view.terminal });
  await waitFor(() => assert.equal(live.getAttribute("aria-disabled"), null));
  fireEvent.click(live);
  assert.equal(screen.getByTestId("opened").textContent, "terminal");
});

function Term({ api, thread }: { api: TerminalApi; thread: string }) {
  const { record, records } = useHumanActivity(thread);
  return (
    <>
      <WorkbenchTerminal
        thread={thread}
        runs={[]}
        responses={[]}
        summary={undefined}
        canPost
        onSelection={() => {}}
        openTab={() => {}}
        record={record}
        expanded={false}
        api={api}
      />
      <p data-testid="records">{records.length}</p>
    </>
  );
}

const fresh = (): ConversationSandboxes => ({
  session: {
    isolated: true,
    files: 0,
    bytes: 0,
    retention_days: 7,
    writable: true,
    commands: true,
  },
  runs: [],
  more: false,
});

test("a terminal opened on a new session starts its sandbox first, says so, and records nothing", async () => {
  const starts: string[] = [];
  let finish: () => void = () => {};
  const api: TerminalApi = {
    sandboxes: async () => fresh(),
    run: async () => {
      throw new Error("not this");
    },
    start: (thread) => {
      starts.push(thread);
      return new Promise((resolve) => {
        finish = () =>
          resolve({
            command: "true",
            exit_code: 0,
            timed_out: false,
            output: "",
            truncated: false,
            duration_ms: 90,
            files: [],
            conflicts: [],
          });
      });
    },
  };
  render(<Term api={api} thread="s_new_start" />);
  const status = await screen.findByTestId("terminal-start");
  assert.equal(status.getAttribute("data-start"), "starting");
  assert.match(status.textContent ?? "", new RegExp(panel.starting));
  finish();
  await waitFor(() =>
    assert.equal(screen.getByTestId("terminal-start").getAttribute("data-start"), "started"),
  );
  assert.ok(screen.getByRole("textbox", { name: defaultCopy.workbenchTerminal.input }));
  assert.deepEqual(starts, ["s_new_start"]);
  assert.equal(screen.getByTestId("records").textContent, "0");
});

test("a sandbox that fails to start says why", async () => {
  const api: TerminalApi = {
    sandboxes: async () => fresh(),
    run: async () => {
      throw new Error("not this");
    },
    start: async () => {
      throw new Error("docker is not running");
    },
  };
  render(<Term api={api} thread="s_new_fail" />);
  await waitFor(() =>
    assert.equal(screen.getByTestId("terminal-start").getAttribute("data-start"), "failed"),
  );
  assert.match(screen.getByTestId("terminal-start").textContent ?? "", /docker is not running/);
});
