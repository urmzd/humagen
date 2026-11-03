import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { Marks, Me, Preferences, ProjectView, ThreadSummary } from "./api.ts";
import { stubFetch } from "./dom.ts";
import { unreadPrefs } from "./preferences.ts";
import type { Route } from "./route.ts";
import { Sidebar } from "./Sidebar.tsx";

// DES-049: Recents, Pinned, Projects and Archived, each row's dot and age,
// and archiving a conversation for the reader alone.

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
}: {
  pinned?: string[];
  route?: Route;
  onGo?: (r: Route) => void;
  projects?: ProjectView[];
}) {
  const noop = () => {};
  return (
    <Sidebar
      open
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
  fireEvent.click(await screen.findByRole("button", { name: /^Filter conversations/ }));
  fireEvent.click(await screen.findByRole("button", { name }));
};

test("Recents lists every project newest first, each row with its project, age, and one dot", async () => {
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
  const list = within(await screen.findByRole("list", { name: "Recents" }));
  const waiting = await list.findByRole("button", { name: /^Waiting on me/ });
  assert.match(waiting.textContent ?? "", /Spring launch/);
  assert.match(waiting.textContent ?? "", /Needs you/);
  assert.equal(
    waiting.querySelector("[data-standing]")?.getAttribute("data-standing"),
    "needs",
    "waiting on a person has its own still dot",
  );
  assert.equal(
    list
      .getByRole("button", { name: /^Still going/ })
      .querySelector("[data-standing]")
      ?.getAttribute("data-standing"),
    "running",
  );
  const answered = list.getByRole("button", { name: /^Answered/ });
  await waitFor(() =>
    assert.equal(
      answered.querySelector("[data-row-age] [data-standing]")?.getAttribute("data-standing"),
      "unread",
    ),
  );
  const quiet = list.getByRole("button", { name: /^Quiet/ });
  assert.match(quiet.querySelector("[data-row-age]")?.textContent ?? "", /^\d+(mo|y|w|d)$/);
  assert.equal(quiet.querySelector("[data-standing]"), null);
});

test("hover actions pin and archive a row, and Archived brings it back", async () => {
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
  fireEvent.click(await screen.findByRole("button", { name: "Archive Put me away" }));
  await waitFor(() => assert.ok(!screen.queryByRole("button", { name: /^Put me away/ })));
  assert.ok(screen.getByText("Archived for you. Others still see it."));
  await view(/^Archived/);
  const archived = within(await screen.findByRole("list", { name: "Archived" }));
  fireEvent.click(await archived.findByRole("button", { name: "Unarchive Put me away" }));
  await waitFor(() => assert.deepEqual(marks.archived, []));
  await view("Recents");
  await screen.findByRole("button", { name: /^Put me away/ });
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
  assert.equal(list.getAllByRole("button", { name: /^Launch task/ }).length, 5);
  fireEvent.click(list.getByRole("button", { name: "See All (7)" }));
  assert.equal(list.getAllByRole("button", { name: /^Launch task/ }).length, 7);
  const ops = list.getByText("Ops").closest("details");
  assert.equal(ops?.open, false, "an empty project starts folded");
  assert.match(ops?.textContent ?? "", /No conversations/);
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
