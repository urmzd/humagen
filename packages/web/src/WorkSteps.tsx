import {
  type Copy,
  callLine,
  clipTarget,
  defaultCopy,
  editCounts,
  type LineKind,
  lineKind,
  TaskList,
  toolsLine,
} from "@humagen/components";
import {
  type FileEdit,
  fileEdits,
  formatArgs,
  formatResult,
  type Segment,
  type Task,
  type Thought,
  type Turn,
  timeline,
  toolFailed,
  toolTarget,
} from "@humagen/core";
import { ItemActionsContext, ShimmerVerb } from "@humagen/render-web";
import {
  createContext,
  type MouseEvent,
  type ReactNode,
  useContext,
  useMemo,
  useState,
} from "react";

/**
 * A turn as it happened: words, the steps taken between them, and the calls
 * made to other agents, in the order they came (`timeline` in
 * @humagen/core). Kept under its old name for callers of this package.
 */
export type Block = Segment;

export function interleave(
  text: string,
  tasks: readonly Task[],
  thoughts: readonly Thought[] = [],
): { blocks: Block[]; tail: string; rest: Block[] } {
  const t = timeline({ text, tasks: [...tasks], thoughts: [...thoughts] });
  return { blocks: t.segments, tail: t.tail, rest: t.rest };
}

/**
 * Where a file an agent wrote opens: the conversation's Workspace at that
 * file's changes. A host passes its own; the default reads the conversation
 * from the address the reader is on and moves there in place, which the
 * console's router follows (it listens for popstate).
 */
export interface FileLinks {
  href: (path: string) => string | undefined;
  open?: (path: string) => void;
}

/** The Workspace address of a file in a conversation, at its Changes tab. */
export function workspaceFileHref(conversation: string, path: string): string {
  const q = new URLSearchParams({ file: path, tab: "changes" });
  return `/conversations/${encodeURIComponent(conversation)}/workspace?${q}`;
}

function conversationHere(): string | undefined {
  if (typeof window === "undefined") return undefined;
  const m = /^\/conversations\/([^/?#]+)/.exec(window.location.pathname);
  return m?.[1] ? decodeURIComponent(m[1]) : undefined;
}

const addressLinks: FileLinks = {
  href: (path) => {
    const id = conversationHere();
    return id ? workspaceFileHref(id, path) : undefined;
  },
  open: (path) => {
    const id = conversationHere();
    if (!id) return;
    try {
      window.history.pushState(null, "", workspaceFileHref(id, path));
      window.dispatchEvent(new PopStateEvent("popstate"));
    } catch {
      window.location.assign(workspaceFileHref(id, path));
    }
  },
};

export const FileLinkContext = createContext<FileLinks>(addressLinks);

/** A plain click opens in place; a modified one is left to the browser (a new tab). */
function onOpen(links: FileLinks, path: string) {
  return (e: MouseEvent<HTMLAnchorElement>) => {
    if (!links.open || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey || e.button !== 0) return;
    e.preventDefault();
    links.open(path);
  };
}

const icons: Record<LineKind | "file", ReactNode> = {
  search: (
    <>
      <circle cx="11" cy="11" r="7" />
      <path d="m20 20-3.5-3.5" />
    </>
  ),
  fetch: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M3 12h18M12 3a14 14 0 0 1 0 18M12 3a14 14 0 0 0 0 18" />
    </>
  ),
  read: (
    <>
      <path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z" />
      <path d="M14 3v5h5" />
    </>
  ),
  lookup: <path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" />,
  memory: (
    <path d="M12 5a3 3 0 0 0-5.9.8A3 3 0 0 0 4 11a3 3 0 0 0 2 5.7A3 3 0 0 0 12 19m0-14a3 3 0 0 1 5.9.8A3 3 0 0 1 20 11a3 3 0 0 1-2 5.7A3 3 0 0 1 12 19m0-14v14" />
  ),
  run: <path d="M4 17l6-5-6-5M12 19h8" />,
  write: <path d="M4 20h4L19 9l-4-4L4 16zM14 6l4 4" />,
  file: (
    <>
      <path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z" />
      <path d="M14 3v5h5M9 15l2 2 4-4" />
    </>
  ),
  tool: (
    <path d="M14.7 6.3a4 4 0 0 0-5.4 5.4L3 18l3 3 6.3-6.3a4 4 0 0 0 5.4-5.4l-2.5 2.5-2.4-.6-.6-2.4z" />
  ),
};

function StepIcon({ kind }: { kind: LineKind | "file" }) {
  return (
    <svg
      aria-hidden="true"
      focusable="false"
      width={15}
      height={15}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.75}
      strokeLinecap="round"
      strokeLinejoin="round"
      className="shrink-0"
      data-kind={kind}
    >
      {icons[kind]}
    </svg>
  );
}

function Chevron({ open }: { open: boolean }) {
  return (
    <svg
      aria-hidden="true"
      focusable="false"
      width={12}
      height={12}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={2}
      className={`shrink-0 transition-transform motion-reduce:transition-none ${open ? "rotate-90" : ""}`}
    >
      <path d="m9 18 6-6-6-6" />
    </svg>
  );
}

/** Lines added and removed, the added in the good tone: "+17 -2". */
function Counts({ edit }: { edit: Pick<FileEdit, "added" | "removed" | "change"> }) {
  const [added, removed] = editCounts(edit).split(" ");
  return (
    <span className="shrink-0 font-mono text-aui-2xs tabular-nums">
      <span className="text-aui-ok">{added}</span>
      {removed && <span className="ml-1 text-aui-muted">{removed}</span>}
    </span>
  );
}

const live = (t: Task) => t.status === "in_progress" || t.status === "pending";

function duration(ms?: number): string | undefined {
  // Under a tenth of a second is no time worth reading.
  if (ms === undefined || !Number.isFinite(ms) || ms < 100) return undefined;
  if (ms < 1000) return `${Math.max(0, Math.round(ms))}ms`;
  const s = ms / 1000;
  return s < 60
    ? `${s < 10 ? s.toFixed(1) : Math.round(s)}s`
    : `${Math.floor(s / 60)}m ${Math.round(s % 60)}s`;
}

const PREVIEW_CHARS = 600;
const PREVIEW_LINES = 10;

/** A block of quiet monospace text, cut to a preview with Show more. */
function Quiet({ label, text, copy }: { label: string; text: string; copy: Copy }) {
  const [all, setAll] = useState(false);
  const cut = useMemo(() => {
    const byLines = text.split("\n").slice(0, PREVIEW_LINES).join("\n");
    const short = byLines.length > PREVIEW_CHARS ? byLines.slice(0, PREVIEW_CHARS) : byLines;
    return short.length < text.length ? `${short.trimEnd()}…` : undefined;
  }, [text]);
  return (
    <div className="min-w-0">
      <p className="text-aui-2xs text-aui-muted">{label}</p>
      <pre
        className="mt-0.5 max-h-96 min-w-0 overflow-auto whitespace-pre-wrap break-words rounded-aui-sm bg-aui-hover px-2.5 py-1.5 font-mono text-aui-2xs text-aui-fg/85"
        data-testid="tool-block"
      >
        {all || !cut ? text : cut}
      </pre>
      {cut && (
        <button
          type="button"
          onClick={() => setAll(!all)}
          className="mt-0.5 rounded-aui-sm text-aui-2xs text-aui-muted underline-offset-2 hover:text-aui-fg hover:underline focus-visible:outline-2 focus-visible:outline-aui-accent"
        >
          {all ? copy.conversation.toolLine.showLess : copy.conversation.toolLine.showMore}
        </button>
      )}
    </div>
  );
}

const rowClass =
  "group/row flex min-h-7 pointer-coarse:min-h-11 w-full min-w-0 items-center gap-2 rounded-aui-sm px-1 text-left text-aui-muted text-aui-sm hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent";

/**
 * One tool call as a compact row: an icon for its kind, what it was, and the
 * command or target in quiet monospace. A row for a file opens the file in
 * the Workspace; any other row opens to the call's arguments and result.
 */
function ToolRow({
  turn,
  task,
  edit,
  copy,
}: {
  turn: Turn;
  task: Task;
  edit?: FileEdit;
  copy: Copy;
}) {
  const links = useContext(FileLinkContext);
  const itemActions = useContext(ItemActionsContext);
  const [open, setOpen] = useState(false);
  const w = copy.conversation.toolLine;
  const failed = toolFailed(task);
  const running = live(task);
  const target = edit ? undefined : toolTarget(task);
  const label = edit ? callLine(task, edit, copy) : task.title;
  const href = edit ? links.href(edit.path) : undefined;
  const took = duration(task.durationMs);
  const args = formatArgs(task);
  const result = formatResult(task.output);
  const detailId = `tool-${turn.id}-${task.id}`;
  const status = (
    <>
      {task.status === "waiting" && <span className="shrink-0 text-aui-accent">{w.waiting}</span>}
      {failed && <span className="shrink-0 text-aui-muted">{w.failed(1)}</span>}
      {took && !running && (
        <span className="ml-auto shrink-0 text-aui-2xs text-aui-muted tabular-nums">{took}</span>
      )}
    </>
  );
  const body = (
    <>
      <StepIcon kind={edit ? "file" : lineKind(task)} />
      <span className="shrink-0 truncate text-aui-fg/90">
        {running ? <ShimmerVerb>{label}</ShimmerVerb> : label}
      </span>
      {edit && <Counts edit={edit} />}
      {target && (
        <span className="min-w-0 flex-1 truncate font-mono text-aui-2xs text-aui-muted">
          {clipTarget(target, 200)}
        </span>
      )}
      {!target && <span className="flex-1" />}
      {status}
    </>
  );
  return (
    <li className="min-w-0" data-testid="tool-row" data-tool={task.tool}>
      <div className="flex min-w-0 items-center">
        {href && edit ? (
          <a
            href={href}
            onClick={onOpen(links, edit.path)}
            aria-label={w.openFile(edit.path)}
            className={rowClass}
          >
            {body}
          </a>
        ) : (
          <button
            type="button"
            aria-expanded={open}
            aria-controls={detailId}
            onClick={() => setOpen(!open)}
            className={rowClass}
          >
            {body}
          </button>
        )}
        {href && (
          <button
            type="button"
            aria-expanded={open}
            aria-controls={detailId}
            aria-label={w.details(label)}
            onClick={() => setOpen(!open)}
            className="flex size-7 pointer-coarse:size-11 shrink-0 items-center justify-center rounded-aui-sm text-aui-muted hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent"
          >
            <Chevron open={open} />
          </button>
        )}
        {itemActions && (
          <span className="shrink-0 opacity-0 group-hover/message:opacity-100 focus-within:opacity-100">
            {itemActions({ kind: "task", turn, task })}
          </span>
        )}
      </div>
      {open && (
        <div id={detailId} className="mt-1 mb-2 ml-6 flex min-w-0 flex-col gap-2">
          {args && <Quiet label={w.arguments} text={args} copy={copy} />}
          {result ? (
            <Quiet label={w.result} text={result} copy={copy} />
          ) : (
            !running && <p className="text-aui-2xs text-aui-muted">{w.noResult}</p>
          )}
        </div>
      )}
    </li>
  );
}

/**
 * Steps taken between two parts of an answer, as one quiet line. Open, it
 * lists each call as a row. A run that wrote one file is that file: the line
 * opens it in the Workspace, and the chevron beside it lists the calls. A
 * step waiting on a person keeps the run open; a failed one is counted in the
 * line, in the same quiet tone, since nobody needs to act on it.
 */
export function StepGroup({
  turn,
  tasks,
  copy = defaultCopy,
}: {
  turn: Turn;
  tasks: Task[];
  copy?: Copy;
}) {
  const links = useContext(FileLinkContext);
  const waiting = tasks.some((t) => t.status === "waiting");
  const [open, setOpen] = useState<boolean | null>(null);
  const shown = open ?? waiting;
  const edits = useMemo(() => fileEdits(turn.tasks), [turn.tasks]);
  const line = useMemo(() => toolsLine(tasks, copy, edits), [tasks, copy, edits]);
  const w = copy.conversation.toolLine;
  const first = tasks[0];
  const kind = line.file ? "file" : first ? lineKind(first) : "tool";
  // Calls waiting on a person, or with their own controls or sources, keep the full task row.
  const full = (t: Task) =>
    t.status === "waiting" || t.interruptId !== undefined || (t.sources?.length ?? 0) > 0;
  const href = line.file ? links.href(line.file.path) : undefined;
  const text = (
    <>
      <StepIcon kind={kind} />
      <span className="min-w-0 truncate">
        {line.running ? <ShimmerVerb>{line.text}</ShimmerVerb> : line.text}
      </span>
      {line.counts && !line.running && line.file && <Counts edit={line.file} />}
      {line.waiting && <span className="shrink-0 text-aui-accent">{w.waiting}</span>}
      {line.failed > 0 && (
        <span className="shrink-0 text-aui-muted">| {w.failed(line.failed)}</span>
      )}
    </>
  );
  const lineClass =
    "flex max-w-full min-w-0 pointer-coarse:min-h-11 items-center gap-2 rounded-aui-sm px-1 py-0.5 text-left text-aui-muted text-aui-sm hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent";
  return (
    <div className="min-w-0" data-testid="step-group">
      <div className="flex min-w-0 items-center gap-0.5">
        {href && line.file ? (
          <a
            href={href}
            onClick={onOpen(links, line.file.path)}
            className={lineClass}
            aria-label={w.openFile(line.file.path)}
          >
            {text}
          </a>
        ) : (
          <button
            type="button"
            aria-expanded={shown}
            onClick={() => setOpen(!shown)}
            className={lineClass}
          >
            {text}
            <Chevron open={shown} />
          </button>
        )}
        {href && (
          <button
            type="button"
            aria-expanded={shown}
            aria-label={w.details(line.text)}
            onClick={() => setOpen(!shown)}
            className="flex size-6 pointer-coarse:size-11 shrink-0 items-center justify-center rounded-aui-sm text-aui-muted hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent"
          >
            <Chevron open={shown} />
          </button>
        )}
      </div>
      {shown && (
        <ul className="mt-0.5 ml-[0.4375rem] flex min-w-0 flex-col border-aui-border border-l pl-2">
          {tasks.map((t) =>
            full(t) ? (
              <li key={t.id} className="min-w-0">
                <TaskList turn={{ ...turn, planTitle: undefined, tasks: [t] }} copy={copy} />
              </li>
            ) : (
              <ToolRow key={t.id} turn={turn} task={t} edit={edits.get(t.id)} copy={copy} />
            ),
          )}
        </ul>
      )}
    </div>
  );
}

/** "Worked for 32s" over a finished turn's steps, which folds them away. */
export function WorkedFor({
  ms,
  open,
  onToggle,
  copy = defaultCopy,
}: {
  ms?: number;
  open: boolean;
  onToggle: () => void;
  copy?: Copy;
}) {
  const s = ms === undefined ? undefined : Math.max(1, Math.round(ms / 1000));
  const time = s === undefined ? "" : s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${s % 60}s`;
  return (
    <button
      type="button"
      aria-expanded={open}
      onClick={onToggle}
      className="group flex w-full min-w-0 pointer-coarse:min-h-11 items-center gap-1.5 border-aui-border border-b pb-1.5 text-left text-aui-muted text-aui-sm hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent"
    >
      {time ? copy.conversation.workedFor(time) : copy.conversation.worked}
      <svg
        aria-hidden="true"
        focusable="false"
        width={13}
        height={13}
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth={2}
        className={`transition-transform motion-reduce:transition-none ${open ? "rotate-180" : ""}`}
      >
        <path d="M6 9l6 6 6-6" />
      </svg>
    </button>
  );
}
