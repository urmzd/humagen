import assert from "node:assert/strict";
import { test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { type decodeReplyModal, REPLY_MODAL } from "@humagen/render-slack";
import {
  click,
  type FakeSlack,
  interactive,
  mention,
  OTHER_USER,
  QUESTIONS,
  questionMail,
  SUBJECT,
  settle,
  stopPressed,
  TEAM,
  USER,
  type World,
  world,
} from "./harness.ts";
import { words } from "./host.ts";
import { HumagenError } from "./humagen.ts";
import { promptIn, recordedOf, settleCard, settledElsewhere } from "./questions.tsx";
import { validateBlocks } from "./validate.ts";

const TS = "1700000000.000100";
const CLIENT = "app";
const AT = "2026-10-09T00:12:00Z";

/** Every Block Kit payload these tests hand to Slack, for the validity check at the end. */
const produced: { from: string; blocks?: unknown[]; view?: object }[] = [];
function collect(name: string, slack: FakeSlack) {
  for (const c of slack.calls) {
    if (Array.isArray(c.args.blocks) && c.args.blocks.length > 0) {
      produced.push({ from: `${name}: ${c.method}`, blocks: c.args.blocks });
    }
    if (c.args.view) produced.push({ from: `${name}: ${c.method}`, view: c.args.view as object });
  }
}

/** A world whose app is a client that maps USER to ana and OTHER_USER to olu. */
function mapped(): World {
  const w = world({ clientId: CLIENT });
  w.humagen.clients.set(CLIENT, {
    id: CLIENT,
    surface: "slack",
    enabled: true,
    default_agent: "lead",
    identity: { team: TEAM },
    slack: { machine: SUBJECT, people: { [USER]: "ana", [OTHER_USER]: "olu" } },
  });
  return w;
}

async function deliver(w: World, envelope: Parameters<World["app"]["handle"]>[0]) {
  await w.app.handle(envelope);
  await settle();
}

/** Start a run that parks on a question, and return the message that shows it. */
async function parkOn(w: World, kind: keyof typeof QUESTIONS) {
  await deliver(w, mention("deploy the web service", TS));
  const run = w.humagen.last.id;
  for (const [type, body] of [
    ["run.accepted.v1", undefined],
    ["run.started.v1", { attempt: 1 }],
    ["run.delta.text.v1", { text: "Checking first. " }],
    ["run.parked.v1", { reason: "waiting on a person", question: QUESTIONS[kind] }],
  ] as const) {
    w.humagen.emit(run, type, body);
    await settle();
  }
  const shown = w.slack.of("chat.stopStream").at(-1);
  return { run, blocks: (shown?.blocks ?? []) as unknown[], ts: String(shown?.ts ?? "") };
}

const end = async (w: World) => {
  await deliver(w, stopPressed(TS));
  await w.app.idle();
};

type View = Parameters<typeof decodeReplyModal>[0] & { blocks?: unknown[] };
/** The modal the app opened, sent back as Slack does when it is submitted with these words. */
const submit = (view: View, text: string, user: string) =>
  interactive({
    type: "view_submission",
    user: { id: user, team_id: TEAM },
    view: { ...view, state: { values: { reply: { value: { value: text } } } } },
  });

const texts = (blocks: unknown) => JSON.stringify(blocks);
const buttons = (blocks: unknown[]) =>
  (blocks as { type: string; elements?: { text?: { text: string } }[] }[])
    .filter((b) => b.type === "actions")
    .flatMap((b) => (b.elements ?? []).map((e) => e.text?.text));

test("Something Else opens a modal with the question above one multiline field, and its words answer as the person who typed them", async () => {
  const w = mapped();
  const { run, blocks, ts } = await parkOn(w, "reply");
  await deliver(
    w,
    click(blocks, defaultCopy.somethingElse, { ts, threadTs: TS, user: OTHER_USER }),
  );

  const opened = w.slack.of("views.open");
  assert.equal(opened.length, 1, "one modal, on the click's trigger");
  assert.match(String(opened[0]?.trigger_id), /^trigger-/);
  const view = opened[0]?.view as View & { private_metadata: string };
  assert.equal(view.callback_id, REPLY_MODAL);
  // The modal names what it answers by id, never by content.
  assert.deepEqual(JSON.parse(view.private_metadata), { run, question: "q-reply" });
  assert.match(texts(view.blocks), /What is the ticket number\?/);
  assert.match(texts(view.blocks), /"multiline":true/);
  assert.equal(w.humagen.answered.length, 0, "opening the form answers nothing");

  await deliver(w, submit(view, "  OPS-4821, the one Dana opened  ", OTHER_USER));
  assert.deepEqual(w.humagen.answered, [
    {
      answer: {
        run,
        question: "q-reply",
        body: { action: "reply", text: "OPS-4821, the one Dana opened" },
      },
      as: { client: CLIENT, user: OTHER_USER },
    },
  ]);
  await settle();
  // The card settles in place from the run's own events, as a button's answer does.
  const card = w.slack.of("chat.update").find((u) => u.ts === ts);
  assert.ok(card, "the question's message is edited, not posted again");
  assert.match(texts(card.blocks), /olu answered \\"OPS-4821, the one Dana opened\\"/);
  assert.deepEqual(buttons(card.blocks as unknown[]), [], "no button is left to press");
  assert.equal(w.slack.of("chat.postEphemeral").length, 0);
  collect("reply to a question", w.slack);
  w.humagen.emit(run, "run.completed.v1", { text: "Checking first. Done." });
  await settle();
  await w.app.idle();
});

test("someone the client does not map is shown no form, and a form they could not have opened answers nothing", async () => {
  const w = mapped();
  const { run, blocks, ts } = await parkOn(w, "reply");
  await deliver(
    w,
    click(blocks, defaultCopy.somethingElse, { ts, threadTs: TS, user: "U0NOBODY1" }),
  );
  assert.equal(w.slack.of("views.open").length, 0, "no form that cannot send");
  assert.equal(w.slack.of("chat.postEphemeral").at(-1)?.text, defaultCopy.noPermission);

  // A mapped person's form, sent back under another id: the words are not theirs to send.
  await deliver(
    w,
    click(blocks, defaultCopy.somethingElse, { ts, threadTs: TS, user: OTHER_USER }),
  );
  const view = w.slack.of("views.open")[0]?.view as View;
  await deliver(w, submit(view, "approve everything", "U0NOBODY1"));
  assert.equal(w.humagen.answered.length, 0, "humagen is never asked");
  assert.ok(w.logs.some((l) => l.includes("does not map them")));

  // And each mapped person answers as themselves, whoever opened the form.
  await deliver(w, submit(view, "OPS-1", USER));
  assert.deepEqual(w.humagen.answered.at(-1), {
    answer: { run, question: "q-reply", body: { action: "reply", text: "OPS-1" } },
    as: { client: CLIENT, user: USER },
  });
  w.humagen.emit(run, "run.completed.v1", { text: "Done." });
  await settle();
  await w.app.idle();
});

test("words humagen refuses are said beside the card, in humagen's words, and the card keeps its button", async () => {
  const w = mapped();
  const { blocks, ts } = await parkOn(w, "reply");
  await deliver(w, click(blocks, defaultCopy.somethingElse, { ts, threadTs: TS, user: USER }));
  const view = w.slack.of("views.open")[0]?.view as View;
  w.humagen.refuseAnswer = new HumagenError(403, "platform: forbidden: ana may not answer this");
  await deliver(w, submit(view, "OPS-1", USER));
  const said = w.slack.of("chat.postEphemeral").at(-1);
  assert.equal(said?.text, defaultCopy.noPermission);
  assert.equal(said?.user, USER);
  assert.equal(said?.thread_ts, TS, "in the thread the card is in");
  assert.ok(!w.slack.of("chat.update").some((u) => u.ts === ts));
  await end(w);
});

test("an empty form answers nothing", async () => {
  const w = mapped();
  const { blocks, ts } = await parkOn(w, "reply");
  await deliver(
    w,
    click(blocks, defaultCopy.somethingElse, { ts, threadTs: TS, user: OTHER_USER }),
  );
  const view = w.slack.of("views.open")[0]?.view as View;
  await deliver(w, submit(view, "   ", OTHER_USER));
  assert.equal(w.humagen.answered.length, 0);
  await end(w);
});

test("an option pressed after someone answered elsewhere changes nothing: the person is told and the card shows what was recorded", async () => {
  const w = mapped();
  const { run, blocks, ts } = await parkOn(w, "choice");
  // Ana answered in the console. Humagen answers a later press with the record.
  w.humagen.recorded = questionMail(run, "choice", {
    action: "choose",
    by: "ana",
    by_name: "Ana Reyes",
    at: AT,
    note: "us-east",
  } as Parameters<typeof questionMail>[2]);
  await deliver(w, click(blocks, "eu-west", { ts, threadTs: TS, user: OTHER_USER }));
  assert.equal(w.humagen.answered.length, 1, "humagen decides, and it said no change");
  assert.equal(w.slack.of("chat.postEphemeral").at(-1)?.text, words.alreadySettled);
  const card = w.slack.of("chat.update").find((u) => u.ts === ts);
  assert.ok(card, "the card pressed is edited in place");
  assert.match(texts(card.blocks), /Ana Reyes chose us-east/);
  assert.ok(!texts(card.blocks).includes("olu"), "the late press is not shown as the answer");
  assert.deepEqual(buttons(card.blocks as unknown[]), []);
  // What the message said besides the card is still there.
  assert.equal((card.blocks as unknown[]).length, blocks.length);
  assert.deepEqual((card.blocks as unknown[]).at(-1), blocks.at(-1));
  collect("answered elsewhere", w.slack);
  await end(w);
});

test("a decision pressed after the run was stopped, or after nobody answered in time, shows why it no longer waits", async () => {
  for (const [outcome, said] of [
    [{ action: "stopped", by: "system", at: AT }, defaultCopy.cancelled],
    [
      {
        action: "expired",
        by: "system",
        at: AT,
        note: "Nobody answered in time, so it was denied",
      },
      defaultCopy.timedOut,
    ],
  ] as const) {
    const w = mapped();
    const { run, blocks, ts } = await parkOn(w, "approval");
    w.humagen.recorded = questionMail(run, "approval", outcome);
    await deliver(w, click(blocks, defaultCopy.approve, { ts, threadTs: TS, user: OTHER_USER }));
    assert.equal(w.slack.of("chat.postEphemeral").at(-1)?.text, words.alreadySettled);
    const card = w.slack.of("chat.update").find((u) => u.ts === ts);
    assert.ok(card);
    assert.ok(texts(card.blocks).includes(said), texts(card.blocks));
    assert.ok(!texts(card.blocks).includes("Approved by"), "nothing was approved");
    assert.deepEqual(buttons(card.blocks as unknown[]), []);
    collect(`no longer waiting (${outcome.action})`, w.slack);
    await end(w);
  }
});

test("words sent after the question was answered elsewhere are not shown as the answer", async () => {
  const w = mapped();
  const { run, blocks, ts } = await parkOn(w, "reply");
  await deliver(
    w,
    click(blocks, defaultCopy.somethingElse, { ts, threadTs: TS, user: OTHER_USER }),
  );
  const view = w.slack.of("views.open")[0]?.view as View;
  w.humagen.recorded = questionMail(run, "reply", {
    action: "reply",
    by: "ana",
    by_name: "Ana Reyes",
    at: AT,
    note: "OPS-7",
  } as Parameters<typeof questionMail>[2]);
  await deliver(w, submit(view, "OPS-4821", OTHER_USER));
  assert.equal(w.slack.of("chat.postEphemeral").at(-1)?.text, words.alreadySettled);
  const card = w.slack.of("chat.update").find((u) => u.ts === ts);
  assert.ok(card);
  assert.match(texts(card.blocks), /Ana Reyes answered \\"OPS-7\\"/);
  assert.ok(!texts(card.blocks).includes("OPS-4821"));
  await end(w);
});

test("a person's own answer, recorded as sent, is not called settled elsewhere", async () => {
  const w = mapped();
  const { run, blocks, ts } = await parkOn(w, "choice");
  w.humagen.recorded = questionMail(run, "choice", {
    action: "choose",
    by: "olu",
    at: AT,
    note: "eu-west",
  });
  await deliver(w, click(blocks, "eu-west", { ts, threadTs: TS, user: OTHER_USER }));
  assert.equal(w.slack.of("chat.postEphemeral").length, 0);
  await end(w);
});

test("Ask a question on a call says it cannot be asked yet, and decides nothing", async () => {
  const w = mapped();
  const { blocks, ts } = await parkOn(w, "approval");
  await deliver(w, click(blocks, defaultCopy.askApproval, { ts, threadTs: TS, user: OTHER_USER }));
  assert.equal(w.humagen.answered.length, 0);
  assert.equal(w.slack.of("views.open").length, 0);
  assert.equal(w.slack.of("chat.postEphemeral").at(-1)?.text, words.askNotYet);
  await end(w);
});

// The pieces, on their own.

test("the record is read from the message an answer returns, and only once it is settled", () => {
  assert.equal(recordedOf({}, "q-choose"), null);
  assert.equal(recordedOf(questionMail("r1", "choice"), "q-choose"), null, "still open");
  const mail = questionMail("r1", "choice", {
    action: "choose",
    by: "ana",
    at: AT,
    note: "us-east",
  });
  assert.equal(recordedOf(mail, "q-other"), null);
  const r = recordedOf(mail, "q-choose");
  assert.equal(r?.by, "ana");
  assert.deepEqual(r?.settled, { action: "choose", by: "ana", at: AT, note: "us-east" });
  assert.deepEqual(r?.question.options, ["us-east", "eu-west"]);
});

test("settled elsewhere means someone else, nobody, or a different answer", () => {
  const rec = (action: string, by: string, note?: string) => {
    const r = recordedOf(
      questionMail("r1", "choice", { action, by, at: AT, ...(note ? { note } : {}) }),
      "q-choose",
    );
    assert.ok(r);
    return r;
  };
  const sent = { action: "choose", choice: "eu-west" } as const;
  assert.equal(settledElsewhere(rec("choose", "olu", "eu-west"), sent, "olu"), false);
  assert.equal(settledElsewhere(rec("choose", "ana", "eu-west"), sent, "olu"), true);
  assert.equal(settledElsewhere(rec("choose", "olu", "us-east"), sent, "olu"), true);
  assert.equal(settledElsewhere(rec("expired", "system"), sent, "olu"), true);
  assert.equal(settledElsewhere(rec("stopped", "system"), sent, "olu"), true);
});

test("a card is replaced whole where it was drawn, and by its record alone where it was not", () => {
  const before = { type: "section", text: { type: "mrkdwn", text: "Checking first." } };
  const after = { type: "context", elements: [{ type: "mrkdwn", text: "AI can make mistakes." }] };
  const question = { type: "section", text: { type: "mrkdwn", text: "*Which region?*" } };
  const actions = { type: "actions", block_id: "clarification:q-choose", elements: [] };
  const recorded = recordedOf(
    questionMail("r1", "choice", { action: "choose", by: "ana", at: AT, note: "us-east" }),
    "q-choose",
  );
  assert.ok(recorded);
  const whole = settleCard([before, question, actions, after], "r1", recorded);
  assert.equal(whole?.length, 4);
  assert.deepEqual(whole?.[0], before);
  assert.deepEqual(whole?.[3], after);
  assert.match(texts(whole?.[1]), /"Which region\?"/, "the question is no longer in bold");
  assert.match(texts(whole?.[2]), /ana chose us-east/);
  // Buttons with no question above them: only the buttons give way.
  const alone = settleCard([before, { type: "divider" }, actions], "r1", recorded);
  assert.equal(alone?.length, 3);
  assert.deepEqual(alone?.slice(0, 2), [before, { type: "divider" }]);
  assert.match(texts(alone?.[2]), /ana chose us-east/);
  // Another question's card is not this one's.
  assert.equal(
    settleCard([question, { ...actions, block_id: "clarification:q-x" }], "r1", recorded),
    null,
  );
});

test("the question above a card's buttons is read as plain words", () => {
  const blocks = [
    { type: "section", text: { type: "mrkdwn", text: "Checking first." } },
    {
      type: "section",
      text: { type: "mrkdwn", text: "*Ship &lt;web&gt; &amp; api?*\n• **Now**: tonight" },
    },
    { type: "actions", block_id: "clarification:q1", elements: [] },
  ];
  assert.equal(promptIn(blocks, "q1"), "Ship <web> & api?");
  assert.equal(promptIn(blocks, "q2"), "");
  assert.equal(promptIn([], "q1"), "");
});

test(
  "every Block Kit payload these tests produced is one Slack accepts",
  { skip: !process.env.SLACK_VALIDATE },
  async () => {
    assert.ok(produced.length > 3);
    const failed: string[] = [];
    for (const p of produced) {
      const res = await validateBlocks(p.blocks ?? [], p.view);
      if (!res.ok) failed.push(`${p.from}: ${JSON.stringify(res.errors ?? res.error)}`);
    }
    assert.deepEqual(failed, []);
  },
);
