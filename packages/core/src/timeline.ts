import type { Task, Thought, Turn } from "./types";

/**
 * A turn as a transcript: words, thinking, runs of tool calls and calls to
 * other agents, in the order they happened. Every surface draws the same
 * segments; only how each looks differs.
 *
 * Order comes from the stream itself. The reducer records, for every step, the
 * length of the answer when the step first appeared (`Task.at`), and for every
 * piece of thinking the length of the answer and the number of steps so far
 * (`Thought.at`, `Thought.after`). Cutting the answer at those places puts each
 * step back between the words it came between, across every model step.
 */
export type Segment =
  | { kind: "text"; key: string; text: string }
  | { kind: "thought"; key: string; text: string; last: boolean }
  | { kind: "tools"; key: string; tasks: Task[] }
  | { kind: "call"; key: string; task: Task };

/** A call to another agent, as a turn's timeline holds it. */
export type CallSegment = Extract<Segment, { kind: "call" }>;

/**
 * Calls to other agents that ran at the same time, as one group where they
 * started: a turn that asks two agents at once shows them side by side, not
 * as one after the other.
 */
export interface Together {
  kind: "together";
  key: string;
  calls: CallSegment[];
}

/** When a call ended, in milliseconds, or Infinity while it has not. */
function callEnd(task: Task): number | undefined {
  if (!task.startedAt) return undefined;
  const start = Date.parse(task.startedAt);
  if (!Number.isFinite(start)) return undefined;
  const ms = task.subagent?.durationMs ?? task.durationMs;
  if (task.status === "in_progress" || task.status === "waiting" || ms === undefined)
    return Number.POSITIVE_INFINITY;
  return start + ms;
}

/**
 * A turn's segments with the calls that overlapped in time put together.
 * Two calls next to each other are together when the second started before
 * the first ended. A call with no start time is taken as it comes, alone:
 * nothing says it ran beside another. Everything else keeps its place.
 */
export function withCallsTogether(segments: readonly Segment[]): (Segment | Together)[] {
  const out: (Segment | Together)[] = [];
  let group: CallSegment[] = [];
  let ends = Number.NEGATIVE_INFINITY;
  const flush = () => {
    if (group.length > 1) {
      const first = group[0] as CallSegment;
      out.push({ kind: "together", key: `together-${first.key}`, calls: group });
    } else out.push(...group);
    group = [];
    ends = Number.NEGATIVE_INFINITY;
  };
  for (const s of segments) {
    if (s.kind !== "call") {
      flush();
      out.push(s);
      continue;
    }
    const end = callEnd(s.task);
    const start = s.task.startedAt ? Date.parse(s.task.startedAt) : Number.NaN;
    if (group.length && end !== undefined && Number.isFinite(start) && start < ends) {
      group.push(s);
      ends = Math.max(ends, end);
      continue;
    }
    flush();
    group = [s];
    ends = end ?? Number.NEGATIVE_INFINITY;
  }
  flush();
  return out;
}

export interface Timeline {
  /** Everything before the last words, in order. */
  segments: Segment[];
  /** The words after the last step: the answer, drawn with the rest of the turn. */
  tail: string;
  /** Steps recorded before steps had a place, which go after the answer, as they did. */
  rest: Segment[];
}

type Placed =
  | { at: number; seq: number; task: Task }
  | { at: number; seq: number; thought: Thought; index: number };

function push(into: Segment[], t: Task) {
  const last = into[into.length - 1];
  if (t.subagent) into.push({ kind: "call", key: t.id, task: t });
  else if (last?.kind === "tools") last.tasks.push(t);
  else into.push({ kind: "tools", key: t.id, tasks: [t] });
}

/** A turn's text, thinking and steps as one ordered list. */
export function timeline(turn: Pick<Turn, "text" | "tasks" | "thoughts">): Timeline {
  const { text, tasks } = turn;
  const thoughts = turn.thoughts ?? [];
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
  const segments: Segment[] = [];
  let cursor = 0;
  for (const p of placed) {
    if (p.at > cursor) {
      const words = text.slice(cursor, p.at);
      if (words.trim()) segments.push({ kind: "text", key: `text-${cursor}`, text: words });
      cursor = p.at;
    }
    if ("thought" in p) {
      if (p.thought.text.trim())
        segments.push({
          kind: "thought",
          key: `thought-${p.index}`,
          text: p.thought.text,
          last: p.index === thoughts.length - 1,
        });
      continue;
    }
    push(segments, p.task);
  }
  const rest: Segment[] = [];
  for (const t of tasks) if (t.at === undefined) push(rest, t);
  return { segments, tail: text.slice(cursor), rest };
}

/** What kind of thing a tool call does, from its name. */
export type ToolKind = "search" | "fetch" | "read" | "memory" | "run" | "write" | "tool";

/** What kind of thing a tool does, from its name and title. */
export function toolKind(task: Pick<Task, "tool" | "title">): ToolKind {
  const name = `${task.tool ?? ""} ${task.title}`.toLowerCase();
  if (/fetch|http|url|browse|scrape/.test(name)) return "fetch";
  if (/web|search|google|bing|query|grep|glob|find/.test(name)) return "search";
  if (/memory|recall|remember/.test(name)) return "memory";
  if (/run|exec|shell|bash|command|terminal|script/.test(name)) return "run";
  if (/write|edit|patch|replace|create|update|publish|delete|post|send|comment/.test(name))
    return "write";
  if (/read|file|open|list|view|get|lookup|retriev|knowledge|source|context|status/.test(name))
    return "read";
  return "tool";
}

/** A call's arguments as an object, when they are a JSON object. */
export function toolArgs(task: Pick<Task, "input">): Record<string, unknown> | undefined {
  try {
    const v = JSON.parse(task.input ?? "") as unknown;
    return v && typeof v === "object" && !Array.isArray(v)
      ? (v as Record<string, unknown>)
      : undefined;
  } catch {
    return undefined;
  }
}

const str = (v: unknown) => (typeof v === "string" && v.trim() ? v.trim() : undefined);

/** A GitHub address as people write it: `owner/repo#12`, `owner/repo`, or the address. */
export function shortAddress(address: string): string {
  const m = /^https?:\/\/github\.com\/([^/]+)\/([^/#?]+)(?:\/(?:pull|issues)\/(\d+))?/.exec(
    address,
  );
  if (m) return m[3] ? `${m[1]}/${m[2]}#${m[3]}` : `${m[1]}/${m[2]}`;
  return address.replace(/^https?:\/\//, "");
}

/** The shell command a call ran, when its arguments carry one. */
export function toolCommand(task: Pick<Task, "input">): string | undefined {
  const a = toolArgs(task);
  const c = a?.command ?? a?.cmd ?? a?.script;
  if (Array.isArray(c) && c.every((x) => typeof x === "string")) return c.join(" ");
  return str(c);
}

/** The one thing a call was pointed at, when its arguments name one: a query, a URL, a path. */
export function toolTarget(task: Pick<Task, "input">): string | undefined {
  const a = toolArgs(task);
  if (!a) return undefined;
  const command = toolCommand(task);
  if (command) return command;
  for (const key of [
    "query",
    "q",
    "pattern",
    "url",
    "address",
    "path",
    "file",
    "file_path",
    "name",
    "ref",
  ]) {
    const v = str(a[key]);
    if (v) return key === "address" || key === "url" ? shortAddress(v) : v;
  }
  return undefined;
}

/** The runtime hands a failed action back to the model as text that starts with this. */
const FAILED = /^action failed:/i;

/** Whether a call failed: its status, or a result that says it failed. */
export function toolFailed(task: Pick<Task, "status" | "output">): boolean {
  return task.status === "error" || (task.status === "complete" && FAILED.test(task.output ?? ""));
}

/** A file a call wrote, and how many lines it added and removed. */
export interface FileEdit {
  path: string;
  /** `created` and `updated` when the call says which; `wrote` when it replaced a file it did not show. */
  change: "created" | "updated" | "wrote";
  added: number;
  removed: number;
}

const lines = (s: string) => (s === "" ? 0 : s.replace(/\n$/, "").split("\n").length);

/**
 * Lines added and removed between two texts. Exact (a longest common
 * subsequence) for files of up to 2,000 lines each; beyond that, by counting
 * lines, which is what a reader needs from a count.
 */
export function lineDiff(before: string, after: string): { added: number; removed: number } {
  const a = before === "" ? [] : before.replace(/\n$/, "").split("\n");
  const b = after === "" ? [] : after.replace(/\n$/, "").split("\n");
  if (a.length > 2000 || b.length > 2000 || a.length * b.length > 1_000_000) {
    const left = new Map<string, number>();
    for (const l of a) left.set(l, (left.get(l) ?? 0) + 1);
    let common = 0;
    for (const l of b) {
      const n = left.get(l) ?? 0;
      if (n > 0) {
        common++;
        left.set(l, n - 1);
      }
    }
    return { added: b.length - common, removed: a.length - common };
  }
  let prev = new Array<number>(b.length + 1).fill(0);
  for (let i = 1; i <= a.length; i++) {
    const row = new Array<number>(b.length + 1).fill(0);
    for (let j = 1; j <= b.length; j++)
      row[j] =
        a[i - 1] === b[j - 1]
          ? (prev[j - 1] as number) + 1
          : Math.max(prev[j] as number, row[j - 1] as number);
    prev = row;
  }
  const common = prev[b.length] as number;
  return { added: b.length - common, removed: a.length - common };
}

/**
 * The file a call wrote, when it is a call that writes a workspace file:
 * `file_write` (path, content), and the common edit shapes (old and new
 * text, a whole new file, a patch). `before` is the file's content as an
 * earlier write in the same turn left it, so a second write can say what it
 * changed. A call that failed wrote nothing.
 */
export function fileEdit(
  task: Pick<Task, "tool" | "title" | "input" | "status" | "output" | "subagent">,
  before?: string,
): FileEdit | undefined {
  if (task.subagent || toolFailed(task) || task.status === "denied") return undefined;
  const name = (task.tool ?? "").toLowerCase();
  if (!/write|edit|patch|replace|create|save/.test(name)) return undefined;
  if (/publish|artifact|comment|issue|pull|message|post|send|memory|scratch/.test(name))
    return undefined;
  const a = toolArgs(task);
  if (!a) return undefined;
  const path = str(a.path) ?? str(a.file_path) ?? str(a.file) ?? str(a.filename);
  if (!path) return undefined;
  const oldText =
    typeof a.old_string === "string"
      ? a.old_string
      : typeof a.old_str === "string"
        ? a.old_str
        : undefined;
  const newText =
    typeof a.new_string === "string"
      ? a.new_string
      : typeof a.new_str === "string"
        ? a.new_str
        : undefined;
  if (oldText !== undefined && newText !== undefined) {
    return { path, change: "updated", ...lineDiff(oldText, newText) };
  }
  const patch = str(a.patch) ?? str(a.diff);
  if (patch) {
    let added = 0;
    let removed = 0;
    for (const l of patch.split("\n")) {
      if (l.startsWith("+") && !l.startsWith("+++")) added++;
      else if (l.startsWith("-") && !l.startsWith("---")) removed++;
    }
    return { path, change: "updated", added, removed };
  }
  const content =
    typeof a.content === "string" ? a.content : typeof a.text === "string" ? a.text : undefined;
  if (content === undefined) return undefined;
  if (before !== undefined) return { path, change: "updated", ...lineDiff(before, content) };
  const created = /create|new/.test(name) || /^created\b/i.test(task.output ?? "");
  return { path, change: created ? "created" : "wrote", added: lines(content), removed: 0 };
}

/**
 * Every file each call wrote, by call id. A later write to a path an earlier
 * call in the turn wrote is counted against what that call left there.
 */
export function fileEdits(tasks: readonly Task[]): Map<string, FileEdit> {
  const out = new Map<string, FileEdit>();
  const content = new Map<string, string>();
  for (const t of tasks) {
    const edit = fileEdit(t, undefined);
    if (!edit) continue;
    const a = toolArgs(t);
    const whole = typeof a?.content === "string" ? a.content : undefined;
    const known = content.get(edit.path);
    out.set(t.id, whole !== undefined && known !== undefined ? (fileEdit(t, known) ?? edit) : edit);
    if (whole !== undefined) content.set(edit.path, whole);
    else content.delete(edit.path);
  }
  return out;
}

/** A call's arguments for reading: the command it ran, or its JSON, indented. */
export function formatArgs(task: Pick<Task, "input">): string {
  const command = toolCommand(task);
  const a = toolArgs(task);
  if (command && a && Object.keys(a).length === 1) return command;
  if (a) return JSON.stringify(a, null, 2);
  return (task.input ?? "").trim();
}

/** A call's result for reading: JSON indented, other text as it is. */
export function formatResult(output: string | undefined): string {
  const raw = (output ?? "").trim();
  if (!/^[[{]/.test(raw)) return raw;
  try {
    return JSON.stringify(JSON.parse(raw), null, 2);
  } catch {
    return raw;
  }
}

/** One file a turn changed, with its lines added and removed across the turn. */
export interface FileChange extends FileEdit {
  /** The calls that wrote it, in order. */
  calls: string[];
}

/**
 * Every file a turn wrote, once each, in the order each was first written,
 * from the same detection as each call's own line (`fileEdits`). A file
 * written whole more than once counts from what the turn started with to
 * what it left; edits counted by their old and new text add up.
 */
export function turnChanges(tasks: readonly Task[]): FileChange[] {
  const edits = fileEdits(tasks);
  const out = new Map<string, FileChange & { first?: string; last?: string; whole: boolean }>();
  for (const t of tasks) {
    const edit = edits.get(t.id);
    if (!edit) continue;
    const a = toolArgs(t);
    const content = typeof a?.content === "string" ? a.content : undefined;
    const was = out.get(edit.path);
    if (!was) {
      out.set(edit.path, {
        ...edit,
        calls: [t.id],
        whole: content !== undefined,
        // A file the turn created, or wrote whole without showing what was
        // there, counts from nothing.
        first: edit.change === "updated" ? undefined : "",
        last: content,
      });
      continue;
    }
    was.calls.push(t.id);
    was.whole &&= content !== undefined;
    was.last = content;
    was.added += edit.added;
    was.removed += edit.removed;
    if (was.change === "wrote" && edit.change === "updated") was.change = "updated";
  }
  return [...out.values()].map(({ first, last, whole, ...change }) =>
    whole && first !== undefined && last !== undefined && change.calls.length > 1
      ? { ...change, ...lineDiff(first, last) }
      : change,
  );
}

const FENCE = /^ {0,3}(`{3,}|~{3,})(.*)$/;

/** Whether `text` ends inside a code fence. A fence closes only on its own character, at least as long, with nothing after it. */
function inFence(text: string): boolean {
  let open: { ch: string; len: number } | null = null;
  for (const line of text.split("\n")) {
    const m = FENCE.exec(line);
    if (!m) continue;
    const mark = m[1] as string;
    if (!open) open = { ch: mark[0] as string, len: mark.length };
    else if (mark[0] === open.ch && mark.length >= open.len && !(m[2] ?? "").trim()) open = null;
  }
  return open !== null;
}

/** A line that a following line of the same kind continues: a list item, a quote or a table row. */
const CONTINUED = /^\s*([-*+]\s|\d+[.)]\s|>|\|)/;
/** What a continuing line may start with. */
const CONTINUES = /^[-*+>|0-9]/;

/** Whether a step inside this (unfinished) line would cut a link or an inline code span. */
function midConstruct(line: string): boolean {
  if ((line.match(/`/g) ?? []).length % 2 === 1) return true;
  return /\[[^\]]*$/.test(line) || /\]\([^)]*$/.test(line);
}

/**
 * An answer's words with a paragraph break wherever a step came between them
 * (`Task.at`), for a surface that shows the words as one text, such as Slack:
 * what an agent said before a call and what it said after it read as two
 * paragraphs, not one run-on line ("on the web.Here are"). The break is
 * exactly one blank line. None is added where the words already break, inside
 * a code fence, inside a table row, or where a list, a quote or a table goes
 * on after the step; a step inside a link, an inline code span or a word
 * moves its break to the end of that line. Each choice depends only on the
 * words before the step and the first character after it, so the result for a
 * prefix of the text is a prefix of the result for all of it, apart from
 * trailing whitespace: a stream can keep appending to it.
 */
export function paragraphsAtSteps(text: string, tasks: readonly Pick<Task, "at">[]): string {
  const cuts = [
    ...new Set(
      tasks
        .map((t) => t.at)
        .filter((at): at is number => at !== undefined && at > 0 && at < text.length),
    ),
  ].sort((a, b) => a - b);
  let out = "";
  let from = 0;
  for (let i = 0; i < cuts.length; i++) {
    const at = cuts[i] as number;
    if (at < from) continue;
    out += text.slice(from, at);
    from = at;
    const rest = text.slice(at);
    if (!out.trim() || /\n[ \t]*\n[ \t]*$/.test(out) || /^[ \t]*\n[ \t]*\n/.test(rest)) continue;
    if (inFence(out)) continue;
    const line = out.slice(out.lastIndexOf("\n") + 1);
    const next = rest.trimStart();
    if (!out.endsWith("\n") && !rest.startsWith("\n")) {
      // Inside a line: never in a table row; in a link, code or a word, at the line's end.
      if (/^\s*\|/.test(line)) continue;
      const word = /\w$/.test(out) && /^\w/.test(rest);
      if (midConstruct(line) || word) {
        const end = text.indexOf("\n", at);
        if (end > at && !cuts.includes(end)) {
          cuts.splice(i + 1, 0, end);
          cuts.sort((a, b) => a - b);
        }
        continue;
      }
    }
    const last = out.trimEnd().split("\n").at(-1) ?? "";
    if (next && CONTINUED.test(last) && CONTINUES.test(next)) continue;
    out = `${out.replace(/\s+$/, "")}\n\n`;
    from = at + (/^\s*/.exec(rest)?.[0].length ?? 0);
  }
  return out + text.slice(from);
}
