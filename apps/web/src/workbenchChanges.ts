/**
 * What a session changed in its working files, for the workbench's Changes
 * tab. Pure; no React and no requests.
 *
 * Three things are known, and nothing else is claimed:
 *
 * - what each run left in the session's files, by path and content hash
 *   (`/api/threads/{id}/sandboxes`),
 * - the files as they are now (`/api/threads/{id}/files`),
 * - the whole text of each `file_write` call, from the session's trace.
 *
 * The platform keeps no copy of a file from before a write. So "what was
 * there before" is the text an earlier call of the same session wrote, when
 * there is one, and otherwise nothing: a file with one known text is drawn
 * whole, as added.
 */
import type { ResponseState, ResponseTool } from "@humagen/core";
import { compareText, type ReviewLine } from "@humagen/web";
import type {
  ConversationSandboxes,
  WorkspaceFileList,
} from "../../../packages/client/src/generated/types.ts";
import type { WorkbenchSession } from "./workbench.ts";

/** A whole text a call of the session wrote to a path. */
export interface WrittenText {
  path: string;
  text: string;
  /** The agent whose call it was, when the trace says. */
  agent?: string;
  /** The call's id in the trace. */
  call: string;
}

/** One file the session changed. */
export interface ChangedFile {
  path: string;
  /**
   * `added`: in the session's files, with no earlier text known.
   * `changed`: written more than once, with different text.
   * `removed`: a run removed it, or it is no longer among the files.
   */
  state: "added" | "changed" | "removed";
  /** The agent whose run last wrote or removed it. Absent when a person removed it. */
  agent?: string;
  /** When the last run to touch it started, as an ISO time. */
  at?: string;
  /** The file as it is now. Absent when it is gone. */
  size?: number;
  revision?: string;
  /** Each different text the session's calls wrote, oldest first. */
  texts: WrittenText[];
  /**
   * Every content hash a run recorded leaving at the path. A text from the
   * trace whose hash is among them is the file exactly as a run left it.
   */
  recorded: string[];
  /** A run could not copy its change back because someone changed the file first. */
  conflict: boolean;
}

export interface SessionChanges {
  files: ChangedFile[];
  /** Whether the reader may change the session's files now, and why not. */
  writable: boolean;
  reason?: string;
  /** Runs or files past what the server lists, so the list may be short. */
  partial: boolean;
}

/** A file's text is read on request above this size, never on opening it. */
export const LARGE_FILE_BYTES = 256 * 1024;
/** Unchanged lines kept on each side of a change; longer runs are folded. */
export const CONTEXT_LINES = 3;
/** The rows of a comparison drawn at once. More are drawn on request. */
export const DIFF_PAGE = 300;

/** A path as the server keeps it: no leading "./" or "/". */
export function cleanPath(path: string): string {
  return path
    .trim()
    .replace(/^(\.\/)+/, "")
    .replace(/^\/+/, "");
}

const text = (v: unknown): string | undefined => (typeof v === "string" ? v : undefined);

/**
 * The whole text a call wrote, when it is a successful `file_write` shape:
 * a path and the file's whole content. An edit by old and new fragments, a
 * patch, and a native Office file (whose content is not its bytes) are left
 * out: none of them is the file's text.
 */
export function writtenText(tool: ResponseTool, agent?: string): WrittenText | undefined {
  if (tool.status !== "done" || tool.result?.isError || tool.hidden) return undefined;
  const name = tool.name.toLowerCase();
  if (!/write|create|save/.test(name)) return undefined;
  if (/publish|artifact|comment|issue|pull|message|post|send|memory|scratch/.test(name))
    return undefined;
  const input = tool.input ?? {};
  const format = text(input.format);
  if (format && format !== "text") return undefined;
  const path = text(input.path) ?? text(input.file_path) ?? text(input.file);
  const content = text(input.content) ?? text(input.text);
  if (!path?.trim() || content === undefined) return undefined;
  return { path: cleanPath(path), text: content, call: tool.id, ...(agent ? { agent } : {}) };
}

const written = new WeakMap<readonly ResponseState[], WrittenText[]>();

/** Every whole text the session's calls wrote, in the order they were written. */
export function sessionWrites(responses: readonly ResponseState[] | undefined): WrittenText[] {
  if (!responses) return [];
  const known = written.get(responses);
  if (known) return known;
  const out: WrittenText[] = [];
  // A call under an agent call is that agent's; the rest are the response's own.
  const visit = (blocks: ResponseState["content"], agent: string | undefined) => {
    for (const block of blocks) {
      if (block?.type !== "tool") continue;
      const w = writtenText(block, agent);
      if (w) out.push(w);
      visit(block.content, block.kind === "agent" ? (block.agent ?? agent) : agent);
    }
  };
  for (const response of responses) visit(response.content, response.agent);
  written.set(responses, out);
  return out;
}

/**
 * Whether the session has anything for the Changes tab, by what the pane
 * already holds: a call that wrote a file, or a sandbox its runs worked in.
 */
export function changesAvailable(session: WorkbenchSession): boolean {
  return (
    sessionWrites(session.responses).length > 0 ||
    (session.summary?.environment.some((e) => e.kind === "sandbox") ?? false)
  );
}

/**
 * The files the session changed: every path a run recorded and every path a
 * call wrote whole, each once, the most recently touched first.
 */
export function sessionChanges(
  sandboxes: ConversationSandboxes | undefined,
  listing: WorkspaceFileList | undefined,
  writes: readonly WrittenText[],
): SessionChanges {
  const now = new Map((listing?.files ?? []).map((f) => [f.path, f]));
  type Seen = {
    agent?: string;
    at?: string;
    removed: boolean;
    conflict: boolean;
    recorded: string[];
  };
  const seen = new Map<string, Seen>();
  let partial = !!sandboxes?.more;
  // Newest run first, so the first record of a path is its latest.
  for (const run of sandboxes?.runs ?? []) {
    if (run.more) partial = true;
    for (const file of run.files) {
      const path = cleanPath(file.path);
      const was = seen.get(path);
      if (was) {
        if (file.revision) was.recorded.push(file.revision);
        continue;
      }
      seen.set(path, {
        agent: run.agent,
        at: run.created_at,
        removed: !!file.removed,
        conflict: false,
        recorded: file.revision ? [file.revision] : [],
      });
    }
    for (const path of run.conflicts ?? []) {
      const was = seen.get(cleanPath(path));
      if (was) was.conflict = true;
    }
  }
  const texts = new Map<string, WrittenText[]>();
  for (const w of writes) {
    const list = texts.get(w.path) ?? [];
    // Writing the same text twice changed nothing.
    if (list[list.length - 1]?.text !== w.text) list.push(w);
    texts.set(w.path, list);
    if (!seen.has(w.path))
      seen.set(w.path, { agent: w.agent, removed: false, conflict: false, recorded: [] });
  }
  const files: ChangedFile[] = [];
  for (const [path, s] of seen) {
    const current = now.get(path);
    // Until the files are listed, a path is taken to be where its run left it.
    const gone = listing ? !current : s.removed;
    const versions = texts.get(path) ?? [];
    files.push({
      path,
      state: gone ? "removed" : versions.length > 1 ? "changed" : "added",
      texts: versions,
      recorded: s.recorded,
      conflict: s.conflict,
      // A file that is gone though no run removed it was removed by a person.
      ...(s.agent && (!gone || s.removed) ? { agent: s.agent } : {}),
      ...(s.at ? { at: s.at } : {}),
      ...(current ? { size: current.size, revision: current.revision } : {}),
    });
  }
  files.sort((a, b) => (b.at ?? "").localeCompare(a.at ?? "") || a.path.localeCompare(b.path));
  return {
    files,
    writable: !!listing?.writable,
    ...(listing?.reason ? { reason: listing.reason } : {}),
    partial,
  };
}

/** What two texts of a file are compared as. */
export type Compare = "all" | "last";

/**
 * The two texts a comparison draws. `all` is from nothing known to the file
 * as it is; `last` is from the text before the latest change to the file as
 * it is. `current` is the file's text now, undefined when it is gone.
 * Returns undefined when there are not two texts for `last`.
 */
export function comparison(
  file: Pick<ChangedFile, "texts">,
  current: string | undefined,
  compare: Compare,
): { before: string; after: string } | undefined {
  const chain = file.texts.map((t) => t.text);
  if (current !== undefined && chain[chain.length - 1] !== current) chain.push(current);
  if (current === undefined) {
    // Removed: what the session last wrote, against nothing.
    const last = chain[chain.length - 1];
    return last === undefined ? undefined : { before: last, after: "" };
  }
  if (compare === "all") return { before: "", after: current };
  const before = chain[chain.length - 2];
  return before === undefined ? undefined : { before, after: current };
}

/** The text before the latest change, when the session wrote one. */
export function previousText(
  file: Pick<ChangedFile, "texts">,
  current: string,
): WrittenText | undefined {
  const versions = file.texts;
  const last = versions[versions.length - 1];
  return last && last.text !== current ? last : versions[versions.length - 2];
}

/** A row of a drawn comparison: a line, or a run of unchanged lines folded away. */
export type DiffRow = ReviewLine | { kind: "fold"; count: number };

export interface FileDiff {
  rows: DiffRow[];
  added: number;
  removed: number;
  /** The comparison was cut at the renderer's bound. */
  truncated: boolean;
  /** Why the texts could not be compared (too large, too many lines). */
  error?: string;
}

/**
 * Two texts as rows to draw: added and removed lines with their numbers, a
 * few unchanged lines around each change, and longer unchanged runs folded
 * into one row that says how many. Bounded by `compareText`. Whether a file
 * ends with a line break is not compared.
 */
export function fileDiff(before: string, after: string, path: string): FileDiff {
  // A file's last line break ends its last line; it is not a line of its own.
  const whole = (s: string) => (s.endsWith("\n") ? s.slice(0, -1) : s);
  const diff = compareText(whole(before), whole(after), path);
  if (diff.error) return { rows: [], added: 0, removed: 0, truncated: false, error: diff.error };
  const file = diff.files[0];
  if (!file) return { rows: [], added: 0, removed: 0, truncated: false };
  const lines = file.lines;
  const keep = new Uint8Array(lines.length);
  for (let i = 0; i < lines.length; i++) {
    if (lines[i]?.kind === "context") continue;
    const from = Math.max(0, i - CONTEXT_LINES);
    const to = Math.min(lines.length - 1, i + CONTEXT_LINES);
    for (let j = from; j <= to; j++) keep[j] = 1;
  }
  const rows: DiffRow[] = [];
  let folded = 0;
  for (let i = 0; i < lines.length; i++) {
    if (keep[i]) {
      if (folded) rows.push({ kind: "fold", count: folded });
      folded = 0;
      rows.push(lines[i] as ReviewLine);
    } else folded++;
  }
  if (folded) rows.push({ kind: "fold", count: folded });
  return {
    rows,
    added: file.additions,
    removed: file.deletions,
    truncated: !!diff.truncated,
  };
}

/**
 * A comparison as plain text for the agent to read: the changed lines with
 * `+` and `-`, cut to `max` characters with a line that says so. The start
 * is kept, since a reader finds the place by it.
 */
export function diffText(diff: FileDiff, max = 560): string {
  const out: string[] = [];
  let size = 0;
  let left = 0;
  for (const row of diff.rows) {
    if (row.kind !== "add" && row.kind !== "remove") continue;
    const n = row.kind === "add" ? row.newLine : row.oldLine;
    const line = `${row.kind === "add" ? "+" : "-"}${n ?? ""}: ${row.text}`;
    if (left || size + line.length + 1 > max) {
      left++;
      continue;
    }
    out.push(line);
    size += line.length + 1;
  }
  if (left) out.push(`(${left} more changed lines)`);
  return out.join("\n");
}

/** The SHA-256 of a text's UTF-8 bytes, as the server writes a file's revision. */
export async function revisionOf(content: string): Promise<string> {
  const data = new TextEncoder().encode(content);
  const hash = await crypto.subtle.digest("SHA-256", data);
  return [...new Uint8Array(hash)].map((b) => b.toString(16).padStart(2, "0")).join("");
}
