import assert from "node:assert/strict";
import { test } from "node:test";
import { defaultCopy } from "@humagen/components";
import type { AppOptions } from "./app.tsx";
import {
  CodeChannels,
  channelTitle,
  codeChannelWords,
  sessionId,
  TITLE_MAX,
} from "./codechannel-open.ts";
import { readConfig } from "./config.ts";
import {
  BOT,
  CHANNEL,
  click,
  DM,
  FakeHumagen,
  FakeSlack,
  interactive,
  mention,
  OTHER_USER,
  SUBJECT,
  settle,
  TEAM,
  USER,
  type World,
  world,
} from "./harness.ts";
import { words } from "./host.ts";
import { runId } from "./keys.ts";
import { validateBlocks } from "./validate.ts";

const TS = "1700000000.000100";
const KEY = `slack-${TEAM}-${CHANNEL}-1700000000-000100`;
const BUTTON = defaultCopy.continueInCodeChannel;
const OPERATOR = "U03ABCDEF";

/**
 * A Slack with code channels: `agents.conversations.create` gives one channel
 * per session id, and `conversations.info` says what each channel is and the
 * thread it was opened from, as Slack records it.
 */
class CodeSlack extends FakeSlack {
  /** Each code channel, and the message it was opened from when it has one. */
  readonly code = new Map<string, { channel_id: string; ts: string } | null>();
  private readonly bySession = new Map<string, string>();

  override async call<T>(method: string, args: Record<string, unknown> = {}): Promise<T> {
    const base = await super.call<T>(method, args);
    if (method === "agents.conversations.create") {
      const session = String(args.session_id);
      let channel = this.bySession.get(session);
      if (!channel) {
        channel = `C0CODE${this.bySession.size + 1}`;
        this.bySession.set(session, channel);
        this.code.set(channel, {
          channel_id: String(args.origin_channel_id),
          ts: String(args.origin_message_ts),
        });
      }
      return { ok: true, channel_id: channel } as T;
    }
    if (method === "conversations.info") {
      const id = String(args.channel);
      const origin = this.code.get(id);
      const properties =
        origin === undefined
          ? {}
          : {
              record_channel: { record_type: "agent_channel" },
              ...(origin ? { agent_session: { origin_link: origin } } : {}),
            };
      return { ok: true, channel: { id, properties } } as T;
    }
    return base;
  }
}

type CodeWorld = World & { slack: CodeSlack };

/** People the app's client maps: only they ask, and only they press. */
const PEOPLE = { [USER]: "ana", [OTHER_USER]: "olu", [OPERATOR]: "oz" };

function codeWorld(opts: Partial<AppOptions> = {}): CodeWorld {
  const slack = new CodeSlack();
  const w = world({ clientId: "app", ...opts }, { slack, humagen: new FakeHumagen() }) as CodeWorld;
  w.humagen.clients.set("app", {
    id: "app",
    surface: "slack",
    enabled: true,
    agents: ["*"],
    identity: { team: TEAM },
    slack: { machine: SUBJECT, people: PEOPLE },
  });
  return w;
}

/**
 * Ana asks about olu's message. A person's own message is posted into the
 * session its Slack thread is, which humagen keys, so no code channel is
 * offered under it; someone else's words are asked through the app, which
 * keys the conversation by the thread, and that one can move.
 */
const askAbout = (text: string, ts: string, channel = CHANNEL, threadTs?: string) =>
  interactive({
    type: "message_action",
    callback_id: "aui_ask_about",
    channel: { id: channel },
    message: { ts, text, user: OTHER_USER, ...(threadTs ? { thread_ts: threadTs } : {}) },
  });

const on = (opts: Partial<AppOptions> = {}) => codeWorld({ codeChannels: true, ...opts });

async function deliver(w: World, envelope: Parameters<World["app"]["handle"]>[0], app = w.app) {
  await app.handle(envelope);
  await settle();
}

async function emit(w: World, kind: string, payload?: unknown, run = w.humagen.last.id) {
  w.humagen.emit(run, kind, payload);
  await settle();
}

const blocksOf = (args: Record<string, unknown> | undefined) => (args?.blocks ?? []) as unknown[];
const labels = (blocks: unknown[]) =>
  (blocks as { type: string; elements?: { type: string; text?: { text: string } }[] }[])
    .filter((b) => b.type === "actions")
    .flatMap((b) => b.elements ?? [])
    .map((e) => e.text?.text);

/** Ask, answer, and return the run and the message that holds the answer. */
async function answered(
  w: World,
  envelope = askAbout("fix the flaky login test", TS),
  app = w.app,
) {
  await deliver(w, envelope, app);
  const run = w.humagen.last.id;
  await emit(w, "run.accepted.v1", { rev: 1 }, run);
  await emit(w, "run.delta.text.v1", { text: "It fails on a race." }, run);
  await emit(w, "run.completed.v1", { text: "It fails on a race." }, run);
  await app.idle();
  const final = w.slack.of("chat.stopStream").at(-1);
  return { run, final, blocks: blocksOf(final), ts: String(final?.ts) };
}

/** An answered question whose button was pressed: the thread now has a code channel. */
async function moved(w: CodeWorld) {
  const first = await answered(w);
  await deliver(w, click(first.blocks, BUTTON, { ts: first.ts, threadTs: TS }));
  await w.app.idle();
  const made = w.slack.of("agents.conversations.create").at(-1);
  const channel = [...w.slack.code.keys()].at(-1) as string;
  return { ...first, made, channel };
}

/** Every Block Kit payload these tests hand to Slack, for the validity check at the end. */
const produced: { from: string; blocks: unknown[] }[] = [];

// Off: nothing changes

test("with the setting off, an answer is what it was and Slack is asked nothing new", async () => {
  const off = codeWorld();
  const a = await answered(off);
  assert.ok(!labels(a.blocks).includes(BUTTON), "no button without the setting");
  assert.deepEqual(off.slack.of("conversations.info"), []);
  assert.deepEqual(off.slack.of("agents.conversations.create"), []);

  // The same exchange with the setting on differs in the one actions block, and the read of the channel.
  const lit = on();
  const b = await answered(lit);
  const without = (blocks: unknown[]) =>
    blocks.filter(
      (x) => !String((x as { block_id?: string }).block_id).startsWith("code-channel:"),
    );
  assert.deepEqual(without(b.blocks), a.blocks);
  assert.equal(b.blocks.length, a.blocks.length + 1);
  assert.deepEqual(
    lit.slack.methods.filter((m) => m !== "conversations.info"),
    off.slack.methods,
  );
});

test("with the setting off, a mention in a code channel is its own conversation, as it was", async () => {
  const w = codeWorld();
  w.slack.code.set("C0CODE9", { channel_id: CHANNEL, ts: TS });
  await answered(w);
  await answered(w, askAbout("carry on", "1700000100.000100", "C0CODE9"));
  assert.equal(w.humagen.started[1]?.idempotencyKey, `slack-${TEAM}-C0CODE9-1700000100-000100`);
  assert.equal(w.humagen.started[1]?.after, undefined);
  assert.notEqual(w.humagen.last.thread, runId("acme", SUBJECT, KEY));
  assert.deepEqual(w.slack.of("conversations.info"), []);
});

test("with the setting off, a button left from when it was on does nothing", async () => {
  const lit = on();
  const a = await answered(lit);
  // The same Slack and humagen, restarted with the setting off.
  const off = world({ clientId: "app" }, lit);
  await deliver(off, click(a.blocks, BUTTON, { ts: a.ts, threadTs: TS }));
  await off.app.idle();
  assert.deepEqual(lit.slack.of("agents.conversations.create"), []);
  assert.deepEqual(lit.slack.of("chat.postEphemeral"), []);
});

// On: the button, the channel, the acknowledgement

test("with the setting on, a finished answer offers Continue in a Code Channel", async () => {
  const w = on();
  const a = await answered(w);
  assert.ok(labels(a.blocks).includes(BUTTON));
  produced.push({ from: "the answer with the button", blocks: a.blocks });
  // Asking is not opening: nothing is created until a person presses.
  assert.deepEqual(w.slack.of("agents.conversations.create"), []);
});

test("pressing the button opens one code channel for the thread and says where it is", async () => {
  const w = on();
  const m = await moved(w);
  assert.deepEqual(m.made, {
    name: "fix the flaky login test",
    session_id: `${CHANNEL}-${TS}`,
    origin_channel_id: CHANNEL,
    origin_message_ts: TS,
  });
  const said = w.slack.of("chat.postMessage").at(-1);
  assert.deepEqual(said, {
    channel: CHANNEL,
    thread_ts: TS,
    text: `Continuing in <#${m.channel}>. Follow along there.`,
  });

  // A second press asks Slack for nothing and posts nothing: the person is told where it went.
  const posts = w.slack.of("chat.postMessage").length;
  await deliver(w, click(m.blocks, BUTTON, { ts: m.ts, threadTs: TS }));
  await w.app.idle();
  assert.equal(w.slack.of("agents.conversations.create").length, 1);
  assert.equal(w.slack.of("chat.postMessage").length, posts);
  assert.equal(w.slack.of("chat.postEphemeral").at(-1)?.text, codeChannelWords.already(m.channel));

  // After a restart the session id is the same, so Slack gives the same channel.
  const again = w.restart();
  await deliver(w, click(m.blocks, BUTTON, { ts: m.ts, threadTs: TS }), again);
  await again.idle();
  assert.equal(w.slack.of("agents.conversations.create").at(-1)?.session_id, `${CHANNEL}-${TS}`);
  assert.equal(w.slack.code.size, 1);
});

test("a message in the code channel continues the conversation the thread started", async () => {
  const w = on();
  const m = await moved(w);
  // What people said in the thread after the app was last asked there.
  w.slack.replies.set(`${CHANNEL}/${TS}`, [
    { ts: TS, user: USER, text: `<@${BOT}> fix the flaky login test` },
    { ts: m.ts, user: BOT, text: "It fails on a race." },
    { ts: "1700000050.000100", user: OTHER_USER, text: "it only fails on CI" },
  ]);
  w.slack.users.set(OTHER_USER, "olu");

  const there = "1700000100.000100";
  const next = await answered(w, mention("open a pull request", there, { channel: m.channel }));

  // One conversation: the new run follows the first, in its thread.
  const asked = w.humagen.started[1];
  assert.equal(asked?.after, m.run);
  assert.equal(w.humagen.last.thread, runId("acme", SUBJECT, KEY));
  assert.equal(asked?.idempotencyKey, `slack-${TEAM}-${m.channel}-1700000100-000100`);
  assert.equal(w.humagen.runs.size, 2);
  // The thread goes with the first turn there, once.
  assert.equal(
    asked?.text,
    [
      codeChannelWords.originOpen,
      `olu (<@${OTHER_USER}>): it only fails on CI`,
      words.historyClose,
      "",
      "open a pull request",
    ].join("\n"),
  );
  // The answer is in the code channel, and offers no second one.
  assert.equal(next.final?.channel, m.channel);
  assert.ok(!labels(next.blocks).includes(BUTTON));

  await answered(w, mention("and add a test", "1700000200.000100", { channel: m.channel }));
  assert.equal(w.humagen.started[2]?.text, "and add a test");
  assert.equal(w.humagen.started[2]?.after, next.run);
  assert.equal(w.humagen.last.thread, runId("acme", SUBJECT, KEY));
});

test("after a restart the code channel is still its thread's conversation, read from Slack", async () => {
  const w = on();
  const m = await moved(w);
  const app = w.restart();
  await answered(w, mention("carry on", "1700000100.000100", { channel: m.channel }), app);
  assert.equal(w.humagen.started[1]?.after, m.run);
  assert.equal(w.humagen.last.thread, runId("acme", SUBJECT, KEY));
  assert.deepEqual(w.slack.of("conversations.info").at(-1), { channel: m.channel });
  // The thread still answers in the thread, in the same conversation.
  await answered(w, askAbout("thanks", "1700000300.000100", CHANNEL, TS), app);
  assert.equal(w.humagen.last.thread, runId("acme", SUBJECT, KEY));
  assert.equal(w.slack.of("chat.startStream").at(-1)?.channel, CHANNEL);
});

test("a direct message can be moved by its person, and its channel gets no thread history", async () => {
  const w = on();
  const a = await answered(w, askAbout("fix the flaky login test", TS, DM));
  assert.ok(labels(a.blocks).includes(BUTTON));
  assert.deepEqual(w.slack.of("conversations.info"), [], "a direct message is never asked about");
  await deliver(w, click(a.blocks, BUTTON, { channel: DM, ts: a.ts, threadTs: TS }));
  await w.app.idle();
  assert.equal(w.slack.of("agents.conversations.create").at(-1)?.origin_channel_id, DM);
  const channel = [...w.slack.code.keys()][0] as string;
  await answered(w, mention("carry on", "1700000100.000100", { channel }));
  assert.equal(w.humagen.started[1]?.text, "carry on");
  assert.equal(w.humagen.started[1]?.after, a.run);
  assert.deepEqual(w.slack.of("conversations.replies"), []);
});

// Who may

test("only the person who asked, or an operator, moves a channel thread", async () => {
  const w = on({ operators: [OPERATOR] });
  const a = await answered(w);
  await deliver(w, click(a.blocks, BUTTON, { ts: a.ts, threadTs: TS, user: OTHER_USER }));
  await w.app.idle();
  assert.deepEqual(w.slack.of("agents.conversations.create"), []);
  assert.equal(w.slack.of("chat.postEphemeral").at(-1)?.text, codeChannelWords.notYours);
  assert.equal(w.slack.of("chat.postEphemeral").at(-1)?.user, OTHER_USER);

  await deliver(w, click(a.blocks, BUTTON, { ts: a.ts, threadTs: TS, user: OPERATOR }));
  await w.app.idle();
  assert.equal(w.slack.of("agents.conversations.create").length, 1);
});

test("a press that names a run from another place opens nothing", async () => {
  const w = on();
  const a = await answered(w);
  await deliver(w, click(a.blocks, BUTTON, { channel: "C09OTHER", ts: a.ts, threadTs: TS }));
  await w.app.idle();
  assert.deepEqual(w.slack.of("agents.conversations.create"), []);
  assert.equal(w.slack.of("chat.postEphemeral").at(-1)?.text, codeChannelWords.elsewhere);
});

test("a person the client maps posts as themselves in the thread, which is not moved", async () => {
  const w = on({ clientId: "app" });
  w.humagen.clients.set("app", {
    id: "app",
    surface: "slack",
    enabled: true,
    default_agent: "lead",
    identity: { team: TEAM },
    slack: { machine: SUBJECT, people: { [USER]: "ana" } },
  });
  const a = await answered(w, mention("fix the flaky login test", TS));
  assert.equal(w.humagen.posts.length, 1, "posted into the humagen thread the Slack thread is");
  assert.ok(!labels(a.blocks).includes(BUTTON));

  // In a code channel opened from a thread the app named, they ask through the app as themselves.
  w.slack.code.set("C0CODE9", { channel_id: "C07NAMED", ts: "1690000000.000100" });
  await answered(w, mention("carry on", "1700000100.000100", { channel: "C0CODE9" }));
  assert.equal(w.humagen.posts.length, 1);
  assert.deepEqual(w.humagen.started.at(-1)?.act_as, { client: "app", user: USER });
  assert.equal(
    w.humagen.started.at(-1)?.idempotencyKey,
    `slack-${TEAM}-C07NAMED-1690000000-000100`,
  );
});

// Refusals: the conversation stays in its thread

for (const code of ["missing_scope", "not_allowed_token_type", "feature_not_enabled"]) {
  test(`when Slack refuses with ${code}, the thread carries on and the button is not offered again`, async () => {
    const w = on();
    w.slack.failing["agents.conversations.create"] = code;
    const a = await answered(w);
    await deliver(w, click(a.blocks, BUTTON, { ts: a.ts, threadTs: TS }));
    await w.app.idle();
    assert.equal(w.slack.of("chat.postEphemeral").at(-1)?.text, codeChannelWords.refused);
    assert.deepEqual(w.slack.of("chat.postMessage"), [], "nothing is announced");
    assert.ok(w.logs.some((l) => l.includes(`code channels are not available (${code})`)));

    // The next message is answered in the thread, in the same conversation, with no button.
    const b = await answered(w, askAbout("then tell me why", "1700000100.000100", CHANNEL, TS));
    assert.equal(b.final?.channel, CHANNEL);
    assert.equal(w.humagen.started[1]?.after, a.run);
    assert.ok(!labels(b.blocks).includes(BUTTON));
  });
}

test("a refusal that may pass keeps the button, and the next press opens the channel", async () => {
  const w = on();
  w.slack.failing["agents.conversations.create"] = "ratelimited";
  const a = await answered(w);
  await deliver(w, click(a.blocks, BUTTON, { ts: a.ts, threadTs: TS }));
  await w.app.idle();
  assert.equal(w.slack.of("chat.postEphemeral").at(-1)?.text, codeChannelWords.refused);
  w.slack.failing = {};
  await deliver(w, click(a.blocks, BUTTON, { ts: a.ts, threadTs: TS }));
  await w.app.idle();
  assert.equal(w.slack.code.size, 1);
  assert.match(String(w.slack.of("chat.postMessage").at(-1)?.text), /^Continuing in <#C0CODE1>/);
});

test("a channel Slack will not describe is answered as an ordinary one, and asked about once", async () => {
  const w = on();
  w.slack.failing["conversations.info"] = "missing_scope";
  w.slack.code.set("C0CODE9", { channel_id: CHANNEL, ts: TS });
  const a = await answered(w, mention("hello", "1700000100.000100", { channel: "C0CODE9" }));
  // Today's behaviour: its own conversation, answered where it was asked.
  assert.equal(w.humagen.posts[0]?.surface.channel, "C0CODE9");
  assert.equal(w.humagen.posts[0]?.surface.ts, "1700000100.000100");
  assert.equal(a.final?.channel, "C0CODE9");
  await answered(w, mention("again", "1700000200.000100", { channel: "C0CODE9" }));
  await answered(w, mention("elsewhere", "1700000300.000100"));
  assert.equal(w.slack.of("conversations.info").length, 1);
});

test("a code channel with no thread behind it is answered as it was, and offers no other", async () => {
  const w = on();
  w.slack.code.set("C0CODE9", null);
  const a = await answered(w, mention("hello", "1700000100.000100", { channel: "C0CODE9" }));
  assert.equal(w.humagen.posts[0]?.surface.channel, "C0CODE9");
  assert.equal(w.humagen.posts[0]?.surface.ts, "1700000100.000100");
  assert.ok(!labels(a.blocks).includes(BUTTON));
});

test("a thread Slack will not give does not fail the first turn in the code channel", async () => {
  const w = on();
  const m = await moved(w);
  w.slack.failing["conversations.replies"] = "missing_scope";
  await answered(w, mention("carry on", "1700000100.000100", { channel: m.channel }));
  assert.equal(w.humagen.started[1]?.text, "carry on");
  assert.equal(w.humagen.started[1]?.after, m.run);
});

// Stop and Retry in the code channel

test("the stop button of a code channel stops the turn in flight there", async () => {
  const w = on();
  const m = await moved(w);
  await deliver(w, mention("open a pull request", "1700000100.000100", { channel: m.channel }));
  const run = w.humagen.last.id;
  await emit(w, "run.accepted.v1", { rev: 1 }, run);
  // A code channel's stop names no thread.
  await deliver(w, {
    type: "events_api",
    envelope_id: "env-stop-code",
    payload: {
      team_id: TEAM,
      event: { type: "agent_session_stopped", channel: m.channel, user: USER },
    },
  });
  assert.deepEqual(w.humagen.stops, [run]);
  await emit(w, "run.stopped.v1", {}, run);
  await w.app.idle();
  assert.equal(w.slack.of("chat.stopStream").at(-1)?.channel, m.channel);
});

test("Retry pressed in the code channel asks again there, in the same conversation", async () => {
  const w = on();
  const m = await moved(w);
  const there = "1700000100.000100";
  await deliver(w, mention("open a pull request", there, { channel: m.channel }));
  const failed = w.humagen.last.id;
  await emit(w, "run.accepted.v1", { rev: 1 }, failed);
  await emit(w, "run.failed.v1", { error: "provider: 529 overloaded", terminal: true }, failed);
  await w.app.idle();
  const card = w.slack.of("chat.stopStream").at(-1);
  await deliver(
    w,
    click(blocksOf(card), defaultCopy.retry, {
      channel: m.channel,
      ts: String(card?.ts),
      threadTs: there,
    }),
  );
  assert.equal(w.humagen.started.length, 3);
  assert.equal(w.humagen.last.thread, runId("acme", SUBJECT, KEY));
  const opened = w.slack.of("chat.startStream").at(-1);
  assert.equal(opened?.channel, m.channel);
  assert.equal(opened?.thread_ts, there);
  await emit(w, "run.accepted.v1", { rev: 1 });
  await emit(w, "run.completed.v1", { text: "Opened." });
  await w.app.idle();
});

// The pieces

test("the session id and the channel's name come from the thread and its first words", () => {
  assert.equal(sessionId({ team: TEAM, channel: CHANNEL, threadTs: TS }), `${CHANNEL}-${TS}`);
  assert.equal(channelTitle("fix the *flaky* `login` test"), "fix the flaky login test");
  assert.equal(channelTitle(`<@${BOT}>   `), codeChannelWords.untitled);
  // A mention deep in a thread: the name is the question, not the thread ahead of it.
  assert.equal(
    channelTitle([words.historyOpen, "<@U1>: hi", words.historyClose, "", "ship it"].join("\n")),
    "ship it",
  );
  const long = channelTitle(`${"refactor the session store ".repeat(8)}`);
  assert.ok(long.length <= TITLE_MAX && !long.endsWith(" "));
  assert.equal(long, "refactor the session store refactor the session store refactor the session");
});

test("a channel is read once, and a code channel names the thread it came from", async () => {
  const slack = new CodeSlack();
  slack.code.set("C0CODE9", { channel_id: CHANNEL, ts: TS });
  slack.code.set("C0CODE8", { channel_id: "not a channel", ts: "soon" });
  const code = new CodeChannels(slack, { team: TEAM, botUser: BOT, subject: SUBJECT });
  assert.deepEqual(await code.originOf("C0CODE9"), { team: TEAM, channel: CHANNEL, threadTs: TS });
  assert.deepEqual(await code.originOf("C0CODE9"), { team: TEAM, channel: CHANNEL, threadTs: TS });
  assert.equal(slack.of("conversations.info").length, 1);
  // A link that is not a channel and a timestamp is no link: it would become a run key.
  assert.deepEqual(await code.place("C0CODE8"), { code: true, origin: null });
  assert.deepEqual(await code.place(CHANNEL), { code: false });
  assert.deepEqual(await code.place(DM), { code: false });
});

test("config: code channels are off unless SLACK_CODE_CHANNELS turns them on", () => {
  const base = {
    SLACK_BOT_TOKEN: "b",
    SLACK_APP_TOKEN: "a",
    SLACK_TEAM_ID: TEAM,
    HUMAGEN_TENANT: "acme",
  };
  assert.equal(readConfig(base).slack.codeChannels, false);
  assert.equal(readConfig({ ...base, SLACK_CODE_CHANNELS: "" }).slack.codeChannels, false);
  assert.equal(readConfig({ ...base, SLACK_CODE_CHANNELS: "0" }).slack.codeChannels, false);
  assert.equal(readConfig({ ...base, SLACK_CODE_CHANNELS: "1" }).slack.codeChannels, true);
  assert.equal(readConfig({ ...base, SLACK_CODE_CHANNELS: "true" }).slack.codeChannels, true);
});

// With SLACK_VALIDATE=1, Slack itself checks the answer that carries the button.
test(
  "the answer with the button is valid Block Kit",
  {
    skip: process.env.SLACK_VALIDATE !== "1",
  },
  async () => {
    assert.ok(produced.length > 0);
    for (const p of produced) {
      const result = await validateBlocks(p.blocks);
      assert.ok(result.ok, `${p.from}: ${JSON.stringify(result.errors ?? result.error)}`);
    }
  },
);
