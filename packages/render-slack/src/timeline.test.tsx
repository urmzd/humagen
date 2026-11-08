import { TurnView } from "@humagen/components";
import type { Task, Turn } from "@humagen/core";
import { multiStepTurn } from "@humagen/core/fixtures";
import { describe, expect, it } from "vitest";
import { LIMITS, renderToSlack } from "./index";

type Ctx = { type: string; elements?: { text?: string }[] };
const contexts = (turn: Turn) =>
  (renderToSlack(<TurnView turn={turn} />).blocks as Ctx[])
    .filter((b) => b.type === "context")
    .map((b) => b.elements?.map((e) => e.text).join(" ") ?? "");

describe("a turn's steps in Slack", () => {
  it("are one context line per run of tool calls, in order, and no task cards", () => {
    const out = renderToSlack(<TurnView turn={multiStepTurn} />);
    expect(out.warnings).toEqual([]);
    expect((out.blocks as Ctx[]).some((b) => b.type === "task_card")).toBe(false);
    const lines = contexts(multiStepTurn);
    const runs = [
      lines.findIndex((l) => l.includes("Checked urmzd/playscape#21")),
      lines.findIndex((l) =>
        l.includes("Read 2 files, checked a source, ran a command | 1 failed"),
      ),
      lines.findIndex((l) => l.includes("Wrote scratchpads/review-notes.md +4")),
    ];
    expect(runs.every((i) => i >= 0)).toBe(true);
    expect([...runs].sort((a, b) => a - b)).toEqual(runs);
  });

  it("stay within the block limit for a long turn", () => {
    const tasks: Task[] = Array.from({ length: 200 }, (_, i) => ({
      id: `t${i}`,
      title: "Bash",
      tool: "bash",
      status: "complete",
      input: JSON.stringify({ command: `echo ${i}` }),
      at: i * 2,
    }));
    const turn: Turn = { ...multiStepTurn, text: "x".repeat(400), tasks };
    const out = renderToSlack(<TurnView turn={turn} />);
    expect(out.warnings).toEqual([]);
    for (const page of out.pages) expect(page.length).toBeLessThanOrEqual(LIMITS.blocks.message);
    expect(contexts(turn).some((l) => /^Used \d+ tools$/.test(l))).toBe(true);
  });
});
