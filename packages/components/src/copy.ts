/**
 * All user-facing strings in one table so hosts can rebrand or translate
 * without forking. Conventions follow Slack's UI: buttons in Title Case,
 * everything else in sentence case, no end punctuation in headings, one
 * neutral voice, no emoji in buttons or headings.
 */
export const defaultCopy = {
  approve: "Approve",
  cancel: "Cancel",
  edit: "Edit",
  confirmDestructive: {
    title: "Are you sure?",
    text: "This can't be undone.",
    confirm: "Yes, Continue",
    deny: "Go Back",
  },
  actingAs: (who: string) => `Will run as ${who}`,
  expires: (time: string) => `Expires ${time}`,
  approvedBy: (by: string, time: string) => `Approved by ${by} · ${time}`,
  rejected: "Rejected, I won't run it.",
  rejectedBy: (by: string, time: string) => `Rejected by ${by} · ${time}`,
  timedOut: "Timed out, I didn't run it.",
  cancelled: "Cancelled, the request was stopped.",
  somethingElse: "Something Else",
  answeredBy: (by: string, choice: string, time: string) => `${by} chose ${choice} · ${time}`,
  declinedBy: (by: string, time: string) => `Declined by ${by} · ${time}`,
  approvalNeeded: (summary: string) => `Approval needed: ${summary}`,
  inputNeeded: (question: string) => `Input needed: ${question}`,
  planWorking: "Working",
  planDone: (seconds: number) => `Worked for ${seconds}s`,
  planError: "Error",
  planStopped: "Stopped",
  failedTitle: "Couldn't finish",
  stopped: "Okay, I stopped.",
  retry: "Retry",
  sources: "Sources",
  openSession: "Open full session",
  helpful: "Helpful",
  notHelpful: "Not helpful",
  status: {
    queued: "Queued",
    working: "Working",
    waiting: "Needs your input",
    done: "Done",
    failed: "Failed",
    stopped: "Stopped",
  },
  tone: { ok: "Done", warn: "Warning", bad: "Error", info: "Note", neutral: "Note" },
  reconnect: (service: string) => `Reconnect ${service}`,
  report: "Report Issue",
  retryIn: (seconds: number) =>
    `Try again in ${seconds < 90 ? `${seconds}s` : `${Math.round(seconds / 60)} min`}.`,
  reference: (ref: string) => `Reference: ${ref}`,
  failure: {
    auth_required: (service = "A connected app") => `${service} needs a reconnect`,
    rate_limited: () => "Rate limited",
    unavailable: (service = "A service") => `${service} is unavailable`,
    timeout: () => "Timed out",
    tool_error: (service = "A tool") => `${service} failed`,
    forbidden: () => "Not allowed",
    internal: () => "Something broke on my end",
  },
};

export type Copy = typeof defaultCopy;
