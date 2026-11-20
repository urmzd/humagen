import { useCallback, useEffect, useState } from "react";
import { coworking, type Marks } from "./api.ts";
import { NO_MARKS } from "./conversationList.ts";

/** Keep header and sidebar marks in sync after an accepted mutation. */
export function publishMarks(marks: Marks) {
  window.dispatchEvent(new CustomEvent("humagen:marks", { detail: marks }));
}

/**
 * The reader's own marks on conversations: archived for them, when they last
 * read each, and the tips they dismissed. Archiving shows at once and is
 * taken back when the server refuses it.
 */
export function useMarks(enabled: boolean) {
  const [marks, setMarks] = useState<Marks>(NO_MARKS);
  // Whether the saved marks have been read: a one-time tip waits for them,
  // so one already dismissed never flashes in.
  const [loaded, setLoaded] = useState(false);
  useEffect(() => {
    if (!enabled) return;
    const changed = (event: Event) => setMarks((event as CustomEvent<Marks>).detail);
    window.addEventListener("humagen:marks", changed);
    coworking
      .marks()
      .then((m) => {
        setMarks(m);
        setLoaded(true);
      })
      .catch(() => {});
    return () => window.removeEventListener("humagen:marks", changed);
  }, [enabled]);
  const archive = useCallback((id: string, on: boolean) => {
    let was: Marks = NO_MARKS;
    setMarks((m) => {
      was = m;
      const rest = m.archived.filter((a) => a !== id);
      return { ...m, archived: on ? [id, ...rest] : rest };
    });
    return coworking.markThread(id, { archived: on }).then(
      (next) => {
        setMarks(next);
        publishMarks(next);
      },
      (err) => {
        setMarks(was);
        throw err;
      },
    );
  }, []);
  const read = useCallback((id: string) => {
    coworking
      .markThread(id, { read: true })
      .then(setMarks)
      .catch(() => {});
  }, []);
  const dismissTip = useCallback((name: string) => {
    setMarks((m) => (m.tips.includes(name) ? m : { ...m, tips: [...m.tips, name] }));
    coworking
      .dismissTip(name)
      .then(setMarks)
      .catch(() => {});
  }, []);
  return { marks, loaded, archive, read, dismissTip };
}
