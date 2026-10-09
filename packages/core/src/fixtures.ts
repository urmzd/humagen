import { ActionId } from "./actions";
import {
  type Approval,
  type Clarification,
  type CodeSnippet,
  type CommandRun,
  type Failure,
  type FailureKind,
  type Notice,
  type Turn,
  timeToken,
} from "./types";

/**
 * Canonical fixtures. Stories and snapshot tests render these on every surface.
 * Copy follows Slack's agent guidance: task titles name the action (present
 * tense while running, past tense when done), facts go in details/output, and
 * notices name the cause and the fix.
 */

const agent = { name: "Assistant" };
const at = "2026-10-08T15:04:00Z";

const citations: Turn["citations"] = [
  {
    id: "c1",
    title: "Incident runbook: payments API",
    url: "https://example.com/runbooks/payments",
    source: "Runbooks",
    snippet: "Roll back the last deploy before paging the on-call.",
  },
  {
    id: "c2",
    title: "Deploy #4812",
    url: "https://example.com/deploys/4812",
    source: "Deploys",
  },
];

const base: Turn = {
  id: "turn-1",
  agent,
  status: "working",
  text: "",
  tasks: [],
  citations: [],
  interrupts: [],
  notices: [],
  sessionUrl: "https://example.com/sessions/turn-1",
  disclaimer: "AI-generated. Check important answers.",
};

export const streamingTurn: Turn = {
  ...base,
  planTitle: "Checking recent deploys",
  tasks: [
    {
      id: "t1",
      title: "Read the PagerDuty alert",
      status: "complete",
      tool: "pagerduty.get",
      output: "5xx rate 4.2% on payments-api since 14:52",
    },
    {
      id: "t2",
      title: "Searched runbooks",
      status: "complete",
      tool: "docs.search",
      details: 'Runbooks space, "payments"',
      output: "1 match",
      sources: [citations[0] as Turn["citations"][number]],
    },
    {
      id: "t3",
      title: "Checking recent deploys",
      status: "in_progress",
      tool: "deploys.list",
      details: "payments-api, last 24 hours",
    },
  ],
  text: "The error rate on **payments-api** started climbing at 14:52, two minutes after deploy",
};

export const doneTurn: Turn = {
  ...streamingTurn,
  status: "done",
  durationMs: 12_400,
  tasks: streamingTurn.tasks.map((t) =>
    t.id === "t3"
      ? {
          ...t,
          title: "Checked recent deploys",
          status: "complete",
          output: "3 deploys: #4810, #4811, #4812",
          sources: [citations[1] as Turn["citations"][number]],
        }
      : t,
  ),
  text: [
    "The error rate on **payments-api** started climbing at 14:52, two minutes after deploy `#4812` [[c2]].",
    "",
    "The runbook says to roll back before paging the on-call [[c1]]:",
    "",
    "1. Roll back `#4812`.",
    "2. Watch the 5xx rate for 10 minutes.",
    "3. If it holds, open a ticket against the deploy owner.",
  ].join("\n"),
  citations,
};

export const pendingApproval: Approval = {
  type: "approval",
  id: "a1",
  summary: "Roll back payments-api in production to deploy #4811?",
  tool: "deploys.rollback",
  approveLabel: "Roll Back",
  preview: [
    { label: "Service", value: "payments-api" },
    { label: "Environment", value: "production" },
    { label: "From", value: "#4812 (14:50)" },
    { label: "To", value: "#4811 (yesterday 17:20)" },
  ],
  args: { service: "payments-api", env: "prod", to: "4811" },
  destructive: true,
  editable: true,
  actingAs: "the deploy bot, on behalf of Dana",
  expiresAt: "2026-10-08T15:34:00Z",
  revision: 1,
  decision: { kind: "pending" },
};

export const approvedApproval: Approval = {
  ...pendingApproval,
  decision: { kind: "approved", by: "Dana", at },
};

export const deniedApproval: Approval = {
  ...pendingApproval,
  decision: { kind: "denied", by: "Dana", at, reason: "A hotfix is already rolling out." },
};

export const expiredApproval: Approval = {
  ...pendingApproval,
  decision: { kind: "expired", at },
};

export const pendingClarification: Clarification = {
  type: "clarification",
  id: "q1",
  question: "Which environment should I check?",
  options: [
    { id: "prod", label: "Check Production", description: "Where the alert fired" },
    { id: "staging", label: "Check Staging", description: "Same build, no customer traffic" },
  ],
  allowOther: true,
  answer: { kind: "pending" },
};

export const answeredClarification: Clarification = {
  ...pendingClarification,
  answer: { kind: "answered", by: "Dana", at, optionIds: ["prod"] },
};

export const waitingTurn: Turn = {
  ...doneTurn,
  status: "waiting",
  text: "Deploy `#4812` is the likely cause [[c2]]. I can roll it back.",
  interrupts: [pendingApproval],
};

export const failedTurn: Turn = {
  ...streamingTurn,
  status: "failed",
  durationMs: 31_000,
  tasks: streamingTurn.tasks.map((t) =>
    t.id === "t3"
      ? {
          ...t,
          title: "Couldn't check recent deploys",
          status: "error",
          output: "The deploys service timed out after 30s",
        }
      : t,
  ),
  failure: {
    kind: "timeout",
    message: "The deploys service didn't answer within 30s.",
    service: "the deploys service",
  },
};

/** One failure per kind, so every recovery path has a rendering to review. */
export const failures: Record<FailureKind, Failure> = {
  auth_required: {
    kind: "auth_required",
    message: "Your Linear connection expired.",
    service: "Linear",
  },
  rate_limited: {
    kind: "rate_limited",
    message: "The model provider is rate limiting requests.",
    service: "the model provider",
    retryAfterSeconds: 60,
  },
  unavailable: {
    kind: "unavailable",
    message: "The model provider isn't responding.",
    service: "the model provider",
  },
  timeout: failedTurn.failure as Failure,
  tool_error: {
    kind: "tool_error",
    message: "The rollback script exited with code 1.",
    service: "deploys",
  },
  forbidden: {
    kind: "forbidden",
    message: "Your workspace doesn't allow agents to post in #exec.",
  },
  internal: {
    kind: "internal",
    message: "Something broke on my end. I've logged it.",
    reference: "req_7f3a91",
  },
};

export const commandOk: CommandRun = {
  type: "command",
  id: "cmd1",
  command: "pnpm test --filter payments-api",
  cwd: "~/src/platform",
  status: "complete",
  exitCode: 0,
  durationMs: 8_400,
  output: [
    " RUN  v5.0.3 /src/platform/services/payments-api",
    "",
    " ✓ src/charge.test.ts (14 tests) 212ms",
    " ✓ src/refund.test.ts (9 tests) 98ms",
    "",
    " Test Files  2 passed (2)",
    "      Tests  23 passed (23)",
  ].join("\n"),
};

export const commandFailed: CommandRun = {
  type: "command",
  id: "cmd2",
  command: "pnpm run migrate --env prod",
  cwd: "~/src/platform",
  status: "error",
  exitCode: 1,
  durationMs: 2_100,
  output: [
    "> migrate --env prod",
    "Applying 0042_add_refund_reason.sql",
    'ERROR: column "reason" of relation "refunds" already exists',
    "Migration failed, rolled back 0042",
  ].join("\n"),
};

export const commandRunning: CommandRun = {
  type: "command",
  id: "cmd3",
  command: "docker build -t payments-api:4813 .",
  status: "in_progress",
  output: Array.from(
    { length: 80 },
    (_, i) => `#${i + 1} [build ${i + 1}/80] RUN step ${i + 1}`,
  ).join("\n"),
};

export const snippet: CodeSnippet = {
  type: "code",
  id: "code1",
  path: "services/payments-api/src/refund.ts",
  language: "ts",
  code: [
    "export function refundReason(input: RefundInput): string {",
    '  return input.reason?.trim() || "unspecified";',
    "}",
  ].join("\n"),
};

export const codingTurn: Turn = {
  ...base,
  id: "turn-2",
  status: "done",
  durationMs: 41_000,
  planTitle: "Fixing the refund migration",
  tasks: [
    { id: "k1", title: "Ran the payments tests", status: "complete", output: "23 passed" },
    {
      id: "k2",
      title: "Ran the migration",
      status: "error",
      output: "Column refunds.reason already exists",
    },
    {
      id: "k3",
      title: "Edited refund.ts",
      status: "complete",
      details: "services/payments-api/src/refund.ts",
    },
  ],
  text: "Migration `0042` failed because `refunds.reason` already exists. I made the migration idempotent and defaulted empty reasons in `refund.ts`.",
  attachments: [commandOk, commandFailed, snippet],
};

export const stoppedTurn: Turn = {
  ...streamingTurn,
  status: "stopped",
  text: "",
};

export const notices: Record<"slow" | "error" | "access" | "done", Notice> = {
  slow: {
    id: "n1",
    tone: "info",
    title: "Still working",
    body: `Started ${timeToken(at)}. Large deploy histories take a minute.`,
  },
  error: {
    id: "n2",
    tone: "bad",
    title: "Rate limited",
    body: "The model provider is rate limiting requests. Try again in a minute.",
    actions: [
      {
        label: "Retry",
        action: { id: ActionId.retry, payload: { turnId: "turn-1", target: "turn-1" } },
      },
    ],
  },
  access: {
    id: "n3",
    tone: "warn",
    title: "Linear needs a reconnect",
    body: "Your Linear connection expired, so I skipped the issue search.",
    actions: [
      {
        label: "Reconnect Linear",
        action: { id: ActionId.retry, payload: { turnId: "turn-1", target: "linear" } },
      },
    ],
  },
  done: {
    id: "n4",
    tone: "ok",
    title: "Rollback complete",
    body: "Error rate is back to baseline.",
  },
};
