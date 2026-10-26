import { emptyTurn, reduce, type Turn } from "@humagen/core";
import { useEffect, useReducer } from "react";
import { replay as fold, settled } from "@humagen/adapter";
import { adapter, type BusEvent, type Run, turnOf } from "./adapter.ts";
import { api, subscribe } from "./api.ts";
import { hueOf, isEnded } from "./ui.tsx";

type Action = { type: "seed"; turn: Turn } | { type: "open" } | { type: "event"; event: BusEvent };

/**
 * One run's live turn. It starts from the run record, then the stream takes
 * over: every connect replays the run from its first event, so the turn is
 * rebuilt from empty on each open rather than appended to.
 */
/** An adapter that colors a call to another agent as that agent. */
const make = () => adapter({ hue: hueOf });

/** A finished run's turn, rebuilt from the events the journal still holds. Undefined when it holds none. */
export function replay(run: Run, events: BusEvent[]): Turn | undefined {
  return fold(run, events, { hue: hueOf });
}

export function useRunTurn(run: Run | undefined): Turn | undefined {
  const [state, dispatch] = useReducer(
    (s: { turn?: Turn; to: ReturnType<typeof adapter>; fresh: boolean }, a: Action) => {
      switch (a.type) {
        case "seed":
          return { turn: a.turn, to: make(), fresh: true };
        case "open":
          return { ...s, to: make(), fresh: true };
        case "event": {
          const base = s.fresh ? emptyTurn(a.event.run_id, { name: a.event.agent }) : s.turn;
          if (!base) return s;
          return { ...s, fresh: false, turn: s.to(a.event).reduce(reduce, base) };
        }
      }
    },
    { to: make(), fresh: true },
  );

  const id = run?.id;
  const agent = run?.agent;
  // biome-ignore lint/correctness/useExhaustiveDependencies: reseed only when the selected run changes, not on every poll of its record
  useEffect(() => {
    if (!run || !id || !agent) return;
    dispatch({ type: "seed", turn: turnOf(run) });
    // A run that has ended is read from the journal, which still has its reasoning
    // and tool calls after the stream has let them go. Its record stands in until
    // then, and stays if the journal holds nothing.
    if (isEnded(run)) {
      let on = true;
      api
        .runEvents(id)
        .then(({ events }) => {
          const turn = replay(run, events);
          if (on && turn) dispatch({ type: "seed", turn });
        })
        .catch(() => {});
      return () => {
        on = false;
      };
    }
    return subscribe(`agent:${agent}:${id}`, {
      onOpen: () => dispatch({ type: "open" }),
      onEvent: (event) => dispatch({ type: "event", event }),
    });
  }, [id, agent]);

  if (!run || !state.turn) return undefined;
  const recorded = turnOf(run);
  const turn = settled(state.turn, run).reduce(reduce, state.turn);
  const citations = new Map(turn.citations.map((citation) => [citation.id,citation]));
  for (const citation of recorded.citations) citations.set(citation.id,citation);
  return { ...turn, citations: [...citations.values()], ...(recorded.outcome ? {outcome:recorded.outcome} : {}) };
}

type Show = "all" | "live" | "failed" | "done";
