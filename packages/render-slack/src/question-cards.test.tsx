import { ApprovalCard, ClarificationCard, defaultCopy } from "@humagen/components";
import type { Answer, Approval, Clarification } from "@humagen/core";
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
    .flatMap((b) => (b.elements ?? []).map((e) => (e.text as { text: string }).text));

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
