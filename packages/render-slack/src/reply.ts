import { defaultCopy } from "@warp/components";

/**
 * The reply modal: what "Reply" opens on a mention or an expert's question
 * sent to a person's DM with the app. It is one text field. What the words
 * answer travels in `private_metadata` as ids, never content, and comes back
 * on submit, so the host posts them as that person: a human message in the
 * thread, or the question's answer.
 */

/** `callback_id` of `replyModal`; match it on view_submission. */
export const REPLY_MODAL = "aui.reply.modal";

/** What a reply answers: a message in a thread, or a question a run is waiting on. */
export type ReplyTarget = { thread: string; message: string } | { run: string; question: string };

/** The modal's words. Defaults are `copy.replyModal` from @warp/components. */
export interface ReplyModalLabels {
  title: string;
  label: string;
  answerLabel: string;
  placeholder: string;
  send: string;
  cancel: string;
}

/** Bounds: Slack's for the field and a view's metadata, and warp's for an id. */
const MAX_REPLY = 3000;
const MAX_ID = 200;

const plain = (text: string, max: number) => ({
  type: "plain_text",
  text: text.length <= max ? text : `${text.slice(0, max - 1)}…`,
});

const id = (v: unknown): v is string => typeof v === "string" && v.length > 0 && v.length <= MAX_ID;

/** The target a view's metadata names, or null when it names none exactly. */
function targetOf(raw: string | undefined): ReplyTarget | null {
  if (!raw) return null;
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return null;
  }
  if (typeof parsed !== "object" || parsed === null) return null;
  const o = parsed as Record<string, unknown>;
  const keys = Object.keys(o).sort().join(",");
  if (keys === "message,thread" && id(o.thread) && id(o.message))
    return { thread: o.thread, message: o.message };
  if (keys === "question,run" && id(o.run) && id(o.question))
    return { run: o.run, question: o.question };
  return null;
}

/**
 * The reply modal for one target. `context` is a line shown above the field,
 * such as who mentioned the reader and where; it is shown as plain text.
 */
export function replyModal(
  target: ReplyTarget,
  options: Partial<ReplyModalLabels> & { context?: string; initial?: string } = {},
) {
  const { context, initial, ...labels } = options;
  const l = { ...defaultCopy.replyModal, ...labels };
  const meta =
    "question" in target
      ? { run: target.run, question: target.question }
      : { thread: target.thread, message: target.message };
  if (!targetOf(JSON.stringify(meta)))
    throw new RangeError("a reply names a thread and a message, or a run and a question");
  const blocks: object[] = [];
  if (context) blocks.push({ type: "context", elements: [plain(context, 2000)] });
  blocks.push({
    type: "input",
    block_id: "reply",
    label: plain("question" in target ? l.answerLabel : l.label, 2000),
    element: {
      type: "plain_text_input",
      action_id: "value",
      multiline: true,
      max_length: MAX_REPLY,
      placeholder: plain(l.placeholder, 150),
      ...(initial ? { initial_value: initial.slice(0, MAX_REPLY) } : {}),
    },
  });
  return {
    type: "modal",
    callback_id: REPLY_MODAL,
    private_metadata: JSON.stringify(meta),
    title: plain(l.title, 24),
    submit: plain(l.send, 24),
    close: plain(l.cancel, 24),
    blocks,
  };
}

/** The words and target of a submitted `replyModal`, or null when it isn't one, names no target, or was sent empty. */
export function decodeReplyModal(view: {
  callback_id?: string;
  private_metadata?: string;
  state?: { values?: Record<string, Record<string, { value?: string | null }>> };
}): { text: string; target: ReplyTarget } | null {
  if (view.callback_id !== REPLY_MODAL) return null;
  const text = view.state?.values?.reply?.value?.value?.trim() ?? "";
  const target = targetOf(view.private_metadata);
  if (!text || text.length > MAX_REPLY || !target) return null;
  return { text, target };
}
