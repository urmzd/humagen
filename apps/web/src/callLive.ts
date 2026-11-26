import { emptyTurn, type ResponseTool, reduce, type Turn } from "@humagen/core";
import { useEffect, useState } from "react";
import { adapter, type RunEvent } from "./adapter.ts";
import { api } from "./api.ts";
import type { CallEvents } from "./callFiles.ts";
import { followSession } from "./sessionStream.ts";
import { hueOf } from "./ui.tsx";

/** Follows a session's run events, as `followSession` does. */
export type FollowRuns = (session: string, run: (event: RunEvent) => void) => () => void;

const followRuns: FollowRuns = (session, run) => followSession(session, { run });

/**
 * A called agent's own turn while it runs: its words as it writes them, and
 * the files it publishes. A call's record is read every few seconds and
 * holds no words until the agent has answered, so without this a called
 * agent says nothing until it is done.
 *
 * The run's events so far are read once, and each event the session's stream
 * brings after them is added. An event is taken once, by its place in the
 * run: the stream starts a run again from its first event whenever it
 * connects, and what was read may overlap what arrives.
 */
export function useCallLive(
  call: ResponseTool | undefined,
  session: string | undefined,
  load: CallEvents = api.runEvents,
  follow: FollowRuns = followRuns,
): Turn | undefined {
  const run = call?.runId;
  const agent = call?.agent ?? call?.name ?? "";
  const running = call?.status === "running";
  const [turn, setTurn] = useState<Turn>();
  useEffect(() => {
    if (!run || !running || !session?.startsWith("s_")) {
      setTurn(undefined);
      return;
    }
    let on = true;
    let at = emptyTurn(run, { name: agent });
    let last = -1;
    let read = false;
    const waiting: RunEvent[] = [];
    const to = adapter({ hue: hueOf });
    const take = (event: RunEvent) => {
      // An event that does not say its place cannot be told from a repeat, and is taken.
      if (event.seq !== undefined) {
        if (event.seq <= last) return;
        last = event.seq;
      }
      at = to(event).reduce(reduce, at);
    };
    const stop = follow(session, (event) => {
      if (event.run !== run) return;
      if (!read) {
        waiting.push(event);
        return;
      }
      take(event);
      setTurn(at);
    });
    const start = (events: RunEvent[]) => {
      if (!on) return;
      for (const event of events) take(event);
      read = true;
      for (const event of waiting) take(event);
      waiting.length = 0;
      setTurn(at);
    };
    // Without what came before, the stream alone still shows what comes next.
    load(run).then(
      ({ events }) => start(events),
      () => start([]),
    );
    return () => {
      on = false;
      stop();
    };
  }, [run, agent, running, session, load, follow]);
  return turn;
}
