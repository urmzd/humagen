import {
  ApprovalBatch,
  ApprovalCard,
  ClarificationCard,
  defaultCopy,
  TurnView,
} from "@humagen/components";
import { ActionId, type Answer, type Approval, type Clarification } from "@humagen/core";
import * as fx from "@humagen/core/fixtures";
import { describe, expect, it } from "vitest";
import { renderToSlack } from "./index";

const at = "2026-10-09T00:12:00Z";

const approval: Approval = {
  type: "approval",
  id: "q-approve",
  summary: "Allow lead to call deploy?",
  tool: "deploy",
  args: { service: "web" },
  decision: { kind: "pending" },
};

const question: Clarification = {
  type: "clarification",
  id: "q-choose",
  question: "Which region?",
  options: [
    { id: "us-east", label: "us-east" },
    { id: "eu-west", label: "eu-west" },
  ],
  answer: { kind: "pending" },
};

type Block = { type: string; elements?: { text?: unknown }[] };
const blocksOf = (el: React.ReactNode) => renderToSlack(el).blocks as Block[];
const contexts = (blocks: Block[]) => blocks.filter((b) => b.type === "context");
const buttons = (blocks: Block[]) =>
  blocks
    .filter((b) => b.type === "actions")
    .flatMap((b) =>
      (b.elements ?? []).filter((e) => e.text).map((e) => (e.text as { text: string }).text),
    );

describe("an approval's reason and risk", () => {
  it("are one line under the call, the workspace's fields first", () => {
    const blocks = blocksOf(
      <ApprovalCard
        turnId="r1"
        approval={{
          ...approval,
          judgement: {
            decision: "ask",
            reason: "Deploys during a freeze need a person",
            fields: [{ label: "Risk", value: "High" }],
            references: ["Change policy 4.2"],
          },
        }}
      />,
    );
    // The call, its arguments, the one line, then the buttons.
    expect(blocks.map((b) => b.type)).toEqual(["section", "section", "context", "actions"]);
    expect(contexts(blocks)[0]?.elements?.map((e) => e.text)).toEqual([
      "*Risk:* High",
      '*Approval agent:* Asked a person: "Deploys during a freeze need a person"',
      "Based on: Change policy 4.2",
    ]);
  });

  it("is the reason alone when the data carries no field", () => {
    const blocks = blocksOf(
      <ApprovalCard
        turnId="r1"
        approval={{ ...approval, judgement: { decision: "ask", reason: "Needs a person" } }}
      />,
    );
    expect(contexts(blocks)).toHaveLength(1);
    expect(contexts(blocks)[0]?.elements).toHaveLength(1);
  });

  it("is nothing when the data carries neither", () => {
    const blocks = blocksOf(<ApprovalCard turnId="r1" approval={approval} />);
    expect(blocks.map((b) => b.type)).toEqual(["section", "section", "actions"]);
  });

  it("stays on the record once decided, with the buttons gone", () => {
    const blocks = blocksOf(
      <ApprovalCard
        turnId="r1"
        approval={{
          ...approval,
          judgement: { decision: "ask", reason: "Needs a person" },
          decision: { kind: "approved", by: "Olu Ade", at },
        }}
      />,
    );
    expect(buttons(blocks)).toEqual([]);
    expect(contexts(blocks)).toHaveLength(2);
    expect(JSON.stringify(contexts(blocks)[1])).toContain("Approved by Olu Ade");
  });

  it.each([
    ["allow", "approved", "Allowed", "Approved by Olu Ade"],
    ["deny", "denied", "Denied", "Rejected by Olu Ade"],
  ] as const)(
    "says what the approval agent decided and why once it %ss, its note not repeated",
    (decision, kind, said, record) => {
      const reason = "The person asked for this deploy.";
      const blocks = blocksOf(
        <ApprovalCard
          turnId="r1"
          approval={{
            ...approval,
            judgement: { decision, reason },
            decision: {
              kind,
              by: "Olu Ade",
              at,
              reason: `${said} by the approval agent: ${reason}`,
            },
          }}
        />,
      );
      const said_ = JSON.stringify(contexts(blocks));
      expect(said_).toContain(`*Approval agent:* ${said}: \\"${reason}\\"`);
      expect(said_).toContain(record);
      // The record keeps who and when: the reason is said once.
      expect(said_.split(reason)).toHaveLength(2);
      expect(buttons(blocks)).toEqual([]);
    },
  );
});

describe("an approval's menu", () => {
  type Overflow = { type: string; options: { text: { text: string }; value: string }[] };
  const overflows = (blocks: Block[]) =>
    blocks
      .filter((b) => b.type === "actions")
      .flatMap((b) => (b.elements ?? []) as unknown as Overflow[])
      .filter((e) => e.type === "overflow");

  it("offers Auto for the thread only where the host says it can", () => {
    expect(overflows(blocksOf(<ApprovalCard turnId="r1" approval={approval} />))).toEqual([]);
    const [menu] = overflows(blocksOf(<ApprovalCard turnId="r1" approval={approval} offerAgent />));
    expect(menu?.options.map((o) => o.text.text)).toEqual(["Set this thread to Auto"]);
    expect(JSON.parse(menu?.options[0]?.value ?? "{}")).toEqual({
      id: ActionId.approvalMode,
      p: { turnId: "r1", target: "auto" },
    });
    // The decision stays with the buttons; the menu sets the thread.
    expect(buttons(blocksOf(<ApprovalCard turnId="r1" approval={approval} offerAgent />))).toEqual(
      buttons(blocksOf(<ApprovalCard turnId="r1" approval={approval} />)),
    );
  });

  it("offers Auto on several approvals at once too, beside approve all and deny all", () => {
    const several = [approval, { ...approval, id: "a2", summary: "Send the second one" }];
    const texts = (el: React.ReactNode) =>
      overflows(blocksOf(el)).flatMap((m) => m.options.map((o) => o.text.text));
    expect(texts(<ApprovalBatch turnId="r1" approvals={several} />)).toEqual([
      defaultCopy.approveAll(2),
      defaultCopy.denyAll,
    ]);
    const [menu] = overflows(
      blocksOf(<ApprovalBatch turnId="r1" approvals={several} offerAgent />),
    );
    expect(menu?.options.map((o) => o.text.text)).toEqual([
      defaultCopy.approveAll(2),
      defaultCopy.approvals.letAgentDecide,
      defaultCopy.denyAll,
    ]);
    expect(JSON.parse(menu?.options[1]?.value ?? "{}")).toEqual({
      id: ActionId.approvalMode,
      p: { turnId: "r1", target: "auto" },
    });
    // A run held at a limit is the person's to decide.
    const held = {
      ...approval,
      id: "a3",
      budget: {
        scope: "the workspace",
        limit: "spend" as const,
        used: "$1",
        max: "$1",
        grant: "$1",
      },
    };
    expect(texts(<ApprovalBatch turnId="r1" approvals={[approval, held]} offerAgent />)).toEqual([
      defaultCopy.approveAll(2),
      defaultCopy.denyAll,
    ]);
  });

  it("is not offered once decided, on a run held at a limit, or to someone who may not decide", () => {
    const decided = { ...approval, decision: { kind: "approved" as const, by: "Olu Ade", at } };
    expect(overflows(blocksOf(<ApprovalCard turnId="r1" approval={decided} offerAgent />))).toEqual(
      [],
    );
    const held = {
      ...approval,
      budget: {
        scope: "the workspace",
        limit: "spend" as const,
        used: "$1",
        max: "$1",
        grant: "$1",
      },
    };
    expect(overflows(blocksOf(<ApprovalCard turnId="r1" approval={held} offerAgent />))).toEqual(
      [],
    );
    const theirs = { ...approval, initiator: "dana" };
    expect(
      overflows(blocksOf(<ApprovalCard turnId="r1" approval={theirs} viewer="olu" offerAgent />)),
    ).toEqual([]);
  });

  it("is offered on a turn's waiting approval through the turn's capabilities", () => {
    const turn = {
      ...fx.skillApprovalTurn,
    };
    expect(overflows(blocksOf(<TurnView turn={turn} />))).toEqual([]);
    expect(
      overflows(blocksOf(<TurnView turn={turn} capabilities={{ approvalAgent: true }} />)),
    ).toHaveLength(1);
  });
});

describe("a question's card", () => {
  it("lists each option's one line of context above the buttons", () => {
    const described = {
      ...question,
      options: [
        { id: "us-east", label: "us-east", description: "Closest to most customers" },
        { id: "eu-west", label: "eu-west" },
      ],
    };
    const [first] = blocksOf(<ClarificationCard turnId="r1" clarification={described} />);
    expect(JSON.stringify(first)).toContain("• *us-east*: Closest to most customers");
    expect(JSON.stringify(first)).not.toContain("• *eu-west*");
  });

  const settled: [string, Answer, string][] = [
    [
      "an option",
      { kind: "answered", by: "Olu Ade", at, optionIds: ["eu-west"] },
      "Olu Ade chose eu-west",
    ],
    [
      "words",
      { kind: "answered", by: "Olu Ade", at, optionIds: [], other: "ap-south" },
      'Olu Ade answered \\"ap-south\\"',
    ],
    [
      "nobody in time",
      { kind: "expired", at, note: "Nobody answered in time, so it went with us-east" },
      "Nobody answered in time, so it went with us-east",
    ],
    ["nobody in time, with no default", { kind: "expired", at }, defaultCopy.questionExpired],
    [
      "a stopped run",
      { kind: "cancelled", at, note: "The run was stopped" },
      "Not answered. The run was stopped",
    ],
    ["a run that ended", { kind: "cancelled", at }, "Not answered. The run ended first"],
  ];
  it.each(settled)("settled by %s is a record with no button", (_name, answer, said) => {
    const blocks = blocksOf(
      <ClarificationCard turnId="r1" clarification={{ ...question, answer }} />,
    );
    expect(blocks.map((b) => b.type)).toEqual(["section", "context"]);
    expect(JSON.stringify(blocks[1])).toContain(said);
    expect(JSON.stringify(blocks)).not.toContain("Declined by Nobody");
  });
});
