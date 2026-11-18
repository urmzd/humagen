import type { Run, RunEvent } from "./adapter.ts";

/**
 * Whether a list of runs already shows what a run event reports.
 *
 * The workspace's stream replays what the bus still holds each time it
 * connects, so a console that has just read the list is sent events the list
 * already reflects. A run's `updated_at` is the time of its last state change,
 * which is the `time` of the event that reported it: an event no later than
 * the run the list holds is old news, and reading the list again for it would
 * fetch the same answer. An event for a run the list does not hold, or later
 * than the run it holds, is news.
 */
export function reflects(
  runs: readonly Pick<Run, "id" | "updated_at">[] | null,
  event: Pick<RunEvent, "run" | "time">,
): boolean {
  if (!runs || !event.run) return false;
  const held = runs.find((r) => r.id === event.run);
  if (!held) return false;
  const seen = instant(held.updated_at);
  const at = instant(event.time);
  if (!seen || !at) return false;
  return at.second < seen.second || (at.second === seen.second && at.fraction <= seen.fraction);
}

/**
 * A time as its whole second and the digits after it, to the nanosecond.
 *
 * The server writes times to the nanosecond and `Date.parse` keeps three
 * digits, so two state changes inside one millisecond would read as the same
 * moment and the later one as old news. The second is compared as a number
 * and the fraction as nine digits, so nothing the server wrote is dropped.
 */
function instant(text: string): { second: number; fraction: string } | null {
  const m = /^(.*?)(?:\.(\d+))?(Z|[+-]\d\d:?\d\d)$/.exec(text.trim());
  if (!m) return null;
  const second = Date.parse(`${m[1]}${m[3]}`);
  if (!Number.isFinite(second)) return null;
  return { second, fraction: (m[2] ?? "").padEnd(9, "0").slice(0, 9) };
}
