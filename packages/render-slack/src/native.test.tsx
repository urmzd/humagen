import { defaultCopy, TurnView } from "@humagen/components";
import { ActionId, type Approval } from "@humagen/core";
import * as fx from "@humagen/core/fixtures";
import { describe, expect, it } from "vitest";
import {
  type AlertInput,
  alertCard,
  alertLevel,
  approvalCard,
  approvalCarousel,
  type Block,
  decodeSlackAction,
  type NativeBlocks,
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
const ctx = { turnId: "run-1", agent: "lead", session: "Payments 5xx", at };

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

const every = (out: NativeBlocks) => {
  expect(out.text.trim().length).toBeGreaterThan(0);
  expect(out.fallback.length).toBeGreaterThan(0);
  for (const b of out.blocks) expect(b).not.toHaveProperty("fallback");
};

describe("an approval as a card", () => {
  it("has the action as its title, who and where as its subtitle, what it does as its body, then Deny and Approve", () => {
    const out = approvalCard(rollback, ctx);
    every(out);
    expect(out.native).toBe(true);
    expect(out.blocks).toMatchSnapshot();
    const [card] = out.blocks as [Block];
    expect(card.type).toBe("card");
    expect((card.subtitle as { text: string }).text).toMatch(/^lead · Payments 5xx · <!date\^/);
    const labels = buttons(out.blocks).map((b) => [b.text.text, (b as { style?: string }).style]);
    expect(labels).toEqual([
      [defaultCopy.declineAction, "danger"],
      ["Roll Back", "primary"],
      [defaultCopy.askApproval, undefined],
    ]);
    expect(out.text).toBe(
      `${defaultCopy.approvalNeeded(rollback.summary)}\nlead · Payments 5xx · 2026-10-08 14:52 UTC`,
    );
  });

  it("sends what the section layout sends, so the app's handlers take the same clicks", () => {
    const out = approvalCard(rollback, ctx);
    expect(sends(out.blocks)).toEqual(sends(out.fallback));
    expect(sends(out.blocks)[defaultCopy.declineAction]).toEqual({
      id: ActionId.deny,
      payload: { turnId: "run-1", target: "a1" },
    });
    expect((out.blocks[0] as Block).block_id).toBe("approval:a1:r1");
  });

  it("keeps the destructive confirm step on Approve", () => {
    const approve = buttons(approvalCard(rollback, ctx).blocks)[1] as Button & {
      confirm?: { style?: string };
    };
    expect(approve.confirm?.style).toBe("danger");
  });

  it("adds the approval agent's reason to the body when it handed the decision to a person", () => {
    const out = approvalCard(rollback, {
      ...ctx,
      reasonBy: "approver",
      reason: "Production needs a person",
    });
    expect((out.blocks[0]?.body as { text: string }).text).toContain(
      defaultCopy.slackBlocks.handedOver("approver", "Production needs a person"),
    );
  });

  it("puts choices past the card's three in a row under it", () => {
    const out = approvalCard(fx.pendingApproval, ctx);
    expect(out.native).toBe(true);
    expect(out.blocks.map((b) => b.type)).toEqual(["card", "actions"]);
    expect(buttons([out.blocks[1] as Block]).map((b) => b.text.text)).toEqual([defaultCopy.edit]);
    expect(sends(out.blocks)).toEqual(sends(out.fallback));
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
    expect(buttons(out.blocks).map((b) => b.text.text)).toEqual([defaultCopy.askApproval]);
    expect((out.blocks[0]?.subtext as { text: string }).text).toContain(defaultCopy.notYours);
  });

  it("posts the section layout rather than cut a word a card can't hold", () => {
    const long = { ...rollback, summary: `${"Roll back every service ".repeat(8)}now?` };
    const out = approvalCard(long, ctx);
    expect(out.native).toBe(false);
    expect(out.blocks).toEqual(out.fallback);
    expect(out.warnings[0]).toMatch(/card title is \d+ chars; a card holds 150/);
    expect(JSON.stringify(out.blocks)).toContain("Roll back every service Roll back");
  });
});

describe("several approvals as a carousel", () => {
  it("is one line, a card for each, then Approve All and Cancel All", () => {
    const out = approvalCarousel([rollback, post], ctx);
    every(out);
    expect(out.native).toBe(true);
    expect(out.blocks).toMatchSnapshot();
    expect(out.blocks.map((b) => b.type)).toEqual(["section", "carousel", "actions"]);
    expect((out.blocks[0]?.text as { text: string }).text).toBe("*lead wants to run 2 actions*");
    const cards = out.blocks[1]?.elements as Block[];
    expect(cards.map((c) => c.block_id)).toEqual(["approval:a1:r1", "approval:a2:r0"]);
    // action_ids stay unique across the carousel.
    const ids = buttons(out.blocks).map((b) => b.action_id);
    expect(new Set(ids).size).toBe(ids.length);
    expect(sends([out.blocks[2] as Block])).toEqual({
      [defaultCopy.approveAll(2)]: {
        id: ActionId.approveAll,
        payload: { turnId: "run-1", target: "batch:run-1" },
      },
      [defaultCopy.denyAll]: {
        id: ActionId.denyAll,
        payload: { turnId: "run-1", target: "batch:run-1" },
      },
    });
    expect(out.text).toBe(
      `${defaultCopy.approvalNeeded("lead wants to run 2 actions")}\n• ${rollback.summary}\n• ${post.summary}`,
    );
  });

  it.each([
    ["more than ten", Array.from({ length: 11 }, (_, i) => ({ ...post, id: `a${i}` })), {}],
    ["a card with more choices than it holds", [fx.pendingApproval, post], {}],
    ["a modal", [rollback, post], { placement: "modal" as const }],
  ])("falls back to today's checkbox layout for %s", (_k, approvals, extra) => {
    const out = approvalCarousel(approvals, { ...ctx, ...extra });
    expect(out.native).toBe(false);
    expect(out.blocks).toEqual(out.fallback);
    expect(out.blocks.map((b) => b.type)).toContain("actions");
    expect(JSON.stringify(out.blocks)).toContain("checkboxes");
    expect(out.text.length).toBeGreaterThan(0);
  });

  it("is a single card for one approval", () => {
    expect(approvalCarousel([rollback], ctx).blocks).toEqual(approvalCard(rollback, ctx).blocks);
  });
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

  it("has the question as its title, the options as buttons and an input for its own answer", () => {
    const out = questionContainer(question, { turnId: "run-1" });
    every(out);
    expect(out.native).toBe(true);
    expect(out.blocks).toMatchSnapshot();
    const [box] = out.blocks as [Block];
    expect(box.type).toBe("container");
    expect(box.title).toEqual({ type: "plain_text", text: question.question, emoji: true });
    const children = box.child_blocks as Block[];
    expect(children.map((b) => b.type)).toEqual(["section", "actions", "input"]);
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
    expect(children.map((b) => b.type)).toEqual(["context"]);
    expect(JSON.stringify(children)).toContain("Dana chose Check Production");
  });

  it.each([
    ["a modal", question, { placement: "modal" as const }],
    ["a question longer than its title holds", { ...question, question: "Which? ".repeat(30) }, {}],
  ])("is the section layout in %s", (_k, clarification, extra) => {
    const out = questionContainer(clarification, { turnId: "run-1", ...extra });
    expect(out.native).toBe(false);
    expect(out.blocks).toEqual(out.fallback);
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
