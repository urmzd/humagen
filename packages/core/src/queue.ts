/**
 * Message queue for a conversation. People keep typing while the agent works;
 * each message either waits its turn or interrupts. One model drives the web
 * composer and the Slack host, so both behave the same.
 *
 * - send:      nothing running → start now; running → enqueue at the back.
 * - sendNow:   interrupt the running turn, then run this message next.
 * - turnEnded: release the next message (auto), or hold it (manual / after a failure).
 */

export interface QueuedMessage {
  id: string;
  text: string;
  queuedAt: string;
}

/** When a turn ends, send the next queued message automatically or wait for the user. */
export type QueueMode = "auto" | "manual";

export type QueueStatus =
  /** No turn running. */
  | "idle"
  /** A turn is running; new messages queue behind it. */
  | "running"
  /** Stop requested (Send Now or Stop); waiting for the turn to end. */
  | "stopping"
  /** Turn ended with messages left, and the queue is waiting for the user. */
  | "held";

export interface QueueState {
  status: QueueStatus;
  mode: QueueMode;
  /** Message the running turn is answering. */
  current: QueuedMessage | null;
  pending: QueuedMessage[];
  /** Why the queue is held, e.g. the last turn failed. */
  heldReason?: "manual" | "failed" | "stopped";
  /** Set by Send Now: the message to run as soon as the interrupted turn ends. */
  interruptFor?: string;
}

export type QueueAction =
  | { type: "send"; message: QueuedMessage }
  | { type: "sendNow"; message: QueuedMessage }
  /** Promote an already-queued message: interrupt and run it next. */
  | { type: "promote"; id: string }
  | { type: "edit"; id: string; text: string }
  | { type: "remove"; id: string }
  | { type: "move"; id: string; to: number }
  | { type: "clear" }
  | { type: "stop" }
  | { type: "turnEnded"; outcome: "done" | "failed" | "stopped" }
  /** The host rejected the post before accepting a turn. Retain it for explicit retry. */
  | { type: "rejected" }
  /** Release a held queue: run the next message. */
  | { type: "resume" }
  | { type: "setMode"; mode: QueueMode };

/** What the host must do after a transition. */
export type QueueEffect =
  | { type: "start"; message: QueuedMessage }
  | { type: "interrupt" }
  | { type: "none" };

export function emptyQueue(mode: QueueMode = "auto"): QueueState {
  return { status: "idle", mode, current: null, pending: [] };
}

function startNext(state: QueueState): [QueueState, QueueEffect] {
  const [next, ...rest] = state.pending;
  const base = { ...state, heldReason: undefined, interruptFor: undefined };
  if (!next) return [{ ...base, status: "idle", current: null }, { type: "none" }];
  return [
    { ...base, status: "running", current: next, pending: rest },
    { type: "start", message: next },
  ];
}

/** Pure transition: (state, action) -> [state, effect]. */
export function queueReduce(state: QueueState, action: QueueAction): [QueueState, QueueEffect] {
  const none: QueueEffect = { type: "none" };
  switch (action.type) {
    case "send":
      if (state.status === "idle") {
        return [
          { ...state, status: "running", current: action.message },
          { type: "start", message: action.message },
        ];
      }
      return [{ ...state, pending: [...state.pending, action.message] }, none];

    case "sendNow":
      if (state.status === "idle" || state.status === "held") {
        return [
          { ...state, status: "running", current: action.message, heldReason: undefined },
          { type: "start", message: action.message },
        ];
      }
      return [
        {
          ...state,
          status: "stopping",
          pending: [action.message, ...state.pending],
          interruptFor: action.message.id,
        },
        state.status === "stopping" ? none : { type: "interrupt" },
      ];

    case "promote": {
      const message = state.pending.find((m) => m.id === action.id);
      if (!message) return [state, none];
      const pending = state.pending.filter((m) => m.id !== action.id);
      return queueReduce({ ...state, pending }, { type: "sendNow", message });
    }

    case "edit":
      return [
        {
          ...state,
          pending: state.pending.map((m) => (m.id === action.id ? { ...m, text: action.text } : m)),
        },
        none,
      ];

    case "remove":
      return [{ ...state, pending: state.pending.filter((m) => m.id !== action.id) }, none];

    case "move": {
      const from = state.pending.findIndex((m) => m.id === action.id);
      if (from === -1) return [state, none];
      const pending = state.pending.slice();
      const [m] = pending.splice(from, 1);
      pending.splice(Math.max(0, Math.min(action.to, pending.length)), 0, m as QueuedMessage);
      return [{ ...state, pending }, none];
    }

    case "clear":
      return [
        { ...state, pending: [], ...(state.status === "held" ? { status: "idle" as const } : {}) },
        none,
      ];

    case "stop":
      if (state.status !== "running") return [state, none];
      return [{ ...state, status: "stopping" }, { type: "interrupt" }];

    case "rejected":
      if (!state.current) return [state, none];
      return [{ ...state, status: "held", current: null, pending: [state.current, ...state.pending], heldReason: "failed", interruptFor: undefined }, none];

    case "turnEnded": {
      const ended = { ...state, current: null };
      // Send Now: the interrupted turn ended (however it ended); run the promoted message.
      if (state.interruptFor && state.pending[0]?.id === state.interruptFor) {
        return startNext(ended);
      }
      if (ended.pending.length === 0)
        return [{ ...ended, status: "idle", heldReason: undefined }, none];
      // Never run queued work after a failure or a plain Stop without the user saying so.
      if (action.outcome === "failed")
        return [{ ...ended, status: "held", heldReason: "failed" }, none];
      if (action.outcome === "stopped")
        return [{ ...ended, status: "held", heldReason: "stopped" }, none];
      if (state.mode === "manual")
        return [{ ...ended, status: "held", heldReason: "manual" }, none];
      return startNext(ended);
    }

    case "resume":
      return state.status === "held" ? startNext(state) : [state, none];

    case "setMode":
      return [{ ...state, mode: action.mode }, none];
  }
}
