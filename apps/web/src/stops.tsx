import { defaultCopy } from "@humagen/components";
import type { ItemStop, ItemStops, TurnItem } from "@humagen/render-web";
import { stopWords } from "@humagen/web";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { Run } from "./adapter.ts";
import { api, type BackgroundTask } from "./api.ts";
import { isEnded, isWorking } from "./ui.tsx";

/*
 * Stop at two levels. The composer's Stop ends everything running in a
 * session: its runs and its child sessions' (`POST /api/sessions/{id}/stop`
 * with children) and the background work its messages started
 * (`POST /api/me/background/{id}/stop`). Each running item in the transcript
 * has its own Stop at the top of its row: a call to another agent stops that
 * agent's run (`POST /api/runs/{id}/stop`), background work a step started
 * stops that work, and a step in flight stops the run it is part of.
 */

const words = defaultCopy.dock;

/** The tools that start background work; their result is the work, with its id. */
const STARTS = new Set(["watch_start", "goal_start", "task_start"]);

/** The background work a step started, read from what the step returned. */
export function startedWork(tool: string | undefined, output: string | undefined): string | null {
  if (!tool || !STARTS.has(tool) || !output) return null;
  try {
    const v = JSON.parse(output) as { id?: unknown };
    return typeof v.id === "string" && v.id ? v.id : null;
  } catch {
    return null;
  }
}

/**
 * A call whose agent was stopped, as the platform records it: the call ends
 * in error with "<agent> was stopped: <why>" (internal/platform/delegate.go).
 * Read after a reload, when nothing on this page asked for the stop.
 */
export function stoppedCall(agent: string, status: string, output: string | undefined): boolean {
  return status === "error" && (output ?? "").startsWith(`${agent} was stopped`);
}

/** Background work still going: it is active, and either working now or due again. */
export function workGoing(item: BackgroundTask): boolean {
  return item.state === "active";
}

/**
 * The reader's background work that belongs to one session: started from it,
 * or running in it. Read when the session opens and again on `refresh`.
 */
export function useSessionBackground(session: string | undefined): {
  items: BackgroundTask[];
  refresh: () => void;
} {
  const [all, setAll] = useState<BackgroundTask[]>([]);
  const live = useRef(true);
  const refresh = useCallback(() => {
    if (!session) return;
    api
      .background()
      .then((items) => {
        if (live.current) setAll(items);
      })
      .catch(() => {
        // Background work is extra: a session reads fine without it.
      });
  }, [session]);
  useEffect(() => {
    live.current = true;
    refresh();
    return () => {
      live.current = false;
    };
  }, [refresh]);
  const items = useMemo(
    () => all.filter((it) => it.session === session || it.thread === session),
    [all, session],
  );
  return { items, refresh };
}

type Mark = "pending" | "stopped" | "failed";

/**
 * Each running item's own Stop, for one answer. `agentStop` is the answer's
 * own run's Stop (useRunStop), used by a step in flight.
 */
export function useItemStops({
  run,
  allowed,
  background,
  onBackgroundChanged,
  agentStop,
}: {
  run: Run;
  allowed: boolean;
  background: BackgroundTask[];
  onBackgroundChanged?: () => void;
  agentStop?: { onStop: () => void; pending: boolean };
}): ItemStops | undefined {
  const [marks, setMarks] = useState<Record<string, Mark>>({});
  const live = useRef(true);
  useEffect(() => {
    live.current = true;
    return () => {
      live.current = false;
    };
  }, []);
  const mark = useCallback((id: string, m: Mark) => {
    if (live.current) setMarks((all) => ({ ...all, [id]: m }));
  }, []);

  const stopChild = useCallback(
    (id: string) => {
      mark(id, "pending");
      let tries = 0;
      const watch = (r: Run) => {
        if (isEnded(r)) return mark(id, "stopped");
        if (++tries > 30) return mark(id, "failed");
        setTimeout(() => {
          api
            .run(id)
            .then(watch)
            .catch(() => mark(id, "failed"));
        }, 1000);
      };
      api
        .stopRun(id)
        .then(watch)
        .catch(() => mark(id, "failed"));
    },
    [mark],
  );
  const stopWork = useCallback(
    (id: string) => {
      mark(id, "pending");
      api
        .stopBackground(id)
        .then((it) => {
          mark(id, it.state === "active" ? "failed" : "stopped");
          onBackgroundChanged?.();
        })
        .catch(() => mark(id, "failed"));
    },
    [mark, onBackgroundChanged],
  );

  return useMemo(() => {
    if (!allowed) return undefined;
    const runWorks = isWorking(run.status) && run.status !== "parked";
    const of = (item: TurnItem): ItemStop | null => {
      const task = item.task;
      if (!task) return null;
      const sub = task.subagent;
      if (item.kind === "subagent" || sub) {
        const id = sub?.runId;
        if (!id || !sub) return null;
        const m = marks[id];
        if (m === "stopped" || stoppedCall(sub.name, task.status, task.output))
          return { what: sub.name, stopped: true };
        const going =
          task.status === "in_progress" || task.status === "pending" || task.status === "waiting";
        if (!going) return null;
        return { what: sub.name, pending: m === "pending", onStop: () => stopChild(id) };
      }
      const work = startedWork(task.tool, task.output);
      if (work) {
        const it = background.find((b) => b.id === work);
        const m = marks[work];
        if (m === "stopped" || it?.state === "stopped")
          return { what: it ? words.backgroundWork(it.kind, it.text) : task.title, stopped: true };
        if (!it || !workGoing(it)) return null;
        return {
          what: words.backgroundWork(it.kind, it.text),
          pending: m === "pending",
          onStop: () => stopWork(work),
        };
      }
      // A step in flight is part of its run: its Stop stops that run.
      if (task.status === "in_progress" && runWorks && agentStop)
        return { what: run.agent, pending: agentStop.pending, onStop: agentStop.onStop };
      return null;
    };
    return { of, words: stopWords(defaultCopy) };
  }, [allowed, run.status, run.agent, marks, background, stopChild, stopWork, agentStop]);
}

/**
 * The composer's Stop: everything running in a session, its child sessions
 * and the background work started from it. `pending` until every run it
 * asked has ended; `problem` when some of it could not be asked.
 */
export function useSessionStop({
  session,
  runs,
  background,
  onBackgroundChanged,
}: {
  session: string | undefined;
  runs: Run[];
  background: BackgroundTask[];
  onBackgroundChanged?: () => void;
}): { busy: boolean; pending: boolean; problem: string | null; stopAll: () => Promise<void> } {
  const [asked, setAsked] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const working = runs.some((r) => isWorking(r.status) && r.status !== "parked");
  const going = background.filter(workGoing);
  const busy = working || going.some((b) => b.working);
  useEffect(() => {
    if (!busy) setAsked(false);
  }, [busy]);
  const stopAll = useCallback(async () => {
    if (!session) return;
    setAsked(true);
    setProblem(null);
    const results = await Promise.allSettled([
      api.stopSession(session),
      ...going.map((b) => api.stopBackground(b.id)),
    ]);
    if (going.length) onBackgroundChanged?.();
    if (results.some((r) => r.status === "rejected")) {
      setAsked(false);
      setProblem(words.stopAllFailed);
    }
  }, [session, going, onBackgroundChanged]);
  return { busy, pending: asked && busy, problem, stopAll };
}
