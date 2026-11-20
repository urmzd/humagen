import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { Automations } from "./Automations.tsx";
import { stubFetch } from "./dom.ts";

afterEach(cleanup);

const soon = new Date(Date.now() + 12 * 60_000).toISOString();
const later = new Date(Date.now() + 20 * 3600_000).toISOString();
const watch = {
  id: "bg_1",
  kind: "watch",
  text: "the release workflow on main passes",
  how: "read the workflow status",
  state: "active",
  agent: "lead",
  project: "Field",
  every: "15m0s",
  until: later,
  next: soon,
  working: false,
  runs: 3,
  limit: 200,
  session: "s-asked",
  thread: "s-checks",
  created_at: "2026-11-20T09:00:00Z",
};
const goal = {
  ...watch,
  id: "bg_2",
  kind: "goal",
  text: "every test in ./pkg passes",
  how: "go test ./pkg/... passes",
  every: undefined,
  next: undefined,
  working: true,
  runs: 2,
  limit: 10,
  thread: "s-asked",
};
const task = {
  ...watch,
  id: "bg_3",
  kind: "task",
  text: "find every caller of Open",
  how: undefined,
  state: "done",
  every: undefined,
  next: undefined,
  runs: 1,
  limit: 1,
  thread: "s-task",
  result: "Found 12 callers\nIn 4 packages.",
  ended_at: "2026-11-20T09:30:00Z",
};

const page = (onConversation: (id: string) => void = () => {}) =>
  render(
    <Automations
      schedules={[]}
      agents={[]}
      projects={[]}
      can={() => true}
      onChanged={() => {}}
      onSchedule={() => {}}
      tab="background"
      onTab={() => {}}
      onConversation={onConversation}
    />,
  );

test("Background lists watches, goals and tasks under the Scheduled tabs, and opens where each runs", async () => {
  const { restore } = stubFetch({
    "GET /api/me/background": () => ({ items: [watch, goal, task] }),
  });
  const opened: string[] = [];
  try {
    page((id) => opened.push(id));
    assert.ok(screen.getByRole("tab", { name: "Background", selected: true }));
    assert.ok(screen.getByRole("tab", { name: "Schedules", selected: false }));
    assert.equal(screen.queryByRole("button", { name: "New Schedule" }), null);
    await waitFor(() => assert.ok(screen.getByText("the release workflow on main passes")));
    assert.ok(screen.getByText("Every 15m"));
    assert.equal(screen.getAllByText(/^Stops in (19|20) hours$/).length, 2);
    assert.ok(screen.getByText(/^Next in 1[12] minutes$/));
    assert.ok(screen.getByText("Turn 2 of 10"));
    assert.ok(screen.getByText("Working"));
    assert.ok(screen.getByText("Found 12 callers"));
    assert.ok(screen.getByText("Done"));
    fireEvent.click(screen.getByText("find every caller of Open"));
    assert.deepEqual(opened, ["s-task"]);
  } finally {
    restore();
  }
});

test("Stop ends one still running, and only those have a menu", async () => {
  const { seen, restore } = stubFetch({
    "GET /api/me/background": () => ({ items: [watch, task] }),
    "POST /api/me/background/bg_1/stop": () => ({
      ...watch,
      state: "stopped",
      ended_at: new Date().toISOString(),
    }),
  });
  try {
    page();
    await waitFor(() => assert.ok(screen.getByText("the release workflow on main passes")));
    assert.equal(
      screen.queryByRole("button", { name: "More for find every caller of Open" }),
      null,
    );
    fireEvent.click(
      screen.getByRole("button", { name: "More for the release workflow on main passes" }),
    );
    fireEvent.click(await screen.findByRole("menuitem", { name: "Stop" }));
    await waitFor(() => assert.ok(screen.getByText("Stopped")));
    assert.ok(seen.some((s) => s.method === "POST" && s.path === "/api/me/background/bg_1/stop"));
  } finally {
    restore();
  }
});

test("Background says when there is none, and offers Retry when it cannot be read", async () => {
  let fail = true;
  const { restore } = stubFetch({
    "GET /api/me/background": () =>
      fail ? new Response(JSON.stringify({ error: "down" }), { status: 500 }) : { items: [] },
  });
  try {
    page();
    await waitFor(() => assert.ok(screen.getByText("Background work could not be loaded")));
    fail = false;
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await waitFor(() => assert.ok(screen.getByText("No background work")));
  } finally {
    restore();
  }
});

test("durations read as a person writes them", async () => {
  const { span } = await import("./Background.tsx");
  assert.equal(span("15m0s"), "15m");
  assert.equal(span("1h0m0s"), "1h");
  assert.equal(span("1h30m0s"), "1h30m");
  assert.equal(span("24h0m0s"), "24h");
});
