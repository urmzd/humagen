import { toolCommand, toolFailed, toolKind, toolTarget } from "./timeline";
import type { Task } from "./types";

/**
 * A shell command a turn ran, read from the call the stream already carries:
 * the command from its arguments, and the exit code and output from its
 * result. The platform's `shell` tool answers `exit <code>` on the first line
 * and then the end of the command's output, for a command that failed as for
 * one that passed, so a non-zero exit arrives as a completed call. Nothing
 * here is guessed: a result with no such first line has no exit code.
 */
export interface CommandResult {
  command: string;
  /** The code the command exited with, when its result says. */
  exitCode?: number;
  /** The command's output, without the exit line. */
  output: string;
  /** It exited non-zero, or the call itself failed. */
  failed: boolean;
  durationMs?: number;
}

const EXIT = /^exit (-?\d+)(?:\n|$)/;

/** The command a finished call ran and how it ended, or undefined when the call is not a command or has not ended. */
export function commandResult(
  task: Pick<Task, "tool" | "title" | "input" | "status" | "output" | "durationMs" | "subagent">,
): CommandResult | undefined {
  if (task.subagent || toolKind(task) !== "run") return undefined;
  if (task.status !== "complete" && task.status !== "error") return undefined;
  const command = toolCommand(task);
  if (!command) return undefined;
  const raw = task.output ?? "";
  const exit = EXIT.exec(raw);
  const exitCode = exit ? Number(exit[1]) : undefined;
  return {
    command,
    ...(exitCode !== undefined ? { exitCode } : {}),
    output: exit ? raw.slice(exit[0].length) : raw,
    failed: toolFailed(task) || (exitCode !== undefined && exitCode !== 0),
    ...(task.durationMs !== undefined ? { durationMs: task.durationMs } : {}),
  };
}

/** Whether a call failed as a reader means it: the call failed, or the command it ran exited non-zero. */
export function callFailed(task: Parameters<typeof commandResult>[0]): boolean {
  return toolFailed(task) || commandResult(task)?.failed === true;
}

/**
 * The failed commands a turn leaves standing, in order: each command that
 * failed and that no later call ran again with success. A test that failed,
 * was fixed and then passed is part of the count, not of what still needs
 * reading.
 */
export function failedCommands(tasks: readonly Task[]): (CommandResult & { id: string })[] {
  const out: (CommandResult & { id: string })[] = [];
  const results = tasks.map((t) => ({ id: t.id, result: commandResult(t) }));
  for (const [i, { id, result }] of results.entries()) {
    if (!result?.failed) continue;
    const fixed = results
      .slice(i + 1)
      .some((later) => later.result?.command === result.command && !later.result.failed);
    if (!fixed) out.push({ ...result, id });
  }
  return out;
}

/** The tool a call was made to, by name, or its title when the stream gave no name. */
const toolName = (task: Pick<Task, "tool" | "title">) => task.tool || task.title;

/**
 * The failed calls in a turn that a later call recovered, by id. A call that
 * failed is recovered when a later call in the same turn ended without
 * failing and either went to the same tool or was pointed at the same
 * target. A command needs the same command line: a different command
 * through the same shell is not a retry. A failure no later call recovered
 * stays a failure.
 *
 * Pass every call of the turn, in order, so a retry in a later run of steps
 * counts.
 */
export function recoveredCalls(tasks: readonly Task[]): Set<string> {
  const out = new Set<string>();
  for (const [i, task] of tasks.entries()) {
    if (!toolFailed(task)) continue;
    const command = toolKind(task) === "run" ? toolCommand(task) : undefined;
    const target = toolTarget(task);
    const name = toolName(task);
    const recovered = tasks.slice(i + 1).some((later) => {
      if (later.status !== "complete" || callFailed(later)) return false;
      if (command !== undefined) return toolCommand(later) === command;
      return (!!name && toolName(later) === name) || (!!target && toolTarget(later) === target);
    });
    if (recovered) out.add(task.id);
  }
  return out;
}
