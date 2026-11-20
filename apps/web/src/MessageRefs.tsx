import { defaultCopy } from "@humagen/components";
import { allTools } from "@humagen/core";
import { useEffect, useMemo, useRef, useState } from "react";
import type { Run } from "./adapter.ts";
import { responseChips, summaryOf, useResponse, WorkedButton } from "./ResponseActivity.tsx";
import { Chips } from "./refChips.tsx";
import { type TraceLoader, traceSource } from "./responseSource.ts";
import type { Chip, Reference } from "./trace.ts";
import { AgentMark } from "./ui.tsx";

export { Chips, inApp, OriginMark } from "./refChips.tsx";

const words = defaultCopy.trace;
const ENDED = new Set(["completed", "failed", "stopped"]);

/** Seen: in view once (IntersectionObserver), or at once when `eager`. */
function useSeen(eager = false) {
  const at = useRef<HTMLDivElement>(null);
  const [seen, setSeen] = useState(eager);
  useEffect(() => {
    if (seen || !at.current || typeof IntersectionObserver === "undefined") return;
    const watch = new IntersectionObserver((entries) => {
      if (entries.some((e) => e.isIntersecting)) {
        setSeen(true);
        watch.disconnect();
      }
    });
    watch.observe(at.current);
    return () => watch.disconnect();
  }, [seen]);
  return { at, seen };
}

/** An ended run's response, folded from its source once it is seen. */
function useRunResponse(run: Run, seen: boolean, loader?: TraceLoader) {
  const ended = ENDED.has(run.status);
  // A run's response changes only when its status does, so the source is keyed by them.
  // biome-ignore lint/correctness/useExhaustiveDependencies: keyed by the run's id and status, not its object.
  const source = useMemo(
    () => (seen && ended ? traceSource(run, loader) : undefined),
    [seen, ended, run.id, run.status, loader],
  );
  return useResponse(source);
}

/**
 * Under an answer: its references as quiet chips and, when it used a tool or
 * brought in another agent, "Worked 42s · 3 agents · 7 tools", which opens the
 * response's activity in Details. Time, cost and model are in the activity.
 */
export function AnswerRefs({
  run,
  eager,
  loader,
  open = false,
  onActivity,
}: {
  run: Run;
  eager?: boolean;
  loader?: TraceLoader;
  /** This answer's activity is open in Details. */
  open?: boolean;
  /** Open or close this response's activity in Details. Without it, no control is offered. */
  onActivity?: (run: string) => void;
}) {
  const { at, seen } = useSeen(eager);
  const { state, ended } = useRunResponse(run, seen, loader);
  const chips: Chip[] = ended
    ? responseChips(state)
    : (run.citations ?? []).map((c) => ({ kind: "source", label: c.title, url: c.url }));
  return (
    <div ref={at} className="flex flex-col gap-1" data-testid="answer-refs">
      <Chips chips={chips} />
      {ended && onActivity && (
        <WorkedButton state={state} open={open} onToggle={() => onActivity(run.id)} />
      )}
    </div>
  );
}

/** Under the root message: where it came from, linked. */
export function OriginRef({ origin }: { origin?: Reference }) {
  if (
    !origin ||
    (origin.kind !== "slack" && origin.kind !== "github" && origin.kind !== "schedule")
  )
    return null;
  return (
    <Chips chips={[{ kind: "origin", label: origin.label, url: origin.url }]} origin={origin} />
  );
}

/** One answer in Details: how it worked, what it cost, on which model, whom it brought in, and its references. */
function ReferenceRow({
  run,
  origin,
  loader,
}: {
  run: Run;
  origin?: Reference;
  loader?: TraceLoader;
}) {
  const { state, ended } = useRunResponse(run, true, loader);
  const agents = ended
    ? [...new Set(allTools(state).flatMap((t) => (t.kind === "agent" ? [t.agent ?? t.name] : [])))]
    : [];
  return (
    <li className="flex flex-col gap-1" data-testid="reference-row">
      <p className="flex flex-wrap items-center gap-x-2 text-aui-xs">
        <AgentMark name={run.agent} size={16} />
        <a
          href={`#turn-${run.id}`}
          className="inline-flex min-h-6 items-center rounded-aui text-aui-fg hover:underline focus-visible:outline-2 focus-visible:outline-aui-accent pointer-coarse:min-h-11"
        >
          {run.agent}
        </a>
        {ended && <span className="text-aui-muted">{summaryOf(state)}</span>}
      </p>
      {agents.length > 0 && (
        <p className="pl-6 text-aui-2xs text-aui-muted">{words.delegatedTo(agents.join(", "))}</p>
      )}
      <div className="pl-6">
        <Chips chips={ended ? responseChips(state, origin) : []} origin={origin} />
      </div>
    </li>
  );
}

/**
 * Every message's references for the whole session, in Details: per answer,
 * how it worked, what it cost and on which model, whom it brought in, and what
 * it read and cited.
 */
export function SessionReferences({
  runs,
  origin,
  loader,
}: {
  runs: Run[];
  origin?: Reference;
  loader?: TraceLoader;
}) {
  // Answers people see: each turn's own run, not the runs it called.
  const answers = runs.filter((r) => !r.parent);
  if (!answers.length) return null;
  return (
    <section aria-labelledby="session-references" className="flex flex-col gap-2">
      <h3 id="session-references" className="font-medium text-aui-fg text-aui-sm">
        {words.references}
      </h3>
      <ul className="flex flex-col gap-3">
        {answers.map((r, i) => (
          <ReferenceRow key={r.id} run={r} origin={i === 0 ? origin : undefined} loader={loader} />
        ))}
      </ul>
    </section>
  );
}
