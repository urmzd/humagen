/*
 * Where a reader is in a list that grows at its end, and what the control
 * that takes them back to the end says about it. No DOM: the list measures
 * itself (useLatest) and reports here.
 */

export type LatestState = {
  /** The end of the list is on view. */
  atEnd: boolean;
  /** Something arrived at the end while it was off view, and has not been seen. */
  unseen: boolean;
};

export type LatestEvent =
  /** The reader scrolled, or the list was measured again. */
  | { type: "moved"; atEnd: boolean }
  /** The list grew at its end. */
  | { type: "grew" };

export const latestStart: LatestState = { atEnd: true, unseen: false };

export function latestReduce(state: LatestState, event: LatestEvent): LatestState {
  if (event.type === "moved") {
    if (event.atEnd) return state.atEnd && !state.unseen ? state : latestStart;
    return state.atEnd ? { ...state, atEnd: false } : state;
  }
  // What grows in view is seen as it arrives.
  return state.atEnd || state.unseen ? state : { ...state, unseen: true };
}

/** What the control shows. `plain` is an arrow and nothing else. */
export type LatestLook = "working" | "unseen" | "plain";

/**
 * The control that returns to the end: nothing while the end is on view,
 * else an arrow. While an answer is still being written below it shows
 * that work is under way; once that ends it is a plain arrow again, with a
 * mark if what arrived has not been seen.
 */
export function latestLook(state: LatestState, working: boolean): LatestLook | null {
  if (state.atEnd) return null;
  if (working) return "working";
  return state.unseen ? "unseen" : "plain";
}

/**
 * Whether the list keeps its end on view as it grows. Following stops when
 * the reader leaves the end and starts again only when they are back at it,
 * by scrolling there or with the control, never because an answer ended.
 */
export function follows(state: LatestState): boolean {
  return state.atEnd;
}
