import { type Copy, defaultCopy, TaskList } from "@humagen/components";
import type { Task, Thought, Turn } from "@humagen/core";
import { ShimmerVerb } from "@humagen/render-web";
import { type ReactNode, useState } from "react";

/**
 * A turn as it happened: words, the steps taken between them, and the calls
 * made to other agents, in the order they came. Each step keeps the place in
 * the answer where it started (`Task.at`), so cutting the text at those
 * places puts every step back where it was.
 */
export type Block =
  | { kind: "text"; key: string; text: string }
  | { kind: "thought"; key: string; text: string; last: boolean }
  | { kind: "steps"; key: string; tasks: Task[] }
  | { kind: "call"; key: string; task: Task };

type Placed =
  | { at: number; seq: number; task: Task }
  | { at: number; seq: number; thought: Thought; index: number };

export function interleave(
  text: string,
  tasks: readonly Task[],
  thoughts: readonly Thought[] = [],
): { blocks: Block[]; tail: string; rest: Block[] } {
  const clamp = (n: number | undefined) => Math.min(Math.max(n ?? text.length, 0), text.length);
  // At one place in the text, a thought that began after k steps comes
  // before step k: seq orders them as they happened.
  const placed: Placed[] = [
    ...tasks.flatMap((task, i) =>
      task.at === undefined ? [] : [{ at: clamp(task.at), seq: i * 2 + 1, task }],
    ),
    ...thoughts.map((thought, index) => ({
      at: clamp(thought.at),
      seq: thought.after * 2,
      thought,
      index,
    })),
  ].sort((a, b) => a.at - b.at || a.seq - b.seq);
  const blocks: Block[] = [];
  let cursor = 0;
  for (const p of placed) {
    if (p.at > cursor) {
      const words = text.slice(cursor, p.at);
      if (words.trim()) blocks.push({ kind: "text", key: `text-${cursor}`, text: words });
      cursor = p.at;
    }
    if ("thought" in p) {
      if (p.thought.text.trim())
        blocks.push({
          kind: "thought",
          key: `thought-${p.index}`,
          text: p.thought.text,
          last: p.index === thoughts.length - 1,
        });
      continue;
    }
    const t = p.task;
    const last = blocks[blocks.length - 1];
    if (t.subagent) blocks.push({ kind: "call", key: t.id, task: t });
    else if (last?.kind === "steps") last.tasks.push(t);
    else blocks.push({ kind: "steps", key: t.id, tasks: [t] });
  }
  // A step recorded before steps had a place goes after the answer, as it did.
  const rest: Block[] = [];
  for (const t of tasks) {
    if (t.at !== undefined) continue;
    const last = rest[rest.length - 1];
    if (t.subagent) rest.push({ kind: "call", key: t.id, task: t });
    else if (last?.kind === "steps") last.tasks.push(t);
    else rest.push({ kind: "steps", key: t.id, tasks: [t] });
  }
  return { blocks, tail: text.slice(cursor), rest };
}

export type StepKind = keyof Copy["conversation"]["stepWords"];

/** What kind of thing a tool does, from its name. */
export function stepKind(task: Pick<Task, "tool" | "title">): StepKind {
  const name = `${task.tool ?? ""} ${task.title}`.toLowerCase();
  if (/web|search|google|bing|query/.test(name)) return "search";
  if (/fetch|http|url|browse|scrape/.test(name)) return "fetch";
  if (/memory|recall|remember/.test(name)) return "memory";
  if (/read|file|open|list|glob|grep|get|lookup|retriev|knowledge|source/.test(name)) return "read";
  if (/run|exec|shell|bash|command|terminal|script/.test(name)) return "run";
  if (/write|edit|create|update|publish|delete|post|send|comment/.test(name)) return "write";
  return "tool";
}

/** The one thing a step was pointed at, when its arguments name one: a query, a URL, a path. */
export function stepTarget(task: Pick<Task, "input">): string | undefined {
  try {
    const args = JSON.parse(task.input ?? "") as Record<string, unknown>;
    for (const key of ["query", "q", "url", "path", "file", "command", "name"]) {
      const v = args?.[key];
      if (typeof v === "string" && v.trim()) return v.trim();
    }
  } catch {
    // Arguments that are not JSON name nothing.
  }
  return undefined;
}

const live = (t: Task) => t.status === "in_progress" || t.status === "pending";

/**
 * A run of steps as one line: what they did, in the order they did it
 * ("Read files, searched the web"). A lone step says what it was pointed at.
 */
export function stepsLine(tasks: readonly Task[], copy: Copy = defaultCopy): string {
  const words = copy.conversation.stepWords;
  const phrases: string[] = [];
  for (const t of tasks) {
    // A tool of no known kind is named by its own title, not "used a tool".
    const kind = stepKind(t);
    const w =
      kind === "tool" && t.title
        ? t.title.charAt(0).toLowerCase() + t.title.slice(1)
        : words[kind][live(t) ? "live" : "done"];
    if (!phrases.includes(w)) phrases.push(w);
  }
  const only = tasks.length === 1 ? tasks[0] : undefined;
  const target = only && !live(only) ? stepTarget(only) : undefined;
  const kind = only && stepKind(only);
  const line =
    only && kind && kind !== "tool" && target ? words[kind].one(target) : phrases.join(", ");
  return line.charAt(0).toUpperCase() + line.slice(1);
}

const icons: Record<StepKind, ReactNode> = {
  search: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M3 12h18M12 3a14 14 0 0 1 0 18M12 3a14 14 0 0 0 0 18" />
    </>
  ),
  fetch: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M3 12h18M12 3a14 14 0 0 1 0 18M12 3a14 14 0 0 0 0 18" />
    </>
  ),
  read: <path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" />,
  memory: (
    <path d="M12 5a3 3 0 0 0-5.9.8A3 3 0 0 0 4 11a3 3 0 0 0 2 5.7A3 3 0 0 0 12 19m0-14a3 3 0 0 1 5.9.8A3 3 0 0 1 20 11a3 3 0 0 1-2 5.7A3 3 0 0 1 12 19m0-14v14" />
  ),
  run: <path d="M4 17l6-5-6-5M12 19h8" />,
  write: <path d="M4 20h4L19 9l-4-4L4 16zM14 6l4 4" />,
  tool: (
    <path d="M14.7 6.3a4 4 0 0 0-5.4 5.4L3 18l3 3 6.3-6.3a4 4 0 0 0 5.4-5.4l-2.5 2.5-2.4-.6-.6-2.4z" />
  ),
};

function StepIcon({ kind }: { kind: StepKind }) {
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
    >
      {icons[kind]}
    </svg>
  );
}

/**
 * Steps taken between two parts of an answer, as one quiet line with an icon.
 * Open, it lists each step with what it was given and what it returned. A
 * step waiting on a person or one that failed keeps the group open.
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
  const attention = tasks.some((t) => t.status === "waiting" || t.status === "error");
  const [open, setOpen] = useState<boolean | null>(null);
  const shown = open ?? attention;
  const running = tasks.some(live);
  const failed = tasks.filter((t) => t.status === "error").length;
  const waiting = tasks.some((t) => t.status === "waiting");
  const first = tasks[0];
  const line = stepsLine(tasks, copy);
  return (
    <div className="min-w-0" data-testid="step-group">
      <button
        type="button"
        aria-expanded={shown}
        onClick={() => setOpen(!shown)}
        className="group -mx-1 flex max-w-full min-w-0 pointer-coarse:min-h-11 items-center gap-2 rounded-aui-sm px-1 py-0.5 text-left text-aui-muted text-aui-sm hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent"
      >
        {first && <StepIcon kind={stepKind(first)} />}
        <span className="min-w-0 truncate">
          {running ? <ShimmerVerb>{line}</ShimmerVerb> : line}
        </span>
        {waiting && (
          <span className="shrink-0 text-aui-accent">{copy.conversation.stepNeedsYou}</span>
        )}
        {failed > 0 && (
          <span className="shrink-0 text-aui-bad">{copy.conversation.stepFailed(failed)}</span>
        )}
      </button>
      {shown && (
        <div className="mt-1 ml-[0.4375rem] flex min-w-0 flex-col border-aui-border border-l pl-3">
          <TaskList turn={{ ...turn, planTitle: undefined, tasks }} copy={copy} />
        </div>
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
