import { defaultCopy } from "@humagen/components/copy";
import type {
  AgentSessionSummary,
  Alert,
  ChangeAlertStateBody,
  ListAlertsResponse,
  ListSessionsResponse,
} from "../../../packages/client/src/generated/types.ts";
import { call, type ThreadSummary } from "./api.ts";
import { parse, type Route } from "./route.ts";
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

/**
 * The console page an alert's action names, such as `/knowledge/hr-policies`
 * from knowledge maintenance, or nothing when its action is a word (retry,
 * answer) or names no page.
 */
export function alertPage(a: Alert): Route | undefined {
  const action = a.action ?? "";
  if (!action.startsWith("/") || action.startsWith("//")) return undefined;
  const url = new URL(action, "http://localhost");
  const route = parse(url.pathname, url.search);
  return route.page === "not-found" ? undefined : route;
}

/** Where opening an alert goes: its session, or the page its action names when it has no session. */
export function alertDestination(a: Alert): Route | undefined {
  if (a.session) return { page: "conversation", id: a.session, message: a.message };
  return alertPage(a);
}

/** What an alert's action button says: the page it opens, or the action's own word. */
export function alertActionLabel(a: Alert): string {
  const page = alertPage(a);
  const words = defaultCopy.corpora;
  if (!page) return a.action || "Open";
  if (page.page === "corpus" && page.tab === "graph") return words.openGraph;
  if (page.page === "corpus" && page.tab === "types") return words.openTypes;
  if (page.page === "corpus" || page.page === "knowledge" || page.page === "context")
    return words.openCorpus;
  return "Open";
}
