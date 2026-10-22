import assert from "node:assert/strict";
import { test } from "node:test";
import { fromServer } from "@humagen/adapter";
import { defaultCopy, MessageView } from "@humagen/components";
import { renderToSlack } from "@humagen/render-slack";
import { echoed, forSlack } from "./app.tsx";
import {
  alertMail,
  BOT,
  budgetMail,
  CHANNEL,
  click,
  command,
  DM,
  direct,
  type FakeSlack,
  failureMail,
  homeOpened,
  interactive,
  memoryMail,
  mention,
  OTHER_TEAM,
  OTHER_USER,
  QUESTIONS,
  questionMail,
  resultMail,
  SUBJECT,
  settle,
  stopPressed,
  TEAM,
  USER,
  type World,
  world,
} from "./harness.ts";
import { words } from "./host.ts";
import { type ClientRecord, HumagenError } from "./humagen.ts";
import { runId, tagId } from "./keys.ts";
import { validateBlocks } from "./validate.ts";

const TS = "1700000000.000100";
const KEY = `slack-${TEAM}-${CHANNEL}-1700000000-000100`;

/** Every Block Kit payload these tests hand to Slack, for the validity check at the end. */
const produced: { from: string; blocks?: unknown[]; view?: object }[] = [];
function collect(name: string, slack: FakeSlack) {
  for (const c of slack.calls) {
    if (Array.isArray(c.args.blocks) && c.args.blocks.length > 0) {
      produced.push({ from: `${name}: ${c.method}`, blocks: c.args.blocks });
    }
    if (c.args.view) produced.push({ from: `${name}: ${c.method}`, view: c.args.view as object });
  }
}

/** Deliver one thing from Slack and let the app get as far as it can. */
async function deliver(w: World, envelope: Parameters<World["app"]["handle"]>[0], app = w.app) {
  await app.handle(envelope);
  await settle();
}

/** Run one event of a run through and let Slack catch up. */
async function emit(w: World, kind: string, payload?: unknown, run = w.humagen.last.id) {
  w.humagen.emit(run, kind, payload);
  await settle();
}

const blocksOf = (args: Record<string, unknown> | undefined) => (args?.blocks ?? []) as unknown[];
const textIn = (blocks: unknown[]) => JSON.stringify(blocks);

test("a mention becomes one run, streamed natively, with the session kept in step", async () => {
  const w = world();
  await deliver(w, mention("what changed in the last deploy?", TS));

  // The run: a new conversation named by the thread, the default agent, the Slack person as actor.
  assert.equal(w.humagen.started.length, 1);
  const asked = w.humagen.started[0];
  assert.equal(asked?.idempotencyKey, KEY);
  assert.equal(asked?.agent, "lead");
  assert.equal(asked?.text, "what changed in the last deploy?");
  assert.equal(asked?.after, undefined);
  assert.equal(w.humagen.last.id, runId("acme", SUBJECT, KEY));
  assert.equal(w.humagen.last.tag_id, tagId(SUBJECT, KEY));

  // Before any event: seen, processing, and a stream opened in the mention's thread.
  assert.deepEqual(w.slack.methods, [
    "reactions.add",
    "agents.sessions.setStatus",
    "chat.startStream",
  ]);
  assert.equal(w.slack.of("reactions.add")[0]?.name, "eyes");
  assert.equal(w.slack.of("reactions.add")[0]?.timestamp, TS);
  assert.deepEqual(w.slack.of("agents.sessions.setStatus")[0], {
    channel_id: CHANNEL,
    thread_ts: TS,
    status: "processing",
  });
  const opened = w.slack.of("chat.startStream")[0];
  assert.equal(opened?.channel, CHANNEL);
  assert.equal(opened?.thread_ts, TS);
  assert.equal(opened?.recipient_user_id, USER);
  assert.equal(opened?.recipient_team_id, TEAM);

  await emit(w, "lifecycle.accepted", { rev: 1 });
  await emit(w, "lifecycle.started", { attempt: 1 });
  await emit(w, "delta.tool.call", { id: "t1", name: "github.search", phase: "requested" });
  await emit(w, "delta.tool.done", { id: "t1", result: "3 commits" });
  await emit(w, "delta.text", { text: "Three commits " });
  await emit(w, "delta.text", { text: "went out." });
  assert.equal(w.slack.streamed, "Three commits went out.");
  const tasks = w.slack
    .of("chat.appendStream")
    .flatMap((a) => a.chunks as { type: string; title?: string; status?: string }[])
    .filter((c) => c.type === "task_update");
  assert.deepEqual(
    tasks.map((t) => [t.title, t.status]),
    [
      ["Github search", "in_progress"],
      ["Github search", "complete"],
    ],
  );
  assert.ok(
    !w.slack.methods.includes("chat.stopStream"),
    "the stream stays open until the run ends",
  );

  await emit(w, "lifecycle.completed", { text: "Three commits went out." });
  await w.app.idle();

  // The whole conversation with Slack, in order.
  const order = w.slack.methods.filter((m) => m !== "chat.appendStream");
  assert.deepEqual(order, [
    "reactions.add",
    "agents.sessions.setStatus",
    "chat.startStream",
    "chat.stopStream",
    "reactions.remove",
    "reactions.add",
    "agents.sessions.setStatus",
  ]);
  assert.deepEqual(w.slack.statuses, ["processing", "active"]);
  assert.equal(w.slack.of("reactions.add").at(-1)?.name, "white_check_mark");
  assert.equal(
    w.slack.streamed,
    "Three commits went out.",
    "the answer is not repeated on completion",
  );
  const final = blocksOf(w.slack.of("chat.stopStream")[0]);
  assert.ok(final.length > 0, "the stream stops with the final blocks");
  assert.match(textIn(final), new RegExp(defaultCopy.helpful));
  assert.match(textIn(final), /lead is AI/);
  collect("streamed answer", w.slack);
});

test("a redelivery of the same Slack event starts no second run, before or after a restart", async () => {
  const w = world();
  await deliver(w, mention("ping", TS));
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.completed", { text: "pong" });
  await w.app.idle();
  const calls = w.slack.calls.length;

  await deliver(w, mention("ping", TS, { retry: 1 }));
  await w.app.idle();
  assert.equal(w.humagen.asked.length, 1, "the same process remembers the event");

  // A new process remembers nothing, so it asks humagen, which has the run.
  const again = w.restart();
  await deliver(w, mention("ping", TS), again);
  await again.idle();
  assert.equal(w.humagen.asked.length, 1, "humagen is not asked to start it again");
  assert.equal(w.humagen.runs.size, 1);
  assert.equal(w.slack.calls.length, calls, "and nothing more is written to Slack");
});

test("a reply in the thread continues the conversation, also after a restart", async () => {
  const w = world();
  await deliver(w, mention("first", TS));
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.completed", { text: "one" });
  await w.app.idle();
  const first = w.humagen.last.id;

  const app = w.restart();
  await deliver(w, mention("second", "1700000050.000200", { thread_ts: TS }), app);
  const second = w.humagen.started[1];
  assert.equal(second?.after, first);
  assert.equal(second?.idempotencyKey, `slack-${TEAM}-${CHANNEL}-1700000050-000200`);
  assert.equal(w.humagen.last.thread, first, "one Slack thread is one conversation");
  assert.equal(w.slack.of("chat.startStream").at(-1)?.thread_ts, TS);
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.completed", { text: "two" });
  await app.idle();
});

test("a direct message is a conversation per thread", async () => {
  const w = world();
  await deliver(w, direct("hello", "1700000100.000100"));
  assert.equal(w.humagen.started[0]?.idempotencyKey, `slack-${TEAM}-${DM}-1700000100-000100`);
  assert.equal(w.slack.of("chat.startStream")[0]?.thread_ts, "1700000100.000100");
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.completed", { text: "hi" });
  await w.app.idle();

  // A new top-level message is a new conversation.
  await deliver(w, direct("something else", "1700000200.000100"));
  assert.equal(w.humagen.started[1]?.after, undefined);
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.completed", { text: "ok" });
  await w.app.idle();
});

test("an event from another team is ignored", async () => {
  const w = world();
  await deliver(w, mention("hello", TS, { team_id: OTHER_TEAM }));
  await deliver(w, {
    type: "interactive",
    payload: {
      type: "shortcut",
      callback_id: "aui_ask",
      team: { id: OTHER_TEAM },
      user: { id: USER },
    },
  });
  await deliver(w, command("hello", { team_id: OTHER_TEAM }));
  await w.app.idle();
  assert.equal(w.humagen.asked.length, 0);
  assert.equal(w.slack.calls.length, 0);
  assert.equal(w.logs.filter((l) => /another team/.test(l)).length, 3);
});

test("a bot's message, the app's own and an edit never start a run", async () => {
  const w = world();
  await deliver(w, mention("loop", TS, { bot_id: "B01ABCDEF" }));
  await deliver(w, direct("loop", "1700000001.000100", { bot_id: "B01ABCDEF", user: undefined }));
  await deliver(w, direct("loop", "1700000002.000100", { user: BOT }));
  await deliver(w, direct("loop", "1700000003.000100", { subtype: "message_changed" }));
  await deliver(w, direct("loop", "1700000004.000100", { subtype: "bot_message" }));
  await w.app.idle();
  assert.equal(w.humagen.asked.length, 0);
  assert.equal(w.slack.calls.length, 0);
});

test("stop mid-run stops the run, confirms in the thread, settles the session and renders nothing more", async () => {
  const w = world();
  await deliver(w, mention("write a long report", TS));
  const run = w.humagen.last.id;
  await emit(w, "lifecycle.accepted");
  await emit(w, "delta.text", { text: "Part one. " });

  await deliver(w, stopPressed(TS));
  await w.app.idle();
  assert.deepEqual(w.humagen.stops, [run]);
  const stopped = w.slack.of("chat.stopStream")[0];
  assert.match(textIn(blocksOf(stopped)), new RegExp(defaultCopy.stopped));
  assert.match(textIn(blocksOf(stopped)), new RegExp(defaultCopy.stopPhase.confirmed));
  assert.deepEqual(w.slack.statuses, ["processing", "active"]);
  assert.equal(w.slack.of("reactions.add").at(-1)?.name, "black_square_for_stop");
  // Slack's order: the work is stopped, then confirmed, then the status moves.
  const m = w.slack.methods;
  assert.ok(m.indexOf("chat.stopStream") < m.lastIndexOf("agents.sessions.setStatus"));

  // The run ended stopped with what it had said. Nothing it publishes later moves the turn.
  const before = w.slack.calls.length;
  w.humagen.emit(run, "delta.text", { text: "Part two." });
  w.humagen.emit(run, "lifecycle.completed", { text: "Part one. Part two." });
  await settle();
  assert.equal(w.slack.calls.length, before);
  assert.equal(w.slack.streamed, "Part one.");
  collect("stopped", w.slack);
});

test("a stop that loses the race to the answer shows the answer, and is not called stopped", async () => {
  const w = world();
  await deliver(w, mention("quick question", TS));
  await emit(w, "lifecycle.accepted");
  await emit(w, "delta.text", { text: "The whole answer." });
  w.humagen.stopsFinishFirst = true;
  await deliver(w, stopPressed(TS));
  await w.app.idle();
  await settle();
  const ended = w.slack.of("chat.stopStream").at(-1);
  assert.doesNotMatch(textIn(blocksOf(ended)), new RegExp(defaultCopy.stopPhase.failed));
  assert.equal((await w.humagen.getRun(w.humagen.last.id))?.status, "completed");
  assert.equal(w.slack.statuses.at(-1), "active");
});

test("a stopped run this process does not follow is confirmed with what it had said", async () => {
  const w = world();
  const run = (await w.humagen.createRun({ agent: "lead", text: "slow question" }, KEY)).id;
  w.humagen.emit(run, "lifecycle.started");
  w.humagen.emit(run, "delta.text", { text: "Half an answer" });
  await deliver(w, stopPressed(TS), w.app);
  await w.app.idle();
  const posted = w.slack.of("chat.postMessage").at(-1);
  assert.match(textIn(blocksOf(posted)), /Half an answer/);
  assert.match(textIn(blocksOf(posted)), new RegExp(defaultCopy.stopped));
});

test("stop after a restart still finds the run, through humagen", async () => {
  const w = world();
  // A run an earlier process started and is no longer there to follow.
  const run = (await w.humagen.createRun({ agent: "lead", text: "slow question" }, KEY)).id;
  w.humagen.emit(run, "lifecycle.started");

  // This process follows nothing. The session still names the conversation.
  const app = w.app;
  const before = w.slack.calls.length;
  await deliver(w, stopPressed(TS), app);
  await app.idle();
  assert.deepEqual(w.humagen.stops, [run]);
  const after = w.slack.calls.slice(before);
  assert.deepEqual(
    after.map((c) => c.method),
    ["chat.postMessage", "agents.sessions.setStatus"],
  );
  assert.equal(after[0]?.args.thread_ts, TS);
  assert.match(textIn(blocksOf(after[0]?.args)), new RegExp(defaultCopy.stopped));
  assert.equal(after[1]?.args.status, "active");
  collect("stopped after restart", w.slack);

  // The confirmation carries what the run had said when humagen stopped it.
  assert.equal((await w.humagen.getRun(run))?.status, "stopped");

  // With nothing left to stop, the button still settles the session.
  const idle = w.restart();
  await deliver(w, stopPressed(TS), idle);
  await idle.idle();
  assert.deepEqual(w.humagen.stops, [run], "an ended run is not stopped again");
  assert.equal(w.slack.calls.at(-1)?.method, "agents.sessions.setStatus");
  await w.app.idle();
});

test("a stop humagen does not confirm says so, and still renders nothing more", async () => {
  const w = world();
  await deliver(w, mention("question", TS));
  await emit(w, "lifecycle.accepted");
  w.humagen.stopFails = true;
  await deliver(w, stopPressed(TS));
  await w.app.idle();
  assert.match(
    textIn(blocksOf(w.slack.of("chat.stopStream")[0])),
    new RegExp(defaultCopy.stopPhase.failed),
  );
  assert.deepEqual(w.slack.statuses, ["processing", "active"]);
});

test("a run stopped from the console ends in Slack, though it publishes no final event", async () => {
  const w = world();
  await deliver(w, mention("question", TS));
  const run = w.humagen.last.id;
  await emit(w, "lifecycle.accepted");
  w.humagen.set(run, { status: "stopped", cause: "Stopped by dana" });
  await w.app.idle();
  assert.match(textIn(blocksOf(w.slack.of("chat.stopStream")[0])), new RegExp(defaultCopy.stopped));
  assert.deepEqual(w.slack.statuses, ["processing", "active"]);
});

test("a stopped event on the stream ends the turn, once the platform publishes one", async () => {
  const w = world();
  await deliver(w, mention("question", TS));
  await emit(w, "lifecycle.accepted");
  await emit(w, "delta.text", { text: "Half an answer." });
  await emit(w, "lifecycle.stopped", { cause: "Stopped by dana" });
  await w.app.idle();
  const final = textIn(blocksOf(w.slack.of("chat.stopStream")[0]));
  assert.match(final, new RegExp(defaultCopy.stopped));
  assert.match(final, new RegExp(defaultCopy.stopPhase.confirmed));
  assert.deepEqual(w.slack.statuses, ["processing", "active"]);
});

test("a failed run shows why, and Retry asks again in the same conversation", async () => {
  const w = world();
  await deliver(w, mention("deploy it", TS));
  const failedRun = w.humagen.last.id;
  await emit(w, "lifecycle.accepted");
  // An attempt the engine will retry is not the end of the turn.
  await emit(w, "lifecycle.failed", { error: "timeout", terminal: false });
  assert.ok(!w.slack.methods.includes("chat.stopStream"));
  await emit(w, "lifecycle.failed", {
    error: "agent: step 2\nprovider: 529 overloaded",
    terminal: true,
  });
  await w.app.idle();
  const final = w.slack.of("chat.stopStream")[0];
  assert.match(textIn(blocksOf(final)), /provider: 529 overloaded/);
  assert.deepEqual(w.slack.statuses, ["processing", "active"]);
  assert.equal(w.slack.of("reactions.add").at(-1)?.name, "x");
  collect("failed", w.slack);

  const ts = String(w.slack.calls.find((c) => c.method === "chat.stopStream")?.args.ts);
  await deliver(w, click(blocksOf(final), defaultCopy.retry, { ts, threadTs: TS }));
  assert.equal(w.humagen.started.length, 2);
  const retry = w.humagen.started[1];
  assert.equal(retry?.text, "deploy it");
  assert.equal(retry?.agent, "lead");
  assert.equal(retry?.after, failedRun, "the retry stays in the thread's conversation");
  assert.equal(w.humagen.last.thread, failedRun);
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.completed", { text: "Deployed." });
  await w.app.idle();

  // A second click joins the first retry.
  await deliver(w, click(blocksOf(final), defaultCopy.retry, { ts, threadTs: TS }));
  await w.app.idle();
  assert.equal(w.humagen.started.length, 2);
});

test("a run whose retries ran out is ended by its record, as the ledger decides", async () => {
  const w = world();
  await deliver(w, mention("deploy it", TS));
  const run = w.humagen.last.id;
  await emit(w, "lifecycle.accepted");
  // Each attempt reports a failure, and none says it was the last.
  await emit(w, "lifecycle.failed", { error: "connection refused", attempt: 1 });
  await emit(w, "lifecycle.resumed", { attempt: 2 });
  await emit(w, "lifecycle.failed", { error: "connection refused", attempt: 2 });
  assert.ok(!w.slack.methods.includes("chat.stopStream"));
  w.humagen.set(run, {
    status: "failed",
    error: "anchor: max attempts exhausted\nconnection refused",
  });
  await w.app.idle();
  const final = w.slack.of("chat.stopStream")[0];
  assert.match(textIn(blocksOf(final)), /connection refused/);
  assert.match(textIn(blocksOf(final)), new RegExp(defaultCopy.retry));
  assert.deepEqual(w.slack.statuses, ["processing", "active"]);
});

/** A failed turn in the thread, and the message holding its Retry button. */
async function failedTurn(w: World) {
  await deliver(w, mention("deploy it", TS));
  const run = w.humagen.last.id;
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.failed", { error: "provider: 529 overloaded", terminal: true });
  await w.app.idle();
  const final = w.slack.of("chat.stopStream").at(-1);
  return { run, blocks: blocksOf(final), ts: String(final?.ts) };
}

test("a turn that acted for nobody is asked again, for nobody, by the person who asked or an operator", async () => {
  const w = world({ operators: ["U03ABCDEF"] });
  const { run, blocks, ts } = await failedTurn(w);
  assert.equal(w.humagen.runs.get(run)?.actor?.bot, true, "the app asked as itself, for nobody");

  // Someone else in the channel is told no, and nothing starts.
  await deliver(w, click(blocks, defaultCopy.retry, { ts, threadTs: TS, user: OTHER_USER }));
  await w.app.idle();
  assert.equal(w.humagen.started.length, 1);
  assert.equal(
    w.slack.of("chat.postEphemeral").at(-1)?.text,
    "Only the person who asked can retry this.",
  );
  assert.equal(w.slack.of("chat.postEphemeral").at(-1)?.user, OTHER_USER);

  // An operator may. The new run acts for nobody, as the turn did, and stays the first asker's to settle.
  await deliver(w, click(blocks, defaultCopy.retry, { ts, threadTs: TS, user: "U03ABCDEF" }));
  assert.equal(w.humagen.started.length, 2);
  assert.equal(w.humagen.started[1]?.act_as, undefined);
  const retried = w.humagen.last.id;
  assert.equal(w.humagen.last.prev ?? w.humagen.last.thread, run);
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.completed", { text: "Deployed." });
  await w.app.idle();

  // So the person who asked can settle what the retried run raises, and the clicker who is not an operator cannot.
  const watching = new AbortController();
  const watch = w.app.watchInbox(watching.signal);
  w.humagen.raise(memoryMail(retried));
  await settle();
  await w.app.idle();
  const posted = w.slack.of("chat.postMessage").at(-1);
  await deliver(
    w,
    click(blocksOf(posted), defaultCopy.discardAll, {
      ts: "1800000009.000100",
      threadTs: TS,
      user: OTHER_USER,
    }),
  );
  assert.equal(w.humagen.resolved.length, 0);
  await deliver(
    w,
    click(blocksOf(posted), defaultCopy.discardAll, { ts: "1800000009.000100", threadTs: TS }),
  );
  await w.app.idle();
  assert.equal(w.humagen.resolved.length, 1);
  watching.abort();
  await watch;
});

test("when the app's own credential is a person, as under dev sign-in, a turn it asked as itself is asked again through it", async () => {
  const w = world({ operators: ["U03ABCDEF"] });
  w.humagen.person = true;
  const { run, blocks, ts } = await failedTurn(w);
  assert.equal(
    w.humagen.runs.get(run)?.actor?.user,
    SUBJECT,
    "every run the app asks acts for its credential",
  );

  // The asker asks again through the app, as the turn was asked, so it acts for the same person.
  await deliver(w, click(blocks, defaultCopy.retry, { ts, threadTs: TS }));
  assert.equal(w.humagen.started.length, 2);
  assert.equal(w.humagen.started[1]?.act_as, undefined);
  assert.equal(w.humagen.last.actor?.user, SUBJECT);
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.completed", { text: "Deployed." });
  await w.app.idle();

  // After a restart an operator may too, and anyone else is told no.
  const app = w.restart();
  await deliver(w, click(blocks, defaultCopy.retry, { ts, threadTs: TS, user: OTHER_USER }), app);
  await app.idle();
  assert.equal(w.humagen.started.length, 2);
  assert.equal(
    w.slack.of("chat.postEphemeral").at(-1)?.text,
    "Only the person who asked can retry this.",
  );
  await deliver(w, click(blocks, defaultCopy.retry, { ts, threadTs: TS, user: "U03ABCDEF" }), app);
  assert.equal(w.humagen.started.length, 3);
  assert.equal(w.humagen.started[2]?.act_as, undefined);
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.completed", { text: "ok" });
  await app.idle();
});

test("after a restart, who asked is not known, so only an operator may ask again", async () => {
  const w = world({ operators: ["U03ABCDEF"] });
  const { blocks, ts } = await failedTurn(w);
  const app = w.restart();
  await deliver(w, click(blocks, defaultCopy.retry, { ts, threadTs: TS }), app);
  await app.idle();
  assert.equal(w.humagen.started.length, 1, "fails closed for the person who asked, too");
  assert.equal(
    w.slack.of("chat.postEphemeral").at(-1)?.text,
    "Only the person who asked can retry this.",
  );
  await deliver(w, click(blocks, defaultCopy.retry, { ts, threadTs: TS, user: "U03ABCDEF" }), app);
  assert.equal(w.humagen.started.length, 2);
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.completed", { text: "ok" });
  await app.idle();
});

// Retry and Resume ask a turn again for whom it acted for, as Retry in humagen's
// inbox does: a turn that acted for a person only by that person, as
// themselves; a turn that acted for nobody for nobody, whoever presses it.
// Humagen's record says whom a turn acted for, so a restart changes nothing.
const OPERATOR = "U03ABCDEF";
const NOT_THEIRS = defaultCopy.retryOnlyTheirs;

/** A Retry or Resume pressed on a turn, as Slack sends it. */
function pressed(id: "retry" | "resume", run: string, user: string) {
  return interactive({
    type: "block_actions",
    user: { id: user, team_id: TEAM },
    channel: { id: CHANNEL },
    container: { type: "message", channel_id: CHANNEL },
    message: { ts: "1800000008.000100", thread_ts: TS },
    actions: [{ action_id: `aui.${id}#0`, value: JSON.stringify({ turnId: run, target: run }) }],
  });
}

test("a turn that acted for a person is asked again only by them, as themselves, after a restart too", async () => {
  const w = mapped({ operators: [OPERATOR] });
  const { run, blocks, ts } = await failedTurn(w);
  assert.equal(w.humagen.runs.get(run)?.actor?.user, "ana");

  // Olu, whom the client maps to someone else, and an operator: refused, nothing starts.
  for (const user of [OTHER_USER, OPERATOR]) {
    await deliver(w, click(blocks, defaultCopy.retry, { ts, threadTs: TS, user }));
    await deliver(w, pressed("resume", run, user));
    await w.app.idle();
    assert.equal(w.humagen.started.length, 1, user);
    assert.equal(w.slack.of("chat.postEphemeral").at(-1)?.text, NOT_THEIRS);
    assert.equal(w.slack.of("chat.postEphemeral").at(-1)?.user, user);
  }

  // Ana asks it again, as herself.
  await deliver(w, click(blocks, defaultCopy.retry, { ts, threadTs: TS }));
  assert.equal(w.humagen.started.length, 2);
  assert.deepEqual(w.humagen.started[1]?.act_as, { client: CLIENT, user: USER });
  assert.equal(w.humagen.last.actor?.user, "ana");
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.completed", { text: "Deployed." });
  await w.app.idle();

  // After a restart nobody here remembers who asked, and nothing changes:
  // the operator is still refused, and ana pressing again joins her retry.
  const app = w.restart();
  await deliver(w, pressed("retry", run, OPERATOR), app);
  await app.idle();
  assert.equal(w.humagen.started.length, 2);
  assert.equal(w.slack.of("chat.postEphemeral").at(-1)?.text, NOT_THEIRS);
  await deliver(w, pressed("retry", run, USER), app);
  await app.idle();
  assert.equal(w.humagen.started.length, 2, "her second press joins her first retry");
});

test("a turn that acted for nobody is asked again for nobody, and each person's press is its own run", async () => {
  // Olu is an operator the client maps. The person who asks is not mapped.
  const w = mapped({ operators: [OTHER_USER, OPERATOR] });
  await deliver(w, mention("deploy it", TS, { user: "U05UNMAPPED" }));
  const run = w.humagen.last.id;
  assert.equal(w.humagen.asked[0]?.act_as, undefined);
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.failed", { error: "provider: 529 overloaded", terminal: true });
  await w.app.idle();

  // After a restart, so who asked is not known: each operator's press is a
  // run of its own, and each acts for nobody, never for whoever pressed.
  const app = w.restart();
  await deliver(w, pressed("retry", run, OTHER_USER), app);
  assert.equal(w.humagen.started.length, 2);
  assert.equal(
    w.humagen.started[1]?.act_as,
    undefined,
    "olu's credential is not put behind the words",
  );
  assert.equal(w.humagen.last.actor?.bot, true);
  const first = w.humagen.last.id;
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.completed", { text: "ok" });
  await app.idle();
  await deliver(w, pressed("resume", run, OPERATOR), app);
  assert.equal(
    w.humagen.started.length,
    3,
    "a second person's press does not join the first's run",
  );
  assert.notEqual(w.humagen.last.id, first);
  assert.equal(w.humagen.started[2]?.act_as, undefined);
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.completed", { text: "ok" });
  await app.idle();
});

test("asking again is refused once the agent may no longer be reached", async () => {
  const w = world({ clientId: "docs-bot", defaultAgent: undefined });
  const client = {
    id: "docs-bot",
    surface: "slack",
    enabled: true,
    default_agent: "lead",
    identity: { team: TEAM },
  };
  w.humagen.clients.set("docs-bot", client);
  const { blocks, ts } = await failedTurn(w);
  // The client is changed: lead is no longer its agent. Its record is read again after 30s.
  w.humagen.clients.set("docs-bot", { ...client, default_agent: "helper" });
  const app = new (w.app.constructor as typeof import("./app.tsx").App)(w.slack, w.humagen, {
    team: TEAM,
    botUser: BOT,
    subject: SUBJECT,
    tenant: "acme",
    clientId: "docs-bot",
    operators: [USER],
    flushMs: 0,
    pollMs: 20,
  });
  await deliver(w, click(blocks, defaultCopy.retry, { ts, threadTs: TS }), app);
  await app.idle();
  assert.equal(w.humagen.started.length, 1);
  assert.equal(w.slack.of("chat.postEphemeral").at(-1)?.text, defaultCopy.cantRetry);
});

/** A failure card as Slack shows it, from Home (a view) or in a thread (a message). */
function failureCard(run: string, where: "home" | "thread", user: string) {
  const blocks = renderToSlack(
    <MessageView message={forSlack(fromServer(failureMail(run)))} />,
  ).blocks;
  const env = click(blocks, defaultCopy.retry, { ts: "1800000005.000100", threadTs: TS, user });
  if (where === "home") {
    const p = env.payload as Record<string, unknown>;
    p.container = { type: "view" };
    p.view = { type: "home" };
    delete p.message;
  }
  return env;
}

for (const where of ["home", "thread"] as const) {
  test(`Retry on an inbox failure card from ${where} never asks humagen to retry on its own`, async () => {
    const w = world({ operators: ["U03ABCDEF"] });
    const { run } = await failedTurn(w);
    w.humagen.mailbox.set("m-failure", failureMail(run));

    // Someone who did not ask: told no, nothing sent.
    await deliver(w, failureCard(run, where, OTHER_USER));
    await w.app.idle();
    assert.equal(w.humagen.started.length, 1);
    assert.deepEqual(w.humagen.resolved, []);
    assert.ok(w.slack.of("chat.postEphemeral").at(-1));

    // A run that is not in one of this app's threads: an operator is told it cannot be retried here.
    w.humagen.mailbox.set("m-console", {
      ...failureMail("api-dana-console-run"),
      id: "m-console",
    });
    w.humagen.runs.set("api-dana-console-run", {
      ...w.humagen.last,
      id: "api-dana-console-run",
      tag_id: "api-dana-console-run",
      thread: "api-dana-console-run",
    });
    const elsewhere = failureCard("api-dana-console-run", where, "U03ABCDEF");
    const v = JSON.parse(
      String((elsewhere.payload as { actions: { value: string }[] }).actions[0]?.value),
    );
    (elsewhere.payload as { actions: { value: string }[] }).actions[0] = {
      ...(elsewhere.payload as { actions: { action_id: string; value: string }[] }).actions[0],
      value: JSON.stringify({ ...v, turnId: "m-console" }),
    } as { action_id: string; value: string };
    await deliver(w, elsewhere);
    await w.app.idle();
    assert.deepEqual(w.humagen.resolved, []);
    assert.equal(w.slack.of("chat.postEphemeral").at(-1)?.text, defaultCopy.cantRetry);

    // The agent is no longer one the app may reach: told no, nothing sent.
    const gated = world(
      { operators: ["U03ABCDEF"], clientId: "docs-bot", defaultAgent: undefined },
      { slack: w.slack, humagen: w.humagen },
    );
    w.humagen.clients.set("docs-bot", {
      id: "docs-bot",
      surface: "slack",
      enabled: true,
      default_agent: "helper",
      identity: { team: TEAM },
    });
    await deliver(w, failureCard(run, where, "U03ABCDEF"), gated.app);
    await gated.app.idle();
    assert.equal(w.humagen.started.length, 1);
    assert.deepEqual(w.humagen.resolved, []);
    assert.equal(w.slack.of("chat.postEphemeral").at(-1)?.text, defaultCopy.cantRetry);

    // The person who asked: the app asks again in the thread and only acknowledges the card.
    await deliver(w, failureCard(run, where, USER));
    assert.equal(w.humagen.started.length, 2);
    assert.deepEqual((w.humagen.resolved as { body: unknown }[]).at(-1)?.body, {
      action: "acknowledge",
    });
    await emit(w, "lifecycle.accepted");
    await emit(w, "lifecycle.completed", { text: "ok" });
    await w.app.idle();
  });
}

test("Retry on an inbox card of a person's failed turn is theirs, after a restart too, and settles the card", async () => {
  const w = mapped({ operators: ["U03ABCDEF"] });
  const { run } = await failedTurn(w);
  assert.equal(w.humagen.runs.get(run)?.actor?.user, "ana");
  // As humagen sends it to the app, whose own credential is not offered Retry on ana's turn.
  const [part] = failureMail(run).parts as Record<string, unknown>[];
  const mail = { ...failureMail(run), parts: [{ ...part, actions: ["acknowledge"] }] };
  w.humagen.mailbox.set("m-failure", mail);
  const blocks = renderToSlack(<MessageView message={forSlack(fromServer(mail))} />).blocks;
  assert.match(
    textIn(blocks),
    new RegExp(`"${defaultCopy.retry}"`),
    "the app decides who may press it",
  );
  assert.doesNotMatch(textIn(blocks), new RegExp(defaultCopy.retryOnlyTheirs));

  // After a restart nobody here knows ana asked, and she is no operator. Olu
  // is told no; ana's press asks again as her and acknowledges the card.
  const app = w.restart();
  const card = (user: string) =>
    click(blocks, defaultCopy.retry, { ts: "1800000005.000100", threadTs: TS, user });
  await deliver(w, card(OTHER_USER), app);
  await app.idle();
  assert.equal(w.humagen.started.length, 1);
  assert.deepEqual(w.humagen.resolved, []);
  assert.equal(w.slack.of("chat.postEphemeral").at(-1)?.text, defaultCopy.retryOnlyTheirs);
  await deliver(w, card(USER), app);
  assert.equal(w.humagen.started.length, 2);
  assert.deepEqual(w.humagen.started[1]?.act_as, { client: CLIENT, user: USER });
  assert.deepEqual((w.humagen.resolved as { body: unknown }[]).at(-1)?.body, {
    action: "acknowledge",
  });
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.completed", { text: "ok" });
  await app.idle();
});

test("a forged Retry on an inbox part that is not a failure is refused, and nothing is sent", async () => {
  const w = world({ operators: [USER] });
  w.humagen.mailbox.set("m-memory", memoryMail("api-svc-slack-any"));
  w.humagen.mailbox.set("m-alert", alertMail());
  for (const [id, target] of [
    ["m-memory", "p1"],
    ["m-alert", "p1"],
    ["m-alert", "no-such-part"],
  ]) {
    const env = interactive({
      type: "block_actions",
      channel: { id: CHANNEL },
      container: { type: "message", channel_id: CHANNEL },
      message: { ts: "1800000007.000100", thread_ts: TS },
      actions: [{ action_id: "aui.retry#0", value: JSON.stringify({ turnId: id, target }) }],
    });
    await deliver(w, env);
    await w.app.idle();
    assert.deepEqual(w.humagen.resolved, [], `${id}/${target}`);
    assert.equal(w.slack.of("chat.postEphemeral").at(-1)?.text, defaultCopy.cantRetry);
  }
  assert.equal(w.humagen.started.length, 0);
});

test("a Slack write that fails mid-turn still ends the turn: whole, and off processing", async () => {
  const w = world();
  await deliver(w, mention("long answer", TS));
  await emit(w, "lifecycle.accepted");
  await emit(w, "delta.text", { text: "First part. " });
  // Slack starts refusing appends, as it does after its retries for a rate limit run out.
  w.slack.failing["chat.appendStream"] = "ratelimited";
  await emit(w, "delta.text", { text: "Second part. " });
  await emit(w, "delta.text", { text: "Third part." });
  await emit(w, "lifecycle.completed", { text: "First part. Second part. Third part." });
  await w.app.idle();
  const m = w.slack.methods;
  assert.equal(
    m.filter((x) => x === "chat.appendStream").length,
    2,
    "nothing more is tried once a write fails",
  );
  // The streamed message is closed with the whole turn, so nothing is posted twice.
  const closed = w.slack.of("chat.stopStream").at(-1);
  assert.match(textIn(blocksOf(closed)), /First part\. Second part\. Third part\./);
  assert.match(textIn(blocksOf(closed)), new RegExp(defaultCopy.helpful));
  assert.equal(w.slack.of("chat.postMessage").length, 0);
  assert.deepEqual(w.slack.statuses, ["processing", "active"]);
  assert.equal(w.slack.of("reactions.remove").at(-1)?.name, "eyes");
  assert.equal(w.slack.of("reactions.add").at(-1)?.name, "white_check_mark");
  collect("write failed", w.slack);
});

test("when the streamed message cannot take the turn at all, the turn is posted as a new one", async () => {
  const w = world();
  await deliver(w, mention("long answer", TS));
  await emit(w, "lifecycle.accepted");
  await emit(w, "delta.text", { text: "First part. " });
  w.slack.failing["chat.appendStream"] = "ratelimited";
  w.slack.failing["chat.stopStream"] = (args) => (args.blocks ? "invalid_blocks" : undefined);
  await emit(w, "delta.text", { text: "Second part." });
  await emit(w, "lifecycle.completed", { text: "First part. Second part." });
  await w.app.idle();
  const whole = w.slack.of("chat.postMessage");
  assert.equal(whole.length, 1);
  assert.match(textIn(blocksOf(whole[0])), /First part\. Second part\./);
  assert.deepEqual(w.slack.statuses, ["processing", "active"]);
});

test("when editing in place fails, the turn is still posted and the session settled", async () => {
  const w = world();
  w.slack.failing["chat.startStream"] = "unknown_method";
  w.slack.failing["chat.update"] = "ratelimited";
  await deliver(w, mention("hello", TS));
  await emit(w, "lifecycle.accepted");
  await emit(w, "delta.text", { text: "Hello " });
  await emit(w, "delta.text", { text: "there." });
  await emit(w, "lifecycle.completed", { text: "Hello there." });
  await w.app.idle();
  const posts = w.slack.of("chat.postMessage");
  assert.match(textIn(blocksOf(posts.at(-1))), /Hello there\./);
  assert.deepEqual(w.slack.statuses, ["processing", "active"]);
});

const pause = (ms: number) => new Promise((r) => setTimeout(r, ms));

/** The answers posted as new messages after a quiet run's stream was closed. */
const finals = (w: World, text: string) =>
  w.slack.of("chat.postMessage").filter((a) => textIn(blocksOf(a)).includes(text));

test("a long, silent tool call is not a failure: the stream closes and the answer follows once", async () => {
  const w = world({ stallMs: 30, slowPollMs: 40 });
  await deliver(w, mention("rebuild the index", TS));
  const run = w.humagen.last.id;
  await emit(w, "lifecycle.accepted");
  await emit(w, "delta.tool.call", { id: "t1", name: "index.rebuild", phase: "requested" });
  await pause(120);
  // The stream is closed as it stands, still working: no failure, still processing.
  const closed = w.slack.of("chat.stopStream")[0];
  assert.ok(closed, "the stream is not held open");
  assert.ok(!textIn(blocksOf(closed)).includes(defaultCopy.failedTitle));
  assert.ok(!textIn(blocksOf(closed)).includes(defaultCopy.failure.timeout()));
  assert.deepEqual(w.slack.statuses, ["processing"]);
  assert.deepEqual(w.humagen.stops, []);

  w.humagen.set(run, { status: "completed", answer: "The index is rebuilt." });
  await w.app.idle();
  await pause(100);
  assert.equal(finals(w, "The index is rebuilt.").length, 1, "the answer is posted once");
  assert.equal(finals(w, "The index is rebuilt.")[0]?.thread_ts, TS);
  assert.deepEqual(w.slack.statuses, ["processing", "active"]);
  assert.equal(w.slack.of("reactions.add").at(-1)?.name, "white_check_mark");
  collect("quiet then done", w.slack);
});

test("a parked run is only waiting, and its end is posted when it comes", async () => {
  const w = world({ stallMs: 30, slowPollMs: 40 });
  await deliver(w, mention("ask the helper", TS));
  const run = w.humagen.last.id;
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.parked", { reason: "waiting on helper" });
  await pause(120);
  assert.equal(w.slack.of("chat.stopStream").length, 1);
  assert.deepEqual(w.slack.statuses, ["processing"]);
  w.humagen.set(run, { status: "completed", answer: "The helper found it." });
  await w.app.idle();
  assert.equal(finals(w, "The helper found it.").length, 1);
  assert.deepEqual(w.slack.statuses, ["processing", "active"]);
});

test("a quiet run does not hold the thread, and the next message follows it as humagen orders turns", async () => {
  const w = world({ stallMs: 30, slowPollMs: 40 });
  await deliver(w, mention("long job", TS));
  const first = w.humagen.last.id;
  await emit(w, "lifecycle.accepted");
  await pause(120);

  await deliver(w, mention("meanwhile, a quick one", "1700000070.000100", { thread_ts: TS }));
  assert.equal(w.humagen.started.length, 2, "the queue was released");
  // Humagen takes a turn after one that is still running; its transcript says so.
  assert.equal(w.humagen.started[1]?.after, first);
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.completed", { text: "Quick answer." });

  w.humagen.set(first, { status: "completed", answer: "Long job done." });
  await w.app.idle();
  assert.equal(finals(w, "Long job done.").length, 1);
});

test("stop on a quiet run confirms it once, and its end is not posted again", async () => {
  const w = world({ stallMs: 30, slowPollMs: 40 });
  await deliver(w, mention("long job", TS));
  const run = w.humagen.last.id;
  await emit(w, "lifecycle.accepted");
  await pause(120);
  await deliver(w, stopPressed(TS));
  await w.app.idle();
  assert.deepEqual(w.humagen.stops, [run]);
  const confirmations = w.slack
    .of("chat.postMessage")
    .filter((a) => textIn(blocksOf(a)).includes(defaultCopy.stopped));
  assert.equal(confirmations.length, 1);
  assert.equal(w.slack.statuses.at(-1), "active");
});

test("a quiet run whose stop loses the race posts its answer, once", async () => {
  const w = world({ stallMs: 30, slowPollMs: 40 });
  await deliver(w, mention("long job", TS));
  const run = w.humagen.last.id;
  await emit(w, "lifecycle.accepted");
  await emit(w, "delta.text", { text: "Long job done." });
  await pause(120);
  w.humagen.stopsFinishFirst = true;
  await deliver(w, stopPressed(TS));
  await w.app.idle();
  await pause(100);
  assert.deepEqual(w.humagen.stops, [run]);
  assert.equal((await w.humagen.getRun(run))?.status, "completed");
  assert.equal(finals(w, "Long job done.").length, 1, "the answer is posted once");
  assert.ok(
    !w.slack.of("chat.postMessage").some((a) => textIn(blocksOf(a)).includes(defaultCopy.stopped)),
  );
  assert.equal(w.slack.statuses.at(-1), "active");
});

test("a stop slower than the wait is confirmed when it lands, and the session waits for it", async () => {
  const w = world({ stopWaitMs: 100 });
  const run = (await w.humagen.createRun({ agent: "lead", text: "slow question" }, KEY)).id;
  w.humagen.emit(run, "lifecycle.started");
  w.humagen.emit(run, "delta.text", { text: "Half an answer" });
  w.humagen.stopDelayMs = 400;
  await deliver(w, stopPressed(TS), w.app);
  await pause(200);
  // The wait is over and the run has not ended: nothing is posted, and the session is not settled yet.
  assert.equal(finals(w, "Half an answer").length, 0);
  assert.ok(!w.slack.statuses.includes("active"), "the session is not settled before the run ends");
  await w.app.idle();
  assert.equal((await w.humagen.getRun(run))?.status, "stopped");
  const posted = finals(w, "Half an answer");
  assert.equal(posted.length, 1, "the stopped run is confirmed with what it had said, once");
  assert.match(textIn(blocksOf(posted[0])), new RegExp(defaultCopy.stopped));
  assert.equal(w.slack.statuses.at(-1), "active");
});

test("a slow follow and a late lifecycle.stopped post a quiet run's stopped end once", async () => {
  const w = world({ stallMs: 30, slowPollMs: 40 });
  await deliver(w, mention("long job", TS));
  const run = w.humagen.last.id;
  await emit(w, "lifecycle.accepted");
  await emit(w, "delta.text", { text: "Partway there" });
  await pause(120);
  await deliver(w, stopPressed(TS));
  await w.app.idle();
  await pause(150);
  // A second press, and the event again: nothing more is posted.
  await deliver(w, stopPressed(TS));
  w.humagen.emit(run, "lifecycle.stopped", { text: "Partway there", reason: "again" });
  await w.app.idle();
  await pause(150);
  assert.equal((await w.humagen.getRun(run))?.status, "stopped");
  const ends = w.slack
    .of("chat.postMessage")
    .filter((a) => textIn(blocksOf(a)).includes("Partway there"));
  assert.equal(ends.length, 1, "the stopped end is posted once");
  assert.match(textIn(blocksOf(ends[0])), new RegExp(defaultCopy.stopped));
});

test("on shutdown a quiet run stops being followed", async () => {
  const w = world({ stallMs: 30, slowPollMs: 40 });
  await deliver(w, mention("long job", TS));
  await emit(w, "lifecycle.accepted");
  await pause(120);
  w.app.close();
  await w.app.idle();
  assert.deepEqual(w.humagen.stops, [], "the run is left running");
});

test("an inbox decision humagen refuses is explained in humagen's words, not the platform's", async () => {
  const w = world();
  const watching = new AbortController();
  const watch = w.app.watchInbox(watching.signal);
  await deliver(w, mention("remember this", TS));
  const run = w.humagen.last.id;
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.completed", { text: "Noted." });
  await w.app.idle();
  w.humagen.raise(memoryMail(run));
  await settle();
  await w.app.idle();
  const posted = w.slack.of("chat.postMessage").at(-1);
  w.humagen.inbox.send = async () => {
    throw new HumagenError(403, "platform: forbidden: keep needs agents:write");
  };
  await deliver(
    w,
    click(blocksOf(posted), defaultCopy.discardAll, { ts: "1800000002.000100", threadTs: TS }),
  );
  await w.app.idle();
  assert.equal(w.slack.of("chat.postEphemeral").at(-1)?.text, defaultCopy.noPermission);
  assert.ok(w.logs.some((l) => l.includes("agents:write")));
  watching.abort();
  await watch;
});

test("an answer that never streamed arrives whole when the run completes", async () => {
  const w = world();
  await deliver(w, mention("quick", TS));
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.completed", { text: "Done." });
  await w.app.idle();
  assert.equal(w.slack.streamed, "Done.");
});

test("where native streaming is refused, the turn is posted once and edited", async () => {
  const w = world();
  w.slack.failing["chat.startStream"] = "unknown_method";
  await deliver(w, mention("hello", TS));
  await emit(w, "lifecycle.accepted");
  await emit(w, "delta.text", { text: "Hello " });
  await emit(w, "delta.text", { text: "there." });
  await emit(w, "lifecycle.completed", { text: "Hello there." });
  await w.app.idle();
  const m = w.slack.methods;
  assert.equal(m.filter((x) => x === "chat.startStream").length, 1, "streaming is tried once");
  assert.equal(m.filter((x) => x === "chat.postMessage").length, 1);
  assert.ok(m.filter((x) => x === "chat.update").length >= 1);
  assert.ok(!m.includes("chat.appendStream") && !m.includes("chat.stopStream"));
  assert.equal(w.slack.of("chat.postMessage")[0]?.thread_ts, TS);
  const last = w.slack.of("chat.update").at(-1);
  assert.match(textIn(blocksOf(last)), /Hello there\./);
  assert.match(textIn(blocksOf(last)), new RegExp(defaultCopy.helpful));
  assert.deepEqual(w.slack.statuses, ["processing", "active"]);
  collect("post and update", w.slack);

  // The refusal is remembered: the next turn does not try to stream.
  await deliver(w, mention("again", "1700000300.000100"));
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.completed", { text: "Yes." });
  await w.app.idle();
  assert.equal(w.slack.methods.filter((x) => x === "chat.startStream").length, 1);
});

test("an agent named in the message answers, when it exists and may be reached", async () => {
  const w = world();
  await deliver(w, mention("@helper summarize this", TS));
  assert.equal(w.humagen.started[0]?.agent, "helper");
  assert.equal(w.humagen.started[0]?.text, "summarize this");
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.completed", { text: "ok" });
  await w.app.idle();

  // A name that is not an agent is only a word.
  await deliver(w, mention("@nobody summarize this", "1700000400.000100"));
  assert.equal(w.humagen.started[1]?.agent, "lead");
  assert.equal(w.humagen.started[1]?.text, "@nobody summarize this");
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.completed", { text: "ok" });
  await w.app.idle();
});

test("as a client, the app routes only to its default agent and its list", async () => {
  const w = world({ clientId: "docs-bot", defaultAgent: undefined });
  w.humagen.clients.set("docs-bot", {
    id: "docs-bot",
    surface: "slack",
    enabled: true,
    default_agent: "helper",
    agents: [],
    identity: { team: TEAM },
  });
  await deliver(w, mention("@lead take this", TS));
  assert.equal(
    w.humagen.started[0]?.agent,
    "helper",
    "a name outside the list is not read as an agent",
  );
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.completed", { text: "ok" });
  await w.app.idle();
});

test("a client for another team, or one that is gone, routes to nothing", async () => {
  const w = world({ clientId: "docs-bot", defaultAgent: undefined });
  w.humagen.clients.set("docs-bot", {
    id: "docs-bot",
    surface: "slack",
    enabled: true,
    default_agent: "helper",
    identity: { team: OTHER_TEAM },
  });
  await deliver(w, mention("hello", TS));
  await w.app.idle();
  assert.equal(w.humagen.asked.length, 0);
  const said = w.slack.of("chat.postMessage")[0];
  assert.match(textIn(blocksOf(said)), /No agent answers here yet/);
  assert.deepEqual(w.slack.statuses, ["active"]);
  collect("no agent", w.slack);

  // A client that does not say which team it serves is not taken for this one.
  const v = world({ clientId: "docs-bot", defaultAgent: undefined });
  v.humagen.clients.set("docs-bot", {
    id: "docs-bot",
    surface: "slack",
    enabled: true,
    default_agent: "helper",
  });
  await deliver(v, mention("hello", TS));
  await v.app.idle();
  assert.equal(v.humagen.asked.length, 0);
});

test("a message that names no agent leaves the choice to the platform's default", async () => {
  const w = world({ defaultAgent: undefined });
  await deliver(w, mention("what changed?", TS));
  assert.equal(w.humagen.asked[0]?.agent, "", "the app picks nobody itself");
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.completed", { text: "ok" });
  await w.app.idle();
});

test("with no default anywhere, the person is asked to tag an agent, for example one this app may reach", async () => {
  const w = world({ defaultAgent: undefined });
  w.humagen.refuse = new HumagenError(
    409,
    "tag an agent: nothing answers in the workspace by default",
  );
  await deliver(w, mention("what changed?", TS));
  await w.app.idle();
  assert.equal(w.humagen.runs.size, 0);
  const said = textIn(blocksOf(w.slack.of("chat.postMessage")[0]));
  assert.match(said, new RegExp(defaultCopy.auto.tagOne("helper")));
  assert.ok(
    !said.includes("nothing answers in the workspace"),
    "the platform's words are not shown",
  );
  collect("tag an agent", w.slack);

  // A named agent that is refused for another reason is not told to tag one.
  const v = world();
  v.humagen.refuse = new HumagenError(409, "conflict");
  await deliver(v, mention("@helper hi", TS));
  await v.app.idle();
  assert.ok(!textIn(blocksOf(v.slack.of("chat.postMessage")[0])).includes("Tag an agent"));
});

test("a run a limit refuses is shown in humagen's words, and humagen's are kept in the log", async () => {
  const w = world();
  w.humagen.refuse = new HumagenError(429, "the workspace reached 200 runs today", {
    limit: "runs",
    resets_at: new Date(Date.now() + 3_600_000).toISOString(),
  });
  await deliver(w, mention("one more", TS));
  await w.app.idle();
  assert.equal(w.humagen.runs.size, 0);
  const said = w.slack.of("chat.postMessage")[0];
  assert.equal(said?.thread_ts, TS);
  assert.match(textIn(blocksOf(said)), new RegExp(defaultCopy.budgetTitle));
  assert.ok(
    !textIn(blocksOf(said)).includes("200 runs today"),
    "the platform's words are not shown",
  );
  assert.ok(w.logs.some((l) => l.includes("200 runs today")));
  assert.ok(!/"Retry"/.test(textIn(blocksOf(said))), "there is no run to retry");
  assert.equal(w.slack.of("reactions.add").at(-1)?.name, "x");
  collect("refused", w.slack);
});

test("a message sent while a turn is running waits its turn", async () => {
  const w = world();
  await deliver(w, mention("first", TS));
  const first = w.humagen.last.id;
  await emit(w, "lifecycle.accepted");
  await deliver(w, mention("second", "1700000060.000100", { thread_ts: TS }));
  assert.equal(w.humagen.started.length, 1, "one turn at a time per thread");
  assert.equal(w.slack.of("reactions.add").at(-1)?.timestamp, "1700000060.000100");

  await emit(w, "lifecycle.completed", { text: "one" }, first);
  await settle();
  assert.equal(w.humagen.started.length, 2);
  assert.equal(w.humagen.started[1]?.after, first);
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.completed", { text: "two" });
  await w.app.idle();
});

test("stop also drops what was waiting behind the stopped turn", async () => {
  const w = world();
  await deliver(w, mention("first", TS));
  await emit(w, "lifecycle.accepted");
  await deliver(w, mention("second", "1700000060.000100", { thread_ts: TS }));
  await deliver(w, stopPressed(TS));
  await w.app.idle();
  assert.equal(w.humagen.started.length, 1);
  const marks = w.slack.of("reactions.add").filter((a) => a.timestamp === "1700000060.000100");
  assert.deepEqual(
    marks.map((a) => a.name),
    ["eyes", "black_square_for_stop"],
  );
});

test("/agent asks in a new thread, and /agent help lists who can be asked", async () => {
  const w = world();
  await deliver(w, command("what is on call this week?"));
  const root = w.slack.of("chat.postMessage")[0];
  assert.equal(root?.channel, CHANNEL);
  assert.equal(root?.text, `<@${USER}>: what is on call this week?`);
  const rootTs = "1800000001.000100";
  assert.equal(w.humagen.started[0]?.idempotencyKey, `slack-${TEAM}-${CHANNEL}-1800000001-000100`);
  assert.equal(w.slack.of("chat.startStream")[0]?.thread_ts, rootTs);
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.completed", { text: "Dana." });
  await w.app.idle();

  await deliver(w, command("help"));
  assert.equal(w.humagen.started.length, 1);
  assert.match(String(w.slack.responses[0]?.body.text), /`@lead`: Takes a request/);
  assert.equal(w.slack.responses[0]?.body.response_type, "ephemeral");

  // Where the app is not a member, it says so to the person alone.
  w.slack.failing["chat.postMessage"] = "not_in_channel";
  await deliver(w, command("hello"));
  await w.app.idle();
  assert.equal(w.humagen.started.length, 1);
  assert.match(String(w.slack.responses[1]?.body.text), /not in this channel/);
});

test("the shortcuts: a form for asking from anywhere, and a message as the question", async () => {
  const w = world();
  await deliver(w, interactive({ type: "shortcut", callback_id: "aui_ask" }));
  const opened = w.slack.of("views.open")[0];
  assert.equal((opened?.view as { callback_id: string }).callback_id, "aui_form");
  collect("ask form", w.slack);

  await deliver(
    w,
    interactive({
      type: "view_submission",
      view: {
        callback_id: "aui_form",
        private_metadata: '{"kind":"ask"}',
        state: { values: { text: { value: { value: "summarize my week" } } } },
      },
    }),
  );
  // No channel: the answer goes to the app's messages with this person.
  assert.equal(w.slack.of("chat.postMessage")[0]?.channel, USER);
  assert.equal(w.humagen.started[0]?.text, "summarize my week");
  assert.match(String(w.humagen.started[0]?.idempotencyKey), new RegExp(`^slack-${TEAM}-${DM}-`));
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.completed", { text: "Busy." });
  await w.app.idle();

  await deliver(
    w,
    interactive({
      type: "message_action",
      callback_id: "aui_ask_about",
      channel: { id: CHANNEL },
      message: { ts: "1700000500.000100", text: "is the deploy safe to ship?" },
    }),
  );
  assert.equal(w.humagen.started[1]?.text, "is the deploy safe to ship?");
  assert.equal(w.slack.of("chat.startStream").at(-1)?.thread_ts, "1700000500.000100");
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.completed", { text: "Yes." });
  await w.app.idle();
});

test("an inbox item about a run lands in its thread, and the button resolves it in humagen", async () => {
  const w = world();
  const watching = new AbortController();
  const watch = w.app.watchInbox(watching.signal);
  await deliver(w, mention("remember how we deploy", TS));
  const run = w.humagen.last.id;
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.completed", { text: "Noted." });
  await w.app.idle();

  w.humagen.raise(memoryMail(run));
  await settle();
  await w.app.idle();
  const posted = w.slack.of("chat.postMessage").at(-1);
  assert.equal(posted?.thread_ts, TS, "it is shown where the run was asked for");
  assert.match(textIn(blocksOf(posted)), /2 proposed memories for lead/);
  assert.match(textIn(blocksOf(posted)), /Deploys are frozen on Fridays/);
  assert.match(textIn(blocksOf(posted)), new RegExp(defaultCopy.keepSelected));
  const ts = "1800000002.000100";

  // Someone who did not ask for the run cannot settle it.
  const picked = {
    values: { "checkboxes:p1": { any: { selected_options: [{ value: "n1" }] } } },
  };
  await deliver(
    w,
    click(blocksOf(posted), defaultCopy.keepSelected, {
      ts,
      threadTs: TS,
      user: OTHER_USER,
      state: picked,
    }),
  );
  assert.equal(w.humagen.resolved.length, 0);
  assert.equal(w.slack.of("chat.postEphemeral")[0]?.text, defaultCopy.notYours);

  // The person who asked can: the ticked note is kept, the rest are not.
  await deliver(
    w,
    click(blocksOf(posted), defaultCopy.keepSelected, { ts, threadTs: TS, state: picked }),
  );
  await w.app.idle();
  assert.deepEqual(w.humagen.resolved, [
    { id: "m-memory", path: "parts/p1/resolve", body: { action: "keep", ids: ["n1"] } },
  ]);
  const updated = w.slack.of("chat.update").at(-1);
  assert.equal(updated?.ts, ts);
  assert.ok(
    !textIn(blocksOf(updated)).includes(defaultCopy.keepSelected),
    "the buttons become a record",
  );
  assert.match(textIn(blocksOf(updated)), /Kept 1 of 2/);
  collect("inbox in thread", w.slack);

  watching.abort();
  await watch;
});

// The platform sends a run's thread beside the run on a result, an artifact,
// a recap and a draft: the message still lands where the run was asked for.
test("an inbox item about a run in a humagen thread lands in the run's Slack thread", async () => {
  const w = world();
  const watching = new AbortController();
  const watch = w.app.watchInbox(watching.signal);
  await deliver(w, mention("check the deploy", TS));
  const run = w.humagen.last.id;
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.completed", { text: "On it." });
  await w.app.idle();
  const before = w.slack.of("chat.postMessage").length;

  w.humagen.raise(resultMail(run));
  await settle();
  await w.app.idle();
  const posted = w.slack.of("chat.postMessage");
  assert.equal(posted.length, before + 1, "the result is posted");
  assert.equal(posted.at(-1)?.thread_ts, TS, "it is shown where the run was asked for");
  assert.match(textIn(blocksOf(posted.at(-1))), /The deploy is green/);

  watching.abort();
  await watch;
});

test("the Home tab shows a person what waits on them, and an operator the whole inbox", async () => {
  const w = world({ operators: [OTHER_USER] });
  await deliver(w, mention("deploy it", TS));
  const run = w.humagen.last.id;
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.failed", { error: "provider: 529 overloaded", terminal: true });
  await w.app.idle();
  w.humagen.mailbox.set("m-failure", failureMail(run));
  w.humagen.mailbox.set("m-other", {
    ...failureMail("api-someone-else"),
    id: "m-other",
    subject: "releaser failed",
  });

  await deliver(w, homeOpened(USER));
  const mine = w.slack.of("views.publish")[0];
  assert.equal(mine?.user_id, USER);
  const view = mine?.view as { type: string; blocks: unknown[] };
  assert.equal(view.type, "home");
  assert.match(textIn(view.blocks), /lead failed/);
  assert.ok(!/releaser failed/.test(textIn(view.blocks)), "another person's run is not shown");

  // An operator sees everything the app may read, and can settle it there.
  w.humagen.mailbox.set("m-alert", alertMail());
  w.humagen.mailbox.set("m-budget", budgetMail());
  await deliver(w, homeOpened(OTHER_USER));
  const all = w.slack.of("views.publish")[1]?.view as { blocks: unknown[] };
  assert.match(textIn(all.blocks), /releaser failed/);
  assert.match(textIn(all.blocks), /6 of 10 runs failed/);
  assert.match(textIn(all.blocks), /Project Launch reached its daily run limit/);

  const home = click(all.blocks, defaultCopy.acknowledge, { ts: "", user: OTHER_USER });
  (home.payload as Record<string, unknown>).container = { type: "view" };
  (home.payload as Record<string, unknown>).view = { type: "home" };
  delete (home.payload as Record<string, unknown>).message;
  await deliver(w, home);
  await w.app.idle();
  assert.deepEqual(w.humagen.resolved.at(-1), {
    id: "m-alert",
    path: "parts/p1/resolve",
    body: { action: "acknowledge" },
  });
  const republished = w.slack.of("views.publish").at(-1);
  assert.equal(republished?.user_id, OTHER_USER);
  assert.equal(
    w.slack.of("views.publish").length,
    3,
    "the tab is published again with the outcome",
  );
  collect("home", w.slack);
});

test("a rating opens a comment modal and is saved even when skipped", async () => {
  const w = mapped();
  await deliver(w, mention("quick", TS));
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.completed", { text: "Done." });
  await w.app.idle();
  const final = w.slack.of("chat.stopStream")[0];
  const before = w.slack.calls.length;
  await deliver(
    w,
    click(blocksOf(final), defaultCopy.notHelpful, { ts: String(final?.ts), threadTs: TS }),
  );
  await w.app.idle();
  assert.equal(w.humagen.feedback.length, 1);
  assert.equal(w.humagen.feedback[0]?.body.rating, "down");
  const modal = w.slack.of("views.open").at(-1)?.view as {
    callback_id: string;
    private_metadata: string;
  };
  assert.equal(modal.callback_id, "aui.feedback.modal");
  assert.ok(w.slack.calls.length > before);
  await deliver(
    w,
    interactive({
      type: "view_submission",
      user: { id: USER },
      view: {
        ...modal,
        state: {
          values: {
            comment: { value: { value: "Needs sources" } },
            reason: { value: { selected_option: { value: "incomplete" } } },
          },
        },
      },
    }),
  );
  assert.equal(w.humagen.feedback.at(-1)?.body.comment, "Needs sources");
  assert.equal(w.humagen.feedback.at(-1)?.body.reason, "incomplete");
  collect("feedback", w.slack);
});

test("like also opens the comment modal and saves the positive rating", async () => {
  const w = mapped();
  await deliver(w, mention("quick", TS));
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.completed", { text: "Done." });
  await w.app.idle();
  const final = w.slack.of("chat.stopStream")[0];
  await deliver(
    w,
    click(blocksOf(final), defaultCopy.helpful, { ts: String(final?.ts), threadTs: TS }),
  );
  assert.equal(w.humagen.feedback[0]?.body.rating, "up");
  assert.equal(w.slack.of("views.open").length, 1);
  collect("positive feedback", w.slack);
});

test("a file-only DM is ingested through its verified surface message", async () => {
  const w = mapped();
  await deliver(w, direct("", TS, { subtype: "file_share", files: [{ id: "F01" }] }));
  assert.equal(w.humagen.posts.length, 1);
  assert.equal(w.humagen.posts[0]?.surface.files, true);
  assert.match(w.humagen.posts[0]?.text ?? "", /attached files/);
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.completed", { text: "Read the file." });
  await w.app.idle();
});

test("configured skills are registered, listed and invoked; unknown skills start nothing", async () => {
  const w = mapped();
  const client = w.humagen.clients.get(CLIENT);
  assert.ok(client?.slack);
  client.slack.commands = [{ name: "summarize", description: "Summarize a topic" }];
  await deliver(w, command("", { command: "/skill" }));
  assert.match(String(w.slack.responses.at(-1)?.body.text), /summarize/);
  await deliver(w, command("missing task", { command: "/skill" }));
  assert.equal(w.humagen.started.length, 0);
  await deliver(w, mention("hello", TS));
  assert.equal(w.slack.of("agents.conversations.setCommands").length, 1);
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.completed", { text: "Done." });
  await w.app.idle();
  await deliver(w, command("summarize this topic", { command: "/skill" }));
  assert.match(w.humagen.last.text, /summarize this topic/);
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.completed", { text: "Summary." });
  await w.app.idle();
});

test("account controls load privately and submit as the mapped person", async () => {
  const w = mapped();
  await deliver(w, command("connections"));
  assert.equal(w.slack.of("views.open").length, 1);
  assert.equal(w.slack.of("views.update").length, 1);
  for (const operation of ["connect", "disconnect"]) {
    await deliver(
      w,
      interactive({
        type: "view_submission",
        user: { id: USER },
        view: {
          callback_id: "warp.connections",
          state: {
            values: {
              provider: { value: { value: "github" } },
              reference: { value: { value: "env:HUMAGEN_PERSON_ANA_GITHUB" } },
              operation: { value: { selected_option: { value: operation } } },
            },
          },
        },
      }),
    );
  }
  assert.equal(w.humagen.connectionCalls.length, 2);
  assert.deepEqual(w.humagen.connectionCalls[0]?.as, { client: CLIENT, user: USER });
  collect("connections", w.slack);
});

test("a finished answer publishes as a canvas only in its own channel", async () => {
  const w = mapped();
  await deliver(w, mention("Explain", TS));
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.completed", { text: "The answer." });
  await w.app.idle();
  await deliver(w, command(`canvas ${w.humagen.last.id}`, { channel_id: "C0OTHER" }));
  assert.equal(w.slack.of("canvases.create").length, 0);
  await deliver(w, command(`canvas ${w.humagen.last.id}`));
  assert.equal(w.slack.of("canvases.create").length, 1);
  assert.deepEqual(w.slack.of("canvases.access.set")[0]?.channel_ids, [CHANNEL]);
});

test(
  "every Block Kit payload these tests produced is one Slack accepts",
  { skip: !process.env.SLACK_VALIDATE },
  async () => {
    assert.ok(produced.length > 10);
    const failed: string[] = [];
    for (const p of produced) {
      const res = await validateBlocks(p.blocks ?? [], p.view);
      if (!res.ok) failed.push(`${p.from}: ${JSON.stringify(res.errors ?? res.error)}`);
    }
    assert.deepEqual(failed, []);
  },
);

test("text the app repeats as itself can mention a person or channel but never notify everyone", () => {
  for (const ping of [
    "<!channel>",
    "<!here>",
    "<!everyone>",
    "<!subteam^S0123ABC|@oncall>",
    "<!date^1^x|y>",
  ]) {
    const out = echoed(`hi ${ping} there`);
    assert.ok(!out.includes("<!"), `${ping} passed through as ${out}`);
  }
  // A form value arrives raw, command text entity-encoded: both end up the same.
  assert.equal(echoed("<!channel> deploy"), "&lt;!channel&gt; deploy");
  assert.equal(echoed("&lt;!channel&gt; deploy"), "&lt;!channel&gt; deploy");
  assert.equal(echoed("<https://evil.example|bank>"), "&lt;https://evil.example|bank&gt;");
  assert.equal(
    echoed("ask <@U0ABCDEF> in <#C0ABCDEF|ops> & <#C0ABCDEF>"),
    "ask <@U0ABCDEF> in <#C0ABCDEF|ops> &amp; <#C0ABCDEF>",
  );
  assert.equal(echoed("a < b > c"), "a &lt; b &gt; c");
});

test("asking from the shortcut form cannot make the app notify a whole channel", async () => {
  const w = world();
  await deliver(
    w,
    interactive({
      type: "view_submission",
      view: {
        callback_id: "aui_form",
        private_metadata: '{"kind":"ask"}',
        state: { values: { text: { value: { value: "<!channel> what changed?" } } } },
      },
    }),
  );
  const root = w.slack.of("chat.postMessage")[0];
  assert.ok(root, "the question was posted");
  assert.ok(!String(root?.text).includes("<!channel>"), String(root?.text));
  // End the run, or the app keeps following it and the test never finishes.
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.completed", { text: "Nothing yet." });
  await w.app.idle();
});

// Questions a run waits on.

const CLIENT = "app";
/** A world whose app is a client that maps USER to ana and OTHER_USER to olu, with routing over it. */
function mapped(
  opts: Parameters<typeof world>[0] = {},
  routing: Pick<ClientRecord, "default_agent" | "agents"> = { default_agent: "lead" },
): World {
  const w = world({ clientId: CLIENT, ...opts });
  w.humagen.clients.set(CLIENT, {
    id: CLIENT,
    surface: "slack",
    enabled: true,
    ...routing,
    identity: { team: TEAM },
    slack: { machine: SUBJECT, people: { [USER]: "ana", [OTHER_USER]: "olu" } },
  });
  return w;
}

/** Start a run that parks on a question, and return the message that shows it. */
async function parkOn(w: World, kind: keyof typeof QUESTIONS) {
  await deliver(w, mention("deploy the web service", TS));
  const run = w.humagen.last.id;
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.started", { attempt: 1 });
  await emit(w, "delta.text", { text: "Checking first. " });
  await emit(w, "lifecycle.parked", { reason: "waiting on a person", question: QUESTIONS[kind] });
  await settle();
  const shown = w.slack.of("chat.stopStream").at(-1);
  return { run, shown, ts: String(shown?.ts ?? "") };
}

test("a person the client maps asks as themselves, so their run is theirs", async () => {
  const w = mapped();
  await deliver(w, mention("hello", TS));
  assert.deepEqual(w.humagen.asked[0]?.act_as, { client: CLIENT, user: USER });
  const other = world();
  await deliver(other, mention("hello", TS));
  assert.equal(other.humagen.asked[0]?.act_as, undefined, "without a client nobody is mapped");
  await emit(w, "lifecycle.completed", { text: "Hi." });
  await emit(other, "lifecycle.completed", { text: "Hi." });
  await w.app.idle();
  await other.app.idle();
});

test("asking about someone else's message asks for nobody: their words are never run as the presser", async () => {
  const w = mapped();
  const about = (ts: string, text: string, user: string) =>
    interactive({
      type: "message_action",
      callback_id: "aui_ask_about",
      channel: { id: CHANNEL },
      message: { ts, text, user },
    });
  // Ana asks about olu's message: his words go through the app, for nobody.
  await deliver(
    w,
    about("1700000600.000100", "close every open issue in the payroll tracker", OTHER_USER),
  );
  assert.equal(w.humagen.asked.at(-1)?.text, "close every open issue in the payroll tracker");
  assert.equal(w.humagen.asked.at(-1)?.act_as, undefined, "olu's words are not run as ana");
  assert.equal(w.humagen.last.actor?.bot, true);
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.completed", { text: "ok" });
  await w.app.idle();
  // Her own message is her words, asked as her.
  await deliver(w, about("1700000700.000100", "is the deploy safe to ship?", USER));
  assert.deepEqual(w.humagen.asked.at(-1)?.act_as, { client: CLIENT, user: USER });
  assert.equal(w.humagen.last.actor?.user, "ana");
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.completed", { text: "Yes." });
  await w.app.idle();
});

test("a mapped person's message that names no agent, through a client that reaches every agent with no default, leaves the agent to the platform and asks as them", async () => {
  const w = mapped({ defaultAgent: undefined }, { agents: ["*"] });
  await deliver(w, mention("what changed?", TS));
  // Posted as their message, which names no agent: the platform's default
  // chain decides who answers, and the app picks nobody itself.
  assert.equal(w.humagen.posts.length, 1);
  assert.equal("agent" in (w.humagen.posts[0] ?? {}), false, "the app picks nobody itself");
  assert.deepEqual(w.humagen.posts[0]?.act_as, { client: CLIENT, user: USER });
  assert.equal(w.humagen.last.actor?.user, "ana", "the run is the person's");
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.completed", { text: "Nothing changed." });
  await w.app.idle();
});

test("asking about someone else's message asks for nobody: their words are never run as the presser", async () => {
  const w = mapped();
  const about = (ts: string, text: string, user: string) =>
    interactive({
      type: "message_action",
      callback_id: "aui_ask_about",
      channel: { id: CHANNEL },
      message: { ts, text, user },
    });
  // Ana asks about olu's message: his words go through the app, for nobody.
  await deliver(
    w,
    about("1700000600.000100", "close every open issue in the payroll tracker", OTHER_USER),
  );
  assert.equal(w.humagen.asked.at(-1)?.text, "close every open issue in the payroll tracker");
  assert.equal(w.humagen.asked.at(-1)?.act_as, undefined, "olu's words are not run as ana");
  assert.equal(w.humagen.last.actor?.bot, true);
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.completed", { text: "ok" });
  await w.app.idle();
  // Her own message is her words, asked as her.
  await deliver(w, about("1700000700.000100", "is the deploy safe to ship?", USER));
  assert.deepEqual(w.humagen.asked.at(-1)?.act_as, { client: CLIENT, user: USER });
  assert.equal(w.humagen.last.actor?.user, "ana");
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.completed", { text: "Yes." });
  await w.app.idle();
});

test("in a bound channel, someone the client maps to nobody is told to be connected, and humagen is never asked", async () => {
  const w = mapped();
  w.humagen.bound.set(CLIENT, [{ slack_channel: CHANNEL, project: "Field", channel: "general" }]);
  await deliver(w, mention("@helper what changed?", TS, { user: "U09NOBODY" }));
  await w.app.idle();
  assert.equal(w.humagen.asked.length, 0, "no run is asked for around the channel's own rules");
  assert.equal(w.humagen.runs.size, 0);
  assert.match(
    textIn(blocksOf(w.slack.of("chat.postMessage")[0])),
    new RegExp(defaultCopy.threads.unmappedInBound.slice(0, 40)),
  );
  collect("unmapped in a bound channel", w.slack);
  // The same person in a channel that is not bound asks through the app, as before.
  const v = mapped();
  await deliver(v, mention("@helper what changed?", TS, { user: "U09NOBODY" }));
  assert.equal(v.humagen.asked.length, 1);
  await emit(v, "lifecycle.completed", { text: "ok" });
  await v.app.idle();
});

test("when humagen cannot say whether a channel is bound, nobody is answered around it, and the next mention asks again", async () => {
  const w = mapped();
  w.humagen.bound.set(CLIENT, [{ slack_channel: CHANNEL, project: "Field", channel: "general" }]);
  const bindings = w.humagen.bindings.bind(w.humagen);
  let down = true;
  w.humagen.bindings = async (client: string) => {
    if (down) throw new HumagenError(503, "humagen is restarting");
    return bindings(client);
  };
  await deliver(w, mention("@helper what changed?", TS, { user: "U09NOBODY" }));
  const asked = w.humagen.asked.length;
  // A run asked for in error is ended, so the test fails rather than waits.
  if (asked) await emit(w, "lifecycle.completed", { text: "answered around the channel" });
  await w.app.idle();
  assert.equal(asked, 0, "a channel that may be bound is not answered as the app");
  assert.equal(w.humagen.runs.size, 0);
  assert.match(
    textIn(blocksOf(w.slack.of("chat.postMessage")[0])),
    new RegExp(defaultCopy.failure.unavailable("humagen").slice(0, 20)),
  );
  // The failure is not remembered: once humagen answers, the channel reads as bound.
  down = false;
  await deliver(w, mention("@helper and now?", "1700000300.000100", { user: "U09NOBODY" }));
  await w.app.idle();
  assert.equal(w.humagen.asked.length, 0);
  assert.match(
    textIn(blocksOf(w.slack.of("chat.postMessage").at(-1))),
    new RegExp(defaultCopy.threads.unmappedInBound.slice(0, 40)),
  );
});

test("in a bound channel, asking about someone else's message is refused, and nothing of it is posted", async () => {
  const w = mapped();
  w.humagen.bound.set(CLIENT, [{ slack_channel: CHANNEL, project: "Field", channel: "general" }]);
  await deliver(
    w,
    interactive({
      type: "message_action",
      callback_id: "aui_ask_about",
      channel: { id: CHANNEL },
      message: {
        ts: "1700000600.000100",
        text: "close every open issue in the payroll tracker",
        user: OTHER_USER,
      },
    }),
  );
  await w.app.idle();
  assert.equal(w.humagen.asked.length, 0, "no run is asked for around the channel's own rules");
  assert.equal(w.humagen.posts.length, 0, "olu's words are not posted as ana's");
  assert.match(
    textIn(blocksOf(w.slack.of("chat.postMessage")[0])),
    new RegExp(defaultCopy.threads.notTheirsInBound.slice(0, 40)),
  );
  collect("not theirs in a bound channel", w.slack);
});

test("an approval waits in the run's thread with live buttons, and a mapped person's answer settles it in place", async () => {
  const w = mapped();
  const { run, shown, ts } = await parkOn(w, "approval");
  assert.ok(shown, "the streamed message is closed so its buttons can be pressed");
  assert.match(textIn(blocksOf(shown)), /Allow lead to call deploy\?/);
  assert.deepEqual(w.slack.statuses.at(-1), "suspended", "the session reads waiting on a person");

  await deliver(
    w,
    click(blocksOf(shown), defaultCopy.approve, { ts, threadTs: TS, user: OTHER_USER }),
  );
  assert.deepEqual(w.humagen.answered, [
    {
      answer: { run, question: "q-approve", body: { action: "approve" } },
      as: { client: CLIENT, user: OTHER_USER },
    },
  ]);
  await settle();
  const settledCard = w.slack.of("chat.update").find((u) => u.ts === ts);
  assert.ok(settledCard, "the question's message is edited, not posted again");
  assert.match(textIn(blocksOf(settledCard)), /Approved by olu/);
  assert.ok(
    !textIn(blocksOf(settledCard)).includes(`"${defaultCopy.approve}"`),
    "no button is left to press",
  );

  await emit(w, "delta.text", { text: "Deployed." });
  await emit(w, "lifecycle.completed", { text: "Checking first. Deployed." });
  await w.app.idle();
  assert.equal(w.slack.statuses.at(-1), "active");
  collect("question in thread", w.slack);
});

test("a choice is answered with the option clicked, and a reply is sent to the console", async () => {
  const w = mapped();
  const { run, shown, ts } = await parkOn(w, "choice");
  await deliver(w, click(blocksOf(shown), "eu-west", { ts, threadTs: TS, user: OTHER_USER }));
  assert.deepEqual(w.humagen.answered.at(-1)?.answer, {
    run,
    question: "q-choose",
    body: { action: "choose", choice: "eu-west" },
  });
  await settle();
  assert.match(
    textIn(blocksOf(w.slack.of("chat.update").find((u) => u.ts === ts))),
    /olu chose eu-west/,
  );
  await emit(w, "lifecycle.completed", { text: "Done." });
  await w.app.idle();

  const r = mapped();
  const reply = await parkOn(r, "reply");
  await deliver(
    r,
    click(blocksOf(reply.shown), defaultCopy.somethingElse, {
      ts: reply.ts,
      threadTs: TS,
      user: OTHER_USER,
    }),
  );
  assert.equal(r.humagen.answered.length, 0);
  assert.equal(r.slack.of("chat.postEphemeral").at(-1)?.text, words.replyInConsole);
  await deliver(r, stopPressed(TS));
  await r.app.idle();
});

test("a Slack person the client does not map cannot answer, and humagen is never asked", async () => {
  const w = mapped();
  const { shown, ts } = await parkOn(w, "approval");
  await deliver(
    w,
    click(blocksOf(shown), defaultCopy.approve, { ts, threadTs: TS, user: "U0NOBODY1" }),
  );
  assert.equal(w.humagen.answered.length, 0);
  assert.equal(w.slack.of("chat.postEphemeral").at(-1)?.text, defaultCopy.noPermission);
  // Without a client, nobody is mapped, so nobody answers from Slack.
  const bare = world();
  const b = await parkOn(bare, "approval");
  await deliver(bare, click(blocksOf(b.shown), defaultCopy.approve, { ts: b.ts, threadTs: TS }));
  assert.equal(bare.humagen.answered.length, 0);
  for (const x of [w, bare]) {
    await deliver(x, stopPressed(TS));
    await x.app.idle();
  }
});

test("an answer humagen refuses is shown in humagen's words, and the card keeps its buttons", async () => {
  const w = mapped();
  const { shown, ts } = await parkOn(w, "approval");
  // ana asked for the run, so she may not approve its call.
  w.humagen.refuseAnswer = new HumagenError(
    403,
    "platform: forbidden: ana may not answer this question",
  );
  await deliver(w, click(blocksOf(shown), defaultCopy.approve, { ts, threadTs: TS, user: USER }));
  assert.equal(w.slack.of("chat.postEphemeral").at(-1)?.text, defaultCopy.noPermission);
  assert.ok(w.logs.some((l) => l.includes("may not answer")));
  assert.equal(w.slack.of("chat.update").filter((u) => u.ts === ts).length, 0);
  await deliver(w, stopPressed(TS));
  await w.app.idle();
});

test("an approval of a call that acts as the person says so on its Slack card, and offers no edit", async () => {
  const w = mapped();
  await deliver(w, mention("file the flaky test", TS));
  await emit(w, "lifecycle.accepted");
  await emit(w, "lifecycle.started", { attempt: 1 });
  const tool = { ...QUESTIONS.approval.tool, acts_for_person: true };
  await emit(w, "lifecycle.parked", {
    reason: "waiting on a person",
    question: { ...QUESTIONS.approval, tool },
  });
  await settle();
  const shown = textIn(blocksOf(w.slack.of("chat.stopStream").at(-1)));
  assert.ok(shown.includes(defaultCopy.actsForPerson), shown);
  assert.match(shown, new RegExp(`"${defaultCopy.approve}"`));
  assert.doesNotMatch(shown, new RegExp(`"${defaultCopy.edit}"`));
  collect("approval that acts as the person", w.slack);
  await deliver(w, stopPressed(TS));
  await w.app.idle();
});

test("a question in the inbox is not posted again in the thread: the run already shows it", async () => {
  const w = mapped();
  const watching = new AbortController();
  const watch = w.app.watchInbox(watching.signal);
  const { run } = await parkOn(w, "approval");
  const posts = w.slack.of("chat.postMessage").length;
  w.humagen.raise(questionMail(run, "approval"));
  await settle();
  assert.equal(w.slack.of("chat.postMessage").length, posts);
  await deliver(w, stopPressed(TS));
  await w.app.idle();
  watching.abort();
  await watch;
});
