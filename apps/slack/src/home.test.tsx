import assert from "node:assert/strict";
import { test } from "node:test";
import { defaultCopy } from "@warp/components";
import { LIMITS } from "@warp/render-slack";
import {
  alertMail,
  announcementMail,
  CHANNEL,
  click,
  DM,
  draftMail,
  failureMail,
  homeClick,
  homeOpened,
  interactive,
  mention,
  mentionMail,
  OTHER_USER,
  SUBJECT,
  settle,
  TEAM,
  USER,
  type World,
  world,
} from "./harness.ts";
import type { WireProject } from "./warp.ts";

const CONSOLE = "https://warp.test";

/**
 * What warp lists for ana: Field and its account Globex, which name her, and
 * News, which she reads as one of everyone. Secret, which she may not see, is
 * not among them.
 */
const ANAS: WireProject[] = [
  {
    name: "Field",
    title: "Field engineering",
    kind: "team",
    path: [{ name: "Field", title: "Field engineering" }],
    active: 1,
    active_at: "2026-10-09T00:30:00Z",
    mine: true,
  },
  {
    name: "Globex",
    kind: "account",
    path: [{ name: "Field", title: "Field engineering" }, { name: "Globex" }],
    active: 0,
    mine: true,
  },
  {
    name: "News",
    title: "Company news",
    kind: "company",
    path: [{ name: "News", title: "Company news" }],
    active: 4,
    active_at: "2026-10-09T00:55:00Z",
    mine: false,
  },
];

/** An app that is the client "app", mapping USER to ana, with the console's address unless it is "". */
function mapping(publicUrl = CONSOLE, operators: string[] = []): World {
  const w = world({ clientId: "app", operators, ...(publicUrl ? { publicUrl } : {}) });
  w.warp.clients.set("app", {
    id: "app",
    surface: "slack",
    enabled: true,
    identity: { team: TEAM },
    agents: ["*"],
    slack: { machine: SUBJECT, people: { [USER]: "ana" } },
  });
  w.warp.personal.set("ana", [mentionMail(), announcementMail()]);
  w.warp.projects.set("ana", ANAS);
  w.slack.users.set(USER, "Ana Lima");
  return w;
}

type Block = {
  type: string;
  block_id?: string;
  text?: { text: string };
  elements?: { type: string; text?: { text: string }; url?: string; value?: string }[];
  accessory?: { type: string; text?: { text: string }; url?: string };
};

/** The Home tab published last for a Slack person. */
function published(w: World, user = USER): Block[] {
  const call = w.slack
    .of("views.publish")
    .filter((c) => c.user_id === user)
    .at(-1);
  assert.ok(call, `no Home was published for ${user}`);
  const view = call.view as { type: string; blocks: Block[] };
  assert.equal(view.type, "home");
  return view.blocks;
}

/** The sections under a heading, up to the next divider. */
function under(blocks: Block[], heading: string): Block[] {
  const at = blocks.findIndex((b) => b.type === "header" && b.text?.text === heading);
  if (at === -1) return [];
  const end = blocks.findIndex((b, i) => i > at && b.type === "divider");
  return blocks.slice(at + 1, end === -1 ? undefined : end);
}

/** Answer the run a message started, so the app stops following it. */
async function answered(w: World) {
  await settle();
  w.warp.emit(w.warp.last.id, "lifecycle.accepted");
  w.warp.emit(w.warp.last.id, "lifecycle.completed", { text: "Done." });
  await w.app.idle();
}

const headers = (blocks: Block[]) =>
  blocks.filter((b) => b.type === "header").map((b) => b.text?.text);

test("a person the client maps sees the shared Home, every part of it read as them through the client", async () => {
  const w = mapping();
  await w.app.handle(homeOpened(USER));
  await settle();
  const as = { client: "app", user: USER };
  assert.deepEqual(w.warp.readFor, [as, as], "their inbox, and their announcements");
  assert.deepEqual(w.warp.projectsReadFor, [as], "their projects");

  const blocks = published(w);
  assert.ok(blocks.length <= LIMITS.blocks.home);
  // The sections the design names.
  assert.deepEqual(headers(blocks), [
    "Hi Ana, I'm Warp.",
    "Needs you",
    "What's new",
    "Your projects",
  ]);
  const [mention] = under(blocks, defaultCopy.home.needsYou);
  assert.match(mention?.text?.text ?? "", /^\*Ana\* mentioned you in #renewals/);
  assert.match(mention?.text?.text ?? "", /https:\/\/warp\.test\/inbox\?message=m-mention/);

  const [news] = under(blocks, defaultCopy.home.whatsNew);
  assert.match(news?.text?.text ?? "", /^\*Field offsite\*\nUnread · Posted in Field engineering/);
  assert.equal(news?.accessory?.url, "https://warp.test/projects/Field?message=m-news");

  // The projects that name her: News, everyone's and the busiest, is not one.
  const projects = under(blocks, defaultCopy.home.yourProjects);
  assert.deepEqual(
    projects.map((b) => b.text?.text.split("\n")[0]),
    ["*Field engineering*", "*Globex*"],
  );
  assert.match(projects[1]?.text?.text ?? "", /Account · in Field engineering/);
  assert.equal(projects[1]?.accessory?.url, "https://warp.test/projects/Globex");

  // It cannot know their access or what they connected, so it says neither.
  const text = JSON.stringify(blocks);
  assert.ok(!text.includes("Signed in as"));
  assert.ok(!text.includes(defaultCopy.home.connections));
  assert.match(text, /<https:\/\/warp\.test\|Open the console>/);
});

test("Home starts a question or an agent as the person, and searches in the console", async () => {
  const w = mapping();
  w.warp.agentList = [...w.warp.agentList, { name: "warp-builder", description: "Builds agents." }];
  await w.app.handle(homeOpened(USER));
  await settle();
  const start = published(w).find((b) => b.block_id === "home:start");
  assert.deepEqual(
    start?.elements?.map((e) => e.text?.text),
    ["Ask a Question", "Build an Agent", "Search Context"],
  );
  assert.equal(start?.elements?.[2]?.url, "https://warp.test/context");

  // Search Context is a link Slack opens itself: its click asks nothing here.
  const calls = w.slack.calls.length;
  await w.app.handle(homeClick(published(w), "Search Context"));
  await w.app.idle();
  assert.equal(w.slack.calls.length, calls);

  // Ask a Question: the form, and its words posted as the person in their messages with the app.
  await w.app.handle(homeClick(published(w), "Ask a Question"));
  const ask = w.slack.of("views.open").at(-1)?.view as Record<string, unknown>;
  assert.equal(ask.callback_id, "aui_form");
  await w.app.handle(
    interactive({
      type: "view_submission",
      view: { ...ask, state: { values: { text: { value: { value: "what changed today?" } } } } },
    }),
  );
  await answered(w);
  assert.equal(w.slack.of("chat.postMessage").at(-1)?.channel, USER);
  assert.deepEqual(
    w.warp.posts.map((p) => ({ text: p.text, act_as: p.act_as })),
    [{ text: "what changed today?", act_as: { client: "app", user: USER } }],
  );

  // Build an Agent: the builder's form, and its words tag the builder.
  await w.app.handle(homeClick(published(w), "Build an Agent"));
  const build = w.slack.of("views.open").at(-1)?.view as Record<string, unknown>;
  assert.equal((build.title as { text: string }).text, "Build an agent");
  await w.app.handle(
    interactive({
      type: "view_submission",
      view: {
        ...build,
        state: { values: { text: { value: { value: "one that triages support tickets" } } } },
      },
    }),
  );
  await answered(w);
  assert.equal(w.warp.posts.at(-1)?.text, "@warp-builder one that triages support tickets");
  assert.deepEqual(w.warp.posts.at(-1)?.act_as, { client: "app", user: USER });
});

test("Build an Agent is offered only where this app may reach the builder", async () => {
  const w = mapping();
  w.warp.agentList = [...w.warp.agentList, { name: "warp-builder" }];
  w.warp.clients.set("app", {
    ...(w.warp.clients.get("app") as NonNullable<ReturnType<typeof w.warp.clients.get>>),
    agents: ["lead"],
  });
  await w.app.handle(homeOpened(USER));
  await settle();
  const start = published(w).find((b) => b.block_id === "home:start");
  assert.ok(!start?.elements?.some((e) => e.text?.text === "Build an Agent"));
});

test("without a console address, Review sends an item to the person's messages, read for them", async () => {
  const w = mapping("");
  await w.app.handle(homeOpened(USER));
  await settle();
  const blocks = published(w);
  assert.ok(!JSON.stringify(blocks).includes("http"), "nothing to link to");
  const start = blocks.find((b) => b.block_id === "home:start");
  assert.deepEqual(
    start?.elements?.map((e) => e.text?.text),
    ["Ask a Question"],
    "no console to search in",
  );
  await w.app.handle(homeClick(blocks, defaultCopy.home.review));
  await w.app.idle();
  const dm = w.slack.of("chat.postMessage").at(-1);
  assert.equal(dm?.channel, USER);
  assert.match(JSON.stringify(dm?.blocks), /can you confirm the renewal date/);
  assert.match(JSON.stringify(dm?.blocks), new RegExp(`"${defaultCopy.reply}"`));
  assert.deepEqual(w.warp.readFor.at(-1), { client: "app", user: USER });
});

/** Press Review on the Home tab, and return the message it sent the person. */
async function reviewed(w: World) {
  await w.app.handle(homeOpened(USER));
  await settle();
  await w.app.handle(homeClick(published(w), defaultCopy.home.review));
  await w.app.idle();
  const dm = w.slack.of("chat.postMessage").at(-1);
  assert.equal(dm?.channel, USER);
  return (dm?.blocks ?? []) as unknown[];
}

for (const operators of [[], [USER]]) {
  const who = operators.length ? "an operator" : "anyone";
  test(`a press in what Review sent is the person's own, for ${who}, even where the app may read it too`, async () => {
    const w = mapping("", operators);
    // An alert that notifies nobody: the app's machine may read it as well as ana.
    w.warp.personal.set("ana", [alertMail()]);
    w.warp.mailbox.set("m-alert", alertMail());
    const blocks = await reviewed(w);
    await w.app.handle(
      click(blocks, defaultCopy.acknowledge, { channel: DM, ts: "1800000009.000100" }),
    );
    await w.app.idle();
    assert.deepEqual(w.warp.resolvedFor, [
      {
        id: "m-alert",
        part: "p1",
        body: { action: "acknowledge" },
        as: { client: "app", user: USER },
      },
    ]);
    assert.deepEqual(w.warp.resolved, [], "never settled with the app's own credential");
    assert.deepEqual(w.slack.of("chat.postEphemeral"), [], "nobody is told no");
    const edit = w.slack.of("chat.update").at(-1);
    assert.equal(edit?.channel, DM, "the message is kept current where it was pressed");
  });
}

test("what Review sent offers no Snooze or Dismiss, which warp does only as the caller", async () => {
  const w = mapping("");
  w.warp.personal.set("ana", [alertMail()]);
  const text = JSON.stringify(await reviewed(w));
  assert.match(text, new RegExp(`"${defaultCopy.acknowledge}"`), "what it can do for them stays");
  for (const label of [defaultCopy.snooze, defaultCopy.dismiss]) {
    assert.ok(!text.includes(`"${label}"`), `${label} would be done as the app, or not at all`);
  }
});

test("Retry in what Review sent asks again as the person, in the run's thread, and acknowledges it as them", async () => {
  const w = mapping("");
  w.warp.personal.set("ana", []);
  await w.app.handle(mention("deploy it", "1700000000.000100"));
  await settle();
  const run = w.warp.last.id;
  assert.equal(w.warp.runs.get(run)?.actor?.user, "ana");
  w.warp.emit(run, "lifecycle.accepted");
  w.warp.emit(run, "lifecycle.failed", { error: "provider: 529 overloaded", terminal: true });
  await settle();
  await w.app.idle();
  // A failed run's message names no audience: the app's machine may read it too.
  w.warp.personal.set("ana", [failureMail(run)]);
  w.warp.mailbox.set("m-failure", failureMail(run));
  const blocks = await reviewed(w);
  const reacted = w.slack.of("reactions.add").length;
  await w.app.handle(click(blocks, defaultCopy.retry, { channel: DM, ts: "1800000009.000100" }));
  await settle();
  assert.equal(w.warp.started.length, 2, "asked again");
  assert.deepEqual(w.warp.started[1]?.act_as, { client: "app", user: USER });
  // The turn asked again is shown in the run's own thread, not on the message in their messages.
  const seen = w.slack.of("reactions.add").slice(reacted);
  assert.ok(seen.length > 0);
  for (const r of seen) assert.deepEqual([r.channel, r.timestamp], [CHANNEL, "1700000000.000100"]);
  assert.deepEqual(w.warp.resolvedFor, [
    {
      id: "m-failure",
      part: "p1",
      body: { action: "acknowledge" },
      as: { client: "app", user: USER },
    },
  ]);
  assert.deepEqual(w.warp.resolved, [], "warp is never asked to retry, nor the app to settle it");
  w.warp.emit(w.warp.last.id, "lifecycle.accepted");
  w.warp.emit(w.warp.last.id, "lifecycle.completed", { text: "ok" });
  await settle();
  await w.app.idle();
});

test("an agent's draft waits in Needs you, counted as warp counts it, and is not news", async () => {
  const w = mapping();
  w.warp.personal.set("ana", [
    mentionMail(),
    announcementMail(),
    draftMail({ subject: "Field shipped two fixes <!here>" }),
  ]);
  await w.app.handle(homeOpened(USER));
  await settle();
  const blocks = published(w);
  const needs = under(blocks, defaultCopy.home.needsYou);
  const rows = needs.filter((b) => b.type === "section").map((b) => b.text?.text ?? "");
  assert.equal(rows.length, 2, JSON.stringify(needs));
  assert.match(rows[0] ?? "", /^\*warp-analyst\* Field shipped two fixes @here/);
  assert.match(rows[0] ?? "", /https:\/\/warp\.test\/inbox\?message=m-draft/);
  assert.match(rows[1] ?? "", /^\*Ana\* mentioned you in #renewals/);
  // Two need her, and both are shown: nothing more is counted.
  const text = JSON.stringify(blocks);
  assert.ok(!text.includes("more in the inbox"), text);
  assert.ok(!text.includes("<!here>"), "its words ping nobody");
  const news = under(blocks, defaultCopy.home.whatsNew);
  assert.deepEqual(
    news.map((b) => b.text?.text.split("\n")[0]),
    ["*Field offsite*"],
  );
});

test("What's new names where it was posted by its title, in a project that is not one of hers too", async () => {
  const w = mapping();
  w.warp.personal.set("ana", [
    announcementMail({ id: "m-all", subject: "Office closed Friday", source: { project: "News" } }),
  ]);
  await w.app.handle(homeOpened(USER));
  await settle();
  const blocks = published(w);
  const [news] = under(blocks, defaultCopy.home.whatsNew);
  assert.match(
    news?.text?.text ?? "",
    /^\*Office closed Friday\*\nUnread · Posted in Company news/,
  );
  assert.ok(
    !under(blocks, defaultCopy.home.yourProjects).some((b) =>
      b.text?.text.includes("Company news"),
    ),
  );
});

test("a person whose projects cannot be read sees the rest of their Home", async () => {
  const w = mapping();
  w.warp.projects.delete("ana");
  await w.app.handle(homeOpened(USER));
  await settle();
  const blocks = published(w);
  assert.ok(!headers(blocks).includes(defaultCopy.home.yourProjects));
  assert.ok(headers(blocks).includes(defaultCopy.home.whatsNew));
  assert.ok(w.logs.some((l) => l.includes("Home shows no projects")));
});

test("an announcement an agent wrote pings nobody on Home either", async () => {
  const w = mapping();
  w.warp.personal.set("ana", [
    announcementMail({
      from: { kind: "agent", name: "recap" },
      subject: "Recap <@U0DANA> <!here>",
    }),
  ]);
  await w.app.handle(homeOpened(USER));
  await settle();
  const [news] = under(published(w), defaultCopy.home.whatsNew);
  const text = news?.text?.text ?? "";
  assert.ok(!text.includes("<@U0DANA>") && !text.includes("<!here>"), text);
  assert.match(text, /@U0DANA/);
});

test("someone the client maps to nobody keeps the mailbox Home, and nothing is read for them", async () => {
  const w = mapping();
  await w.app.handle(homeOpened(OTHER_USER));
  await settle();
  const blocks = published(w, OTHER_USER);
  assert.ok(!headers(blocks).includes(defaultCopy.home.whatsNew));
  assert.ok(!headers(blocks).includes(defaultCopy.home.yourProjects));
  assert.deepEqual(w.warp.readFor, []);
  assert.deepEqual(w.warp.projectsReadFor, []);
});
