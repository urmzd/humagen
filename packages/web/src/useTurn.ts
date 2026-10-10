import { emptyTurn, reduce, type Turn, type TurnEvent } from "@vultus/core";
import { useCallback, useEffect, useReducer, useRef, useState } from "react";

type Input = TurnEvent | { type: "@reset"; seed: Turn };

/**
 * Live turn state for the web. Feed it events from any transport (SSE,
 * WebSocket, saige stream) with `push`; the turn re-renders as tokens arrive.
 * `reset` starts over from the seed (or a new one).
 */
export function useTurn(initial?: Turn) {
  const [seed] = useState(() => initial ?? emptyTurn("", { name: "" }));
  const [turn, dispatch] = useReducer(
    (t: Turn, e: Input) => (e.type === "@reset" ? e.seed : reduce(t, e)),
    seed,
  );
  const push = useCallback((event: TurnEvent) => dispatch(event), []);
  const reset = useCallback(
    (next: Turn = seed) => dispatch({ type: "@reset", seed: next }),
    [seed],
  );
  return { turn, push, reset };
}

/**
 * Replay a recorded event list on a timer. For stories, demos, and tests.
 *
 * Plays on mount unless `autoplay` is false; `play` restarts from the first
 * event, `stop` halts and ends the turn the way a user Stop does, and `push`
 * folds in live events (an approval click, feedback) alongside the recording.
 */
export function useReplay(
  events: TurnEvent[],
  {
    intervalMs = 60,
    loop = false,
    autoplay = true,
    seed,
  }: { intervalMs?: number; loop?: boolean; autoplay?: boolean; seed?: Turn } = {},
) {
  const { turn, push, reset } = useTurn(seed);
  const index = useRef(0);
  const [playing, setPlaying] = useState(autoplay);

  useEffect(() => {
    if (!playing) return;
    const timer = setInterval(() => {
      const event = events[index.current];
      if (event) {
        push(event);
        index.current += 1;
      } else if (loop) {
        index.current = 0;
        reset();
      } else {
        setPlaying(false);
      }
    }, intervalMs);
    return () => clearInterval(timer);
  }, [events, intervalMs, loop, push, reset, playing]);

  const play = useCallback(() => {
    index.current = 0;
    reset();
    setPlaying(true);
  }, [reset]);

  const stop = useCallback(() => {
    setPlaying(false);
    push({ type: "turn.stopped" });
  }, [push]);

  return { turn, push, playing, play, stop, restart: play };
}

/** Split text into word-sized deltas, the way a model streams tokens. */
export function tokenize(
  text: string,
  type: "text.delta" | "reasoning.delta" = "text.delta",
): TurnEvent[] {
  return (text.match(/\S+\s*|\s+/g) ?? []).map((t) => ({ type, text: t }));
}
