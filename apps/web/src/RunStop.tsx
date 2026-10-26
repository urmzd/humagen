import { defaultCopy } from "@humagen/components";
import { useEffect, useRef, useState } from "react";
import { api } from "./api.ts";
import type { Run } from "./adapter.ts";
import { cause, isEnded } from "./ui.tsx";

/** A stop request never claims completion until the run ledger records it. */
export function useRunStop(run: Run, allowed: boolean) {
  const [record, setRecord] = useState<Run>();
  const [pending, setPending] = useState(false);
  const [problem, setProblem] = useState("");
  const active = useRef(true);
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);
  useEffect(() => { active.current = true; return () => { active.current = false; clearTimeout(timer.current); }; }, []);
  const shown = record?.id === run.id && isEnded(record) ? record : run;
  const stop = async () => {
    setPending(true);
    setProblem("");
    let attempts = 0;
    const observe = async (latest: Run) => {
      if (!active.current) return;
      setRecord(latest);
      if (isEnded(latest)) { setPending(false); return; }
      if (++attempts >= 60) { setPending(false); setProblem(defaultCopy.conversation.stopDelayed); return; }
      timer.current = setTimeout(() => { api.run(run.id).then(observe).catch(failed); }, 1000);
    };
    const failed = (error: unknown) => { if (active.current) { setPending(false); setProblem(cause(error instanceof Error ? error.message : String(error))); } };
    try { await observe(await api.stopRun(run.id)); } catch (error) { failed(error); }
  };
  return {
    run: shown,
    control: allowed && !isEnded(shown) ? <span className="flex min-w-0 flex-wrap items-center gap-2">
      <button type="button" disabled={pending} onClick={stop} className="min-h-7 rounded-aui px-2 text-aui-xs text-aui-muted hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent disabled:opacity-60">{pending ? defaultCopy.conversation.stoppingTurn : defaultCopy.conversation.stopTurn}</button>
      {pending && <output className="text-aui-xs text-aui-muted">{defaultCopy.conversation.stopPending}</output>}
      {problem && <span role="alert" className="text-aui-xs text-aui-bad">{problem}</span>}
    </span> : null,
  };
}
