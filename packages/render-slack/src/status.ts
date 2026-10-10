import type { TaskStatus } from "@vultus/core";

/**
 * Slack has fewer task states than vultus. Plans accept `pending`; task cards
 * and stream task_update chunks accept only in_progress/complete/error
 * (verified with blocks.validate). `waiting` is blocked on a person, so in a
 * plan it shows as pending (no spinner); `denied` is an outcome, shown as error
 * with the reason in output.
 */
export function slackTaskStatus(status: TaskStatus, inPlan: boolean): string {
  switch (status) {
    case "waiting":
      return inPlan ? "pending" : "in_progress";
    case "pending":
      return inPlan ? "pending" : "in_progress";
    case "denied":
      return "error";
    default:
      return status;
  }
}

/** Words that replace a state Slack can't show natively. */
export function slackTaskNote(status: TaskStatus): string | undefined {
  if (status === "waiting") return "Waiting for approval";
  if (status === "denied") return "Not run: approval denied";
  return undefined;
}
