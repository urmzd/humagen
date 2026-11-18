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
  const seen = Date.parse(held.updated_at);
  const at = Date.parse(event.time);
  return Number.isFinite(seen) && Number.isFinite(at) && at <= seen;
}
