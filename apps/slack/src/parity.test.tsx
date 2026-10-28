import "./react.ts";
import assert from "node:assert/strict";
import { test } from "node:test";
import {
  follow,
  fromServer,
  type PlatformMessage,
  type Run,
  type RunEvent,
  replay,
  toRequest,
  updatesOf,
  type WireProject,
} from "@humagen/adapter";
import { defaultCopy, Home, MessageView, TurnView } from "@humagen/components";
import { ActionId, type MessagePart, partResolution, type Turn } from "@humagen/core";
import { homeView, renderToSlack } from "@humagen/render-slack";
import { WebSurface } from "@humagen/render-web";
import type { ReactElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { forSlack } from "./app.tsx";
import {
  alertMail,
  announcementMail,
  budgetMail,
  draftMail,
  failureMail,
  memoryMail,
  mentionMail,
  QUESTIONS,
  questionMail,
} from "./harness.ts";
import { homeModel } from "./home.ts";

/**
 * One state, two renderers. A recorded run goes through the shared adapter
 * once. The console and the Slack app both render that one turn with the same
 * humagen components, so what a person reads is the same in both places.
 */

const RUN = "api-svc-slack-slack-T01ABCDEF-C01ABCDEF-1700000000-000100";
const run = (over: Partial<Run> = {}): Run => ({
  id: RUN,
  tenant: "acme",
  agent: "lead",
  rev: 1,
  origin: "api",
  text: "what changed in the last deploy?",
  root: RUN,
  depth: 0,
  thread: RUN,
  lease: 0,
  granted: 0,
  calls: 0,
  status: "running",
  created_at: "2026-10-09T00:00:00Z",
  updated_at: "2026-10-09T00:00:00Z",
  ...over,
});

let seq = 0;
const ev = (type: string, data: unknown = {}): RunEvent => {
  const n = ++seq;
  return {
    id: `e${n}`,
    source: "humagen:acme",
    type,
    channel: "humagen://acme",
    time: "2026-10-09T00:00:01Z",
    hops: 0,
    tenant: "acme",
    run: RUN,
    seq: n,
    agent: "lead",
    version: "1.0.0",
    data,
  } as RunEvent;
};

/** The turn the Slack app holds: events folded in one at a time, as they arrive. */
function live(events: RunEvent[], record?: Run): Turn {
  const f = follow(run());
  for (const e of events) f.apply(e);
  if (record) f.settle(record);
  return f.turn;
}

/** What Slack shows: every string in every block. */
function slack(element: ReactElement): string {
  const out = renderToSlack(element);
  return JSON.stringify(out.pages).replace(/\\n/g, " ");
}

/** What the console shows: the markup's text. */
function web(element: ReactElement): string {
  return renderToStaticMarkup(<WebSurface>{element}</WebSurface>)
    .replace(/<[^>]+>/g, " ")
    .replace(/&#x27;/g, "'")
    .replace(/&quot;/g, '"')
    .replace(/&amp;/g, "&")
    .replace(/\s+/g, " ");
}

/** Text as both surfaces reduce it: no markup, no emphasis, single spaces. */
const plain = (t: string) =>
  t
    .replace(/[*_`>]/g, "")
    .replace(/\[([^\]]*)\]\([^)]*\)/g, "$1")
    .replace(/\s+/g, " ")
    .trim();

/**
 * Everything a turn holds that a person must be able to read, taken from the
 * model and not from either renderer. A renderer that drops a task's output,
 * a citation, a file, the failure or the stop fails the comparison.
 */
function turnContent(turn: Turn): string[] {
  return [
    ...turn.text.split(/\n+/),
    ...turn.tasks.flatMap((t) => [t.title, t.output ?? ""]),
    ...turn.citations.map((c) => c.title),
    ...(turn.attachments ?? []).map((a) => ("title" in a ? (a.title ?? "") : "")),
    ...turn.notices.flatMap((n) => [n.title ?? "", n.body]),
    turn.failure?.message ?? "",
    turn.status === "stopped" ? defaultCopy.stopped : "",
    turn.stop ? defaultCopy.stopPhase[turn.stop] : "",
    defaultCopy.disclaimer(turn.agent.name),
  ]
    .map(plain)
    .filter(Boolean);
}

/** Everything an inbox message holds that a person must be able to read. */
function messageContent(message: PlatformMessage): string[] {
  const part = (p: MessagePart): string[] => {
    const done = partResolution(p);
    const record = done?.detail ? [done.detail] : [];
    switch (p.type) {
      case "text":
        return [p.text];
      case "memory":
        return [...p.proposals.map((n) => n.text), ...record];
      case "alert":
        return [p.alert.title, p.alert.body ?? "", ...record];
      case "failure":
        return [p.failure.message, ...record];
      case "budget":
        return [defaultCopy.budgetTitle, ...record];
      default:
        return record;
    }
  };
  return [message.subject, ...message.parts.flatMap(part)].map(plain).filter(Boolean);
}

/** What each surface is missing of some content. Both lists must be empty. */
function missing(slackText: string, webText: string, content: string[]) {
  const s = plain(slackText);
  const w = plain(webText);
  return {
    slack: content.filter((c) => !s.includes(c)),
    web: content.filter((c) => !w.includes(c)),
  };
}

/** Both surfaces show all of a turn. */
function bothShow(turn: Turn) {
  const el = <TurnView turn={turn} />;
  assert.deepEqual(missing(slack(el), web(el), turnContent(turn)), { slack: [], web: [] });
}

/** Both surfaces say each of these. */
function bothSay(element: ReactElement, ...phrases: string[]) {
  const s = slack(element);
  const w = web(element);
  for (const phrase of phrases) {
    assert.ok(s.includes(phrase), `Slack does not say "${phrase}"`);
    assert.ok(w.includes(phrase), `the console does not say "${phrase}"`);
  }
}

const answered = [
  ev("run.accepted.v1", { rev: 1 }),
  ev("run.started.v1", { attempt: 1 }),
  ev("run.delta.tool.call.v1", { id: "t1", name: "github.search", phase: "requested" }),
  ev("run.delta.tool.done.v1", { id: "t1", result: "3 commits" }),
  ev("run.delta.citation.v1", {
    label: "c1",
    index: 1,
    text: "Deploy 4812 went out at 14:52.",
    source: { document_title: "Deploy log", source_uri: "https://deploys.example.com/4812" },
  }),
  ev("run.delta.tool.call.v1", { id: "t2", name: "publish_artifact" }),
  ev("run.action.taken.v1", {
    name: "publish_artifact",
    data: {
      id: "art-1",
      title: "deploy-notes.md",
      kind: "document",
      mime: "text/markdown",
      size: 120,
      version: 1,
    },
  }),
  ev("run.delta.tool.done.v1", { id: "t2", result: "published" }),
  ev("run.delta.text.v1", { text: "Three commits " }),
  ev("run.delta.text.v1", { text: "went out." }),
  ev("run.completed.v1", { text: "Three commits went out." }),
];

test("a streamed answer with a tool call is one turn, rendered by both surfaces", () => {
  const turn = live(answered);
  // The console rebuilds a run with `replay`. It is the same state the Slack app streamed.
  assert.deepEqual(replay(run(), answered), turn);
  assert.equal(turn.status, "done");
  assert.equal(turn.text, "Three commits went out.");
  assert.deepEqual(
    turn.tasks.map((t) => [t.title, t.status, t.output]),
    [
      ["Github search", "complete", "3 commits"],
      ["Publish artifact", "complete", "published"],
    ],
  );
  assert.equal(turn.citations.length, 1);
  assert.equal(turn.attachments?.length, 1);
  bothShow(turn);
});

test("a failed run is the same failure in both", () => {
  const events = [
    ev("run.accepted.v1"),
    ev("run.failed.v1", { error: "timeout", terminal: false }),
    ev("run.failed.v1", { error: "agent: step 2\nprovider: 529 overloaded", terminal: true }),
  ];
  const turn = live(events);
  assert.deepEqual(replay(run(), events), turn);
  assert.equal(turn.status, "failed");
  assert.deepEqual(turn.failure, {
    kind: "internal",
    message: "provider: 529 overloaded",
    reference: RUN,
  });
  // The reference is a copy chip on the web and a line of text on Slack: the same id either way.
  bothSay(<TurnView turn={turn} />, defaultCopy.retry, RUN);
  bothShow(turn);

  // A run whose final event is gone is ended from its record, to the same state.
  const fromRecord = live(
    [ev("run.accepted.v1")],
    run({ status: "failed", error: "agent: step 2\nprovider: 529 overloaded" }),
  );
  assert.deepEqual(fromRecord.failure, turn.failure);
  assert.equal(fromRecord.status, "failed");
});

test("a stopped run is stopped in both, with nothing after the stop", () => {
  const events = [ev("run.accepted.v1"), ev("run.delta.text.v1", { text: "Part one. " })];
  const turn = live(events, run({ status: "stopped", cause: "Stopped by svc-slack" }));
  assert.equal(turn.status, "stopped");
  assert.equal(turn.stop, "confirmed");
  assert.equal(turn.text, "Part one. ");
  bothSay(<TurnView turn={turn} />, defaultCopy.resume);
  bothShow(turn);

  // A stopped run that settled from its record shows the partial answer it carries, in both.
  const fromRecord = live(
    [ev("run.accepted.v1")],
    run({ status: "stopped", answer: "Half an answer" }),
  );
  assert.equal(fromRecord.status, "stopped");
  assert.equal(fromRecord.text, "Half an answer");
  bothSay(<TurnView turn={fromRecord} />, "Half an answer");
  bothShow(fromRecord);

  // Once stopped, a turn does not move again, whatever a later record says.
  const f = follow(run());
  for (const e of events) f.apply(e);
  f.settle(run({ status: "stopped" }));
  assert.equal(
    f.settle(run({ status: "completed", answer: "Part one. Part two." })).text,
    "Part one. ",
  );
});

test("an inbox item is one message, with the same choices in both", () => {
  const memory = fromServer(memoryMail(RUN));
  bothSay(
    <MessageView message={memory} />,
    "2 proposed memories for lead",
    "Deploys are frozen on Fridays.",
    "The staging cluster is rebuilt every Monday.",
    defaultCopy.discardAll,
  );
  // The keep button counts the ticked notes on the web ("Keep 2 of 2"); Slack cannot update a label as boxes are ticked.
  assert.ok(slack(<MessageView message={memory} />).includes(defaultCopy.keepSelected));
  assert.match(web(<MessageView message={memory} />), /Keep 2 of 2/);
  const failure = fromServer(failureMail(RUN));
  bothSay(
    <MessageView message={failure} />,
    "lead failed",
    "provider: 529 overloaded",
    defaultCopy.retry,
  );

  // Once decided, both show the record instead of the buttons.
  const wire = memoryMail(RUN);
  const parts = wire.parts as Record<string, unknown>[];
  const decided = fromServer({
    ...wire,
    state: "resolved",
    parts: [
      {
        ...parts[0],
        state: "resolved",
        actions: [],
        outcome: { action: "keep", by: "dana", at: "2026-10-09T00:20:00Z", note: "Kept 1 of 2" },
      },
    ],
  });
  bothSay(<MessageView message={decided} />, "Kept 1 of 2");
  assert.ok(!slack(<MessageView message={decided} />).includes(defaultCopy.keepSelected));
  assert.ok(!/Keep \d/.test(web(<MessageView message={decided} />)));
});

test("every kind of inbox message holds the same content on both, as each surface is given it", () => {
  for (const wire of [
    memoryMail(RUN),
    failureMail(RUN),
    alertMail(),
    budgetMail(),
    mentionMail(),
  ]) {
    const message = fromServer(wire);
    // The console renders the message as the platform sends it, Slack as the app hands it over.
    const s = slack(<MessageView message={forSlack(message)} />);
    const w = web(<MessageView message={message} />);
    assert.deepEqual(
      missing(s, w, messageContent(message)),
      { slack: [], web: [] },
      String(wire.id),
    );
  }
});

test("a mention is a message to reply to, the same on both, and replying posts as the reader", () => {
  const mention = fromServer(mentionMail());
  assert.equal(mention.template, "mention");
  bothSay(
    <MessageView message={mention} />,
    "Ana mentioned you in #renewals",
    "@dana can you confirm the renewal date with Globex?",
    defaultCopy.template.mention,
  );
  const part = mention.parts[0];
  assert.equal(part?.type, "reply");
  // Replying is a resolve with the reader's words; nothing names an author.
  const request = toRequest(mention, {
    id: ActionId.reply,
    payload: { turnId: mention.id, target: part?.id ?? "", text: "It renews in May." },
  });
  assert.deepEqual(request, {
    call: "resolve",
    partId: "p1",
    action: "reply",
    values: { text: "It renews in May." },
  });
  // Once replied, both show the record.
  const wire = mentionMail();
  const replied = fromServer({
    ...wire,
    state: "resolved",
    parts: [
      {
        ...(wire.parts as Record<string, unknown>[])[0],
        state: "resolved",
        actions: [],
        outcome: {
          action: "reply",
          by: "dana",
          at: "2026-10-09T00:50:00Z",
          note: "Replied as m-4",
        },
      },
    ],
  });
  bothSay(<MessageView message={replied} />, "Replied as m-4");
});

/** The buttons a rendering offers, by label: what a person can do there. */
function slackButtons(element: ReactElement): string[] {
  const pages = renderToSlack(element).pages.flat() as {
    type: string;
    elements?: { type: string; text?: { text: string } }[];
  }[];
  return pages
    .filter((b) => b.type === "actions")
    .flatMap((b) => b.elements ?? [])
    .filter((e) => e.type === "button")
    .map((e) => e.text?.text ?? "")
    .sort();
}
function webButtons(element: ReactElement): string[] {
  const html = renderToStaticMarkup(<WebSurface>{element}</WebSurface>);
  return [...html.matchAll(/<button[^>]*>([\s\S]*?)<\/button>/g)]
    .map((m) => (m[1] ?? "").replace(/<[^>]+>/g, "").trim())
    .filter(Boolean)
    .sort();
}

const asked = (kind: keyof typeof QUESTIONS) => [
  ev("run.accepted.v1", { rev: 1 }),
  ev("run.started.v1", { attempt: 1 }),
  ...(kind === "approval"
    ? [ev("run.delta.tool.call.v1", { id: "call-1", name: "deploy", phase: "requested" })]
    : []),
  ev("run.parked.v1", { reason: "waiting on a person", question: QUESTIONS[kind] }),
];
const at = "2026-10-09T00:20:00Z";
const answer = {
  approval: {
    run: { question_id: "q-approve", decision: "approve", by: "dana", at },
    inbox: { action: "approve", by: "dana", at },
    says: "Approved by dana",
  },
  choice: {
    run: { question_id: "q-choose", choice: "eu-west", by: "dana", at },
    inbox: { action: "choose", by: "dana", at, note: "eu-west" },
    says: "dana chose eu-west",
  },
  reply: {
    run: { question_id: "q-reply", text: "OPS-42", by: "dana", at },
    inbox: { action: "reply", by: "dana", at, note: "OPS-42" },
    says: "OPS-42",
  },
} as const;

for (const kind of ["approval", "choice", "reply"] as const) {
  test(`a ${kind} a run waits on reads and answers the same on both, on the run and in the inbox`, () => {
    // On the run: waiting, not working, with the question and its buttons.
    const waiting = live(asked(kind));
    assert.equal(waiting.status, "waiting");
    assert.equal(waiting.activity, "waiting");
    assert.deepEqual(replay(run(), asked(kind)), waiting, "the console rebuilds the same state");
    const onRun = <TurnView turn={waiting} />;
    bothSay(onRun, QUESTIONS[kind].prompt);
    if (kind === "approval") bothSay(onRun, defaultCopy.approve, defaultCopy.cancel);
    if (kind === "choice") bothSay(onRun, "us-east", "eu-west");
    if (kind === "reply") bothSay(onRun, defaultCopy.somethingElse);
    assert.deepEqual(
      slackButtons(onRun),
      webButtons(onRun).filter((b) => slackButtons(onRun).includes(b)),
    );

    // In the inbox: the same question, the same choices.
    const mail = fromServer(questionMail(RUN, kind));
    const inMail = <MessageView message={forSlack(mail)} />;
    bothSay(<MessageView message={mail} />, QUESTIONS[kind].prompt);
    if (kind === "approval") bothSay(inMail, defaultCopy.approve, defaultCopy.cancel);
    if (kind === "choice") bothSay(inMail, "us-east", "eu-west");
    if (kind === "reply") bothSay(inMail, defaultCopy.reply);

    // Answered: the record replaces the buttons, in the same words, wherever it is read.
    const settled = live([
      ...asked(kind),
      ev("run.resumed.v1", { attempt: 2, answer: answer[kind].run }),
    ]);
    assert.notEqual(settled.status, "waiting");
    const decided = fromServer(questionMail(RUN, kind, answer[kind].inbox));
    for (const el of [
      <TurnView key="turn" turn={settled} />,
      <MessageView key="message" message={decided} />,
    ]) {
      bothSay(el, answer[kind].says);
      assert.ok(!slack(el).includes(`"${defaultCopy.approve}"`), "no button is left to press");
    }
  });
}

test("an agent's draft reads the same on both, with Publish and Discard for its owner and its words inert", () => {
  const wire = {
    id: "m-12",
    kind: "announcement",
    subject: "Field shipped two fixes",
    from: { kind: "agent", name: "humagen-analyst" },
    at: "2026-10-21T09:00:00Z",
    updated_at: "2026-10-21T09:00:00Z",
    severity: "info",
    state: "open",
    unread: true,
    source: { project: "Field", run: "r1", agent: "humagen-analyst" },
    parts: [
      {
        id: "p1",
        type: "draft",
        state: "open",
        actions: ["publish", "discard"],
        draft: {
          project: "Field",
          agent: "humagen-analyst",
          run: "r1",
          subject: "Field shipped two fixes",
          text: "Read [this](https://evil.example) <!here>",
        },
      },
    ],
  };
  const message = fromServer(wire);
  bothSay(
    <MessageView message={message} />,
    "Field shipped two fixes",
    defaultCopy.draftTitle("humagen-analyst", "Field"),
  );
  assert.deepEqual(
    slackButtons(<MessageView message={message} />),
    [defaultCopy.discardDraft, defaultCopy.publish].sort(),
  );
  assert.deepEqual(
    webButtons(<MessageView message={message} />)
      .filter((b) => b === defaultCopy.publish || b === defaultCopy.discardDraft)
      .sort(),
    [defaultCopy.discardDraft, defaultCopy.publish].sort(),
  );
  // Nothing the agent wrote becomes a link or pings a channel.
  const s = slack(<MessageView message={message} />);
  assert.ok(!s.includes("<https://evil.example|this>") && !s.includes("<!here>"), s);
  assert.ok(!web(<MessageView message={message} />).includes('href="https://evil.example"'));
});

test("Home's What's new is made as the console makes it, and What's new and Your projects read the same on both", () => {
  const announcements = [
    announcementMail({
      id: "m-old",
      subject: "Kickoff notes",
      unread: false,
      at: "2026-10-08T00:00:00Z",
      updated_at: "2026-10-08T00:00:00Z",
    }),
    announcementMail(),
    // Posted in the workspace itself, and newer than the unread one.
    announcementMail({
      id: "m-ws",
      subject: "Holiday hours",
      unread: false,
      source: {},
      at: "2026-10-09T00:55:00Z",
      updated_at: "2026-10-09T00:55:00Z",
    }),
  ];
  const projects: WireProject[] = [
    { name: "Launch", kind: "team", path: [{ name: "Launch" }], active: 0, mine: true },
    {
      name: "Field",
      title: "Field engineering",
      kind: "team",
      path: [{ name: "Field", title: "Field engineering" }],
      active: 2,
      active_at: "2026-10-09T00:30:00Z",
      mine: true,
    },
    // Everyone's: she reads it, and it does not name her.
    {
      name: "News",
      kind: "company",
      path: [{ name: "News" }],
      active: 5,
      active_at: "2026-10-09T00:50:00Z",
      mine: false,
    },
  ];
  const href = (path: string) => `https://humagen.test${path}`;
  const model = homeModel({
    person: { id: "U01ANA", name: "Ana" },
    assistant: { name: "Humagen" },
    intro: "I hand your questions to this workspace's agents.",
    inbox: { messages: [], open: 0 },
    announcements,
    projects,
    workspace: "acme",
    build: false,
    href,
  });
  // The console's Home makes What's new with the same call, from the same reads (apps/web/src/App.tsx).
  const titles = new Map(projects.map((p) => [p.name, p.title || p.name]));
  assert.deepEqual(
    model.updates,
    updatesOf(
      announcements.map(fromServer),
      (name) => (name === "" ? "acme" : (titles.get(name) ?? name)),
      (name, id) => href(`/projects/${name || "~"}?message=${id}`),
    ),
  );
  // The console's Home has no Your projects: the ones that name her, the busiest first, each opening its page.
  assert.deepEqual(model.projects, [
    {
      name: "Field",
      title: "Field engineering",
      within: [],
      kind: "team",
      active: 2,
      activeAt: "2026-10-09T00:30:00Z",
      url: "https://humagen.test/projects/Field",
    },
    {
      name: "Launch",
      title: "Launch",
      within: [],
      kind: "team",
      active: 0,
      url: "https://humagen.test/projects/Launch",
    },
  ]);

  const el = <Home home={model} />;
  const onSlack = plain(JSON.stringify(homeView(el).view.blocks).replace(/\\n/g, " "));
  const onWeb = plain(web(el));
  const inOrder = (text: string, phrases: string[]) => {
    let at = 0;
    for (const p of phrases) {
      const found = text.indexOf(p, at);
      assert.ok(found >= 0, `"${p}" missing, or out of order, in ${text}`);
      at = found + p.length;
    }
  };
  // Unread first, then the newest; the busiest project first.
  const phrases = [
    defaultCopy.home.whatsNew,
    "Field offsite",
    "Holiday hours",
    "Posted in acme",
    "Kickoff notes",
    defaultCopy.home.yourProjects,
    "Field engineering",
    "2 running",
    "Launch",
  ];
  inOrder(onSlack, phrases);
  inOrder(onWeb, phrases);
});

test("Home's Needs you holds the same rows and the same total on both, an agent's draft among them and no announcement", () => {
  // A page of ana's open inbox, newest first, as humagen sends it: an agent's
  // draft waits on her as one of Field's owners, beside a posted announcement.
  const inbox = [
    draftMail(),
    mentionMail(),
    announcementMail(),
    budgetMail(),
    alertMail(),
    questionMail(RUN, "approval"),
    failureMail(RUN),
    memoryMail(RUN),
  ];
  // humagen counts what may need her across her whole inbox: more than the page
  // holds, the draft among them, the announcement not.
  const open = 9;
  const model = homeModel({
    person: { id: "U01ANA", name: "Ana" },
    assistant: { name: "Humagen" },
    intro: "I hand your questions to this workspace's agents.",
    inbox: { messages: inbox, open },
    // Her announcements, as an owner reads them: the draft has the same kind.
    announcements: [announcementMail(), draftMail()],
    projects: [],
    workspace: "acme",
    build: false,
    href: (path: string) => `https://humagen.test${path}`,
  });
  // The console's Home says "Needs you" with this same number (inboxCounts.open).
  assert.equal(model.notifications.inbox?.total, open);
  assert.deepEqual(
    model.notifications.items.map((n) => n.id),
    ["m-draft", "m-mention", "m-budget", "m-alert", "m-approval"],
  );
  assert.deepEqual(
    model.updates?.map((u) => u.id),
    ["m-news"],
  );

  const el = <Home home={model} />;
  const onSlack = plain(JSON.stringify(homeView(el).view.blocks).replace(/\\n/g, " "));
  const onWeb = plain(web(el));
  const rows = [
    defaultCopy.home.needsYou,
    "humagen-analyst Field shipped two fixes",
    "Ana mentioned you in #renewals",
    "Limits Project Launch reached its daily run limit",
    "oncall-failure-rate oncall is failing more than half its runs",
    // Above normal, so said in words on both; a normal row says nothing more.
    defaultCopy.home.urgency.critical as string,
    "lead wants to run deploy",
    defaultCopy.home.moreWaiting(open - 5),
    defaultCopy.home.whatsNew,
    "Field offsite",
  ];
  for (const [surface, text] of [
    ["Slack", onSlack],
    ["the console", onWeb],
  ] as const) {
    let at = 0;
    for (const p of rows) {
      const found = text.indexOf(p, at);
      assert.ok(found >= 0, `${surface}: "${p}" missing, or out of order, in ${text}`);
      at = found + p.length;
    }
    // Five rows and the rest counted, the same on both: nothing else is a row.
    assert.ok(!text.includes("lead failed"), `${surface} shows a sixth row`);
    assert.equal(
      text.split("Field shipped two fixes").length - 1,
      1,
      `${surface} shows the draft once`,
    );
  }
});

test("a Slack task links to the conversation rather than its individual run", () => {
  const record = run({ thread: "shared-thread" });
  const f = follow(record, { href: (path) => `https://humagen.test${path}` });
  const view = renderToSlack(
    <TurnView turn={f.settle({ ...record, status: "completed", answer: "Done" })} />,
  );
  assert.match(
    JSON.stringify(view),
    /https:\/\/humagen\.test\/conversations\/shared-thread\|Open Conversation/,
  );
});
