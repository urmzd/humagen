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
  | { type: "text.delta"; text: string }
  | { type: "reasoning.delta"; text: string }
  | { type: "plan.updated"; title: string }
  | { type: "task.upserted"; task: Task }
  | { type: "citation.added"; citation: Citation }
  | { type: "interrupt.raised"; interrupt: Interrupt }
  | { type: "interrupt.resolved"; interrupt: Interrupt }
  | { type: "notice"; notice: Notice }
  | { type: "feedback"; rating: Rating }
  | { type: "turn.completed" }
  | { type: "attachment.upserted"; attachment: Attachment }
  | { type: "turn.failed"; failure: Failure }
  | { type: "turn.stopped" }
  | { type: "stop.updated"; stop: Turn["stop"] }
  | { type: "activity"; activity: NonNullable<Turn["activity"]> }
  | { type: "usage"; usage: NonNullable<Turn["usage"]> }
  | { type: "handoff"; handoff: NonNullable<Turn["handoff"]> };

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
    case "reasoning.delta":
      return { ...turn, reasoning: (turn.reasoning ?? "") + event.text };
    case "plan.updated":
      return { ...turn, planTitle: event.title };
    case "task.upserted":
      return { ...turn, tasks: upsert(turn.tasks, event.task) };
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
    case "feedback":
      return { ...turn, feedback: event.rating };
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
