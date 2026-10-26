import { emptyTurn, reduce, type Turn, type TurnEvent } from "@humagen/core";
import { adapter, citationOf, type BusEvent, type Run, turnOf } from "./run.ts";

/**
 * A run as one shared UI turn, kept current. This is the state every surface
 * renders from: the console hands it to its web components and the Slack app
 * hands the same object to the Block Kit renderer.
 */
export interface Follower {
  /** The turn as it stands. */
  readonly turn: Turn;
  /** Fold one of the run's events in. */
  apply(e: BusEvent): Turn;
  /** Fold humagen events in directly, for what a surface itself knows, such as a stop it asked for. */
  push(...events: TurnEvent[]): Turn;
  /**
   * Bring the turn in line with the run's record. The ledger decides whether a
   * run is over, and events only report attempts, so a run that ended without
   * a final event (a stop, a reconciled failure) is ended here.
   */
  settle(run: Run): Turn;
}

type Options = Parameters<typeof adapter>[0];

/** Follow one run from its first event. */
export function follow(run: Pick<Run, "id" | "agent"> & Partial<Pick<Run, "thread" | "citations" | "outcome">>, opts: Options = {}): Follower {
  const to = adapter(opts);
  const sessionUrl = opts.href?.(`/conversations/${encodeURIComponent(run.thread || run.id)}`);
  let turn = emptyTurn(run.id, { name: run.agent });
  if (sessionUrl) turn = { ...turn, sessionUrl };
  if(run.citations !== undefined) turn={...turn,citations:run.citations.map(citationOf)};
  const push = (...events: TurnEvent[]) => {
    turn = events.reduce(reduce, turn);
    if (sessionUrl) turn = { ...turn, sessionUrl };
    turn = {...turn,...(!turn.outcome && run.outcome ? {outcome:{kind:run.outcome.kind,summary:run.outcome.summary,decisions:run.outcome.decisions,nextAction:run.outcome.next_action}} : {})};
    return turn;
  };
  return {
    get turn() {
      return turn;
    },
    apply: (e) => push(...to(e)),
    push,
    settle: (record) => {
      // Present lists are the reader's authoritative projection, including
      // an empty list after source access is revoked. Older APIs omit it.
      if (record.citations !== undefined)
        turn = { ...turn, citations: record.citations.map(citationOf) };
      // The record's answer is the whole answer. When the streamed text is not
      // where it begins, the stream missed or changed something: the record wins.
      if (record.status === "completed" && record.answer && !ended(turn)) {
        if (!record.answer.startsWith(turn.text)) turn = { ...turn, text: "" };
      }
      const drawn = push(...settled(turn, record));
      const metadata = turnOf(record);
      turn = {...drawn,...(record.citations !== undefined ? {citations:metadata.citations}:{}),...(metadata.outcome?{outcome:metadata.outcome}:{})};
      return turn;
    },
  };
}

/** A turn rebuilt from the events the journal still holds. Undefined when it holds none. */
export function replay(run: Pick<Run, "id" | "agent"> & Partial<Pick<Run, "thread" | "citations" | "outcome">>, events: BusEvent[], opts: Options = {}): Turn | undefined {
  if (events.length === 0) return undefined;
  const f = follow(run, opts);
  for (const e of events) f.apply(e);
  return f.turn;
}

/** Whether a turn has reached an end no event will move it from. */
export function ended(turn: Turn): boolean {
  return turn.status === "done" || turn.status === "failed" || turn.status === "stopped";
}

/** The events that take a turn to where its run's record says it ended. None while the run is still going, or once the turn agrees. */
export function settled(turn: Turn, run: Run): TurnEvent[] {
  if (ended(turn)) return [];
  // What the stream did not deliver is the rest of the record's answer: the
  // whole answer of a completed run, the partial answer of a stopped one.
  const rest: TurnEvent[] =
    run.answer?.startsWith(turn.text) && run.answer.length > turn.text.length
      ? [{ type: "text.delta", text: run.answer.slice(turn.text.length) }]
      : [];
  switch (run.status) {
    case "completed":
      return [...rest, { type: "turn.completed" }];
    case "failed": {
      const failure = turnOf(run).failure;
      return failure ? [{ type: "turn.failed", failure }] : [];
    }
    case "stopped":
      // The platform has confirmed the stop: the run can start nothing more,
      // and what it had said is its partial answer.
      return [...rest, { type: "turn.stopped" }, { type: "stop.updated", stop: "confirmed" }];
    default:
      return [];
  }
}

/**
 * What asking a turn again means to the platform: the same words, to the same
 * agent, from the same point in the conversation. A run that answers a
 * person's message in a thread follows that message, which a turn cannot
 * follow through the API, so it is asked again after the run itself, in the
 * same conversation.
 */
export function retryOf(run: Run): { agent: string; text: string; after?: string } {
  const after = run.message ? run.id : run.prev;
  return { agent: run.agent, text: run.text, ...(after ? { after } : {}) };
}
