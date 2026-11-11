import { defaultCopy } from "@humagen/components";
import { ShimmerVerb } from "@humagen/render-web";
import { useTurnActivity } from "@humagen/web";
import { useEffect, useState } from "react";
import { actedFor, type Run, turnOf } from "./adapter.ts";
import { took } from "./format.ts";
import { useRunStop } from "./RunStop.tsx";
import { useRunTurn } from "./Runs.tsx";
import { AgentMark } from "./ui.tsx";

/** How many working threads the prompt lists before it folds the rest. */
export const WORKING_SHOWN = 3;

/** The time now, read again every second while something is counting. */
function useNow(on: boolean): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!on) return;
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [on]);
  return now;
}

/**
 * One thread at work: its agent, what it is doing now, whom it works for
 * when that is someone else, how long it has worked, and Stop. Its line
 * opens the thread's activity.
 */
function WorkingRow({
  run,
  me,
  onOpen,
}: {
  run: Run;
  me?: string;
  onOpen?: (run: string) => void;
}) {
  const turn = useRunTurn(run) ?? turnOf(run);
  const doing = useTurnActivity(turn);
  const { stop } = useRunStop(run, true);
  const parked = run.status === "parked";
  const now = useNow(!parked);
  // Named as the person is shown, never by the subject they sign in with.
  const by = actedFor(run) !== (me ?? "") ? run.actor?.display : undefined;
  const label = parked
    ? defaultCopy.trace.waitingForYou
    : (doing ?? defaultCopy.trace.status.running);
  const line = (
    <>
      <AgentMark name={run.agent} size={16} />
      <span className="shrink-0 font-medium text-aui-fg">{run.agent}</span>
      <span className="min-w-0 flex-1 truncate text-aui-muted">
        {parked ? label : <ShimmerVerb>{label}</ShimmerVerb>}
      </span>
      {by && me !== undefined && (
        <span className="shrink-0 text-aui-muted">{defaultCopy.working.for(by)}</span>
      )}
      <span className="shrink-0 text-aui-muted tabular-nums">
        {took(Math.max(0, now - Date.parse(run.created_at)))}
      </span>
    </>
  );
  const row =
    "flex min-h-8 pointer-coarse:min-h-11 min-w-0 flex-1 items-center gap-2 rounded-aui px-1.5 text-left";
  return (
    <li data-testid="working-thread" data-run={run.id} className="flex min-w-0 items-center gap-1">
      {onOpen ? (
        <button
          type="button"
          aria-label={defaultCopy.working.open(run.agent)}
          onClick={() => onOpen(run.id)}
          className={`${row} hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent`}
        >
          {line}
        </button>
      ) : (
        <span className={row}>{line}</span>
      )}
      {stop && (
        <button
          type="button"
          onClick={stop.onStop}
          disabled={stop.pending}
          aria-label={
            stop.pending
              ? defaultCopy.conversation.stoppingAgent(run.agent)
              : defaultCopy.conversation.stopAgent(run.agent)
          }
          className="inline-flex min-h-8 pointer-coarse:min-h-11 shrink-0 items-center rounded-aui px-2 text-aui-fg hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent disabled:opacity-60"
        >
          {stop.pending ? defaultCopy.working.stopping : defaultCopy.working.stop}
        </button>
      )}
    </li>
  );
}

/**
 * Every thread at work in the session, in the prompt: one line each, oldest
 * first. A session holds several at once: different people's messages, and
 * work an agent handed on. Past a few, the rest fold behind a count. The
 * agents a thread called are part of its line, not lines of their own.
 */
export function WorkingStrip({
  runs,
  me,
  onOpen,
}: {
  /** The session's runs that have not ended and answer in their own right. */
  runs: Run[];
  /** The reader, so a thread working for someone else says for whom. */
  me?: string;
  onOpen?: (run: string) => void;
}) {
  const [all, setAll] = useState(false);
  if (runs.length === 0) return null;
  const shown = all ? runs : runs.slice(0, WORKING_SHOWN);
  const more = runs.length - shown.length;
  return (
    <ul
      data-testid="working-strip"
      aria-label={defaultCopy.working.label(runs.length)}
      className="flex min-w-0 flex-col px-1.5 pt-1.5 text-aui-xs"
    >
      {shown.map((run) => (
        <WorkingRow key={run.id} run={run} me={me} onOpen={onOpen} />
      ))}
      {(more > 0 || all) && runs.length > WORKING_SHOWN && (
        <li>
          <button
            type="button"
            aria-expanded={all}
            onClick={() => setAll(!all)}
            className="min-h-7 pointer-coarse:min-h-11 rounded-aui px-1.5 text-aui-muted hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent"
          >
            {all ? defaultCopy.working.fewer : defaultCopy.working.more(more)}
          </button>
        </li>
      )}
    </ul>
  );
}
