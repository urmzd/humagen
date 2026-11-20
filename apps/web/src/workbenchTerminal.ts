import type { ResponseBlock, ResponseState, ResponseTool } from "@humagen/core";
import { MAX_HUMAN_DETAIL, MAX_HUMAN_SUMMARY } from "@humagen/core";
import type { SandboxCommandResult } from "../../../packages/client/src/generated/types.ts";

/*
 * The Terminal tab's reading of a session, with no React: the commands its
 * agents ran, taken from each answer's folded stream, the commands its
 * reader ran by hand, kept for as long as the page lives, and what of one
 * of those is recorded for the reader's next message.
 */

/**
 * The tools that run a command. `shell` is the platform's own
 * (internal/platform/isolation.go); the others are what an imported session
 * calls its shell.
 */
const SHELL_TOOLS = new Set(["shell", "Bash", "bash", "local_shell", "exec_command"]);

export type CommandState =
  | "queued"
  | "running"
  | "waiting"
  | "exited"
  | "failed"
  | "denied"
  | "stopped"
  | "timed_out";

/** One command in the log. */
export interface Command {
  id: string;
  /** Who ran it: an agent's name, or nothing for the reader. */
  agent?: string;
  by: "agent" | "person";
  command: string;
  state: CommandState;
  /** The exit status, when the command ran to an end and said one. */
  exit?: number;
  output: string;
  /** Earlier output was cut before it reached the page. */
  cut?: boolean;
  elapsedMs?: number;
  /** When it ran, for the reader's own; an agent's are placed by their run. */
  at?: string;
  /** Kept from the reader by the audience rule. */
  hidden?: boolean;
  /** Files of the session a reader's command changed, and changes not kept. */
  files?: { path: string; removed?: boolean }[];
  conflicts?: string[];
}

function commandOf(input: Record<string, unknown>): string {
  const raw = input.command ?? input.cmd ?? input.script;
  if (typeof raw === "string") return raw;
  if (Array.isArray(raw)) return raw.filter((part) => typeof part === "string").join(" ");
  return "";
}

const stateOf: Record<ResponseTool["status"], CommandState> = {
  queued: "queued",
  running: "running",
  awaiting_approval: "waiting",
  awaiting_input: "waiting",
  done: "exited",
  error: "failed",
  denied: "denied",
  stopped: "stopped",
};

const CUT = "[earlier output cut]\n";

function fromTool(tool: ResponseTool, agent: string | undefined): Command {
  const out: Command = {
    id: tool.id,
    agent,
    by: "agent",
    command: commandOf(tool.input),
    state: stateOf[tool.status] ?? "exited",
    output: "",
    elapsedMs: tool.elapsedMs,
    hidden: tool.hidden,
  };
  const text = tool.result?.text ?? "";
  // The platform's shell answers "exit N" and then the end of the output.
  const exit = /^exit (-?\d+)(?:\n|$)/.exec(text);
  let body = exit ? text.slice(exit[0].length) : text;
  if (exit && !tool.result?.isError) out.exit = Number(exit[1]);
  if (body.startsWith(CUT)) {
    out.cut = true;
    body = body.slice(CUT.length);
  }
  out.output = body;
  if (tool.result?.isError && out.state === "exited") out.state = "failed";
  return out;
}

function walk(blocks: ResponseBlock[], agent: string | undefined, into: Command[]): void {
  for (const block of blocks) {
    if (block.type !== "tool") continue;
    if (SHELL_TOOLS.has(block.name) && (block.hidden || commandOf(block.input) !== ""))
      into.push(fromTool(block, agent));
    // An agent call's own work is nested under it, and is that agent's.
    if (block.content.length > 0) walk(block.content, block.agent ?? agent, into);
  }
}

/** Every command the session's agents ran, in the order they ran them. */
export function agentCommands(responses: readonly ResponseState[] | undefined): Command[] {
  const out: Command[] = [];
  for (const response of responses ?? []) walk(response.content, response.agent, out);
  return out;
}

/** Whether any answer ran a command, for the tab's `available`. It reads only what it is given. */
export function ranCommands(responses: readonly ResponseState[] | undefined): boolean {
  return agentCommands(responses).length > 0;
}

/** The most of a reader's commands the page keeps for one session. */
export const MAX_PERSON_COMMANDS = 50;

type Listener = () => void;
const mine = new Map<string, Command[]>();
const listeners = new Set<Listener>();
const NONE: readonly Command[] = Object.freeze([]);

/**
 * The commands a reader ran by hand, by session, in memory: they outlive a
 * change of tab and are gone on reload, as what was done by hand is. The
 * server keeps no list of them; the message that carries them does.
 */
export const personCommands = {
  list: (thread: string): readonly Command[] => mine.get(thread) ?? NONE,
  put(thread: string, command: Command): void {
    const held = mine.get(thread) ?? [];
    const next = held.some((c) => c.id === command.id)
      ? held.map((c) => (c.id === command.id ? command : c))
      : [...held, command];
    mine.set(thread, next.slice(-MAX_PERSON_COMMANDS));
    for (const tell of listeners) tell();
  },
  clear(thread: string): void {
    if (!mine.delete(thread)) return;
    for (const tell of listeners) tell();
  },
  subscribe(listener: Listener): () => void {
    listeners.add(listener);
    return () => listeners.delete(listener);
  },
};

/** A reader's command as the log shows it, from the server's answer. */
export function fromResult(id: string, at: string, result: SandboxCommandResult): Command {
  return {
    id,
    by: "person",
    command: result.command,
    state: result.timed_out ? "timed_out" : "exited",
    exit: result.timed_out ? undefined : result.exit_code,
    output: result.output,
    cut: result.truncated,
    elapsedMs: result.duration_ms,
    at,
    files: result.files.map((f) => ({ path: f.path, removed: f.removed || undefined })),
    conflicts: result.conflicts,
  };
}

/**
 * The log: the agents' commands, each answer's together, and the reader's
 * own among them by when they ran. `answers` are the session's answers the
 * responses were read from, oldest first: an answer is found by its run's
 * id, and otherwise by its place counted from the newest, since the newest
 * answers are the ones read. A reader runs a command only while no run is
 * working, so a command of theirs sits whole before or after an answer's.
 */
export function commandLog(
  responses: readonly ResponseState[] | undefined,
  person: readonly Command[],
  answers: readonly { id: string; created_at: string }[],
): Command[] {
  const when = (iso: string | undefined) => (iso ? Date.parse(iso) : Number.NaN) || 0;
  const groups: { at: number; commands: Command[] }[] = [];
  const all = responses ?? [];
  let last = 0;
  all.forEach((response, index) => {
    const commands: Command[] = [];
    walk(response.content, response.agent, commands);
    const answer =
      answers.find((a) => a.id === response.rootRun) ??
      answers[answers.length - (all.length - index)];
    // An answer never starts before the one above it.
    last = Math.max(last, when(answer?.created_at));
    if (commands.length > 0) groups.push({ at: last, commands });
  });
  for (const command of person) groups.push({ at: when(command.at), commands: [command] });
  // Stable: with equal or unknown times the agents' commands keep their order.
  return groups
    .map((group, index) => ({ group, index }))
    .sort((a, b) => a.group.at - b.group.at || a.index - b.index)
    .flatMap(({ group }) => group.commands);
}

const clip = (text: string, max: number) =>
  text.length <= max ? text : `${text.slice(0, max - 1)}…`;

/** The end of some output, from a line's start where it can be, in at most `max` characters. */
export function outputTail(output: string, max: number): string {
  const text = output.replace(/\s+$/, "");
  if (text.length <= max) return text;
  const room = max - 2;
  let tail = text.slice(-room);
  const line = tail.indexOf("\n");
  if (line >= 0 && line < tail.length - 1 && line < room / 2) tail = tail.slice(line + 1);
  return `…\n${tail}`;
}

/**
 * What of a reader's command goes with their next message: one line saying
 * what ran and how it came out, then the files it changed and the end of its
 * output, inside the record's limits so nothing that matters is cut.
 */
export function activityOf(
  command: Command,
  words: {
    ran: (command: string, outcome: string) => string;
    exit: (code: number) => string;
    timedOut: string;
    changed: (files: string) => string;
    removed: (path: string) => string;
    notKept: (n: number) => string;
    noOutput: string;
  },
): { source: "terminal"; summary: string; detail: string; at?: string } {
  const outcome = command.state === "timed_out" ? words.timedOut : words.exit(command.exit ?? 0);
  const oneLine = command.command.replace(/\s+/g, " ").trim();
  const room = MAX_HUMAN_SUMMARY - words.ran("", outcome).length;
  const summary = words.ran(clip(oneLine, Math.max(16, room)), outcome);
  const lines: string[] = [];
  const files = (command.files ?? []).map((f) => (f.removed ? words.removed(f.path) : f.path));
  if (files.length > 0) lines.push(clip(words.changed(files.join(", ")), 160));
  if (command.conflicts?.length) lines.push(words.notKept(command.conflicts.length));
  const head = lines.join("\n");
  const left = MAX_HUMAN_DETAIL - head.length - (head ? 1 : 0);
  const tail = outputTail(command.output, left);
  const detail = [head, tail || words.noOutput].filter(Boolean).join("\n");
  return { source: "terminal", summary, detail, at: command.at };
}
