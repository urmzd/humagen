import {
  emptyQueue,
  type QueueAction,
  type QueuedMessage,
  type QueueMode,
  type QueueState,
  queueReduce,
} from "@humagen/core";
import { useCallback, useMemo, useRef, useState } from "react";

let seq = 0;
function message(text: string): QueuedMessage {
  seq += 1;
  return { id: `q${Date.now().toString(36)}${seq}`, text, queuedAt: new Date().toISOString() };
}

export interface QueueControls {
  state: QueueState;
  dispatch: (action: QueueAction) => void;
  /** Start now if idle, else queue behind the running turn. */
  send: (text: string) => void;
  /** Interrupt the running turn and run this next. */
  sendNow: (text: string) => void;
  promote: (id: string) => void;
  edit: (id: string, text: string) => void;
  remove: (id: string) => void;
  move: (id: string, to: number) => void;
  clear: () => void;
  stop: () => void;
  /** Tell the queue the running turn ended; it may start the next message. */
  turnEnded: (outcome: "done" | "failed" | "stopped") => void;
  /** Retain a post that the server did not accept, for editing and explicit retry. */
  reject: () => void;
  resume: () => void;
  /** Take a reply already in flight as the running turn, so messages queue behind it. */
  adopt: (text: string) => void;
  setMode: (mode: QueueMode) => void;
  /** A turn is running or stopping. */
  running: boolean;
}

/**
 * The composer's message queue: `queueReduce` from core plus its effects.
 * `onStart` runs a message (begin a turn); `onInterrupt` asks the runtime to
 * stop the running turn. Call `turnEnded` when a turn settles.
 */
export function useQueue({
  mode = "auto",
  onStart,
  onInterrupt,
}: {
  mode?: QueueMode;
  onStart: (message: QueuedMessage) => void;
  onInterrupt: () => void;
}): QueueControls {
  const [state, setState] = useState(() => emptyQueue(mode));
  const current = useRef(state);
  const handlers = useRef({ onStart, onInterrupt });
  handlers.current = { onStart, onInterrupt };

  // Transitions run against the ref, not React's queued state, so an effect
  // that synchronously ends a turn still sees the state it produced.
  const dispatch = useCallback((action: QueueAction) => {
    const [next, effect] = queueReduce(current.current, action);
    current.current = next;
    setState(next);
    if (effect.type === "start") handlers.current.onStart(effect.message);
    else if (effect.type === "interrupt") handlers.current.onInterrupt();
  }, []);

  return useMemo(
    () => ({
      state,
      dispatch,
      send: (text) => dispatch({ type: "send", message: message(text) }),
      sendNow: (text) => dispatch({ type: "sendNow", message: message(text) }),
      promote: (id) => dispatch({ type: "promote", id }),
      edit: (id, text) => dispatch({ type: "edit", id, text }),
      remove: (id) => dispatch({ type: "remove", id }),
      move: (id, to) => dispatch({ type: "move", id, to }),
      clear: () => dispatch({ type: "clear" }),
      stop: () => dispatch({ type: "stop" }),
      turnEnded: (outcome) => dispatch({ type: "turnEnded", outcome }),
      reject: () => dispatch({ type: "rejected" }),
      resume: () => dispatch({ type: "resume" }),
      adopt: (text) => dispatch({ type: "adopt", message: message(text) }),
      setMode: (m) => dispatch({ type: "setMode", mode: m }),
      running: state.status === "running" || state.status === "stopping",
    }),
    [state, dispatch],
  );
}
