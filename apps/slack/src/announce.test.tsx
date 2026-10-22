import assert from "node:assert/strict";
import { test } from "node:test";
import {
  announcementMail,
  homeOpened,
  mentionMail,
  SUBJECT,
  settle,
  TEAM,
  USER,
  type World,
  world,
} from "./harness.ts";

const NEWS = "C01NEWS";

/** A client app whose general channel of Field is bound to NEWS. */
function bound(): World {
  const w = world({ clientId: "app" });
  w.warp.clients.set("app", {
    id: "app",
    surface: "slack",
    enabled: true,
    identity: { team: TEAM },
    slack: { machine: SUBJECT, people: {} },
  });
  w.warp.bound.set("app", [{ slack_channel: NEWS, project: "Field", channel: "general" }]);
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

test("an announcement of a project whose general channel is bound is posted there once, with its receipt", async () => {
  const w = bound();
  await watching(w, async () => {
    w.warp.raise(announcementMail());
    await settle();
    await w.app.idle();
    // A second notice about the same message, before or after the receipt, posts nothing new.
    w.warp.raise(w.warp.mailbox.get("m-news") ?? announcementMail());
  });
  const posts = w.slack.of("chat.postMessage").filter((p) => p.channel === NEWS);
  assert.equal(posts.length, 1, "posted once");
  assert.equal(posts[0]?.thread_ts, undefined, "top-level, not in a thread");
  assert.match(JSON.stringify(posts[0]?.blocks), /Lisbon, in May\./);
  assert.deepEqual(
    w.warp.deliveries.map((d) => ({ message: d.message, client: d.client, channel: d.channel })),
    [{ message: "m-news", client: "app", channel: NEWS }],
  );
  assert.equal(w.slack.of("chat.update").length, 1, "the later notice edits the post");
});

test("a withdrawal edits the post, and a restart finds it through its receipt", async () => {
  const w = bound();
  await watching(w, async () => {
    w.warp.raise(announcementMail());
  });
  const restarted = world({ clientId: "app" }, w);
  await (async () => {
    const stop = new AbortController();
    const watch = restarted.app.watchInbox(stop.signal);
    const posted = w.warp.mailbox.get("m-news") ?? {};
    w.warp.raise({ ...posted, state: "dismissed" });
    await settle();
    await restarted.app.idle();
    stop.abort();
    await watch;
  })();
  assert.equal(
    w.slack.of("chat.postMessage").filter((p) => p.channel === NEWS).length,
    1,
    "never posted again",
  );
  const edit = w.slack.of("chat.update").at(-1);
  assert.equal(edit?.channel, NEWS);
  assert.equal(edit?.ts, w.warp.deliveries[0]?.ts);
});

test("a draft, a project that is not bound, and an app that is no client post nothing", async () => {
  const w = bound();
  await watching(w, async () => {
    w.warp.raise(
      announcementMail({
        id: "m-draft",
        parts: [{ id: "p1", type: "draft", state: "open", actions: [] }],
      }),
    );
    w.warp.raise(announcementMail({ id: "m-launch", source: { project: "Launch" } }));
  });
  const plain = world();
  await watching(plain, async () => {
    plain.warp.raise(announcementMail());
  });
  assert.equal(w.slack.of("chat.postMessage").length, 0);
  assert.equal(plain.slack.of("chat.postMessage").length, 0);
});

test("the Home tab reads a mapped person's own inbox and what's new, through the client", async () => {
  const w = bound();
  w.warp.clients.set("app", {
    id: "app",
    surface: "slack",
    enabled: true,
    identity: { team: TEAM },
    slack: { machine: SUBJECT, people: { [USER]: "ana" } },
  });
  w.warp.personal.set("ana", [mentionMail(), announcementMail()]);
  await w.app.handle(homeOpened(USER));
  await settle();
  assert.deepEqual(w.warp.readFor, [
    { client: "app", user: USER },
    { client: "app", user: USER },
  ]);
  const view = JSON.stringify(w.slack.of("views.publish").at(-1)?.view);
  assert.match(view, /Ana mentioned you in #renewals/);
  assert.match(view, /Field offsite/);
  assert.ok(!view.includes('"type":"button"'), "nothing to press: decisions are made in warp");
});
