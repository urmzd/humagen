import type { Turn, TurnStatus } from "@humagen/core";

/**
 * Signals that live outside the message on Slack: a reaction on the user's
 * message, the agent session status, and the one-line status above the reply
 * box. Pure mapping; the host calls reactions.add/remove,
 * agents.sessions.setStatus and assistant.threads.setStatus.
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
  /**
   * Short status line while processing, e.g. "Checking recent deploys", for
   * assistant.threads.setStatus. One line of at most STATUS_TEXT_CHARS.
   */
  statusText?: string;
}

/**
 * The longest status line sent. Slack shows it on one line above the reply
 * box and documents no limit of its own, so it is kept to what fits there.
 */
export const STATUS_TEXT_CHARS = 50;

/** A status line on one line, shortened with an ellipsis when it is too long. */
export function statusLine(text: string): string {
  const line = text.replace(/\s+/g, " ").trim();
  if (line.length <= STATUS_TEXT_CHARS) return line;
  return `${line.slice(0, STATUS_TEXT_CHARS - 1).trimEnd()}…`;
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
      ? { statusText: statusLine(running?.title || turn.planTitle || "Thinking") || "Thinking" }
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
