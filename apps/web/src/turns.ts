import type { ThreadPage, ThreadTurn } from "./api.ts";

/**
 * The most pages of a conversation read to walk it from its start: 50 pages of
 * the server's 200 turns.
 */
export const MAX_PAGES = 50;

/** What a turn is, whichever read it came in: a message's id or a run's. */
const keyOf = (t: ThreadTurn) => (t.message ? `m:${t.message.id}` : `r:${t.run?.id ?? ""}`);

/**
 * Turns read at different times, as one list, oldest first. A turn is only
 * ever added after every turn before it, so what `older` holds that `newer`
 * does not comes before all of `newer`. A turn in both is `newer`'s, which was
 * read later.
 */
export function mergeTurns(older: ThreadTurn[], newer: ThreadTurn[]): ThreadTurn[] {
  const seen = new Set(newer.map(keyOf));
  return [...older.filter((t) => !seen.has(keyOf(t))), ...newer];
}

/**
 * A conversation from its start: its newest page, then each page before it
 * until there are none, or until MAX_PAGES have been read. The page returned
 * holds every turn read, oldest first, and `next` is where reading stopped,
 * null when it reached the start.
 */
export async function readFromStart(read: (before?: string) => Promise<ThreadPage>): Promise<ThreadPage> {
  const first = await read();
  let turns = first.turns;
  let next = first.next;
  for (let n = 1; next && n < MAX_PAGES; n++) {
    const page = await read(next);
    turns = mergeTurns(page.turns, turns);
    next = page.next;
  }
  return { ...first, turns, next };
}
