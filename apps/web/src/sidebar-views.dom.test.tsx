import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { Marks, Me, Preferences, ProjectView, ThreadSummary } from "./api.ts";
import { stubFetch } from "./dom.ts";
import { unreadPrefs } from "./preferences.ts";
import type { Route } from "./route.ts";
import { Sidebar } from "./Sidebar.tsx";

// DES-054: Recents, Pinned, Projects and Archived, each row's source, state
// and age, and archiving a conversation for the reader alone.

let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  if (!net) return;
  cleanup();
  net.restore();
  net = undefined;
});

const prefs: Preferences = { ...unreadPrefs, display_name: "Ada", pinned: [] };
const me: Me = {
  principal: { subject: "ada", tenant: "acme", kind: "user", name: "Ada", roles: ["member"] },
  permissions: ["runs:read", "runs:write", "projects:read"],
};

const hour = 3_600_000;
function task(id: string, title: string, over: Partial<ThreadSummary> = {}): ThreadSummary {
  return {
    id,
    title,
    named: true,
    origin: "api",
    channel: "general",
    agents: ["lead"],
    people: ["ada"],
    visibility: "shared",
    messages: 0,
    turns: 1,
    branches: 0,
    active: 0,
    failed: 0,
    created_at: new Date(Date.now() - 48 * hour).toISOString(),
    updated_at: new Date(Date.now() - 3 * hour).toISOString(),
    ...over,
  };
}

function Bar({
  pinned = [],
  route = { page: "new" },
  onGo = () => {},
  projects = [],
  sessionMode = false,
}: {
  pinned?: string[];
  route?: Route;
  onGo?: (r: Route) => void;
  projects?: ProjectView[];
  sessionMode?: boolean;
}) {
  const noop = () => {};
  return (
    <Sidebar
      open
      sessionMode={sessionMode}
      onToggle={noop}
      route={route}
      go={onGo}
      me={me}
      prefs={{ ...prefs, pinned }}
      can={(p) => me.permissions.includes(p)}
      threads={[]}
      projects={projects}
      pinned={pinned}
      onPin={async () => {}}
      inboxCount={0}
      live
      onSignOut={noop}
    />
  );
}

const view = async (name: "Recents" | "Projects" | RegExp) => {
  fireEvent.click(await screen.findByRole("button", { name: "Filter conversations" }));
  const panel = within(await screen.findByRole("dialog", { name: "Filter conversations" }));
  const disclosure = panel.getByRole("button", { name: /^View / });
  fireEvent.click(disclosure);
  fireEvent.click(
    within(panel.getByRole("group", { name: "View" })).getByRole("button", {
      name: name === "Projects" ? "By project" : name === "Recents" ? "By day" : name,
    }),
  );
  fireEvent.keyDown(disclosure, { key: "Escape" });
};

test("Recents lists every project newest first, each row one line with one state mark", async () => {
  const since = new Date(Date.now() - 24 * hour).toISOString();
  const marks: Marks = { archived: [], read: {}, tips: [], since };
  net = stubFetch({
    "GET /api/marks": () => marks,
    "GET /api/threads": () => ({
      threads: [
        task("t1", "Waiting on me", { project: "Launch", active: 1, waiting: 1 }),
        task("t2", "Still going", { active: 1 }),
        task("t3", "Answered", { last_agent: true, updated_at: new Date().toISOString() }),
        task("t4", "Quiet", { project: "Launch", updated_at: "2026-01-01T00:00:00Z" }),
      ],
      next: null,
    }),
  });
  render(<Bar projects={[{ name: "Launch", title: "Spring launch" } as ProjectView]} />);
  const start = screen.getByRole("link", { name: "New" });
  assert.ok(start.closest("[data-sidebar-start]"));
  assert.equal(
    start.closest("[data-sidebar-scroll]"),
    null,
    "starting work stays outside the scrolling list",
  );
  assert.ok(screen.getByRole("button", { name: "Filter conversations" }));
  assert.equal(
    screen.getByRole("region", { name: "Sessions" }).getAttribute("data-view"),
    "projects",
    "sessions open filed under their projects",
  );
  await view("Recents");
  const list = within(await screen.findByRole("list", { name: "Recents" }));
  const waiting = await list.findByRole("link", { name: /^Waiting on me/ });
  assert.doesNotMatch(waiting.textContent ?? "", /Spring launch/);
  assert.match(waiting.textContent ?? "", /Needs you/);
  assert.equal(
    waiting.querySelector("[data-standing]")?.getAttribute("data-standing"),
    "needs",
    "waiting on a person has its own still mark",
  );
  assert.equal(
    list
      .getByRole("link", { name: /^Still going/ })
      .querySelector("[data-standing]")
      ?.getAttribute("data-standing"),
    "running",
  );
  const answered = list.getByRole("link", { name: /^Answered/ });
  // Unread is the title's weight, not a dot: nothing is marked in the accent.
  await waitFor(() =>
    assert.equal(
      answered.querySelector("[data-standing]")?.getAttribute("data-standing"),
      "unread",
    ),
  );
  assert.equal(answered.querySelector("[data-row-age] [data-standing]"), null);
  assert.match(answered.textContent ?? "", /, Unread$/);
  const quiet = list.getByRole("link", { name: /^Quiet/ });
  assert.equal(quiet.querySelector("[data-row-age]")?.textContent, "");
  assert.equal(quiet.querySelector("[data-standing]"), null);
});

test("session rows show relative ages, and an unread reply keeps its age beside a heavier title", async () => {
  const since = new Date(Date.now() - 24 * hour).toISOString();
  const rows = [
    task("s_read", "Read session"),
    task("s_unread", "Unread reply", { last_agent: true }),
  ];
  net = stubFetch({
    "GET /api/marks": () => ({ archived: [], read: {}, tips: [], since }),
    "GET /api/sessions": () => ({
      sessions: rows.map((conversation) => ({
        id: conversation.id,
        parent: "console:ada",
        agent: "lead",
        title: conversation.title,
        status: "idle",
        updated_at: conversation.updated_at,
        conversation,
      })),
      next: "",
    }),
  });
  render(<Bar sessionMode />);
  await view("Recents");
  const list = within(await screen.findByRole("list", { name: "Recents" }));
  const read = await list.findByRole("link", { name: /^Read session/ });
  assert.equal(read.querySelector("[data-row-age]")?.textContent, "3h");
  const unread = list.getByRole("link", { name: /^Unread reply/ });
  await waitFor(() => assert.ok(unread.querySelector('[data-standing="unread"]')));
  assert.equal(unread.querySelector("[data-row-age]")?.textContent, "3h");
  assert.match(unread.querySelector('[data-standing="unread"]')?.className ?? "", /font-semibold/);
});

test("every row leads with where its session started, said in words, never a bare dot", async () => {
  const rows = [
    task("s_dm", "Repo status", { source: "slack_dm" }),
    task("s_thread", "Deploy rollback", { source: "slack" }),
    task("s_pr", "Review PR 482", { origin: "github" }),
    task("s_cron", "Weekly digest", { origin: "cron" }),
    task("s_work", "Standing goal", { origin: "assignment" }),
    task("s_web", "Launch plan"),
  ];
  net = stubFetch({
    "GET /api/marks": () => ({ archived: [], read: {}, tips: [] }),
    "GET /api/sessions": () => ({
      sessions: rows.map((conversation) => ({
        id: conversation.id,
        parent: "console:ada",
        agent: "lead",
        title: conversation.title,
        status: "idle",
        updated_at: conversation.updated_at,
        conversation,
      })),
      next: "",
    }),
  });
  render(<Bar sessionMode />);
  await view("Recents");
  const list = within(await screen.findByRole("list", { name: "Recents" }));
  const expected: [RegExp, string, string][] = [
    [/^Repo status/, "slack_dm", "From a Slack DM"],
    [/^Deploy rollback/, "slack", "From a Slack thread"],
    [/^Review PR 482/, "github", "From GitHub"],
    [/^Weekly digest/, "scheduled", "From a schedule"],
    [/^Standing goal/, "assignment", "From an assignment"],
    [/^Launch plan/, "console", "Started in the console"],
  ];
  for (const [name, source, words] of expected) {
    const row = await list.findByRole("link", { name });
    const mark = row.querySelector("[data-source]");
    assert.equal(mark?.getAttribute("data-source"), source, `${name} leads with its source`);
    assert.equal(mark?.getAttribute("title"), words, "a mouse reads the source on hover");
    assert.equal(mark?.getAttribute("aria-hidden"), "true");
    assert.ok(mark?.querySelector("svg"), "the source is a glyph");
    // A screen reader hears it after the title, so the row is still found by its title.
    assert.match(row.textContent ?? "", new RegExp(`, ${words}$`));
    assert.equal(row.firstElementChild, mark, "the source leads the row");
  }
  assert.equal(document.querySelector(".humagen-row-dot"), null, "no row carries a dot");
});

test("a row's state is a labelled mark beside its age, red only when a reply failed", async () => {
  const since = new Date(Date.now() - 24 * hour).toISOString();
  net = stubFetch({
    "GET /api/marks": () => ({ archived: [], read: {}, tips: [], since }),
    "GET /api/threads": () => ({
      threads: [
        task("t1", "Approve contract", { active: 1, waiting: 1 }),
        task("t2", "Deploying", { active: 1 }),
        task("t3", "Broke", { failed: 1 }),
        task("t4", "Fresh answer", { last_agent: true, updated_at: new Date().toISOString() }),
        task("t5", "Nothing new"),
      ],
      next: null,
    }),
  });
  render(<Bar />);
  await view("Recents");
  const list = within(await screen.findByRole("list", { name: "Recents" }));
  const mark = async (name: RegExp) =>
    (await list.findByRole("link", { name })).querySelector<HTMLElement>(
      "[data-row-age] [data-standing]",
    );
  const needs = await mark(/^Approve contract/);
  assert.equal(needs?.getAttribute("title"), "Needs you");
  assert.match(needs?.className ?? "", /text-aui-warn/);
  const running = await mark(/^Deploying/);
  assert.equal(running?.getAttribute("title"), "Running");
  assert.ok(running?.querySelector(".humagen-row-spinner"));
  assert.doesNotMatch(running?.className ?? "", /aui-bad|aui-accent/);
  const failed = await mark(/^Broke/);
  assert.equal(failed?.getAttribute("title"), "A reply failed");
  assert.match(failed?.className ?? "", /text-aui-bad/, "red says failure");
  assert.match(list.getByRole("link", { name: /^Broke/ }).textContent ?? "", /, A reply failed$/);
  await waitFor(async () => assert.equal(await mark(/^Fresh answer/), null));
  assert.equal(await mark(/^Nothing new/), null);
  // Hovering a row trades only its age for the ⋮ button: every state mark stays
  // in view, so its tooltip can show under the pointer.
  for (const name of [/^Approve contract/, /^Deploying/, /^Broke/]) {
    const row = list.getByRole("link", { name });
    const state = row.querySelector<HTMLElement>("[data-standing]");
    for (let el: HTMLElement | null = state; el && el !== row.parentElement; el = el.parentElement)
      assert.doesNotMatch(el.className, /:invisible/, `${name} keeps its state mark on hover`);
    assert.match(
      row.querySelector<HTMLElement>("[data-row-time]")?.className ?? "",
      /group-hover\/task:invisible/,
      "the age gives way to the menu",
    );
  }
  // Nothing in a row is drawn in the accent.
  for (const row of document.querySelectorAll("[data-thread-row]"))
    assert.doesNotMatch(row.innerHTML, /aui-accent/);
});

test("the row menu archives a session, and Archived brings it back", async () => {
  let marks: Marks = { archived: [], read: {}, tips: [] };
  net = stubFetch({
    "GET /api/marks": () => marks,
    "PATCH /api/threads/t1/marks": (body) => {
      const on = (body as { archived: boolean }).archived;
      marks = { ...marks, archived: on ? ["t1"] : [] };
      return marks;
    },
    "GET /api/threads": () => ({ threads: [task("t1", "Put me away")], next: null }),
  });
  render(<Bar />);
  fireEvent.click(await screen.findByRole("button", { name: "Manage Put me away" }));
  await waitFor(() =>
    assert.equal(
      screen.getByRole("button", { name: "Manage Put me away" }).getAttribute("aria-expanded"),
      "true",
    ),
  );
  fireEvent.click(await screen.findByRole("menuitem", { name: "Archive" }));
  await waitFor(() => assert.ok(!screen.queryByRole("link", { name: /^Put me away/ })));
  assert.ok(screen.getByText("Archived for you. Others still see it."));
  await view(/^Archived/);
  const archived = within(await screen.findByRole("list", { name: "Archived" }));
  fireEvent.click(await archived.findByRole("button", { name: "Manage Put me away" }));
  fireEvent.click(await screen.findByRole("menuitem", { name: "Unarchive" }));
  await waitFor(() => assert.deepEqual(marks.archived, []));
  await view("Recents");
  await screen.findByRole("link", { name: /^Put me away/ });
});

test("Projects groups by project, folds an empty one to No conversations, and caps a long one at See All", async () => {
  const many = Array.from({ length: 7 }, (_, i) =>
    task(`t${i}`, `Launch task ${i}`, { project: "Launch" }),
  );
  net = stubFetch({
    "GET /api/marks": () => ({ archived: [], read: {}, tips: [] }),
    "GET /api/threads": () => ({ threads: many, next: null }),
  });
  render(
    <Bar
      projects={[
        { name: "Launch", title: "Launch" } as ProjectView,
        { name: "Ops", title: "Ops" } as ProjectView,
      ]}
    />,
  );
  await view("Projects");
  const list = within(await screen.findByRole("list", { name: "Projects" }));
  assert.equal(list.getAllByRole("link", { name: /^Launch task/ }).length, 5);
  fireEvent.click(list.getByRole("button", { name: "See All (7)" }));
  assert.equal(list.getAllByRole("link", { name: /^Launch task/ }).length, 7);
  const ops = list.getByText("Ops").closest("details");
  assert.equal(ops?.open, false, "an empty project starts folded");
  assert.match(ops?.textContent ?? "", /No conversations/);
});

test("a project group that rendered before its rows arrived opens when they do, unless the reader folded it", async () => {
  net = stubFetch({
    "GET /api/marks": () => ({ archived: [], read: {}, tips: [] }),
    "GET /api/threads": () => ({
      threads: [task("t1", "Ops review", { project: "Ops" })],
      next: null,
    }),
  });
  render(<Bar projects={[{ name: "Ops", title: "Ops" } as ProjectView]} />);
  // Projects is the default view, so the group is drawn while the list is still loading.
  const list = within(await screen.findByRole("list", { name: "Projects" }));
  await list.findByRole("link", { name: /^Ops review/ });
  const ops = list.getByText("Ops").closest("details");
  await waitFor(() => assert.equal(ops?.open, true, "rows arriving open their group"));
  fireEvent.click(ops?.querySelector("summary") as HTMLElement);
  if (ops) ops.open = false;
  fireEvent(ops as HTMLElement, new Event("toggle"));
  await waitFor(() => assert.equal(ops?.open, false, "a group the reader folded stays folded"));
});

/** Every list and item in the sidebar holds something: no empty block takes up room. */
function assertNoEmptyBlocks() {
  const nav = screen.getByRole("complementary", { name: "Navigation" });
  for (const el of nav.querySelectorAll("ul:not([hidden]), li, details")) {
    const shown = el.textContent?.trim() || el.querySelector("svg, img, input");
    assert.ok(shown, `an empty ${el.tagName.toLowerCase()} renders in the sidebar`);
  }
}

for (const c of [
  {
    name: "no sessions, with projects",
    threads: [] as ThreadSummary[],
    pinned: [] as string[],
    filter: false,
    says: "Your sessions appear here. Start with New.",
  },
  {
    name: "only pinned sessions",
    threads: [task("t1", "Pinned plan")],
    pinned: ["t1"],
    filter: false,
    says: "No other sessions yet.",
  },
  {
    name: "a filter that matches nothing",
    threads: [] as ThreadSummary[],
    pinned: [] as string[],
    filter: true,
    says: "No session matches these filters.",
  },
])
  test(`an empty Recents says one short, true thing and draws nothing else: ${c.name}`, async () => {
    net = stubFetch({
      "GET /api/marks": () => ({ archived: [], read: {}, tips: [] }),
      "GET /api/threads": (_body, query) => ({
        threads: query.get("standing") ? [] : c.threads,
        next: null,
      }),
    });
    render(
      <Bar
        pinned={c.pinned}
        projects={[
          { name: "Launch", title: "Launch" } as ProjectView,
          { name: "Ops", title: "Ops" } as ProjectView,
        ]}
      />,
    );
    if (c.filter) {
      fireEvent.click(await screen.findByRole("button", { name: "Filter conversations" }));
      const panel = within(await screen.findByRole("dialog", { name: "Filter conversations" }));
      fireEvent.click(panel.getByRole("button", { name: /^Status / }));
      fireEvent.click(
        within(panel.getByRole("group", { name: "Status" })).getByRole("button", {
          name: "Failed",
        }),
      );
      fireEvent.keyDown(panel.getByRole("button", { name: /^Status / }), { key: "Escape" });
    }
    const message = await screen.findByText(c.says);
    const list = message.closest("ul");
    assert.ok(list);
    assert.equal(list.children.length, 1, "the message is the list's only item");
    assert.equal(list.querySelector("details"), null, "no project folder contradicts the message");
    assertNoEmptyBlocks();
  });

test("opening a conversation marks it read", async () => {
  net = stubFetch({
    "GET /api/marks": () => ({ archived: [], read: {}, tips: [] }),
    "PATCH /api/threads/t1/marks": () => ({
      archived: [],
      read: { t1: new Date().toISOString() },
      tips: [],
    }),
    "GET /api/threads": () => ({ threads: [task("t1", "Open me")], next: null }),
  });
  render(<Bar route={{ page: "conversation", id: "t1" }} />);
  await waitFor(() =>
    assert.ok(
      net?.seen.some(
        (r) =>
          r.method === "PATCH" &&
          r.path === "/api/threads/t1/marks" &&
          (r.body as { read?: boolean }).read === true,
      ),
    ),
  );
});

test("search has one way in: the Search row, with no tip card repeating it", async () => {
  let marks: Marks = { archived: [], read: {}, tips: [] };
  net = stubFetch({
    "GET /api/marks": () => marks,
    "POST /api/marks/tips": (body) => {
      marks = { ...marks, tips: [...marks.tips, (body as { name: string }).name] };
      return marks;
    },
    "GET /api/threads": () => ({ threads: [], next: null }),
  });
  const noop = () => {};
  const bar = () => (
    <Sidebar
      open
      onToggle={noop}
      route={{ page: "new" }}
      go={noop}
      me={me}
      prefs={prefs}
      can={(p) => me.permissions.includes(p)}
      threads={[]}
      pinned={[]}
      onPin={async () => {}}
      inboxCount={0}
      live
      onSignOut={noop}
      onSearch={noop}
    />
  );
  render(bar());
  await screen.findByRole("list", { name: "Projects" });
  await new Promise((r) => setTimeout(r, 20));
  assert.equal(screen.queryByRole("complementary", { name: "Search across conversations" }), null);
  assert.ok(screen.getByRole("button", { name: "Search" }));
});

test("choosing a sidebar filter returns keyboard focus to its disclosure", async () => {
  net = stubFetch({
    "GET /api/marks": () => ({ archived: [], read: {}, tips: [] }),
    "GET /api/threads": () => ({ threads: [task("t1", "A session")], next: null }),
  });
  render(<Bar />);
  fireEvent.click(await screen.findByRole("button", { name: "Filter conversations" }));
  const disclosure = screen.getByRole("button", { name: "Status All" });
  disclosure.focus();
  fireEvent.click(disclosure);
  const failed = within(screen.getByRole("group", { name: "Status" })).getByRole("button", {
    name: "Failed",
  });
  failed.focus();
  assert.equal(document.activeElement, failed);
  fireEvent.click(failed);
  await waitFor(() => assert.equal(disclosure.getAttribute("aria-expanded"), "false"));
  assert.equal(document.activeElement, disclosure);
  assert.equal(screen.queryByRole("group", { name: "Status" }), null);
});
