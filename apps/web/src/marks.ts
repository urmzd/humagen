import { useCallback, useEffect, useState } from "react";
import { coworking, type Marks } from "./api.ts";
import { NO_MARKS } from "./conversationList.ts";

/**
 * The reader's own marks on conversations: archived for them, when they last
 * read each, and the tips they dismissed. Archiving shows at once and is
 * taken back when the server refuses it.
 */
export function useMarks(enabled: boolean) {
  const [marks, setMarks] = useState<Marks>(NO_MARKS);
  useEffect(() => {
    if (!enabled) return;
    coworking
      .marks()
      .then(setMarks)
      .catch(() => {});
  }, [enabled]);
  const archive = useCallback((id: string, on: boolean) => {
    let was: Marks = NO_MARKS;
    setMarks((m) => {
      was = m;
      const rest = m.archived.filter((a) => a !== id);
      return { ...m, archived: on ? [id, ...rest] : rest };
    });
    return coworking.markThread(id, { archived: on }).then(setMarks, (err) => {
      setMarks(was);
      throw err;
    });
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
  return { marks, archive, read, dismissTip };
}
