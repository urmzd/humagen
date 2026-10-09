import { emptyTurn, reduce, type Turn } from "@agent-ui/core";
import * as fx from "@agent-ui/core/fixtures";
import { describe, expect, it } from "vitest";
import { lifecycle, reactionChanges } from "./lifecycle";
import { streamStart, streamStopTitle, streamUpdate, suggestedPrompts } from "./stream";

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
