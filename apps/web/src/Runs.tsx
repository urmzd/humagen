import { endedRunTurn, replay as fold, settled } from "@humagen/adapter";
import { emptyTurn, reduce, type Turn } from "@humagen/core";
import { useEffect, useReducer, useRef } from "react";
import { adapter, type Run, type RunEvent, turnOf } from "./adapter.ts";
import { api, subscribe } from "./api.ts";
import { followSession } from "./sessionStream.ts";
import { hueOf, isEnded } from "./ui.tsx";

type Action = { type: "seed"; turn: Turn } | { type: "open" } | { type: "event"; event: RunEvent };

/**
 * One run's live turn. It starts from the run record, then the stream takes
 * over: every connect replays the run from its first event, so the turn is
 * rebuilt from empty on each open rather than appended to.
 */
/** An adapter that colors a call to another agent as that agent. */
const make = () => adapter({ hue: hueOf, writing: true });

/** A finished run's turn, rebuilt from the events the journal still holds. Undefined when it holds none. */
export function replay(run: Run, events: RunEvent[]): Turn | undefined {
  return fold(run, events, { hue: hueOf });
}

export function useRunTurn(run: Run | undefined): Turn | undefined {
  const [state, dispatch] = useReducer(
    (s: { turn?: Turn; to: ReturnType<typeof adapter>; fresh: boolean }, a: Action) => {
      switch (a.type) {
        case "seed":
          return { turn: a.turn, to: make(), fresh: false };
        case "open":
          return { ...s, to: make(), fresh: true };
        case "event": {
          const base = s.fresh
            ? emptyTurn(a.event.run ?? "", { name: a.event.agent ?? "" })
            : s.turn;
          if (!base) return s;
          return { ...s, fresh: false, turn: s.to(a.event).reduce(reduce, base) };
        }
      }
    },
    { to: make(), fresh: true },
  );

  const latest = useRef(run);
  latest.current = run;
  const id = run?.id;
  const agent = run?.agent;
  // biome-ignore lint/correctness/useExhaustiveDependencies: reseed only when the selected run changes, not on every poll of its record
  useEffect(() => {
    if (!run || !id || !agent) return;
    dispatch({ type: "seed", turn: turnOf(run) });
    // A run that has ended is read from the journal, which still has its reasoning
    // and tool calls after the stream has let them go, in a session as anywhere
    // else: without them a reloaded answer loses its steps and runs what it said
    // before each one into its first line. Its record stands in until then, and
    // stays if the journal no longer holds the run whole.
    if (isEnded(run)) {
      let on = true;
      endedRunTurn(run, api.runEvents, { hue: hueOf })
        .then((turn) => {
          if (on && turn) dispatch({ type: "seed", turn });
        })
        .catch(() => {});
      return () => {
        on = false;
      };
    }
    if (run.thread?.startsWith("s_"))
      return followSession(run.thread, {
        run: (event) => {
          if (event.run === id) dispatch({ type: "event", event });
        },
        reset: (session) => {
          const authoritative =
            session.messages.find((message) => message.run?.id === id)?.run ?? latest.current;
          if (authoritative) dispatch({ type: "seed", turn: turnOf(authoritative) });
        },
      });
    return subscribe(
      { run: id },
      {
        onOpen: () => dispatch({ type: "open" }),
        onEvent: (event) => dispatch({ type: "event", event }),
      },
    );
  }, [id, agent]);

  if (!run || !state.turn) return undefined;
  const recorded = turnOf(run);
  const turn = settled(state.turn, run).reduce(reduce, state.turn);
  const citations = new Map(turn.citations.map((citation) => [citation.id, citation]));
  for (const citation of recorded.citations) citations.set(citation.id, citation);
  return {
    ...turn,
    citations: [...citations.values()],
    ...(recorded.outcome ? { outcome: recorded.outcome } : {}),
    // The record, not the stream, says which connection the run needed.
    ...(recorded.needsConnection ? { needsConnection: recorded.needsConnection } : {}),
  };
}

type Show = "all" | "live" | "failed" | "done";
