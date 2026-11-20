import type { AgentSession } from "../../../packages/client/src/generated/types.ts";
import type { RunEvent } from "./adapter.ts";
import { openStream } from "./stream.ts";

type Listener = {
  session?: (session: AgentSession) => void;
  reset?: (session: AgentSession) => void;
  run?: (event: RunEvent) => void;
};
const streams = new Map<string, { stop: () => void; listeners: Set<Listener> }>();
/**
 * One resumable event stream per visible session, shared by its messages and
 * panels. It is closed while the tab is hidden, and waits while the browser
 * has no connection to spare (stream.ts). The stream opens with the session
 * as it stands, so the first frame after either is handed on as a reset as
 * well, and every listener starts again from the record.
 */
export function followSession(id: string, listener: Listener): () => void {
  let entry = streams.get(id);
  if (!entry) {
    const listeners = new Set<Listener>();
    // With no connection to spare, the session is read as it stands instead.
    const poll = () => {
      fetch(`/api/sessions/${encodeURIComponent(id)}`, { credentials: "same-origin" })
        .then((res) => (res.ok ? (res.json() as Promise<AgentSession>) : undefined))
        .then((session) => {
          if (!session) return;
          for (const current of listeners) {
            current.reset?.(session);
            current.session?.(session);
          }
        })
        .catch(() => {});
    };
    const path = `/api/sessions/${encodeURIComponent(id)}/stream`;
    const stop = openStream(
      path,
      (source, resumed) => {
        let reset = resumed;
        source.addEventListener("session", (event) => {
          try {
            const session = JSON.parse((event as MessageEvent).data) as AgentSession;
            const first = reset;
            reset = false;
            for (const current of listeners) {
              if (first) current.reset?.(session);
              current.session?.(session);
            }
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
      },
      { poll },
    );
    entry = { stop, listeners };
    streams.set(id, entry);
  }
  entry.listeners.add(listener);
  return () => {
    entry.listeners.delete(listener);
    if (!entry.listeners.size) {
      entry.stop();
      streams.delete(id);
    }
  };
}
