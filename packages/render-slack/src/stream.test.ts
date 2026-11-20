import { emptyTurn, reduce, type Turn } from "@humagen/core";
import * as fx from "@humagen/core/fixtures";
import { describe, expect, it } from "vitest";
import { lifecycle, reactionChanges, STATUS_TEXT_CHARS, statusLine } from "./lifecycle";
import {
  emptyBudget,
  STREAM_MESSAGE_CHARS,
  type StreamChunk,
  streamHeader,
  streamLink,
  streamPages,
  streamStart,
  streamStopTitle,
  streamUpdate,
  suggestedPrompts,
} from "./stream";

describe("streamUpdate", () => {
  it("puts a new card where its step came in the words, and sends only the text delta", () => {
    const a: Turn = { ...emptyTurn("t", { name: "A" }), text: "Hello" };
    const b = reduce(reduce(a, { type: "text.delta", text: " world." }), {
      type: "task.upserted",
      task: { id: "x", title: "Searching", status: "in_progress" },
    });
    const { chunks, rewritten } = streamUpdate(a, b);
    expect(rewritten).toBe(false);
    // The step began after " world." was said, so its card goes after it, not before.
    expect(chunks).toEqual([
      { type: "markdown_text", text: " world." },
      { type: "task_update", id: "x", title: "Searching", status: "in_progress" },
    ]);
    // What comes after the step starts a paragraph below its card.
    const c = reduce(b, { type: "text.delta", text: "Found it." });
    expect(streamUpdate(b, c).chunks).toEqual([{ type: "markdown_text", text: "\n\nFound it." }]);
  });

  it("sends updates to cards already shown before the words that follow them", () => {
    const task = { id: "x", title: "Searching", status: "in_progress" as const, at: 5 };
    const a: Turn = { ...emptyTurn("t", { name: "A" }), text: "Hello", tasks: [task] };
    const b: Turn = { ...a, text: "Hello. Done", tasks: [{ ...task, status: "complete" }] };
    expect(streamUpdate(a, b).chunks[0]).toMatchObject({ type: "task_update", status: "complete" });
  });

  it("keeps a sentence whole above a call when the stream had sent only its first word", () => {
    const said = " bring in @researcher to look it up on the web.";
    const a: Turn = { ...emptyTurn("t", { name: "A" }), text: "I'll" };
    const call = {
      id: "c",
      title: "researcher",
      status: "in_progress" as const,
      subagent: { name: "researcher" },
    };
    const b = reduce(reduce(a, { type: "text.delta", text: said }), {
      type: "task.upserted",
      task: call,
    });
    const c = reduce(reduce(b, { type: "task.upserted", task: { ...call, status: "complete" } }), {
      type: "text.delta",
      text: "Here are three changes.",
    });
    const first = streamUpdate(a, b).chunks;
    expect(first).toEqual([
      { type: "markdown_text", text: said },
      { type: "task_update", id: "c", title: "researcher", status: "in_progress" },
    ]);
    expect(streamUpdate(b, c).chunks).toEqual([
      { type: "task_update", id: "c", title: "researcher", status: "complete" },
      { type: "markdown_text", text: "\n\nHere are three changes." },
    ]);
  });

  it("names another agent's step once, with its own title when it has one", () => {
    const turn = (title: string): Turn => ({
      ...emptyTurn("t", { name: "A" }),
      tasks: [{ id: "c", title, status: "complete", subagent: { name: "researcher" } }],
    });
    const title = (t: Turn) => (streamUpdate(null, t).chunks[0] as { title: string }).title;
    expect(title(turn("researcher"))).toBe("researcher");
    expect(title(turn("Researcher"))).toBe("researcher");
    expect(title(turn(""))).toBe("researcher");
    expect(title(turn("Looked up Go releases"))).toBe("researcher: Looked up Go releases");
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

  it("has a status line only while processing, and nothing to clear it with afterwards", () => {
    expect(lifecycle(fx.waitingTurn).statusText).toBeUndefined();
    expect(lifecycle(fx.doneTurn).statusText).toBeUndefined();
    expect(lifecycle(fx.failedTurn).statusText).toBeUndefined();
  });

  it("names the running task, else the plan, else says it is thinking", () => {
    const working: Turn = { ...fx.streamingTurn, tasks: [], planTitle: "Testing Plaud alerts" };
    expect(lifecycle(working).statusText).toBe("Testing Plaud alerts");
    expect(lifecycle({ ...working, planTitle: undefined }).statusText).toBe("Thinking");
    expect(lifecycle({ ...working, planTitle: "  " }).statusText).toBe("Thinking");
  });

  it("keeps the status line to one short line", () => {
    expect(statusLine("Checking\n  recent   deploys ")).toBe("Checking recent deploys");
    const long = statusLine(`Reading ${"the incident timeline ".repeat(10)}`);
    expect(long.length).toBeLessThanOrEqual(STATUS_TEXT_CHARS);
    expect(long.endsWith("…")).toBe(true);
    expect(long).not.toMatch(/\s…$/);
    const title = "x".repeat(STATUS_TEXT_CHARS);
    expect(statusLine(title)).toBe(title);
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

describe("streamLink", () => {
  const session = "https://example.com/sessions/turn-1";
  const sent = (turn: Turn) => streamPages(emptyBudget(), streamUpdate(null, turn).chunks).budget;

  it("ends a finished answer's last line with the arrow, as text and not on a line of its own", () => {
    const turn: Turn = { ...fx.doneTurn, citations: [], text: "Rolled back. The rate is normal." };
    expect(streamLink(turn, sent(turn))).toEqual({
      type: "markdown_text",
      text: ` [\u2197\uFE0E](${session})`,
    });
  });

  it("leaves it to the final blocks when the words cannot take it", () => {
    const at = (patch: Partial<Turn>) => {
      const turn: Turn = { ...fx.doneTurn, citations: [], ...patch };
      return streamLink(turn, sent(turn));
    };
    expect(at({ text: "Run:\n\n```sh\nmake\n```" })).toBeNull();
    expect(at({ text: "| a |\n| --- |\n| 1 |" })).toBeNull();
    expect(at({ text: "Cut short", status: "failed" })).toBeNull();
    expect(at({ text: "Cut short", status: "stopped" })).toBeNull();
    expect(at({ text: "" })).toBeNull();
    expect(at({ text: "No session", sessionUrl: undefined })).toBeNull();
  });

  it("does not put the arrow under a task card that came after the words", () => {
    const turn: Turn = { ...fx.doneTurn, citations: [], tasks: [], text: "Words first." };
    const words = sent(turn);
    const card = streamPages(words, [
      { type: "task_update", id: "late", title: "Checked", status: "complete" },
    ]).budget;
    expect(streamLink(turn, words)).not.toBeNull();
    expect(streamLink(turn, card)).toBeNull();
    // An update to a card already above the words leaves them last.
    const early = sent({ ...turn, tasks: [{ id: "k", title: "Read", status: "in_progress" }] });
    const updated = streamPages(early, [
      { type: "task_update", id: "k", title: "Read", status: "complete" },
    ]).budget;
    expect(streamLink(turn, updated)).not.toBeNull();
  });

  it("is left out when it would push a full message over into a new one", () => {
    const turn: Turn = { ...fx.doneTurn, citations: [], tasks: [] };
    const full = { ...emptyBudget(), text: "x".repeat(STREAM_MESSAGE_CHARS - 5), textLast: true };
    expect(streamLink(turn, full)).toBeNull();
  });
});
