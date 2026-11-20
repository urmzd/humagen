import assert from "node:assert/strict";
import { test } from "node:test";
import type { Run } from "@humagen/adapter";
import { CodeChannels, contextBar, MAX_TITLE, summaryOf } from "./codechannel.ts";
import { CHANNEL, FakeSlack, mention, SUBJECT, settle, TEAM, world } from "./harness.ts";
import { runId } from "./keys.ts";

const TS = "1700000000.000100";
const KEY = `slack-${TEAM}-${CHANNEL}-1700000000-000100`;
const CONSOLE = "https://humagen.test";
const href = (path: string) => `${CONSOLE}${path}`;

const run = (over: Partial<Run> = {}): Run => ({
  id: "r1",
  tenant: "acme",
  agent: "lead",
  rev: 1,
  origin: "slack",
  text: "fix the flaky login test",
  root: "r1",
  depth: 0,
  thread: "th-1",
  lease: 0,
  granted: 0,
  calls: 0,
  status: "running",
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
  ...over,
});

function channels(titles: Record<string, string | Error> = {}, waits: number[] = []) {
  const slack = new FakeSlack();
  const logs: string[] = [];
  const asked: string[] = [];
  const naps: number[] = [];
  const chrome = new CodeChannels(slack, {
    title: async (thread) => {
      asked.push(thread);
      const title = titles[thread];
      if (title instanceof Error) throw title;
      return title ?? null;
    },
    href,
    log: (l) => logs.push(l),
    titleWaits: waits,
    sleep: async (ms) => {
      naps.push(ms);
    },
  });
  return { slack, chrome, logs, asked, naps, titles };
}

test("the context bar shows the run's project, its agent and the conversation, and nothing a run does not say", () => {
  assert.deepEqual(contextBar(run({ project: "payments" }), { href }), [
    {
      key: "project",
      label: "payments",
      icon: "folder",
      url: `${CONSOLE}/projects/payments`,
    },
    { key: "agent", label: "lead", icon: "terminal" },
    {
      key: "conversation",
      label: "Open Conversation",
      icon: "link",
      url: `${CONSOLE}/conversations/th-1`,
    },
  ]);
  // No project and no console address: only who answers.
  assert.deepEqual(contextBar(run()), [{ key: "agent", label: "lead", icon: "terminal" }]);
  const keys = contextBar(run({ project: "payments" }), { href }).map((i) => i.key);
  for (const absent of ["repo", "branch", "pr", "ci"]) assert.ok(!keys.includes(absent));
});

test("the context bar is sent once and again only when it changes", async () => {
  const { slack, chrome } = channels();
  await chrome.show(CHANNEL, run({ project: "payments" }));
  await chrome.show(CHANNEL, run({ project: "payments", id: "r2" }));
  assert.equal(slack.of("agents.conversations.setProperties").length, 1);
  assert.deepEqual(slack.of("agents.conversations.setProperties")[0], {
    channel_id: CHANNEL,
    code_channel: { context_bar_items: contextBar(run({ project: "payments" }), { href }) },
  });
  await chrome.show(CHANNEL, run({ project: "payments", agent: "helper" }));
  const sent = slack.of("agents.conversations.setProperties");
  assert.equal(sent.length, 2);
  const items = (sent[1]?.code_channel as { context_bar_items: { key: string; label: string }[] })
    .context_bar_items;
  assert.equal(items.find((i) => i.key === "agent")?.label, "helper");
});

test("a channel Slack refuses the context bar for is not asked again", async () => {
  const { slack, chrome, logs } = channels({ "th-1": "Fix flaky login test" });
  slack.failing["agents.conversations.setProperties"] = "not_agent_channel";
  await chrome.sync(CHANNEL, run());
  await chrome.sync(CHANNEL, run({ agent: "helper" }));
  assert.equal(slack.of("agents.conversations.setProperties").length, 1);
  assert.match(logs.join("\n"), /context bar skipped: .*not_agent_channel/);
  // The name is its own call and is still given.
  assert.deepEqual(slack.of("agents.sessions.rename"), [
    { channel_id: CHANNEL, title: "Fix flaky login test" },
  ]);
});

test("a refusal that may pass is tried again the next turn", async () => {
  const { slack, chrome } = channels();
  slack.failing["agents.conversations.setProperties"] = "ratelimited";
  await chrome.show(CHANNEL, run());
  slack.failing = {};
  await chrome.show(CHANNEL, run());
  assert.equal(slack.of("agents.conversations.setProperties").length, 2);
});

test("the session is named once humagen has a title, and never again", async () => {
  const { slack, chrome, asked, titles } = channels();
  // No title yet: nothing is renamed.
  await chrome.sync(CHANNEL, run());
  assert.deepEqual(slack.of("agents.sessions.rename"), []);
  titles["th-1"] = "  Fix flaky\nlogin test ";
  await chrome.sync(CHANNEL, run());
  assert.deepEqual(slack.of("agents.sessions.rename"), [
    { channel_id: CHANNEL, title: "Fix flaky login test" },
  ]);
  const looks = asked.length;
  titles["th-1"] = "Something else";
  await chrome.sync(CHANNEL, run());
  await chrome.settle(CHANNEL, run());
  assert.equal(slack.of("agents.sessions.rename").length, 1);
  assert.equal(asked.length, looks, "humagen is not asked for a title once the session is named");
});

test("a long title is clipped to one short line", async () => {
  const { slack, chrome } = channels({ "th-1": "word ".repeat(60) });
  await chrome.name(CHANNEL, "th-1");
  const title = String(slack.of("agents.sessions.rename")[0]?.title);
  assert.ok(title.length <= MAX_TITLE);
  assert.ok(title.endsWith("…"));
});

test("after a turn ends the title is looked for a few more times, then left", async () => {
  const { slack, chrome, asked, naps } = channels({}, [10, 20]);
  await chrome.show(CHANNEL, run());
  await chrome.settle(CHANNEL, run());
  assert.deepEqual(naps, [10, 20]);
  assert.equal(asked.length, 3);
  assert.deepEqual(slack.of("agents.sessions.rename"), []);

  // The title agent answers between two looks.
  let looks = 0;
  const slackLate = new FakeSlack();
  const late = new CodeChannels(slackLate, {
    title: async () => {
      looks += 1;
      return looks >= 2 ? "Fix flaky login test" : null;
    },
    titleWaits: [10, 20],
    sleep: async () => {},
  });
  await late.show(CHANNEL, run());
  await late.settle(CHANNEL, run());
  assert.equal(looks, 2);
  assert.equal(slackLate.of("agents.sessions.rename").length, 1);
});

test("a refused name is not tried again, and a failed read of the title is", async () => {
  const refused = channels({ "th-1": "Fix flaky login test" });
  refused.slack.failing["agents.sessions.rename"] = "not_agent_channel";
  await refused.chrome.name(CHANNEL, "th-1");
  await refused.chrome.name(CHANNEL, "th-1");
  assert.equal(refused.slack.of("agents.sessions.rename").length, 1);

  const unread = channels({ "th-1": new Error("humagen is restarting") });
  assert.equal(await unread.chrome.name(CHANNEL, "th-1"), false);
  unread.titles["th-1"] = "Fix flaky login test";
  assert.equal(await unread.chrome.name(CHANNEL, "th-1"), true);
  assert.equal(unread.slack.of("agents.sessions.rename").length, 1);
});

test("closing posts the summary, closes the session and shares it back to where the session began", async () => {
  const { slack, chrome } = channels();
  await chrome.show(CHANNEL, run());
  slack.calls.length = 0;
  assert.equal(await chrome.close(CHANNEL, "Fixed the test. PR is open."), "closed");
  assert.deepEqual(slack.methods, [
    "chat.postMessage",
    "agents.sessions.setStatus",
    "agents.conversations.archive",
  ]);
  const posted = slack.of("chat.postMessage")[0];
  assert.deepEqual(posted, { channel: CHANNEL, markdown_text: "Fixed the test. PR is open." });
  assert.deepEqual(slack.of("agents.sessions.setStatus")[0], {
    channel_id: CHANNEL,
    status: "closed",
  });
  const archived = slack.of("agents.conversations.archive")[0];
  assert.equal(archived?.channel_id, CHANNEL);
  assert.match(String(archived?.summary_message_ts), /^\d+\.\d+$/);
  assert.equal(chrome.has(CHANNEL), false);
});

test("a channel with no message it began from is archived without sharing the summary back", async () => {
  const { slack, chrome } = channels();
  await chrome.show(CHANNEL, run());
  slack.failing["agents.conversations.archive"] = (args) =>
    args.summary_message_ts ? "no_origin_link" : undefined;
  assert.equal(await chrome.close(CHANNEL, "Done."), "closed");
  assert.deepEqual(
    slack.of("agents.conversations.archive").map((a) => Object.keys(a)),
    [["channel_id", "summary_message_ts"], ["channel_id"]],
  );
});

test("closing leaves alone a channel not seen to be a code channel, and says when Slack refuses", async () => {
  const { slack, chrome, logs } = channels();
  assert.equal(await chrome.close(CHANNEL, "Done."), "skipped");
  assert.deepEqual(slack.methods, []);
  await chrome.show(CHANNEL, run());
  assert.equal(await chrome.close(CHANNEL, "  "), "skipped");
  slack.failing["agents.conversations.archive"] = "not_agent_channel";
  assert.equal(await chrome.close(CHANNEL, "Done."), "failed");
  assert.equal(slack.of("agents.conversations.archive").length, 1);
  assert.match(logs.join("\n"), /closing the code channel .* failed/);
});

test("a closing summary is the run's own summary of its outcome, else its answer", () => {
  assert.equal(
    summaryOf({ outcome: { kind: "update", summary: "Shipped." }, answer: "long answer" }),
    "Shipped.",
  );
  assert.equal(summaryOf({ answer: " The answer. " }), "The answer.");
  assert.equal(summaryOf({}), "");
});

// The app: only a channel Slack says is a code channel gets any of it.

/** Slack as a code channel answers: a session has no thread there. */
function asCodeChannel(slack: FakeSlack) {
  slack.failing["agents.sessions.setStatus"] = (args) =>
    args.thread_ts ? "thread_ts_not_allowed" : undefined;
}

const CHROME = [
  "agents.conversations.setProperties",
  "agents.sessions.rename",
  "agents.conversations.archive",
];

test("an ordinary thread makes no code channel call", async () => {
  const w = world({ publicUrl: CONSOLE, titleWaits: [] });
  w.humagen.titles.set(runId("acme", SUBJECT, KEY), "Deploy changes");
  await w.app.handle(mention("what changed in the last deploy?", TS));
  await settle();
  w.humagen.emit(w.humagen.last.id, "run.completed.v1", { text: "Three commits went out." });
  await settle();
  await w.app.idle();
  assert.deepEqual(
    w.slack.methods.filter((m) => CHROME.includes(m)),
    [],
  );
});

test("in a code channel a turn sets the context bar, and the session is named when the conversation is titled", async () => {
  const w = world({ publicUrl: CONSOLE, titleWaits: [] });
  asCodeChannel(w.slack);
  await w.app.handle(mention("fix the flaky login test", TS));
  await settle();
  const id = w.humagen.last.id;
  assert.deepEqual(w.slack.of("agents.conversations.setProperties"), [
    {
      channel_id: CHANNEL,
      code_channel: {
        context_bar_items: [
          { key: "agent", label: "lead", icon: "terminal" },
          {
            key: "conversation",
            label: "Open Conversation",
            icon: "link",
            url: `${CONSOLE}/conversations/${id}`,
          },
        ],
      },
    },
  ]);
  // Humagen has no title while the first turn runs.
  assert.deepEqual(w.slack.of("agents.sessions.rename"), []);

  // The conversation is titled as its first turn ends.
  w.humagen.titles.set(id, "Fix flaky login test");
  w.humagen.emit(id, "run.completed.v1", { text: "Fixed." });
  await settle();
  await w.app.idle();
  await settle();
  assert.deepEqual(w.slack.of("agents.sessions.rename"), [
    { channel_id: CHANNEL, title: "Fix flaky login test" },
  ]);
  // The answer is where it always is, and the session settles without a thread.
  assert.ok(w.slack.methods.includes("chat.stopStream"));
  assert.deepEqual(w.slack.of("agents.sessions.setStatus").at(-1), {
    channel_id: CHANNEL,
    status: "active",
  });

  // A second turn there changes nothing: the bar is as it was and the session has its name.
  const TS2 = "1700000100.000200";
  await w.app.handle(mention("and the signup test?", TS2));
  await settle();
  w.humagen.emit(w.humagen.last.id, "run.completed.v1", { text: "That one too." });
  await settle();
  await w.app.idle();
  assert.equal(w.slack.of("agents.sessions.rename").length, 1);
  const bars = w.slack.of("agents.conversations.setProperties");
  assert.equal(bars.length, 2, "a new conversation changes only the link");
});

test("a code channel Slack refuses the chrome for still gets its answer", async () => {
  const w = world({ publicUrl: CONSOLE, titleWaits: [] });
  asCodeChannel(w.slack);
  w.slack.failing["agents.conversations.setProperties"] = "missing_scope";
  w.slack.failing["agents.sessions.rename"] = "missing_scope";
  await w.app.handle(mention("fix the flaky login test", TS));
  await settle();
  const id = w.humagen.last.id;
  w.humagen.titles.set(id, "Fix flaky login test");
  w.humagen.emit(id, "run.delta.text.v1", { text: "Fixed." });
  await settle();
  w.humagen.emit(id, "run.completed.v1", { text: "Fixed." });
  await settle();
  await w.app.idle();
  await settle();
  assert.equal(w.slack.streamed, "Fixed.");
  assert.ok(w.slack.methods.includes("chat.stopStream"));
  assert.equal(w.slack.of("agents.conversations.setProperties").length, 1);
  assert.equal(w.slack.of("agents.sessions.rename").length, 1);
});

test("an archived code channel is forgotten", async () => {
  const w = world({ publicUrl: CONSOLE, titleWaits: [] });
  asCodeChannel(w.slack);
  await w.app.handle(mention("fix the flaky login test", TS));
  await settle();
  w.humagen.emit(w.humagen.last.id, "run.completed.v1", { text: "Fixed." });
  await settle();
  await w.app.idle();
  await w.app.handle({
    type: "events_api",
    envelope_id: "env-archive",
    payload: { team_id: TEAM, event: { type: "channel_archive", channel: CHANNEL, user: "U1" } },
  });
  await settle();
  assert.equal(w.slack.of("agents.conversations.archive").length, 0, "the app archives nothing");
});
