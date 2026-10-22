import assert from "node:assert/strict";
import { test } from "node:test";
import { defaultCopy } from "@humagen/components";
import {
  announcementMail,
  BOT,
  mention,
  OTHER_USER,
  SUBJECT,
  settle,
  TEAM,
  USER,
  type World,
  world,
} from "./harness.ts";
import { HumagenError } from "./humagen.ts";
import { SlackError } from "./slack.ts";

const NEWS = "C01NEWS";
const HOUR = 60 * 60_000;

/** A client app whose general channel of Field is bound to NEWS, two days ago. */
function bound(): World {
  const w = world({ clientId: "app", catchUpRetryMs: 1 });
  w.humagen.clients.set("app", {
    id: "app",
    surface: "slack",
    enabled: true,
    identity: { team: TEAM },
    slack: { machine: SUBJECT, people: {} },
  });
  w.humagen.bound.set("app", [
    {
      slack_channel: NEWS,
      project: "Field",
      channel: "general",
      bound_at: w.humagen.ago(48 * HOUR),
    },
  ]);
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
    w.humagen.raise(announcementMail());
    await settle();
    await w.app.idle();
    // A second notice about the same message, before or after the receipt, posts nothing new.
    w.humagen.raise(w.humagen.mailbox.get("m-news") ?? announcementMail());
  });
  const posts = w.slack.of("chat.postMessage").filter((p) => p.channel === NEWS);
  assert.equal(posts.length, 1, "posted once");
  assert.equal(posts[0]?.thread_ts, undefined, "top-level, not in a thread");
  assert.match(JSON.stringify(posts[0]?.blocks), /Lisbon, in May\./);
  assert.deepEqual(
    w.humagen.deliveries.map((d) => ({ message: d.message, client: d.client, channel: d.channel })),
    [{ message: "m-news", client: "app", channel: NEWS }],
  );
  assert.equal(w.slack.of("chat.update").length, 1, "the later notice edits the post");
});

test("a withdrawal edits the post, and a restart finds it through its receipt", async () => {
  const w = bound();
  await watching(w, async () => {
    w.humagen.raise(announcementMail());
  });
  const restarted = world({ clientId: "app" }, w);
  await (async () => {
    const stop = new AbortController();
    const watch = restarted.app.watchInbox(stop.signal);
    const posted = w.humagen.mailbox.get("m-news") ?? {};
    w.humagen.raise({ ...posted, state: "dismissed" });
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
  assert.equal(edit?.ts, w.humagen.deliveries[0]?.ts);
});

test("a draft, a project that is not bound, and an app that is no client post nothing", async () => {
  const w = bound();
  await watching(w, async () => {
    w.humagen.raise(
      announcementMail({
        id: "m-draft",
        parts: [{ id: "p1", type: "draft", state: "open", actions: [] }],
      }),
    );
    w.humagen.raise(announcementMail({ id: "m-launch", source: { project: "Launch" } }));
  });
  const plain = world();
  await watching(plain, async () => {
    plain.humagen.raise(announcementMail());
  });
  assert.equal(w.slack.of("chat.postMessage").length, 0);
  assert.equal(plain.slack.of("chat.postMessage").length, 0);
});

test("an announcement an agent wrote pings nobody: every Slack mention in it is shown as words", async () => {
  const w = bound();
  await watching(w, async () => {
    w.humagen.raise(
      announcementMail({
        from: { kind: "agent", name: "recap" },
        subject: "Recap <@U0DANA> <!channel> <!subteam^S1|@oncall>",
        parts: [
          {
            id: "p1",
            type: "text",
            state: "info",
            actions: [],
            text: "Ping <@U0BOB> and <!here> and <!subteam^S2|@eng> <!everyone> <@W0EVE|eve>",
          },
        ],
      }),
    );
  });
  const posts = w.slack.of("chat.postMessage").filter((p) => p.channel === NEWS);
  assert.equal(posts.length, 1, "posted once");
  const sent = JSON.stringify({ text: posts[0]?.text, blocks: posts[0]?.blocks });
  for (const token of [
    "<@U0DANA>",
    "<!channel>",
    "<!subteam^",
    "<@U0BOB>",
    "<!here>",
    "<!everyone>",
    "<@W0EVE",
  ]) {
    assert.ok(!sent.includes(token), `${token} reached the channel: ${sent}`);
  }
  assert.match(sent, /@U0BOB/);
  assert.match(sent, /@oncall/);
});

// Catching up. The event stream does not replay what it carried while the
// app was not listening, so each time it opens the app reads the newest
// announcements of every project whose general channel is bound here and
// compares them with its receipts.

/** An announcement put in humagen while the app was not listening: no notice is sent. */
function missed(w: World, over: Record<string, unknown>) {
  const at = String(over.at ?? w.humagen.ago(HOUR));
  const m = announcementMail({ at, updated_at: at, ...over });
  w.humagen.mailbox.set(String(m.id), m);
  return m;
}

const postsIn = (w: World, channel: string) =>
  w.slack.of("chat.postMessage").filter((p) => p.channel === channel);

test("what was announced while the app was away is posted when it connects, oldest first, and once", async () => {
  const w = bound();
  missed(w, { id: "m-early", subject: "Early news", at: w.humagen.ago(2 * HOUR) });
  missed(w, { id: "m-late", subject: "Late news", at: w.humagen.ago(HOUR) });
  await watching(w, async () => {
    await settle();
    await w.app.idle();
    // The stream reconnects: the receipts say both are posted.
    w.humagen.reopen();
  });
  const posts = postsIn(w, NEWS);
  assert.deepEqual(
    posts.map((p) => p.text),
    ["Early news", "Late news"],
    "each posted once, in the order announced",
  );
  assert.ok(
    posts.every((p) => p.thread_ts === undefined),
    "top-level",
  );
  assert.deepEqual(
    w.humagen.deliveries.map((d) => d.message),
    ["m-early", "m-late"],
  );
  assert.equal(w.slack.of("chat.update").length, 0, "nothing posted is edited for nothing");
});

test("a withdrawal while the app was away turns its post into the withdrawn text, once", async () => {
  const w = bound();
  await watching(w, async () => {
    w.humagen.raise(announcementMail());
    await settle();
    await w.app.idle();
    w.humagen.quietly("m-news", { state: "dismissed" });
    w.humagen.reopen();
    await settle();
    await w.app.idle();
    w.humagen.reopen();
  });
  assert.equal(postsIn(w, NEWS).length, 1, "never posted again");
  const edits = w.slack.of("chat.update");
  assert.equal(edits.length, 1, "edited once, and not again at the next reconnect");
  const sent = JSON.stringify({ text: edits[0]?.text, blocks: edits[0]?.blocks });
  assert.equal(edits[0]?.ts, w.humagen.deliveries[0]?.ts);
  assert.match(sent, new RegExp(defaultCopy.announcements.seedWithdrawn));
  assert.ok(
    !sent.includes("Lisbon") && !sent.includes("Field offsite"),
    `withdrawn words stayed: ${sent}`,
  );
  // The receipt is recorded again, so it says the post shows the withdrawal.
  assert.deepEqual(
    w.humagen.deliveries.map((d) => d.message),
    ["m-news", "m-news"],
  );
});

test("a withdrawal heard on the stream turns the post into the withdrawn text too", async () => {
  const w = bound();
  await watching(w, async () => {
    w.humagen.raise(announcementMail());
    await settle();
    await w.app.idle();
    w.humagen.raise({ ...(w.humagen.mailbox.get("m-news") ?? {}), state: "dismissed" });
    await settle();
    await w.app.idle();
    w.humagen.reopen();
  });
  const edits = w.slack.of("chat.update");
  assert.equal(edits.length, 1);
  assert.match(
    JSON.stringify(edits[0]?.blocks),
    new RegExp(defaultCopy.announcements.seedWithdrawn),
  );
  assert.ok(!JSON.stringify(edits[0]).includes("Lisbon"));
});

test("a catch-up posts nothing older than a day, withdrawn, or a draft, and posts what it missed between its posts", async () => {
  const w = bound();
  missed(w, { id: "m-ancient", subject: "Ancient", at: w.humagen.ago(30 * HOUR) });
  missed(w, { id: "m-early", subject: "Early", at: w.humagen.ago(4 * HOUR) });
  const posted = missed(w, { id: "m-posted", subject: "Posted", at: w.humagen.ago(3 * HOUR) });
  posted.deliveries = {
    app: { channel: NEWS, ts: "1700000000.000100", at: w.humagen.ago(3 * HOUR - 1000) },
  };
  missed(w, { id: "m-gone", subject: "Gone", state: "dismissed", at: w.humagen.ago(2 * HOUR) });
  missed(w, {
    id: "m-draft",
    subject: "Draft",
    at: w.humagen.ago(HOUR),
    parts: [{ id: "p1", type: "draft", state: "open", actions: [] }],
  });
  missed(w, { id: "m-missed", subject: "Missed", at: w.humagen.ago(HOUR / 2) });
  await watching(w, async () => {});
  assert.deepEqual(
    postsIn(w, NEWS).map((p) => p.text),
    ["Early", "Missed"],
  );
  assert.equal(w.slack.of("chat.update").length, 0, "the post that has a receipt is up to date");
});

test("a catch-up posts nothing announced before its Slack channel was bound", async () => {
  // Field's general was bound half an hour ago; X was announced an hour before that.
  const w = bound();
  w.humagen.bound.set("app", [
    {
      slack_channel: NEWS,
      project: "Field",
      channel: "general",
      bound_at: w.humagen.ago(HOUR / 2),
    },
  ]);
  missed(w, { id: "m-x", subject: "Before the binding", at: w.humagen.ago(HOUR + HOUR / 2) });
  missed(w, { id: "m-y", subject: "After the binding", at: w.humagen.ago(HOUR / 4) });
  await watching(w, async () => {});
  assert.deepEqual(
    postsIn(w, NEWS).map((p) => p.text),
    ["After the binding"],
  );
});

test("a binding that does not say when it was made posts nothing new, and still edits what changed", async () => {
  const w = bound();
  w.humagen.bound.set("app", [{ slack_channel: NEWS, project: "Field", channel: "general" }]);
  const gone = missed(w, {
    id: "m-gone",
    state: "dismissed",
    at: w.humagen.ago(2 * HOUR),
    updated_at: w.humagen.ago(HOUR),
  });
  gone.deliveries = {
    app: { channel: NEWS, ts: "1700000000.000404", at: w.humagen.ago(2 * HOUR - 1000) },
  };
  missed(w, { id: "m-new", subject: "New", at: w.humagen.ago(HOUR / 4) });
  await watching(w, async () => {});
  assert.equal(postsIn(w, NEWS).length, 0);
  assert.deepEqual(
    w.slack.of("chat.update").map((u) => u.ts),
    ["1700000000.000404"],
  );
});

test("an app whose client names another machine catches up on nothing and posts nothing, and says why", async () => {
  const w = bound();
  w.humagen.clients.set("app", {
    id: "app",
    surface: "slack",
    enabled: true,
    identity: { team: TEAM },
    slack: { machine: "svc:rotated", people: {} },
  });
  missed(w, { id: "m-missed", subject: "Missed", at: w.humagen.ago(HOUR) });
  await watching(w, async () => {
    w.humagen.raise(announcementMail());
  });
  assert.equal(w.humagen.newsReads.length, 0, "nothing read");
  assert.equal(w.slack.of("chat.postMessage").length, 0, "nothing posted");
  assert.ok(w.logs.some((l) => l.includes("does not name this app's credential as its machine")));
});

test("an app whose client is disabled posts nothing, as humagen shows it no receipts", async () => {
  const w = bound();
  w.humagen.clients.set("app", {
    id: "app",
    surface: "slack",
    enabled: false,
    identity: { team: TEAM },
    slack: { machine: SUBJECT, people: {} },
  });
  missed(w, { id: "m-missed", subject: "Missed", at: w.humagen.ago(HOUR) });
  await watching(w, async () => {
    w.humagen.raise(announcementMail());
  });
  assert.equal(w.humagen.newsReads.length, 0, "nothing read");
  assert.equal(w.slack.of("chat.postMessage").length, 0, "nothing posted");
  assert.ok(w.logs.some((l) => l.includes("is disabled")));
});

test("a notice about an announcement from before the binding, or days old, posts nothing", async () => {
  const w = bound();
  w.humagen.bound.set("app", [
    {
      slack_channel: NEWS,
      project: "Field",
      channel: "general",
      bound_at: w.humagen.ago(HOUR / 2),
    },
  ]);
  await watching(w, async () => {
    w.humagen.raise(
      announcementMail({
        id: "m-before",
        at: w.humagen.ago(HOUR),
        updated_at: w.humagen.ago(HOUR),
      }),
    );
    w.humagen.raise(
      announcementMail({
        id: "m-now",
        subject: "Now",
        at: w.humagen.ago(60_000),
        updated_at: w.humagen.ago(60_000),
      }),
    );
  });
  assert.deepEqual(
    postsIn(w, NEWS).map((p) => p.text),
    ["Now"],
  );
});

test("a change between an edit and its receipt is shown too, though no notice tells of it", async () => {
  const w = bound();
  const news = missed(w, { id: "m-news", subject: "Field offsite", at: w.humagen.ago(2 * HOUR) });
  news.deliveries = {
    app: { channel: NEWS, ts: "1700000000.000100", at: w.humagen.ago(2 * HOUR - 1000) },
  };
  w.humagen.quietly("m-news", { subject: "Field offsite moved" });
  let first = true;
  w.slack.failing["chat.update"] = () => {
    if (first) {
      first = false;
      // Withdrawn while the post is being edited, before the receipt is recorded.
      w.humagen.quietly("m-news", { state: "dismissed" });
    }
    return undefined;
  };
  await watching(w, async () => {
    await settle();
    await w.app.idle();
    w.humagen.reopen();
  });
  const edits = w.slack.of("chat.update");
  assert.equal(edits.length, 2, "edited for the move, then for the withdrawal, and not again");
  assert.match(
    JSON.stringify(edits[1]?.blocks),
    new RegExp(defaultCopy.announcements.seedWithdrawn),
  );
});

test("a catch-up that could not read humagen is tried again until it finishes", async () => {
  const w = bound();
  w.humagen.failNews = 2;
  missed(w, { id: "m-missed", subject: "Missed", at: w.humagen.ago(HOUR) });
  await watching(w, async () => {});
  assert.deepEqual(
    postsIn(w, NEWS).map((p) => p.text),
    ["Missed"],
  );
  assert.equal(w.humagen.newsReads.length, 3, "read until it could");
  assert.ok(w.logs.some((l) => l.includes("trying again")));
});

test("an announcement humagen could not be read for is tried again, not taken as nothing to post", async () => {
  const w = bound();
  w.humagen.failReads = 1;
  missed(w, { id: "m-missed", subject: "Missed", at: w.humagen.ago(HOUR) });
  await watching(w, async () => {});
  assert.deepEqual(
    postsIn(w, NEWS).map((p) => p.text),
    ["Missed"],
  );
  assert.ok(w.logs.some((l) => l.includes("trying again")));
});

test("a post that could not be checked after its receipt is brought up to date on the next try", async () => {
  const w = bound();
  missed(w, { id: "m-missed", subject: "Missed", at: w.humagen.ago(HOUR) });
  // The announcement is withdrawn just before its receipt is recorded, so
  // the change reads older than the receipt, and the read that would show
  // it fails once.
  const record = w.humagen.recordDelivery.bind(w.humagen);
  let first = true;
  w.humagen.recordDelivery = async (message, client, at) => {
    if (first) {
      first = false;
      w.humagen.quietly(message, { state: "dismissed" });
      w.humagen.failReads = 1;
    }
    return record(message, client, at);
  };
  await watching(w, async () => {});
  const edits = w.slack.of("chat.update");
  assert.ok(edits.length >= 1, "the post is edited once humagen answers");
  assert.match(
    JSON.stringify(edits.at(-1)?.blocks),
    new RegExp(defaultCopy.announcements.seedWithdrawn),
  );
  assert.ok(w.logs.some((l) => l.includes("trying again")));
});

test("a bound project the app may not read is skipped, not tried again and again", async () => {
  const w = bound();
  w.humagen.bound.set("app", [
    {
      slack_channel: NEWS,
      project: "Field",
      channel: "general",
      bound_at: w.humagen.ago(48 * HOUR),
    },
    {
      slack_channel: "C01VAULT",
      project: "Vault",
      channel: "general",
      bound_at: w.humagen.ago(48 * HOUR),
    },
  ]);
  w.humagen.unseen.add("Vault");
  missed(w, { id: "m-missed", subject: "Missed", at: w.humagen.ago(HOUR) });
  await watching(w, async () => {});
  assert.deepEqual(w.humagen.newsReads, ["Field", "Vault"], "each read once");
  assert.deepEqual(
    postsIn(w, NEWS).map((p) => p.text),
    ["Missed"],
  );
  assert.ok(!w.logs.some((l) => l.includes("trying again")));
});

test("a look that fails does not drop the one a catch-up asked for meanwhile", async () => {
  const w = bound();
  await watching(w, async () => {
    w.humagen.raise(announcementMail());
    await settle();
    await w.app.idle();
    // The next edit fails, and while it is under way the announcement is
    // withdrawn quietly and the stream reconnects.
    const call = w.slack.call.bind(w.slack);
    let first = true;
    w.slack.call = async <T,>(method: string, args: Record<string, unknown> = {}): Promise<T> => {
      if (method === "chat.update" && first) {
        first = false;
        w.humagen.quietly("m-news", { state: "dismissed" });
        w.humagen.reopen();
        await settle();
        throw new SlackError(method, "ratelimited");
      }
      return call<T>(method, args);
    };
    w.humagen.raise({ ...(w.humagen.mailbox.get("m-news") ?? {}), subject: "Field offsite moved" });
  });
  const last = w.slack.of("chat.update").at(-1);
  assert.match(JSON.stringify(last?.blocks), new RegExp(defaultCopy.announcements.seedWithdrawn));
  assert.ok(!JSON.stringify(last).includes("Lisbon"), "the withdrawn words are gone");
});

test("a notice that comes during a catch-up never posts twice: the receipt is the record", async () => {
  const w = bound();
  missed(w, { id: "m-news" });
  await watching(w, async () => {
    // The catch-up and the notice both look at it as the stream opens.
    w.humagen.raise(w.humagen.mailbox.get("m-news") ?? {});
    w.humagen.reopen();
  });
  assert.equal(postsIn(w, NEWS).length, 1, "posted once");
  assert.equal(w.humagen.deliveries.length, 1, "one receipt");
});

test("a post whose receipt humagen did not take is recorded at the next look, never posted again", async () => {
  const w = bound();
  w.humagen.refuseDelivery = new HumagenError(503, "humagen is restarting");
  await watching(w, async () => {
    w.humagen.raise(announcementMail());
    await settle();
    await w.app.idle();
    w.humagen.reopen();
  });
  assert.equal(postsIn(w, NEWS).length, 1, "posted once");
  assert.deepEqual(
    w.humagen.deliveries.map((d) => ({ message: d.message, channel: d.channel })),
    [{ message: "m-news", channel: NEWS }],
  );
  // The post this process made is the one recorded, and brought up to date.
  assert.equal(w.slack.of("chat.update")[0]?.ts, w.humagen.deliveries[0]?.ts);
});

test("a post Slack will not edit holds up nothing else in a catch-up", async () => {
  const w = bound();
  w.humagen.bound.set("app", [
    {
      slack_channel: NEWS,
      project: "Field",
      channel: "general",
      bound_at: w.humagen.ago(48 * HOUR),
    },
    {
      slack_channel: "C01LAUNCH",
      project: "Launch",
      channel: "general",
      bound_at: w.humagen.ago(48 * HOUR),
    },
  ]);
  // Withdrawn after it was posted, while the app was away.
  const gone = missed(w, {
    id: "m-gone",
    state: "dismissed",
    at: w.humagen.ago(2 * HOUR),
    updated_at: w.humagen.ago(HOUR + HOUR / 2),
  });
  gone.deliveries = {
    app: { channel: NEWS, ts: "1700000000.000404", at: w.humagen.ago(2 * HOUR - 1000) },
  };
  missed(w, { id: "m-field", subject: "Field news", at: w.humagen.ago(HOUR) });
  missed(w, { id: "m-launch", subject: "Launch news", source: { project: "Launch" } });
  w.slack.failing["chat.update"] = "message_not_found";
  await watching(w, async () => {});
  assert.deepEqual(
    w.slack.of("chat.postMessage").map((p) => p.text),
    ["Field news", "Launch news"],
  );
  assert.ok(w.logs.some((l) => l.includes("m-gone") && l.includes("message_not_found")));
});

test("an app that is no client, or has no bound general channel, reads nothing to catch up on", async () => {
  const plain = world();
  missed(plain, { id: "m-news" });
  await watching(plain, async () => {});
  const w = bound();
  w.humagen.bound.set("app", [{ slack_channel: NEWS, project: "Field", channel: "renewals" }]);
  missed(w, { id: "m-news" });
  await watching(w, async () => {});
  assert.equal(plain.humagen.newsReads.length + w.humagen.newsReads.length, 0);
  assert.equal(
    plain.slack.of("chat.postMessage").length + w.slack.of("chat.postMessage").length,
    0,
  );
});

// Release notes. humagen posts them in the workspace's inbox at start, before
// the app connects, so only a catch-up brings them to Slack.

/** A client app whose workspace's general channel is bound to ALL, and which maps USER to ana. */
const ALL = "C01ALL";
function releaseRoom(): World {
  const w = world({ clientId: "app", catchUpRetryMs: 1 });
  w.humagen.clients.set("app", {
    id: "app",
    surface: "slack",
    enabled: true,
    identity: { team: TEAM },
    slack: { machine: SUBJECT, people: { [USER]: "ana" } },
  });
  w.humagen.bound.set("app", [
    { slack_channel: ALL, project: "", channel: "general", bound_at: w.humagen.ago(48 * HOUR) },
  ]);
  return w;
}

test("release notes posted before the app connects reach the workspace's channel, and a reply under them is seeded with them", async () => {
  const w = releaseRoom();
  missed(w, {
    id: "m-release",
    subject: "What's new in humagen 0.16.0",
    from: { kind: "system", name: "humagen" },
    source: { project: "" },
    at: w.humagen.ago(60_000),
    parts: [{ id: "p1", type: "text", state: "info", actions: [], text: "- Slack catch-up" }],
  });
  await watching(w, async () => {});
  const posts = postsIn(w, ALL);
  assert.equal(posts.length, 1, "posted once");
  assert.match(JSON.stringify(posts[0]?.blocks), /Slack catch-up/);
  assert.deepEqual(w.humagen.newsReads, [""], "the workspace's own inbox is read");
  const post = w.humagen.deliveries[0]?.ts ?? "";
  await w.app.handle(
    mention("@lead what is catch-up?", "1800000200.000100", { channel: ALL, thread_ts: post }),
  );
  await settle();
  assert.deepEqual([...w.humagen.seeded.values()], ["m-release"]);
  w.humagen.emit(w.humagen.last.id, "lifecycle.completed", { text: "It posts what was missed." });
  await settle();
  await w.app.idle();
});

// Replies in an announcement's Slack thread. Slack delivers a message in a
// channel thread to the app only when it mentions the app (the manifest
// subscribes to app_mention, and to message.im for direct messages alone),
// so the reply that mentions the app is what reaches humagen.

/** bound() whose client maps USER to ana and OTHER_USER to olu. */
function mappedNews(): World {
  const w = bound();
  w.humagen.clients.set("app", {
    id: "app",
    surface: "slack",
    enabled: true,
    identity: { team: TEAM },
    slack: { machine: SUBJECT, people: { [USER]: "ana", [OTHER_USER]: "olu" } },
  });
  return w;
}

/** A mention in the Slack thread of the post at ts in NEWS. */
const replyTo = (post: string, text: string, ts: string, user = USER) =>
  mention(text, ts, { channel: NEWS, thread_ts: post, user });

const blocksText = (args: Record<string, unknown> | undefined) =>
  JSON.stringify(args?.blocks ?? []);

test("a mapped person's reply that mentions the app under an announcement is theirs, in the thread the announcement starts", async () => {
  const w = mappedNews();
  await watching(w, async () => {
    w.humagen.raise(announcementMail());
  });
  const post = w.humagen.deliveries[0]?.ts ?? "";
  assert.ok(post, "the announcement was posted with its receipt");
  // Under the post, olu said something without mentioning the app, then ana asks.
  w.slack.replies.set(`${NEWS}/${post}`, [
    { ts: post, user: BOT, text: "Field offsite" },
    { ts: "1800000100.000100", user: OTHER_USER, text: "Lisbon again?" },
  ]);
  await w.app.handle(replyTo(post, "@lead what does it mean for us?", "1800000200.000100"));
  await settle();
  // olu's words go first, as his own and posted only, then ana's: both in
  // the thread of the announcement's post, which humagen starts with it.
  assert.deepEqual(
    w.humagen.posts.map((p) => ({ as: p.act_as.user, surface: p.surface, post_only: p.post_only })),
    [
      {
        as: OTHER_USER,
        surface: {
          client: "app",
          channel: NEWS,
          ts: post,
          message: "1800000100.000100",
          app_user: BOT,
        },
        post_only: true,
      },
      {
        as: USER,
        surface: {
          client: "app",
          channel: NEWS,
          ts: post,
          message: "1800000200.000100",
          app_user: BOT,
        },
        post_only: undefined,
      },
    ],
  );
  assert.deepEqual([...w.humagen.seeded.values()], ["m-news"]);
  // The answer streams under the announcement's post.
  assert.equal(w.slack.of("chat.startStream").at(-1)?.thread_ts, post);
  w.humagen.emit(w.humagen.last.id, "lifecycle.completed", { text: "A trip in May." });
  await settle();
  await w.app.idle();

  // A later reply finds the same thread.
  await w.app.handle(replyTo(post, "@lead and the dates?", "1800000300.000100"));
  await settle();
  w.humagen.emit(w.humagen.last.id, "lifecycle.completed", { text: "The second week." });
  await settle();
  await w.app.idle();
  const threads = new Set(w.humagen.posts.map((p) => `${p.surface.channel}/${p.surface.ts}`));
  assert.deepEqual([...threads], [`${NEWS}/${post}`]);
  assert.equal(w.humagen.seeded.size, 1, "one announcement, one thread");
});

test("someone the client maps to nobody who replies under an announcement is told to connect their account", async () => {
  const w = bound();
  await watching(w, async () => {
    w.humagen.raise(announcementMail());
  });
  const post = w.humagen.deliveries[0]?.ts ?? "";
  await w.app.handle(replyTo(post, "@lead what does it mean?", "1800000200.000100", "U09NOBODY"));
  await settle();
  await w.app.idle();
  assert.equal(w.humagen.posts.length, 0, "nothing is posted for them");
  assert.equal(w.humagen.asked.length, 0, "humagen is never asked");
  const told = w.slack.of("chat.postMessage").at(-1);
  assert.equal(told?.thread_ts, post);
  assert.match(blocksText(told), new RegExp(defaultCopy.threads.unmappedInBound.slice(0, 40)));
});

test("a reply under an announcement that was withdrawn starts nothing, and says so", async () => {
  const w = mappedNews();
  await watching(w, async () => {
    w.humagen.raise(announcementMail());
    await settle();
    await w.app.idle();
    w.humagen.raise({ ...(w.humagen.mailbox.get("m-news") ?? {}), state: "dismissed" });
  });
  const post = w.humagen.deliveries[0]?.ts ?? "";
  await w.app.handle(replyTo(post, "@lead is it still on?", "1800000200.000100"));
  await settle();
  await w.app.idle();
  assert.equal(w.humagen.started.length, 0, "no run is started");
  assert.equal(w.humagen.seeded.size, 0, "no thread is started");
  const told = w.slack.of("chat.postMessage").at(-1);
  assert.equal(told?.thread_ts, post);
  assert.match(blocksText(told), new RegExp(defaultCopy.announcements.replyGone.slice(0, 40)));
});

test("once its channel is bound to another project, a reply under an old post of a withdrawn announcement starts a thread there as any other", async () => {
  const w = mappedNews();
  await watching(w, async () => {
    w.humagen.raise(announcementMail());
    await settle();
    await w.app.idle();
    w.humagen.raise({ ...(w.humagen.mailbox.get("m-news") ?? {}), state: "dismissed" });
  });
  const post = w.humagen.deliveries[0]?.ts ?? "";
  w.humagen.bound.set("app", [{ slack_channel: NEWS, project: "Initech", channel: "general" }]);
  await w.app.handle(replyTo(post, "@lead what is this?", "1800000200.000100"));
  await settle();
  assert.equal(w.humagen.started.length, 1, "the reply starts a run in Initech");
  w.humagen.emit(w.humagen.last.id, "lifecycle.completed", { text: "A thread in Initech." });
  await settle();
  await w.app.idle();
  assert.equal(w.humagen.posts.length, 1, "the reply is posted as any other");
  assert.equal(w.humagen.seeded.size, 0, "nothing of Field's announcement comes with it");
  const told = w.slack.of("chat.postMessage").map((m) => blocksText(m));
  assert.ok(
    !told.some((b) => b.includes(defaultCopy.announcements.replyGone.slice(0, 40))),
    "nobody in Initech's channel is told about Field's announcement",
  );
});
