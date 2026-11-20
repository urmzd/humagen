import assert from "node:assert/strict";
import { test } from "node:test";
import type { Turn } from "@humagen/core";
import { doneTurn, streamingTurn, waitingTurn } from "@humagen/core/fixtures";
import { STATUS_TEXT_CHARS } from "@humagen/render-slack";
import { CHANNEL, FakeSlack, TEAM, USER } from "./harness.ts";
import { Presenter, STATUS_LINE_EVERY_MS } from "./present.tsx";

const THREAD = "1700000000.000100";
const target = {
  channel: CHANNEL,
  threadTs: THREAD,
  triggerTs: THREAD,
  recipientUser: USER,
  recipientTeam: TEAM,
};

/** A turn still working on one task with this title. */
const doing = (title: string): Turn => ({
  ...streamingTurn,
  tasks: [{ id: `t-${title}`, title, status: "in_progress" }],
});

/** A presenter on a clock the test moves. */
function presenter(slack: FakeSlack, opts: { statusEvery?: number } = {}) {
  const clock = { at: 0 };
  const p = new Presenter(slack, target, { now: () => clock.at, ...opts });
  return { p, clock };
}

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

test("the status line says what the run is doing, in the thread it answers in", async () => {
  const slack = new FakeSlack();
  const { p } = presenter(slack);
  await p.update(doing("Checking recent deploys"));
  assert.deepEqual(slack.of("assistant.threads.setStatus"), [
    { channel_id: CHANNEL, thread_ts: THREAD, status: "Checking recent deploys" },
  ]);
  // It follows the session status, which Slack is told first.
  const m = slack.methods;
  assert.ok(m.indexOf("agents.sessions.setStatus") < m.indexOf("assistant.threads.setStatus"));
});

test("the status line is sent only when it changes, and at most once every few seconds", async () => {
  const slack = new FakeSlack();
  const { p, clock } = presenter(slack);
  await p.update(doing("Checking recent deploys"));
  clock.at = 500;
  await p.update(doing("Checking recent deploys"));
  assert.deepEqual(slack.lines, ["Checking recent deploys"], "the same line is not sent again");
  clock.at = 1000;
  await p.update(doing("Reading the runbook"));
  clock.at = 2000;
  await p.update(doing("Testing Plaud alerts"));
  assert.deepEqual(slack.lines, ["Checking recent deploys"], "too soon for another");
  clock.at = STATUS_LINE_EVERY_MS;
  await p.update(doing("Testing Plaud alerts"));
  assert.deepEqual(
    slack.lines,
    ["Checking recent deploys", "Testing Plaud alerts"],
    "the latest line, once the time is up; the one in between is skipped",
  );
  await p.finish(doneTurn);
});

test("a line held back by the throttle is still shown when no update follows", async () => {
  const slack = new FakeSlack();
  const { p, clock } = presenter(slack, { statusEvery: 30 });
  await p.update(doing("Checking recent deploys"));
  clock.at = 10;
  await p.update(doing("Testing Plaud alerts"));
  assert.deepEqual(slack.lines, ["Checking recent deploys"]);
  await sleep(80);
  assert.deepEqual(slack.lines, ["Checking recent deploys", "Testing Plaud alerts"]);
  await p.finish(doneTurn);
});

test("the status line is cleared at once when the run ends, and a held line never follows", async () => {
  const slack = new FakeSlack();
  const { p, clock } = presenter(slack, { statusEvery: 30 });
  await p.update(doing("Checking recent deploys"));
  clock.at = 10;
  await p.update(doing("Testing Plaud alerts"));
  await p.finish(doneTurn);
  await sleep(80);
  assert.deepEqual(slack.lines, ["Checking recent deploys", ""]);
  // Cleared before the session is set back to active.
  const m = slack.methods;
  assert.ok(
    m.lastIndexOf("assistant.threads.setStatus") < m.lastIndexOf("agents.sessions.setStatus"),
  );
});

test("the status line is cleared when the run parks on a person, and comes back when it resumes", async () => {
  const slack = new FakeSlack();
  const { p, clock } = presenter(slack);
  await p.update(doing("Checking recent deploys"));
  clock.at = 100;
  await p.pause(waitingTurn);
  assert.deepEqual(slack.lines, ["Checking recent deploys", ""]);
  clock.at = 200;
  await p.update(doing("Rolling back the deploy"));
  assert.deepEqual(slack.lines, ["Checking recent deploys", "", "Rolling back the deploy"]);
  await p.finish(doneTurn);
});

test("a run that never showed a line clears nothing", async () => {
  const slack = new FakeSlack();
  const { p } = presenter(slack);
  await p.finish(doneTurn);
  assert.deepEqual(slack.lines, []);
});

test("a long status line is cut to one short line", async () => {
  const slack = new FakeSlack();
  const { p } = presenter(slack);
  await p.update(doing(`Reading ${"every incident in the payments timeline ".repeat(5)}`));
  const [line = ""] = slack.lines;
  assert.ok(line.length <= STATUS_TEXT_CHARS);
  assert.ok(line.endsWith("…"));
  await p.finish(doneTurn);
});

test("a thread that refuses a status line is not asked again, and the answer goes on", async () => {
  const slack = new FakeSlack();
  slack.failing["assistant.threads.setStatus"] = "invalid_thread";
  const logged: string[] = [];
  const clock = { at: 0 };
  const p = new Presenter(slack, target, { now: () => clock.at, log: (l) => logged.push(l) });
  await p.update(doing("Checking recent deploys"));
  clock.at = 10_000;
  await p.update(doing("Testing Plaud alerts"));
  await p.finish(doneTurn);
  assert.equal(slack.of("assistant.threads.setStatus").length, 1);
  assert.ok(logged.some((l) => l.startsWith("status line skipped:")));
  assert.deepEqual(slack.statuses, ["processing", "active"]);
  assert.equal(slack.of("chat.stopStream").length, 1);
});

test("a code channel's session, which has no thread, gets no status line", async () => {
  const slack = new FakeSlack();
  slack.failing["agents.sessions.setStatus"] = (args) =>
    args.thread_ts ? "thread_ts_not_allowed" : undefined;
  const { p } = presenter(slack);
  await p.update(doing("Checking recent deploys"));
  await p.finish(doneTurn);
  assert.deepEqual(slack.lines, []);
});

test("a bridged outcome shows no status line", async () => {
  const slack = new FakeSlack();
  const p = new Presenter(slack, target, { outcomesOnly: true });
  await p.update(doing("Checking recent deploys"));
  await p.finish(doneTurn);
  assert.deepEqual(slack.lines, []);
});
