import { defaultCopy } from "@humagen/components";
import { useRef, useState } from "react";
import { ApiError, coworking, type Thread, type ThreadPage } from "./api.ts";
import { conflictOf } from "./conflicts.ts";
import type { Selection } from "./Plus.tsx";

/**
 * A conversation's + selection: its agents and how they are routed to, and
 * where it lives, shown at once as it is changed and saved on the thread
 * with PUT /api/threads/{id}. One save at a time: a change made while one
 * is in flight waits for it, then is saved from the version it answered,
 * so quick changes never send a version already gone. A refusal is said in
 * the console's own words, never the server's, and a thread someone else
 * changed meanwhile is read again.
 */
export function useThreadSelection(
  id: string,
  th: Thread,
  on: {
    saved: (t: Thread) => void;
    reread: (p: ThreadPage) => void;
    problem: (why: string | null) => void;
  },
): { selection: Selection; select: (next: Selection) => void } {
  const saved: Selection = { agents: th.agents ?? [], mode: th.mode === "all" ? "all" : "join", project: th.project, channel: th.channel };
  const [shown, setShown] = useState<Selection | null>(null);
  const saving = useRef<{ busy: boolean; next: Selection | null }>({ busy: false, next: null });
  // The newest thread a save answered, which a render may not show yet.
  const latest = useRef<Thread>(th);
  if (th.version > latest.current.version || th.id !== latest.current.id) latest.current = th;
  const words = defaultCopy.threads;

  const save = (from: Thread, next: Selection) => {
    saving.current = { busy: true, next: null };
    const moved = next.project !== from.project || next.channel !== from.channel;
    coworking
      .update(id, {
        version: from.version,
        title: from.title ?? "",
        visibility: from.visibility,
        default: from.default ?? "",
        agents: next.agents,
        mode: next.mode,
        ...(moved && next.project !== null ? { project: next.project === "" ? "~" : next.project, channel: next.channel } : {}),
      })
      .then((t) => {
        latest.current = t;
        on.saved(t);
        const later = saving.current.next;
        if (later) return save(t, later);
        saving.current = { busy: false, next: null };
        setShown(null);
      })
      .catch((e) => {
        saving.current = { busy: false, next: null };
        setShown(null);
        const why = conflictOf(e);
        if (why === "version") {
          on.problem(words.changedMeanwhile);
          coworking
            .thread(id)
            .then((p) => {
              latest.current = p.thread;
              on.reread(p);
            })
            .catch(() => {});
        } else if (why === "project") on.problem(words.shareConflict);
        else if (why === "channel") on.problem(words.shareChannelFixed);
        else if (e instanceof ApiError && e.status === 403) on.problem(defaultCopy.noPermission);
        else on.problem(defaultCopy.chat.saveFailed);
      });
  };

  const select = (next: Selection) => {
    setShown(next);
    on.problem(null);
    if (saving.current.busy) saving.current.next = next;
    else save(latest.current, next);
  };

  return { selection: shown ?? saved, select };
}
