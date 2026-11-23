import assert from "node:assert/strict";
import { test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { TURNED_AWAY_BURST, TURNED_AWAY_MS } from "./app.tsx";
import {
  CHANNEL,
  click,
  command,
  DM,
  direct,
  interactive,
  mappedWorld,
  mention,
  OTHER_TEAM,
  said,
  settle,
  TEAM,
  USER,
  type World,
} from "./harness.ts";

// Deny by default: a Slack message, command, shortcut or button starts
// something only for a person the app's client maps. Anyone else (a new
// member, a guest, someone from another company in a shared channel) is told
// once, privately, and nothing is posted, kept or run for them.

const TS = "1700000000.000100";
const STRANGER = "U09STRANGER";
const NOT_CONNECTED = defaultCopy.threads.notConnected("Humagen");

async function deliver(w: World, envelope: Parameters<World["app"]["handle"]>[0]) {
  await w.app.handle(envelope);
  await settle();
  await w.app.idle();
}

/** Nothing reached humagen for anyone: no run, no message, no answer, no mode. */
function nothingAsked(w: World) {
  assert.equal(w.humagen.asked.length, 0, "no run is asked for");
  assert.equal(w.humagen.posts.length, 0, "no message is posted");
  assert.equal(w.humagen.answered.length, 0, "nothing is answered");
  assert.equal(w.humagen.modes.length, 0, "no mode is set");
  assert.deepEqual(w.humagen.resolved, [], "nothing is settled");
}

/** What the app said to the stranger: one private reply, and nothing else in Slack. */
function toldOnce(w: World, where: { channel: string; thread_ts?: string }) {
  const told = w.slack.of("chat.postEphemeral");
  assert.equal(told.length, 1);
  assert.deepEqual(told[0], { ...where, user: STRANGER, text: NOT_CONNECTED });
  for (const m of [
    "chat.postMessage",
    "chat.startStream",
    "reactions.add",
    "views.open",
    "agents.sessions.setStatus",
  ])
    assert.ok(!w.slack.methods.includes(m), `no ${m}`);
}

test("a direct message from someone the client maps to nobody starts nothing, and they are told there", async () => {
  const w = mappedWorld();
  await deliver(w, direct("delete the prod database", TS, { user: STRANGER }));
  nothingAsked(w);
  toldOnce(w, { channel: DM });
});

test("a mention in a channel bound to nothing, from someone the client maps to nobody, starts nothing", async () => {
  const w = mappedWorld();
  await deliver(w, mention("@helper summarize the payroll sheet", TS, { user: STRANGER }));
  nothingAsked(w);
  toldOnce(w, { channel: CHANNEL });
});

test("in a mapped person's session, a stranger's reply starts nothing: a mention is told, a plain reply is not answered at all", async () => {
  const w = mappedWorld();
  await w.app.handle(mention("what changed?", TS));
  await settle();
  const run = w.humagen.last.id;
  w.humagen.emit(run, "run.accepted.v1");
  w.humagen.emit(run, "run.completed.v1", { text: "Three commits." });
  await settle();
  await w.app.idle();
  const posts = w.humagen.posts.length;
  const runs = w.humagen.runs.size;
  const before = w.slack.calls.length;

  // A mention in the thread: told privately, in the thread.
  await deliver(
    w,
    mention("now run it as me", "1700000100.000100", { thread_ts: TS, user: STRANGER }),
  );
  // A reply that mentions nobody: not listened to, and not answered.
  await deliver(
    w,
    said("and drop the backups", "1700000200.000100", { thread_ts: TS, user: STRANGER }),
  );

  assert.equal(w.humagen.posts.length, posts, "nothing more is posted into the session");
  assert.equal(w.humagen.runs.size, runs, "no run starts");
  const after = w.slack.calls.slice(before);
  assert.deepEqual(
    after.map((c) => c.method),
    ["chat.postEphemeral"],
  );
  assert.deepEqual(after[0]?.args, {
    channel: CHANNEL,
    thread_ts: TS,
    user: STRANGER,
    text: NOT_CONNECTED,
  });
});

test("someone from another company in a shared channel is nobody, even with an id the client maps", async () => {
  const w = mappedWorld();
  // Slack Connect: the delivery is this team's, the sender's team is not.
  await deliver(w, mention("what changed?", TS, { user: USER, user_team: OTHER_TEAM }));
  await deliver(
    w,
    direct("what changed?", "1700000100.000100", { user: USER, user_team: OTHER_TEAM }),
  );
  nothingAsked(w);
  const told = w.slack.of("chat.postEphemeral");
  assert.equal(told.length, 1, "told once, however many times they ask");
  assert.equal(told[0]?.user, USER);
  assert.equal(told[0]?.text, NOT_CONNECTED);

  // Their button press, from a shared channel, acts on nothing either.
  const v = mappedWorld();
  await deliver(
    v,
    interactive({
      type: "block_actions",
      user: { id: USER, team_id: OTHER_TEAM },
      channel: { id: CHANNEL },
      container: { type: "message", channel_id: CHANNEL },
      message: { ts: "1800000008.000100", thread_ts: TS },
      actions: [
        { action_id: "aui.retry#0", value: JSON.stringify({ turnId: "r1", target: "r1" }) },
      ],
    }),
  );
  nothingAsked(v);
  assert.equal(v.slack.of("chat.postEphemeral")[0]?.text, NOT_CONNECTED);
});

test("a stranger's button acts on nothing: Approve, Retry, Stop and the rest are refused before anything is read", async () => {
  const w = mappedWorld();
  await w.app.handle(mention("deploy it", TS));
  await settle();
  const run = w.humagen.last.id;
  w.humagen.emit(run, "run.accepted.v1");
  w.humagen.emit(run, "run.failed.v1", { error: "provider: 529 overloaded", terminal: true });
  await settle();
  await w.app.idle();
  const final = w.slack.of("chat.stopStream").at(-1);
  const blocks = (final?.blocks ?? []) as unknown[];
  const reads = w.slack.calls.length;

  await deliver(
    w,
    click(blocks, defaultCopy.retry, { ts: String(final?.ts), threadTs: TS, user: STRANGER }),
  );
  assert.equal(w.humagen.started.length, 1, "Retry started nothing");
  // Stop, from Slack's own control on the session.
  await deliver(w, {
    type: "events_api",
    envelope_id: "env-stranger-stop",
    payload: {
      team_id: TEAM,
      event: { type: "agent_session_stopped", channel: CHANNEL, thread_ts: TS, user: STRANGER },
    },
  });
  assert.deepEqual(w.humagen.stops, [], "Stop stopped nothing");
  const after = w.slack.calls.slice(reads).map((c) => c.method);
  assert.deepEqual(after, ["chat.postEphemeral"], "told once, and Slack is asked nothing else");
});

test("a stranger's command, shortcut or form starts nothing, and posts nothing in their name", async () => {
  const w = mappedWorld();
  await deliver(w, command("delete everything", { user_id: STRANGER }));
  assert.deepEqual(w.slack.responses.at(-1)?.body, {
    response_type: "ephemeral",
    text: NOT_CONNECTED,
  });
  assert.ok(
    !w.slack.methods.includes("chat.postMessage"),
    "their words are not posted as a thread",
  );

  // The global shortcut and the form behind it: told in their messages with the app.
  const v = mappedWorld();
  await deliver(
    v,
    interactive({
      type: "shortcut",
      callback_id: "aui_ask",
      user: { id: STRANGER, team_id: TEAM },
    }),
  );
  await deliver(
    v,
    interactive({
      type: "view_submission",
      user: { id: STRANGER, team_id: TEAM },
      view: {
        callback_id: "aui_form",
        private_metadata: '{"kind":"ask"}',
        state: { values: { text: { value: { value: "summarize the payroll sheet" } } } },
      },
    }),
  );
  // Asking about a message: told in that message's thread.
  await deliver(
    v,
    interactive({
      type: "message_action",
      callback_id: "aui_ask_about",
      user: { id: STRANGER, team_id: TEAM },
      channel: { id: CHANNEL },
      message: { ts: "1700000500.000100", text: "rotate every key", user: USER },
    }),
  );
  for (const x of [w, v]) nothingAsked(x);
  assert.equal(v.slack.of("views.open").length, 0, "no form is opened for them");
  assert.deepEqual(v.slack.of("chat.postMessage"), [{ channel: STRANGER, text: NOT_CONNECTED }]);
  assert.equal(
    v.slack.of("chat.postEphemeral").length,
    0,
    "told once in ten minutes, wherever they ask",
  );
});

test("`/agent connections` still tells a stranger how to be connected", async () => {
  const w = mappedWorld();
  await deliver(w, command("connections", { user_id: STRANGER }));
  assert.equal(w.slack.of("views.open").length, 1);
  assert.match(JSON.stringify(w.slack.of("views.update").at(-1)), /connect your Slack identity/);
  assert.equal(w.slack.responses.length, 0);
});

test("the reply is told once per person in a window, and to a bounded number of people a minute", async () => {
  const w = mappedWorld();
  for (let i = 0; i < 5; i++)
    await deliver(w, direct(`try ${i}`, `17000000${10 + i}.000100`, { user: STRANGER }));
  assert.equal(w.slack.of("chat.postEphemeral").length, 1, "one reply for five messages");
  // After the window, they are told again.
  w.humagen.advance(TURNED_AWAY_MS);
  await deliver(w, direct("again", "1700000100.000100", { user: STRANGER }));
  assert.equal(w.slack.of("chat.postEphemeral").length, 2);

  // Many strangers at once: at most TURNED_AWAY_BURST replies a minute.
  const v = mappedWorld();
  for (let i = 0; i < TURNED_AWAY_BURST + 10; i++)
    await deliver(
      v,
      direct("hi", `17000002${String(i).padStart(2, "0")}.000100`, {
        user: `U0FLOOD${String(i).padStart(3, "0")}`,
      }),
    );
  assert.equal(v.slack.of("chat.postEphemeral").length, TURNED_AWAY_BURST);
  nothingAsked(v);
});

test("a person the client maps still asks everywhere: a direct message, a mention, a command and a form", async () => {
  const w = mappedWorld();
  const send = async (envelope: Parameters<World["app"]["handle"]>[0]) => {
    await w.app.handle(envelope);
    await settle();
  };
  await send(direct("hello", TS));
  await send(mention("what changed?", "1700000100.000100"));
  await send(command("who is on call?"));
  await send(
    interactive({
      type: "view_submission",
      view: {
        callback_id: "aui_form",
        private_metadata: '{"kind":"ask"}',
        state: { values: { text: { value: { value: "summarize my week" } } } },
      },
    }),
  );
  assert.deepEqual(
    w.humagen.posts.map((p) => p.text),
    ["hello", "what changed?", "who is on call?", "summarize my week"],
  );
  for (const p of w.humagen.posts) assert.deepEqual(p.act_as, { client: "app", user: USER });
  assert.equal(w.humagen.runs.size, 4);
  assert.equal(w.slack.of("chat.postEphemeral").length, 0, "nobody is told they are not connected");
  for (const r of w.humagen.runs.values()) {
    w.humagen.emit(r.id, "run.accepted.v1");
    w.humagen.emit(r.id, "run.completed.v1", { text: "ok" });
  }
  await settle();
  await w.app.idle();
});
