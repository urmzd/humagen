import assert from "node:assert/strict";
import { test } from "node:test";
import {
  BOT,
  CHANNEL,
  direct,
  mention,
  OTHER_USER,
  SUBJECT,
  settle,
  TEAM,
  USER,
  type World,
  world,
} from "./harness.ts";
import { HISTORY, type SlackMessage, sinceLastTurn, withHistory } from "./history.ts";
import { words } from "./host.ts";
import { SlackError, slackClient } from "./slack.ts";

const ROOT = "1700000000.000100";
const ASK = "1700000009.000100";
const at = { botUser: BOT, threadTs: ROOT, triggerTs: ASK };

const THREAD: SlackMessage[] = [
  { ts: ROOT, user: OTHER_USER, text: "deploy 42 is failing on staging" },
  { ts: "1700000001.000100", user: USER, text: "I see it too, the migration timed out" },
  { ts: "1700000002.000100", user: USER, text: `<@${BOT}> what does the log say?` },
  { ts: "1700000003.000100", user: BOT, text: "The migration waited on a lock." },
  { ts: "1700000004.000100", user: OTHER_USER, text: "who holds it?" },
  { ts: "1700000005.000100", bot_id: "B1", text: "CI: build 43 started" },
  { ts: "1700000006.000100", user: USER, subtype: "channel_join", text: "joined" },
  { ts: "1700000007.000100", user: USER, text: "the nightly backfill,\nI think" },
  { ts: ASK, user: USER, text: `<@${BOT}> can we stop it safely?` },
];

test("history: only people's messages since the app was last asked, before the one asking", () => {
  const got = sinceLastTurn(THREAD, at);
  assert.deepEqual(
    got.map((m) => m.ts),
    ["1700000004.000100", "1700000007.000100"],
  );
  // A mention deep in a human thread brings everything before it.
  const first = sinceLastTurn(THREAD.slice(0, 2).concat([THREAD[8] as SlackMessage]), at);
  assert.deepEqual(
    first.map((m) => m.ts),
    [ROOT, "1700000001.000100"],
  );
  // A thread the app opened (/agent) counts its own first post as the last time it was asked.
  const opened = sinceLastTurn(
    [
      { ts: ROOT, user: BOT, text: `<@${USER}>: status?` },
      { ts: "1700000001.000100", user: BOT, text: "All green." },
      { ts: "1700000002.000100", user: OTHER_USER, text: "not for me" },
    ],
    at,
  );
  assert.deepEqual(
    opened.map((m) => m.ts),
    ["1700000002.000100"],
  );
});

test("history: each message says who wrote it, and the question comes last", () => {
  const text = withHistory(
    "can we stop it safely?",
    sinceLastTurn(THREAD, at),
    new Map([[OTHER_USER, "Dana"]]),
  );
  assert.equal(
    text,
    [
      words.historyOpen,
      `Dana (<@${OTHER_USER}>): who holds it?`,
      `<@${USER}>: the nightly backfill, I think`,
      words.historyClose,
      "",
      "can we stop it safely?",
    ].join("\n"),
  );
  assert.equal(withHistory("q", []), "q", "nothing to add, nothing added");
});

test("history: a long thread keeps its newest messages within the caps", () => {
  const many: SlackMessage[] = Array.from({ length: 100 }, (_, i) => ({
    ts: `17000000${String(i).padStart(2, "0")}.000100`,
    user: OTHER_USER,
    text: `message ${i} ${"x".repeat(2_000)}`,
  }));
  const text = withHistory("q", many);
  const lines = text.split("\n").slice(1, -3);
  assert.ok(lines.length <= HISTORY.messages);
  assert.ok(lines.join("").length <= HISTORY.chars);
  assert.match(lines.at(-1) ?? "", /message 99 /, "the newest is kept");
  assert.ok((lines.at(-1) ?? "").endsWith("…"), "a long message is cut");
});

function withThread(w: World, messages: SlackMessage[], users: Record<string, string> | "refused") {
  const call = w.slack.call.bind(w.slack);
  w.slack.call = async <T>(method: string, args: Record<string, unknown> = {}): Promise<T> => {
    if (method === "conversations.replies") {
      w.slack.calls.push({ method, args });
      return { ok: true, messages } as T;
    }
    if (method === "users.info") {
      w.slack.calls.push({ method, args });
      if (users === "refused") throw new SlackError(method, "missing_scope");
      return { ok: true, user: { profile: { display_name: users[String(args.user)] } } } as T;
    }
    return call(method, args);
  };
}

async function finish(w: World) {
  w.humagen.emit(w.humagen.last.id, "run.accepted.v1");
  await settle();
  w.humagen.emit(w.humagen.last.id, "run.completed.v1", { text: "ok" });
  await settle();
  await w.app.idle();
}

test("history: a mention in a thread carries what people said there, with who said it", async () => {
  const w = world();
  withThread(w, THREAD, { [OTHER_USER]: "Dana", [USER]: "Sam" });
  await w.app.handle(mention("can we stop it safely?", ASK, { thread_ts: ROOT }));
  await settle();
  const [read] = w.slack.of("conversations.replies");
  assert.deepEqual(read, {
    channel: CHANNEL,
    ts: ROOT,
    latest: ASK,
    inclusive: false,
    limit: 200,
  });
  const asked = w.humagen.started[0]?.text ?? "";
  assert.match(asked, new RegExp(`Dana \\(<@${OTHER_USER}>\\): who holds it\\?`));
  assert.match(asked, new RegExp(`Sam \\(<@${USER}>\\): the nightly backfill, I think`));
  assert.ok(!asked.includes("CI: build"), "a bot's message is left out");
  assert.ok(!asked.includes("waited on a lock"), "the app's own answer is left out");
  assert.ok(asked.endsWith("\ncan we stop it safely?"));
  await finish(w);
});

test("history: without users:read people are named by mention, and Slack is asked once", async () => {
  const w = world();
  withThread(w, THREAD, "refused");
  await w.app.handle(mention("can we stop it safely?", ASK, { thread_ts: ROOT }));
  await settle();
  const asked = w.humagen.started[0]?.text ?? "";
  assert.match(asked, new RegExp(`^<@${OTHER_USER}>: who holds it\\?`, "m"));
  assert.equal(w.slack.of("users.info").length, 1);
  assert.ok(w.logs.some((l) => /users:read/.test(l)));
  await finish(w);
});

test("history: a thread Slack will not give still answers the question alone", async () => {
  const w = world();
  w.slack.failing["conversations.replies"] = "missing_scope";
  await w.app.handle(mention("can we stop it safely?", ASK, { thread_ts: ROOT }));
  await settle();
  assert.equal(w.humagen.started[0]?.text, "can we stop it safely?");
  assert.ok(
    w.logs.some((l) => /thread history skipped: conversations.replies: missing_scope/.test(l)),
  );
  await finish(w);
});

test("history: a top-level mention or a direct message reads no thread", async () => {
  const w = world();
  withThread(w, THREAD, {});
  await w.app.handle(mention("hello", ROOT));
  await settle();
  await finish(w);
  await w.app.handle(direct("hi", "1700000100.000100", { thread_ts: "1700000099.000100" }));
  await settle();
  await finish(w);
  assert.equal(w.slack.of("conversations.replies").length, 0);
});

test("history: Slack's read methods are sent as a form, writes as JSON", async () => {
  const sent: { url: string; type: string | null; body: string }[] = [];
  const fake = (async (url: string, init: RequestInit) => {
    const headers = new Headers(init.headers);
    sent.push({ url, type: headers.get("content-type"), body: String(init.body) });
    return new Response(JSON.stringify({ ok: true }));
  }) as unknown as typeof fetch;
  const slack = slackClient("xoxb-test", { fetch: fake });
  await slack.call("conversations.replies", { channel: CHANNEL, ts: ROOT, inclusive: false });
  await slack.call("chat.postMessage", { channel: CHANNEL, text: "hi" });
  assert.equal(sent[0]?.body, `channel=${CHANNEL}&ts=${ROOT}&inclusive=false`);
  assert.equal(sent[0]?.type, null);
  assert.match(sent[1]?.type ?? "", /application\/json/);
});

test("manifest: the app asks for what it uses, thread history included", async () => {
  const { readFileSync } = await import("node:fs");
  const manifest = JSON.parse(readFileSync(new URL("../manifest.json", import.meta.url), "utf8"));
  const scopes: string[] = manifest.oauth_config.scopes.bot;
  // conversations.replies, by the kind of conversation a thread is in; users.info for names.
  for (const s of [
    "channels:history",
    "groups:history",
    "mpim:history",
    "im:history",
    "users:read",
  ]) {
    assert.ok(scopes.includes(s), `${s} is asked for`);
  }
  const events: string[] = manifest.settings.event_subscriptions.bot_events;
  for (const e of [
    "app_mention",
    "message.im",
    "app_home_opened",
    "agent_session_stopped",
    "assistant_thread_started",
  ]) {
    assert.ok(events.includes(e), `${e} is subscribed`);
  }
  assert.equal(manifest.settings.socket_mode_enabled, true);
});

/** A world whose app is a client that maps USER to sam and OTHER_USER to dana. */
function coworking(): World {
  const w = world({ clientId: "app" });
  w.humagen.clients.set("app", {
    id: "app",
    surface: "slack",
    enabled: true,
    default_agent: "lead",
    identity: { team: TEAM },
    slack: { machine: SUBJECT, people: { [USER]: "sam", [OTHER_USER]: "dana" } },
  });
  return w;
}

test("coworking: a mapped person's mention is their message in the humagen thread its Slack thread is", async () => {
  const w = coworking();
  await w.app.handle(mention("@helper what changed?", ROOT));
  await settle();
  assert.deepEqual(w.humagen.posts, [
    {
      text: "@helper what changed?",
      act_as: { client: "app", user: USER },
      surface: { client: "app", channel: CHANNEL, ts: ROOT, message: ROOT, app_user: BOT },
    },
  ]);
  assert.equal(w.humagen.last.agent, "helper", "the platform read the tag");
  await finish(w);
  // Slack delivers the event again after a restart: humagen has the message, and nothing starts again.
  const again = w.restart();
  await again.handle(mention("@helper what changed?", ROOT));
  await settle();
  assert.equal(w.humagen.started.length, 1);
});

test("coworking: what people said since the app was last asked goes first, each as its person, posted only", async () => {
  const w = coworking();
  withThread(w, THREAD, { [OTHER_USER]: "Dana", [USER]: "Sam" });
  await w.app.handle(mention("can we stop it safely?", ASK, { thread_ts: ROOT }));
  await settle();
  const said = w.humagen.posts.filter((p) => p.post_only);
  assert.ok(said.length > 0, "the room was posted");
  for (const p of said) {
    assert.equal(p.surface.ts, ROOT);
    assert.ok([USER, OTHER_USER].includes(p.act_as.user), "each as its person");
    assert.ok(
      !p.text.includes("CI: build") && !p.text.includes("waited on a lock"),
      "no bot's message, nor the app's",
    );
  }
  const asked = w.humagen.posts.at(-1);
  assert.equal(asked?.surface.message, ASK);
  assert.equal(asked?.post_only, undefined);
  assert.equal(
    asked?.text,
    "can we stop it safely?",
    "the question goes alone: the room is in humagen",
  );
  await finish(w);
});

test("coworking: tagging only a person starts nothing and says who was told", async () => {
  const w = coworking();
  await w.app.handle(mention("@dana can you check?", ROOT));
  await settle();
  assert.equal(w.humagen.started.length, 0);
  await w.app.idle();
  const said = JSON.stringify(
    w.slack.calls.filter((c) =>
      /chat\.(postMessage|update|stopStream|appendStream)/.test(c.method),
    ),
  );
  assert.match(said, /Sent to @dana/);
});

test("coworking: where nothing answers by default, the app says to tag an agent", async () => {
  const w = coworking();
  w.humagen.noDefault = true;
  await w.app.handle(mention("what changed?", ROOT));
  await settle();
  await w.app.idle();
  assert.equal(w.humagen.started.length, 0);
  const said = JSON.stringify(w.slack.calls);
  assert.match(said, /Tag an agent with @/);
});
