import { type Copy, defaultCopy } from "@humagen/components";
import type { Interrupt, Turn } from "@humagen/core";
import type { ThreadAsk, ThreadTurn } from "./api.ts";

/**
 * A question a run waits on is read by everyone in its thread and answered
 * by the people it is for. The platform says which a reader is (`asks` on a
 * thread's turn, one for each question the run waits on), so the console
 * offers each answer only to them, and tells anyone else whom it is for.
 */

/**
 * Every question a turn's run waits on, as the reader stands to each. A run
 * parked by a batch of tool calls waits on several at once; a server from
 * before batches sends the first alone, as `ask`.
 */
export function asksOf(turn: Pick<ThreadTurn, "ask" | "asks"> | undefined): ThreadAsk[] {
  if (!turn) return [];
  return turn.asks?.length ? turn.asks : turn.ask ? [turn.ask] : [];
}

/** True when a run waits on questions and none of them is the reader's to answer. */
export function allForOthers(asks: ThreadAsk[] | undefined): boolean {
  return !!asks?.length && asks.every((a) => !a.yours);
}

const words = defaultCopy.threads.questionFor;

/** Whom a question is for, in words, for a reader it is not for. agent is the agent that asks. */
export function questionFor(ask: ThreadAsk, agent: string): string {
  switch (ask.for) {
    case "experts":
      return words.experts(ask.topic ?? "");
    case "person":
      return words.person(ask.person ?? "");
    case "answerers":
      return words.answerers(agent);
    case "owners":
      return words.owners(ask.owners_of ?? "");
    case "nobody":
      return words.nobody;
    default:
      return words.anyone;
  }
}

/**
 * The turn as one reader sees it: each question that is not theirs to answer
 * shows whom it is for, and no way to answer it. A question theirs, or one
 * the platform said nothing about, is left as it is.
 */
export function askedOf(turn: Turn, asks: ThreadAsk[] | undefined): Turn {
  const others = (asks ?? []).filter((a) => !a.yours);
  if (others.length === 0) return turn;
  let changed = false;
  const interrupts = turn.interrupts.map((i) => {
    const ask = others.find((a) => a.question === i.id);
    if (!ask || i.type !== "clarification" || i.answer.kind !== "pending") return i;
    changed = true;
    return { ...i, forOthers: questionFor(ask, turn.agent.name) };
  });
  return changed ? { ...turn, interrupts } : turn;
}

/** The words a turn is drawn with for one reader: a run waiting on someone else does not say it needs them. */
const othersCopy: Copy = {
  ...defaultCopy,
  status: { ...defaultCopy.status, waiting: defaultCopy.threads.waitingOnOthers },
};

export function copyFor(asks: ThreadAsk[] | undefined): Copy {
  return allForOthers(asks) ? othersCopy : defaultCopy;
}

/** Why the platform did not take a reader's answer to one question of a run. */
export interface Refusal {
  question: string;
  why: string;
}

/**
 * Whether a turn still waits on the question with this id: shown, and not
 * yet answered or decided by anyone. A refusal of an answer to it is said
 * only while it does.
 */
export function stillAsks(turn: Turn, question: string): boolean {
  return turn.interrupts.some((i) => i.id === question && pending(i));
}

function pending(i: Interrupt): boolean {
  return i.type === "approval"
    ? i.decision.kind === "pending" || i.decision.kind === "asked"
    : i.answer.kind === "pending";
}
