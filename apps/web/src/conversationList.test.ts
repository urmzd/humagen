import assert from "node:assert/strict";
import { test } from "node:test";
import type { Marks, ThreadSummary } from "./api.ts";
import {
  age,
  deriveTitle,
  isUnread,
  NO_MARKS,
  partition,
  placeholderTitle,
  projectOrder,
  rowState,
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

test("an unnamed conversation is titled from its first message, as the server titles it", () => {
  const cases: [string, string][] = [
    [
      "@code-reviewer Review the pull request urmzd/humagen#210",
      "Review the pull request urmzd/humagen#210",
    ],
    ["@a @b  fix the build\nmore detail", "Fix the build"],
    ["@code-reviewer", "@code-reviewer"],
    ["   ", ""],
    ["draft the launch checklist:", "Draft the launch checklist"],
    [
      "summarize every incident from last quarter and group them by service owner please",
      "Summarize every incident from last quarter and group them…",
    ],
  ];
  for (const [input, want] of cases) assert.equal(deriveTitle(input), want, input);
});

test("a session's placeholder title is cut on a word under 40 characters, as the server cuts it", () => {
  const cases: [string, string][] = [
    [
      "Write four short sentences about the sea, the tide and the boats",
      "Write four short sentences about the…",
    ],
    ["@lead check the build", "Check the build"],
    ["hi", "Hi"],
  ];
  for (const [input, want] of cases) {
    assert.equal(placeholderTitle(input), want, input);
    assert.ok([...placeholderTitle(input)].length <= 40, input);
  }
});

test("a row badge prioritizes waiting, failure, work and open alerts", async () => {
  const words = {
    waitingYou: (what: string, kind?: string) => `you:${kind ?? ""}:${what}`,
    waitingOther: (what: string, kind?: string) => `other:${kind ?? ""}:${what}`,
    waitingUnknown: (what: string, kind?: string) => `unknown:${kind ?? ""}:${what}`,
    failed: (reason: string) => `failed:${reason}`,
    working: "working",
    alert: (title: string) => `alert:${title}`,
  };
  const since = "2026-01-01T00:00:00Z";
  const m: Marks = { archived: [], read: {}, tips: [], since };
  const state = (over: Partial<ThreadSummary>) => rowState(t("a", over), words, { session: true });
  assert.deepEqual(
    state({
      active: 1,
      waiting: 1,
      waiting_for: "deploy",
      waiting_kind: "approval",
      waiting_on_you: true,
      failed: 1,
    }),
    { kind: "approval", tone: "warn", text: "you:approval:deploy" },
  );
  // Only a listing that says the reader is asked makes it theirs: a missing
  // answer is no one's, never the reader's by default.
  assert.deepEqual(
    state({ active: 1, waiting: 1, waiting_for: "deploy", waiting_kind: "approval" }),
    { kind: "approval", tone: "neutral", text: "other:approval:deploy" },
  );
  assert.deepEqual(
    state({ active: 1, waiting: 1, waiting_kind: "question", waiting_on_you: false }),
    { kind: "question", tone: "neutral", text: "other:question:" },
  );
  // A listing that names nothing waited on and no one: it waits on someone.
  assert.deepEqual(state({ active: 1, waiting: 1, waiting_on_you: false }), {
    kind: "waiting",
    tone: "neutral",
    text: "other::",
  });
  // The listing could not tell whom it waits on: it is said to wait, in the
  // neutral tone, and never on someone else or on the reader.
  assert.deepEqual(
    state({
      active: 1,
      waiting: 1,
      waiting_for: "deploy",
      waiting_kind: "approval",
      waiting_on_you: false,
      waiting_unknown: true,
    }),
    { kind: "approval", tone: "neutral", text: "unknown:approval:deploy" },
  );
  assert.deepEqual(state({ waiting: 1, waiting_on_you: false, waiting_unknown: true }), {
    kind: "waiting",
    tone: "neutral",
    text: "unknown::",
  });
  // Told it is the reader's, that is what is said, whatever else is set.
  assert.deepEqual(
    state({ waiting: 1, waiting_for: "deploy", waiting_on_you: true, waiting_unknown: true }),
    { kind: "waiting", tone: "warn", text: "you::deploy" },
  );
  const said = (await import("@humagen/components/copy")).defaultCopy.sidebar.state;
  assert.equal(said.waitingUnknown("file_write", "approval"), "Waiting: approve file_write");
  assert.equal(said.waitingUnknown("Which region?", "question"), "Waiting: Which region?");
  assert.equal(said.waitingUnknown(""), "Waiting");
  for (const text of [said.waitingUnknown("x", "approval"), said.waitingUnknown("")])
    assert.doesNotMatch(text, /you|someone|answer|approval:/i);
  assert.deepEqual(state({ active: 1, failed: 1, failure: "boom" }), {
    kind: "failed",
    tone: "bad",
    text: "failed:boom",
  });
  // An unread failure is the failed badge on a row whose title reads unread.
  const unreadFailure = t("a", { failed: 1, failure: "boom", last_agent: true });
  assert.deepEqual(rowState(unreadFailure, words, { session: true }), {
    kind: "failed",
    tone: "bad",
    text: "failed:boom",
  });
  assert.equal(isUnread(unreadFailure, m), true);
  // A session listing reports no failure once a later turn answered.
  assert.equal(state({ failed: 1 }), undefined);
  // Other listings only count failures.
  assert.deepEqual(rowState(t("a", { failed: 1 }), words), {
    kind: "failed",
    tone: "bad",
    text: "failed:",
  });
  assert.deepEqual(state({ active: 1 }), { kind: "working", tone: "neutral", text: "working" });
  assert.deepEqual(rowState(t("a"), words, { alert: { priority: "urgent", title: "Hi" } }), {
    kind: "alert",
    tone: "warn",
    text: "alert:Hi",
  });
  // Nothing to say of its state: only the unread dot is left to show.
  assert.equal(state({ last_agent: true }), undefined);
  assert.equal(isUnread(t("a", { last_agent: true }), m), true);
});
