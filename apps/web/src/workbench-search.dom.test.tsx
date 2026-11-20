import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import type { ResponseState, ResponseTool } from "@humagen/core";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { Run } from "./adapter.ts";
import { stubFetch } from "./dom.ts";
import { href, type WorkbenchTabId } from "./route.ts";
import { summarize } from "./sessionSummary.ts";
import { GROUP_SHOWN, isSearchShortcut, WorkbenchSearch } from "./WorkbenchSearch.tsx";
import { WORKBENCH_TABS, type WorkbenchTabProps } from "./workbench.ts";
import {
  resolveTarget,
  type SearchItem,
  type SessionRead,
  searchItems,
  searchSession,
} from "./workbenchSearch.ts";

const words = defaultCopy.workbenchSearch;
const thread = "s_search";
let fetched: { restore: () => void } | undefined;

afterEach(() => {
  cleanup();
  fetched?.restore();
  fetched = undefined;
});

const tool = (id: string, name: string, more: Partial<ResponseTool> = {}): ResponseTool => ({
  type: "tool",
  id,
  name,
  kind: "function" as ResponseTool["kind"],
  input: {},
  depth: 1,
  status: "done" as ResponseTool["status"],
  content: [],
  ...more,
});

const responses = [
  {
    id: "m1",
    rootRun: "run-1",
    status: "done",
    content: [
      tool("t1", "bash", { input: { command: "pnpm test" }, title: "Run the tests" }),
      tool("t2", "browser_navigate", {
        input: { action: "goto", url: "https://example.test/login" },
        title: "Open the sign-in page",
      }),
      tool("t3", "github_create_pull_request", {
        result: { text: "Opened https://github.com/urmzd/humagen/pull/300" },
      } as Partial<ResponseTool>),
      tool("t4", "call_agent", { kind: "agent" as ResponseTool["kind"], agent: "reviewer" }),
    ],
  },
  {
    id: "m2",
    rootRun: "run-2",
    status: "done",
    content: [
      tool("t5", "bash", { input: { command: "git status" } }),
      tool("t6", "bash", { input: { command: "pnpm typecheck" } }),
      tool("t7", "call_agent", { kind: "agent" as ResponseTool["kind"], agent: "reviewer" }),
    ],
  },
] as unknown as ResponseState[];

const summary = summarize(responses, {
  files: [
    { id: "a1", title: "Test report.pdf", kind: "pdf", mime: "application/pdf" },
    { id: "a2", title: "pnpm-notes.md", kind: "document", mime: "text/markdown" },
  ],
  uploads: ["leases.pdf"],
});

const read: SessionRead = {
  links: [{ target: "s_other", kind: "evidence" }],
  children: [{ id: "s_child", agent: "reviewer", title: "Review the tests" }],
  messages: [
    {
      id: "sm1",
      message: {
        id: "msg-1",
        attachments: [{ name: "leases.pdf", media_type: "application/pdf" }],
      },
    },
  ],
};

const runs = [{ id: "run-1" }, { id: "run-2" }] as Run[];
const all = () =>
  searchItems({ thread, runs: ["run-1", "run-2"], responses, summary, session: read });
const byKey = (items: SearchItem[], key: string) => items.find((i) => i.key === key);
const titles = (items: SearchItem[], query: string) =>
  searchSession(items, query).map((g) => [g.group, ...g.hits.map((h) => h.item.title)]);

test("what a session has is sorted by kind, and each thing knows where it opens", () => {
  const items = all();
  assert.deepEqual(byKey(items, "file:a1")?.target, { kind: "file", artifact: "a1" });
  const pull = items.find((i) => i.group === "pulls");
  assert.equal(
    pull?.target?.kind === "url" && pull.target.url,
    "https://github.com/urmzd/humagen/pull/300",
  );

  // A command opens in Terminal on its call, or in its answer's activity until Terminal is built.
  const ran = byKey(items, "call:t1");
  assert.equal(ran?.group, "commands");
  assert.equal(ran?.title, "pnpm test");
  assert.deepEqual(ran?.target, {
    kind: "tab",
    tab: "terminal",
    ref: "t1",
    fallback: {
      kind: "route",
      route: { page: "conversation", id: thread, panel: "response", run: "run-1", step: "t1" },
    },
  });
  const step = byKey(items, "call:t2");
  assert.equal(step?.group, "browser");
  assert.equal(step?.title, "Open the sign-in page");
  assert.equal(step?.target?.kind === "tab" && step.target.tab, "browser");

  // Tools and agents are listed once each, with how often, at their latest call.
  assert.equal(byKey(items, "tool:bash")?.count, 3);
  assert.deepEqual(byKey(items, "tool:bash")?.target, {
    kind: "route",
    route: { page: "conversation", id: thread, panel: "response", run: "run-2", step: "t6" },
  });
  assert.equal(byKey(items, "agent:reviewer")?.count, 2);

  // Linked sessions open as sessions; an attachment opens the message it came with.
  assert.deepEqual(byKey(items, "session:s_child")?.target, {
    kind: "route",
    route: { page: "conversation", id: "s_child" },
  });
  assert.equal(byKey(items, "session:s_other")?.detail, words.note.evidence);
  assert.deepEqual(byKey(items, "attachment:msg-1:0")?.target, {
    kind: "route",
    route: { page: "conversation", id: thread, message: "msg-1" },
  });

  // Commands are newest first.
  assert.deepEqual(
    items.filter((i) => i.group === "commands").map((i) => i.title),
    ["pnpm typecheck", "git status", "pnpm test"],
  );
});

test("a call in an answer the page cannot open has no view of its own", () => {
  const items = searchItems({ thread, runs: ["run-2"], responses, summary });
  const ran = byKey(items, "call:t1");
  assert.deepEqual(ran?.target, { kind: "tab", tab: "terminal", ref: "t1", fallback: undefined });
  assert.equal(
    resolveTarget(ran?.target, () => false),
    undefined,
  );
  assert.equal(resolveTarget(ran?.target, () => true)?.kind, "tab");
});

test("matches are ranked as the composer's menu ranks them, group by group", () => {
  const items = all();
  // Everything, in the groups' order, for an empty query.
  assert.deepEqual(
    searchSession(items, "  ").map((g) => g.group),
    ["files", "pulls", "links", "commands", "browser", "tools", "agents", "attachments"],
  );
  // A title that starts with the query comes before one that only holds it;
  // equally close ones keep their order (newest first).
  assert.deepEqual(titles(items, "pnpm"), [
    ["files", "pnpm-notes.md"],
    ["commands", "pnpm typecheck", "pnpm test"],
  ]);
  assert.deepEqual(titles(items, "test")[0], ["files", "Test report.pdf"]);
  // A second line is matched when the title is not: the PDF by its type.
  assert.ok(titles(items, "PDF").some((g) => g[0] === "files" && g.includes("Test report.pdf")));
  // The letters to mark are the title's.
  const hit = searchSession(items, "status")[0]?.hits[0];
  assert.equal(hit?.item.title, "git status");
  assert.deepEqual(hit?.hits, [[4, 10]]);
  assert.deepEqual(searchSession(items, "zzzz-nothing"), []);
  // Letters in order with gaps count only when nothing holds the query whole.
  assert.deepEqual(titles(items, "gtsts"), [["commands", "git status"]]);
  assert.ok(!titles(items, "git").flat().includes("urmzd/humagen #300"));
});

test("the shortcut is Command or Control with Shift and F, and no other search's keys", () => {
  const key = (k: string, more: Record<string, boolean> = {}) => ({
    key: k,
    metaKey: false,
    ctrlKey: false,
    shiftKey: false,
    altKey: false,
    ...more,
  });
  assert.ok(isSearchShortcut(key("F", { metaKey: true, shiftKey: true })));
  assert.ok(isSearchShortcut(key("f", { ctrlKey: true, shiftKey: true })));
  // The workspace's search (⌘K), the editor's find (⌘F) and the composer's menu (⌘/).
  assert.ok(!isSearchShortcut(key("k", { metaKey: true })));
  assert.ok(!isSearchShortcut(key("f", { metaKey: true })));
  assert.ok(!isSearchShortcut(key("/", { metaKey: true })));
  assert.ok(!isSearchShortcut(key("f", { shiftKey: true })));
});

function mount(
  more: Partial<WorkbenchTabProps> & { built?: (tab: WorkbenchTabId) => boolean } = {},
) {
  const opened: [string, string?][] = [];
  const files: string[] = [];
  const selections: (string | undefined)[] = [];
  const props: WorkbenchTabProps = {
    thread,
    runs,
    responses,
    summary,
    canPost: true,
    onSelection: (ref) => selections.push(ref),
    openTab: (id, ref) => opened.push(ref === undefined ? [id] : [id, ref]),
    openFile: (id) => files.push(id),
    record: () => undefined,
    expanded: false,
    ...more,
  };
  const out = render(<WorkbenchSearch built={() => false} {...props} />);
  return { opened, files, selections, props, ...out };
}

const stubSession = (answer: () => unknown = () => read) => {
  fetched = stubFetch({ [`GET /api/sessions/${thread}`]: answer });
  return fetched as ReturnType<typeof stubFetch>;
};
const result = (key: string) =>
  document.querySelector<HTMLElement>(`[data-result="${key}"]`) as HTMLElement;
const field = () => screen.getByRole("searchbox", { name: words.field }) as HTMLInputElement;

test("the tab lists what the session has by kind, and typing narrows it", async () => {
  stubSession();
  mount();
  // The session's own record brings its links and attachments.
  await screen.findByText("Review the tests");
  for (const group of [
    "files",
    "pulls",
    "links",
    "commands",
    "browser",
    "tools",
    "agents",
  ] as const)
    assert.ok(screen.getByRole("heading", { name: new RegExp(`^${words.group[group]}`) }));
  assert.ok(screen.getByTestId("workbench-search-shortcut").textContent);

  fireEvent.change(field(), { target: { value: "pnpm" } });
  assert.deepEqual(
    [...document.querySelectorAll("[data-result]")].map((el) => el.getAttribute("data-result")),
    ["file:a2", "call:t6", "call:t1"],
  );
  assert.equal(result("call:t1").querySelector("mark")?.textContent, "pnpm");
  assert.equal(screen.getByRole("status").textContent, words.count(3));
});

test("each result opens where it lives, and a tab that is not built gives way to what exists", async () => {
  stubSession();
  const { files, opened } = mount();
  await screen.findByText("Review the tests");

  fireEvent.click(result("file:a1"));
  assert.deepEqual(files, ["a1"]);

  const pull = document.querySelector<HTMLAnchorElement>('[data-opens="url"]');
  assert.equal(pull?.getAttribute("href"), "https://github.com/urmzd/humagen/pull/300");
  assert.equal(pull?.target, "_blank");

  // No Terminal or Browser tab yet: the call opens in its answer's activity.
  assert.equal(
    result("call:t1").getAttribute("href"),
    href({ page: "conversation", id: thread, panel: "response", run: "run-1", step: "t1" }),
  );
  assert.ok(within(result("call:t1")).getByText(words.opens.activity));
  assert.equal(
    result("attachment:msg-1:0").getAttribute("href"),
    href({ page: "conversation", id: thread, message: "msg-1" }),
  );
  assert.equal(
    result("session:s_child").getAttribute("href"),
    href({ page: "conversation", id: "s_child" }),
  );
  assert.deepEqual(opened, []);
});

test("with Terminal and Browser built, a command and a browser step open those tabs on their call", async () => {
  stubSession();
  const { opened } = mount({ built: (tab) => tab === "terminal" || tab === "browser" });
  await screen.findByText("Review the tests");
  assert.ok(within(result("call:t1")).getByText(defaultCopy.workbench.tab.terminal));
  fireEvent.click(result("call:t1"));
  fireEvent.click(result("call:t2"));
  assert.deepEqual(opened, [
    ["terminal", "t1"],
    ["browser", "t2"],
  ]);
});

test("a browser step reached through any provider is found, worded as the Browser tab words it", () => {
  const items = searchItems({
    thread,
    runs: ["run-9"],
    responses: [
      {
        id: "m9",
        rootRun: "run-9",
        status: "done",
        content: [
          tool("b1", "pw__browser_navigate", {
            input: { url: "https://example.test/orders" },
            result: {
              text: "- Page URL: https://example.test/orders\n- Page Title: Orders",
              sources: [],
              isError: false,
            },
          } as Partial<ResponseTool>),
          tool("b2", "mcp__pw__browser_click", { input: { element: "Late shipments link" } }),
        ],
      },
    ] as unknown as ResponseState[],
  });
  const steps = items.filter((i) => i.group === "browser");
  assert.deepEqual(
    steps.map((s) => [s.key, s.title, s.detail]),
    [
      ["call:b2", defaultCopy.workbenchBrowser.step.click("Late shipments link"), "Orders"],
      [
        "call:b1",
        defaultCopy.workbenchBrowser.step.navigate("https://example.test/orders"),
        "Orders",
      ],
    ],
  );
  assert.ok(steps.every((s) => s.target?.kind === "tab" && s.target.tab === "browser"));
});

test("the keyboard reaches the field and every result", async () => {
  stubSession();
  const { files, selections } = mount();
  await screen.findByText("Review the tests");

  // The shortcut puts the cursor in the field from anywhere on the page.
  assert.notEqual(document.activeElement, field());
  fireEvent.keyDown(window, { key: "F", metaKey: true, shiftKey: true });
  assert.equal(document.activeElement, field());

  fireEvent.change(field(), { target: { value: "report" } });
  fireEvent.keyDown(field(), { key: "ArrowDown" });
  assert.equal(document.activeElement, result("file:a1"));
  // Up from the first result is the field again; Escape in a result too.
  fireEvent.keyDown(result("file:a1"), { key: "ArrowUp" });
  assert.equal(document.activeElement, field());
  fireEvent.keyDown(field(), { key: "ArrowDown" });
  fireEvent.keyDown(result("file:a1"), { key: "Escape" });
  assert.equal(document.activeElement, field());

  // Enter in the field opens the closest match, with the query at the address first.
  fireEvent.keyDown(field(), { key: "Enter" });
  assert.deepEqual(files, ["a1"]);
  assert.equal(selections.at(-1), "report");
  // The field losing the cursor as the pane goes writes nothing more, which
  // would bring the tab back over what was opened.
  const before = selections.length;
  fireEvent.blur(field());
  assert.equal(selections.length, before);
  fireEvent.focus(field());

  // Leaving the field puts the query at the tab's address.
  // (Here nothing moves the address, so each time the field was left says it again.)
  fireEvent.blur(field());
  assert.deepEqual([...new Set(selections)], ["report"]);

  // Escape in the field clears the query before it closes anything.
  fireEvent.keyDown(field(), { key: "Escape" });
  assert.equal(field().value, "");
});

test("the tab opens on the query its address names", async () => {
  stubSession();
  mount({ selection: "git" });
  await waitFor(() => assert.ok(result("call:t5")));
  assert.equal(field().value, "git");
  assert.deepEqual(
    [...document.querySelectorAll("[data-result]")].map((el) => el.getAttribute("data-result")),
    ["call:t5", "tool:github_create_pull_request"],
  );
});

test("a long group shows its first few and the rest on request", async () => {
  stubSession(() => ({}));
  const many = [
    {
      id: "m1",
      rootRun: "run-1",
      status: "done",
      content: Array.from({ length: GROUP_SHOWN + 3 }, (_, i) =>
        tool(`c${i}`, "bash", { input: { command: `echo ${i}` } }),
      ),
    },
  ] as unknown as ResponseState[];
  mount({ responses: many, summary: summarize(many) });
  const more = await screen.findByRole("button", { name: words.showAll(GROUP_SHOWN + 3) });
  assert.equal(document.querySelectorAll('[data-result^="call:"]').length, GROUP_SHOWN);
  fireEvent.click(more);
  assert.equal(document.querySelectorAll('[data-result^="call:"]').length, GROUP_SHOWN + 3);
});

test("with nothing to find the tab says so and offers Details; with no match it offers to clear", async () => {
  stubSession(() => ({}));
  const { opened, selections } = mount({ responses: [], summary: summarize([]) });
  await screen.findByText(words.empty);
  fireEvent.click(screen.getByRole("button", { name: words.emptyAction }));
  assert.deepEqual(opened, [["details"]]);
  cleanup();

  const again = mount();
  await waitFor(() => assert.ok(result("file:a1")));
  fireEvent.change(field(), { target: { value: "qqqq" } });
  assert.ok(screen.getByText(words.none("qqqq")));
  fireEvent.click(
    within(screen.getByTestId("workbench-search"))
      .getAllByRole("button", { name: words.clear })
      .at(-1) as HTMLElement,
  );
  assert.equal(field().value, "");
  assert.ok(result("file:a1"));
  assert.deepEqual(selections, []);
  assert.deepEqual(again.selections, [undefined]);
});

test("while the session is read the tab shows rows loading, and a failed read is said with Retry", async () => {
  let fail = true;
  const net = stubSession(() =>
    fail ? new Response(JSON.stringify({ error: "down" }), { status: 500 }) : read,
  );
  mount({ responses: undefined, summary: undefined });
  assert.equal(document.querySelector("[aria-busy=true]")?.id, "workbench-search-results");
  cleanup();

  mount();
  // What the page already holds is still searched.
  await screen.findByText(words.partial);
  assert.ok(result("file:a1"));
  assert.equal(result("session:s_child"), null);
  fail = false;
  fireEvent.click(screen.getByRole("button", { name: words.retry }));
  await screen.findByText("Review the tests");
  assert.equal(screen.queryByText(words.partial), null);
  assert.ok(net.seen.every((s) => s.method === "GET"));
});

test("a thread that is not a session asks the server nothing", async () => {
  const net = stubSession();
  mount({ thread: "th-plain" });
  await waitFor(() => assert.ok(result("file:a1")));
  assert.deepEqual(net.seen, []);
});

test("Search is a built tab that fills the pane and is always on the strip", () => {
  const search = WORKBENCH_TABS.find((t) => t.id === "search");
  assert.ok(search);
  assert.equal(search.fill, true);
  assert.equal(search.available, undefined);
});
