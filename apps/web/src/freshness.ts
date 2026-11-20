import type { Corpus, CorpusMember, Source } from "./api.ts";
import type { Tone } from "./cards.tsx";

/**
 * How fresh a corpus's sources are, worked out in the console from what the
 * API already returns: a source's lifecycle (its attempts, its last good sync
 * and its own refresh interval) and a member's status. Nothing here calls the
 * API. This is not the Graph tab's staleness, which is about one document's
 * owner and age.
 */

export type Freshness = "fresh" | "stale" | "syncing" | "failed" | "never";

/** A member's state, and when it last synced well, where that is known. */
export interface MemberFreshness {
  state: Freshness;
  at?: string;
}

/**
 * A source is stale once its last good sync is older than this many of its
 * own refresh intervals. Twice, so one late or skipped sync is not called
 * stale, and exactly twice the interval is still fresh.
 */
export const STALE_AFTER_INTERVALS = 2;

/** Worst first: the order a corpus's rollup picks from. */
const WORST_FIRST: Freshness[] = ["failed", "stale", "never", "syncing", "fresh"];

/** Failed and stale are the states that ask for a person; the rest stay quiet. */
export const freshnessTone: Record<Freshness, Tone> = {
  failed: "bad",
  stale: "warn",
  never: "neutral",
  syncing: "neutral",
  fresh: "neutral",
};

/**
 * A source's freshness from its lifecycle, or null where syncing does not
 * apply: a source with no lifecycle, and one searched live at its origin,
 * which keeps no copy to go stale.
 *
 * An attempt in flight is syncing. Otherwise a failed last attempt, or a
 * source that needs attention, is failed. Otherwise a source with no good
 * sync is never synced, one whose good sync is older than twice its refresh
 * interval is stale, and anything else is fresh. A source with no refresh
 * interval is fresh once it has synced.
 */
export function sourceFreshness(
  lifecycle: Source["lifecycle"],
  now: number = Date.now(),
): MemberFreshness | null {
  if (!lifecycle || lifecycle.mode === "live") return null;
  const at = lifecycle.last_good_at;
  const last = lifecycle.attempts[lifecycle.attempts.length - 1];
  const working = ["importing", "indexing", "syncing"];
  if ((last && !last.finished_at) || working.includes(lifecycle.status))
    return { state: "syncing", at };
  if (last?.status === "failed" || lifecycle.status === "needs_attention")
    return { state: "failed", at };
  if (!at) return { state: "never" };
  const every = (lifecycle.sync_every_seconds ?? 0) * 1000;
  if (every > 0 && now - Date.parse(at) > STALE_AFTER_INTERVALS * every)
    return { state: "stale", at };
  return { state: "fresh", at };
}

/**
 * A member's freshness, or null where the console cannot tell more than the
 * member's own status says. A source the reader has loaded is judged by its
 * lifecycle. A warehouse, or a source not loaded, has no sync time here, so
 * only its failing or syncing is known.
 */
export function memberFreshness(
  member: CorpusMember,
  sources: Source[],
  now: number = Date.now(),
): MemberFreshness | null {
  if (member.status === "hidden" || member.status === "missing") return null;
  const source = member.kind === "source" ? sources.find((s) => s.name === member.name) : undefined;
  const known = source ? sourceFreshness(source.lifecycle, now) : null;
  if (known) return known;
  return member.status === "failed" || member.status === "syncing"
    ? { state: member.status }
    : null;
}

/** The worst of some states, or null when there are none. */
export function worstFreshness(states: Freshness[]): Freshness | null {
  return WORST_FIRST.find((s) => states.includes(s)) ?? null;
}

/** A corpus's rollup: the worst state among the members whose freshness is known. */
export function corpusFreshness(
  corpus: Corpus,
  sources: Source[],
  now: number = Date.now(),
): Freshness | null {
  return worstFreshness(
    corpus.members.flatMap((m) => {
      const f = memberFreshness(m, sources, now);
      return f ? [f.state] : [];
    }),
  );
}
