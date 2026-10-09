import type { PromptConfig, Task, Turn } from "@agent-ui/core";
import { toPlain } from "./mrkdwn";
import { slackTaskStatus } from "./status";

/**
 * Slack native streaming (chat.startStream → appendStream → stopStream) as a
 * pure diff between two snapshots of the same Turn. The host sends the chunks
 * in order and awaits each call, then posts `renderToSlack(<TurnView streamed />)`
 * blocks in stopStream.
 */

/** Chunk payloads for chat.appendStream (docs.slack.dev/reference/methods/chat.appendStream). */
export type StreamChunk =
  | { type: "markdown_text"; text: string }
  | {
      type: "task_update";
      id: string;
      title: string;
      status: Task["status"];
      details?: string;
      output?: string;
      sources?: { type: "url"; url: string; text: string }[];
    }
  | { type: "plan_update"; title: string };

/** Per-chunk text limit for task_update and plan_update fields. */
const CHUNK_TEXT = 256;
/** markdown_text limit per append. */
const MARKDOWN_CHUNK = 12_000;

const clip = (text: string, max = CHUNK_TEXT) =>
  text.length <= max ? text : `${text.slice(0, max - 1)}…`;

/** Arguments for chat.startStream. Plan mode when the turn is plan-shaped. */
export function streamStart(turn: Turn): { task_display_mode: "plan" | "timeline" } {
  return { task_display_mode: turn.planTitle ? "plan" : "timeline" };
}

function taskChunk(task: Task, prev: Task | undefined): StreamChunk {
  return {
    type: "task_update",
    id: task.id,
    title: clip(task.title),
    status: slackTaskStatus(task.status, false) as Task["status"],
    // Slack appends details on every update with the same id, so send each field once.
    ...(task.details && task.details !== prev?.details ? { details: clip(task.details) } : {}),
    ...(task.output && task.output !== prev?.output ? { output: clip(task.output) } : {}),
    ...(task.sources?.length && !prev?.sources?.length
      ? {
          sources: task.sources
            .filter((s) => s.url)
            .map((s) => ({ type: "url" as const, url: s.url as string, text: s.title })),
        }
      : {}),
  };
}

function sameTask(a: Task, b: Task | undefined) {
  return (
    b !== undefined &&
    a.title === b.title &&
    a.status === b.status &&
    a.details === b.details &&
    a.output === b.output &&
    (a.sources?.length ?? 0) === (b.sources?.length ?? 0)
  );
}

/**
 * Chunks that move Slack from `prev` to `next`. Progress first, then text, so
 * a task never finishes after the words that depend on it. Text that was
 * rewritten (not appended) can't be streamed; the result says so and the host
 * should fall back to chat.update.
 */
export function streamUpdate(
  prev: Turn | null,
  next: Turn,
): { chunks: StreamChunk[]; rewritten: boolean } {
  const chunks: StreamChunk[] = [];
  const before = new Map((prev?.tasks ?? []).map((t) => [t.id, t]));
  for (const task of next.tasks) {
    const old = before.get(task.id);
    if (!sameTask(task, old)) chunks.push(taskChunk(task, old));
  }

  if (next.planTitle && next.planTitle !== prev?.planTitle) {
    chunks.push({ type: "plan_update", title: clip(next.planTitle) });
  }

  const sent = prev?.text ?? "";
  const rewritten = !next.text.startsWith(sent);
  if (!rewritten && next.text.length > sent.length) {
    // Citation markers stream as plain text; numbered links land with the final blocks.
    const delta = next.text.slice(sent.length).replace(/\[\[[^\]]+\]\]/g, "");
    for (let i = 0; i < delta.length; i += MARKDOWN_CHUNK) {
      chunks.push({ type: "markdown_text", text: delta.slice(i, i + MARKDOWN_CHUNK) });
    }
  }
  return { chunks, rewritten };
}

/** Plan title for chat.stopStream: the outcome, so a collapsed plan still says how it ended. */
export function streamStopTitle(turn: Turn): string | undefined {
  if (!turn.planTitle) return undefined;
  if (turn.status === "failed") return "Error";
  if (turn.status === "stopped") return "Stopped";
  if (turn.status === "done" && turn.durationMs !== undefined) {
    return `Worked for ${Math.round(turn.durationMs / 1000)}s`;
  }
  return turn.planTitle;
}

/**
 * Payload for assistant.threads.setSuggestedPrompts. Slack owns the composer,
 * so this is the only part of the web prompt bar that carries over.
 */
export function suggestedPrompts(config: PromptConfig, title?: string) {
  return {
    ...(title ? { title: toPlain(title) } : {}),
    prompts: (config.suggestions ?? []).slice(0, 4).map((p) => ({
      title: clip(p.title, 75),
      message: p.message,
    })),
  };
}
