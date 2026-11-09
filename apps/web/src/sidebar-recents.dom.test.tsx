import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { cleanup, render, screen, within } from "@testing-library/react";
import type { Me, Preferences, ThreadSummary } from "./api.ts";
import { stubFetch } from "./dom.ts";
import { unreadPrefs } from "./preferences.ts";
import { recentDay, Sidebar } from "./Sidebar.tsx";
import type { Alert } from "./sessionCenter.ts";

let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  cleanup();
  net?.restore();
  net = undefined;
});

const prefs: Preferences = { ...unreadPrefs, display_name: "Ada", pinned: [] };
const me: Me = {
  principal: { subject: "ada", tenant: "acme", kind: "user", name: "Ada", roles: ["member"] },
  permissions: ["runs:read", "runs:write", "projects:read", "alerts:read"],
};

// Local calendar days, so the test holds in any time zone and at any hour.
const now = new Date();
const startToday = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
const at = (ms: number) => new Date(ms).toISOString();
const newest = at(Math.max(startToday + 2000, now.getTime() - 1000));
const earlierToday = at(startToday + 1000);
const yesterday = at(new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1, 12).getTime());
const lastWeek = at(new Date(now.getFullYear(), now.getMonth(), now.getDate() - 6, 12).getTime());

function session(
  id: string,
  title: string,
  updated_at: string,
  over: Partial<ThreadSummary> = {},
): ThreadSummary {
  return {
    id,
    title,
    named: true,
    origin: "api",
    channel: "general",
    agents: ["lead"],
    people: ["ada"],
    visibility: "private",
    messages: 1,
    turns: 1,
    branches: 0,
    active: 0,
    failed: 0,
    created_at: lastWeek,
    updated_at,
    ...over,
  };
}

test("a day is Today, Yesterday or Earlier by the reader's own calendar", () => {
  assert.equal(recentDay(newest, now), "today");
  assert.equal(recentDay(earlierToday, now), "today");
  assert.equal(recentDay(yesterday, now), "yesterday");
  assert.equal(recentDay(lastWeek, now), "earlier");
});

test("Recents is one list: Today, Yesterday, Earlier, newest first, each session once, no alert-only sessions", async () => {
  const research = session("s_research", "@lead Have your researcher check", newest);
  const smoke = session("s_smoke", "Say hello for the smoke test.", earlierToday);
  const deploy = session("s_deploy", "Why did the deploy fail", yesterday);
  const quarter = session("s_quarter", "Summarize the quarter", lastWeek);
  // A session opened only to hold an alert: nothing was asked in it.
  const health = session("s_health", "Agent health: code-reviewer", newest, {
    messages: 0,
    turns: 0,
  });
  net = stubFetch({
    "GET /api/marks": () => ({ archived: [], read: {}, tips: [] }),
    // The page repeats a session, as two pages can when it moves between them.
    "GET /api/sessions": () => ({
      sessions: [deploy, research, health, smoke, research, quarter].map((t) => ({
        id: t.id,
        parent: `console://${t.id}`,
        agent: "lead",
        project: "",
        title: t.title,
        status: "done",
        updated_at: t.updated_at,
        conversation: t,
      })),
      next: "",
    }),
  });
  // An open alert on yesterday's session marks it; it does not lift it above today.
  const alert = {
    id: "al-1",
    title: "deploy needs you",
    session: "s_deploy",
    state: "open",
    priority: "urgent",
    at: newest,
  } as unknown as Alert;
  render(
    <Sidebar
      open
      sessionMode
      onToggle={() => {}}
      route={{ page: "new" }}
      go={() => {}}
      me={me}
      prefs={prefs}
      can={(p) => me.permissions.includes(p)}
      threads={[research, smoke]}
      projects={[]}
      pinned={[]}
      onPin={async () => {}}
      inboxCount={0}
      live
      onSignOut={() => {}}
      sessionAlerts={[alert]}
    />,
  );
  const list = within(await screen.findByRole("list", { name: defaultCopy.sidebar.recents }));
  await list.findByText("Summarize the quarter");
  const groups = list.getAllByTestId("recents-group");
  assert.deepEqual(
    groups.map((g) => [
      g.getAttribute("data-day"),
      g.querySelector("p")?.textContent,
      [...g.querySelectorAll("li [data-title], li button")]
        .map((b) => b.getAttribute("aria-label") ?? b.textContent ?? "")
        .filter((t) => /researcher|smoke|deploy|quarter/i.test(t))
        .map(
          (t) =>
            t.match(
              /@lead Have your researcher check|Say hello for the smoke test\.|Why did the deploy fail|Summarize the quarter/,
            )?.[0],
        )
        .filter((t, i, all) => t && all.indexOf(t) === i),
    ]),
    [
      [
        "today",
        defaultCopy.history.today,
        ["@lead Have your researcher check", "Say hello for the smoke test."],
      ],
      ["yesterday", defaultCopy.history.yesterday, ["Why did the deploy fail"]],
      ["earlier", defaultCopy.sidebar.earlier, ["Summarize the quarter"]],
    ],
  );
  for (const title of ["@lead Have your researcher check", "Say hello for the smoke test."])
    assert.equal(list.getAllByText(title).length, 1, `${title} is listed once`);
  assert.equal(
    list.queryAllByText("Agent health: code-reviewer").length,
    0,
    "alerts are under Alerts",
  );
});

test("Sessions lists newest first; an alert marks its row without moving it; alert-only sessions are left out", async () => {
  const { SessionsPage } = await import("./SessionCenter.tsx");
  const rows = [
    session("s_old", "Why did the deploy fail", yesterday),
    session("s_new", "In one line: is MD5 acceptable", newest),
    session("s_health", "Agent health: code-reviewer", newest, { messages: 0, turns: 0 }),
  ];
  net = stubFetch({
    "GET /api/sessions": () => ({
      sessions: rows.map((t) => ({
        id: t.id,
        parent: `console://${t.id}`,
        agent: "lead",
        project: "",
        title: t.title,
        status: "done",
        updated_at: t.updated_at,
        conversation: t,
      })),
      next: "",
    }),
  });
  const alerts = [
    {
      id: "al-1",
      title: "deploy needs you",
      session: "s_old",
      state: "open",
      priority: "urgent",
      at: newest,
    },
    {
      id: "al-2",
      title: "code-reviewer needs review",
      session: "s_health",
      state: "open",
      priority: "normal",
      at: newest,
    },
  ] as unknown as Alert[];
  render(<SessionsPage route={{ page: "sessions" }} go={() => {}} alerts={alerts} tick={0} />);
  await screen.findByRole("link", { name: /In one line: is MD5 acceptable/ });
  const titles = screen
    .getAllByRole("link")
    .map((a) => a.textContent ?? "")
    .filter((t) => /MD5|deploy|Agent health/.test(t));
  assert.deepEqual(titles, ["In one line: is MD5 acceptable", "Why did the deploy fail"]);
  const deploy = screen.getByRole("link", { name: /Why did the deploy fail/ }).closest("li");
  assert.match(deploy?.textContent ?? "", /Urgent/, "the alert still marks its row");
});
