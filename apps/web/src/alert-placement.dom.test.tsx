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

test("with earlier turns still unread, a settled alert older than every turn waits for them", () => {
  const old = [
    alert({ id: "open-old", at: "2026-11-22T08:00:00Z" }),
    alert({ id: "done-old", state: "resolved", at: "2026-11-22T08:00:00Z" }),
  ];
  assert.deepEqual(
    placeAlerts(turns, old, true).lead.map((a) => a.id),
    ["open-old"],
  );
  assert.deepEqual(
    placeAlerts(turns, old, false).lead.map((a) => a.id),
    ["open-old", "done-old"],
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
