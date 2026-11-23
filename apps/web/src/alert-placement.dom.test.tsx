import assert from "node:assert/strict";
import { test } from "node:test";
import { alertReason, groupAlerts, placeAlerts } from "./alertPlacement.ts";
import type { ThreadTurn } from "./api.ts";
import type { Alert } from "./sessionCenter.ts";

const turns = [
  { kind: "message", message: { id: "m-1", at: "2026-11-22T09:00:00Z" } },
  { kind: "run", run: { id: "r-1", created_at: "2026-11-22T09:00:01Z" } },
  { kind: "message", message: { id: "m-2", at: "2026-11-22T09:10:00Z" } },
] as unknown as ThreadTurn[];

const alert = (over: Partial<Alert>): Alert => ({
  id: "a",
  rule: "",
  kind: "eval",
  severity: "warning",
  title: "lead needs review: format",
  state: "open",
  at: "2026-11-22T09:05:00Z",
  ...over,
});

test("an alert about a turn on view sits after it; any other at its own time", () => {
  const placed = placeAlerts(turns, [
    alert({ id: "late", run_id: "r-1", message: "r-1", at: "2026-11-22T09:30:00Z" }),
    alert({ id: "plain", at: "2026-11-22T09:11:00Z" }),
    alert({ id: "child", run_id: "r-child", at: "2026-11-22T09:05:00Z" }),
    alert({ id: "first", at: "2026-11-22T08:00:00Z" }),
    // An offset time is compared as a time, not as text.
    alert({ id: "offset", message: "m-1", at: "2026-11-22T04:00:30.5-05:00" }),
  ]);
  assert.deepEqual(
    placed.lead.map((a) => a.id),
    ["first"],
  );
  assert.deepEqual(
    [...placed.after].map(([turn, alerts]) => [turn, alerts.map((a) => a.id)]),
    [
      ["m-1", ["offset"]],
      ["r-1", ["child", "late"]],
      ["m-2", ["plain"]],
    ],
  );
});

test("with earlier turns still unread, an alert older than every turn, or about a turn not loaded, is listed as earlier", () => {
  const alerts = [
    alert({ id: "open-old", at: "2026-11-22T08:00:00Z" }),
    alert({ id: "done-old", state: "resolved", at: "2026-11-22T08:00:00Z" }),
    // About a turn on an unread page: never placed by time among turns it is not about.
    alert({ id: "unloaded", message: "m-0", at: "2026-11-22T09:20:00Z" }),
    alert({ id: "late-plain", at: "2026-11-22T09:20:00Z" }),
  ];
  const unread = placeAlerts(turns, alerts, true);
  assert.deepEqual(
    unread.earlier.map((a) => a.id),
    ["open-old", "unloaded"],
  );
  assert.deepEqual(unread.lead, []);
  // About a run whose caller is not known: by time, once newer than the oldest loaded turn.
  const child = alert({
    id: "child",
    run_id: "r-child",
    message: "r-child",
    at: "2026-11-22T09:20:00Z",
  });
  assert.deepEqual(
    placeAlerts(turns, [child], true)
      .after.get("m-2")
      ?.map((a) => a.id),
    ["child"],
  );
  // A run known to be a turn not loaded is listed as earlier.
  assert.deepEqual(
    placeAlerts(turns, [child], true, new Map([["r-child", "r-child"]])).earlier.map((a) => a.id),
    ["child"],
  );
  assert.deepEqual(
    unread.after.get("m-2")?.map((a) => a.id),
    ["late-plain"],
  );
  // Once every turn is read, the start of the session is the start.
  const all = placeAlerts(turns, alerts, false);
  assert.deepEqual(
    all.lead.map((a) => a.id),
    ["open-old", "done-old"],
  );
  assert.deepEqual(all.earlier, []);
});

test("an alert about a run another turn called sits after that turn, when the turn is on view", () => {
  const child = alert({ id: "child", run_id: "r-child", at: "2026-11-22T09:20:00Z" });
  assert.deepEqual(
    placeAlerts(turns, [child], false, new Map([["r-child", "r-1"]]))
      .after.get("r-1")
      ?.map((a) => a.id),
    ["child"],
  );
  // A caller not on view does not place it.
  assert.deepEqual(
    placeAlerts(turns, [child], false, new Map([["r-child", "r-9"]]))
      .after.get("m-2")
      ?.map((a) => a.id),
    ["child"],
  );
});

test("the same alert on one answer is one group, newest first; a different state or answer is not", () => {
  const groups = groupAlerts([
    alert({ id: "b", agent: "lead", run_id: "r-1", at: "2026-11-22T09:06:00Z" }),
    alert({ id: "a", agent: "lead", run_id: "r-1", at: "2026-11-22T09:05:00Z" }),
    alert({ id: "c", agent: "lead", run_id: "r-2", at: "2026-11-22T09:07:00Z" }),
    alert({ id: "d", agent: "lead", run_id: "r-1", state: "resolved", at: "2026-11-22T09:08:00Z" }),
  ]);
  assert.deepEqual(
    groups.map((g) => [g.alerts.map((a) => a.id), g.lead.id, g.open]),
    [
      [["b", "a"], "b", true],
      [["c"], "c", true],
      [["d"], "d", false],
    ],
  );
});

test("the reason drops the agent's handle that leads the title", () => {
  assert.equal(
    alertReason({ title: "github-reviewer needs review: format", agent: "github-reviewer" }),
    "Needs review: format",
  );
  assert.equal(
    alertReason({ title: "github-reviewer failed", agent: "github-reviewer" }),
    "Failed",
  );
  assert.equal(alertReason({ title: "Confirm the owner" }), "Confirm the owner");
});
