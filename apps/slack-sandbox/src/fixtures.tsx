import {
  ApprovalCard,
  AttachmentView,
  ClarificationCard,
  FailureAlert,
  NoticeView,
  SessionNotices,
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
  const events: TurnEvent[] = [{ type: "turn.started", turnId: turn.id, agent: turn.agent }];
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

/** Pick a scenario from what the user typed in Slack. */
export function scenarioFor(text: string): Turn {
  const t = text.toLowerCase();
  if (/(roll ?back|approve|deploy)/.test(t)) return fx.waitingTurn;
  if (/(fail|error|broken)/.test(t)) return fx.failedTurn;
  if (/(code|test|migration|terminal)/.test(t)) return fx.codingTurn;
  return fx.doneTurn;
}
