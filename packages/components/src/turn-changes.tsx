import { type FileChange, type Task, type TurnStatus, turnChanges } from "@humagen/core";
import { Context } from "@humagen/primitives";
import { type Copy, defaultCopy } from "./copy";
import { editCounts } from "./timeline";

/** The most files the summary names; the rest are counted in its last line. */
export const MAX_CHANGED_FILES = 8;

/** The longest path shown whole. A longer one keeps its end, where the file's name is. */
const MAX_PATH = 72;

/**
 * A path a model wrote, made safe to show as code on one line: no line
 * breaks, nothing that ends the code span or becomes a link or a time, and
 * its start cut when it is long.
 */
export function shownPath(path: string): string {
  const flat = path
    .replace(/\s+/g, " ")
    .replaceAll("`", "ˋ")
    .replaceAll("](", "]​(")
    .replaceAll("{{", "{​{")
    .trim();
  return flat.length > MAX_PATH ? `…${flat.slice(flat.length - MAX_PATH + 1)}` : flat;
}

/** Whether a turn is over, so what it changed can be summed up: it ended, failed or was stopped. */
export function turnSettled(status: TurnStatus): boolean {
  return status === "done" || status === "failed" || status === "stopped";
}

/**
 * The lines of the summary: how many files and their lines in all, then one
 * line per file with its own counts, then how many more there were. Empty
 * when the turn changed no file.
 */
export function changedFilesLines(
  files: readonly FileChange[],
  copy: Copy = defaultCopy,
): string[] {
  if (files.length === 0) return [];
  const w = copy.conversation.changes;
  const added = files.reduce((n, f) => n + f.added, 0);
  const removed = files.reduce((n, f) => n + f.removed, 0);
  const total = editCounts({
    added,
    removed,
    change: files.some((f) => f.change === "updated") ? "updated" : "created",
  });
  const shown = files.length > MAX_CHANGED_FILES ? files.slice(0, MAX_CHANGED_FILES - 1) : files;
  const more = files.length - shown.length;
  return [
    // One file's counts are on its own line already.
    files.length > 1 ? `**${w.title(files.length)}** ${total}` : `**${w.title(1)}**`,
    ...shown.map((f) => `\`${shownPath(f.path)}\` ${editCounts(f)}`),
    ...(more > 0 ? [w.more(more)] : []),
  ];
}

/**
 * The files a finished turn changed, where a surface has no card for them:
 * one muted block under the answer, a line per file. Slack draws this; the
 * web has its own card (`TurnChanges` in `@humagen/web`), from the same
 * `turnChanges`. Nothing when the turn changed no file.
 */
export function ChangedFiles({
  tasks,
  copy = defaultCopy,
}: {
  tasks: readonly Task[];
  copy?: Copy;
}) {
  const lines = changedFilesLines(turnChanges(tasks), copy);
  if (lines.length === 0) return null;
  return <Context items={[lines.join("\n")]} />;
}
