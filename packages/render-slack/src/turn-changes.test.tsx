import { changedFilesLines, MAX_CHANGED_FILES, shownPath, TurnView } from "@humagen/components";
import { type Task, type Turn, turnChanges } from "@humagen/core";
import { changesTurn, manyChangesTurn, multiStepTurn } from "@humagen/core/fixtures";
import { describe, expect, it } from "vitest";
import { renderToSlack } from "./index";

type Ctx = { type: string; elements?: { type?: string; text?: string }[] };

/** The summary's block: the one context block that says how many files changed. */
const summary = (turn: Turn, streamed = false) =>
  (renderToSlack(<TurnView turn={turn} streamed={streamed} />).blocks as Ctx[]).filter(
    (b) => b.type === "context" && /files? changed\*/.test(b.elements?.[0]?.text ?? ""),
  );

describe("what a finished turn changed, in Slack", () => {
  it("is one context block: the total, then each file once with its lines", () => {
    const out = renderToSlack(<TurnView turn={changesTurn} />);
    expect(out.warnings).toEqual([]);
    const [block, ...others] = summary(changesTurn);
    expect(others).toEqual([]);
    expect(block?.elements).toEqual([
      {
        type: "mrkdwn",
        text: [
          "*3 files changed* +11 -1",
          "`services/payments-api/src/refund.ts` +3 -1",
          "`services/payments-api/migrations/0042_refund_reason.sql` +2",
          "`services/payments-api/src/refund.test.ts` +6",
        ].join("\n"),
      },
    ]);
  });

  it("sits under the answer and above feedback and the footer", () => {
    const types = (renderToSlack(<TurnView turn={changesTurn} />).blocks as Ctx[]).map((b) =>
      b.type === "context" && /files changed/.test(b.elements?.[0]?.text ?? "")
        ? "changes"
        : b.type,
    );
    const at = types.indexOf("changes");
    expect(at).toBeGreaterThan(types.indexOf("markdown"));
    expect(at).toBeLessThan(types.indexOf("context_actions"));
  });

  it("is in the blocks that close a streamed message", () => {
    expect(summary(changesTurn, true)).toHaveLength(1);
  });

  it("names one file without repeating its counts", () => {
    expect(summary(multiStepTurn)[0]?.elements?.[0]?.text).toBe(
      "*1 file changed*\n`scratchpads/review-notes.md` +4",
    );
  });

  it("names the first files and counts the rest", () => {
    const text = summary(manyChangesTurn)[0]?.elements?.[0]?.text ?? "";
    const lines = text.split("\n");
    expect(lines[0]).toBe("*11 files changed* +19 -11");
    expect(lines).toHaveLength(MAX_CHANGED_FILES + 1);
    expect(lines.at(-1)).toBe("and 4 more");
    expect(lines.filter((l) => l.startsWith("`"))).toHaveLength(MAX_CHANGED_FILES - 1);
  });

  it("is left out while the turn is still going, and when it changed nothing", () => {
    expect(summary({ ...changesTurn, status: "working" })).toEqual([]);
    expect(summary({ ...changesTurn, status: "waiting" })).toEqual([]);
    expect(summary({ ...changesTurn, tasks: [] })).toEqual([]);
    expect(changedFilesLines([])).toEqual([]);
  });

  it("is shown for a turn that failed or was stopped after it wrote", () => {
    expect(summary({ ...changesTurn, status: "stopped" })).toHaveLength(1);
    expect(
      summary({
        ...changesTurn,
        status: "failed",
        failure: { kind: "internal", message: "It broke." },
      } as Turn),
    ).toHaveLength(1);
  });

  it("leaves out a write that failed", () => {
    const tasks: Task[] = changesTurn.tasks.map((t) =>
      t.id === "ch4" ? { ...t, status: "error", output: "action failed: no space" } : t,
    );
    expect(summary({ ...changesTurn, tasks })[0]?.elements?.[0]?.text).not.toContain(
      "refund.test.ts",
    );
  });

  it("shows a path a model wrote as inert code on one line", () => {
    const tasks: Task[] = [
      {
        id: "x1",
        title: "File write",
        tool: "file_write",
        status: "complete",
        input: JSON.stringify({
          path: "a`b\n<!channel> [x](http://evil.example).md",
          content: "x",
        }),
      },
    ];
    const text = summary({ ...changesTurn, tasks })[0]?.elements?.[0]?.text ?? "";
    expect(text.split("\n")).toHaveLength(2);
    expect(text).not.toContain("<!channel>");
    expect(text).not.toContain("<http://evil.example");
    expect(text).toContain("&lt;!channel&gt;");
  });

  it("keeps the end of a long path", () => {
    const long = `${"deep/".repeat(40)}file.ts`;
    const shown = shownPath(long);
    expect(shown.length).toBe(72);
    expect(shown.startsWith("…")).toBe(true);
    expect(shown.endsWith("deep/file.ts")).toBe(true);
    expect(turnChanges(changesTurn.tasks).map((f) => shownPath(f.path))).toEqual(
      turnChanges(changesTurn.tasks).map((f) => f.path),
    );
  });
});
