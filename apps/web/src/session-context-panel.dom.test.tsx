import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { GitHubPull } from "../../../packages/client/src/generated/types.ts";
import type { Run } from "./adapter.ts";
import { stubFetch } from "./dom.ts";
import type { PaneTab } from "./pane.ts";
import type { ConversationView, WorkbenchTabId } from "./route.ts";
import { SessionActions } from "./SessionActions.tsx";
import { SessionPlace } from "./SessionPlace.tsx";
import {
  ConnectorsSection,
  ContextSection,
  EnvironmentSection,
  PanelChanges,
  PULLS_SHOWN,
} from "./SessionSections.tsx";
import { type SessionView, SessionViews, viewShortcut } from "./SessionViews.tsx";
import {
  contextUse,
  localChanges,
  pullFacts,
  pullsOf,
  reposOf,
  sandboxState,
} from "./sessionContext.ts";
import { type SessionSummary, summarize } from "./sessionSummary.ts";
import { SessionWorkbench } from "./Workbench.tsx";
import type { ConversationSandboxes } from "./workspace-files.ts";

/** The session's menu: its title, a button in the header's heading. */
function sessionMenu(): HTMLElement {
  const el = document.querySelector<HTMLElement>('h1 button[aria-haspopup="menu"]');
  if (!el) throw new Error("no session menu in the header");
  return el;
}
const findSessionMenu = () => waitFor(sessionMenu);

const words = defaultCopy.sessionPanel;
const thread = "th-panel";

afterEach(() => cleanup());

const run = (over: Partial<Run>): Run =>
  ({
    id: "r1",
    agent: "billing-dev",
    text: "Fix the rounding",
    status: "completed",
    lease: 0,
    granted: 0,
    created_at: "2026-11-20T00:00:00Z",
    ...over,
  }) as Run;

const usageRun = run({
  usage: {
    own: {},
    total: {
      model_calls: 6,
      prompt_tokens: 535004,
      cached_prompt_tokens: 1200,
      completion_tokens: 2520,
      cost: { micros: 1362711, currency: "USD" },
    },
    context: { model: "gpt-4o", window: 128000, tokens: 89688 },
  },
});

const pull = (over: Partial<GitHubPull> = {}): GitHubPull => ({
  subject: {
    number: 41,
    title: "fix(billing): round invoice totals to the cent",
    body: "",
    state: "open",
    html_url: "https://github.com/acme/billing/pull/41",
    head: { sha: "1".repeat(40), ref: "fix/invoice-rounding" },
  },
  files: [
    {
      filename: "billing/invoice.go",
      status: "modified",
      patch: "@@ -1,2 +1,3 @@\n-old\n+new\n+more\n context\n",
      blob_url: "",
    },
  ],
  comments: [],
  workflows: [
    {
      id: 1,
      name: "test",
      head_sha: "1".repeat(40),
      run_attempt: 1,
      status: "completed",
      conclusion: "failure",
      html_url: "",
    },
    {
      id: 2,
      name: "test",
      head_sha: "1".repeat(40),
      run_attempt: 2,
      status: "completed",
      conclusion: "failure",
      html_url: "",
    },
    {
      id: 3,
      name: "lint",
      head_sha: "1".repeat(40),
      run_attempt: 1,
      status: "completed",
      conclusion: "failure",
      html_url: "",
    },
    {
      id: 4,
      name: "build",
      head_sha: "1".repeat(40),
      run_attempt: 1,
      status: "completed",
      conclusion: "success",
      html_url: "",
    },
  ],
  ...over,
});

/** A summary with two pull requests and a saved file, as the panel is given it. */
function summaryWithPulls(n = 2): SessionSummary {
  const s = summarize([], {
    files: [{ id: "a1", title: "rounding-cases.csv", kind: "data", mime: "text/csv" }],
  });
  for (let i = 0; i < n; i++)
    s.outputs.push({
      key: `p${i}`,
      kind: "pr",
      type: "PR",
      title: `acme/billing #${41 + i}`,
      url: `https://github.com/acme/billing/pull/${41 + i}`,
    });
  s.used.connections.push({ label: "GitHub" });
  return s;
}

const boxes = (
  over: Partial<ConversationSandboxes["session"]> = {},
  runs = 1,
): ConversationSandboxes =>
  ({
    session: {
      isolated: true,
      files: 2,
      bytes: 135,
      retention_days: 7,
      writable: true,
      commands: true,
      ...over,
    },
    runs: Array.from({ length: runs }, (_, i) => ({
      run: `r${i}`,
      agent: "billing-dev",
      depth: 0,
      status: "completed",
      created_at: "2026-11-20T00:00:00Z",
      isolated: true,
      mounts: [],
      files: [],
    })),
    more: false,
  }) as ConversationSandboxes;

test("context is the latest answer's fill, with what every answer used added up", () => {
  const use = contextUse([run({ id: "r0" }), usageRun, run({ id: "child", parent: "r1" })]);
  assert.equal(use?.percent, 70);
  assert.equal(use?.used, 89688);
  assert.equal(use?.window, 128000);
  assert.equal(use?.model, "gpt-4o");
  assert.equal(use?.total.prompt_tokens, 535004);
  assert.equal(use?.total.cost?.micros, 1362711);
  assert.equal(contextUse([run({})]), undefined);
});

test("a pull request reads its title, branch, lines and checks, each check by its latest attempt", () => {
  const f = pullFacts(pull());
  assert.equal(f.title, "fix(billing): round invoice totals to the cent");
  assert.equal(f.branch, "fix/invoice-rounding");
  assert.equal(f.additions, 2);
  assert.equal(f.deletions, 1);
  assert.deepEqual(f.checks, { state: "failing", failing: 2, total: 3 });
  const green = pullFacts(
    pull({
      workflows: [
        {
          id: 9,
          name: "test",
          head_sha: "1".repeat(40),
          run_attempt: 1,
          status: "completed",
          conclusion: "success",
          html_url: "",
        },
      ],
    }),
  );
  assert.equal(green.checks.state, "passing");
  const busy = pullFacts(
    pull({
      workflows: [
        {
          id: 9,
          name: "test",
          head_sha: "1".repeat(40),
          run_attempt: 1,
          status: "in_progress",
          conclusion: "",
          html_url: "",
        },
      ],
    }),
  );
  assert.equal(busy.checks.state, "running");
  assert.equal(pullFacts(pull({ workflows: [] })).checks.state, "none");
});

test("repositories and branches come from the session's pull requests, each once", () => {
  const pulls = pullsOf(summaryWithPulls());
  assert.deepEqual(
    pulls.map((p) => [p.repo, p.number]),
    [
      ["acme/billing", 41],
      ["acme/billing", 42],
    ],
  );
  const repos = reposOf(pulls, {
    "acme/billing#41": pullFacts(pull()),
    "acme/billing#42": { ...pullFacts(pull()), branch: "docs/rounding" },
  });
  assert.deepEqual(repos, [
    { repo: "acme/billing", branches: ["fix/invoice-rounding", "docs/rounding"] },
  ]);
});

test("the sandbox says whether it is isolated, not started, on the server, unreachable or still read", () => {
  assert.equal(sandboxState(boxes(), false), "isolated");
  assert.equal(sandboxState(boxes({ files: 0 }, 0), false), "none");
  assert.equal(sandboxState(boxes({ isolated: false }, 0), false), "host");
  assert.equal(sandboxState(undefined, true), "unreachable");
  assert.equal(sandboxState(undefined, false), "loading");
});

test("local changes count the files the session wrote and their lines", () => {
  assert.deepEqual(localChanges(undefined), { files: 0, additions: 0, deletions: 0 });
});

function environment(answer: () => unknown, summary = summaryWithPulls()) {
  const opened: { tab: [WorkbenchTabId, string | undefined][]; pane: PaneTab[] } = {
    tab: [],
    pane: [],
  };
  const fetch = stubFetch({ [`GET /api/threads/${thread}/sandboxes`]: answer });
  render(
    <ul>
      <EnvironmentSection
        thread={thread}
        summary={summary}
        pulls={pullsOf(summary)}
        facts={{ "acme/billing#41": pullFacts(pull()) }}
        open={{
          tab: (id, ref) => opened.tab.push([id, ref]),
          workspace: (tab) => opened.pane.push(tab),
        }}
      />
    </ul>,
  );
  return { opened, fetch };
}

test("Environment: the sandbox opens the Terminal, its files the Workspace, and the repo and branch link out", async () => {
  const { opened, fetch } = environment(() => boxes());
  const sandbox = await screen.findByTestId("panel-sandbox");
  await waitFor(() => assert.match(sandbox.textContent ?? "", /Isolated/));
  fireEvent.click(sandbox);
  assert.deepEqual(opened.tab, [["terminal", undefined]]);
  const files = screen.getByTestId("panel-files");
  assert.match(files.textContent ?? "", /2 files/);
  fireEvent.click(files);
  assert.deepEqual(opened.pane, [{ kind: "files" }]);
  assert.equal(
    screen.getByTestId("panel-repo").getAttribute("href"),
    "https://github.com/acme/billing",
  );
  assert.equal(
    screen.getByTestId("panel-branch").getAttribute("href"),
    "https://github.com/acme/billing/tree/fix/invoice-rounding",
  );
  fetch.restore();
});

test("Environment: a sandbox not started yet says it starts with the first tool that needs it", async () => {
  const { fetch } = environment(() => boxes({ files: 0 }, 0), summarize([], {}));
  const sandbox = await screen.findByTestId("panel-sandbox");
  await waitFor(() => assert.match(sandbox.textContent ?? "", /Not started/));
  assert.match(sandbox.textContent ?? "", new RegExp(words.sandboxHint.none));
  assert.equal(screen.queryByTestId("panel-repo"), null);
  fetch.restore();
});

test("Environment: an unreachable sandbox says so and reads it again on Try Again", async () => {
  let calls = 0;
  const { fetch } = environment(() => {
    calls++;
    return calls === 1
      ? new Response(JSON.stringify({ error: "sandbox unreachable" }), { status: 503 })
      : boxes();
  });
  const sandbox = await screen.findByTestId("panel-sandbox");
  await waitFor(() => assert.match(sandbox.textContent ?? "", /Unreachable/));
  assert.equal(screen.queryByTestId("panel-files"), null);
  fireEvent.click(screen.getByRole("button", { name: words.retry }));
  await waitFor(() =>
    assert.match(screen.getByTestId("panel-sandbox").textContent ?? "", /Isolated/),
  );
  assert.equal(calls, 2);
  fetch.restore();
});

test("Context: the fill in a percent and a meter, opening to its breakdown", () => {
  render(<ContextSection runs={[usageRun]} />);
  const row = screen.getByTestId("panel-context");
  assert.match(row.textContent ?? "", /70%/);
  assert.equal(row.getAttribute("aria-expanded"), "false");
  assert.equal(screen.queryByTestId("panel-context-breakdown"), null);
  fireEvent.click(row);
  assert.equal(row.getAttribute("aria-expanded"), "true");
  const breakdown = screen.getByTestId("panel-context-breakdown");
  assert.match(breakdown.textContent ?? "", /89,688 of 128,000 tokens/);
  assert.match(breakdown.textContent ?? "", /gpt-4o/);
  assert.match(breakdown.textContent ?? "", /535,004/);
});

test("Context: a session that has reported nothing says so and opens nothing", () => {
  render(<ContextSection runs={[run({})]} />);
  const row = screen.getByTestId("panel-context") as HTMLButtonElement;
  assert.match(row.textContent ?? "", new RegExp(words.contextNone));
  assert.equal(row.disabled, true);
});

test("Connectors: how many are connected, opening to each and whether this session used it", async () => {
  const fetch = stubFetch({
    "GET /api/me/credentials": () => ({
      providers: [
        { provider: "linear", transport: "http", tools: 1, connected: true },
        { provider: "jira", transport: "http", tools: 1, connected: false },
      ],
      values: true,
      references: [],
    }),
  });
  render(<ConnectorsSection summary={summaryWithPulls()} />);
  const row = await screen.findByTestId("panel-connectors");
  // Linear is the reader's; GitHub reached this session through the workspace's app.
  await waitFor(() => assert.match(row.textContent ?? "", /2 connected/));
  fireEvent.click(row);
  const list = screen.getByTestId("panel-connector-list");
  const rows = within(list)
    .getAllByTestId("panel-connector")
    .map((r) => r.textContent);
  assert.deepEqual(rows, ["GitHubUsed here", "linearConnected", "jiraNot connected"]);
  assert.ok(within(list).getByRole("link", { name: words.manageConnectors }));
  fetch.restore();
});

test("Changes: the files written open the Changes tab; a pull request shows its lines and failing checks", () => {
  let opened = 0;
  const pulls = pullsOf(summaryWithPulls());
  render(
    <PanelChanges
      pulls={pulls}
      facts={{ "acme/billing#41": pullFacts(pull()), "acme/billing#42": null }}
      local={{ files: 2, additions: 7, deletions: 0 }}
      onLocal={() => opened++}
    />,
  );
  const local = screen.getByTestId("panel-local-changes");
  assert.match(local.textContent ?? "", /2 files changed\+7−0/);
  fireEvent.click(local);
  assert.equal(opened, 1);
  const [first, second] = screen.getAllByTestId("panel-pull");
  assert.match(first?.textContent ?? "", /round invoice totals/);
  assert.match(first?.textContent ?? "", /billing #41\+2−1/);
  assert.match(first?.textContent ?? "", /2 failing/);
  assert.equal(first?.getAttribute("href"), "https://github.com/acme/billing/pull/41");
  // A pull request GitHub could not be asked about says so, by its number.
  assert.match(second?.textContent ?? "", /billing #42/);
  assert.match(second?.textContent ?? "", new RegExp(words.ci.unknown));
});

test("Changes: past five pull requests the rest wait behind Show More", () => {
  const pulls = pullsOf(summaryWithPulls(PULLS_SHOWN + 2));
  render(
    <PanelChanges
      pulls={pulls}
      facts={{}}
      local={{ files: 0, additions: 0, deletions: 0 }}
      onLocal={() => {}}
    />,
  );
  assert.equal(screen.getAllByTestId("panel-pull").length, PULLS_SHOWN);
  fireEvent.click(screen.getByRole("button", { name: words.showMore(2) }));
  assert.equal(screen.getAllByTestId("panel-pull").length, PULLS_SHOWN + 2);
});

test("Details in the workbench draws the sections, and their rows open the workbench's views", async () => {
  const fetch = stubFetch({
    [`GET /api/threads/${thread}/sandboxes`]: () => boxes(),
    "GET /api/me/credentials": () => ({
      providers: [{ provider: "linear", transport: "http", tools: 1, connected: true }],
      values: true,
      references: [],
    }),
    "GET /api/github/pull": () => pull(),
  });
  const views: ConversationView[] = [];
  const panes: PaneTab[] = [];
  render(
    <SessionWorkbench
      runs={[usageRun]}
      thread={thread}
      view={{ panel: "details" }}
      onView={(v) => views.push(v)}
      onClose={() => {}}
      onOpenWorkspace={(tab) => panes.push(tab)}
      loader={{
        trace: async () =>
          ({
            message: "s_1:r1",
            nodes: [
              {
                run: "r1",
                agent: "billing-dev",
                status: "completed",
                started_at: "2026-11-20T00:00:00Z",
                ended_at: "2026-11-20T00:00:05Z",
                answer: "Done.",
                ms: 5000,
                tools: [],
                context: [],
              },
            ],
            hidden: 0,
            truncated: false,
          }) as never,
      }}
      files={async () => ({
        files: [],
        sandbox: "2 files",
      })}
    />,
  );
  const sandbox = await screen.findByTestId("panel-sandbox");
  fireEvent.click(sandbox);
  assert.deepEqual(views.at(-1), { panel: "details", bench: "terminal" });
  await waitFor(() => assert.ok(screen.getByTestId("panel-files")));
  fireEvent.click(screen.getByTestId("panel-files"));
  assert.deepEqual(panes, [{ kind: "files" }]);
  assert.match(screen.getByTestId("panel-context").textContent ?? "", /70%/);
  await waitFor(() =>
    assert.deepEqual(
      [...document.querySelectorAll("[data-section]")].map((s) => s.getAttribute("data-section")),
      ["environment", "context", "connectors"],
    ),
  );
  fetch.restore();
});

function views(active?: SessionView, hidden: SessionView[] = []) {
  const opened: SessionView[] = [];
  let closed = 0;
  render(
    <SessionViews
      active={active}
      progress={{ open: 0, working: false }}
      onOpen={(v) => opened.push(v)}
      onClose={() => closed++}
      hidden={hidden}
    />,
  );
  return { opened, closed: () => closed };
}

test("the header opens Details, Terminal, Changes, Browser and Files, each pressed while open", () => {
  const { opened, closed } = views("terminal");
  const group = screen.getByRole("group", { name: words.views });
  for (const v of ["terminal", "changes", "browser", "files"] as const)
    fireEvent.click(within(group).getByRole("button", { name: words.view[v] }));
  fireEvent.click(within(group).getByRole("button", { name: defaultCopy.details.open(0) }));
  // Terminal was open, so pressing it closed the pane.
  assert.deepEqual(opened, ["changes", "browser", "files", "details"]);
  assert.equal(closed(), 1);
  assert.equal(
    within(group).getByRole("button", { name: words.view.terminal }).getAttribute("aria-pressed"),
    "true",
  );
});

test("More Views holds Artifacts, Search, Background Tasks and Progress, with their keys", async () => {
  const { opened } = views();
  fireEvent.click(screen.getByRole("button", { name: words.more }));
  const menu = await screen.findByRole("menu", { name: words.more });
  const items = within(menu)
    .getAllByRole("menuitem")
    .map((i) => i.getAttribute("data-session-view"));
  // The primary views are in the menu too, shown there only on a phone.
  assert.deepEqual(items, [
    "terminal",
    "changes",
    "browser",
    "files",
    "artifacts",
    "search",
    "background",
    "progress",
  ]);
  const search = within(menu).getByRole("menuitem", { name: new RegExp(words.view.search) });
  assert.match(search.textContent ?? "", /F/);
  // The Side Chat is gone: a second chat is a new session that refers to this one.
  assert.equal(within(menu).queryByRole("menuitem", { name: /Side Chat/ }), null);
  // Background is the session's own: the row opens a view of this session, and leads nowhere else.
  assert.equal(
    within(menu).getByRole("menuitem", { name: words.view.background }).getAttribute("href"),
    null,
  );
  fireEvent.click(search);
  assert.deepEqual(opened, ["search"]);
});

test("a hidden view is left out of More Views, and the one key opens Search", async () => {
  views(undefined, ["search"]);
  fireEvent.click(screen.getByRole("button", { name: words.more }));
  const menu = await screen.findByRole("menu", { name: words.more });
  assert.equal(within(menu).queryByRole("menuitem", { name: new RegExp(words.view.search) }), null);
  const key = { metaKey: true, ctrlKey: false, shiftKey: false, altKey: false };
  // Command ; opened the Side Chat. It opens nothing now.
  assert.equal(viewShortcut({ ...key, key: ";" }), undefined);
  assert.equal(viewShortcut({ ...key, key: "F", shiftKey: true }), "search");
  assert.equal(viewShortcut({ ...key, key: ";", metaKey: false }), undefined);
  assert.equal(viewShortcut({ ...key, key: "k" }), undefined);
});

test("the title is the session's menu, and Rename edits it in place", async () => {
  const writes: unknown[] = [];
  const fetch = stubFetch({
    "GET /api/threads/th-1": () => ({
      thread: {
        id: "th-1",
        version: 3,
        title: "Before",
        visibility: "private",
        default: "reviewer",
        agents: ["reviewer"],
        mode: "join",
      },
      turns: [],
    }),
    "PUT /api/threads/th-1": (body) => {
      writes.push(body);
      return { id: "th-1", title: (body as { title: string }).title };
    },
    "GET /api/marks": () => ({ archived: [] }),
  });
  let updated = "";
  render(
    <h1>
      <SessionActions
        shownTitle="Before"
        thread={{ id: "th-1", title: "Before", visibility: "private" } as never}
        canManage
        onChanged={() => {}}
        onUpdated={(t) => {
          updated = t.title ?? "";
        }}
      />
    </h1>,
  );
  const trigger = sessionMenu();
  assert.match(trigger.textContent ?? "", /^Before/);
  fireEvent.click(trigger);
  fireEvent.click(await screen.findByRole("menuitem", { name: "Rename" }));
  const field = await screen.findByRole("textbox", { name: "Session name" });
  fireEvent.change(field, { target: { value: "After" } });
  fireEvent.submit(field.closest("form") as HTMLFormElement);
  await waitFor(() => assert.equal(updated, "After"));
  assert.equal((writes[0] as { title: string }).title, "After");
  // Escape leaves the title as it was.
  fireEvent.click(sessionMenu());
  fireEvent.click(await screen.findByRole("menuitem", { name: "Rename" }));
  fireEvent.keyDown(await screen.findByRole("textbox", { name: "Session name" }), {
    key: "Escape",
  });
  assert.ok(sessionMenu());
  assert.equal(writes.length, 1);
  fetch.restore();
});

test("the header's place says where the session started and who sent it, once, with no Item link", () => {
  render(
    <SessionPlace
      place={{
        kind: "slack",
        label: "Slack DM",
        audience: "dm",
        origin: "slack://T/D1/1.2",
        url: "https://x",
      }}
      sender="Ana"
    />,
  );
  const line = screen.getByTestId("session-place");
  assert.equal((line.textContent ?? "").match(/Slack DM/g)?.length, 1);
  assert.match(line.textContent ?? "", /from Ana/);
  assert.equal(within(line).queryByRole("link"), null);
});
