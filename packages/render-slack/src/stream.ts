import type { PromptConfig, Task, Turn } from "@warp/core";
import type { Block } from "./blocks";
import { toMrkdwn, toPlain } from "./mrkdwn";
import { nextBreak } from "./split";
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
  | { type: "plan_update"; title: string }
  | { type: "blocks"; blocks: Block[] };

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
    // Another agent's step says so in its title; a task card has no other way to show it.
    title: clip(task.subagent ? `${task.subagent.name}: ${task.title}` : task.title),
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
 * The answer as it is streamed: `[[citation-id]]` markers become the numbered
 * links the final Sources list uses, and unknown ones vanish with the space
 * before them. Trailing whitespace and a half-arrived marker are held back
 * until the next append shows what they belong to.
 */
function streamText(text: string, citations: Turn["citations"]): string {
  return text
    .replace(/(\s*)\[\[([^\]]+)\]\]/g, (_m, space: string, id: string) => {
      const i = citations.findIndex((c) => c.id === id);
      const url = citations[i]?.url;
      if (i === -1) return "";
      return url ? `${space}[[${i + 1}]](${url})` : `${space}[${i + 1}]`;
    })
    .replace(/\s*(\[\[[^\]]*\]?|\[)?$/, "");
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
    const before = streamText(sent, prev?.citations ?? []);
    const after = streamText(next.text, next.citations);
    // A citation that arrived after its marker was sent changes text Slack already has.
    const delta = after.startsWith(before)
      ? after.slice(before.length)
      : streamText(next.text.slice(sent.length), next.citations);
    for (let i = 0; i < delta.length; i += MARKDOWN_CHUNK) {
      chunks.push({ type: "markdown_text", text: delta.slice(i, i + MARKDOWN_CHUNK) });
    }
  }
  return { chunks, rewritten };
}

/**
 * What a continuation message opens with: which part it is. The first message
 * opens with nothing, and the source and session link appear once, with the
 * final blocks at the very end (see `TurnView`), never at the top.
 * Send it as the first chunks of chat.startStream; empty for the first message.
 */
export function streamHeader(
  _turn: Turn,
  { page = 1, labels = {} }: { page?: number; labels?: { part?: (n: number) => string } } = {},
): StreamChunk[] {
  if (page <= 1) return [];
  const part = (labels.part ?? ((n) => `Part ${n}`))(page);
  return [
    {
      type: "blocks",
      blocks: [{ type: "context", elements: [{ type: "mrkdwn", text: toMrkdwn(part) }] }],
    },
  ];
}

/** Closes a message that ran out of room, so the cut reads as deliberate. */
export function streamTrailer(text = "Continues in the next message"): StreamChunk {
  return { type: "markdown_text", text: `\n\n_${text}_` };
}

/**
 * Characters one streamed message can hold before Slack rejects appends with
 * msg_too_long. Slack documents 12,000 per markdown_text but no total; this
 * leaves room for the header, the trailer and the final blocks.
 */
export const STREAM_MESSAGE_CHARS = 11_000;

/** Per-card cost beyond its text, so many small task updates still add up. */
const TASK_OVERHEAD = 120;
const PLAN_OVERHEAD = 80;

/** What the current streamed message already holds. Start each turn with `emptyBudget()`. */
export interface StreamBudget {
  /** Markdown sent on this message; needed to know if a break lands inside a fence or table. */
  text: string;
  /** Weight per task id (and "plan"): an update replaces its card, it doesn't add one. */
  cards: Record<string, number>;
  /** Tasks still running on this message; they are settled when it closes. */
  open: Record<string, Extract<StreamChunk, { type: "task_update" }>>;
}

export const emptyBudget = (): StreamBudget => ({ text: "", cards: {}, open: {} });

function cardWeight(chunk: Exclude<StreamChunk, { type: "markdown_text" }>): number {
  if (chunk.type === "plan_update") return PLAN_OVERHEAD + chunk.title.length;
  if (chunk.type === "blocks") return JSON.stringify(chunk.blocks).length;
  return (
    TASK_OVERHEAD +
    chunk.title.length +
    (chunk.details?.length ?? 0) +
    (chunk.output?.length ?? 0) +
    (chunk.sources ?? []).reduce((n, s) => n + s.text.length + s.url.length, 0)
  );
}

/**
 * Fit chunks into Slack messages. `pages[0]` belongs to the message that is
 * streaming now; every later page needs a new one: append `streamTrailer()`,
 * stopStream, startStream with `streamHeader(turn, { page })`, then append the page.
 * Text breaks at paragraphs and lines, and code fences and tables survive the cut.
 */
export function streamPages(
  budget: StreamBudget,
  chunks: StreamChunk[],
  max = STREAM_MESSAGE_CHARS,
): { pages: StreamChunk[][]; budget: StreamBudget } {
  let now: StreamBudget = {
    text: budget.text,
    cards: { ...budget.cards },
    open: { ...budget.open },
  };
  const pages: StreamChunk[][] = [[]];
  const page = () => pages[pages.length - 1] as StreamChunk[];
  const cards = () => Object.values(now.cards).reduce((n, w) => n + w, 0);
  const used = () => now.text.length + cards();
  const turnPage = () => {
    // A card left spinning on a closed message would spin forever.
    for (const task of Object.values(now.open)) {
      page().push({ type: "task_update", id: task.id, title: task.title, status: "complete" });
    }
    pages.push([]);
    now = emptyBudget();
  };

  for (const chunk of chunks) {
    if (chunk.type !== "markdown_text") {
      const key = chunk.type === "task_update" ? chunk.id : chunk.type;
      const weight = cardWeight(chunk);
      if (used() > 0 && used() + weight - (now.cards[key] ?? 0) > max) turnPage();
      page().push(chunk);
      now.cards[key] = chunk.type === "task_update" ? weight : (now.cards[key] ?? 0) + weight;
      if (chunk.type === "task_update") {
        if (chunk.status === "in_progress" || chunk.status === "pending")
          now.open[chunk.id] = chunk;
        else delete now.open[chunk.id];
      }
      continue;
    }
    let text = chunk.text;
    for (;;) {
      const room = max - cards();
      // The last stretch of a message is only for finishing the current line.
      const cut = nextBreak(now.text + text, room, {
        from: now.text.length,
        soft: room - Math.min(1000, max / 4),
      });
      if (!cut) break;
      if (cut.head) page().push({ type: "markdown_text", text: cut.head });
      turnPage();
      text = cut.tail;
    }
    if (text) page().push({ type: "markdown_text", text });
    now.text += text;
  }
  return { pages, budget: now };
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
