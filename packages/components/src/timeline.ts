import {
  type FileEdit,
  fileEdits,
  type Task,
  type ToolKind,
  toolFailed,
  toolKind,
  toolTarget,
} from "@humagen/core";
import { type Copy, defaultCopy } from "./copy";

/**
 * Words for a run of tool calls, the same on every surface. Each is a pure
 * function of the calls' names and arguments, so a line reads the same on a
 * reload, in Slack and in a test.
 */

/** How a call reads in a line: a read of a file, or a look at something else. */
export type LineKind = Exclude<ToolKind, "read"> | "read" | "lookup";

/** The kind a call reads as: a read of a file is "read", any other read is "lookup". */
export function lineKind(task: Pick<Task, "tool" | "title" | "input">): LineKind {
  const kind = toolKind(task);
  if (kind !== "read") return kind;
  const name = `${task.tool ?? ""} ${task.title}`.toLowerCase();
  return /file|view|open|cat\b|list/.test(name) ? "read" : "lookup";
}

const MAX_TARGET = 60;

/** A target short enough for one line: a path by its last parts, other text cut with an ellipsis. */
export function clipTarget(target: string, max = MAX_TARGET): string {
  const one = target.replace(/\s+/g, " ").trim();
  if (one.length <= max) return one;
  if (!one.includes(" ") && one.includes("/")) {
    const parts = one.split("/");
    let out = parts.pop() as string;
    while (parts.length && out.length + (parts[parts.length - 1] as string).length + 1 <= max - 2)
      out = `${parts.pop()}/${out}`;
    return `…/${out}`;
  }
  return `${one.slice(0, max - 1)}…`;
}

const live = (t: Task) => t.status === "in_progress" || t.status === "pending";

/** A tool name as words, with a lower-case start, for "Used github comment". */
function toolWords(t: Pick<Task, "tool" | "title">): string {
  const w = t.title || (t.tool ?? "").replace(/[._-]+/g, " ");
  return w.charAt(0).toLowerCase() + w.slice(1);
}

/** The counts after a file's name: "+17 -2", "+42". */
export function editCounts(edit: Pick<FileEdit, "added" | "removed" | "change">): string {
  return edit.change === "updated" || edit.removed > 0
    ? `+${edit.added} -${edit.removed}`
    : `+${edit.added}`;
}

/** What one call did, as a row reads it: "Ran npm test", "Read docker.go", "Updated notes.md". */
export function callLine(task: Task, edit?: FileEdit, copy: Copy = defaultCopy): string {
  const w = copy.conversation.toolLine;
  if (edit) return w[edit.change](clipTarget(edit.path));
  const kind = lineKind(task);
  const target = toolTarget(task);
  // A tool of no known kind is named by its own title.
  if (kind === "tool") {
    const name = task.title || toolWords(task);
    return target ? `${name} ${clipTarget(target, 40)}` : name;
  }
  if (!target) return w[kind].a;
  const shown = kind === "read" ? (target.split("/").pop() ?? target) : target;
  return w[kind].one(clipTarget(shown));
}

/** The run of calls as one line, with what needs saying after it. */
export interface ToolsLine {
  /** "Read 3 files, ran 2 commands"; "Running npm test" while a call runs. */
  text: string;
  /** Calls that failed, said after the line without alarm. */
  failed: number;
  /** A call is waiting on a person. */
  waiting: boolean;
  /** A call is running now. */
  running: boolean;
  /** The one file the run wrote, when that is all it did: the line opens it. */
  file?: FileEdit;
  /** Lines added and removed across every file the run wrote, for "+17 -2". */
  counts?: string;
}

const lower = (s: string) => s.charAt(0).toLowerCase() + s.slice(1);

/**
 * A run of tool calls as one line. While a call runs, the line says which.
 * Once done: a lone call says what it was pointed at ("Read docker.go"), a
 * run of one kind counts ("Ran 3 commands"), a mix names each kind in the
 * order it came ("Read 2 files, ran a command"), and more than three kinds
 * count calls ("Used 6 tools"). Files written are said as files.
 */
export function toolsLine(
  tasks: readonly Task[],
  copy: Copy = defaultCopy,
  /** Files written across the whole turn (`fileEdits(turn.tasks)`), so a later write counts against an earlier one. */
  turnEdits?: Map<string, FileEdit>,
): ToolsLine {
  const w = copy.conversation.toolLine;
  const all = turnEdits ?? fileEdits(tasks);
  const edits = new Map(
    tasks.flatMap((t) => (all.has(t.id) ? [[t.id, all.get(t.id) as FileEdit]] : [])),
  );
  const failed = tasks.filter(toolFailed).length;
  const waiting = tasks.some((t) => t.status === "waiting");
  const running = [...tasks].reverse().find(live);
  const fileList = [...edits.values()];
  const added = fileList.reduce((n, e) => n + e.added, 0);
  const removed = fileList.reduce((n, e) => n + e.removed, 0);
  const counts = fileList.length
    ? editCounts({
        added,
        removed,
        change: fileList.some((e) => e.change === "updated") ? "updated" : "created",
      })
    : undefined;
  const base = { failed, waiting, running: running !== undefined, counts };
  if (waiting) {
    const pending = tasks.find((task) => task.status === "waiting");
    return { ...base, text: `Awaiting approval: ${pending?.title || "tool call"}` };
  }
  if (running) {
    const edit = edits.get(running.id);
    const what = edit ? clipTarget(edit.path) : toolWords(running);
    return { ...base, text: w.running(what) };
  }
  if (tasks.length === 1) {
    const only = tasks[0] as Task;
    const edit = edits.get(only.id);
    return { ...base, text: callLine(only, edit, copy), ...(edit ? { file: edit } : {}) };
  }
  // Calls by kind, in the order each kind first came. Files written count as one kind.
  const order: string[] = [];
  const count = new Map<string, number>();
  for (const t of tasks) {
    const k = edits.has(t.id) ? "file" : lineKind(t);
    if (!count.has(k)) order.push(k);
    count.set(k, (count.get(k) ?? 0) + 1);
  }
  const uniquePaths = new Set(fileList.map((e) => e.path));
  if (order.length === 1 && order[0] === "file" && uniquePaths.size === 1) {
    const last = fileList[fileList.length - 1] as FileEdit;
    return { ...base, text: w.updated(clipTarget(last.path)), file: { ...last, added, removed } };
  }
  if (order.length > 3) return { ...base, text: w.tool.many(tasks.length) };
  const phrases = order.map((k) => {
    const n = count.get(k) as number;
    if (k === "file")
      return n === 1
        ? w[fileList[0]?.change ?? "updated"](clipTarget(fileList[0]?.path ?? ""))
        : w.files(uniquePaths.size);
    const kind = k as LineKind;
    if (n === 1) {
      const t = tasks.find((x) => !edits.has(x.id) && lineKind(x) === kind) as Task;
      return kind === "tool" ? w.tool.one(toolWords(t)) : w[kind].a;
    }
    return w[kind].many(n);
  });
  return { ...base, text: phrases.map((p, i) => (i === 0 ? p : lower(p))).join(", ") };
}
