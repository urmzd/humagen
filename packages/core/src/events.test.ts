import { describe, expect, it } from "vitest";
import { ActionId, decodeAction, encodeAction, FEEDBACK_CLEAR, type UiAction } from "./actions";
import { emptyTurn, feedbackEvent, reduce, replay } from "./events";
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

describe("feedback", () => {
  const rate = (target: string): UiAction => ({
    id: ActionId.feedback,
    payload: { turnId: "t", target },
  });
  const comment = (target: string, extra: { text?: string; option?: string }): UiAction => ({
    id: ActionId.feedbackComment,
    payload: { turnId: "t", target, ...extra },
  });
  const apply = (actions: UiAction[]) =>
    actions.reduce(
      (turn, action) => {
        const event = feedbackEvent(action);
        return event ? reduce(turn, event) : turn;
      },
      emptyTurn("t", { name: "A" }),
    );

  it("keeps the rating when the comment prompt is skipped", () => {
    // Skipping sends nothing more: the click alone is the whole record.
    const turn = apply([rate("down")]);
    expect(turn.feedback).toBe("down");
    expect(turn.feedbackComment).toBeUndefined();
  });

  it("attaches a comment and a reason to the rating already sent", () => {
    const turn = apply([
      rate("down"),
      comment("down", { text: " Wrong deploy ", option: "wrong" }),
    ]);
    expect(turn).toMatchObject({
      feedback: "down",
      feedbackComment: "Wrong deploy",
      feedbackReason: "wrong",
    });
  });

  it("starts over when the rating changes, and clears when it is taken back", () => {
    const changed = apply([rate("down"), comment("down", { text: "Too slow" }), rate("up")]);
    expect(changed.feedback).toBe("up");
    expect(changed.feedbackComment).toBeUndefined();
    const cleared = apply([rate("up"), comment("up", { text: "Clear" }), rate(FEEDBACK_CLEAR)]);
    expect(cleared.feedback).toBeUndefined();
    expect(cleared.feedbackComment).toBeUndefined();
  });

  it("ignores actions that aren't feedback", () => {
    expect(
      feedbackEvent({ id: ActionId.retry, payload: { turnId: "t", target: "up" } }),
    ).toBeNull();
    expect(feedbackEvent(rate("sideways"))).toBeNull();
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
