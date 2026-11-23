import type { ThreadTurn } from "./api.ts";
import { type Alert, openAlert } from "./sessionCenter.ts";

/** The message or run an alert is about, if it names one. */
export const alertAnchor = (alert: Alert) => alert.message || alert.run_id;

/** A turn's key in the transcript, the one its row is drawn under. */
export const turnKey = (turn: ThreadTurn) =>
  turn.announcement?.id ?? turn.message?.id ?? turn.run?.id ?? "";

const turnAt = (turn: ThreadTurn) =>
  turn.message?.at ?? turn.run?.created_at ?? turn.announcement?.at ?? "";

/** A time as milliseconds: server times carry offsets and fractions, so text order is not time order. */
const ms = (at: string) => {
  const t = Date.parse(at);
  return Number.isNaN(t) ? 0 : t;
};

/** Alerts oldest first. */
export const byTime = (alerts: Alert[]) => [...alerts].sort((a, b) => ms(a.at) - ms(b.at));

/**
 * Alerts that say the same thing once: one kind, one agent, one answer, one
 * title, in one state. `alerts` is newest first, and `lead` is the newest.
 */
export interface AlertGroup {
  key: string;
  alerts: Alert[];
  lead: Alert;
  open: boolean;
}

/** Collapse duplicates, keeping the order in which each first happened. */
export function groupAlerts(alerts: Alert[]): AlertGroup[] {
  const groups = new Map<string, AlertGroup>();
  for (const alert of byTime(alerts)) {
    const open = openAlert(alert);
    const key = [
      open ? "open" : (alert.state ?? "resolved"),
      alert.review ? "reviewed" : "",
      alert.kind,
      alert.agent ?? "",
      alertAnchor(alert) ?? "",
      alert.title,
    ].join("\u0000");
    const group = groups.get(key);
    if (group) {
      group.alerts.unshift(alert);
      group.lead = alert;
    } else groups.set(key, { key, alerts: [alert], lead: alert, open });
  }
  return [...groups.values()];
}

/**
 * Where each alert sits in the transcript. One about a turn on view sits
 * right after it, and one about a run called from a turn on view (`callers`
 * maps a run to the run at the top of its call tree) sits after that turn. While earlier turns are unread, one about a turn not on view,
 * or older than every loaded turn, is listed as `earlier` (open only; a
 * settled one waits for its turn to load) rather than placed among turns it
 * is not about. Any other sits at its own time: after the last turn that
 * began before it, or before every turn when none did.
 */
export function placeAlerts(
  turns: ThreadTurn[],
  alerts: Alert[],
  unread = false,
  callers: Map<string, string> = new Map(),
): { lead: Alert[]; earlier: Alert[]; after: Map<string, Alert[]> } {
  const shown = new Set(turns.map((t) => t.message?.id ?? t.run?.id).filter(Boolean));
  const times = turns.map((t) => ms(turnAt(t)));
  const lead: Alert[] = [];
  const earlier: Alert[] = [];
  const after = new Map<string, Alert[]>();
  for (const alert of byTime(alerts)) {
    const caller = alert.run_id ? callers.get(alert.run_id) : undefined;
    let slot =
      [alert.message, alert.run_id].find((id) => !!id && shown.has(id)) ??
      (caller && shown.has(caller) ? caller : undefined);
    if (!slot && unread) {
      const at = ms(alert.at);
      if (alertAnchor(alert) || !turns.length || at < times[0]!) {
        if (openAlert(alert)) earlier.push(alert);
        continue;
      }
    }
    if (!slot) {
      const at = ms(alert.at);
      for (let i = turns.length - 1; i >= 0; i--)
        if (times[i]! <= at) {
          slot = turnKey(turns[i]!);
          break;
        }
    }
    if (slot) after.set(slot, [...(after.get(slot) ?? []), alert]);
    else lead.push(alert);
  }
  return { lead, earlier, after };
}

/** Whether an alert sits right after the answer it is about, so a link to that answer goes nowhere new. */
export const besideItsTurn = (alert: Alert, turns: ThreadTurn[]) =>
  turns.some((t) => {
    const id = t.message?.id ?? t.run?.id;
    return !!id && (id === alert.message || id === alert.run_id);
  });

/**
 * What an alert says once its agent is named beside it: "code-reviewer
 * needs review: format" is "Needs review: format".
 */
export function alertReason(alert: Pick<Alert, "title" | "agent">): string {
  const agent = alert.agent ?? "";
  const rest =
    agent && alert.title.startsWith(`${agent} `)
      ? alert.title.slice(agent.length + 1)
      : alert.title;
  return rest ? `${rest[0]!.toUpperCase()}${rest.slice(1)}` : alert.title;
}
