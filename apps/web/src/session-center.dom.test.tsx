import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { ItemCard } from "./cards.tsx";
import { stubFetch } from "./dom.ts";
import { unreadPrefs } from "./preferences.ts";
import { href } from "./route.ts";
import {
  AlertsPage,
  HomeSessionGlance,
  SessionsPage,
  useSessionAttention,
} from "./SessionCenter.tsx";
import { Sidebar } from "./Sidebar.tsx";
import type { AgentSessionSummary, Alert } from "./sessionCenter.ts";
import { sampleFiles } from "./storage.fixtures.ts";

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
test("Home does not repeat the alert count: the menu's Alerts entry carries it", async () => {
  net = stubFetch({ "GET /api/sessions": () => ({ sessions: [session("s_root")], next: "" }) });
  let sessionsLoaded = false;
  const { container } = render(
    <HomeSessionGlance
      go={() => {}}
      tick={0}
      via={{ list: async () => ({ artifacts: [], next: null }) }}
      onHasSessions={() => {
        sessionsLoaded = true;
      }}
    />,
  );
  await waitFor(() => assert.equal(sessionsLoaded, true));
  assert.equal(screen.queryByText(/waiting on you/), null);
  assert.equal(screen.queryByRole("link"), null);
  assert.equal(container.textContent, "");
});

test("the badge's count comes from the count alone, before the alerts are read", async () => {
  // The alerts never answer here: the badge's number still shows.
  net = stubFetch({
    "GET /api/alerts/count": () => ({ waiting: 4 }),
    "GET /api/alerts": () => new Promise(() => {}),
    "GET /api/sessions": () => ({ sessions: [], next: "" }),
  });
  function Badge() {
    const attention = useSessionAttention(true, null);
    return <output aria-label="badge">{attention.count}</output>;
  }
  render(<Badge />);
  await waitFor(() => assert.equal(screen.getByLabelText("badge").textContent, "4"));
  await waitFor(() => assert.ok(net?.seen.some((r) => r.path.startsWith("/api/alerts?"))));
  const asked = net.seen.map((r) => r.path);
  assert.ok(asked.includes("/api/alerts/count"), `asked ${asked.join(", ")}`);
  // The count is asked for before the alerts, so it is first in line for a connection.
  assert.ok(asked.indexOf("/api/alerts/count") < asked.indexOf("/api/alerts?mine=true&state=open"));
});

test("once the alerts are read the count is theirs, and the count alone is not asked for again", async () => {
  let listed = 0;
  net = stubFetch({
    "GET /api/alerts/count": () => ({ waiting: 9 }),
    "GET /api/alerts": () => {
      listed++;
      return { alerts: [alert("a"), alert("b", "urgent"), alert("low", "low")] };
    },
  });
  let again = () => {};
  function Badge() {
    const attention = useSessionAttention(true, null);
    again = attention.refresh;
    return <output aria-label="badge">{attention.count}</output>;
  }
  render(<Badge />);
  await waitFor(() => assert.equal(screen.getByLabelText("badge").textContent, "2"));
  again();
  await waitFor(() => assert.equal(listed, 2));
  assert.equal(screen.getByLabelText("badge").textContent, "2");
  assert.equal(net.seen.filter((r) => r.path === "/api/alerts/count").length, 1);
});

test("Home shows the three newest artifacts in one strip, each opening where it lives", async () => {
  net = stubFetch({ "GET /api/sessions": () => ({ sessions: [], next: "" }) });
  const asked: number[] = [];
  const went: unknown[] = [];
  render(
    <HomeSessionGlance
      go={(route) => went.push(route)}
      tick={0}
      via={{
        list: async (q) => {
          asked.push(q.limit);
          return { artifacts: sampleFiles().slice(0, 4), next: null };
        },
      }}
    />,
  );
  const strip = await screen.findByRole("region", { name: "Recently made" });
  assert.deepEqual(asked, [3]);
  const cards = within(strip).getAllByRole("listitem");
  assert.equal(cards.length, 3);
  const first = sampleFiles()[0];
  assert.ok(first);
  const card = within(strip).getByRole("link", { name: first.title });
  assert.equal(card.getAttribute("href"), href({ page: "artifacts", artifact: first.id }));
  fireEvent.click(card);
  assert.deepEqual(went, [{ page: "artifacts", artifact: first.id }]);
  // The menu's Artifacts entry opens the full list, so the strip does not link to it again.
  assert.equal(within(strip).queryByRole("link", { name: "All artifacts" }), null);
});

test("Home says in one line when its recent artifacts could not be read", async () => {
  net = stubFetch({ "GET /api/sessions": () => ({ sessions: [], next: "" }) });
  render(
    <HomeSessionGlance
      go={() => {}}
      tick={0}
      via={{
        list: async () => {
          throw new Error("offline");
        },
      }}
    />,
  );
  assert.ok(await screen.findByText("Recent artifacts could not be loaded."));
  assert.equal(screen.queryByRole("region", { name: "Recently made" }), null);
});

test("Home with nothing made draws nothing, and asks for no artifacts it may not read", async () => {
  net = stubFetch({ "GET /api/sessions": () => ({ sessions: [], next: "" }) });
  let asked = 0;
  const { container } = render(
    <HomeSessionGlance
      go={() => {}}
      tick={0}
      canArtifacts={false}
      via={{
        list: async () => {
          asked++;
          return { artifacts: sampleFiles(), next: null };
        },
      }}
    />,
  );
  await new Promise((done) => setTimeout(done, 0));
  assert.equal(asked, 0);
  assert.equal(container.textContent, "");
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
test("Alerts opens the decision for review", async () => {
  net = stubFetch({
    "GET /api/alerts": () => ({ alerts: [{ ...alert("approval"), action: "approve" }] }),
  });
  const went: unknown[] = [];
  render(
    <AlertsPage
      route={{ page: "alerts" }}
      go={(route) => went.push(route)}
      canWrite
      onChanged={() => {}}
      tick={0}
    />,
  );
  fireEvent.click(await screen.findByRole("button", { name: "Review Approval" }));
  assert.deepEqual(went, [{ page: "conversation", id: "s_root", message: "msg1" }]);
  assert.equal(
    net.seen.some(({ method }) => method === "PATCH"),
    false,
  );
});

test("an alert title takes two lines in a stacked row and one in the table", async () => {
  const title = "Spend for launch is a lower bound: some model calls have no price in US dollars";
  net = stubFetch({ "GET /api/alerts": () => ({ alerts: [{ ...alert("long"), title }] }) });
  render(
    <AlertsPage route={{ page: "alerts" }} go={() => {}} canWrite onChanged={() => {}} tick={0} />,
  );
  // A touch screen shows no tooltip, so a stacked row must show the second line itself.
  const link = await screen.findByRole("link", { name: title });
  const classes = link.firstElementChild?.className.split(/\s+/) ?? [];
  assert.ok(classes.includes("line-clamp-2"), `two lines when stacked: ${classes.join(" ")}`);
  assert.ok(
    classes.includes("@3xl:pointer-fine:line-clamp-1"),
    `one line in the table: ${classes.join(" ")}`,
  );
  assert.ok(!classes.includes("truncate"), `not one line everywhere: ${classes.join(" ")}`);
  // A tooltip does not show on keyboard focus, so the table's one line opens while its link has it.
  assert.ok(
    classes.includes("@3xl:pointer-fine:group-focus-visible/title:line-clamp-none"),
    `whole on keyboard focus in the table: ${classes.join(" ")}`,
  );
  assert.ok(
    link.className.split(/\s+/).includes("group/title"),
    "the link is what takes the focus",
  );
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
  fireEvent.click(screen.getByRole("button", { name: "Mark Selected Done" }));
  fireEvent.click(await screen.findByRole("button", { name: "Undo" }));
  await waitFor(() => assert.deepEqual(sent, [{ state: "resolved" }, { state: "open" }]));
  assert.equal(changed.length, 2);
  assert.ok(net.seen.some((r) => r.path.includes("state=open") && r.path.includes("mine=true")));
});
test("Alerts has one box that selects every row, and the bulk actions show only once a row is selected", async () => {
  const rows = [alert("one"), alert("two", "urgent")];
  const sent: string[] = [];
  net = stubFetch({
    "GET /api/alerts": () => ({ alerts: rows }),
    "PATCH /api/alerts/one": () => {
      sent.push("one");
      return rows[0];
    },
    "PATCH /api/alerts/two": () => {
      sent.push("two");
      return rows[1];
    },
  });
  render(
    <AlertsPage route={{ page: "alerts" }} go={() => {}} canWrite onChanged={() => {}} tick={0} />,
  );
  const all = (await screen.findByRole("checkbox", {
    name: "Select all alerts",
  })) as HTMLInputElement;
  assert.equal(screen.queryByRole("button", { name: "Mark Selected Done" }), null);
  // One row selected: the header box is neither on nor off.
  fireEvent.click(screen.getByRole("checkbox", { name: "Select Alert one" }));
  assert.equal(all.indeterminate, true);
  assert.ok(screen.getByText("1 selected"));
  fireEvent.click(all);
  assert.ok(screen.getByText("2 selected"));
  assert.equal(
    (screen.getByRole("checkbox", { name: "Select Alert two" }) as HTMLInputElement).checked,
    true,
  );
  fireEvent.click(screen.getByRole("button", { name: "Mark Selected Done" }));
  await waitFor(() => assert.deepEqual(sent.sort(), ["one", "two"]));
});
test("Sessions fetches mine roots, one card each, with nothing listed beneath a session", async () => {
  net = stubFetch({
    "GET /api/sessions": () => ({ sessions: [session("s_root")], next: "" }),
  });
  render(<SessionsPage route={{ page: "sessions" }} go={() => {}} tick={0} />);
  const card = await screen.findByRole("link", { name: /Work s_root/ });
  assert.equal(card.getAttribute("href"), "/messages/s_root");
  // What an agent did is inside the session, under its response: no second level here.
  assert.equal(screen.queryByRole("button", { name: /child/i }), null);
  assert.equal(screen.queryByText(/child sessions/i), null);
  assert.ok(net.seen.some((r) => r.path.includes("roots=true") && r.path.includes("mine=true")));
  assert.ok(!net.seen.some((r) => r.path.includes("parent=")));
});
test("the main navigation has seven destinations, Projects and Scheduled among them, and one alert count", async () => {
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
    ["/", "/sessions", "/alerts", "/projects", "/agents", "/scheduled", "/artifacts"],
  );
  // The open sidebar names the link by the label and count it shows.
  assert.equal(
    within(screen.getByRole("navigation", { name: "Sections" })).getByRole("link", {
      name: "Alerts 2 need you",
    }),
    links[2],
  );
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
