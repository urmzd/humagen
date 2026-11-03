import { defaultCopy } from "@humagen/components/copy";

const words = defaultCopy.editor;
/** Bounded, display-only diffs. These records never authorize or apply a change. */
export interface ReviewLine {
  kind: "context" | "add" | "remove" | "hunk" | "meta";
  text: string;
  oldLine?: number;
  newLine?: number;
}
export interface ReviewFile {
  path: string;
  oldPath: string;
  lines: ReviewLine[];
  additions: number;
  deletions: number;
  binary?: boolean;
}
export interface ReviewDiff {
  files: ReviewFile[];
  error?: string;
  truncated?: boolean;
}
const MAX_BYTES = 2 * 1024 * 1024;
const MAX_LINES = 20000;
const MAX_FILES = 200;
const MAX_RENDER_LINES = 4000;

function bounded(text: string): boolean {
  return text.length <= MAX_BYTES && new TextEncoder().encode(text).length <= MAX_BYTES;
}

function headerPath(text: string): string {
  const raw = text.split("\t", 1)[0] ?? "";
  let path = raw;
  if (raw.startsWith('"')) {
    try {
      path = JSON.parse(raw);
    } catch {
      return raw;
    }
  }
  return path.replace(/^[ab]\//, "");
}

/** Parse standard git/unified patches with hunk counts checked before review. */
export function parseUnifiedDiff(patch: string): ReviewDiff {
  if (!bounded(patch)) return { files: [], error: words.patchTooLarge };
  const rows = patch.replace(/\r\n/g, "\n").split("\n");
  if (rows.length > MAX_LINES) return { files: [], error: words.patchTooManyLines };
  const files: ReviewFile[] = [];
  let current: ReviewFile | undefined;
  let oldPath = "";
  let oldLine = 0;
  let newLine = 0;
  let oldLeft = 0;
  let newLeft = 0;
  let inHunk = false;
  let shown = 0;
  let truncated = false;
  const invalid = (): ReviewDiff => ({
    files: [],
    error: words.invalidPatch,
  });
  const push = (line: ReviewLine) => {
    if (shown++ < MAX_RENDER_LINES) current?.lines.push(line);
    else truncated = true;
  };
  for (let index = 0; index < rows.length; index++) {
    const row = rows[index] ?? "";
    if (inHunk && (oldLeft > 0 || newLeft > 0)) {
      if (row.startsWith("\\ No newline at end of file")) {
        push({ kind: "meta", text: row });
        continue;
      }
      const prefix = row[0];
      if (prefix === " " && oldLeft > 0 && newLeft > 0) {
        push({ kind: "context", text: row.slice(1), oldLine: oldLine++, newLine: newLine++ });
        oldLeft--;
        newLeft--;
      } else if (prefix === "-" && oldLeft > 0) {
        push({ kind: "remove", text: row.slice(1), oldLine: oldLine++ });
        oldLeft--;
        if (current) current.deletions++;
      } else if (prefix === "+" && newLeft > 0) {
        push({ kind: "add", text: row.slice(1), newLine: newLine++ });
        newLeft--;
        if (current) current.additions++;
      } else return invalid();
      continue;
    }
    inHunk = false;
    if (row.startsWith("diff --git ")) {
      current = undefined;
      oldPath = "";
      continue;
    }
    if (row.startsWith("--- ")) {
      oldPath = headerPath(row.slice(4));
      current = undefined;
      continue;
    }
    if (row.startsWith("+++ ")) {
      if (!oldPath || files.length >= MAX_FILES) return invalid();
      const nextPath = headerPath(row.slice(4));
      if (!nextPath) return invalid();
      current = {
        path: nextPath === "/dev/null" ? oldPath : nextPath,
        oldPath,
        lines: [],
        additions: 0,
        deletions: 0,
      };
      files.push(current);
      continue;
    }
    if (row.startsWith("@@")) {
      const match = /^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@(?:.*)$/.exec(row);
      if (!match || !current) return invalid();
      oldLine = Number(match[1]);
      newLine = Number(match[3]);
      oldLeft = match[2] === undefined ? 1 : Number(match[2]);
      newLeft = match[4] === undefined ? 1 : Number(match[4]);
      if (![oldLine, newLine, oldLeft, newLeft].every(Number.isSafeInteger)) return invalid();
      if (oldLeft + newLeft > MAX_LINES * 2) return invalid();
      push({ kind: "hunk", text: row });
      inHunk = true;
      continue;
    }
    if (row.startsWith("Binary files ") || row === "GIT binary patch") {
      if (current) current.binary = true;
      else
        return {
          files: [],
          error: words.binaryPatch,
        };
    }
    if (row.startsWith("\\ No newline at end of file") && current)
      push({ kind: "meta", text: row });
    else if (/^[ +-]/.test(row) && current) return invalid();
  }
  if (oldLeft > 0 || newLeft > 0) return invalid();
  if (!files.length && patch.trim())
    return {
      files: [],
      error: words.noUnifiedDiff,
    };
  return { files, truncated };
}

/**
 * Compare lines with common ends removed. Small middles use LCS; large middles
 * remain one replacement. Memory and work stay bounded for unrelated files.
 */
export function compareText(before: string, after: string, path = "file"): ReviewDiff {
  if (!bounded(before) || !bounded(after)) return { files: [], error: words.comparisonTooLarge };
  if (before === after) return { files: [] };
  const a = before === "" ? [] : before.split("\n");
  const b = after === "" ? [] : after.split("\n");
  if (a.length > MAX_LINES || b.length > MAX_LINES)
    return { files: [], error: words.comparisonTooManyLines };
  let prefix = 0;
  while (prefix < a.length && prefix < b.length && a[prefix] === b[prefix]) prefix++;
  let suffix = 0;
  while (
    suffix < a.length - prefix &&
    suffix < b.length - prefix &&
    a[a.length - suffix - 1] === b[b.length - suffix - 1]
  )
    suffix++;
  const left = a.slice(prefix, a.length - suffix);
  const right = b.slice(prefix, b.length - suffix);
  const lines: ReviewLine[] = [];
  let oldLine = Math.max(0, prefix - 3) + 1;
  let newLine = oldLine;
  const add = (kind: ReviewLine["kind"], text: string) => {
    lines.push({
      kind,
      text,
      ...(kind !== "add" ? { oldLine: oldLine++ } : {}),
      ...(kind !== "remove" ? { newLine: newLine++ } : {}),
    });
  };
  for (const text of a.slice(Math.max(0, prefix - 3), prefix)) add("context", text);
  if (left.length * right.length <= 250000) {
    const width = right.length + 1;
    const lengths = new Uint32Array((left.length + 1) * width);
    for (let i = left.length - 1; i >= 0; i--)
      for (let j = right.length - 1; j >= 0; j--)
        lengths[i * width + j] =
          left[i] === right[j]
            ? (lengths[(i + 1) * width + j + 1] ?? 0) + 1
            : Math.max(lengths[(i + 1) * width + j] ?? 0, lengths[i * width + j + 1] ?? 0);
    let i = 0;
    let j = 0;
    while (i < left.length || j < right.length) {
      if (i < left.length && j < right.length && left[i] === right[j]) {
        add("context", left[i++] ?? "");
        j++;
      } else if (
        i < left.length &&
        (j === right.length ||
          (lengths[(i + 1) * width + j] ?? 0) >= (lengths[i * width + j + 1] ?? 0))
      )
        add("remove", left[i++] ?? "");
      else add("add", right[j++] ?? "");
    }
  } else {
    for (const text of left) add("remove", text);
    for (const text of right) add("add", text);
  }
  for (const text of a.slice(a.length - suffix, a.length - suffix + Math.min(3, suffix)))
    add("context", text);
  const file: ReviewFile = {
    path,
    oldPath: path,
    lines: lines.slice(0, MAX_RENDER_LINES),
    additions: lines.filter((line) => line.kind === "add").length,
    deletions: lines.filter((line) => line.kind === "remove").length,
  };
  return { files: [file], truncated: lines.length > MAX_RENDER_LINES };
}

/** One row of a side-by-side diff: the old line, the new line, or a full-width hunk header. */
export interface SplitRow {
  left?: ReviewLine;
  right?: ReviewLine;
  full?: ReviewLine;
}

/**
 * Pairs a unified diff's lines for a side-by-side view: context on both
 * sides, a run of removals beside the additions that follow it, and hunk
 * headers across both.
 */
export function splitRows(lines: ReviewLine[]): SplitRow[] {
  const rows: SplitRow[] = [];
  let removed: ReviewLine[] = [];
  let added: ReviewLine[] = [];
  const flush = () => {
    for (let i = 0; i < Math.max(removed.length, added.length); i++)
      rows.push({ left: removed[i], right: added[i] });
    removed = [];
    added = [];
  };
  for (const line of lines) {
    if (line.kind === "remove") {
      if (added.length) flush();
      removed.push(line);
    } else if (line.kind === "add") added.push(line);
    else {
      flush();
      if (line.kind === "context") rows.push({ left: line, right: line });
      else rows.push({ full: line });
    }
  }
  flush();
  return rows;
}

/**
 * A note pinned to a line of a compared file, such as a review finding that
 * cites `path:line`. Display only.
 */
export interface ReviewPin {
  path: string;
  line: number;
  text: string;
  severity?: "error" | "warning" | "info";
}
