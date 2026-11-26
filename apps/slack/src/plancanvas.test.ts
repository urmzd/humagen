import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import type { Turn } from "@humagen/core";
import { codingTurn, doneTurn, streamingTurn } from "@humagen/core/fixtures";
import {
  BOT,
  CHANNEL,
  DM,
  FakeHumagen,
  FakeSlack,
  interactive,
  mappedWorld,
  OTHER_USER,
  settle,
  USER,
  type World,
} from "./harness.ts";
import { Names } from "./history.ts";
import {
  type CanvasComment,
  filePlanStore,
  hasPlan,
  memoryPlanStore,
  PLAN,
  PLAN_VIEW_KEY,
  PlanCanvas,
  planMarkdown,
  planStorePath,
  planWords,
  unseenComments,
  withComments,
} from "./plancanvas.ts";

/** A Slack that also answers the calls a plan canvas reads. */
class CanvasSlack extends FakeSlack {
  comments: CanvasComment[] = [];
  views: { view_key?: string; canvas_id?: string }[] = [];
  override async call<T>(method: string, args: Record<string, unknown> = {}): Promise<T> {
    const done = await super.call<T>(method, args);
    if (method === "agents.conversations.getCanvas") {
      return { ok: true, content: "# Plan", comments: this.comments } as T;
    }
    if (method === "agents.conversations.listViews") return { ok: true, views: this.views } as T;
    return done;
  }
}

const plan = (over: Partial<Turn> = {}): Turn => ({
  ...streamingTurn,
  planTitle: "Fixing the refund migration",
  agent: { name: "deploy" },
  status: "working",
  tasks: [
    { id: "t1", title: "Read the migration", status: "complete" },
    { id: "t2", title: "Write the fix", status: "in_progress" },
    { id: "t3", title: "Run the tests", status: "pending" },
  ],
  ...over,
});

const canvas = (slack = new CanvasSlack(), store = memoryPlanStore(), now = () => 0) => {
  const logs: string[] = [];
  const plans = new PlanCanvas(slack, store, { botUser: BOT, log: (l) => logs.push(l), now });
  return { slack, store, plans, logs };
};

const PLAN_CALLS =
  /^(canvases\.|agents\.conversations\.(setView|setCanvasContent|listViews|getCanvas))/;
const planCalls = (slack: FakeSlack) => slack.methods.filter((m) => PLAN_CALLS.test(m));

// The markdown

test("a plan reads as a checklist: not started, in progress, done", () => {
  assert.equal(
    planMarkdown(plan()),
    [
      "# Fixing the refund migration",
      "",
      "**deploy** is working on this.",
      "",
      "- [x] Read the migration",
      "- [ ] Write the fix *(in progress)*",
      "- [ ] Run the tests",
      "",
    ].join("\n"),
  );
});

test("a step's line does not change with the rest of the plan, so its comments stay", () => {
  const before = (planMarkdown(plan()) ?? "").split("\n");
  const after = (
    planMarkdown(
      plan({
        status: "done",
        tasks: plan().tasks.map((t) => (t.id === "t2" ? { ...t, status: "complete" } : t)),
      }),
    ) ?? ""
  ).split("\n");
  assert.equal(after[0], before[0]);
  assert.equal(after[4], before[4]);
  assert.equal(after[6], before[6]);
  assert.equal(after[2], "**deploy** finished this.");
  assert.equal(after[5], "- [x] Write the fix");
});

test("how a plan ended, and a step that waits, failed or was refused, are said in words", () => {
  const md = planMarkdown(
    plan({
      status: "failed",
      agent: { name: "" },
      tasks: [
        { id: "a", title: "Ask to deploy", status: "waiting" },
        { id: "b", title: "Drop the table", status: "denied" },
        { id: "c", title: "Run\n  the tests", status: "error" },
      ],
    }),
  );
  assert.match(md ?? "", /^The agent could not finish this\.$/m);
  assert.match(md ?? "", /^- \[ \] Ask to deploy \*\(waiting on a person\)\*$/m);
  assert.match(md ?? "", /^- \[ \] Drop the table \*\(not allowed\)\*$/m);
  assert.match(md ?? "", /^- \[ \] Run the tests \*\(failed\)\*$/m);
  assert.match(planMarkdown(plan({ status: "stopped" })) ?? "", /was stopped before finishing/);
});

test("a turn without a plan title, or without steps, has no plan", () => {
  assert.equal(hasPlan(plan()), true);
  assert.equal(planMarkdown(plan({ planTitle: undefined })), null);
  assert.equal(planMarkdown(plan({ planTitle: "  " })), null);
  assert.equal(planMarkdown(plan({ tasks: [] })), null);
  // The shared fixtures: a plan-shaped turn has one, a timeline turn does not.
  assert.equal(hasPlan(codingTurn), true);
  assert.equal(hasPlan({ ...doneTurn, planTitle: undefined }), false);
});

test("a very long plan is cut, and says how much is left out", () => {
  const tasks = Array.from({ length: PLAN.tasks + 2 }, (_, i) => ({
    id: `t${i}`,
    title: `Step ${i}`,
    status: "pending" as const,
  }));
  const md = planMarkdown(plan({ tasks })) ?? "";
  assert.equal(md.match(/^- \[ \]/gm)?.length, PLAN.tasks);
  assert.match(md, /\*2 more steps are not shown\.\*/);
});

// The canvas

test("the first plan in a code channel makes a canvas and attaches it as the Plan tab for comments", async () => {
  const { slack, store, plans } = canvas();
  const md = planMarkdown(plan()) ?? "";
  assert.equal(await plans.publish(CHANNEL, md), "created");
  assert.deepEqual(planCalls(slack), [
    "agents.conversations.listViews",
    "canvases.create",
    "agents.conversations.setView",
  ]);
  assert.deepEqual(slack.of("canvases.create")[0], {
    title: "Plan",
    document_content: { type: "markdown", markdown: md },
  });
  assert.deepEqual(slack.of("agents.conversations.setView")[0], {
    channel_id: CHANNEL,
    type: "canvas",
    view_key: PLAN_VIEW_KEY,
    name: "Plan",
    canvas_id: "F01",
    access_level: "comment",
  });
  assert.deepEqual(store.get(CHANNEL), { canvas: "F01", seen: [] });
});

test("a later plan is written to the same canvas in place", async () => {
  const { slack, plans } = canvas();
  await plans.publish(CHANNEL, planMarkdown(plan()) ?? "");
  const next = planMarkdown(plan({ status: "done" })) ?? "";
  assert.equal(await plans.publish(CHANNEL, next), "updated");
  assert.equal(slack.of("canvases.create").length, 1);
  assert.equal(slack.of("agents.conversations.setView").length, 1);
  assert.deepEqual(slack.of("agents.conversations.setCanvasContent"), [
    { channel: CHANNEL, canvas_id: "F01", content: next },
  ]);
});

test("an ordinary channel gets no canvas, is asked once, and says nothing", async () => {
  const { slack, store, plans, logs } = canvas();
  slack.failing["agents.conversations.listViews"] = "not_agent_channel";
  assert.equal(await plans.publish(CHANNEL, "# a"), "skipped");
  assert.equal(await plans.publish(CHANNEL, "# b"), "skipped");
  assert.deepEqual(planCalls(slack), ["agents.conversations.listViews"]);
  assert.equal(store.get(CHANNEL), undefined);
  assert.deepEqual(logs, []);
});

test("a channel Slack refused is asked again after it has rested", async () => {
  let now = 0;
  const { slack, plans } = canvas(new CanvasSlack(), memoryPlanStore(), () => now);
  slack.failing["agents.conversations.listViews"] = "not_agent_channel";
  await plans.publish(CHANNEL, "# a");
  now = PLAN.restMs + 1;
  delete slack.failing["agents.conversations.listViews"];
  assert.equal(await plans.publish(CHANNEL, "# a"), "created");
});

test("a direct message is never asked about", async () => {
  const { slack, plans } = canvas();
  assert.equal(await plans.publish(DM, "# a"), "skipped");
  plans.follow("run-1", DM);
  await plans.settle("run-1", plan());
  assert.deepEqual(slack.calls, []);
});

test("a canvas Slack will not attach as a tab is taken away again", async () => {
  const { slack, store, plans, logs } = canvas();
  slack.failing["agents.conversations.setView"] = "too_many_views";
  assert.equal(await plans.publish(CHANNEL, "# a"), "skipped");
  assert.deepEqual(slack.of("canvases.delete"), [{ canvas_id: "F01" }]);
  assert.equal(store.get(CHANNEL), undefined);
  assert.deepEqual(logs, [`plan canvas tab refused in ${CHANNEL}: too_many_views`]);
});

test("a refused update keeps the canvas and costs nothing but the write", async () => {
  const { slack, store, plans } = canvas();
  await plans.publish(CHANNEL, "# a");
  slack.failing["agents.conversations.setCanvasContent"] = "ratelimited";
  assert.equal(await plans.publish(CHANNEL, "# b"), "skipped");
  assert.deepEqual(store.get(CHANNEL), { canvas: "F01", seen: [] });
  delete slack.failing["agents.conversations.setCanvasContent"];
  assert.equal(await plans.publish(CHANNEL, "# b"), "updated");
});

test("a canvas that was deleted is made again", async () => {
  const { slack, plans } = canvas();
  await plans.publish(CHANNEL, "# a");
  slack.failing["agents.conversations.setCanvasContent"] = "canvas_not_found";
  assert.equal(await plans.publish(CHANNEL, "# b"), "created");
  assert.equal(slack.of("canvases.create").length, 2);
});

test("a Plan tab the channel already has is taken up, not made twice", async () => {
  const { slack, store, plans } = canvas();
  slack.views = [{ view_key: "diff" }, { view_key: PLAN_VIEW_KEY, canvas_id: "F77" }];
  assert.equal(await plans.publish(CHANNEL, "# a"), "updated");
  assert.equal(slack.of("canvases.create").length, 0);
  assert.deepEqual(slack.of("agents.conversations.setCanvasContent"), [
    { channel: CHANNEL, canvas_id: "F77", content: "# a" },
  ]);
  assert.deepEqual(store.get(CHANNEL), { canvas: "F77", seen: [] });
});

test("a run's turns keep the canvas current, and the same plan is not written twice", async () => {
  const { slack, plans } = canvas();
  plans.follow("run-1", CHANNEL);
  plans.offer("run-1", plan({ planTitle: undefined }));
  assert.deepEqual(slack.calls, []);
  plans.offer("run-1", plan());
  plans.offer("run-1", plan());
  const done = plan({
    status: "done",
    tasks: plan().tasks.map((t) => ({ ...t, status: "complete" as const })),
  });
  await plans.settle("run-1", done);
  await plans.settle("run-1", done);
  assert.equal(slack.of("canvases.create").length, 1);
  const writes = slack.of("agents.conversations.setCanvasContent");
  assert.equal(writes.length, 1);
  assert.equal(writes[0]?.content, planMarkdown(done));
  // The run is over: nothing more is written for it.
  plans.offer("run-1", plan());
  await settle();
  assert.equal(slack.of("agents.conversations.setCanvasContent").length, 1);
});

test("plans that arrive while one is being written wait, and only the newest goes out", async () => {
  const { slack, plans } = canvas();
  plans.follow("run-1", CHANNEL);
  const titles = ["one", "two", "three"].map((t) => plan({ planTitle: t }));
  for (const t of titles) plans.offer("run-1", t);
  await plans.settle("run-1", titles[2] as Turn);
  const written = [
    ...slack
      .of("canvases.create")
      .map((a) => (a.document_content as { markdown: string }).markdown),
    ...slack.of("agents.conversations.setCanvasContent").map((a) => String(a.content)),
  ];
  assert.deepEqual(written, [planMarkdown(titles[0] as Turn), planMarkdown(titles[2] as Turn)]);
});

// The record

test("the record of plan canvases is kept in a file beside the stream journal", () => {
  const dir = mkdtempSync(join(tmpdir(), "plans-"));
  try {
    const path = planStorePath(join(dir, "streams-T1.json"), "T1");
    assert.equal(path, join(dir, "plan-canvases-T1.json"));
    const store = filePlanStore(path);
    store.set(CHANNEL, { canvas: "F01", seen: ["c1"] });
    assert.deepEqual(JSON.parse(readFileSync(path, "utf8")), {
      plans: { [CHANNEL]: { canvas: "F01", seen: ["c1"] } },
    });
    assert.deepEqual(filePlanStore(path).get(CHANNEL), { canvas: "F01", seen: ["c1"] });
    store.delete(CHANNEL);
    assert.equal(filePlanStore(path).get(CHANNEL), undefined);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("a record that cannot be read is reported and starts empty", () => {
  const logs: string[] = [];
  const store = filePlanStore(tmpdir(), (l) => logs.push(l));
  assert.equal(store.get(CHANNEL), undefined);
  assert.match(logs[0] ?? "", /could not be read, starting empty/);
});

// Comments

const COMMENTS: CanvasComment[] = [
  {
    id: "c1",
    user_id: USER,
    quoted_text: "Write the fix",
    text: "do this on a branch,\nnot on main",
    replies: [{ id: "r1", user_id: OTHER_USER, text: "agreed" }],
  },
  { id: "c2", user_id: BOT, quoted_text: "Run the tests", text: "noted" },
  { id: "c3", user_id: OTHER_USER, text: "   " },
];

test("only people's comments the agent has not been shown are picked", () => {
  const fresh = unseenComments(COMMENTS, new Set(), BOT);
  assert.deepEqual(
    fresh.map((r) => [r.key, r.fresh, r.replies.map((x) => x.key)]),
    [["c1", true, ["c1/r1"]]],
  );
  assert.deepEqual(unseenComments(COMMENTS, new Set(["c1", "c1/r1"]), BOT), []);
  // A new reply under a comment shown before brings the comment back for context.
  const again = unseenComments(COMMENTS, new Set(["c1"]), BOT);
  assert.deepEqual(
    again.map((r) => [r.key, r.fresh, r.replies.map((x) => x.key)]),
    [["c1", false, ["c1/r1"]]],
  );
});

test("comments without an id are told apart by who said what about which line", () => {
  const a = { user_id: USER, quoted_text: "Write the fix", text: "why?" };
  const [first] = unseenComments([a], new Set(), BOT);
  assert.ok(first);
  assert.deepEqual(unseenComments([{ ...a }], new Set([first.key]), BOT), []);
  assert.equal(unseenComments([{ ...a, text: "why not?" }], new Set([first.key]), BOT).length, 1);
});

test("comments go ahead of the question, marked as people's comments on the plan", () => {
  const got = withComments(
    "ship it",
    unseenComments(COMMENTS, new Set(), BOT),
    new Map([[USER, "Ada"]]),
  );
  assert.equal(
    got.text,
    [
      planWords.commentsOpen,
      `- Ada (<@${USER}>) on "Write the fix": do this on a branch, not on main`,
      `  - reply from <@${OTHER_USER}>: agreed`,
      planWords.commentsClose,
      "",
      "ship it",
    ].join("\n"),
  );
  assert.deepEqual(got.shown, ["c1", "c1/r1"]);
  assert.match(planWords.commentsOpen, /people's feedback on the plan/);
  assert.deepEqual(withComments("ship it", []), { text: "ship it", shown: [] });
});

test("more comments than one turn carries: the oldest go, the rest wait for the next turn", () => {
  const many = Array.from({ length: PLAN.comments + 3 }, (_, i) => ({
    id: `c${i}`,
    user_id: USER,
    text: `note ${i}`,
  }));
  const first = withComments("q", unseenComments(many, new Set(), BOT));
  assert.equal(first.shown.length, PLAN.comments);
  assert.equal(first.shown[0], "c0");
  const rest = withComments("q", unseenComments(many, new Set(first.shown), BOT));
  assert.deepEqual(rest.shown, ["c20", "c21", "c22"]);
});

test("a channel without a plan canvas is not asked for comments", async () => {
  const { slack, plans } = canvas();
  const got = await plans.comments(CHANNEL);
  assert.equal(got.count, 0);
  assert.equal(got.with("q"), "q");
  assert.deepEqual(slack.calls, []);
});

test("comments are read from the channel's canvas, and delivered ones are not sent again", async () => {
  const { slack, store, plans } = canvas();
  slack.comments = COMMENTS;
  store.set(CHANNEL, { canvas: "F01", seen: [] });
  const got = await plans.comments(CHANNEL);
  assert.deepEqual(slack.of("agents.conversations.getCanvas"), [
    { channel: CHANNEL, canvas_id: "F01", include_resolved: false },
  ]);
  assert.equal(got.count, 2);
  assert.match(got.with("ship it"), /on "Write the fix": do this on a branch, not on main/);
  assert.ok(got.with("ship it").endsWith(`${planWords.commentsClose}\n\nship it`));
  // Not yet delivered: a turn that never started leaves them for the next.
  assert.equal((await plans.comments(CHANNEL)).count, 2);
  got.delivered();
  assert.deepEqual(store.get(CHANNEL), { canvas: "F01", seen: ["c1", "c1/r1"] });
  assert.equal((await plans.comments(CHANNEL)).count, 0);
  // A resolved comment is forgotten, so the record does not grow without end.
  slack.comments = [{ id: "c9", user_id: USER, text: "one more" }];
  const next = await plans.comments(CHANNEL);
  assert.equal(next.count, 1);
  next.delivered();
  assert.deepEqual(store.get(CHANNEL), { canvas: "F01", seen: ["c9"] });
});

test("comments Slack will not give are no comments, and never fail the turn", async () => {
  const { slack, store, plans } = canvas();
  store.set(CHANNEL, { canvas: "F01", seen: [] });
  slack.failing["agents.conversations.getCanvas"] = "not_agent_channel";
  const got = await plans.comments(CHANNEL);
  assert.equal(got.with("q"), "q");
  // The channel rests: it is not asked again at once.
  await plans.comments(CHANNEL);
  assert.equal(slack.of("agents.conversations.getCanvas").length, 1);
});

test("commenters are named when the app may read names", async () => {
  const slack = new CanvasSlack();
  slack.users.set(USER, "Ada");
  slack.comments = [{ id: "c1", user_id: USER, text: "why?" }];
  const store = memoryPlanStore();
  store.set(CHANNEL, { canvas: "F01", seen: [] });
  const plans = new PlanCanvas(slack, store, { botUser: BOT, names: new Names(slack) });
  assert.match(
    (await plans.comments(CHANNEL)).with("q"),
    new RegExp(`- Ada \\(<@${USER}>\\): why\\?`),
  );
});

// In the app

/**
 * Ana asks about olu's message: the app asks it, for nobody, so it carries
 * what people wrote on the plan. A person's own message is theirs, posted
 * into the session its Slack thread is.
 */
const askAbout = (text: string, ts: string) =>
  interactive({
    type: "message_action",
    callback_id: "aui_ask_about",
    channel: { id: CHANNEL },
    message: { ts, text, user: OTHER_USER },
  });

async function finish(w: World) {
  w.humagen.emit(w.humagen.last.id, "run.accepted.v1");
  w.humagen.emit(w.humagen.last.id, "run.completed.v1", { text: "ok" });
  await settle();
  await w.app.idle();
}

test("a question in a channel with a plan canvas carries people's new comments, once", async () => {
  const slack = new CanvasSlack();
  slack.comments = COMMENTS;
  const plans = memoryPlanStore();
  plans.set(CHANNEL, { canvas: "F01", seen: [] });
  const w = mappedWorld({ plans }, undefined, undefined, { slack, humagen: new FakeHumagen() });
  await w.app.handle(askAbout("ship it", "1700000100.000100"));
  await settle();
  const first = w.humagen.asked[0];
  assert.ok(first);
  assert.ok(first.text.startsWith(planWords.commentsOpen), first.text);
  assert.ok(first.text.endsWith("\n\nship it"), first.text);
  assert.deepEqual(plans.get(CHANNEL)?.seen, ["c1", "c1/r1"]);
  await finish(w);

  await w.app.handle(askAbout("and now?", "1700000200.000100"));
  await settle();
  assert.equal(w.humagen.asked[1]?.text, "and now?");
  await finish(w);
});

test("a channel without a plan canvas is asked nothing new by a turn", async () => {
  const w = mappedWorld();
  await w.app.handle(askAbout("ship it", "1700000300.000100"));
  await settle();
  assert.equal(w.humagen.asked[0]?.text, "ship it");
  await finish(w);
  assert.deepEqual(planCalls(w.slack), []);
});
