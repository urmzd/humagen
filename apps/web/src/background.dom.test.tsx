import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { SessionBackground } from "./Background.tsx";
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

const section = (session = "s-asked", version: unknown = 0) =>
  render(<SessionBackground session={session} version={version} />);

test("a session's Details lists the background work that session carries, running first, and no other session's", async () => {
  const other = {
    ...task,
    id: "bg_9",
    text: "another session's task",
    session: "s-x",
    thread: "s-y",
  };
  const { restore } = stubFetch({
    "GET /api/me/background": () => ({ items: [task, other, watch, goal] }),
  });
  try {
    // The watch was asked for here, the goal runs here; the task ran in a session of its own.
    section("s-asked");
    const list = await screen.findByRole("region", { name: "Background" });
    const rows = within(list).getAllByTestId("details-background");
    assert.deepEqual(
      rows.map((r) => r.dataset.state),
      ["active", "active", "done"],
    );
    assert.ok(within(list).getByText("the release workflow on main passes"));
    assert.ok(
      within(list).getByText(/Watch · lead · Every 15m · Next in 1[12] minutes · Stops in/),
    );
    assert.ok(within(list).getByText(/Goal · lead · Turn 2 of 10/));
    assert.ok(within(list).getByText("Working"));
    assert.ok(within(list).getByText("Done"));
    assert.equal(within(list).queryByText("another session's task"), null);
  } finally {
    restore();
  }
});

test("Stop is on the row of work still running, and only there", async () => {
  const { seen, restore } = stubFetch({
    "GET /api/me/background": () => ({ items: [watch, task] }),
    "POST /api/me/background/bg_1/stop": () => ({
      ...watch,
      state: "stopped",
      ended_at: new Date().toISOString(),
    }),
  });
  try {
    section("s-asked");
    await screen.findByText("the release workflow on main passes");
    assert.equal(screen.queryByRole("button", { name: "Stop: find every caller of Open" }), null);
    fireEvent.click(
      screen.getByRole("button", { name: "Stop: the release workflow on main passes" }),
    );
    await waitFor(() => assert.ok(screen.getByText("Stopped")));
    assert.ok(seen.some((s) => s.method === "POST" && s.path === "/api/me/background/bg_1/stop"));
    assert.equal(screen.queryByRole("button", { name: /^Stop: / }), null);
  } finally {
    restore();
  }
});

test("a session with no background work has no Background section, also when it cannot be read", async () => {
  let fail = true;
  const { restore } = stubFetch({
    "GET /api/me/background": () =>
      fail ? new Response(JSON.stringify({ error: "down" }), { status: 500 }) : { items: [task] },
  });
  try {
    const { container, rerender } = section("s-none", 0);
    await new Promise((done) => setTimeout(done, 20));
    assert.equal(container.textContent, "");
    fail = false;
    rerender(<SessionBackground session="s-none" version={1} />);
    await new Promise((done) => setTimeout(done, 20));
    assert.equal(container.textContent, "", "another session's work is not listed here");
  } finally {
    restore();
  }
});

test("the section is read again as the session's runs start and end, so an ended task stops offering Stop", async () => {
  let ended = false;
  const { restore } = stubFetch({
    "GET /api/me/background": () => ({
      items: [
        ended ? { ...watch, state: "done", result: "Deploy is green", next: undefined } : watch,
      ],
    }),
  });
  try {
    const { rerender } = section("s-asked", 0);
    await screen.findByRole("button", { name: "Stop: the release workflow on main passes" });
    ended = true;
    rerender(<SessionBackground session="s-asked" version={1} />);
    await waitFor(() => assert.ok(screen.getByText("Done")));
    assert.equal(screen.queryByRole("button", { name: /^Stop: / }), null);
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
