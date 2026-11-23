import {
  callFailed,
  type FileEdit,
  failedCommands,
  fileEdits,
  recoveredCalls,
  type Task,
  type Turn,
} from "@humagen/core";
import { Context, Terminal } from "@humagen/primitives";
import { type Copy, defaultCopy } from "./copy";
import { clipTarget, editCounts, type LineKind, lineKind } from "./timeline";

/**
 * A finished turn's work in one line, counted by kind in the order each kind
 * first came: "Ran 3 commands (1 failed), read 2 files, updated 4 files
 * (+120 -18), ran 2 searches". The kinds are the ones every tool line uses
 * (`lineKind`, and `fileEdits` for files written), so the count agrees with
 * the steps a person opens in the console. Calls to other agents are not
 * counted here: each keeps its own row.
 */
export function workSummary(
  tasks: readonly Task[],
  copy: Copy = defaultCopy,
  turnEdits?: Map<string, FileEdit>,
  /** Failed calls a later call recovered (`recoveredCalls`): counted as retried, not failed. */
  recovered: ReadonlySet<string> = recoveredCalls(tasks),
): string {
  const w = copy.conversation.toolLine;
  const own = tasks.filter((t) => !t.subagent);
  const all = turnEdits ?? fileEdits(tasks);
  const order: string[] = [];
  type Tally = { n: number; failed: number; retried: number; first: Task };
  const count = new Map<string, Tally>();
  for (const t of own) {
    const k = all.has(t.id) ? "file" : lineKind(t);
    let was = count.get(k);
    if (!was) {
      order.push(k);
      was = { n: 0, failed: 0, retried: 0, first: t };
      count.set(k, was);
    }
    was.n += 1;
    if (recovered.has(t.id)) was.retried += 1;
    else if (callFailed(t)) was.failed += 1;
  }
  const written = own.flatMap((t) => (all.has(t.id) ? [all.get(t.id) as FileEdit] : []));
  const paths = new Set(written.map((e) => e.path));
  const phrases = order.map((k) => {
    const { n, failed, retried, first } = count.get(k) as Tally;
    const notes: string[] = [];
    let phrase: string;
    if (k === "file") {
      const added = written.reduce((sum, e) => sum + e.added, 0);
      const removed = written.reduce((sum, e) => sum + e.removed, 0);
      const updated = written.some((e) => e.change === "updated");
      const one = written[written.length - 1] as FileEdit;
      phrase =
        paths.size === 1
          ? w[n === 1 ? one.change : "updated"](clipTarget(one.path, 40))
          : w.files(paths.size);
      notes.push(editCounts({ added, removed, change: updated ? "updated" : "created" }));
    } else {
      const kind = k as LineKind;
      phrase =
        n > 1
          ? w[kind].many(n)
          : kind === "tool"
            ? w.tool.one(first.title || (first.tool ?? "").replace(/[._-]+/g, " "))
            : w[kind].a;
    }
    if (failed > 0) notes.push(w.failed(failed));
    if (retried > 0) notes.push(w.retried(retried));
    return notes.length ? `${phrase} (${notes.join(", ")})` : phrase;
  });
  return phrases.map((p, i) => (i === 0 ? p : p.charAt(0).toLowerCase() + p.slice(1))).join(", ");
}

/** The most failed commands one message shows in full. Earlier ones are counted. */
const MAX_FAILED_COMMANDS = 3;

/**
 * The commands a turn ran that failed and were not run again with success:
 * each with its command, its exit code and the end of its output. A command
 * that passed stays a count in the turn's line. The newest are shown; earlier
 * ones are counted in a line above them.
 */
export function FailedCommands({ turn, copy = defaultCopy }: { turn: Turn; copy?: Copy }) {
  const failed = failedCommands(turn.tasks);
  if (failed.length === 0) return null;
  const shown = failed.slice(-MAX_FAILED_COMMANDS);
  const earlier = failed.length - shown.length;
  return (
    <>
      {earlier > 0 && <Context items={[copy.conversation.toolLine.earlierFailed(earlier)]} />}
      {shown.map((c) => (
        <Terminal
          key={c.id}
          command={c.command}
          status="error"
          exitCode={c.exitCode}
          durationMs={c.durationMs}
          output={c.output}
        />
      ))}
    </>
  );
}
