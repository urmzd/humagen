import "./react.ts";
import {
  type AnswerRequest,
  interruptOf,
  type Settled,
  settledFromOutcome,
  type WireQuestion,
} from "@humagen/adapter";
import { type Copy, defaultCopy, InterruptCard } from "@humagen/components";
import { renderToSlack } from "@humagen/render-slack";

/**
 * A question's card in a thread, once the question is no longer waiting.
 *
 * A card settles from its run's events while the app follows the run. A card
 * the app no longer follows (it restarted, or the run went quiet and its end
 * was posted as a new message) keeps its buttons after the question was
 * answered somewhere else, expired, or its run stopped. Humagen answers such
 * a press with what it recorded and changes nothing, so the press is where
 * the card is brought up to date: its buttons give way to the record.
 */

/** What the platform recorded for a question, as the message an answer returns shows it. */
export interface Recorded {
  question: WireQuestion;
  settled: Settled;
  /** Who it was recorded for, by subject, or `system`. Compared, never shown. */
  by: string;
}

type Wire = Record<string, unknown>;
const obj = (v: unknown): Wire => (v && typeof v === "object" ? (v as Wire) : {});
const str = (v: unknown) => (typeof v === "string" ? v : "");

/** The record of one question in the message `answerQuestion` returns, or null while it is open or not there. */
export function recordedOf(message: unknown, question: string): Recorded | null {
  const parts = obj(message).parts;
  if (!Array.isArray(parts)) return null;
  for (const raw of parts) {
    const part = obj(raw);
    const q = obj(part.question);
    if (str(q.id) !== question) continue;
    const outcome = obj(part.outcome);
    const settled = settledFromOutcome(outcome);
    if (!settled) return null;
    const tool = obj(q.tool);
    return {
      question: {
        id: question,
        kind: str(q.kind) || str(part.type),
        prompt: str(q.prompt),
        options: Array.isArray(q.options) ? q.options.map(str) : [],
        ...(q.tool ? { tool: { name: str(tool.name), arguments: tool.arguments } } : {}),
        ...(str(q.expires_at) ? { expires_at: str(q.expires_at) } : {}),
      },
      settled,
      by: str(outcome.by),
    };
  }
  return null;
}

/**
 * Whether what was recorded is not what this person just sent: someone else
 * answered, nobody did (it expired, its run stopped), or they answered it
 * differently before.
 */
export function settledElsewhere(
  recorded: Recorded,
  sent: AnswerRequest["body"],
  person: string,
): boolean {
  if (recorded.by !== person || recorded.settled.action !== sent.action) return true;
  if (sent.action === "choose") return (recorded.settled.note ?? "") !== (sent.choice ?? "");
  if (sent.action === "reply") return (recorded.settled.note ?? "") !== (sent.text ?? "");
  return false;
}

type Block = {
  type?: string;
  block_id?: string;
  text?: { text?: string };
  title?: { text?: string };
  body?: { text?: string };
  child_blocks?: unknown[];
  elements?: unknown[];
};

/** Whether a block is drawn for this question: its card, its container or its buttons. */
const isFor = (b: unknown, question: string) => {
  const { block_id: id = "" } = obj(b) as Block;
  return id === `clarification:${question}` || id.startsWith(`approval:${question}:`);
};

/** Slack's own blocks a question is drawn as (render-slack `native.tsx`). */
const NATIVE = new Set(["card", "container"]);

/** Where a question's buttons are in a message's blocks, or -1. */
function buttonsOf(blocks: readonly unknown[], question: string): number {
  return blocks.findIndex((b) => obj(b).type === "actions" && isFor(b, question));
}

/** Where a question's own card or container is: at the top, or in a carousel. */
function nativeOf(
  blocks: readonly unknown[],
  question: string,
): { at: number; inCarousel?: number } | null {
  for (const [at, b] of blocks.entries()) {
    const block = obj(b) as Block;
    if (NATIVE.has(block.type ?? "") && isFor(b, question)) return { at };
    if (block.type === "carousel" && Array.isArray(block.elements)) {
      const i = block.elements.findIndex((c) => isFor(c, question));
      if (i >= 0) return { at, inCarousel: i };
    }
  }
  return null;
}

const card = (
  run: string,
  q: WireQuestion,
  copy: Copy,
  settled?: Settled,
  native = true,
): unknown[] => {
  const interrupt = interruptOf(q, settled);
  return interrupt
    ? (renderToSlack(<InterruptCard turnId={run} interrupt={interrupt} copy={copy} />, {
        native,
      }).blocks as unknown[])
    : [];
};

/**
 * A message's blocks with one question's card replaced by its record: what
 * was chosen and by whom, or why nobody answered, and no button left. The
 * record is drawn the way the card was: as Slack's own card or container, or
 * as sections. The rest of the message is untouched. Null when the card is
 * not in the blocks.
 */
export function settleCard(
  blocks: readonly unknown[],
  run: string,
  recorded: Recorded,
  copy: Copy = defaultCopy,
): unknown[] | null {
  const id = recorded.question.id;
  const native = nativeOf(blocks, id);
  if (native) {
    const record = card(run, recorded.question, copy, recorded.settled);
    if (record.length === 0) return null;
    const { at, inCarousel } = native;
    if (inCarousel !== undefined) {
      // One card of several: the carousel keeps its place, and the card gives way to its record's.
      const [only] = record;
      if (record.length !== 1 || obj(only).type !== "card") return null;
      const carousel = obj(blocks[at]) as Block;
      const elements = [...(carousel.elements ?? [])];
      elements[inCarousel] = only;
      return [...blocks.slice(0, at), { ...carousel, elements }, ...blocks.slice(at + 1)];
    }
    // The row of choices a card could not hold goes with it.
    const next = obj(blocks[at + 1]) as Block;
    const more = next.type === "actions" && isFor(next, id) ? 1 : 0;
    return [...blocks.slice(0, at), ...record, ...blocks.slice(at + 1 + more)];
  }
  const at = buttonsOf(blocks, id);
  if (at < 0) return null;
  const open = card(run, recorded.question, copy, undefined, false);
  const record = card(run, recorded.question, copy, recorded.settled, false);
  if (record.length === 0) return null;
  // The card as it was drawn, around its buttons: the same blocks, in order.
  const k = open.findIndex((b) => obj(b).type === "actions");
  const from = at - k;
  const whole =
    k >= 0 &&
    from >= 0 &&
    from + open.length <= blocks.length &&
    open.every((b, i) => obj(b).type === obj(blocks[from + i]).type);
  if (whole) return [...blocks.slice(0, from), ...record, ...blocks.slice(from + open.length)];
  // Drawn some other way: the buttons alone give way to the record's last line.
  return [...blocks.slice(0, at), record.at(-1), ...blocks.slice(at + 1)];
}

/** The question a card asks, as plain words, read from the blocks above its buttons. */
export function promptIn(blocks: readonly unknown[], question: string): string {
  const native = nativeOf(blocks, question);
  if (native) {
    const at = obj(blocks[native.at]) as Block;
    const own = (
      native.inCarousel === undefined ? at : obj(at.elements?.[native.inCarousel])
    ) as Block;
    // The title says only what kind of card it is: the call or the question leads its body.
    const first = own.type === "container" ? (obj(own.child_blocks?.[0]) as Block) : null;
    return plainWords(own.body?.text ?? first?.text?.text ?? "");
  }
  const at = buttonsOf(blocks, question);
  for (let i = at - 1; i >= 0; i--) {
    const b = obj(blocks[i]) as Block;
    if (b.type !== "section" || !b.text?.text) continue;
    return plainWords(b.text.text);
  }
  return "";
}

/** A card's first line as plain words: its bold and Slack's escapes taken off. */
function plainWords(text: string): string {
  return (text.split("\n")[0] ?? "")
    .replace(/^\*(.+)\*$/, "$1")
    .replaceAll("&lt;", "<")
    .replaceAll("&gt;", ">")
    .replaceAll("&amp;", "&")
    .trim();
}
