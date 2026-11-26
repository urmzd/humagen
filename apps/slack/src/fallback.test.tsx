import assert from "node:assert/strict";
import { test } from "node:test";
import { ApprovalCard, NoticeView } from "@humagen/components";
import type { Turn } from "@humagen/core";
import {
  doneTurn,
  pendingApproval,
  pendingClarification,
  waitingTurn,
} from "@humagen/core/fixtures";
import { renderToSlack } from "@humagen/render-slack";
import { withBlockFallback } from "./fallback.ts";
import { CHANNEL, FakeSlack, TEAM, USER } from "./harness.ts";
import { Presenter } from "./present.tsx";

const THREAD = "1700000000.000100";
const target = {
  channel: CHANNEL,
  threadTs: THREAD,
  triggerTs: THREAD,
  recipientUser: USER,
  recipientTeam: TEAM,
};

type Block = { type: string; block_id?: string; elements?: unknown[] };
const NATIVE = new Set(["card", "carousel", "container"]);
const types = (args: Record<string, unknown>) =>
  ((args.blocks as Block[] | undefined) ?? []).map((b) => b.type);
const hasNative = (args: Record<string, unknown>) => types(args).some((t) => NATIVE.has(t));

/** A turn posted, not streamed, as it is where streaming is not available. */
async function post(slack: FakeSlack, turn: Turn) {
  const p = new Presenter(withBlockFallback(slack), target, {});
  // A refused stream start falls back to posting and editing.
  slack.failing["chat.startStream"] = "not_allowed_token_type";
  await p.update(turn);
  return p;
}

test("a turn that waits on a person is posted with Slack's own card", async () => {
  const slack = new FakeSlack();
  await post(slack, waitingTurn);
  const [posted] = slack.of("chat.postMessage");
  assert.ok(posted);
  assert.ok(types(posted).includes("card"), `posted ${types(posted).join(", ")}`);
});

test("a question is posted as Slack's own container", async () => {
  const slack = new FakeSlack();
  await post(slack, { ...waitingTurn, interrupts: [pendingClarification] });
  const [posted] = slack.of("chat.postMessage");
  assert.ok(posted && types(posted).includes("container"));
});

test("blocks Slack refuses as invalid are posted again as the sections they stand for", async () => {
  const slack = new FakeSlack();
  slack.failing["chat.postMessage"] = (args) => (hasNative(args) ? "invalid_blocks" : undefined);
  await post(slack, waitingTurn);
  const [refused, sent] = slack.of("chat.postMessage");
  assert.ok(refused && hasNative(refused));
  assert.ok(sent && !hasNative(sent));
  // The same choices, as the buttons the app's handlers take.
  const actions = (sent.blocks as Block[]).find(
    (b) => b.type === "actions" && b.block_id?.startsWith("approval:a1:"),
  );
  assert.ok(actions, "the approval's buttons are in the sections");
  assert.equal(sent.text, refused.text);
});

test("an edit Slack refuses is sent again as sections", async () => {
  const slack = new FakeSlack();
  slack.failing["chat.update"] = (args) => (hasNative(args) ? "invalid_blocks" : undefined);
  const p = await post(slack, { ...doneTurn, status: "working", interrupts: [] });
  await p.update(waitingTurn);
  const edits = slack.of("chat.update");
  assert.ok(edits.length >= 2);
  assert.ok(hasNative(edits.at(-2) ?? {}));
  assert.ok(!hasNative(edits.at(-1) ?? {}));
  assert.ok(types(edits.at(-1) ?? {}).includes("actions"));
});

test("the text alone is sent when Slack refuses the sections too", async () => {
  const slack = new FakeSlack();
  slack.failing["chat.postMessage"] = (args) => (args.blocks ? "invalid_blocks" : undefined);
  await post(slack, waitingTurn);
  const sent = slack.of("chat.postMessage");
  assert.equal(sent.length, 3);
  assert.equal(sent[2]?.blocks, undefined);
  assert.ok(String(sent[2]?.text).length > 0);
});

test("blocks with nothing native in them fail as before", async () => {
  const slack = new FakeSlack();
  slack.failing["chat.postMessage"] = "invalid_blocks";
  const wrapped = withBlockFallback(slack);
  await assert.rejects(
    wrapped.call("chat.postMessage", {
      channel: CHANNEL,
      text: "hi",
      blocks: [{ type: "divider" }],
    }),
    /invalid_blocks/,
  );
  assert.equal(slack.of("chat.postMessage").length, 1);
});

test("a refusal of anything else than the blocks is not sent again", async () => {
  const slack = new FakeSlack();
  slack.failing["chat.postMessage"] = (args) => (hasNative(args) ? "channel_not_found" : undefined);
  await post(slack, waitingTurn);
  assert.equal(slack.of("chat.postMessage").filter((a) => !hasNative(a)).length, 0);
});

test("a message whose sections are longer than its cards still falls back to the sections, buttons and all", async () => {
  // 48 blocks and an approval: the card fits one message, the sections need two.
  const view = renderToSlack(
    <>
      {Array.from({ length: 48 }, (_, i) => `Line ${i}`).map((id) => (
        <NoticeView key={id} notice={{ id, kind: "silence", tone: "info", body: id }} />
      ))}
      <ApprovalCard turnId="r1" approval={{ ...pendingApproval, actingAs: undefined }} />
    </>,
  );
  const slack = new FakeSlack();
  slack.failing["chat.postMessage"] = (args) => (hasNative(args) ? "invalid_blocks" : undefined);
  const wrapped = withBlockFallback(slack);
  for (const page of view.pages) {
    await wrapped.call("chat.postMessage", { channel: CHANNEL, text: view.text, blocks: page });
  }
  const sent = slack.of("chat.postMessage").filter((a) => !hasNative(a));
  assert.equal(sent.length, view.pages.length, "every page is posted");
  const all = sent.flatMap((a) => (a.blocks as Block[] | undefined) ?? []);
  assert.ok(
    sent.every((a) => Array.isArray(a.blocks)),
    "no page falls back to its text alone",
  );
  assert.ok(all.some((b) => b.type === "actions" && b.block_id?.startsWith("approval:a1:")));
});
