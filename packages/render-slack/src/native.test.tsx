import {
  ApprovalCard,
  defaultCopy,
  MessageView,
  SystemAlertPart,
  TurnView,
} from "@humagen/components";
import { ActionId, type Approval, type InboxMessage } from "@humagen/core";
import * as fx from "@humagen/core/fixtures";
import { Text } from "@humagen/primitives";
import { describe, expect, it } from "vitest";
import {
  type AlertInput,
  alertCard,
  alertLevel,
  approvalCard,
  approvalCarousel,
  type Block,
  decodeSlackAction,
  fallbackOf,
  LIMITS,
  NATIVE_LIMITS,
  type NativeBlocks,
  nativeBlocks,
  questionContainer,
  renderToSlack,
} from "./index";

/** An approval as the platform raises one: no Edit or Always Allow, which it does not offer yet. */
const rollback: Approval = {
  ...fx.pendingApproval,
  editable: false,
  actingAs: undefined,
  expiresAt: undefined,
};
const post: Approval = { ...fx.secondApproval, rememberable: false };
const at = "2026-10-08T14:52:00Z";
const ctx = { turnId: "run-1" };

/** The knowledge alert the platform sends for stale documents. */
const knowledge: AlertInput = {
  id: "al-1",
  kind: "knowledge",
  severity: "warning",
  priority: "normal",
  state: "open",
  agent: "humagen-builder",
  title: "2 documents you own in HR policies are stale",
  body: "Update or replace them, or hand them to someone who owns them now: Leave policy 2023 (old, unowned), Travel policy (superseded).",
  action: "/knowledge/hr-policies",
};
const resolve = { id: ActionId.acknowledge, payload: { turnId: "m-alert", target: "p1" } };

type Button = {
  type: string;
  action_id: string;
  value?: string;
  url?: string;
  text: { text: string };
};

/** Every button in the blocks, wherever it sits: an actions row, a card, a carousel's cards, a container. */
function buttons(blocks: Block[]): Button[] {
  return blocks.flatMap((b): Button[] => {
    if (b.type === "actions") return (b.elements as Button[]).filter((e) => e.type === "button");
    if (b.type === "card") return (b.actions as Button[] | undefined) ?? [];
    if (b.type === "carousel") return buttons(b.elements as Block[]);
    if (b.type === "container") return buttons(b.child_blocks as Block[]);
    if (b.type === "section" && (b.accessory as Button | undefined)?.type === "button")
      return [b.accessory as Button];
    return [];
  });
}

/** What each button sends, by label, so the native blocks can be held to the sections they replace. */
const sends = (blocks: Block[]) =>
  Object.fromEntries(
    buttons(blocks)
      .filter((b) => b.value)
      .map((b) => [b.text.text, decodeSlackAction(b)]),
  );
/** What the buttons send, whatever they say: Slack's card may word a button shorter. */
const actions = (blocks: Block[]) =>
  Object.values(sends(blocks))
    .map((a) => JSON.stringify(a))
    .sort();

const every = (out: NativeBlocks) => {
  expect(out.text.trim().length).toBeGreaterThan(0);
  expect(out.fallback.length).toBeGreaterThan(0);
  for (const b of out.blocks) expect(b).not.toHaveProperty("fallback");
};

describe("an approval as a card", () => {
  it("has a one-line title, the whole call and what it does as its body, then Approve, Ask and Decline", () => {
    const out = approvalCard(rollback, ctx);
    every(out);
    expect(out.native).toBe(true);
    expect(out.blocks).toMatchSnapshot();
    const [card] = out.blocks as [Block];
    expect(card.type).toBe("card");
    // Slack shows a title on one line: the call is never cut there.
    expect((card.title as { text: string }).text).toBe(
      `*${defaultCopy.slackBlocks.approvalTitle.pending}*`,
    );
    expect((card.body as { text: string }).text).toMatch(
      /^\*Roll back payments-api in production to deploy #4811\?\*\nService: payments-api/,
    );
    const labels = buttons(out.blocks).map((b) => [b.text.text, (b as { style?: string }).style]);
    // As the shared drawing has them: the way forward first, Decline unstyled.
    expect(labels).toEqual([
      ["Roll Back", "danger"],
      [defaultCopy.slackBlocks.ask, undefined],
      [defaultCopy.declineAction, undefined],
    ]);
    // Short enough that no label is cut; a screen reader still says Ask a Question.
    expect(labels.reduce((n, [l]) => n + String(l).length, 0)).toBeLessThanOrEqual(
      NATIVE_LIMITS.cardButtonChars,
    );
    expect(buttons(out.blocks)[1]).toMatchObject({
      accessibility_label: `${defaultCopy.askApproval}: ${rollback.summary}`.slice(0, 75),
    });
    expect(out.text).toBe(defaultCopy.approvalNeeded(rollback.summary));
  });

  it("names a sub-agent that raised it as its subtitle", () => {
    const out = approvalCard({ ...rollback, origin: "Raised by Deploy agent" }, ctx);
    expect((out.blocks[0]?.subtitle as { text: string }).text).toBe("Raised by Deploy agent");
  });

  it("sends what the section layout sends, so the app's handlers take the same clicks", () => {
    const out = approvalCard(rollback, ctx);
    expect(actions(out.blocks)).toEqual(actions(out.fallback));
    expect(sends(out.blocks)[defaultCopy.declineAction]).toEqual({
      id: ActionId.deny,
      payload: { turnId: "run-1", target: "a1" },
    });
    expect((out.blocks[0] as Block).block_id).toBe("approval:a1:r1");
  });

  it("keeps the destructive confirm step on Approve, and a plain one primary", () => {
    const [approve] = buttons(approvalCard(rollback, ctx).blocks) as (Button & {
      confirm?: { style?: string };
    })[];
    expect(approve?.confirm?.style).toBe("danger");
    const [plain] = buttons(approvalCard({ ...rollback, destructive: false }, ctx).blocks);
    expect(plain).toMatchObject({ style: "primary" });
    expect(plain).not.toHaveProperty("confirm");
  });

  it("says what the approval agent decided and why in its body, its fields first", () => {
    const out = approvalCard(
      {
        ...rollback,
        preview: [{ label: "Service", value: "payments-api" }],
        judgement: {
          decision: "ask",
          reason: "Production needs a person",
          fields: [{ label: "Risk", value: "High" }],
        },
      },
      ctx,
    );
    expect(out.native).toBe(true);
    expect((out.blocks[0]?.body as { text: string }).text).toBe(
      `*${rollback.summary}*\nService: payments-api\n*Risk:* High | *Approval agent:* Asked a person: "Production needs a person"`,
    );
  });

  it("keeps the approval agent's reason once on the record", () => {
    const reason = "The person asked for this deploy.";
    const out = approvalCard(
      {
        ...rollback,
        preview: [],
        judgement: { decision: "allow", reason },
        decision: {
          kind: "approved",
          by: "Olu Ade",
          at,
          reason: `Allowed by the approval agent: ${reason}`,
        },
      },
      ctx,
    );
    const said = JSON.stringify(out.blocks);
    expect(said).toContain("Approved by Olu Ade");
    expect(said.split(reason)).toHaveLength(2);
    expect(buttons(out.blocks)).toEqual([]);
  });

  it("puts choices past the card's three in a row under it, with the thread's Auto mode in a menu", () => {
    const out = approvalCard(fx.pendingApproval, { ...ctx, offerAgent: true });
    expect(out.native).toBe(true);
    expect(out.blocks.map((b) => b.type)).toEqual(["card", "actions"]);
    expect(buttons([out.blocks[1] as Block]).map((b) => b.text.text)).toEqual([defaultCopy.edit]);
    expect(actions(out.blocks)).toEqual(actions(out.fallback));
    const menu = (out.blocks[1]?.elements as Block[]).find((e) => e.type === "overflow");
    const [auto] = (menu?.options ?? []) as { text: { text: string }; value: string }[];
    expect(auto?.text.text).toBe(defaultCopy.approvals.letAgentDecide);
    expect(
      decodeSlackAction({ action_id: "aui.menu#0", selected_option: { value: auto?.value ?? "" } }),
    ).toEqual({ id: ActionId.approvalMode, payload: { turnId: "run-1", target: "auto" } });
  });

  it("offers a run held at a limit a grant and a stop, and no Auto", () => {
    const held: Approval = {
      type: "approval",
      id: "q-b",
      summary: "lead is held at the limit of spend a day.",
      tool: "",
      budget: {
        scope: "the workspace",
        limit: "spend",
        used: "$9.00",
        max: "$5.00",
        grant: "$10.00",
      },
      decision: { kind: "pending" },
    };
    const out = approvalCard(held, { ...ctx, offerAgent: true });
    expect(out.native).toBe(true);
    expect(out.blocks.map((b) => b.type)).toEqual(["card"]);
    expect(buttons(out.blocks).map((b) => b.text.text)).toEqual(["Grant $10.00", "Stop Run"]);
  });

  it.each([
    ["approved", fx.approvedApproval, defaultCopy.approvedBy("Dana", "")],
    ["denied", fx.deniedApproval, defaultCopy.rejectedBy("Dana", "")],
  ])(
    "updates in place once %s: no buttons, and the record as its subtext",
    (_k, approval, record) => {
      const out = approvalCard(approval, ctx);
      every(out);
      expect(out.blocks).toHaveLength(1);
      expect(out.blocks[0]?.block_id).toBe("approval:a1:r1");
      expect(out.blocks[0]).not.toHaveProperty("actions");
      expect((out.blocks[0]?.subtext as { text: string }).text).toContain(record.split(" | ")[0]);
    },
  );

  it("shows a reader who may not decide only Ask, and says why", () => {
    const out = approvalCard({ ...rollback, initiator: "dana" }, { ...ctx, viewer: "ana" });
    expect(buttons(out.blocks).map((b) => b.text.text)).toEqual([defaultCopy.slackBlocks.ask]);
    expect((out.blocks[0]?.subtext as { text: string }).text).toContain(defaultCopy.notYours);
  });

  it("posts the section layout rather than cut a word a card can't hold", () => {
    const long = { ...rollback, summary: `${"Roll back every service ".repeat(8)}now?` };
    const out = approvalCard(long, ctx);
    expect(out.native).toBe(false);
    expect(out.blocks).toEqual(out.fallback);
    expect(out.warnings[0]).toMatch(/card body is \d+ chars; a card holds 200/);
    expect(JSON.stringify(out.blocks)).toContain("Roll back every service Roll back");
  });

  it.each(["modal", "home"] as const)("is the section layout in a %s", (placement) => {
    const out = approvalCard(rollback, { ...ctx, placement });
    expect(out.native).toBe(false);
    expect(out.warnings).toEqual([]);
  });
});

describe("several approvals as a carousel", () => {
  it("is one line, a card for each, then only a menu with Auto: nothing that acts on all at once", () => {
    const out = approvalCarousel([rollback, post], { ...ctx, offerAgent: true });
    every(out);
    expect(out.native).toBe(true);
    expect(out.blocks).toMatchSnapshot();
    expect(out.blocks.map((b) => b.type)).toEqual(["section", "carousel", "actions"]);
    expect((out.blocks[0]?.text as { text: string }).text).toBe(`*${defaultCopy.batchTitle(2)}*`);
    const cards = out.blocks[1]?.elements as Block[];
    expect(cards.map((c) => c.block_id)).toEqual(["approval:a1:r1", "approval:a2:r0"]);
    // action_ids stay unique across the carousel.
    const ids = buttons(out.blocks).map((b) => b.action_id);
    expect(new Set(ids).size).toBe(ids.length);
    // Each card decides its own call, as one card does.
    expect(sends([cards[1] as Block])[defaultCopy.declineAction]).toEqual({
      id: ActionId.deny,
      payload: { turnId: "run-1", target: "a2" },
    });
    const row = out.blocks[2] as Block;
    // Approve All and Cancel All have no handler in the app: they are not offered.
    expect(buttons([row])).toEqual([]);
    expect(JSON.stringify(out.blocks)).not.toMatch(/aui\.approve\.all|aui\.deny\.all/);
    const menu = (row.elements as Block[]).find((e) => e.type === "overflow");
    const options = (menu?.options ?? []) as { text: { text: string }; value: string }[];
    expect(
      options.map((o) => [
        o.text.text,
        decodeSlackAction({ action_id: "aui.menu#0", selected_option: { value: o.value } }),
      ]),
    ).toEqual([
      [
        defaultCopy.approvals.letAgentDecide,
        { id: ActionId.approvalMode, payload: { turnId: "run-1", target: "auto" } },
      ],
    ]);
    expect(out.text).toBe(defaultCopy.approvalNeeded(defaultCopy.batchTitle(2)));
    // Without Auto there is no row at all.
    expect(approvalCarousel([rollback, post], ctx).blocks.map((b) => b.type)).toEqual([
      "section",
      "carousel",
    ]);
  });

  it("shows a card its reader may not decide with only Ask, and says why", () => {
    const out = approvalCarousel([{ ...rollback, initiator: "dana" }, post], {
      ...ctx,
      viewer: "ana",
    });
    const [mine, theirs] = out.blocks[1]?.elements as Block[];
    expect(buttons([mine as Block]).map((b) => b.text.text)).toEqual([defaultCopy.slackBlocks.ask]);
    expect((mine?.subtext as { text: string }).text).toContain(defaultCopy.notYours);
    expect(buttons([theirs as Block])).toHaveLength(3);
  });

  it.each([
    ["more than ten", Array.from({ length: 11 }, (_, i) => ({ ...post, id: `a${i}` })), {}],
    ["a card with more choices than it holds", [fx.pendingApproval, post], {}],
    ["a modal", [rollback, post], { placement: "modal" as const }],
  ])("falls back to the checkbox layout for %s", (_k, approvals, extra) => {
    const out = approvalCarousel(approvals, { ...ctx, ...extra });
    expect(out.native).toBe(false);
    expect(out.blocks).toEqual(out.fallback);
    expect(out.blocks.map((b) => b.type)).toContain("actions");
    expect(JSON.stringify(out.blocks)).toContain("checkboxes");
    expect(out.text.length).toBeGreaterThan(0);
  });
});

describe("a turn as production posts it", () => {
  it("draws what waits on a person as Slack's own blocks, and keeps the sections as its fallback", () => {
    const out = renderToSlack(<TurnView turn={fx.waitingTurn} />);
    const types = out.blocks.map((b) => b.type);
    expect(types).toContain("card");
    expect(out.fallback).toBeDefined();
    expect(out.fallback?.blocks.map((b) => b.type)).not.toContain("card");
    // The words a notification reads are the same either way.
    expect(out.text).toBe(out.fallback?.text);
    expect(fallbackOf(out.blocks)).toEqual(out.fallback?.blocks);
    expect(fallbackOf(out.fallback?.blocks ?? [])).toBeUndefined();
  });

  it("draws several waiting approvals as a carousel", () => {
    const turn = {
      ...fx.waitingTurn,
      interrupts: [rollback, post],
    };
    const out = renderToSlack(<TurnView turn={turn} capabilities={{ approvalAgent: true }} />);
    expect(out.blocks.map((b) => b.type)).toContain("carousel");
    expect(JSON.stringify(out.blocks)).toContain(defaultCopy.approvals.letAgentDecide);
  });

  it("draws an open question as a container", () => {
    const out = renderToSlack(
      <TurnView turn={{ ...fx.waitingTurn, interrupts: [fx.pendingClarification] }} />,
    );
    expect(out.blocks.map((b) => b.type)).toContain("container");
  });

  it("splits into the same messages either way, so every page has its sections to fall back to", () => {
    // 48 blocks, then an approval: its card (1 block) would fit in the first message, its sections (3) do not.
    const out = renderToSlack(
      <>
        {Array.from({ length: 48 }, (_, i) => `Line ${i}`).map((line) => (
          <Text key={line} text={line} />
        ))}
        <ApprovalCard turnId="run-1" approval={rollback} />
      </>,
    );
    expect(out.fallback?.pages.length).toBe(out.pages.length);
    expect(out.pages.length).toBe(2);
    for (const page of out.pages) expect(page.length).toBeLessThanOrEqual(LIMITS.blocks.message);
    for (const page of out.fallback?.pages ?? [])
      expect(page.length).toBeLessThanOrEqual(LIMITS.blocks.message);
    // The page with the card falls back to the page with its sections, buttons and all.
    const withCard = out.pages.find((p) => p.some((b) => b.type === "card")) as Block[];
    const sections = fallbackOf(withCard);
    expect(sections).toEqual(out.fallback?.pages[out.pages.indexOf(withCard)]);
    expect(actions(sections ?? [])).toEqual(actions(withCard));
    // The page before it has nothing native to take back.
    expect(fallbackOf(out.pages[0] as Block[])).toBeUndefined();
  });

  it("has no fallback when nothing in it is native", () => {
    const out = renderToSlack(<TurnView turn={fx.doneTurn} />);
    expect(out.fallback).toBeUndefined();
    expect(fallbackOf(out.blocks)).toBeUndefined();
  });

  it("can be drawn without native blocks at all", () => {
    const out = renderToSlack(<TurnView turn={fx.waitingTurn} />, { native: false });
    expect(out.blocks.map((b) => b.type)).not.toContain("card");
    expect(out.fallback).toBeUndefined();
  });
});

describe("an inbox alert as a card", () => {
  const message = fx.mailMessages.find((m) => m.id === "m-alert") as InboxMessage;

  it("has the service as its title, its status as its subtitle, what is wrong as its body, then its buttons", () => {
    const out = nativeBlocks(<MessageView message={message} />);
    expect(out.native).toBe(true);
    const card = out.blocks.find((b) => b.type === "card") as Block;
    expect(card.block_id).toBe("alert:a-5xx");
    expect((card.title as { text: string }).text).toBe("*payments-api down*");
    expect((card.subtitle as { text: string }).text).toBe(`:x: ${defaultCopy.serviceStatus.down}`);
    expect((card.body as { text: string }).text).toMatch(
      /^\*5xx rate is 4\.2%\*\n.*Charges, Refunds/,
    );
    // What fits uncut is on the card; Open Rule is in the row under it.
    expect(buttons([card]).map((b) => b.text.text)).toEqual([
      defaultCopy.acknowledge,
      defaultCopy.snooze,
    ]);
    expect(buttons(out.blocks).map((b) => b.text.text)).toContain(defaultCopy.openRule);
    expect(actions(out.blocks)).toEqual(actions(out.fallback));
  });

  it("keeps its record in place once resolved, with nothing to press", () => {
    const part = message.parts[0] as InboxMessage["parts"][number];
    const done = {
      ...message,
      parts: [{ ...part, resolution: { outcome: "acknowledged", by: "Dana", at } }],
    } as InboxMessage;
    const out = nativeBlocks(<MessageView message={done} />);
    const card = out.blocks.find((b) => b.type === "card") as Block;
    expect(card).not.toHaveProperty("actions");
    expect((card.subtext as { text: string }).text).toContain("Dana");
  });

  it.each([["back to normal", { ...fx.systemAlerts.resolved }]])(
    "is the shared one-line record once its service is %s",
    (_k, alert) => {
      const out = nativeBlocks(
        <SystemAlertPart alert={alert} blockId="alert:p1" record={undefined} />,
      );
      expect(out.native).toBe(false);
      expect(out.blocks.map((b) => b.type)).toEqual(["context"]);
    },
  );
});

describe("an alert as a card", () => {
  it("has its title, its priority in words, its body, then Open and Resolve", () => {
    const out = alertCard(knowledge, { consoleUrl: "https://platform.example.com", resolve });
    every(out);
    expect(out.native).toBe(true);
    expect(out.blocks).toMatchSnapshot();
    const [card] = out.blocks as [Block];
    expect(card.type).toBe("card");
    expect((card.subtitle as { text: string }).text).toBe(
      ":warning: Normal priority · humagen-builder",
    );
    const [open, done] = buttons(out.blocks);
    expect(open?.url).toBe("https://platform.example.com/knowledge/hr-policies");
    expect(decodeSlackAction(done as Button)).toEqual(resolve);
    expect(out.text).toBe(`Normal priority: ${knowledge.title}\n${knowledge.body}`);
  });

  it("offers Open Session as a link or an action", () => {
    const link = alertCard(
      { ...knowledge, action: undefined },
      { session: "https://platform.example.com/s/1", resolve },
    );
    expect(buttons(link.blocks).map((b) => [b.text.text, b.url])).toEqual([
      [defaultCopy.slackBlocks.openSession, "https://platform.example.com/s/1"],
      [defaultCopy.slackBlocks.resolve, undefined],
    ]);
  });

  it("opens a console path by action when the host gives no console origin", () => {
    const [open] = buttons(alertCard(knowledge).blocks);
    expect(decodeSlackAction(open as Button)).toEqual({
      id: ActionId.open,
      payload: { turnId: "al-1", target: "/knowledge/hr-policies" },
    });
  });

  it.each([
    [{ priority: "urgent" }, "error"],
    [{ priority: "normal", severity: "warning" }, "warning"],
    [{ priority: "normal", severity: "critical" }, "warning"],
    [{ priority: "normal", severity: "info" }, "info"],
    [{ priority: "urgent", state: "resolved" }, "success"],
  ])("reads %o as the %s level", (alert, level) => {
    expect(alertLevel(alert)).toBe(level);
  });

  it("drops Resolve once resolved, and says who resolved it", () => {
    const out = alertCard(
      { ...knowledge, state: "resolved" },
      { resolve, resolvedBy: { by: "Dana", at } },
    );
    expect(buttons(out.blocks).map((b) => b.text.text)).toEqual([defaultCopy.slackBlocks.open]);
    expect((out.blocks[0]?.subtitle as { text: string }).text).toMatch(
      /^:white_check_mark: Resolved/,
    );
    expect((out.blocks[0]?.subtext as { text: string }).text).toMatch(
      /^Resolved by Dana \| <!date/,
    );
  });

  it("is an alert block in a modal, the only place Slack draws one", () => {
    const short = { ...knowledge, body: "Update or replace them." };
    const out = alertCard(short, { placement: "modal", resolve });
    expect(out.blocks.map((b) => b.type)).toEqual(["alert", "actions"]);
    expect(out.blocks[0]).toMatchObject({ level: "warning" });
    // A body longer than an alert block holds: sections, not a cut.
    const long = alertCard(
      { ...knowledge, body: `${knowledge.body} ${knowledge.body}` },
      { placement: "modal", resolve },
    );
    expect(long.native).toBe(false);
    expect(long.blocks.map((b) => b.type)).toEqual(["section", "context", "actions"]);
  });

  it("never draws an alert block in a message or on Home", () => {
    for (const placement of ["message", "home"] as const) {
      expect(alertCard(knowledge, { placement }).blocks.map((b) => b.type)).not.toContain("alert");
    }
  });
});

describe("a question as a container", () => {
  const question = { ...fx.pendingClarification, allowUnsure: true };

  it("has a one-line title, the whole question first, the options as buttons and an input for its own answer", () => {
    const out = questionContainer(question, { turnId: "run-1" });
    every(out);
    expect(out.native).toBe(true);
    expect(out.blocks).toMatchSnapshot();
    const [box] = out.blocks as [Block];
    expect(box.type).toBe("container");
    // Slack shows a title on one line: the question is never cut there.
    expect(box.title).toEqual({
      type: "plain_text",
      text: defaultCopy.slackBlocks.questionTitle.open,
      emoji: true,
    });
    const children = box.child_blocks as Block[];
    expect(children.map((b) => b.type)).toEqual(["section", "actions", "input"]);
    expect((children[0]?.text as { text: string }).text).toMatch(
      new RegExp(`^\\*${question.question.replace("?", "\\?")}\\*\\n• \\*Check Production\\*`),
    );
    expect(buttons(out.blocks).map((b) => b.text.text)).toEqual([
      "Check Production",
      "Check Staging",
      defaultCopy.unsure,
    ]);
    expect(out.text).toBe(defaultCopy.inputNeeded(question.question));
  });

  it("sends a pick as the sections do, and the typed words as Something Else with its text", () => {
    const out = questionContainer(question, { turnId: "run-1" });
    expect(sends(out.blocks)["Check Production"]).toEqual(sends(out.fallback)["Check Production"]);
    const input = (out.blocks[0]?.child_blocks as Block[])[2] as Block & {
      block_id: string;
      element: { action_id: string };
    };
    // What Slack sends when Enter is pressed in the input.
    const typed = {
      type: "plain_text_input",
      block_id: input.block_id,
      action_id: input.element.action_id,
      value: "  check both  ",
    };
    expect(decodeSlackAction(typed)).toEqual({
      id: ActionId.clarifyOther,
      payload: { turnId: "run-1", target: "q1", text: "check both" },
    });
    expect(decodeSlackAction({ ...typed, value: " " })).toBeNull();
    expect(decodeSlackAction({ ...typed, block_id: "elsewhere" })).toBeNull();
  });

  it("shows the record in place once answered, with nothing to press", () => {
    const out = questionContainer(fx.answeredClarification, { turnId: "run-1" });
    const children = out.blocks[0]?.child_blocks as Block[];
    expect(children.map((b) => b.type)).toEqual(["section", "context"]);
    expect((children[0]?.text as { text: string }).text).toBe(fx.answeredClarification.question);
    expect(JSON.stringify(children)).toContain("Dana chose Check Production");
  });

  it("is the section layout in a modal", () => {
    const out = questionContainer(question, { turnId: "run-1", placement: "modal" });
    expect(out.native).toBe(false);
    expect(out.blocks).toEqual(out.fallback);
  });

  it("shows a long question whole, under the title", () => {
    const long = `${"Which environment should I check first? ".repeat(4)}`.trim();
    const out = questionContainer({ ...question, question: long }, { turnId: "run-1" });
    expect(out.native).toBe(true);
    expect(JSON.stringify(out.blocks)).toContain(long);
  });
});

describe("answer feedback", () => {
  it.each([
    ["done", fx.doneTurn],
    ["rated", fx.ratedTurn],
    ["review", fx.reviewTurn],
    ["artifact", fx.artifactTurn],
  ])("ends every %s answer with the rating control", (_k, turn) => {
    const blocks = renderToSlack(<TurnView turn={turn} />).blocks;
    const rating = blocks.find((b) => b.type === "context_actions");
    expect(rating?.elements).toEqual([expect.objectContaining({ type: "feedback_buttons" })]);
  });
});

/** `SLACK_VALIDATE=1 pnpm test` asks Slack itself, as `pnpm validate:slack` does. */
const env = (globalThis as { process?: { env: Record<string, string | undefined> } }).process?.env;

describe.runIf(env?.SLACK_VALIDATE === "1")("Slack's own check", () => {
  const validate = async (blocks: Block[], view?: object) => {
    const body = new URLSearchParams(
      view ? { view: JSON.stringify({ ...view, blocks }) } : { blocks: JSON.stringify(blocks) },
    );
    // blocks.validate needs no token, only the network; it asks callers to slow down at times.
    for (let attempt = 0; ; attempt++) {
      const res = await fetch("https://slack.com/api/blocks.validate", { method: "POST", body });
      const out = (await res.json()) as { ok: boolean; error?: string; errors?: unknown };
      if (out.error !== "ratelimited" || attempt >= 5) return out;
      await new Promise((r) => setTimeout(r, Number(res.headers.get("retry-after") ?? "2") * 1000));
    }
  };
  const cases: [string, NativeBlocks][] = [
    ["approval card", approvalCard(rollback, ctx)],
    ["approval card with more", approvalCard(fx.pendingApproval, ctx)],
    ["approved card", approvalCard(fx.approvedApproval, ctx)],
    ["carousel", approvalCarousel([rollback, post], ctx)],
    ["alert card", alertCard(knowledge, { consoleUrl: "https://platform.example.com", resolve })],
    ["question container", questionContainer(question(), { turnId: "run-1" })],
  ];
  function question() {
    return { ...fx.pendingClarification, allowUnsure: true };
  }
  it.each(cases)(
    "accepts the %s and its fallback, in a message and on Home",
    { timeout: 60_000 },
    async (_k, out) => {
      for (const blocks of [out.blocks, out.fallback]) {
        expect(await validate(blocks)).toMatchObject({ ok: true });
        expect(await validate(blocks, { type: "home" })).toMatchObject({ ok: true });
      }
    },
  );
});
