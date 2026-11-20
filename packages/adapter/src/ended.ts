import type { Turn } from "@humagen/core";
import { replay } from "./fold.ts";
import type { Run, RunEvent } from "./run.ts";

/** Reads a run's events from the journal, and whether its first one is still held. */
export type EventLoader = (id: string) => Promise<{ events: RunEvent[]; complete?: boolean }>;

type Options = Parameters<typeof replay>[2];

/**
 * An ended run's turn, rebuilt from the events the journal holds: its
 * reasoning and steps, each where it happened in the answer, which the run's
 * record does not keep. Undefined when the journal no longer holds the run
 * from its first event: a turn rebuilt from the middle of a run would show
 * the end of an answer as the whole of it, so the record stands instead.
 */
export async function endedRunTurn(run: Run, load: EventLoader, opts?: Options): Promise<Turn | undefined> {
  const { events, complete } = await load(run.id);
  return complete === false ? undefined : replay(run, events, opts);
}
