import type { Approval, Clarification, Failure, Tone, Turn } from "./types";
import { isRetryable } from "./types";

/**
 * Everything waiting on a person, across conversations: approvals, questions,
 * failed runs, and system alerts. The web renders it as an inbox panel; Slack
 * publishes it as the App Home tab. Items resolve in place, so one source of
 * truth updates every surface.
 */

export type ServiceStatus = "operational" | "degraded" | "down" | "maintenance" | "resolved";

/** An incident in a dependency the agent relies on. Updated in place as it develops. */
export interface SystemAlert {
  id: string;
  service: string;
  status: ServiceStatus;
  /** Point first: "Model provider responses are slow". */
  title: string;
  /** What users will notice and what to do. */
  body?: string;
  startedAt: string;
  updatedAt: string;
  url?: string;
  /** Affected capabilities, in plain words: "Streaming", "File uploads". */
  affects?: string[];
}

export type InboxItemKind = "approval" | "clarification" | "failure" | "alert";
export type InboxItemState = "open" | "resolved" | "dismissed" | "snoozed";

interface Base {
  id: string;
  state: InboxItemState;
  /** Where it came from, for "Open conversation". */
  source: { label: string; turnId?: string; url?: string };
  at: string;
  snoozedUntil?: string;
}

export type InboxItem =
  | (Base & { kind: "approval"; approval: Approval })
  | (Base & { kind: "clarification"; clarification: Clarification })
  | (Base & { kind: "failure"; failure: Failure })
  | (Base & { kind: "alert"; alert: SystemAlert });

export const SERVICE_TONE: Record<ServiceStatus, Tone> = {
  operational: "ok",
  degraded: "warn",
  down: "bad",
  maintenance: "info",
  resolved: "ok",
};

/** Higher sorts first: outages, then decisions people are blocked on, then the rest. */
export function priority(item: InboxItem): number {
  if (item.state !== "open") return 0;
  switch (item.kind) {
    case "alert":
      return item.alert.status === "down" ? 50 : item.alert.status === "degraded" ? 30 : 10;
    case "approval":
      return item.approval.destructive ? 45 : 40;
    case "clarification":
      return 35;
    case "failure":
      return isRetryable(item.failure) ? 20 : 25;
  }
}

/** Open items first by priority, newest first within a priority. */
export function sortInbox(items: InboxItem[]): InboxItem[] {
  return items.slice().sort((a, b) => priority(b) - priority(a) || b.at.localeCompare(a.at));
}

/** Collect what's waiting on a person from a set of turns. */
export function inboxFromTurns(turns: { turn: Turn; label: string; url?: string }[]): InboxItem[] {
  const items: InboxItem[] = [];
  for (const { turn, label, url } of turns) {
    const source = { label, turnId: turn.id, url };
    for (const i of turn.interrupts) {
      if (i.type === "approval") {
        items.push({
          id: `${turn.id}:${i.id}`,
          kind: "approval",
          approval: i,
          state: i.decision.kind === "pending" ? "open" : "resolved",
          source,
          at:
            i.decision.kind === "pending"
              ? (i.expiresAt ?? "")
              : "at" in i.decision
                ? i.decision.at
                : "",
        });
      } else {
        items.push({
          id: `${turn.id}:${i.id}`,
          kind: "clarification",
          clarification: i,
          state: i.answer.kind === "pending" ? "open" : "resolved",
          source,
          at: "at" in i.answer ? i.answer.at : "",
        });
      }
    }
    if (turn.status === "failed" && turn.failure) {
      items.push({
        id: `${turn.id}:failure`,
        kind: "failure",
        failure: turn.failure,
        state: "open",
        source,
        at: "",
      });
    }
  }
  return sortInbox(items);
}

export function openCount(items: InboxItem[]): number {
  return items.filter((i) => i.state === "open").length;
}
