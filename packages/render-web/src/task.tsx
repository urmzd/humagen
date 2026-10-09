import type { Citation, TaskStatus } from "@agent-ui/core";
import type { PrimitiveProps } from "@agent-ui/primitives";
import {
  Children,
  createContext,
  isValidElement,
  type ReactNode,
  useContext,
  useEffect,
  useRef,
} from "react";
import { hostOf, SourceMark } from "./citations";
import { CheckIcon, CircleIcon, ListChecksIcon, Spinner, XIcon } from "./icons";
import { Collapse, Shimmer, useDisclosure } from "./motion";

export const statusLabel: Record<TaskStatus, string> = {
  pending: "Pending",
  in_progress: "In progress",
  complete: "Complete",
  error: "Error",
};

/**
 * Task state in a fixed 16px square. Running is an SVG ring rotating about the
 * square's center; settled states never animate.
 */
export function StatusIcon({ status, size = 16 }: { status: TaskStatus; size?: number }) {
  const box = { width: size, height: size };
  if (status === "in_progress") {
    return (
      <span
        aria-hidden
        style={box}
        className="flex shrink-0 items-center justify-center text-aui-accent"
      >
        <Spinner size={size - 2} />
      </span>
    );
  }
  if (status === "complete") {
    return (
      <span
        aria-hidden
        style={box}
        className="flex shrink-0 items-center justify-center rounded-full bg-aui-ok/15 text-aui-ok"
      >
        <CheckIcon size={size - 6} strokeWidth={3} />
      </span>
    );
  }
  if (status === "error") {
    return (
      <span
        aria-hidden
        style={box}
        className="flex shrink-0 items-center justify-center rounded-full bg-aui-bad/15 text-aui-bad"
      >
        <XIcon size={size - 6} strokeWidth={3} />
      </span>
    );
  }
  return (
    <span
      aria-hidden
      style={box}
      className="flex shrink-0 items-center justify-center text-aui-muted"
    >
      <CircleIcon size={size - 2} strokeDasharray="2.5 3" />
    </span>
  );
}

function SourceChips({ sources }: { sources: Citation[] }) {
  return (
    <ul className="flex flex-wrap gap-1.5" aria-label="Sources">
      {sources.map((c) => {
        const origin = c.source ?? hostOf(c.url);
        const inner = (
          <>
            <SourceMark label={origin} className="size-3.5 text-[0.55rem]" />
            <span className="max-w-56 truncate">{c.title}</span>
          </>
        );
        const cls =
          "inline-flex h-6 items-center gap-1.5 rounded-full border border-aui-border bg-aui-surface pr-2 pl-1 text-aui-2xs text-aui-muted";
        return (
          <li key={c.id}>
            {c.url ? (
              <a
                href={c.url}
                target="_blank"
                rel="noreferrer"
                className={`${cls} transition-colors hover:border-aui-accent hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent`}
              >
                {inner}
              </a>
            ) : (
              <span className={cls}>{inner}</span>
            )}
          </li>
        );
      })}
    </ul>
  );
}

/**
 * One tool call as a row: status, title, and detail on demand. Errors open by
 * themselves (latched), so the reason is visible without a click.
 */
export function TaskRow({ title, status, details, output, sources }: PrimitiveProps["Task"]) {
  const hasBody = Boolean(details || output || (sources && sources.length > 0));
  const { open, toggle } = useDisclosure({ auto: status === "error" });
  const titleNode =
    status === "in_progress" ? (
      <Shimmer>{title}</Shimmer>
    ) : (
      <span className={status === "pending" ? "text-aui-muted" : "text-aui-fg"}>{title}</span>
    );
  const summary = !open && (output || details);

  const head = (
    <>
      <span className="mt-0.5">
        <StatusIcon status={status} />
      </span>
      <span className="min-w-0 flex-1">
        <span className="block truncate">
          {titleNode}
          <span className="sr-only">, {statusLabel[status]}</span>
        </span>
        {summary && <span className="block truncate text-aui-muted text-aui-xs">{summary}</span>}
      </span>
    </>
  );

  return (
    <li className="relative list-none" data-status={status}>
      {hasBody ? (
        <>
          <button
            type="button"
            aria-expanded={open}
            onClick={toggle}
            className="group -mx-1.5 flex w-[calc(100%+0.75rem)] min-w-0 items-start gap-2.5 rounded-aui-sm px-1.5 py-1 text-left text-aui-sm transition-colors hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
          >
            {head}
            <span
              aria-hidden
              className={`mt-1 text-aui-muted opacity-0 transition group-hover:opacity-100 group-focus-visible:opacity-100 motion-reduce:transition-none ${open ? "rotate-90 opacity-100" : ""}`}
            >
              <svg
                width="12"
                height="12"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2.5"
                strokeLinecap="round"
                strokeLinejoin="round"
                aria-hidden="true"
              >
                <path d="m9 18 6-6-6-6" />
              </svg>
            </span>
          </button>
          <Collapse open={open}>
            <div className="ml-[0.4375rem] flex flex-col gap-1.5 border-aui-border border-l py-1 pr-1 pb-2 pl-[1.1875rem] text-aui-xs">
              {details && <p className="text-aui-muted">{details}</p>}
              {output && (
                <p
                  className={`whitespace-pre-wrap font-aui-mono text-aui-2xs ${status === "error" ? "text-aui-bad" : "text-aui-fg"}`}
                >
                  {output}
                </p>
              )}
              {sources && sources.length > 0 && <SourceChips sources={sources} />}
            </div>
          </Collapse>
        </>
      ) : (
        <div className="flex min-w-0 items-start gap-2.5 py-1 text-aui-sm">{head}</div>
      )}
    </li>
  );
}

const InPlan = createContext(false);

/** The Task primitive: a row inside a Plan, else one row of a timeline (adjacent rows join up via CSS). */
export function TaskItem(props: PrimitiveProps["Task"]) {
  if (useContext(InPlan)) return <TaskRow {...props} />;
  return (
    <ul data-aui="task" className="flex flex-col">
      <TaskRow {...props} />
    </ul>
  );
}

function statusesOf(children: ReactNode): TaskStatus[] {
  return Children.toArray(children).flatMap((c) =>
    isValidElement<{ status?: TaskStatus }>(c) && c.props.status ? [c.props.status] : [],
  );
}

/**
 * A plan: a collapsible card whose header carries the state. Its title already
 * says where the run is ("Checking recent deploys", "Worked for 12s"); the icon
 * and the "2 of 3" counter say it at a glance. Open while running; folds itself
 * once everything is complete, unless the viewer chose otherwise.
 */
export function Plan({ title, children }: PrimitiveProps["Plan"]) {
  const statuses = statusesOf(children);
  const total = statuses.length;
  const done = statuses.filter((s) => s === "complete").length;
  const failed = statuses.some((s) => s === "error");
  const running = statuses.some((s) => s === "in_progress");
  const allDone = total > 0 && done === total;

  const { open, setOpen, toggle } = useDisclosure({ defaultOpen: !allDone, auto: failed });
  const wasDone = useRef(allDone);
  useEffect(() => {
    if (allDone && !wasDone.current) {
      const t = setTimeout(() => {
        wasDone.current = true;
        setOpen(false);
      }, 900);
      return () => clearTimeout(t);
    }
    wasDone.current = allDone;
  }, [allDone, setOpen]);

  const state: TaskStatus = failed
    ? "error"
    : running
      ? "in_progress"
      : allDone
        ? "complete"
        : "pending";

  return (
    <section
      data-aui="plan"
      aria-label={title}
      className="overflow-hidden rounded-aui-lg border border-aui-border bg-aui-surface"
    >
      <button
        type="button"
        aria-expanded={open}
        onClick={toggle}
        className="group flex w-full min-w-0 items-center gap-2.5 px-3 py-2.5 text-left transition-colors hover:bg-aui-hover focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-aui-accent"
      >
        {state === "pending" ? (
          <ListChecksIcon size={16} className="text-aui-muted" />
        ) : (
          <StatusIcon status={state} />
        )}
        <span className="min-w-0 flex-1 truncate font-medium text-aui-sm">
          {running ? <Shimmer>{title}</Shimmer> : title}
        </span>
        {total > 0 && (
          <span className="shrink-0 text-aui-muted text-aui-xs tabular-nums">
            {done} of {total}
          </span>
        )}
        <svg
          aria-hidden="true"
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          className={`shrink-0 text-aui-muted transition-transform duration-200 group-hover:text-aui-fg motion-reduce:transition-none ${open ? "rotate-180" : ""}`}
        >
          <path d="m6 9 6 6 6-6" />
        </svg>
      </button>
      {total > 0 && (
        <div className="h-0.5 bg-aui-subtle" aria-hidden>
          <div
            className={`h-full transition-[width] duration-500 motion-reduce:transition-none ${failed ? "bg-aui-bad" : allDone ? "bg-aui-ok" : "bg-aui-accent"}`}
            style={{ width: `${(done / total) * 100}%` }}
          />
        </div>
      )}
      <Collapse open={open}>
        <ul className="flex flex-col px-3 pt-1.5 pb-2">
          <InPlan.Provider value={true}>{children}</InPlan.Provider>
        </ul>
      </Collapse>
    </section>
  );
}
