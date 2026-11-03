import assert from "node:assert/strict";
import { test } from "node:test";
import type { Marks, ThreadSummary } from "./api.ts";
import {
  age,
  isUnread,
  NO_MARKS,
  partition,
  projectOrder,
  standingOf,
} from "./conversationList.ts";

const t = (id: string, over: Partial<ThreadSummary> = {}): ThreadSummary => ({
  id,
  title: id,
  named: false,
  origin: "api",
  channel: "general",
  agents: [],
  people: [],
  visibility: "shared",
  messages: 0,
  turns: 1,
  branches: 0,
  active: 0,
  failed: 0,
  created_at: "2026-11-01T00:00:00Z",
  updated_at: "2026-11-02T00:00:00Z",
  ...over,
});

const marks = (over: Partial<Marks> = {}): Marks => ({
  ...NO_MARKS,
  since: "2026-11-01T12:00:00Z",
  ...over,
});

test("nothing is unread before the reader's first mark", () => {
  assert.equal(isUnread(t("a", { last_agent: true }), NO_MARKS), false);
});

test("an agent's newer line is unread until the reader opens it", () => {
  const row = t("a", { last_agent: true });
  assert.equal(isUnread(row, marks()), true);
  assert.equal(isUnread(row, marks({ read: { a: "2026-11-03T00:00:00Z" } })), false);
});

test("the reader's own line is never unread, nor one unchanged since the first mark", () => {
  assert.equal(isUnread(t("a", { last_agent: false }), marks()), false);
  assert.equal(
    isUnread(t("a", { last_agent: true, updated_at: "2026-11-01T00:00:00Z" }), marks()),
    false,
  );
});

test("a row's dot puts waiting on a person first, then running, failed and unread", () => {
  const m = marks();
  assert.equal(standingOf(t("a", { active: 1, waiting: 1 }), m), "needs");
  assert.equal(standingOf(t("a", { active: 1 }), m), "running");
  assert.equal(standingOf(t("a", { failed: 1, last_agent: true }), m), "failed");
  assert.equal(standingOf(t("a", { last_agent: true }), m), "unread");
  assert.equal(standingOf(t("a"), m), "");
});

test("an age is the fewest characters", () => {
  const now = Date.parse("2026-11-03T00:00:00Z");
  for (const [iso, want] of [
    ["2026-11-02T23:59:30Z", "now"],
    ["2026-11-02T23:55:00Z", "5m"],
    ["2026-11-02T21:00:00Z", "3h"],
    ["2026-10-31T00:00:00Z", "3d"],
    ["2026-10-13T00:00:00Z", "3w"],
    ["2026-02-03T00:00:00Z", "9mo"],
    ["2024-11-01T00:00:00Z", "2y"],
  ] as const) {
    assert.equal(age(iso, now), want, iso);
  }
});

test("archived are listed only under Archived, pinned only under Pinned", () => {
  const rows = [t("a"), t("b"), t("c"), t("d")];
  const got = partition(rows, ["c", "b"], marks({ archived: ["b", "d", "gone"] }));
  assert.deepEqual(
    got.pinned.map((r) => r.id),
    ["c"],
  );
  assert.deepEqual(
    got.recents.map((r) => r.id),
    ["a"],
  );
  assert.deepEqual(
    got.archived.map((r) => r.id),
    ["b", "d"],
  );
});

test("projects with conversations come first, then the empty ones", () => {
  const rows = [t("a", { project: "Launch" }), t("b"), t("c", { project: "Launch" })];
  assert.deepEqual(projectOrder(rows, ["Ops", "Launch", "~"]), ["Launch", "~", "Ops"]);
});
