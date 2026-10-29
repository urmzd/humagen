import { defaultCopy } from "@humagen/components";
import type { MarkStop } from "@humagen/web";
import { useEffect, useRef, useState } from "react";
import type { Run } from "./adapter.ts";
import { api } from "./api.ts";
import { cause, isEnded } from "./ui.tsx";

/** A stop request never claims completion until the run ledger records it. */
export function useRunStop(run: Run, allowed: boolean): { run: Run; stop?: MarkStop } {
  const [record, setRecord] = useState<Run>();
  const [pending, setPending] = useState(false);
  const [problem, setProblem] = useState("");
  const active = useRef(true);
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);
  useEffect(() => {
    active.current = true;
    return () => {
      active.current = false;
      clearTimeout(timer.current);
    };
  }, []);
  const shown = record?.id === run.id && isEnded(record) ? record : run;
  const stop = async () => {
    setPending(true);
    setProblem("");
    let attempts = 0;
    const observe = async (latest: Run) => {
      if (!active.current) return;
      setRecord(latest);
      if (isEnded(latest)) {
        setPending(false);
        return;
      }
      if (++attempts >= 60) {
        setPending(false);
        setProblem(defaultCopy.conversation.stopDelayed);
        return;
      }
      timer.current = setTimeout(() => {
        api.run(run.id).then(observe).catch(failed);
      }, 1000);
    };
    const failed = (error: unknown) => {
      if (active.current) {
        setPending(false);
        setProblem(cause(error instanceof Error ? error.message : String(error)));
      }
    };
    try {
      await observe(await api.stopRun(run.id));
    } catch (error) {
      failed(error);
    }
  };
  return {
    run: shown,
    // Stopped from the agent's mark, which shows stopping until the ledger records the end.
    stop:
      allowed && !isEnded(shown)
        ? { onStop: stop, pending, problem: problem || undefined }
        : undefined,
  };
}
