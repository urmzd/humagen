import {
  ActionId,
  type Citation,
  formatTokens,
  type Subagent,
  type TaskStatus,
} from "@humagen/core";
import type { PrimitiveProps } from "@humagen/primitives";
import {
  Children,
  type CSSProperties,
  createContext,
  isValidElement,
  type ReactNode,
  useContext,
  useEffect,
  useRef,
  useState,
} from "react";
import { ActionContext, Actions, Button, DenseContext, Menu } from "./buttons";
import { Favicon, hostOf } from "./citations";
import {
  BanIcon,
  BotIcon,
  CheckIcon,
  ChevronDownIcon,
  ChevronRightIcon,
  CircleIcon,
  HourglassIcon,
  ListChecksIcon,
  Spinner,
  XIcon,
} from "./icons";
import { ItemActionsContext, type TurnItem, useTurnMeta } from "./meta";
import { Collapse, Shimmer, useDisclosure } from "./motion";

export const statusLabel: Record<TaskStatus, string> = {
  pending: "Pending",
  in_progress: "In progress",
  waiting: "Waiting for approval",
  complete: "Complete",
  denied: "Not run",
  error: "Error",
};

/** Prefer the tool's own explanation; keep structured data behind technical details. */
export function readableToolResult(output: string): string {
  try {
    const value: unknown = JSON.parse(output);
    if (typeof value === "string") return value.slice(0, 600);
    if (Array.isArray(value))
      return `${value.length} ${value.length === 1 ? "result" : "results"} returned`;
    if (value && typeof value === "object") {
      for (const key of ["summary", "message", "text", "result"]) {
        const line = (value as Record<string, unknown>)[key];
        if (typeof line === "string" && line.trim()) return line.slice(0, 600);
      }
      return `Returned structured data with ${Object.keys(value).length} fields`;
    }
  } catch {
    /* Plain text already reads as a result. */
  }
  return output.slice(0, 600);
}

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
  if (status === "waiting") {
    // Blocked on a person: a static mark, never a spinner.
    return (
      <span
        aria-hidden
        style={box}
        className="flex shrink-0 items-center justify-center text-aui-accent/70"
      >
        <HourglassIcon size={size - 3} />
      </span>
    );
  }
  if (status === "denied") {
    return (
      <span
        aria-hidden
        style={box}
        className="flex shrink-0 items-center justify-center text-aui-muted"
      >
        <BanIcon size={size - 3} />
      </span>
    );
  }
  return (
    <span
      aria-hidden
      style={box}
      className="flex shrink-0 items-center justify-center text-aui-muted"
    >
      <CircleIcon size={size - 4} strokeDasharray="2.5 3" />
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
            <Favicon url={c.url} label={origin} className="size-3" />
            <span className="max-w-56 truncate">{c.title}</span>
          </>
        );
        const cls =
          "inline-flex h-5 items-center gap-1 rounded-full bg-aui-subtle/70 pr-2 pl-1 text-aui-2xs text-aui-muted";
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

/** The host's controls for this row, at its trailing edge. Hidden until hover or focus, never unreachable. */
function ItemSlot({ item }: { item: TurnItem | null }) {
  const actions = useContext(ItemActionsContext);
  const node = actions && item ? actions(item) : null;
  if (!node) return null;
  return (
    <span
      data-aui="item-actions"
      className="absolute top-0 right-0 flex h-6 items-center opacity-0 transition-opacity focus-within:opacity-100 group-hover/item:opacity-100 motion-reduce:transition-none"
    >
      {node}
    </span>
  );
}

/** Room for the slot, so it never sits on top of the row's own text. */
const SLOT_ROOM = "group/item pr-7";

function duration(ms: number): string {
  const s = Math.round(ms / 1000);
  return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${s % 60}s`;
}

/**
 * A step another agent did, drawn as that agent rather than as a tool call:
 * its mark in its own hue, its name, where it stands, and one line of what it
 * was asked or what it came back with. With a run id the whole item is a
 * button that asks the host to open that run (`ActionId.openRun`).
 */
function AgentItem({
  title,
  status,
  output,
  subagent,
  turnId,
}: {
  title: string;
  status: TaskStatus;
  output?: string;
  subagent: Subagent;
  turnId: string;
}) {
  const onAction = useContext(ActionContext);
  const summary = subagent.summary ?? (status === "complete" && output ? output : title);
  const stats = [
    subagent.steps ? `${subagent.steps} steps` : null,
    subagent.tokens ? `${formatTokens(subagent.tokens)} tok` : null,
    subagent.durationMs ? duration(subagent.durationMs) : null,
  ].filter(Boolean);
  const inner = (
    <>
      <span
        aria-hidden
        className="flex size-4 shrink-0 items-center justify-center rounded-full bg-aui-accent text-aui-accent-fg"
      >
        <BotIcon size={10} />
      </span>
      <span className="flex min-w-0 flex-1 items-baseline gap-2">
        <span className="shrink-0 font-medium text-aui-fg">
          {status === "in_progress" ? <Shimmer>{subagent.name}</Shimmer> : subagent.name}
        </span>
        {/* Settled and fine needs no word; anything else is said, not just colored. */}
        {status !== "complete" && (
          <span
            className={`shrink-0 text-aui-2xs ${status === "error" ? "text-aui-bad" : "text-aui-accent"}`}
          >
            {agentStatus[status]}
          </span>
        )}
        <span className="min-w-0 flex-1 truncate text-aui-2xs text-aui-muted">{summary}</span>
        {stats.length > 0 && (
          <span className="shrink-0 text-aui-2xs text-aui-muted tabular-nums">
            {stats.join(" · ")}
          </span>
        )}
      </span>
    </>
  );
  const frame = {
    "data-aui": "agent-task",
    "data-status": status,
    // The agent's own hue re-derives the accent for this item only.
    ...(subagent.hue === undefined
      ? {}
      : { "data-accent": "agent", style: { "--aui-accent-h": subagent.hue } as CSSProperties }),
  };
  const cls =
    "group -mx-1 flex w-[calc(100%+0.5rem)] min-w-0 items-center gap-2 rounded-aui-sm px-1 py-0.5 text-left text-aui-sm";
  const live = `${cls} transition-colors hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent`;
  const label = `${subagent.name}, ${agentStatus[status]}: ${summary}`;
  const open = (
    <ChevronRightIcon
      size={12}
      strokeWidth={2.5}
      className="shrink-0 text-aui-muted opacity-60 group-hover:opacity-100"
    />
  );
  if (subagent.runId) {
    const target = subagent.runId;
    return (
      <button
        type="button"
        {...frame}
        aria-label={`Open ${label}`}
        aria-haspopup="dialog"
        onClick={() => onAction({ id: ActionId.openRun, payload: { turnId, target } })}
        className={live}
      >
        {inner}
        {open}
      </button>
    );
  }
  if (subagent.url) {
    return (
      <a {...frame} href={subagent.url} target="_blank" rel="noreferrer" className={live}>
        {inner}
        {open}
      </a>
    );
  }
  return (
    <div {...frame} className={cls}>
      {inner}
    </div>
  );
}

const agentStatus: Record<TaskStatus, string> = {
  pending: "Queued",
  in_progress: "Working",
  waiting: "Waiting for approval",
  complete: "Done",
  denied: "Not run",
  error: "Failed",
};

/**
 * One tool call as a single line: status, title, and a muted one-line summary.
 * Detail opens on demand. Errors open by themselves (latched).
 */
/** A gated call's own controls, inline on its row: Approve (with a split menu) and Decline. */
function DecisionControls({
  id,
  title,
  decision,
}: {
  id: string;
  title: string;
  decision: NonNullable<PrimitiveProps["Task"]["decision"]>;
}) {
  return (
    <DenseContext.Provider value={true}>
      <span className="ml-auto flex shrink-0 items-center">
        <Actions blockId={`decision:${id}`}>
          <Button
            label={decision.approve.label}
            action={decision.approve.action}
            style={decision.approve.style ?? "primary"}
            confirm={decision.approve.confirm}
            accessibilityLabel={`${decision.approve.label}: ${title}`}
          />
          {decision.menu && decision.menu.length > 0 && (
            <Menu label={`More options for ${decision.approve.label}`} items={decision.menu} />
          )}
          <Button
            label={decision.decline.label}
            action={decision.decline.action}
            accessibilityLabel={`${decision.decline.label}: ${title}`}
          />
        </Actions>
      </span>
    </DenseContext.Provider>
  );
}

export function TaskRow({
  id,
  title,
  status,
  details,
  output,
  sources,
  decision,
}: PrimitiveProps["Task"]) {
  const turn = useTurnMeta();
  const task = turn?.tasks.find((t) => t.id === id);
  const [technicalTab, setTechnicalTab] = useState<"input" | "output">("output");
  const subagent = task?.subagent;
  const hasSlot = useContext(ItemActionsContext) !== null && Boolean(turn && task);
  const item: TurnItem | null =
    turn && task ? { kind: subagent ? "subagent" : "task", turn, task } : null;
  const li = `relative list-none ${hasSlot ? SLOT_ROOM : ""}`;
  const shownDetails = details;
  const hasBody = Boolean(shownDetails || output || task?.input || (sources && sources.length > 0));
  const { open, toggle } = useDisclosure({ auto: status === "error" });
  const titleNode =
    status === "in_progress" ? (
      <Shimmer>{title}</Shimmer>
    ) : (
      <span
        className={
          status === "pending"
            ? "text-aui-muted"
            : status === "denied"
              ? "text-aui-muted line-through decoration-aui-muted/60"
              : "text-aui-fg"
        }
      >
        {title}
      </span>
    );
  const inline =
    status === "denied"
      ? (output ?? "Not run")
      : status === "waiting"
        ? "Waiting for approval"
        : !open
          ? output
            ? readableToolResult(output)
            : shownDetails
          : undefined;

  const head = (
    <>
      <StatusIcon status={status} size={14} />
      <span className="flex min-w-0 flex-1 items-baseline gap-2">
        <span className="min-w-0 shrink truncate">
          {titleNode}
          <span className="sr-only">, {statusLabel[status]}</span>
        </span>
        {inline && (
          <span
            className={`min-w-0 flex-1 truncate text-aui-2xs ${status === "waiting" ? "text-aui-accent" : "text-aui-muted"}`}
          >
            {inline}
          </span>
        )}
      </span>
      {task?.durationMs !== undefined && (
        <span className="shrink-0 text-aui-2xs text-aui-muted tabular-nums">
          {(task.durationMs / 1000).toFixed(1)}s
        </span>
      )}
    </>
  );

  const controls = decision && status === "waiting" && (
    <DecisionControls id={id} title={title} decision={decision} />
  );

  // Another agent's step is that agent, not a tool row. A gated one keeps its controls.
  if (subagent && !controls) {
    return (
      <li className={li} data-status={status}>
        <AgentItem
          title={title}
          status={status}
          output={output}
          subagent={subagent}
          turnId={turn?.id ?? ""}
        />
        <ItemSlot item={item} />
      </li>
    );
  }

  return (
    <li className={li} data-status={status} data-decision={controls ? "" : undefined}>
      {controls ? (
        <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 py-0.5 text-aui-sm">
          <span className="flex min-w-0 flex-1 items-center gap-2">{head}</span>
          {controls}
        </div>
      ) : hasBody ? (
        <>
          <button
            type="button"
            aria-expanded={open}
            onClick={toggle}
            title={shownDetails}
            className="group -mx-1 flex w-[calc(100%+0.5rem)] min-w-0 items-center gap-2 rounded-aui-sm px-1 py-0.5 text-left text-aui-sm transition-colors hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
          >
            {head}
            <ChevronRightIcon
              size={12}
              strokeWidth={2.5}
              className={`text-aui-muted opacity-0 transition group-hover:opacity-100 group-focus-visible:opacity-100 motion-reduce:transition-none ${open ? "rotate-90 opacity-100" : ""}`}
            />
          </button>
          <Collapse open={open}>
            <div className="ml-[0.4375rem] flex flex-col gap-1 border-aui-border border-l py-0.5 pr-1 pb-1.5 pl-[0.9375rem] text-aui-xs">
              {shownDetails && <p className="text-aui-muted">{shownDetails}</p>}
              {output && (
                <p
                  className={`whitespace-pre-wrap break-words text-aui-xs ${status === "error" ? "text-aui-bad" : "text-aui-fg"}`}
                >
                  {readableToolResult(output)}
                </p>
              )}
              {(task?.input || output) && (
                <details className="mt-2 min-w-0 rounded-aui border border-aui-border p-2">
                  <summary className="min-h-6 cursor-pointer text-aui-2xs text-aui-muted">
                    Technical details
                  </summary>
                  <div className="mt-2 flex gap-3">
                    <button
                      type="button"
                      aria-pressed={technicalTab === "input"}
                      onClick={() => setTechnicalTab("input")}
                      className="min-h-6 rounded-aui px-1 py-0.5 text-aui-2xs aria-pressed:bg-aui-hover"
                    >
                      Input
                    </button>
                    <button
                      type="button"
                      aria-pressed={technicalTab === "output"}
                      onClick={() => setTechnicalTab("output")}
                      className="min-h-6 rounded-aui px-1 py-0.5 text-aui-2xs aria-pressed:bg-aui-hover"
                    >
                      Output
                    </button>
                  </div>
                  <pre className="aui-scroll mt-2 max-h-64 overflow-auto whitespace-pre-wrap break-all font-aui-mono text-aui-2xs">
                    {technicalTab === "input"
                      ? (task?.input ?? "Input was not recorded for this step.")
                      : (output ?? "No output recorded.")}
                  </pre>
                </details>
              )}
              {sources && sources.length > 0 && <SourceChips sources={sources} />}
            </div>
          </Collapse>
        </>
      ) : (
        <div className="flex min-w-0 items-center gap-2 py-0.5 text-aui-sm">{head}</div>
      )}
      <ItemSlot item={item} />
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
 * A plan: one header line that carries the state, then one line per task.
 * No card: a hairline progress rule under the header is the only chrome.
 * Open while running; folds itself once everything is settled.
 */
export function Plan({ title, children }: PrimitiveProps["Plan"]) {
  const statuses = statusesOf(children);
  const total = statuses.length;
  const settled = (s: TaskStatus) => s === "complete" || s === "denied";
  const done = statuses.filter(settled).length;
  const failed = statuses.some((s) => s === "error");
  const running = statuses.some((s) => s === "in_progress");
  const waiting = !running && statuses.some((s) => s === "waiting");
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
      : waiting
        ? "waiting"
        : allDone
          ? "complete"
          : "pending";

  return (
    <section data-aui="plan" aria-label={title} className="flex flex-col">
      <button
        type="button"
        aria-expanded={open}
        onClick={toggle}
        className="group -mx-1 flex min-w-0 items-center gap-2 rounded-aui-sm px-1 py-0.5 text-left transition-colors hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
      >
        {state === "pending" ? (
          <ListChecksIcon size={14} className="text-aui-muted" />
        ) : (
          <StatusIcon status={state} size={14} />
        )}
        <span className="min-w-0 truncate font-medium text-aui-sm">
          {running ? <Shimmer>{title}</Shimmer> : title}
        </span>
        {total > 0 && (
          <span className="flex shrink-0 items-center gap-1.5 text-aui-2xs text-aui-muted tabular-nums">
            <span aria-hidden className="h-0.5 w-10 overflow-hidden rounded-full bg-aui-subtle">
              <span
                className={`block h-full transition-[width] duration-500 motion-reduce:transition-none ${failed ? "bg-aui-bad" : allDone ? "bg-aui-ok" : "bg-aui-accent"}`}
                style={{ width: `${(done / total) * 100}%` }}
              />
            </span>
            {done}/{total}
          </span>
        )}
        <ChevronDownIcon
          size={13}
          className={`ml-auto shrink-0 text-aui-muted opacity-60 transition-transform duration-200 group-hover:opacity-100 motion-reduce:transition-none ${open ? "rotate-180" : ""}`}
        />
      </button>
      <Collapse open={open}>
        <ul className="ml-[0.4375rem] flex flex-col border-aui-border border-l pt-0.5 pl-2.5">
          <InPlan.Provider value={true}>{children}</InPlan.Provider>
        </ul>
      </Collapse>
    </section>
  );
}
