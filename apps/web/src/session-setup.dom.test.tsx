import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { useState } from "react";
import type { ConversationSandboxes } from "../../../packages/client/src/generated/types.ts";
import { coworking } from "./api.ts";
import { stubFetch } from "./dom.ts";
import { useHumanActivity } from "./humanActivity.tsx";
import { type SessionOptions, SessionSettings, type SessionSetup } from "./SessionPickers.tsx";
import { EnvironmentSection } from "./SessionSections.tsx";
import { type SessionView, SessionViews, useUnavailableViews } from "./SessionViews.tsx";
import { DEFAULT_SETUP, useSessionOptions } from "./sessionOptions.ts";
import { summarize } from "./sessionSummary.ts";
import { type TerminalApi, WorkbenchTerminal } from "./WorkbenchTerminal.tsx";

const words = defaultCopy.sessionSetup;
const picker = defaultCopy.chat.picker;
const setup = defaultCopy.chat.setup;
const panel = defaultCopy.sessionPanel;

afterEach(() => cleanup());

const digest = `ghcr.io/acme/agent@sha256:${"0f".repeat(32)}`;
const options: SessionOptions = {
  images: [
    {
      id: "default",
      name: "Default",
      ref: "python:3.13-alpine",
      status: "ready",
      warm: true,
      default: true,
    },
    {
      id: "img_a",
      name: "Agent tools",
      ref: "ghcr.io/acme/agent:1.4",
      digest,
      status: "ready",
      warm: true,
      default: false,
    },
    {
      id: "img_b",
      name: "Nightly",
      ref: "ghcr.io/acme/nightly:1",
      status: "failed",
      problem: "manifest unknown",
      warm: false,
      default: false,
    },
    {
      id: "img_c",
      name: "Next",
      ref: "ghcr.io/acme/next:2",
      status: "checking",
      warm: false,
      default: false,
    },
  ],
  imageRegistries: ["ghcr.io"],
  sandbox: "docker/runsc",
  isolated: true,
  terminal: true,
  repos: ["acme/billing", "acme/web"],
};

function Settings({
  given = options,
  seen,
  fixed,
  mentioned = [],
  onMention = () => {},
  agents = [
    { name: "lead", description: "Plans the work" },
    { name: "code-reviewer", description: "Reviews code" },
    { name: "qa-reviewer", description: "Reviews behavior" },
  ],
  maxRepos,
}: {
  given?: SessionOptions | null;
  seen: SessionSetup[];
  fixed?: string;
  mentioned?: string[];
  onMention?: (name: string) => void;
  agents?: { name: string; description?: string }[] | null;
  maxRepos?: number;
}) {
  const [value, setValue] = useState<SessionSetup>(DEFAULT_SETUP);
  return (
    <form aria-label="Message" onSubmit={() => assert.fail("the menu sent the message")}>
      <SessionSettings
        project={{
          projects: [],
          value: "personal",
          personalProject: "personal",
          onChange: () => {},
          onCreated: () => {},
        }}
        agents={{ available: agents, mentioned, onMention }}
        options={given}
        setup={value}
        onSetup={(next) => {
          seen.push(next);
          setValue(next);
        }}
        fixed={fixed}
        connectHref="/settings/connections"
        maxRepos={maxRepos}
      />
    </form>
  );
}

const open = async () => {
  fireEvent.click(screen.getByRole("button", { name: setup.label }));
  return await screen.findByRole("dialog", { name: setup.label });
};
const row = (menu: HTMLElement, id: string) =>
  menu.querySelector<HTMLElement>(`[data-pane-row="${id}"]`) as HTMLElement;

test("the settings menu has a row for every setting, each with its value", async () => {
  render(<Settings seen={[]} given={{ ...options, images: options.images.slice(0, 1) }} />);
  const menu = await open();
  const rows = [...menu.querySelectorAll<HTMLElement>("[data-pane-row]")].map((r) =>
    r.textContent?.trim(),
  );
  assert.deepEqual(rows, [
    `${defaultCopy.chat.project}${defaultCopy.projects.personalTitle}`,
    `${defaultCopy.chat.agents}${setup.anyAgent}`,
    `${picker.repos}${setup.noRepos}`,
    // The default alone is still named.
    `${words.image}${words.defaultImage}`,
  ]);
});

test("every row is there before the options are read, and a row that cannot change says why", async () => {
  render(<Settings seen={[]} given={undefined} />);
  let menu = await open();
  assert.equal(menu.querySelectorAll("[data-pane-row]").length, 4, "nothing pops in later");
  cleanup();
  render(<Settings seen={[]} given={{ ...options, isolated: false }} />);
  menu = await open();
  assert.match(menu.textContent ?? "", new RegExp(setup.notSandboxed));
  assert.equal(row(menu, "image").getAttribute("aria-haspopup"), null);
  cleanup();
  render(<Settings seen={[]} given={null} />);
  menu = await open();
  assert.equal(menu.querySelectorAll("[data-pane-row]").length, 4);
  assert.match(menu.textContent ?? "", new RegExp(setup.unreadable));
  cleanup();
  render(<Settings seen={[]} fixed={setup.channelFixed} />);
  menu = await open();
  assert.match(menu.textContent ?? "", new RegExp(setup.channelFixed));
});

test("a new session runs the Default image until a ready one of the workspace's is picked", async () => {
  const seen: SessionSetup[] = [];
  render(<Settings seen={seen} />);
  const menu = await open();
  fireEvent.click(row(menu, "image"));
  const group = await within(menu).findByRole("radiogroup", { name: words.image });
  const rows = within(group).getAllByRole("radio");
  assert.deepEqual(
    rows.map((r) => r.getAttribute("data-setup-choice")),
    ["default", "img_a", "img_b", "img_c"],
  );
  assert.equal(rows[0]?.getAttribute("aria-checked"), "true");
  assert.match(rows[0]?.textContent ?? "", /python:3\.13-alpine/);
  assert.match(rows[1]?.textContent ?? "", new RegExp(words.warm));
  // A failed image says why and a checking one says so; neither may be chosen.
  assert.equal(rows[2]?.getAttribute("aria-disabled"), "true");
  assert.match(rows[2]?.textContent ?? "", /manifest unknown/);
  assert.equal(rows[3]?.getAttribute("aria-disabled"), "true");
  fireEvent.click(rows[2] as HTMLElement);
  assert.equal(seen.length, 0);
  fireEvent.click(rows[1] as HTMLElement);
  assert.deepEqual(seen.at(-1), { image: "img_a", repos: [] });
  // A single pick closes the menu; the row says the new image.
  assert.equal(screen.queryByRole("dialog", { name: setup.label }), null);
  assert.match(row(await open(), "image").textContent ?? "", /Agent tools/);
});

test("the image picker ends with a way to manage images where the workspace may add them", async () => {
  render(<Settings seen={[]} />);
  let menu = await open();
  fireEvent.click(row(menu, "image"));
  assert.equal(
    within(menu).getByRole("link", { name: words.manage }).getAttribute("href"),
    "/settings/images",
  );
  cleanup();
  render(<Settings seen={[]} given={{ ...options, imageRegistries: [] }} />);
  menu = await open();
  fireEvent.click(row(menu, "image"));
  assert.equal(within(menu).queryByRole("link", { name: words.manage }), null);
});

test("an image still checking when the options were read can be chosen once it is ready, with no reload", async () => {
  // The server's answers: checking twice, then ready.
  let reads = 0;
  const read = async (): Promise<SessionOptions> => {
    reads++;
    const settled = reads > 2;
    return {
      ...options,
      images: [
        options.images[0] as SessionOptions["images"][number],
        {
          id: "img_new",
          name: "Just added",
          ref: "ghcr.io/acme/new:1",
          status: settled ? "ready" : "checking",
          warm: false,
          default: false,
        },
      ],
    };
  };
  function Statuses() {
    const o = useSessionOptions(true, read, 10);
    return <p data-testid="statuses">{o?.images.map((i) => `${i.id}:${i.status}`).join(" ")}</p>;
  }
  render(<Statuses />);
  await waitFor(() =>
    assert.equal(screen.getByTestId("statuses").textContent, "default:ready img_new:checking"),
  );
  await waitFor(() =>
    assert.equal(screen.getByTestId("statuses").textContent, "default:ready img_new:ready"),
  );
  // Once nothing is checking it is not read again.
  const settledAt = reads;
  await new Promise((r) => setTimeout(r, 60));
  assert.equal(reads, settledAt);
});

test("repositories are any number up to the most, and the picker stays open", async () => {
  const seen: SessionSetup[] = [];
  render(
    <Settings
      seen={seen}
      given={{ ...options, repos: ["acme/billing", "acme/web", "acme/infra"] }}
      maxRepos={2}
    />,
  );
  const menu = await open();
  fireEvent.click(row(menu, "repos"));
  const group = await within(menu).findByRole("group", { name: picker.repos });
  fireEvent.click(within(group).getByRole("checkbox", { name: /acme\/billing/ }));
  fireEvent.click(within(group).getByRole("checkbox", { name: /acme\/web/ }));
  assert.deepEqual(seen.at(-1)?.repos, ["acme/billing", "acme/web"]);
  // At the most, another is not taken; a second press takes one off.
  const infra = within(group).getByRole("checkbox", { name: /acme\/infra/ });
  assert.equal(infra.getAttribute("aria-disabled"), "true");
  fireEvent.click(infra);
  assert.deepEqual(seen.at(-1)?.repos, ["acme/billing", "acme/web"]);
  fireEvent.click(within(group).getByRole("checkbox", { name: /acme\/web/ }));
  assert.deepEqual(seen.at(-1)?.repos, ["acme/billing"]);
  assert.ok(screen.getByRole("dialog", { name: setup.label }), "a multi-select picker stays open");
  // Escape steps back to the rows, which say the count; Escape again closes.
  fireEvent.keyDown(document.activeElement as Element, { key: "Escape" });
  assert.match(row(menu, "repos").textContent ?? "", new RegExp(setup.repos(1)));
  fireEvent.keyDown(document.activeElement as Element, { key: "Escape" });
  assert.equal(screen.queryByRole("dialog", { name: setup.label }), null);
});

test("with no repository connected the picker says so and leads to Connections", async () => {
  render(<Settings seen={[]} given={{ ...options, repos: [] }} />);
  const menu = await open();
  fireEvent.click(row(menu, "repos"));
  assert.ok(await within(menu).findByText(picker.noRepos));
  assert.equal(
    within(menu).getByRole("link", { name: picker.connectGitHub }).getAttribute("href"),
    "/settings/connections",
  );
});

test("agents: search has focus, marks matches, Enter picks the first, and the words keep the mentions", async () => {
  const picked: string[] = [];
  render(<Settings seen={[]} onMention={(name) => picked.push(name)} mentioned={["lead"]} />);
  const menu = await open();
  assert.match(row(menu, "agents").textContent ?? "", /lead/);
  fireEvent.click(row(menu, "agents"));
  const search = within(menu).getByRole("searchbox", { name: defaultCopy.chat.findAgent });
  await waitFor(() => assert.equal(document.activeElement, search));
  assert.equal(
    within(menu).getByRole("checkbox", { name: /^lead/ }).getAttribute("aria-checked"),
    "true",
  );
  fireEvent.change(search, { target: { value: "rev" } });
  assert.deepEqual(
    [...menu.querySelectorAll("mark")].map((m) => m.textContent),
    ["rev", "rev"],
  );
  // Enter in the field picks the first match and never sends the message.
  const allowed = fireEvent.keyDown(search, { key: "Enter" });
  assert.equal(allowed, false);
  assert.deepEqual(picked, ["code-reviewer"]);
  fireEvent.click(within(menu).getByRole("checkbox", { name: /^qa-/ }));
  assert.deepEqual(picked, ["code-reviewer", "qa-reviewer"]);
  assert.ok(screen.getByRole("dialog", { name: setup.label }), "the agents picker stays open");
});

test("a private session is started with its image and repositories, and the default is not sent", async () => {
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
      image: "img_a",
      repos: ["acme/billing"],
    })
    .catch(() => undefined);
  await coworking
    .start({
      project: "",
      channel: "general",
      text: "Hi",
      visibility: "private",
      image: "default",
      repos: [],
    })
    .catch(() => undefined);
  assert.equal((bodies[0] as { image?: string }).image, "img_a");
  assert.deepEqual((bodies[0] as { repos?: string[] }).repos, ["acme/billing"]);
  assert.equal("image" in (bodies[1] as object), false);
  assert.equal("repos" in (bodies[1] as object), false);
  net.restore();
});

test("the panel's Environment names the session's image, pinned, and where it runs", async () => {
  const net = stubFetch({
    "GET /api/sessions/options": () => options,
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
        chosen={{ image: "img_a", repos: ["acme/billing"] }}
      />
    </ul>,
  );
  await waitFor(() =>
    assert.match(
      screen.getByTestId("panel-image").textContent ?? "",
      /Agent tools.*ghcr\.io\/acme\/agent@0f0f0f0f0f0f · docker\/runsc/,
    ),
  );
  assert.equal(screen.getByTestId("panel-image").getAttribute("href"), "/settings/images");
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
  // Pressed, it says why where a touch screen can read it, and opens nothing.
  assert.equal(screen.getByTestId("view-unavailable").textContent, panel.unavailable.terminalHost);
  fireEvent.click(browser);
  assert.equal(screen.getByTestId("view-unavailable").textContent, panel.unavailable.browser);
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
