/**
 * Interaction contract shared by every surface. A Slack button and a web
 * button emit the same action id and payload, so one handler serves both.
 */

export const ActionId = {
  approve: "aui.approve",
  deny: "aui.deny",
  edit: "aui.edit",
  clarify: "aui.clarify",
  clarifyOther: "aui.clarify.other",
  /** A rating. `target` is "up" or "down", or `FEEDBACK_CLEAR` to take it back. Sent on the click itself. */
  feedback: "aui.feedback",
  /**
   * A comment on a rating that was already sent. `target` repeats the rating,
   * `text` is the comment and `option` an optional reason (see FEEDBACK_REASONS).
   * It only ever adds: a host that ignores it still has the rating.
   */
  feedbackComment: "aui.feedback.comment",
  retry: "aui.retry",
  approveSelected: "aui.approve.selected",
  denyAll: "aui.deny.all",
  ask: "aui.ask",
  remember: "aui.remember",
  unsure: "aui.clarify.unsure",
  resume: "aui.resume",
  compact: "aui.compact",
  reconnect: "aui.reconnect",
  approveAll: "aui.approve.all",
  dismiss: "aui.inbox.dismiss",
  snooze: "aui.inbox.snooze",
  open: "aui.inbox.open",
  report: "aui.report",
  /** Open a delegated run. `turnId` is the turn it was called from; `target` the child run id. */
  openRun: "aui.run.open",
  // Mailbox parts (see mailbox.ts). `turnId` carries the message id, `target` the part id.
  read: "aui.inbox.read",
  reply: "aui.inbox.reply",
  acknowledge: "aui.alert.acknowledge",
  keepMemories: "aui.memory.keep",
  discardMemories: "aui.memory.discard",
  allowOnce: "aui.budget.once",
  raiseBudget: "aui.budget.raise",
  // Home (see home.ts). `turnId` is HOME_ID; Review reuses `open` with the item's ids.
  /** Ask the assistant from Home. `text` is the question; Slack sends none, so collect it in a form. */
  homeAsk: "aui.home.ask",
  /** A way to start. `target` is a HomeStartId. */
  homeStart: "aui.home.start",
  /** Attach a file to the next question. */
  homeAttach: "aui.home.attach",
  /** Who answers the person's DMs. `option` is the agent id, or ANY_AGENT. */
  homeAnsweredBy: "aui.home.answered_by",
  /** Connect an account. `target` is a connection id, or PERSONAL_ACCOUNT. */
  homeConnect: "aui.home.connect",
} as const;

export type ActionId = (typeof ActionId)[keyof typeof ActionId];

/** `target` of a feedback action that removes the rating (clicking the selected one again). */
export const FEEDBACK_CLEAR = "clear";

/** Quick reasons offered with "Not helpful": a tap is likelier than typing. */
export const FEEDBACK_REASONS = ["wrong", "incomplete", "slow", "other"] as const;
export type FeedbackReason = (typeof FEEDBACK_REASONS)[number];

export interface ActionPayload {
  turnId: string;
  /** Interrupt id, option id, or rating, depending on the action. */
  target: string;
  /** Present for clarification picks. */
  option?: string;
  /**
   * Checked ids from a Checkboxes group, filled in by the surface at click time
   * (web: from the rendered group; Slack: from state.values in the payload).
   */
  selection?: string[];
  /**
   * Words the person typed (a reply, a question, "something else"). The web
   * sends it with the action; Slack collects it in a modal first, since a
   * button value can't carry it.
   */
  text?: string;
}

/** Slack caps button values at 2000 chars; keep payloads to ids, never content. */
export const MAX_ACTION_VALUE = 2000;

export function encodeAction(payload: ActionPayload): string {
  const value = JSON.stringify(payload);
  if (value.length > MAX_ACTION_VALUE) {
    throw new RangeError(
      `action payload is ${value.length} chars; limit is ${MAX_ACTION_VALUE}. Pass ids, not content.`,
    );
  }
  return value;
}

export function decodeAction(value: string): ActionPayload {
  const parsed: unknown = JSON.parse(value);
  if (
    typeof parsed !== "object" ||
    parsed === null ||
    typeof (parsed as ActionPayload).turnId !== "string" ||
    typeof (parsed as ActionPayload).target !== "string"
  ) {
    throw new TypeError("not an vultus action payload");
  }
  return parsed as ActionPayload;
}

export interface UiAction {
  id: ActionId;
  payload: ActionPayload;
}

export type ActionHandler = (action: UiAction) => void;
