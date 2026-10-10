import {
  ApprovalCard,
  AttachmentView,
  ClarificationCard,
  FailureAlert,
  InboxView,
  MailboxView,
  MessageView,
  NoticeView,
  SessionNotices,
  SystemAlertView,
  TurnView,
} from "@agent-ui/components";
import type { Turn, TurnEvent } from "@agent-ui/core";
import * as fx from "@agent-ui/core/fixtures";
import type { Placement } from "@agent-ui/render-slack";
import type { ReactElement } from "react";

/**
 * Every fixture in @agent-ui/core/fixtures, as something postable. Names match
 * the fixture exports (and Storybook docs): `pnpm sandbox post waitingTurn`.
 */
export interface Postable {
  name: string;
  element: ReactElement;
  placement?: Placement;
}

const isTurn = (v: unknown): v is Turn =>
  typeof v === "object" && v !== null && "tasks" in v && "agent" in v;

export function registry(): Postable[] {
  const out: Postable[] = [];
  for (const [name, value] of Object.entries(fx)) {
    if (isTurn(value)) {
      out.push({ name, element: <TurnView turn={value} /> });
    } else if (typeof value === "object" && value !== null && "type" in value) {
      const v = value as { type: string };
      if (v.type === "approval") {
        out.push({ name, element: <ApprovalCard turnId="sandbox" approval={value as never} /> });
      } else if (v.type === "clarification") {
        out.push({
          name,
          element: <ClarificationCard turnId="sandbox" clarification={value as never} />,
        });
      } else if (v.type === "command" || v.type === "code") {
        out.push({ name, element: <AttachmentView attachment={value as never} /> });
      }
    }
  }
  for (const [key, notice] of Object.entries(fx.notices)) {
    out.push({ name: `notices.${key}`, element: <NoticeView notice={notice} /> });
  }
  for (const [key, notice] of Object.entries(fx.runtimeNotices)) {
    out.push({ name: `runtimeNotices.${key}`, element: <NoticeView notice={notice} /> });
  }
  for (const [key, artifact] of Object.entries(fx.artifacts)) {
    out.push({ name: `artifacts.${key}`, element: <AttachmentView attachment={artifact} /> });
  }
  for (const [key, session] of Object.entries(fx.sessions)) {
    out.push({ name: `sessions.${key}`, element: <SessionNotices session={session} /> });
  }
  for (const [key, alert] of Object.entries(fx.systemAlerts)) {
    out.push({ name: `systemAlerts.${key}`, element: <SystemAlertView alert={alert} /> });
  }
  out.push({ name: "inbox@home", element: <InboxView items={fx.inboxItems} />, placement: "home" });
  out.push({
    name: "mailbox@home",
    element: <MailboxView messages={fx.mailMessages} />,
    placement: "home",
  });
  // One message per way of resolving, as it would arrive in a DM.
  for (const message of fx.mailMessages) {
    out.push({ name: `mail.${message.id}`, element: <MessageView message={message} /> });
  }
  for (const [key, failure] of Object.entries(fx.failures)) {
    out.push({
      name: `failures.${key}`,
      element: <FailureAlert turnId="sandbox" failure={failure} />,
    });
  }
  out.push({
    name: "failures.rate_limited@modal",
    element: <FailureAlert turnId="sandbox" failure={fx.failures.rate_limited} />,
    placement: "modal",
  });
  return out;
}

/**
 * Replay a finished Turn as the event stream that would have produced it:
 * each task starts then settles, citations arrive, then tokens. Used to drive
 * real chat.appendStream calls from static fixtures.
 */
export function eventsFor(turn: Turn): TurnEvent[] {
  const events: TurnEvent[] = [
    {
      type: "turn.started",
      turnId: turn.id,
      agent: turn.agent,
      sessionUrl: turn.sessionUrl,
      source: turn.source,
      disclaimer: turn.disclaimer,
    },
  ];
  for (const task of turn.tasks) {
    if (turn.planTitle) events.push({ type: "plan.updated", title: presentTense(task.title) });
    events.push({
      type: "task.upserted",
      task: {
        id: task.id,
        title: presentTense(task.title),
        status: "in_progress",
        details: task.details,
      },
    });
    events.push({ type: "task.upserted", task });
  }
  for (const citation of turn.citations) events.push({ type: "citation.added", citation });
  for (const word of turn.text.match(/\S+\s*|\s+/g) ?? []) {
    events.push({ type: "text.delta", text: word });
  }
  for (const attachment of turn.attachments ?? []) {
    events.push({ type: "attachment.upserted", attachment });
  }
  for (const notice of turn.notices) events.push({ type: "notice", notice });
  for (const interrupt of turn.interrupts) events.push({ type: "interrupt.raised", interrupt });
  if (turn.status === "failed" && turn.failure) {
    events.push({ type: "turn.failed", failure: turn.failure });
  } else if (turn.status === "done") {
    events.push({ type: "turn.completed" });
  }
  return events;
}

/** "Searched runbooks" → "Searching runbooks", for the in-progress state. */
function presentTense(title: string): string {
  // A failed task's in-progress title is the attempt: "Couldn't check X" was "Checking X".
  const words = title.replace(/^Couldn't (\w)/, (_m, c: string) => c.toUpperCase()).split(" ");
  const [verb = "", ...rest] = words;
  const irregular: Record<string, string> = { Read: "Reading", Ran: "Running", Check: "Checking" };
  const ing = irregular[verb] ?? (verb.endsWith("ed") ? `${verb.slice(0, -2)}ing` : verb);
  return [ing, ...rest].join(" ");
}

/**
 * What you can say to the sandbox bot, most specific first. One table drives
 * the matcher, the help reply and the suggested prompts, so they can't drift.
 */
export const SCENARIOS: { match: RegExp; say: string; shows: string; turn: Turn }[] = [
  // Human in the loop
  { match: /skill/, say: "add a skill", shows: "approve a new skill", turn: fx.skillApprovalTurn },
  {
    match: /(prompt|instruction)/,
    say: "update your prompt",
    shows: "approve an edit to the agent's instructions",
    turn: fx.promptApprovalTurn,
  },
  {
    match: /(which|clarif|environment)/,
    say: "which environment",
    shows: "answer a question the agent is stuck on",
    turn: fx.clarificationTurn,
  },
  {
    match: /(batch|both)/,
    say: "do both",
    shows: "two approvals in one turn",
    turn: fx.batchTurn,
  },
  {
    match: /(roll ?back|approve|deploy)/,
    say: "roll back the last deploy",
    shows: "approve a destructive action",
    turn: fx.waitingTurn,
  },
  // Files
  {
    match: /(email|pii|customer)/,
    say: "export with customer emails",
    shows: "approve a file before it exists",
    turn: fx.artifactApprovalTurn,
  },
  {
    match: /(revise|version|update the export)/,
    say: "revise the export",
    shows: "a file that replaces an earlier one",
    turn: fx.artifactRevisedTurn,
  },
  {
    match: /(export failed|partial)/,
    say: "partial export",
    shows: "one file ready, one failed",
    turn: fx.artifactFailedTurn,
  },
  {
    match: /(export|file|artifact)/,
    say: "export the refunds",
    shows: "finished files",
    turn: fx.artifactTurn,
  },
  // References
  {
    match: /(source|reference|cite)/,
    say: "show your sources",
    shows: "every kind of reference",
    turn: fx.referencesTurn,
  },
  {
    match: /(long|every|report|quarter)/,
    say: "long report",
    shows: "an answer that continues across messages",
    turn: fx.longTurn,
  },
  // Alerts and failures
  {
    match: /(reconnect|auth|sign)/,
    say: "reconnect",
    shows: "a failure you can fix yourself",
    turn: fx.reconnectTurn,
  },
  {
    match: /(rate|limit|busy)/,
    say: "rate limit",
    shows: "a failure that clears on its own",
    turn: fx.rateLimitedTurn,
  },
  {
    match: /(notice|fallback|slow)/,
    say: "runtime notices",
    shows: "what the runtime did on its own",
    turn: fx.noticesTurn,
  },
  { match: /(fail|error|broken)/, say: "fail", shows: "a failed run", turn: fx.failedTurn },
  {
    match: /(code|test|migration|terminal)/,
    say: "run the tests",
    shows: "a coding turn",
    turn: fx.codingTurn,
  },
];

/** Pick a scenario from what the user typed in Slack. */
export function scenarioFor(text: string): Turn {
  const t = text.toLowerCase();
  return SCENARIOS.find((s) => s.match.test(t))?.turn ?? fx.doneTurn;
}
