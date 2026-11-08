import type { AgentSession } from "../../../packages/client/src/generated/types.ts";
import type { RunEvent } from "./adapter.ts";

type Listener = {
  session?: (session: AgentSession) => void;
  reset?: (session: AgentSession) => void;
  run?: (event: RunEvent) => void;
};
const streams = new Map<string, { source: EventSource; listeners: Set<Listener> }>();
/** One resumable EventSource per visible session, shared by its messages and panels. */
export function followSession(id: string, listener: Listener): () => void {
  let entry = streams.get(id);
  if (!entry) {
    const source = new EventSource(`/api/sessions/${encodeURIComponent(id)}/stream`);
    const listeners = new Set<Listener>();
    entry = { source, listeners };
    streams.set(id, entry);
    source.addEventListener("session", (event) => {
      try {
        const session = JSON.parse((event as MessageEvent).data) as AgentSession;
        for (const current of listeners) current.session?.(session);
      } catch {
        /* A malformed frame cannot replace a saved view. */
      }
    });
    source.addEventListener("session-reset", (event) => {
      try {
        const session = JSON.parse((event as MessageEvent).data) as AgentSession;
        for (const current of listeners) {
          current.reset?.(session);
          current.session?.(session);
        }
      } catch {
        /* Keep the previous view if a reset frame cannot be decoded. */
      }
    });
    source.addEventListener("run", (event) => {
      try {
        const run = JSON.parse((event as MessageEvent).data) as RunEvent;
        for (const current of listeners) current.run?.(run);
      } catch {
        /* The next recorded snapshot restores state. */
      }
    });
  }
  entry.listeners.add(listener);
  return () => {
    entry.listeners.delete(listener);
    if (!entry.listeners.size) {
      entry.source.close();
      streams.delete(id);
    }
  };
}
