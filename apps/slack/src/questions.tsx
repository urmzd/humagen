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

type Block = { type?: string; block_id?: string; text?: { text?: string } };

/** Where a question's buttons are in a message's blocks, or -1. */
function buttonsOf(blocks: readonly unknown[], question: string): number {
  return blocks.findIndex((b) => {
    const { type, block_id: id = "" } = obj(b) as Block;
    return (
      type === "actions" &&
      (id === `clarification:${question}` || id.startsWith(`approval:${question}:`))
    );
  });
}

const card = (run: string, q: WireQuestion, copy: Copy, settled?: Settled) => {
  const interrupt = interruptOf(q, settled);
  return interrupt
    ? (renderToSlack(<InterruptCard turnId={run} interrupt={interrupt} copy={copy} />)
        .blocks as unknown[])
    : [];
};

/**
 * A message's blocks with one question's card replaced by its record: what
 * was chosen and by whom, or why nobody answered, and no button left. The
 * rest of the message is untouched. Null when the card is not in the blocks.
 */
export function settleCard(
  blocks: readonly unknown[],
  run: string,
  recorded: Recorded,
  copy: Copy = defaultCopy,
): unknown[] | null {
  const at = buttonsOf(blocks, recorded.question.id);
  if (at < 0) return null;
  const open = card(run, recorded.question, copy);
  const record = card(run, recorded.question, copy, recorded.settled);
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
  const at = buttonsOf(blocks, question);
  for (let i = at - 1; i >= 0; i--) {
    const b = obj(blocks[i]) as Block;
    if (b.type !== "section" || !b.text?.text) continue;
    return (b.text.text.split("\n")[0] ?? "")
      .replace(/^\*(.+)\*$/, "$1")
      .replaceAll("&lt;", "<")
      .replaceAll("&gt;", ">")
      .replaceAll("&amp;", "&")
      .trim();
  }
  return "";
}
