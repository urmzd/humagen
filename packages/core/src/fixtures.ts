import { ActionId } from "./actions";
import type { HomeModel, HomeNotification } from "./home";
import { type InboxItem, type SystemAlert, sortInbox } from "./inbox";
import { type InboxMessage, messageFromItem, sortMessages } from "./mailbox";
import type { SessionState } from "./session";
import {
  type Approval,
  type Artifact,
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
  source: { label: "Incident console", url: "https://example.com/console/payments" },
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

/**
 * A report long enough to outgrow one Slack message (about 22k characters): a
 * wide table and a code block each straddle a message boundary.
 */
export const longTurn: Turn = {
  ...doneTurn,
  id: "turn-long",
  durationMs: 48_000,
  text: [
    "Here is every payments-api deploy from the last quarter, then the replay log for `#4812`.",
    "",
    "| Deploy | Time | Owner | 5xx rate | Result |",
    "| --- | --- | --- | --- | --- |",
    ...Array.from(
      { length: 240 },
      (_, i) =>
        `| #${4573 + i} | Day ${Math.floor(i / 3) + 1}, ${9 + (i % 3) * 4}:00 | team-${["ledger", "checkout", "risk"][i % 3]} | ${(0.04 + (i % 7) * 0.01).toFixed(2)}% | ${i === 239 ? "Rolled back" : "Healthy"} |`,
    ),
    "",
    "Only the last deploy moved the error rate [[c2]]. Its replay log:",
    "",
    "```text",
    ...Array.from(
      { length: 150 },
      (_, i) =>
        `14:${String(50 + Math.floor(i / 30)).padStart(2, "0")}:${String((i * 2) % 60).padStart(2, "0")} payments-api pod-${i % 6} POST /v1/charges ${i > 60 && i % 4 === 0 ? "502 upstream reset" : "200 ok"}`,
    ),
    "```",
    "",
    "The runbook says to roll back before paging the on-call [[c1]].",
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

// ---------------------------------------------------------------------------
// Variants found in the upstream apps: gated tasks, batches, stop phases,
// runtime notices, delegation, usage, and session-level state.

export const secondApproval: Approval = {
  type: "approval",
  id: "a2",
  summary: "Post the incident summary to #payments-oncall (38 members)?",
  tool: "slack.post",
  approveLabel: "Post Summary",
  preview: [
    { label: "Channel", value: "#payments-oncall" },
    { label: "Mentions", value: "@payments-oncall" },
  ],
  actingAs: "you",
  rememberable: true,
  decision: { kind: "pending" },
};

export const askedApproval: Approval = {
  ...pendingApproval,
  decision: {
    kind: "asked",
    by: "Dana",
    at,
    question: "Will this also roll back the config change from #4812?",
  },
};

/** Two gated calls at once: the web shows a batch with per-row choices. */
export const batchTurn: Turn = {
  ...doneTurn,
  id: "turn-batch",
  status: "waiting",
  text: "Deploy `#4812` is the likely cause [[c2]]. I can roll it back and tell on-call.",
  tasks: [
    ...doneTurn.tasks,
    {
      id: "g1",
      title: "Roll back payments-api",
      status: "waiting",
      tool: "deploys.rollback",
      interruptId: "a1",
    },
    {
      id: "g2",
      title: "Post incident summary",
      status: "waiting",
      tool: "slack.post",
      interruptId: "a2",
    },
  ],
  interrupts: [
    { ...pendingApproval, initiator: "dana" },
    { ...secondApproval, initiator: "dana" },
  ],
};

export const deniedTaskTurn: Turn = {
  ...doneTurn,
  id: "turn-denied",
  tasks: [
    ...doneTurn.tasks,
    {
      id: "g1",
      title: "Roll back payments-api",
      status: "denied",
      interruptId: "a1",
      output: "Dana said no",
    },
  ],
  interrupts: [deniedApproval],
};

export const unsureClarification: Clarification = {
  ...pendingClarification,
  allowUnsure: true,
  answer: { kind: "unsure", by: "Dana", at },
};

export const stoppingTurn: Turn = {
  ...streamingTurn,
  id: "turn-stop",
  status: "stopped",
  stop: "requested",
};
export const stopFailedTurn: Turn = { ...stoppingTurn, stop: "failed" };
export const stopConfirmedTurn: Turn = { ...stoppingTurn, stop: "confirmed" };

export const runtimeNotices: Record<"silence" | "fallback" | "retried" | "spend", Notice> = {
  silence: {
    id: "s1",
    kind: "silence",
    tone: "neutral",
    title: "Still working",
    body: "2m so far, nothing to show yet.",
  },
  fallback: {
    id: "s2",
    kind: "model_fallback",
    tone: "neutral",
    body: "Switched to the backup model after the primary timed out.",
  },
  retried: {
    id: "s3",
    kind: "retried",
    tone: "neutral",
    body: "Retried twice after an interrupted response.",
  },
  spend: {
    id: "s4",
    kind: "spend_blocked",
    tone: "warn",
    title: "Next turn would pass the spend cap",
    body: "This workspace has $0.42 left of its $50 monthly cap. Add budget to keep going.",
    actions: [
      {
        label: "Add Budget",
        action: { id: ActionId.reconnect, payload: { turnId: "turn-1", target: "budget" } },
      },
    ],
  },
};

export const delegatingTurn: Turn = {
  ...streamingTurn,
  id: "turn-sub",
  activity: "delegating",
  planTitle: "Delegating to the deploy agent",
  tasks: [
    ...(streamingTurn.tasks.slice(0, 2) as Turn["tasks"]),
    {
      id: "d0",
      title: "Asked the billing agent for refund volume",
      status: "complete",
      output: "1,284 refunds in Q3",
      subagent: {
        name: "Billing agent",
        runId: "run-d0",
        summary: "1,284 refunds in Q3, 212 of them disputed",
        steps: 3,
        tokens: 5_200,
        durationMs: 8_000,
        url: "https://example.com/runs/d0",
        hue: 90,
      },
    },
    {
      id: "d1",
      title: "Asked the deploy agent to compare #4811 and #4812",
      status: "in_progress",
      subagent: {
        name: "Deploy agent",
        runId: "run-d1",
        steps: 6,
        tokens: 12_400,
        url: "https://example.com/runs/d1",
        hue: 185,
      },
    },
  ],
};

export const handoffTurn: Turn = {
  ...doneTurn,
  id: "turn-handoff",
  handoff: { to: "Billing agent", reason: "The question is about refunds, which billing owns." },
};

export const usageTurn: Turn = {
  ...doneTurn,
  id: "turn-usage",
  usage: {
    model: "claude-sonnet-5-5",
    inputTokens: 18_240,
    outputTokens: 912,
    cacheReadTokens: 14_100,
    costUsd: 0.071,
    latencyMs: 12_400,
  },
};

export const sessions: Record<
  "fresh" | "busy" | "compacting" | "compacted" | "nearLimit" | "overBudget",
  SessionState
> = {
  fresh: {
    model: "claude-sonnet-5-5",
    approvalMode: "ask",
    context: { usedTokens: 4_200, limitTokens: 200_000, compactAt: 0.8 },
    budget: { spentUsd: 3.1, limitUsd: 50, period: "month" },
    status: "idle",
    activity: "idle",
  },
  busy: {
    model: "claude-sonnet-5-5",
    approvalMode: "ask",
    context: { usedTokens: 92_000, limitTokens: 200_000, compactAt: 0.8 },
    budget: { spentUsd: 18.4, limitUsd: 50, period: "month", nextTurnUsd: 0.09 },
    queued: 2,
    status: "working",
    activity: "searching",
  },
  compacting: {
    model: "claude-sonnet-5-5",
    approvalMode: "auto",
    context: { usedTokens: 168_000, limitTokens: 200_000, compactAt: 0.8 },
    compaction: { kind: "compacting", startedAt: at },
    budget: { spentUsd: 22, limitUsd: 50, period: "month" },
    status: "working",
    activity: "thinking",
  },
  compacted: {
    model: "claude-sonnet-5-5",
    approvalMode: "ask",
    context: { usedTokens: 41_000, limitTokens: 200_000, compactAt: 0.8 },
    compaction: {
      kind: "compacted",
      at,
      savedTokens: 127_000,
      summary:
        "Kept the incident timeline, decisions, and open questions; dropped raw tool output.",
    },
    budget: { spentUsd: 22.3, limitUsd: 50, period: "month" },
    status: "idle",
  },
  nearLimit: {
    model: "claude-sonnet-5-5",
    approvalMode: "ask",
    context: { usedTokens: 194_000, limitTokens: 200_000, compactAt: 0.8 },
    compaction: {
      kind: "failed",
      at,
      reason: "Nothing left to summarize without losing decisions.",
    },
    budget: { spentUsd: 31, limitUsd: 50, period: "month" },
    status: "idle",
  },
  overBudget: {
    model: "claude-sonnet-5-5",
    approvalMode: "ask",
    context: { usedTokens: 30_000, limitTokens: 200_000 },
    budget: { spentUsd: 49.58, limitUsd: 50, period: "month", nextTurnUsd: 0.6 },
    status: "idle",
  },
};

const artifactBase: Artifact = {
  type: "artifact",
  id: "f1",
  title: "refunds-q3.xlsx",
  kind: "spreadsheet",
  mimeType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
  phase: "ready",
  sizeBytes: 24_576,
  version: 2,
  url: "https://example.com/files/f1",
  downloadUrl: "https://example.com/files/f1/download",
  description: "3 sheets: Summary, Refunds by reason, Disputes",
};

/** One artifact per phase, plus other kinds, so every state has a rendering. */
export const artifacts: Record<
  | "queued"
  | "generating"
  | "waiting"
  | "finalizing"
  | "ready"
  | "failed"
  | "denied"
  | "report"
  | "image",
  Artifact
> = {
  queued: {
    ...artifactBase,
    phase: "queued",
    sizeBytes: undefined,
    url: undefined,
    downloadUrl: undefined,
  },
  generating: {
    ...artifactBase,
    phase: "generating",
    sizeBytes: undefined,
    url: undefined,
    downloadUrl: undefined,
  },
  waiting: {
    ...artifactBase,
    phase: "waiting",
    sizeBytes: undefined,
    url: undefined,
    downloadUrl: undefined,
  },
  finalizing: { ...artifactBase, phase: "finalizing", url: undefined, downloadUrl: undefined },
  ready: artifactBase,
  failed: {
    ...artifactBase,
    phase: "failed",
    url: undefined,
    downloadUrl: undefined,
    error: "The refunds export timed out after 60s. Try a smaller date range.",
  },
  denied: { ...artifactBase, phase: "denied", url: undefined, downloadUrl: undefined },
  report: {
    type: "artifact",
    id: "f2",
    title: "incident-4812-postmortem.md",
    kind: "document",
    mimeType: "text/markdown",
    phase: "ready",
    sizeBytes: 6_200,
    url: "https://example.com/files/f2",
    downloadUrl: "https://example.com/files/f2/download",
    previewText:
      "## Summary\nA config change in deploy #4812 raised the 5xx rate to 4.2% for 14 minutes…",
  },
  image: {
    type: "artifact",
    id: "f3",
    title: "error-rate.png",
    kind: "image",
    mimeType: "image/png",
    phase: "ready",
    sizeBytes: 182_000,
    url: "https://example.com/files/f3",
    downloadUrl: "https://example.com/files/f3/download",
    thumbnailUrl: "https://placehold.co/640x320/png?text=error+rate",
    description: "5xx rate, 14:30 to 15:30",
  },
};

export const artifactTurn: Turn = {
  ...doneTurn,
  id: "turn-artifacts",
  planTitle: undefined,
  tasks: [
    { id: "x1", title: "Exported Q3 refunds", status: "complete", output: "1,284 rows" },
    { id: "x2", title: "Drafted the postmortem", status: "complete" },
  ],
  text: "Here's the Q3 refunds export and a first draft of the postmortem.",
  citations: [],
  attachments: [artifacts.ready, artifacts.report],
};

export const systemAlerts: Record<"degraded" | "down" | "maintenance" | "resolved", SystemAlert> = {
  degraded: {
    id: "inc-1",
    service: "Model provider",
    status: "degraded",
    title: "Responses are slower than usual",
    body: "Answers may take up to a minute. Nothing is lost; streams resume on their own.",
    startedAt: "2026-10-08T14:40:00Z",
    updatedAt: "2026-10-08T14:58:00Z",
    url: "https://status.example.com/incidents/1",
    affects: ["Streaming", "Long answers"],
  },
  down: {
    id: "inc-2",
    service: "Deploys API",
    status: "down",
    title: "Rollbacks and deploy history are unavailable",
    body: "Approvals that roll back a deploy will wait until it recovers.",
    startedAt: "2026-10-08T15:01:00Z",
    updatedAt: "2026-10-08T15:01:00Z",
    url: "https://status.example.com/incidents/2",
    affects: ["deploys.rollback", "deploys.list"],
  },
  maintenance: {
    id: "inc-3",
    service: "Search index",
    status: "maintenance",
    title: "Reindexing runbooks until 16:00",
    body: "New runbook pages may not show up in answers yet.",
    startedAt: "2026-10-08T15:00:00Z",
    updatedAt: "2026-10-08T15:00:00Z",
  },
  resolved: {
    id: "inc-1",
    service: "Model provider",
    status: "resolved",
    title: "Response times are back to normal",
    startedAt: "2026-10-08T14:40:00Z",
    updatedAt: "2026-10-08T15:20:00Z",
    url: "https://status.example.com/incidents/1",
  },
};

export const inboxItems: InboxItem[] = sortInbox([
  {
    id: "i1",
    kind: "approval",
    approval: pendingApproval,
    state: "open",
    source: { label: "#payments-incidents", turnId: "turn-1", url: "https://example.com/t/1" },
    at: "2026-10-08T15:04:00Z",
  },
  {
    id: "i2",
    kind: "approval",
    approval: secondApproval,
    state: "open",
    source: { label: "#payments-incidents", turnId: "turn-1", url: "https://example.com/t/1" },
    at: "2026-10-08T15:05:00Z",
  },
  {
    id: "i3",
    kind: "clarification",
    clarification: pendingClarification,
    state: "open",
    source: { label: "DM with Assistant", turnId: "turn-3", url: "https://example.com/t/3" },
    at: "2026-10-08T14:50:00Z",
  },
  {
    id: "i4",
    kind: "failure",
    failure: failures.auth_required,
    state: "open",
    source: { label: "Weekly triage", turnId: "turn-4", url: "https://example.com/t/4" },
    at: "2026-10-08T13:00:00Z",
  },
  {
    id: "i5",
    kind: "alert",
    alert: systemAlerts.down,
    state: "open",
    source: { label: "Status" },
    at: "2026-10-08T15:01:00Z",
  },
  {
    id: "i6",
    kind: "alert",
    alert: systemAlerts.degraded,
    state: "open",
    source: { label: "Status" },
    at: "2026-10-08T14:58:00Z",
  },
  {
    id: "i7",
    kind: "approval",
    approval: approvedApproval,
    state: "resolved",
    source: { label: "#payments-incidents", turnId: "turn-0" },
    at: "2026-10-08T12:00:00Z",
  },
]);

// ---------------------------------------------------------------------------
// Human-in-the-loop, artifact, reference and alert variants. Each is a whole
// turn, so it posts, streams and replays the same way on every surface.

/** The agent wants a new capability: the reviewer sees what it is, where it's from and what it can touch. */
export const skillApproval: Approval = {
  type: "approval",
  id: "a-skill",
  summary: "Add the Incident postmortem skill to Assistant?",
  tool: "skills.install",
  approveLabel: "Add Skill",
  preview: [
    { label: "Skill", value: "incident-postmortem 1.2.0" },
    { label: "From", value: "[acme/agent-skills](https://example.com/acme/agent-skills)" },
    { label: "Can use", value: "deploys.list, pagerduty.get, files.create" },
    { label: "Applies to", value: "Assistant, in every conversation" },
  ],
  actingAs: "you, as a workspace admin",
  decision: { kind: "pending" },
};

export const skillApprovalTurn: Turn = {
  ...base,
  id: "turn-skill",
  status: "waiting",
  tasks: [
    { id: "k1", title: "Searched the skill catalog", status: "complete", output: "2 matches" },
    {
      id: "k2",
      title: "Read the skill's instructions",
      status: "complete",
      details: "incident-postmortem/SKILL.md",
    },
  ],
  text: "Postmortems are written by hand today. The **Incident postmortem** skill [[s1]] would let me draft one from the timeline as soon as an incident closes. It adds tools I don't have yet, so it needs your approval.",
  citations: [
    {
      id: "s1",
      title: "incident-postmortem",
      url: "https://example.com/acme/agent-skills/incident-postmortem",
      source: "Skill catalog",
    },
  ],
  attachments: [
    {
      type: "code",
      id: "skill-md",
      path: "incident-postmortem/SKILL.md",
      language: "markdown",
      code: [
        "---",
        "name: incident-postmortem",
        "description: Draft a postmortem from an incident timeline. Use when an incident closes.",
        "allowed-tools: deploys.list, pagerduty.get, files.create",
        "---",
        "",
        "1. Read the alert and every deploy in the incident window.",
        "2. Write the summary, timeline, cause and follow-ups.",
        "3. Save the draft as a file. Never post it without approval.",
      ].join("\n"),
    },
  ],
  interrupts: [skillApproval],
};

/** The agent proposes a change to its own instructions: editable, and shown as a diff. */
export const promptApproval: Approval = {
  type: "approval",
  id: "a-prompt",
  summary: "Update Assistant's instructions for incident replies?",
  tool: "agent.update_prompt",
  approveLabel: "Update Prompt",
  preview: [
    { label: "Agent", value: "Assistant" },
    { label: "Section", value: "Incident replies" },
    { label: "Takes effect", value: "New conversations only" },
  ],
  editable: true,
  revision: 2,
  actingAs: "you, as the agent's owner",
  decision: { kind: "pending" },
};

export const promptApprovalTurn: Turn = {
  ...base,
  id: "turn-prompt",
  status: "waiting",
  tasks: [
    {
      id: "p1",
      title: "Reviewed this week's incident threads",
      status: "complete",
      output: "3 people asked who owns the deploy",
    },
  ],
  text: "Three people asked this week who owns a deploy after I reported an incident. I'd like to name the owner every time. Here is the change to my instructions.",
  attachments: [
    {
      type: "code",
      id: "prompt-diff",
      path: "Assistant instructions: Incident replies",
      language: "diff",
      code: [
        " When you report an incident, lead with the service and the time it started.",
        "-Link the deploy that caused it.",
        "+Link the deploy that caused it, and name its owner and their team.",
        "+If the owner is unknown, say so instead of guessing.",
      ].join("\n"),
    },
  ],
  interrupts: [promptApproval],
};

/** The agent can't continue without a choice. */
export const clarificationTurn: Turn = {
  ...base,
  id: "turn-clarify",
  status: "waiting",
  tasks: [
    {
      id: "q1",
      title: "Read the PagerDuty alert",
      status: "complete",
      output: "No environment tag on the alert",
    },
  ],
  text: "I can check the error rate, but the alert doesn't say which environment fired it.",
  interrupts: [{ ...pendingClarification, allowUnsure: true }],
};

/** Creating the file is the gated action: nothing exists until someone approves. */
export const artifactApproval: Approval = {
  type: "approval",
  id: "a-file",
  summary: "Create refunds-q3.xlsx with 1,284 customer emails?",
  tool: "files.create",
  approveLabel: "Create File",
  preview: [
    { label: "Rows", value: "1,284 refunds" },
    { label: "Includes", value: "Customer email, amount, reason" },
    { label: "Shared with", value: "This thread (4 people)" },
  ],
  actingAs: "you",
  decision: { kind: "pending" },
};

export const artifactApprovalTurn: Turn = {
  ...base,
  id: "turn-artifact-approval",
  status: "waiting",
  tasks: [{ id: "x1", title: "Queried Q3 refunds", status: "complete", output: "1,284 rows" }],
  text: "The export is ready to write. It includes customer emails, so I need your go-ahead before I create the file.",
  attachments: [artifacts.waiting],
  interrupts: [artifactApproval],
};

/** Several files at different stages of the same run. */
export const artifactProgressTurn: Turn = {
  ...base,
  id: "turn-artifact-progress",
  tasks: [
    { id: "x1", title: "Exported Q3 refunds", status: "complete", output: "1,284 rows" },
    { id: "x2", title: "Drafting the postmortem", status: "in_progress" },
  ],
  text: "The export is done. I'm writing the postmortem now, and the chart comes after it.",
  attachments: [
    artifacts.ready,
    {
      ...artifacts.report,
      phase: "generating",
      sizeBytes: undefined,
      url: undefined,
      downloadUrl: undefined,
    },
    {
      ...artifacts.image,
      phase: "queued",
      sizeBytes: undefined,
      url: undefined,
      downloadUrl: undefined,
    },
  ],
};

/** One file landed and one didn't: the answer still stands, and the failure says what to do. */
export const artifactFailedTurn: Turn = {
  ...base,
  id: "turn-artifact-failed",
  status: "done",
  durationMs: 64_000,
  tasks: [
    { id: "x1", title: "Drafted the postmortem", status: "complete" },
    {
      id: "x2",
      title: "Couldn't export Q3 refunds",
      status: "error",
      output: "Timed out after 60s",
    },
  ],
  text: "The postmortem draft is ready. The refunds export timed out, so that file wasn't created.",
  attachments: [artifacts.report, artifacts.failed],
};

/** A file that replaces an earlier one. */
export const artifactRevisedTurn: Turn = {
  ...base,
  id: "turn-artifact-revised",
  status: "done",
  durationMs: 9_000,
  tasks: [{ id: "x1", title: "Added the Disputes sheet", status: "complete", output: "212 rows" }],
  text: "I added the disputes you asked for. This version replaces the earlier export.",
  attachments: [
    {
      ...artifacts.ready,
      version: 2,
      description: "4 sheets: Summary, Refunds, Disputes, Notes. Replaces version 1.",
    },
  ],
};

/**
 * Every kind of reference at once: web pages, a Slack message, a source with
 * no link, one cited twice, some never cited, and more than fit on one line.
 */
export const referencesTurn: Turn = {
  ...base,
  id: "turn-references",
  status: "done",
  durationMs: 21_000,
  tasks: [
    {
      id: "r1",
      title: "Searched runbooks, deploys and Slack",
      status: "complete",
      output: "12 sources",
    },
  ],
  text: [
    "Deploy `#4812` changed the retry budget [[ref-pr]], and the 5xx rate rose two minutes later [[ref-dash]].",
    "",
    "On-call saw it first in Slack [[ref-slack]]. The runbook says to roll back before paging [[ref-runbook]], which matches the restricted postmortem template [[ref-template]].",
    "",
    "Rolling back is the documented first step [[ref-runbook]].",
  ].join("\n"),
  citations: [
    {
      id: "ref-runbook",
      title: "Incident runbook: payments API",
      url: "https://example.com/runbooks/payments",
      source: "Runbooks",
      snippet: "Roll back the last deploy before paging the on-call.",
    },
    {
      id: "ref-pr",
      title: "Lower the retry budget (#1931)",
      url: "https://example.com/acme/payments-api/pull/1931",
      source: "GitHub",
    },
    {
      id: "ref-dash",
      title: "payments-api 5xx rate",
      url: "https://example.com/dashboards/payments?from=14:30&to=15:30",
      source: "Dashboards",
    },
    {
      id: "ref-slack",
      title: "Dana in #payments-oncall",
      url: "https://example.slack.com/archives/C0PAYMENTS/p1791600000000100",
      source: "Slack",
      snippet: "Seeing 502s on charges since the deploy.",
    },
    { id: "ref-template", title: "Postmortem template", source: "Drive, restricted" },
    ...Array.from({ length: 7 }, (_, i) => ({
      id: `ref-deploy-${i}`,
      title: `Deploy #${4805 + i}`,
      url: `https://example.com/deploys/${4805 + i}`,
      source: "Deploys",
    })),
  ],
};

/** What the runtime did on its own, said quietly next to an answer that still stands. */
export const noticesTurn: Turn = {
  ...doneTurn,
  id: "turn-notices",
  notices: [runtimeNotices.fallback, runtimeNotices.retried, notices.slow],
};

/** A failure the reader can fix themselves. */
export const reconnectTurn: Turn = {
  ...failedTurn,
  id: "turn-reconnect",
  failure: failures.auth_required,
};

/** A failure that fixes itself: say when to try again. */
export const rateLimitedTurn: Turn = {
  ...failedTurn,
  id: "turn-rate-limited",
  failure: failures.rate_limited,
};

// ---------------------------------------------------------------------------
// The mailbox: one message per way of resolving, plus the read-only ones.

const assistant = { kind: "agent" as const, name: "Assistant" };

export const mailMessages: InboxMessage[] = sortMessages([
  {
    id: "m-approval",
    subject: "Roll back payments-api?",
    from: assistant,
    at: "2026-10-08T15:04:00Z",
    unread: true,
    severity: "warn",
    source: {
      kind: "turn",
      label: "#payments-incidents",
      id: "turn-1",
      url: "https://example.com/t/1",
    },
    parts: [
      {
        type: "text",
        id: "why",
        text: "Deploy `#4812` is the likely cause of the 5xx rate. Rolling back is the runbook's first step.",
      },
      { type: "approval", id: pendingApproval.id, approval: pendingApproval },
      { type: "reference", id: "refs", citations },
    ],
  },
  {
    id: "m-skill",
    subject: "Add the Incident postmortem skill?",
    from: assistant,
    at: "2026-10-08T14:58:00Z",
    unread: true,
    source: {
      kind: "project",
      label: "Payments on-call",
      url: "https://example.com/projects/payments",
    },
    parts: [
      {
        type: "text",
        id: "why",
        text: "It would let me draft a postmortem as soon as an incident closes. It adds tools I don't have yet.",
      },
      { type: "approval", id: skillApproval.id, approval: skillApproval },
    ],
  },
  {
    id: "m-choice",
    subject: "Which environment should I check?",
    from: assistant,
    at: "2026-10-08T14:50:00Z",
    unread: true,
    source: {
      kind: "turn",
      label: "DM with Assistant",
      id: "turn-3",
      url: "https://example.com/t/3",
    },
    parts: [
      {
        type: "choice",
        id: pendingClarification.id,
        clarification: { ...pendingClarification, allowUnsure: true },
      },
    ],
  },
  {
    id: "m-reply",
    subject: "Who should own the follow-up?",
    from: assistant,
    at: "2026-10-08T14:40:00Z",
    source: { kind: "run", label: "Postmortem draft", url: "https://example.com/runs/71" },
    parts: [
      {
        type: "text",
        id: "why",
        text: "The postmortem has one open follow-up: add an alert on the retry budget.",
      },
      {
        type: "reply",
        id: "owner",
        prompt: "Who should own it?",
        placeholder: "A person or a team",
      },
      { type: "artifact", id: "draft", artifact: artifacts.report },
    ],
  },
  {
    id: "m-memory",
    subject: "3 things worth remembering from this incident",
    from: assistant,
    at: "2026-10-08T14:30:00Z",
    source: {
      kind: "project",
      label: "Payments on-call",
      url: "https://example.com/projects/payments",
    },
    parts: [
      {
        type: "memory",
        id: "memories",
        proposals: [
          {
            id: "mem1",
            text: "Roll back before paging for payments-api incidents",
            scope: "This project",
          },
          { id: "mem2", text: "Dana owns deploys to payments-api", scope: "This project" },
          { id: "mem3", text: "Report times in UTC", scope: "All conversations" },
        ],
      },
    ],
  },
  {
    id: "m-budget",
    subject: "Nightly eval is over its budget",
    from: { kind: "rule", name: "Spend limit" },
    at: "2026-10-08T14:20:00Z",
    unread: true,
    severity: "warn",
    source: { kind: "policy", label: "Evals budget", url: "https://example.com/policies/evals" },
    parts: [
      {
        type: "budget",
        id: "limit",
        meter: { used: 48.2, limit: 50, unit: "usd", period: "month", needs: 6.4 },
        raiseTo: [75, 100],
      },
    ],
  },
  {
    id: "m-runs",
    subject: "Support triage hit its daily run limit",
    from: { kind: "rule", name: "Run limit" },
    at: "2026-10-08T13:40:00Z",
    severity: "warn",
    source: { kind: "policy", label: "Triage limits", url: "https://example.com/policies/triage" },
    parts: [
      {
        type: "budget",
        id: "runs",
        meter: { used: 200, limit: 200, unit: "runs", period: "day", needs: 1 },
        raiseTo: [300, 500],
      },
    ],
  },
  {
    id: "m-limit",
    subject: "Support triage reached its monthly spend limit",
    from: { kind: "rule", name: "Spend limit" },
    at: "2026-10-08T13:20:00Z",
    severity: "warn",
    source: { kind: "policy", label: "Triage spend", url: "https://example.com/policies/triage" },
    parts: [
      {
        type: "budget",
        id: "spend",
        meter: { used: 250, limit: 250, unit: "usd", period: "month" },
        // The platform names no amounts: the reader types one, or only acknowledges it.
        allowed: [ActionId.raiseBudget, ActionId.acknowledge],
      },
    ],
  },
  {
    id: "m-alert",
    subject: "5xx rate above 2% on payments-api",
    from: { kind: "rule", name: "Error rate watch" },
    at: "2026-10-08T14:54:00Z",
    unread: true,
    severity: "bad",
    source: { kind: "rule", label: "Error rate watch", url: "https://example.com/rules/5xx" },
    parts: [
      {
        type: "alert",
        id: "a-5xx",
        ruleUrl: "https://example.com/rules/5xx",
        alert: {
          id: "a-5xx",
          service: "payments-api",
          status: "down",
          title: "5xx rate is 4.2%",
          body: "The rule fires above 2% for 2 minutes.",
          startedAt: "2026-10-08T14:52:00Z",
          updatedAt: "2026-10-08T14:54:00Z",
          affects: ["Charges", "Refunds"],
        },
      },
    ],
  },
  {
    id: "m-failure",
    subject: "Weekly report didn't run",
    from: { kind: "system", name: "Scheduler" },
    at: "2026-10-08T13:00:00Z",
    severity: "bad",
    source: { kind: "run", label: "Weekly report", url: "https://example.com/runs/64" },
    parts: [{ type: "failure", id: "failure", failure: failures.auth_required }],
  },
  {
    id: "m-fyi",
    subject: "Q3 refunds export is ready",
    from: assistant,
    at: "2026-10-08T12:10:00Z",
    source: { kind: "run", label: "Refunds export", url: "https://example.com/runs/58" },
    parts: [
      { type: "text", id: "note", text: "1,284 rows across 3 sheets. Nothing needs a decision." },
      { type: "artifact", id: "file", artifact: artifacts.ready },
    ],
  },
  {
    id: "m-custom",
    subject: "Review the eval regression",
    from: { kind: "rule", name: "Eval gate" },
    at: "2026-10-08T11:30:00Z",
    source: { kind: "run", label: "Nightly eval", url: "https://example.com/runs/51" },
    parts: [
      {
        type: "custom",
        id: "eval",
        name: "eval-diff",
        fallback: "Groundedness fell from 0.91 to 0.84 on 12 cases.",
        data: { metric: "groundedness", before: 0.91, after: 0.84, cases: 12 },
        resolvable: true,
      },
    ],
  },
  {
    id: "m-done",
    subject: "Post the incident summary to #payments-oncall?",
    from: assistant,
    at: "2026-10-08T10:00:00Z",
    source: {
      kind: "turn",
      label: "#payments-incidents",
      id: "turn-1",
      url: "https://example.com/t/1",
    },
    parts: [
      {
        type: "approval",
        id: secondApproval.id,
        approval: { ...secondApproval, decision: { kind: "approved", by: "Dana", at } },
      },
    ],
  },
]);

/** What a person asked, in their own words: the web's bubble, Slack's thread root. */
export const request = { by: "Dana", text: "Why is payments-api returning 5xx since 14:52?" };

/** The agents a person can ask, as `/agent help` and the web's agent menu list them. */
export const agentList: { name: string; description?: string }[] = [
  { name: "oncall", description: "Investigates alerts and incidents, and proposes fixes." },
  { name: "author", description: "Builds and changes agents. You approve every change." },
  { name: "researcher", description: "Searches docs and the wiki, and cites what it used." },
  { name: "scratch" },
];

/** A run still going after its live output was closed: followed quietly, shown without motion. */
export const quietTurn: Turn = { ...streamingTurn, id: "turn-quiet", detached: true };

/** A finished turn the reader already rated. */
export const ratedTurn: Turn = { ...doneTurn, id: "turn-rated", feedback: "up" };

/** The older item list, read as messages: what `inboxFromTurns` producers get for free. */
export const mailFromItems: InboxMessage[] = inboxItems.map((item) => messageFromItem(item));

/**
 * Home, as Ada sees it: an operator through eng-oncall, with two inbox items
 * waiting on her and two open alerts, GitHub connected and Jira not. Each
 * notification links to its own page. The variants change one thing each.
 */
export const home: HomeModel = {
  person: { id: "U0ADA", name: "Ada Lovelace" },
  assistant: { name: "Collegium" },
  intro: "I run this workspace's agents.",
  ask: { placeholder: "Why did last night's deploy fail?", attach: true },
  starts: [
    { id: "talk" },
    { id: "build" },
    { id: "search" },
    { id: "review", count: 2 },
    { id: "runs" },
    { id: "schedule" },
  ],
  notifications: {
    items: [
      {
        kind: "inbox",
        id: "msg-docs-reply",
        from: "docs-bot",
        summary: "wants to post a reply in #support.",
        detail: "Waiting for your approval.",
        at: "2026-10-13T14:05:00Z",
        url: "https://example.com/console/?view=inbox&message=msg-docs-reply",
      },
      {
        kind: "alert",
        id: "al-deploy-failed",
        from: "deploy-failures",
        summary: "fired: a run of deployer failed.",
        detail: "The deploy to production stopped at the migration step.",
        at: "2026-10-13T13:58:00Z",
        severity: "critical",
        url: "https://example.com/console/?view=alerts&alert=al-deploy-failed",
      },
      {
        kind: "inbox",
        id: "msg-triage-revision",
        from: "author",
        summary: "drafted a change to the triage agent.",
        detail: "Approve it to save the new revision.",
        at: "2026-10-13T12:40:00Z",
        url: "https://example.com/console/?view=inbox&message=msg-triage-revision",
      },
      {
        kind: "alert",
        id: "al-slow-triage",
        from: "slow-runs",
        summary: "fired: a run of triage took 94s.",
        detail: "The limit is 60s.",
        at: "2026-10-13T11:15:00Z",
        severity: "warning",
        url: "https://example.com/console/?view=alerts&alert=al-slow-triage",
      },
    ],
    inbox: { total: 2, url: "https://example.com/console/?view=inbox" },
    alerts: { total: 2, url: "https://example.com/console/?view=alerts" },
  },
  answeredBy: {
    agents: [
      { id: "docs-bot", name: "docs-bot" },
      { id: "triage", name: "triage" },
      { id: "author", name: "author" },
    ],
  },
  connections: [
    {
      id: "slack",
      name: "Slack",
      description: "Mention an agent in any channel it has been added to.",
      connected: true,
      account: "Acme",
      canConnect: true,
    },
    {
      id: "github",
      name: "GitHub",
      description: "Agents answer on issues and review pull requests.",
      connected: true,
      account: "acme-corp",
      canConnect: true,
    },
    {
      id: "jira",
      name: "Jira",
      connected: false,
      canConnect: true,
    },
  ],
  personal: { connected: 0, canConnect: true },
  connectionsUrl: "https://example.com/console/connections",
  access: {
    role: "operator",
    group: "eng-oncall",
    via: "your company's identity provider",
    allows: "run agents and manage schedules",
    denies: "change what an agent is",
    explainUrl: "https://example.com/console/access/why",
    policyUrl: "https://example.com/console/access/policy",
  },
  consoleUrl: "https://example.com/console",
  links: [
    { label: "Documentation", url: "https://example.com/docs" },
    { label: "API reference", url: "https://example.com/docs/api" },
    { label: "Command line", url: "https://example.com/docs/cli" },
    { label: "Open in Slack", url: "https://slack.com/app_redirect?app=A0C8T98JWJC" },
  ],
};

const alertsOnly = (h: HomeModel) => h.notifications.items.filter((n) => n.kind === "alert");

/** Twelve inbox items and six alerts, more than Home shows: the rest are counted and linked. */
const many: HomeNotification[] = [
  ...Array.from({ length: 12 }, (_, i) => ({
    kind: "inbox" as const,
    id: `msg-${i + 1}`,
    from: i % 2 ? "triage" : "docs-bot",
    summary:
      i % 2 ? "asked which queue a ticket belongs in." : "wants to post a reply in #support.",
    at: new Date(Date.parse("2026-10-13T14:00:00Z") - i * 600_000).toISOString(),
    url: `https://example.com/console/?view=inbox&message=msg-${i + 1}`,
  })),
  ...Array.from({ length: 6 }, (_, i) => ({
    kind: "alert" as const,
    id: `al-${i + 1}`,
    from: "failure-rate",
    summary: `fired: ${40 + i * 5}% of runs of deployer failed.`,
    at: new Date(Date.parse("2026-10-13T14:03:00Z") - i * 900_000).toISOString(),
    severity: i < 2 ? ("critical" as const) : ("warning" as const),
    url: `https://example.com/console/?view=alerts&alert=al-${i + 1}`,
  })),
];

export const homes: Record<
  "default" | "empty" | "alertsOnly" | "overflow" | "unlinked" | "noConnect" | "noGrants",
  HomeModel
> = {
  /** Inbox items and alerts together: a critical alert first, then what waits on Ada, then a warning. */
  default: home,
  /** Nothing waiting and no alert open: the section stays and says so. */
  empty: {
    ...home,
    starts: home.starts.map((s) => (s.id === "review" ? { id: s.id } : s)),
    notifications: {
      items: [],
      inbox: { total: 0, url: home.notifications.inbox?.url },
      alerts: { total: 0, url: home.notifications.alerts?.url },
    },
  },
  /** Only alerts are open; the inbox is clear. */
  alertsOnly: {
    ...home,
    starts: home.starts.map((s) => (s.id === "review" ? { id: s.id } : s)),
    notifications: {
      items: alertsOnly(home),
      inbox: { total: 0, url: home.notifications.inbox?.url },
      alerts: home.notifications.alerts,
    },
  },
  /** More than Home shows: the first eight, then how many more of each kind, linked to their pages. */
  overflow: {
    ...home,
    starts: home.starts.map((s) => (s.id === "review" ? { id: s.id, count: 14 } : s)),
    notifications: {
      items: many,
      inbox: { total: 14, url: home.notifications.inbox?.url },
      alerts: { total: 6, url: home.notifications.alerts?.url },
    },
  },
  /** A host with no public console address: Slack shows each item without a link, and Review stays for inbox items. */
  unlinked: {
    ...home,
    consoleUrl: undefined,
    notifications: {
      items: home.notifications.items.map(({ url: _url, ...n }) => n),
    },
  },
  /** A viewer who may not connect workspace accounts: no Connect, and an admin is named instead. */
  noConnect: {
    ...home,
    connections: home.connections.map((c) => ({ ...c, canConnect: false })),
  },
  /** Signed in, but no rule grants a role: no ask box, no alerts, and the way to get one. */
  noGrants: {
    ...home,
    ask: undefined,
    starts: [{ id: "talk" }, { id: "search" }],
    notifications: { items: [] },
    answeredBy: undefined,
    connections: home.connections.map((c) => ({ ...c, canConnect: false })),
    personal: { connected: 0, canConnect: false },
    access: {
      via: home.access.via,
      explainUrl: home.access.explainUrl,
      policyUrl: home.access.policyUrl,
    },
  },
};
