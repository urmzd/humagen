import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { ItemCard } from "./cards.tsx";
import { stubFetch } from "./dom.ts";
import { unreadPrefs } from "./preferences.ts";
import { AlertsPage, HomeSessionGlance, SessionsPage } from "./SessionCenter.tsx";
import { Sidebar } from "./Sidebar.tsx";
import { type AgentSessionSummary, type Alert, attentionAlerts } from "./sessionCenter.ts";

let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  cleanup();
  net?.restore();
  net = undefined;
});
const now = new Date().toISOString();
const session = (id: string, parent = "console:p"): AgentSessionSummary => ({
  id,
  parent,
  agent: "reviewer",
  project: "Personal",
  title: `Work ${id}`,
  status: "working",
  updated_at: now,
  conversation: {
    id,
    title: `Work ${id}`,
    named: true,
    origin: "api",
    project: "Personal",
    channel: "",
    agents: ["reviewer"],
    people: ["ada"],
    visibility: "private",
    messages: 1,
    turns: 1,
    branches: 0,
    active: 1,
    waiting: 0,
    failed: 0,
    created_at: now,
    updated_at: now,
    last_agent: true,
    last_by: "reviewer",
    last: "Working",
  },
});
const alert = (
  id: string,
  priority: Alert["priority"] = "normal",
  state: Alert["state"] = "open",
): Alert => ({
  id,
  priority,
  state,
  session: "s_root",
  message: "msg1",
  title: `Alert ${id}`,
  rule: "",
  kind: "run_failed",
  severity: "warning",
  at: now,
});
test("Home attention counts only open urgent and normal session alerts and caps rows at three", async () => {
  const alerts = [
    alert("low", "low"),
    alert("resolved", "urgent", "resolved"),
    alert("a"),
    alert("b", "urgent"),
    alert("c"),
    alert("d"),
  ];
  assert.deepEqual(
    attentionAlerts(alerts).map((a) => a.id),
    ["a", "b", "c", "d"],
  );
  net = stubFetch({ "GET /api/sessions": () => ({ sessions: [session("s_root")], next: "" }) });
  let sessionsLoaded = false;
  render(
    <HomeSessionGlance
      alerts={alerts}
      go={() => {}}
      canWrite={false}
      onChanged={() => {}}
      tick={0}
      onHasSessions={() => {
        sessionsLoaded = true;
      }}
    />,
  );
  await waitFor(() => assert.equal(sessionsLoaded, true));
  assert.equal(screen.getAllByRole("listitem").length, 3);
  assert.equal(screen.getAllByRole("listitem")[0]?.textContent?.includes("Alert b"), true);
  assert.equal(screen.queryByText("Continue"), null);
  assert.ok(screen.getByRole("button", { name: "All alerts" }));
});

test("alert source filters reach the server and do not expose another source", async () => {
  let source: string | null = null;
  net = stubFetch({
    "GET /api/alerts": (_body, query) => {
      source = query.get("source");
      return { alerts: [alert("failure"), { ...alert("eval"), kind: "eval_regression" }] };
    },
  });
  render(
    <AlertsPage
      route={{ page: "alerts", source: "eval_regression" }}
      go={() => {}}
      canWrite={false}
      onChanged={() => {}}
      tick={0}
    />,
  );
  await screen.findByText("Alert eval");
  assert.equal(source, "eval_regression");
  assert.equal(screen.queryByText("Alert failure"), null);
});

test("session rows retain readable project context and age on narrow surfaces", async () => {
  net = stubFetch({
    "GET /api/sessions": () => ({
      sessions: [
        { ...session("s_private"), project: "personal-7e5222" },
        { ...session("s_launch"), project: "launch" },
      ],
      next: "",
    }),
  });
  render(
    <SessionsPage
      route={{ page: "sessions" }}
      go={() => {}}
      projects={[{ name: "launch", title: "Spring launch" } as never]}
      tick={0}
    />,
  );
  const personal = await screen.findByRole("link", { name: /Work s_private/ });
  assert.match(personal.closest("li")?.textContent ?? "", /Personal/);
  assert.doesNotMatch(personal.closest("li")?.textContent ?? "", /personal-7e5222/);
  assert.match(
    screen.getByRole("link", { name: /Work s_launch/ }).closest("li")?.textContent ?? "",
    /Spring launch/,
  );
  assert.ok(personal.closest("li")?.querySelector("time[datetime]"));
});
test("Alerts resolves selected records and Undo reopens only successful changes", async () => {
  let rows = [alert("one"), alert("two", "urgent")];
  const sent: unknown[] = [];
  net = stubFetch({
    "GET /api/alerts": () => ({ alerts: rows }),
    "PATCH /api/alerts/one": (body) => {
      sent.push(body);
      rows = rows.map((a) => (a.id === "one" ? { ...a, ...(body as object) } : a));
      return rows[0];
    },
  });
  const changed: number[] = [];
  render(
    <AlertsPage
      route={{ page: "alerts" }}
      go={() => {}}
      canWrite
      onChanged={() => changed.push(1)}
      tick={0}
    />,
  );
  fireEvent.click(await screen.findByRole("checkbox", { name: "Select Alert one" }));
  fireEvent.click(screen.getByRole("button", { name: "Resolve selected" }));
  fireEvent.click(await screen.findByRole("button", { name: "Undo" }));
  await waitFor(() => assert.deepEqual(sent, [{ state: "resolved" }, { state: "open" }]));
  assert.equal(changed.length, 2);
  assert.ok(net.seen.some((r) => r.path.includes("state=open") && r.path.includes("mine=true")));
});
test("Sessions fetches mine roots and expands children beneath their parent", async () => {
  net = stubFetch({
    "GET /api/sessions": (_body, q) => ({
      sessions: q.get("parent") ? [session("s_child", "s_root")] : [session("s_root")],
      next: "",
    }),
  });
  render(<SessionsPage route={{ page: "sessions" }} go={() => {}} tick={0} />);
  assert.ok(await screen.findByRole("link", { name: /Work s_root/ }));
  assert.equal(screen.queryByRole("link", { name: /Work s_child/ }), null);
  fireEvent.click(screen.getByRole("button", { name: "Show children of Work s_root" }));
  const child = await screen.findByRole("link", { name: /Work s_child/ });
  assert.equal(child.getAttribute("href"), "/sessions/s_child");
  assert.ok(child.closest("ul")?.closest("li")?.textContent?.includes("Work s_root"));
  assert.ok(net.seen.some((r) => r.path.includes("roots=true") && r.path.includes("mine=true")));
});
test("the main navigation has nine destinations, Projects and Scheduled among them, and one alert count", async () => {
  net = stubFetch({
    "GET /api/sessions": () => ({ sessions: [], next: "" }),
    "GET /api/marks": () => ({ archived: [], read: {}, tips: [] }),
  });
  render(
    <Sidebar
      sessionMode
      open
      onToggle={() => {}}
      route={{ page: "new" }}
      go={() => {}}
      me={{
        principal: { subject: "ada", tenant: "acme", kind: "user", name: "Ada", roles: ["admin"] },
        permissions: [],
      }}
      prefs={unreadPrefs}
      can={() => true}
      threads={[]}
      pinned={[]}
      onPin={async () => {}}
      inboxCount={2}
      live
      onSignOut={() => {}}
    />,
  );
  const links = within(screen.getByRole("navigation", { name: "Sections" })).getAllByRole("link");
  assert.deepEqual(
    links.map((a) => a.getAttribute("href")),
    [
      "/",
      "/sessions",
      "/alerts",
      "/projects",
      "/agents",
      "/connections",
      "/scheduled",
      "/artifacts",
      "/settings/profile",
    ],
  );
  assert.equal(links[2]?.getAttribute("aria-label"), "Alerts, 2 need you");
});

test("session card links preserve native modified clicks and intercept only ordinary navigation", () => {
  let opened = 0;
  render(<ItemCard title="Review session" href="/sessions/s_review" onOpen={() => opened++} />);
  const link = screen.getByRole("link", { name: "Review session" });
  assert.equal(link.getAttribute("href"), "/sessions/s_review");
  for (const modifiers of [
    { metaKey: true },
    { ctrlKey: true },
    { shiftKey: true },
    { altKey: true },
    { button: 1 },
  ]) {
    const event = new MouseEvent("click", { bubbles: true, cancelable: true, ...modifiers });
    fireEvent(link, event);
    assert.equal(event.defaultPrevented, false);
    assert.equal(opened, 0);
  }
  const ordinary = new MouseEvent("click", { bubbles: true, cancelable: true, button: 0 });
  fireEvent(link, ordinary);
  assert.equal(ordinary.defaultPrevented, true);
  assert.equal(opened, 1);
});
