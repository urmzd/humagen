import type { Turn, TurnStatus } from "@warp/core";

/**
 * Signals that live outside the message on Slack: a reaction on the user's
 * message and the agent session status. Pure mapping; the host calls
 * reactions.add/remove and agents.sessions.setStatus.
 *
 * Each emoji has exactly one meaning, and words always back it up (the reply
 * and the session status), so color and emoji never carry state alone.
 */

export const REACTIONS = {
  /** Seen it, working. Added immediately, removed when the reply lands. */
  acknowledged: "eyes",
  /** Paused on a human: an approval or a question is waiting. */
  needsYou: "raised_hand",
  done: "white_check_mark",
  failed: "x",
  stopped: "black_square_for_stop",
} as const;

export type SessionStatus = "processing" | "active" | "suspended" | "closed";

export interface SlackLifecycle {
  /** Reaction to show on the triggering message, or null for none. */
  reaction: string | null;
  /** agents.sessions.setStatus. "suspended" while a human must act. */
  sessionStatus: SessionStatus;
  /** Short status line while processing, e.g. "Checking recent deploys". */
  statusText?: string;
}

const REACTION_BY_STATUS: Record<TurnStatus, string | null> = {
  queued: REACTIONS.acknowledged,
  working: REACTIONS.acknowledged,
  waiting: REACTIONS.needsYou,
  done: REACTIONS.done,
  failed: REACTIONS.failed,
  stopped: REACTIONS.stopped,
};

const SESSION_BY_STATUS: Record<TurnStatus, SessionStatus> = {
  queued: "processing",
  working: "processing",
  waiting: "suspended",
  // Always clear "processing" when the run ends, even on failure.
  done: "active",
  failed: "active",
  stopped: "active",
};

export function lifecycle(turn: Turn): SlackLifecycle {
  const running = turn.tasks.findLast((t) => t.status === "in_progress");
  return {
    reaction: REACTION_BY_STATUS[turn.status],
    sessionStatus: SESSION_BY_STATUS[turn.status],
    ...(SESSION_BY_STATUS[turn.status] === "processing"
      ? { statusText: running?.title ?? turn.planTitle ?? "Thinking" }
      : {}),
  };
}

/** Reaction changes for a status transition: at most one reaction is ever shown. */
export function reactionChanges(
  prev: TurnStatus | null,
  next: TurnStatus,
): { add: string[]; remove: string[] } {
  const before = prev ? REACTION_BY_STATUS[prev] : null;
  const after = REACTION_BY_STATUS[next];
  if (before === after) return { add: [], remove: [] };
  return { add: after ? [after] : [], remove: before ? [before] : [] };
}
