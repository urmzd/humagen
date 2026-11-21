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

test("Recent lists sessions newest first with their source, time, and compact status", async () => {
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
    "flat",
    "sessions open newest first; projects are a grouping in the filter",
  );
  const list = within(await screen.findByRole("list", { name: "Recents" }));
  const names = (await list.findAllByRole("link")).map((a) => a.textContent?.split(/\d|Just/)[0]);
  assert.deepEqual(names, ["Answered", "Waiting on me", "Still going", "Quiet"]);
  const waiting = list.getByRole("link", {
    name: /^Waiting on me/,
    description: "Waiting for you",
  });
  assert.doesNotMatch(waiting.textContent ?? "", /Spring launch/);
  assert.equal(
    waiting.querySelector('[data-state="waiting"]')?.getAttribute("title"),
    "Waiting for you",
  );
  assert.equal(waiting.querySelector("[data-dot]"), null);
  assert.equal(
    list
      .getByRole("link", { name: /^Still going/, description: "Working" })
      .querySelector('[data-state="working"]')
      ?.getAttribute("title"),
    "Working",
  );
  const answered = await list.findByRole("link", { name: /^Answered/, description: "Unread" });
  assert.equal(answered.querySelector("[data-dot]")?.getAttribute("data-dot"), "unread");
  assert.match(answered.querySelector("[data-standing]")?.className ?? "", /font-semibold/);
  assert.equal(answered.querySelector("[data-state]"), null);
  const quiet = list.getByRole("link", { name: /^Quiet/ });
  assert.match(quiet.querySelector("[data-row-age]")?.textContent ?? "", /ago\s*Web \/ Console$/);
  assert.equal(quiet.querySelector("[data-state]"), null);
  assert.equal(quiet.querySelector("[data-dot]"), null);
  // The header says how many each section holds.
  assert.equal(
    screen.getByRole("button", { name: /^Recent/ }).querySelector("[data-count]")?.textContent,
    "4",
  );
});

test("a row's second line says how long ago it moved and where it started, in words with its glyph", async () => {
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
  const list = within(await screen.findByRole("list", { name: "Recents" }));
  const expected: [RegExp, string, string, string][] = [
    [/^Repo status/, "slack_dm", "Slack DM", "From a Slack DM"],
    [/^Deploy rollback/, "slack", "Slack", "From a Slack thread"],
    [/^Review PR 482/, "github", "GitHub", "From GitHub"],
    [/^Weekly digest/, "scheduled", "Schedule", "From a schedule"],
    [/^Standing goal/, "assignment", "Assignment", "From an assignment"],
    [/^Launch plan/, "console", "Web / Console", "Started in the web console"],
  ];
  for (const [name, source, short, words] of expected) {
    const row = await list.findByRole("link", { name });
    const meta = row.querySelector("[data-row-age]");
    const mark = meta?.querySelector("[data-source]");
    assert.equal(mark?.getAttribute("data-source"), source, `${name} says where it started`);
    assert.equal(mark?.textContent, short, "the source is written, not only drawn");
    assert.equal(mark?.getAttribute("title"), words);
    assert.ok(mark?.querySelector("svg"), "beside its glyph");
    assert.equal(meta?.textContent, `3h ago${short}`, "after how long ago it moved");
    assert.match(row.textContent ?? "", new RegExp(`, ${words}$`));
  }
  for (const glyph of ["!", "•", "|"])
    assert.ok(
      ![...document.querySelectorAll("[data-thread-row]")].some((r) =>
        r.textContent?.includes(glyph),
      ),
      `no row carries a bare ${glyph}`,
    );
});

test("status badges distinguish approvals, questions, failures, and running work with accessible details", async () => {
  const since = new Date(Date.now() - 24 * hour).toISOString();
  const fresh = new Date().toISOString();
  const rows = [
    task("s1", "Approve contract", {
      active: 1,
      waiting: 1,
      waiting_for: "file_write",
      waiting_kind: "approval",
      waiting_on_you: true,
    }),
    task("s2", "Pick a scope", {
      active: 1,
      waiting: 1,
      waiting_for: "Which repositories should I check?",
      waiting_kind: "question",
      waiting_on_you: true,
    }),
    task("s3", "Someone else's call", {
      active: 1,
      waiting: 1,
      waiting_for: "deploy",
      waiting_kind: "approval",
      waiting_on_you: false,
    }),
    task("s4", "Broke", {
      failed: 1,
      failure: "provider: rate limited (429)",
      last_agent: true,
      updated_at: fresh,
    }),
    task("s5", "Recovered", { failed: 1 }),
    task("s6", "Deploying", { active: 1 }),
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
  const list = within(await screen.findByRole("list", { name: "Recents" }));
  const statuses = [
    [/^Approve contract/, "approval", "Waiting for you: approve file_write", "text-aui-warn"],
    [
      /^Pick a scope/,
      "question",
      "Waiting for you: Which repositories should I check?",
      "text-aui-warn",
    ],
    [/^Someone else's call/, "approval", "Waiting for approval: deploy", "text-aui-muted"],
    [/^Broke/, "failed", "Failed: provider: rate limited (429)", "text-aui-bad", true],
    [/^Deploying/, "working", "Working", "text-aui-muted"],
  ] as const;
  for (const [name, state, reason, tone, unread] of statuses) {
    const row = await list.findByRole("link", {
      name,
      description: unread ? `${reason}, Unread` : reason,
    });
    const badge = row.querySelector(`[data-state="${state}"]`);
    assert.ok(badge);
    assert.equal(badge.getAttribute("title"), reason);
    assert.ok(badge.querySelector("svg"));
    assert.equal(badge.textContent, "");
    assert.ok(badge.firstElementChild?.classList.contains("rounded-full"));
    assert.ok(badge.firstElementChild?.classList.contains(tone), `${state} uses ${tone}`);
    assert.equal(row.querySelector("[data-dot]"), null, "the badge carries the alert by itself");
    assert.ok(
      !row.textContent?.includes(reason),
      "the details stay out of the link's visible text",
    );
  }
  const recovered = await list.findByRole("link", { name: /^Recovered/ });
  assert.equal(
    recovered.querySelector("[data-state]"),
    null,
    "a failure a later turn answered is not one: the session listing names none",
  );
  for (const row of document.querySelectorAll("[data-thread-row]"))
    assert.doesNotMatch(row.innerHTML, /aui-accent/, "nothing in a row uses the accent");
});

test("sessions someone else started that the reader joined have their own section, with a count", async () => {
  const rows = [
    task("s_mine", "My plan"),
    task("s_joined", "Vignesh's org lookup", { joined: true }),
    task("s_pin", "Pinned review"),
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
  render(<Bar sessionMode pinned={["s_pin"]} />);
  const joined = await screen.findByRole("region", { name: "Participated" });
  await within(joined).findByRole("link", { name: /^Vignesh's org lookup/ });
  const recent = within(await screen.findByRole("list", { name: "Recents" }));
  await recent.findByRole("link", { name: /^My plan/ });
  assert.equal(recent.queryByRole("link", { name: /^Vignesh's/ }), null, "listed once");
  assert.equal(recent.queryByRole("link", { name: /^Pinned review/ }), null);
  const count = (name: RegExp) =>
    screen.getByRole("button", { name }).querySelector("[data-count]")?.textContent;
  assert.equal(count(/^Participated/), "1");
  assert.equal(count(/^Pinned/), "1");
  assert.equal(count(/^Recent/), "1");
  // Participated sits above Pinned, which sits above Recent.
  const order = [...document.querySelectorAll("section[aria-label]")].map((s) =>
    s.getAttribute("aria-label"),
  );
  assert.deepEqual(order, ["Participated", "Pinned", "Sessions"]);
});

test("the sidebar's edge resizes it between its bounds, by drag or arrow keys, and remembers the width", async () => {
  window.localStorage.removeItem("humagen.sidebar.width");
  net = stubFetch({
    "GET /api/marks": () => ({ archived: [], read: {}, tips: [] }),
    "GET /api/threads": () => ({ threads: [], next: null }),
  });
  render(<Bar />);
  const edge = await screen.findByRole("separator", { name: "Resize sidebar" });
  assert.equal(edge.getAttribute("aria-valuenow"), "296");
  assert.equal(edge.getAttribute("aria-valuemin"), "240");
  assert.equal(edge.getAttribute("aria-valuemax"), "440");
  const width = () => document.documentElement.style.getPropertyValue("--humagen-sidebar-w");
  fireEvent.keyDown(edge, { key: "ArrowRight" });
  assert.equal(edge.getAttribute("aria-valuenow"), "312");
  assert.equal(width(), "312px");
  assert.equal(window.localStorage.getItem("humagen.sidebar.width"), "312");
  fireEvent.keyDown(edge, { key: "End" });
  assert.equal(edge.getAttribute("aria-valuenow"), "440");
  fireEvent.keyDown(edge, { key: "ArrowRight" });
  assert.equal(edge.getAttribute("aria-valuenow"), "440", "it stops at its widest");
  fireEvent.pointerDown(edge, { button: 0, clientX: 440, pointerId: 1 });
  fireEvent.pointerMove(edge, { clientX: 100, pointerId: 1 });
  fireEvent.pointerUp(edge, { clientX: 100, pointerId: 1 });
  assert.equal(edge.getAttribute("aria-valuenow"), "240", "it stops at its narrowest");
  assert.equal(window.localStorage.getItem("humagen.sidebar.width"), "240");
  fireEvent.doubleClick(edge);
  assert.equal(edge.getAttribute("aria-valuenow"), "296", "a double click puts it back");
  window.localStorage.removeItem("humagen.sidebar.width");
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
    // The rows arrive after the Projects view has drawn its groups.
    "GET /api/threads": async () => {
      await new Promise((r) => setTimeout(r, 50));
      return { threads: [task("t1", "Ops review", { project: "Ops" })], next: null };
    },
  });
  render(<Bar projects={[{ name: "Ops", title: "Ops" } as ProjectView]} />);
  await view("Projects");
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
    name: "only sessions the reader joined",
    threads: [task("t1", "Joined plan", { joined: true })],
    pinned: [] as string[],
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
    // By project is where folders could contradict the message.
    await view("Projects");
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
  await screen.findByRole("list", { name: "Recents" });
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
