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

test("Reply opens the reply modal, and sending it posts in the thread as the person", async () => {
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
  assert.deepEqual(JSON.parse(view.private_metadata), {
    thread: "th-0123456789abcdef",
    message: "m-3",
  });
  await w.app.handle(
    interactive({
      type: "view_submission",
      view: { ...view, state: { values: { reply: { value: { value: "May 3, confirmed." } } } } },
    }),
  );
  await settle();
  assert.deepEqual(w.warp.postedFor, [
    {
      thread: "th-0123456789abcdef",
      body: { text: "May 3, confirmed.", after: "m-3", act_as: { client: "app", user: USER } },
    },
  ]);
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

test("a mention raised in this app's own Slack thread is not sent again as a direct message", async () => {
  const w = mapping();
  // The thread the mention is in is one the app filed from Slack.
  const filed = await w.warp.postThread({
    text: "noted",
    act_as: { client: "app", user: USER },
    surface: { client: "app", channel: "C01ABCDEF", ts: "1700000000.000100" },
    post_only: true,
  });
  await watching(w, async () => {
    w.warp.tellFor(
      "dana",
      { client: "app", user: USER },
      { ...mentionMail(), source: { thread: filed.thread.id, project: "Globex" } },
    );
  });
  assert.equal(w.slack.of("chat.postMessage").filter((m) => m.channel === USER).length, 0);
});
