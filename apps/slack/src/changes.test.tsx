import assert from "node:assert/strict";
import { test } from "node:test";
import type { Turn } from "@humagen/core";
import { changesTurn } from "@humagen/core/fixtures";
import { CHANNEL, FakeSlack, TEAM, USER } from "./harness.ts";
import { Presenter } from "./present.tsx";

const target = {
  channel: CHANNEL,
  threadTs: "1700000000.000100",
  triggerTs: "1700000000.000100",
  recipientUser: USER,
  recipientTeam: TEAM,
};

type Blocks = { type: string; elements?: { text?: string }[] }[];

/** The changed-files summaries among a call's blocks. */
const summaries = (args: Record<string, unknown> | undefined) =>
  ((args?.blocks ?? []) as Blocks)
    .filter((b) => b.type === "context")
    .map((b) => b.elements?.[0]?.text ?? "")
    .filter((t) => /files? changed\*/.test(t));

const working: Turn = { ...changesTurn, status: "working", durationMs: undefined, text: "" };

test("a streamed turn that changed files ends with their summary in its final blocks", async () => {
  const slack = new FakeSlack();
  const presenter = new Presenter(slack, target);
  await presenter.update(working);
  await presenter.finish(changesTurn);
  const [stop] = slack.of("chat.stopStream");
  assert.deepEqual(summaries(stop), [
    [
      "*3 files changed* +11 -1",
      "`services/payments-api/src/refund.ts` +3 -1",
      "`services/payments-api/migrations/0042_refund_reason.sql` +2",
      "`services/payments-api/src/refund.test.ts` +6",
    ].join("\n"),
  ]);
  assert.deepEqual(slack.of("chat.postMessage"), [], "nothing is posted beside the stream");
});

test("a turn posted where streaming is refused carries the same summary, once it has ended", async () => {
  const slack = new FakeSlack();
  slack.failing["chat.startStream"] = "unknown_method";
  const presenter = new Presenter(slack, target);
  await presenter.update(working);
  assert.deepEqual(summaries(slack.of("chat.postMessage")[0]), [], "not while it works");
  await presenter.finish(changesTurn);
  const [edit] = slack.of("chat.update");
  assert.equal(summaries(edit).length, 1);
  assert.match(summaries(edit)[0] ?? "", /^\*3 files changed\* \+11 -1\n/);
});

test("a turn that changed no file says nothing about files", async () => {
  const slack = new FakeSlack();
  const presenter = new Presenter(slack, target);
  await presenter.finish({ ...changesTurn, tasks: [] });
  assert.deepEqual(summaries(slack.of("chat.postMessage")[0]), []);
  assert.equal(slack.of("chat.postMessage").length, 1);
});

test("a bridge that carries outcomes only names no file", async () => {
  const slack = new FakeSlack();
  const presenter = new Presenter(slack, target, { outcomesOnly: true });
  await presenter.finish(changesTurn);
  const [post] = slack.of("chat.postMessage");
  assert.ok(post, "the outcome is posted");
  assert.deepEqual(summaries(post), []);
});
