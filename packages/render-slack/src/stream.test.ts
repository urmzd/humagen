import { emptyTurn, reduce, type Turn } from "@vultus/core";
import * as fx from "@vultus/core/fixtures";
import { describe, expect, it } from "vitest";
import { lifecycle, reactionChanges } from "./lifecycle";
import {
  emptyBudget,
  STREAM_MESSAGE_CHARS,
  type StreamChunk,
  streamHeader,
  streamPages,
  streamStart,
  streamStopTitle,
  streamUpdate,
  suggestedPrompts,
} from "./stream";

describe("streamUpdate", () => {
  it("sends progress before text, and only the text delta", () => {
    const a: Turn = { ...emptyTurn("t", { name: "A" }), text: "Hello" };
    const b = reduce(reduce(a, { type: "text.delta", text: " world" }), {
      type: "task.upserted",
      task: { id: "x", title: "Searching", status: "in_progress" },
    });
    const { chunks, rewritten } = streamUpdate(a, b);
    expect(rewritten).toBe(false);
    expect(chunks).toEqual([
      { type: "task_update", id: "x", title: "Searching", status: "in_progress" },
      { type: "markdown_text", text: " world" },
    ]);
  });

  it("sends task details once, since Slack appends them", () => {
    const task = { id: "x", title: "Searched", status: "complete" as const, details: "#eng" };
    const a: Turn = { ...emptyTurn("t", { name: "A" }), tasks: [task] };
    const b: Turn = { ...a, tasks: [{ ...task, output: "3 matches" }] };
    expect(streamUpdate(a, b).chunks).toEqual([
      { type: "task_update", id: "x", title: "Searched", status: "complete", output: "3 matches" },
    ]);
  });

  it("streams citations as the numbered links the Sources list uses", () => {
    const cited: Turn = {
      ...emptyTurn("t", { name: "A" }),
      citations: [{ id: "c1", title: "Runbook", url: "https://x.dev/r" }],
    };
    const a: Turn = { ...cited, text: "Roll back first [[c" };
    const b: Turn = { ...cited, text: "Roll back first [[c1]] and an unknown one [[zz]]." };
    // The half-arrived marker and the space before it wait for the next append.
    expect(streamUpdate(cited, a).chunks).toEqual([
      { type: "markdown_text", text: "Roll back first" },
    ]);
    expect(streamUpdate(a, b).chunks).toEqual([
      { type: "markdown_text", text: " [[1]](https://x.dev/r) and an unknown one." },
    ]);
  });

  it("flags rewritten text so the host falls back to chat.update", () => {
    const a: Turn = { ...emptyTurn("t", { name: "A" }), text: "Draft" };
    expect(streamUpdate(a, { ...a, text: "Final" }).rewritten).toBe(true);
  });

  it("starts in plan mode for plan-shaped turns and ends on the outcome", () => {
    expect(streamStart(fx.streamingTurn)).toEqual({ task_display_mode: "plan" });
    expect(streamStopTitle(fx.doneTurn)).toBe("Worked for 12s");
    expect(streamStopTitle(fx.failedTurn)).toBe("Error");
  });
});

describe("streamPages", () => {
  const text = (chunks: StreamChunk[]) =>
    chunks.map((c) => (c.type === "markdown_text" ? c.text : "")).join("");

  it("keeps everything on the current message while it fits", () => {
    const chunks: StreamChunk[] = [{ type: "markdown_text", text: "Hello" }];
    const { pages, budget } = streamPages(emptyBudget(), chunks);
    expect(pages).toEqual([chunks]);
    expect(budget.text).toBe("Hello");
  });

  it("breaks at a line and loses nothing", () => {
    const lines = Array.from({ length: 40 }, (_, i) => `line ${i} of the answer`).join("\n");
    const { pages } = streamPages(emptyBudget(), [{ type: "markdown_text", text: lines }], 300);
    expect(pages.length).toBeGreaterThan(2);
    for (const page of pages) expect(text(page).length).toBeLessThanOrEqual(300);
    for (const page of pages.slice(0, -1)) expect(text(page).endsWith("\n")).toBe(true);
    expect(pages.map(text).join("")).toBe(lines);
  });

  it("never rewrites text already sent", () => {
    const first = streamPages(
      emptyBudget(),
      [{ type: "markdown_text", text: "x".repeat(250) }],
      300,
    );
    const { pages } = streamPages(
      first.budget,
      [{ type: "markdown_text", text: "word ".repeat(40) }],
      300,
    );
    // No line break in reach, so the whole addition moves rather than splitting mid-sentence.
    expect(pages[0]).toEqual([]);
    expect(text(pages[1] ?? [])).toBe("word ".repeat(40));
  });

  it("closes a code fence at the cut and reopens it", () => {
    const code = [
      "Intro",
      "```ts",
      ...Array.from({ length: 30 }, (_, i) => `const v${i} = ${i};`),
      "```",
    ];
    const { pages } = streamPages(
      emptyBudget(),
      [{ type: "markdown_text", text: code.join("\n") }],
      300,
    );
    expect(text(pages[0] ?? []).endsWith("\n```")).toBe(true);
    for (const page of pages.slice(1)) expect(text(page).startsWith("```ts\n")).toBe(true);
    for (const page of pages) expect(text(page).match(/```/g)?.length).toBe(2);
  });

  it("repeats the table header on the continuation", () => {
    const table = [
      "| A | B |",
      "| --- | --- |",
      ...Array.from({ length: 40 }, (_, i) => `| ${i} | row |`),
    ];
    const { pages } = streamPages(
      emptyBudget(),
      [{ type: "markdown_text", text: table.join("\n") }],
      300,
    );
    expect(pages.length).toBeGreaterThan(1);
    for (const page of pages)
      expect(text(page).startsWith("| A | B |\n| --- | --- |\n")).toBe(true);
  });

  it("starts a new message before a table passes Slack's 100 rows", () => {
    const rows = Array.from({ length: 150 }, (_, i) => `| ${i} | row |`);
    const table = ["| A | B |", "| --- | --- |", ...rows].join("\n");
    const { pages } = streamPages(emptyBudget(), [{ type: "markdown_text", text: table }]);
    expect(pages).toHaveLength(2);
    // Header plus 99 rows, then the header again with the rest.
    expect(
      text(pages[0] ?? [])
        .trimEnd()
        .split("\n"),
    ).toHaveLength(101);
    expect(
      text(pages[1] ?? [])
        .split("\n")
        .slice(0, 3),
    ).toEqual(["| A | B |", "| --- | --- |", "| 99 | row |"]);
  });

  it("cuts a live stream between lines, not mid-row", () => {
    const rows = [
      "| A | B |",
      "| --- | --- |",
      ...Array.from({ length: 30 }, (_, i) => `| ${i} | row |`),
    ];
    let budget = emptyBudget();
    const sent: string[] = [""];
    // Arrives a few characters at a time, the way tokens do.
    for (const piece of rows.join("\n").match(/.{1,7}/gs) ?? []) {
      const out = streamPages(budget, [{ type: "markdown_text", text: piece }], 300);
      out.pages.forEach((page, i) => {
        if (i > 0) sent.push("");
        sent[sent.length - 1] += text(page);
      });
      budget = out.budget;
    }
    expect(sent.length).toBeGreaterThan(1);
    for (const message of sent) {
      expect(message.startsWith("| A | B |\n| --- | --- |\n")).toBe(true);
      for (const line of message.trimEnd().split("\n")) expect(line).toMatch(/^\|.*\|$/);
    }
  });

  it("settles running tasks on the message it leaves", () => {
    const task = {
      type: "task_update" as const,
      id: "t",
      title: "Searching",
      status: "in_progress" as const,
    };
    const started = streamPages(
      emptyBudget(),
      [task, { type: "markdown_text", text: "x".repeat(100) }],
      300,
    );
    const { pages } = streamPages(
      started.budget,
      [{ type: "markdown_text", text: `\n${"y".repeat(250)}` }],
      300,
    );
    expect(pages[0]?.at(-1)).toEqual({
      type: "task_update",
      id: "t",
      title: "Searching",
      status: "complete",
    });
    expect(pages).toHaveLength(2);
  });

  it("counts a task update once, however often it changes", () => {
    let budget = emptyBudget();
    for (let i = 0; i < 200; i++) {
      const out = streamPages(budget, [
        { type: "task_update", id: "t", title: "Searching", status: "in_progress" },
      ]);
      expect(out.pages).toHaveLength(1);
      budget = out.budget;
    }
  });

  it("paginates the long fixture under the message budget", () => {
    const { pages } = streamPages(emptyBudget(), streamUpdate(null, fx.longTurn).chunks);
    expect(pages.length).toBeGreaterThan(2);
    for (const page of pages) expect(text(page).length).toBeLessThanOrEqual(STREAM_MESSAGE_CHARS);
  });
});

describe("streamHeader", () => {
  it("numbers continuations and never carries the session link", () => {
    const context = (chunks: StreamChunk[]) => JSON.stringify(chunks);
    // The first message opens with nothing: the session link belongs at the end, once.
    expect(streamHeader(fx.doneTurn)).toEqual([]);
    expect(context(streamHeader(fx.doneTurn, { page: 2 }))).toContain('"text":"Part 2"');
    expect(context(streamHeader(fx.doneTurn, { page: 2 }))).not.toContain("example.com");
  });
});

describe("lifecycle", () => {
  it("maps each status to one reaction and a session status", () => {
    expect(lifecycle(fx.streamingTurn)).toEqual({
      reaction: "eyes",
      sessionStatus: "processing",
      statusText: "Checking recent deploys",
    });
    expect(lifecycle(fx.waitingTurn)).toMatchObject({
      reaction: "raised_hand",
      sessionStatus: "suspended",
    });
    expect(lifecycle(fx.failedTurn)).toMatchObject({ reaction: "x", sessionStatus: "active" });
  });

  it("swaps reactions on transitions", () => {
    expect(reactionChanges("working", "done")).toEqual({
      add: ["white_check_mark"],
      remove: ["eyes"],
    });
    expect(reactionChanges("queued", "working")).toEqual({ add: [], remove: [] });
  });

  it("caps suggested prompts at four", () => {
    const suggestions = Array.from({ length: 6 }, (_, i) => ({ title: `T${i}`, message: `M${i}` }));
    expect(suggestedPrompts({ suggestions }).prompts).toHaveLength(4);
  });
});
