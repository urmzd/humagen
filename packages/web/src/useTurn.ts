import { emptyTurn, reduce, type Turn, type TurnEvent } from "@agent-ui/core";
import { useCallback, useEffect, useReducer, useRef } from "react";

/**
 * Live turn state for the web. Feed it events from any transport (SSE,
 * WebSocket, saige stream) with `push`; the turn re-renders as tokens arrive.
 */
export function useTurn(seed: Turn = emptyTurn("", { name: "" })) {
  const [turn, dispatch] = useReducer(reduce, seed);
  return { turn, push: dispatch };
}

/** Replay a recorded event list on a timer. For stories, demos, and tests. */
export function useReplay(events: TurnEvent[], { intervalMs = 60, loop = false } = {}) {
  const { turn, push } = useTurn();
  const index = useRef(0);
  const restart = useCallback(() => {
    index.current = 0;
  }, []);

  useEffect(() => {
    index.current = 0;
    const timer = setInterval(() => {
      const event = events[index.current];
      if (event) {
        push(event);
        index.current += 1;
      } else if (loop) {
        index.current = 0;
      }
    }, intervalMs);
    return () => clearInterval(timer);
  }, [events, intervalMs, loop, push]);

  return { turn, restart };
}

/** Split text into word-sized deltas, the way a model streams tokens. */
export function tokenize(text: string): TurnEvent[] {
  return (text.match(/\S+\s*|\s+/g) ?? []).map((t) => ({ type: "text.delta", text: t }));
}
