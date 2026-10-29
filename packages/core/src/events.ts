import { ActionId, FEEDBACK_CLEAR, type UiAction } from "./actions";
import type { Attachment, Citation, Failure, Interrupt, Notice, Rating, Task, Turn } from "./types";

/**
 * Stream events an agent runtime emits. Any producer (saige, opentag, an HTTP
 * SSE feed) maps onto these; `reduce` folds them into a Turn.
 */
export type TurnEvent =
  | {
      type: "turn.started";
      turnId: string;
      agent: Turn["agent"];
      /** Known before the first token, so the link can lead a message that is still arriving. */
      sessionUrl?: string;
      source?: Turn["source"];
      disclaimer?: string;
    }
  | { type: "outcome.updated"; outcome: NonNullable<Turn["outcome"]> }
  | { type: "text.delta"; text: string }
  | { type: "reasoning.delta"; text: string }
  | { type: "plan.updated"; title: string }
  | { type: "task.upserted"; task: Task }
  | { type: "citation.added"; citation: Citation }
  | { type: "interrupt.raised"; interrupt: Interrupt }
  | { type: "interrupt.resolved"; interrupt: Interrupt }
  | { type: "notice"; notice: Notice }
  /** `rating: null` takes the rating back. A comment or reason attaches to the rating it names. */
  | { type: "feedback"; rating: Rating | null; comment?: string; reason?: string }
  | { type: "turn.completed" }
  | { type: "attachment.upserted"; attachment: Attachment }
  | { type: "turn.failed"; failure: Failure }
  | { type: "turn.stopped" }
  | { type: "stop.updated"; stop: Turn["stop"] }
  | { type: "activity"; activity: NonNullable<Turn["activity"]> }
  | { type: "usage"; usage: NonNullable<Turn["usage"]> }
  | { type: "handoff"; handoff: NonNullable<Turn["handoff"]> };

/**
 * The event a feedback click means, or null for any other action. The rating
 * is its own event, sent on the click; the comment is a second one that only
 * adds to it, so closing the prompt can never lose the rating.
 */
export function feedbackEvent(action: UiAction): TurnEvent | null {
  const { target, text, option } = action.payload;
  if (action.id === ActionId.feedback) {
    if (target === FEEDBACK_CLEAR) return { type: "feedback", rating: null };
    if (target !== "up" && target !== "down") return null;
    // Older web builds sent the comment as `option` on the rating action itself.
    return { type: "feedback", rating: target, ...(option ? { comment: option } : {}) };
  }
  if (action.id === ActionId.feedbackComment && (target === "up" || target === "down")) {
    return {
      type: "feedback",
      rating: target,
      ...(text?.trim() ? { comment: text.trim() } : {}),
      ...(option ? { reason: option } : {}),
    };
  }
  return null;
}

export function emptyTurn(id: string, agent: Turn["agent"]): Turn {
  return {
    id,
    agent,
    status: "queued",
    text: "",
    tasks: [],
    citations: [],
    interrupts: [],
    notices: [],
  };
}

function upsert<T extends { id: string }>(items: T[], item: T): T[] {
  const i = items.findIndex((x) => x.id === item.id);
  if (i === -1) return [...items, item];
  const next = items.slice();
  next[i] = item;
  return next;
}

function hasPendingInterrupt(interrupts: Interrupt[]): boolean {
  return interrupts.some((x) =>
    x.type === "approval" ? x.decision.kind === "pending" : x.answer.kind === "pending",
  );
}

/** Pure reducer: (turn, event) -> turn. Unknown turns start from `emptyTurn`. */
export function reduce(turn: Turn, event: TurnEvent): Turn {
  switch (event.type) {
    case "turn.started":
      return {
        ...emptyTurn(event.turnId, event.agent),
        status: "working",
        ...(event.sessionUrl ? { sessionUrl: event.sessionUrl } : {}),
        ...(event.source ? { source: event.source } : {}),
        ...(event.disclaimer ? { disclaimer: event.disclaimer } : {}),
      };
    case "text.delta":
      return { ...turn, status: "working", text: turn.text + event.text };
    case "reasoning.delta": {
      // Thinking that goes on with no words or steps since is one piece.
      const thoughts = turn.thoughts ?? [];
      const last = thoughts[thoughts.length - 1];
      const at = turn.text.length;
      const after = turn.tasks.length;
      const next =
        last && last.at === at && last.after === after
          ? [...thoughts.slice(0, -1), { ...last, text: last.text + event.text }]
          : [...thoughts, { at, after, text: event.text }];
      return { ...turn, reasoning: (turn.reasoning ?? "") + event.text, thoughts: next };
    }
    case "plan.updated":
      return { ...turn, planTitle: event.title };
    case "task.upserted": {
      // A step keeps the place in the answer where it first appeared.
      const was = turn.tasks.find((t) => t.id === event.task.id);
      const at = was?.at ?? event.task.at ?? turn.text.length;
      return { ...turn, tasks: upsert(turn.tasks, { ...event.task, at }) };
    }
    case "citation.added":
      return { ...turn, citations: upsert(turn.citations, event.citation) };
    case "interrupt.raised":
    case "interrupt.resolved": {
      const interrupts = upsert(turn.interrupts, event.interrupt);
      return {
        ...turn,
        interrupts,
        status: hasPendingInterrupt(interrupts) ? "waiting" : "working",
        activity: hasPendingInterrupt(interrupts) ? "waiting" : "thinking",
      };
    }
    case "attachment.upserted":
      return { ...turn, attachments: upsert(turn.attachments ?? [], event.attachment) };
    case "notice":
      return { ...turn, notices: upsert(turn.notices, event.notice) };
    case "feedback": {
      const { feedback: _f, feedbackComment: _c, feedbackReason: _r, ...rest } = turn;
      if (event.rating === null) return rest;
      // A comment arrives after its rating; a different rating starts over without the old words.
      const same = turn.feedback === event.rating;
      const comment = event.comment ?? (same ? turn.feedbackComment : undefined);
      const reason = event.reason ?? (same ? turn.feedbackReason : undefined);
      return {
        ...rest,
        feedback: event.rating,
        ...(comment ? { feedbackComment: comment } : {}),
        ...(reason ? { feedbackReason: reason } : {}),
      };
    }
    case "outcome.updated":
      return { ...turn, outcome: event.outcome };
    case "turn.completed":
      return { ...turn, status: "done" };
    case "turn.failed":
      return { ...turn, status: "failed", failure: event.failure };
    case "turn.stopped":
      return { ...turn, status: "stopped", activity: "idle" };
    case "stop.updated":
      return { ...turn, stop: event.stop };
    case "activity":
      return { ...turn, activity: event.activity };
    case "usage":
      return { ...turn, usage: { ...turn.usage, ...event.usage } };
    case "handoff":
      return { ...turn, handoff: event.handoff };
  }
}

export function replay(events: TurnEvent[], seed: Turn = emptyTurn("", { name: "" })): Turn {
  return events.reduce(reduce, seed);
}
