import { type Copy, defaultCopy } from "@warp/components";
import type { Turn } from "@warp/core";
import type { ThreadAsk } from "./api.ts";

/**
 * A question a run waits on is read by everyone in its thread and answered
 * by the people it is for. The platform says which a reader is (`ask` on a
 * thread's turn), so the console offers the answer only to them, and tells
 * anyone else whom it is for.
 */

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
    case "nobody":
      return words.nobody;
    default:
      return words.anyone;
  }
}

/**
 * The turn as one reader sees it: a question that is not theirs to answer
 * shows whom it is for, and no way to answer it. A question theirs, or one
 * the platform said nothing about, is left as it is.
 */
export function askedOf(turn: Turn, ask: ThreadAsk | undefined): Turn {
  if (!ask || ask.yours) return turn;
  const forOthers = questionFor(ask, turn.agent.name);
  let changed = false;
  const interrupts = turn.interrupts.map((i) => {
    if (i.id !== ask.question || i.type !== "clarification" || i.answer.kind !== "pending") return i;
    changed = true;
    return { ...i, forOthers };
  });
  return changed ? { ...turn, interrupts } : turn;
}

/** The words a turn is drawn with for one reader: a run waiting on someone else does not say it needs them. */
const othersCopy: Copy = { ...defaultCopy, status: { ...defaultCopy.status, waiting: defaultCopy.threads.waitingOnOthers } };

export function copyFor(ask: ThreadAsk | undefined): Copy {
  return ask && !ask.yours ? othersCopy : defaultCopy;
}
