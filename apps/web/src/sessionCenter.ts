import type {
  AgentSessionSummary,
  Alert,
  ChangeAlertStateBody,
  ListAlertsResponse,
  ListSessionsResponse,
} from "../../../packages/client/src/generated/types.ts";
import { call, type ThreadSummary } from "./api.ts";
export type { AgentSessionSummary, Alert };
export const sessionCenter = {
  sessions: (filters: Record<string, string> = {}) =>
    call<ListSessionsResponse>(
      "GET",
      `/api/sessions?${new URLSearchParams({ limit: "50", ...filters })}`,
    ),
  alerts: (filters: Record<string, string> = {}) =>
    call<ListAlertsResponse>("GET", `/api/alerts?${new URLSearchParams(filters)}`).then(
      (r) => r.alerts,
    ),
  change: (id: string, body: ChangeAlertStateBody) =>
    call<Alert>("PATCH", `/api/alerts/${encodeURIComponent(id)}`, body),
};
export const isRootSession = (s: AgentSessionSummary) => !s.parent.startsWith("s_");
export const alertPriority = (a: Alert): "urgent" | "normal" | "low" => a.priority ?? "normal";
export const openAlert = (a: Alert) => a.state === "open" || (!a.state && !a.acked_at);
export const attentionAlerts = (alerts: Alert[]) =>
  alerts.filter((a) => !!a.session && openAlert(a) && alertPriority(a) !== "low");
export const priorityRank = (priority?: string) =>
  priority === "urgent" ? 0 : priority === "low" ? 2 : 1;
export const orderAlerts = (alerts: Alert[]) =>
  [...alerts].sort(
    (a, b) => priorityRank(a.priority) - priorityRank(b.priority) || b.at.localeCompare(a.at),
  );
export const sessionSource = (s: AgentSessionSummary) => {
  const origin = s.conversation.origin;
  return origin === "github"
    ? "GitHub"
    : origin === "slack"
      ? "Slack"
      : ["cron", "manual"].includes(origin)
        ? "Scheduled"
        : s.parent.startsWith("s_")
          ? "Agent"
          : s.parent.startsWith("console:")
            ? "Console"
            : "System";
};
export function sessionThread(s: AgentSessionSummary): ThreadSummary {
  return s.conversation;
}
