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
  feedback: "aui.feedback",
  retry: "aui.retry",
  approveSelected: "aui.approve.selected",
  denyAll: "aui.deny.all",
  ask: "aui.ask",
  remember: "aui.remember",
  unsure: "aui.clarify.unsure",
  resume: "aui.resume",
  compact: "aui.compact",
  reconnect: "aui.reconnect",
  report: "aui.report",
} as const;

export type ActionId = (typeof ActionId)[keyof typeof ActionId];

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
    throw new TypeError("not an agent-ui action payload");
  }
  return parsed as ActionPayload;
}

export interface UiAction {
  id: ActionId;
  payload: ActionPayload;
}

export type ActionHandler = (action: UiAction) => void;
