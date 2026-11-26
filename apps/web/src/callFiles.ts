import { replay } from "@humagen/adapter";
import type { Attachment, ResponseTool } from "@humagen/core";
import { useEffect, useState } from "react";
import type { RunEvent } from "./adapter.ts";
import { api } from "./api.ts";

const PUBLISH = "publish_artifact";

/** Reads a run's events, as `api.runEvents` does. */
export type CallEvents = (run: string) => Promise<{ events: RunEvent[]; complete?: boolean }>;

/** How many publishes a called agent has finished, as its own steps say. */
export function publishesOf(call: ResponseTool | undefined): number {
  if (!call) return 0;
  return call.content.filter(
    (b) => b.type === "tool" && !b.hidden && b.name === PUBLISH && b.status !== "running",
  ).length;
}

/**
 * The files a called agent published, read from its own run's events: the
 * same events a top-level answer's cards are built from. A call's record
 * keeps each step's words, not the file a publish made, so without this a
 * called agent's message names a card it does not show.
 */
export async function callFiles(
  run: string,
  agent: string,
  load: CallEvents = api.runEvents,
): Promise<Attachment[]> {
  const { events } = await load(run);
  return replay({ id: run, agent }, events)?.attachments ?? [];
}

// A finished publish does not change, so each count is read once per page load.
const read = new Map<string, Promise<Attachment[]>>();

/** A called agent's published files, read again whenever it finishes another publish. */
export function useCallFiles(
  call: ResponseTool | undefined,
  load: CallEvents = api.runEvents,
): Attachment[] | undefined {
  const run = call?.runId;
  const agent = call?.agent ?? call?.name ?? "";
  const count = publishesOf(call);
  const [files, setFiles] = useState<Attachment[]>();
  useEffect(() => {
    if (!run || count === 0) {
      setFiles(undefined);
      return;
    }
    let live = true;
    const key = `${run}:${count}`;
    const cached = load === api.runEvents ? read.get(key) : undefined;
    const reading = cached ?? callFiles(run, agent, load);
    if (load === api.runEvents && !cached) {
      read.set(key, reading);
      // A failed read is tried again the next time the message is drawn.
      reading.catch(() => read.delete(key));
    }
    reading.then(
      (found) => {
        if (live) setFiles(found.length ? found : undefined);
      },
      () => {},
    );
    return () => {
      live = false;
    };
  }, [run, agent, count, load]);
  return files;
}
