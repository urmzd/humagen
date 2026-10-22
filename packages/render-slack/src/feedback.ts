import { ActionId, type UiAction } from "@warp/core";

/**
 * The comment prompt that follows a rating on Slack. Feedback buttons can't
 * ask anything themselves, so the host opens this modal on the click's
 * trigger_id. The host saves the rating alongside opening it: submitting adds a
 * comment, and closing the modal ("Skip") adds nothing and loses nothing.
 */

/** `callback_id` of the modal; match it on view_submission. */
export const FEEDBACK_MODAL = "aui.feedback.modal";

export interface FeedbackModalLabels {
  askUp: string;
  askDown: string;
  comment: string;
  placeholder: string;
  reason: string;
  send: string;
  skip: string;
  reasons: { id: string; label: string }[];
}

const DEFAULT_LABELS: FeedbackModalLabels = {
  askUp: "What was helpful?",
  askDown: "What went wrong?",
  comment: "Comment",
  placeholder: "Add a comment (optional)",
  reason: "What was the problem?",
  send: "Send",
  skip: "Skip",
  reasons: [
    { id: "wrong", label: "Wrong" },
    { id: "incomplete", label: "Incomplete" },
    { id: "slow", label: "Too slow" },
    { id: "other", label: "Other" },
  ],
};

const plain = (text: string, max: number) => ({
  type: "plain_text",
  text: text.length <= max ? text : `${text.slice(0, max - 1)}…`,
});

/**
 * The view for views.open. Call it straight away: a trigger_id lasts about
 * three seconds, so record the rating without waiting on anything slow first.
 */
export function feedbackModal(
  { turnId, rating }: { turnId: string; rating: "up" | "down" },
  labels: Partial<FeedbackModalLabels> = {},
) {
  const l = { ...DEFAULT_LABELS, ...labels };
  return {
    type: "modal",
    callback_id: FEEDBACK_MODAL,
    // Everything the submit handler needs; ids only, never content.
    private_metadata: JSON.stringify({ turnId, rating }),
    title: plain(rating === "up" ? l.askUp : l.askDown, 24),
    submit: plain(l.send, 24),
    close: plain(l.skip, 24),
    blocks: [
      ...(rating === "down" && l.reasons.length > 0
        ? [
            {
              type: "input",
              block_id: "reason",
              optional: true,
              label: plain(l.reason, 2000),
              element: {
                type: "radio_buttons",
                action_id: "value",
                options: l.reasons
                  .slice(0, 10)
                  .map((r) => ({ text: plain(r.label, 75), value: r.id })),
              },
            },
          ]
        : []),
      {
        type: "input",
        block_id: "comment",
        optional: true,
        label: plain(l.comment, 2000),
        element: {
          type: "plain_text_input",
          action_id: "value",
          multiline: true,
          max_length: 3000,
          placeholder: plain(l.placeholder, 150),
        },
      },
    ],
  };
}

/**
 * The action a submitted feedback modal means, or null when it isn't one or
 * the person sent it empty. Feed it to the same handler as a web comment.
 */
export function decodeFeedbackModal(view: {
  callback_id?: string;
  private_metadata?: string;
  state?: {
    values?: Record<
      string,
      Record<string, { value?: string | null; selected_option?: { value: string } | null }>
    >;
  };
}): UiAction | null {
  if (view.callback_id !== FEEDBACK_MODAL || !view.private_metadata) return null;
  let metadata: unknown;
  try {
    metadata = JSON.parse(view.private_metadata);
  } catch {
    return null;
  }
  if (!metadata || typeof metadata !== "object") return null;
  const { turnId, rating } = metadata as { turnId?: unknown; rating?: unknown };
  if (typeof turnId !== "string" || !turnId || (rating !== "up" && rating !== "down")) return null;
  const text = view.state?.values?.comment?.value?.value?.trim();
  const option = view.state?.values?.reason?.value?.selected_option?.value;
  if (!text && !option) return null;
  return {
    id: ActionId.feedbackComment,
    payload: { turnId, target: rating, ...(text ? { text } : {}), ...(option ? { option } : {}) },
  };
}
