import { describe, expect, it } from "vitest";
import { decodeAction, encodeAction } from "./actions";
import { replay } from "./events";
import { pendingApproval } from "./fixtures";

describe("reduce", () => {
  it("folds a stream into a turn", () => {
    const turn = replay([
      { type: "turn.started", turnId: "t", agent: { name: "A" } },
      { type: "task.upserted", task: { id: "x", title: "Search", status: "in_progress" } },
      { type: "text.delta", text: "Hello " },
      { type: "text.delta", text: "world" },
      { type: "task.upserted", task: { id: "x", title: "Search", status: "complete" } },
      { type: "turn.completed" },
    ]);
    expect(turn.text).toBe("Hello world");
    expect(turn.tasks).toEqual([{ id: "x", title: "Search", status: "complete" }]);
    expect(turn.status).toBe("done");
  });

  it("waits while an interrupt is pending and resumes once resolved", () => {
    const started = { type: "turn.started", turnId: "t", agent: { name: "A" } } as const;
    const waiting = replay([started, { type: "interrupt.raised", interrupt: pendingApproval }]);
    expect(waiting.status).toBe("waiting");

    const resumed = replay([
      started,
      { type: "interrupt.raised", interrupt: pendingApproval },
      {
        type: "interrupt.resolved",
        interrupt: { ...pendingApproval, decision: { kind: "approved", by: "D", at: "now" } },
      },
    ]);
    expect(resumed.status).toBe("working");
    expect(resumed.interrupts).toHaveLength(1);
  });
});

describe("actions", () => {
  it("round-trips payloads", () => {
    const payload = { turnId: "t", target: "a1" };
    expect(decodeAction(encodeAction(payload))).toEqual(payload);
  });

  it("rejects content-sized payloads", () => {
    expect(() => encodeAction({ turnId: "t", target: "x".repeat(3000) })).toThrow(RangeError);
  });
});
