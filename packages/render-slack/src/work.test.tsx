import { RunDetailsView, TurnView } from "@humagen/components";
import type { Task, Turn } from "@humagen/core";
import { delegatingTurn, workingTurn, workTurn } from "@humagen/core/fixtures";
import { describe, expect, it } from "vitest";
import { renderToSlack } from "./index";

// biome-ignore lint/suspicious/noExplicitAny: Block Kit JSON is untyped by design
type Any = any;
const blocks = (turn: Turn, props: Partial<Parameters<typeof TurnView>[0]> = {}) =>
  renderToSlack(<TurnView turn={turn} {...props} />).blocks as Any[];
const contexts = (bs: Any[]) =>
  bs.filter((b) => b.type === "context").map((b) => b.elements.map((e: Any) => e.text).join(" "));
const pre = (bs: Any[]) =>
  bs
    .filter((b) => b.type === "rich_text")
    .map((b) => b.elements.map((e: Any) => e.elements.map((t: Any) => t.text).join("")).join(""));
const buttons = (bs: Any[]) =>
  bs.filter((b) => b.type === "actions").flatMap((b) => b.elements.map((e: Any) => e.text.text));

describe("a finished turn's work in Slack", () => {
  it("is one line counted by kind", () => {
    const lines = contexts(blocks(workTurn));
    expect(lines[0]).toBe(
      "Ran 2 searches, read 3 files, ran 3 commands (2 failed), updated 2 files (+4)",
    );
    // No line per run of calls is left.
    expect(lines.filter((l) => /^(Ran|Read|Updated|Wrote)/.test(l))).toHaveLength(1);
  });

  it("shows what is happening now while the turn is going", () => {
    const lines = contexts(blocks(workingTurn));
    expect(lines[0]).toBe("Running shell");
    expect(lines.some((l) => l.includes("searches"))).toBe(false);
  });

  it("keeps each call to another agent as its own row", () => {
    const turn: Turn = { ...delegatingTurn, planTitle: undefined, status: "done" };
    const bs = blocks(turn);
    const cards = bs.filter((b) => b.type === "task_card").map((b) => b.title);
    expect(cards).toHaveLength(2);
    expect(contexts(bs)[0]).toMatch(/^[A-Z]/);
    expect(bs.findIndex((b) => b.type === "context")).toBeLessThan(
      bs.findIndex((b) => b.type === "task_card"),
    );
  });

  it("still says what a lone call was pointed at", () => {
    const turn: Turn = { ...workTurn, tasks: workTurn.tasks.slice(2, 3) };
    expect(contexts(blocks(turn))[0]).toBe("Read refund.ts");
  });
});

describe("a command that failed", () => {
  it("shows its command, exit code and output, and a passed one stays a count", () => {
    const bs = blocks(workTurn);
    const lines = contexts(bs);
    expect(lines).toContain("*Failed* · exit 2 · 0.6s");
    const shown = pre(bs);
    expect(shown).toHaveLength(1);
    expect(shown[0]).toContain("$ pnpm exec biome check src");
    expect(shown[0]).toContain("Found 1 error.");
    // The exit line is said once, in the line above, not repeated in the output.
    expect(shown[0]).not.toContain("exit 2");
    // The test run that later passed is in the count only.
    expect(shown[0]).not.toContain("pnpm test");
    expect(lines.some((l) => l.includes("Passed"))).toBe(false);
  });

  it("is in the final blocks of a streamed turn too, where the steps were streamed", () => {
    const bs = blocks(workTurn, { streamed: true });
    expect(contexts(bs)).toContain("*Failed* · exit 2 · 0.6s");
    expect(contexts(bs).some((l) => l.includes("searches"))).toBe(false);
    expect(pre(bs)[0]).toContain("$ pnpm exec biome check src");
  });

  it("shows the newest three and counts the earlier ones", () => {
    const tasks: Task[] = Array.from({ length: 5 }, (_, i) => ({
      id: `c${i}`,
      title: "Shell",
      tool: "shell",
      status: "complete",
      input: JSON.stringify({ command: `make step-${i}` }),
      output: `exit 1\nstep ${i} broke`,
    }));
    const bs = blocks({ ...workTurn, tasks });
    expect(pre(bs).map((t) => t.split("\n")[0])).toEqual([
      "$ make step-2",
      "$ make step-3",
      "$ make step-4",
    ]);
    expect(contexts(bs)).toContain("2 earlier failed commands not shown");
  });

  it("keeps a long output to its end, within Slack's limits", () => {
    const output = `exit 1\n${Array.from({ length: 400 }, (_, i) => `line ${i}`).join("\n")}`;
    const tasks: Task[] = [
      {
        id: "c",
        title: "Shell",
        tool: "shell",
        status: "complete",
        input: '{"command":"make"}',
        output,
      },
    ];
    const out = renderToSlack(<TurnView turn={{ ...workTurn, tasks }} />);
    expect(out.warnings).toEqual([]);
    const shown = pre(out.blocks as Any[])[0] as string;
    expect(shown).toContain("line 399");
    expect(shown).not.toContain("line 10\n");
    expect(shown).toMatch(/… \d+ earlier lines/);
  });

  it("says no exit code for a command that passed", () => {
    const view = renderToSlack(
      <TurnView
        turn={{
          ...workTurn,
          tasks: [],
          attachments: [
            {
              type: "command",
              id: "a",
              command: "make",
              status: "complete",
              exitCode: 0,
              durationMs: 1200,
            },
          ],
        }}
      />,
    );
    expect(contexts(view.blocks as Any[])).toContain("*Passed* · 1.2s");
  });
});

describe("Run Details", () => {
  it("is offered on a finished turn only where the host can answer it", () => {
    expect(buttons(blocks(workTurn))).not.toContain("Run Details");
    expect(buttons(blocks(workTurn, { capabilities: { runDetails: true } }))).toContain(
      "Run Details",
    );
    expect(
      buttons(blocks(workTurn, { capabilities: { runDetails: true }, streamed: true })),
    ).toContain("Run Details");
    expect(buttons(blocks(workingTurn, { capabilities: { runDetails: true } }))).not.toContain(
      "Run Details",
    );
  });

  it("puts no tokens or cost on the turn itself", () => {
    const turn: Turn = {
      ...workTurn,
      usage: { model: "m", inputTokens: 10, outputTokens: 5, costUsd: 1.5 },
    };
    const text = JSON.stringify(blocks(turn, { capabilities: { runDetails: true } }));
    expect(text).not.toMatch(/token|\$1\.5|Cost/i);
  });

  it("reads as the run's agent, model, tokens, cost and elapsed time", () => {
    const view = renderToSlack(
      <RunDetailsView
        details={{
          agent: "lead",
          model: "claude-sonnet-4-5",
          inputTokens: 48210,
          cachedTokens: 39000,
          outputTokens: 1840,
          cost: { micros: 42100, currency: "USD", atLeast: true },
          elapsedMs: 52000,
        }}
      />,
    );
    expect(view.warnings).toEqual([]);
    const fields = (view.blocks as Any[]).flatMap((b) => b.fields ?? []).map((f: Any) => f.text);
    expect(fields).toEqual([
      "*Agent*\nlead",
      "*Model*\nclaude-sonnet-4-5",
      "*Input tokens*\n48,210 (39,000 from cache)",
      "*Output tokens*\n1,840",
      "*Cost*\nAt least $0.0421",
      "*Elapsed*\n52s",
    ]);
    expect(contexts(view.blocks as Any[])).toEqual(["Only you can see this."]);
  });

  it("never shows tokens with no price as a cost of zero, and says when nothing was recorded", () => {
    const unpriced = renderToSlack(
      <RunDetailsView details={{ inputTokens: 900, outputTokens: 120 }} />,
    );
    expect(JSON.stringify(unpriced.blocks)).toContain("*Cost*\\nNot priced");
    const none = renderToSlack(<RunDetailsView details={{ agent: "lead", elapsedMs: 800 }} />);
    expect(contexts(none.blocks as Any[])).toEqual([
      "This run recorded no model usage.",
      "Only you can see this.",
    ]);
  });
});
