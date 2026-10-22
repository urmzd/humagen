import assert from "node:assert/strict";
import { test } from "node:test";
import { defaultCopy } from "@warp/components";
import {
  click,
  DM,
  interactive,
  mentionMail,
  SUBJECT,
  settle,
  TEAM,
  USER,
  type World,
  world,
} from "./harness.ts";

/** An app that is the client "app", mapping USER to dana, with a console to link to. */
function mapping(): World {
  const w = world({ clientId: "app", publicUrl: "https://warp.test" });
  w.warp.clients.set("app", {
    id: "app",
    surface: "slack",
    enabled: true,
    identity: { team: TEAM },
    slack: { machine: SUBJECT, people: { [USER]: "dana" } },
  });
  return w;
}

async function watching(w: World, work: () => Promise<void>) {
  const stop = new AbortController();
  const watch = w.app.watchInbox(stop.signal);
  await work();
  await settle();
  await w.app.idle();
  stop.abort();
  await watch;
}

const blocksOf = (args: Record<string, unknown> | undefined) => (args?.blocks ?? []) as unknown[];

test("a mention of a person the client maps reaches their direct messages, with Reply and where it was said", async () => {
  const w = mapping();
  await watching(w, async () => {
    w.warp.tellFor("dana", { client: "app", user: USER }, mentionMail());
  });
  const dm = w.slack.of("chat.postMessage").at(-1);
  assert.equal(dm?.channel, USER, "posted to the person, which opens their direct messages");
  const text = JSON.stringify(dm?.blocks);
  assert.match(text, /can you confirm the renewal date/);
  assert.match(text, /https:\/\/warp\.test\/tasks\/th-0123456789abcdef\|Open Thread/);
  assert.deepEqual(
    w.warp.readFor,
    [{ client: "app", user: USER }],
    "read for them, through the client",
  );
});

test("Reply opens the reply modal, and sending it replies to the mention as the person, through their inbox", async () => {
  const w = mapping();
  await watching(w, async () => {
    w.warp.tellFor("dana", { client: "app", user: USER }, mentionMail());
  });
  const dm = w.slack.of("chat.postMessage").at(-1);
  await w.app.handle(
    click(blocksOf(dm), defaultCopy.reply, { channel: USER, ts: "1800000000.000100" }),
  );
  await settle();
  const opened = w.slack.of("views.open").at(-1);
  const view = opened?.view as { callback_id: string; private_metadata: string };
  assert.ok(view, "the modal opened");
  assert.deepEqual(JSON.parse(view.private_metadata), { inbox: "m-mention", part: "p1" });
  await w.app.handle(
    interactive({
      type: "view_submission",
      view: { ...view, state: { values: { reply: { value: { value: "May 3, confirmed." } } } } },
    }),
  );
  await settle();
  // A reply to a mention is a human response: sent through the inbox, so
  // the platform posts it in the thread and starts no run, whatever the
  // thread's default.
  assert.deepEqual(w.warp.resolvedFor, [
    {
      id: "m-mention",
      part: "p1",
      body: { action: "reply", text: "May 3, confirmed." },
      as: { client: "app", user: USER },
    },
  ]);
  const edit = w.slack.of("chat.update").at(-1);
  assert.equal(edit?.channel, DM, "the direct message is kept current");
  assert.ok(
    !JSON.stringify(edit?.blocks).includes(`"${defaultCopy.reply}"`),
    "nothing left to reply to",
  );
});

test("once the person has replied, the direct message is kept current", async () => {
  const w = mapping();
  await watching(w, async () => {
    w.warp.tellFor("dana", { client: "app", user: USER }, mentionMail());
    await settle();
    await w.app.idle();
    const open = mentionMail();
    const settled: Record<string, unknown> = {
      ...open,
      state: "resolved",
      parts: (open.parts as Record<string, unknown>[]).map((p) => ({
        ...p,
        state: "resolved",
        actions: [],
        outcome: {
          action: "reply",
          by: "dana",
          at: "2026-10-09T00:45:00Z",
          note: "Replied in the thread",
        },
      })),
    };
    w.warp.tellFor("dana", { client: "app", user: USER }, settled);
  });
  assert.equal(w.slack.of("chat.postMessage").length, 1, "told once");
  const edit = w.slack.of("chat.update").at(-1);
  assert.equal(edit?.channel, DM, "the direct message, edited");
  assert.ok(edit?.ts);
  assert.ok(
    !JSON.stringify(edit?.blocks).includes(`"${defaultCopy.reply}"`),
    "nothing left to reply to",
  );
});

/** A warp thread the app filed from a Slack thread, which dana's Slack user wrote in when `posted`. */
async function slackThread(w: World, posted: boolean): Promise<string> {
  const filed = await w.warp.postThread({
    text: "noted",
    act_as: { client: "app", user: USER },
    surface: { client: "app", channel: "C01ABCDEF", ts: "1700000000.000100" },
    post_only: true,
  });
  w.slack.replies.set("C01ABCDEF/1700000000.000100", [
    { ts: "1700000000.000100", user: "U0ANA", text: "<@UBOT> who checks the renewal?" },
    ...(posted ? [{ ts: "1700000000.000200", user: USER, text: "I can look." }] : []),
  ]);
  return filed.thread.id;
}

test("a mention raised in a Slack thread of this app's that the person is in is not sent again as a direct message", async () => {
  const w = mapping();
  const thread = await slackThread(w, true);
  await watching(w, async () => {
    w.warp.tellFor(
      "dana",
      { client: "app", user: USER },
      { ...mentionMail(), source: { thread, project: "Globex" } },
    );
  });
  assert.equal(w.slack.of("chat.postMessage").filter((m) => m.channel === USER).length, 0);
  assert.deepEqual(
    w.slack.of("conversations.replies").at(-1),
    { channel: "C01ABCDEF", ts: "1700000000.000100", limit: 200 },
    "the whole Slack thread is read",
  );
});

test("a mention natively in this app's Slack thread is not sent again either: Slack pinged them", async () => {
  const w = mapping();
  const thread = await slackThread(w, false);
  w.slack.replies.set("C01ABCDEF/1700000000.000100", [
    { ts: "1700000000.000100", user: "U0ANA", text: `<@UBOT> <@${USER}|dana> can you check?` },
  ]);
  await watching(w, async () => {
    w.warp.tellFor(
      "dana",
      { client: "app", user: USER },
      { ...mentionMail(), source: { thread, project: "Globex" } },
    );
  });
  assert.equal(w.slack.of("chat.postMessage").filter((m) => m.channel === USER).length, 0);
});

test("a tag by handle in this app's Slack thread reaches someone not in it in their direct messages", async () => {
  const w = mapping();
  // "@dana" in Slack is plain text there: it pinged nobody.
  const thread = await slackThread(w, false);
  await watching(w, async () => {
    w.warp.tellFor(
      "dana",
      { client: "app", user: USER },
      { ...mentionMail(), source: { thread, project: "Globex" } },
    );
  });
  assert.equal(w.slack.of("chat.postMessage").filter((m) => m.channel === USER).length, 1);
});

test("an expert asked by a run in this app's Slack thread is told in their direct messages, and a thread that cannot be read tells them too", async () => {
  const w = mapping();
  const thread = await slackThread(w, false);
  const question = {
    ...mentionMail(),
    id: "m-question",
    kind: "clarification",
    subject: "lead asks the security experts",
    source: { thread, project: "Globex", run: "r-1", agent: "lead" },
  };
  await watching(w, async () => {
    w.warp.tellFor("dana", { client: "app", user: USER }, question);
  });
  assert.equal(w.slack.of("chat.postMessage").filter((m) => m.channel === USER).length, 1);

  const unreadable = mapping();
  const other = await slackThread(unreadable, true);
  unreadable.slack.failing["conversations.replies"] = "missing_scope";
  await watching(unreadable, async () => {
    unreadable.warp.tellFor(
      "dana",
      { client: "app", user: USER },
      { ...mentionMail(), source: { thread: other, project: "Globex" } },
    );
  });
  assert.equal(
    unreadable.slack.of("chat.postMessage").filter((m) => m.channel === USER).length,
    1,
    "told twice is better than never",
  );
});

test("a question to a role reaches each person the client maps who holds it, each kept current on their own", async () => {
  const w = mapping();
  const OTHER = "U02OTHER";
  const app = w.warp.clients.get("app");
  if (app?.slack) app.slack.people = { ...app.slack.people, [OTHER]: "vic" };
  const toRole = { ...mentionMail(), to: undefined, audience: ["role:security"] };
  await watching(w, async () => {
    w.warp.tellFor("dana", { client: "app", user: USER }, toRole);
    w.warp.tellFor("vic", { client: "app", user: OTHER }, toRole);
  });
  const sent = w.slack.of("chat.postMessage").map((m) => m.channel);
  assert.deepEqual(sent.sort(), [OTHER, USER].sort(), "one direct message each");
  await watching(w, async () => {
    w.warp.tellFor("vic", { client: "app", user: OTHER }, { ...toRole, state: "resolved" });
  });
  assert.equal(w.slack.of("chat.postMessage").length, 2, "none sent again");
  assert.equal(w.slack.of("chat.update").length, 1, "only vic's is edited");
});
