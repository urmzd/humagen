import { describe, expect, it } from "vitest";
import {
  emptyQueue,
  type QueueAction,
  type QueueEffect,
  type QueueState,
  queueReduce,
} from "./queue";

const msg = (id: string) => ({ id, text: id, queuedAt: id });

function run(actions: QueueAction[], start: QueueState = emptyQueue()) {
  const effects: QueueEffect[] = [];
  const state = actions.reduce((s, a) => {
    const [next, effect] = queueReduce(s, a);
    effects.push(effect);
    return next;
  }, start);
  return { state, effects };
}

describe("queue", () => {
  it("keeps a rejected post editable ahead of later messages without retrying", () => {
    const { state, effects } = run([
      { type: "send", message: msg("a") },
      { type: "send", message: msg("b") },
      { type: "rejected" },
    ]);
    expect(state).toMatchObject({ status: "held", current: null, heldReason: "failed", pending: [msg("a"), msg("b")] });
    expect(effects[2]).toEqual({ type: "none" });
    const edited = queueReduce(state, { type: "edit", id: "a", text: "corrected" })[0];
    expect(queueReduce(edited, { type: "resume" })[1]).toEqual({ type: "start", message: { ...msg("a"), text: "corrected" } });
    expect(queueReduce(state, { type: "rejected" })[0]).toEqual(state);
  });
  it("starts immediately when idle and queues while running", () => {
    const { state, effects } = run([
      { type: "send", message: msg("a") },
      { type: "send", message: msg("b") },
    ]);
    expect(effects[0]).toEqual({ type: "start", message: msg("a") });
    expect(effects[1]).toEqual({ type: "none" });
    expect(state.pending.map((m) => m.id)).toEqual(["b"]);
  });

  it("auto mode releases the next message when a turn completes", () => {
    const { state, effects } = run([
      { type: "send", message: msg("a") },
      { type: "send", message: msg("b") },
      { type: "turnEnded", outcome: "done" },
    ]);
    expect(effects[2]).toEqual({ type: "start", message: msg("b") });
    expect(state.status).toBe("running");
  });

  it("manual mode holds until resume", () => {
    const { state } = run(
      [
        { type: "send", message: msg("a") },
        { type: "send", message: msg("b") },
        { type: "turnEnded", outcome: "done" },
      ],
      emptyQueue("manual"),
    );
    expect(state).toMatchObject({ status: "held", heldReason: "manual" });
    expect(queueReduce(state, { type: "resume" })[1]).toEqual({ type: "start", message: msg("b") });
  });

  it("never runs queued work after a failure or a plain stop", () => {
    const failed = run([
      { type: "send", message: msg("a") },
      { type: "send", message: msg("b") },
      { type: "turnEnded", outcome: "failed" },
    ]);
    expect(failed.state).toMatchObject({ status: "held", heldReason: "failed" });
    const stopped = run([
      { type: "send", message: msg("a") },
      { type: "send", message: msg("b") },
      { type: "stop" },
      { type: "turnEnded", outcome: "stopped" },
    ]);
    expect(stopped.state).toMatchObject({ status: "held", heldReason: "stopped" });
  });

  it("send now interrupts, then runs the new message ahead of the queue", () => {
    const { state, effects } = run([
      { type: "send", message: msg("a") },
      { type: "send", message: msg("b") },
      { type: "sendNow", message: msg("c") },
      { type: "turnEnded", outcome: "stopped" },
    ]);
    expect(effects[2]).toEqual({ type: "interrupt" });
    expect(effects[3]).toEqual({ type: "start", message: msg("c") });
    expect(state.pending.map((m) => m.id)).toEqual(["b"]);
  });

  it("promotes, edits, moves and removes queued messages", () => {
    const { state, effects } = run([
      { type: "send", message: msg("a") },
      { type: "send", message: msg("b") },
      { type: "send", message: msg("c") },
      { type: "move", id: "c", to: 0 },
      { type: "edit", id: "b", text: "b2" },
      { type: "promote", id: "b" },
    ]);
    expect(effects[5]).toEqual({ type: "interrupt" });
    expect(state.pending.map((m) => [m.id, m.text])).toEqual([
      ["b", "b2"],
      ["c", "c"],
    ]);
  });
});
