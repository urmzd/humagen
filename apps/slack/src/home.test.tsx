import assert from "node:assert/strict";
import { test } from "node:test";
import { defaultCopy } from "@warp/components";
import { LIMITS } from "@warp/render-slack";
import {
  announcementMail,
  homeClick,
  homeOpened,
  interactive,
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

/** Field and its account Globex, as warp lists them for ana: Secret, which she may not see, is not among them. */
const ANAS: WireProject[] = [
  {
    name: "Field",
    title: "Field engineering",
    kind: "team",
    path: [{ name: "Field", title: "Field engineering" }],
    active: 1,
    active_at: "2026-10-09T00:30:00Z",
  },
  {
    name: "Globex",
    kind: "account",
    path: [{ name: "Field", title: "Field engineering" }, { name: "Globex" }],
    active: 0,
  },
];

/** An app that is the client "app", mapping USER to ana, with the console's address unless it is "". */
function mapping(publicUrl = CONSOLE): World {
  const w = world({ clientId: "app", ...(publicUrl ? { publicUrl } : {}) });
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
  assert.deepEqual(headers(blocks), [
    "Hi Ana, I'm Warp.",
    defaultCopy.home.needsYou,
    defaultCopy.home.whatsNew,
    defaultCopy.home.yourProjects,
  ]);
  const [mention] = under(blocks, defaultCopy.home.needsYou);
  assert.match(mention?.text?.text ?? "", /^\*Ana\* mentioned you in #renewals/);
  assert.match(mention?.text?.text ?? "", /https:\/\/warp\.test\/inbox\?message=m-mention/);

  const [news] = under(blocks, defaultCopy.home.whatsNew);
  assert.match(news?.text?.text ?? "", /^\*Field offsite\*\nUnread · Posted in Field engineering/);
  assert.equal(news?.accessory?.url, "https://warp.test/projects/Field?message=m-news");

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
