import { defaultCopy } from "@humagen/components/copy";
import type { Route } from "./route.ts";

export type WorkspaceHub = "library" | "agents" | "operations" | "notifications" | "customize";
export interface HubTab {
  label: string;
  route: Route;
  active: boolean;
}

/** Workspace pages use Settings navigation instead of a second hub tab strip. */
export function settingsPageLabel(route: Route): string | undefined {
  if (route.page === "directory" && route.tab === "people") return "People";
  if (route.page === "connections") return "Connections";
  if (route.page === "usage") return "Usage";
  if (route.page === "monitor" && route.tab === "health") return "Health";
  if (route.page === "action-policy") return "Policies";
  if (route.page === "audit") return "Audit";
  if (route.page === "agent-control") return "Agent controls";
  return undefined;
}

export function workspaceHub(route: Route): WorkspaceHub | null {
  if (settingsPageLabel(route)) return null;
  // Agents, and what they draw on (skills, knowledge, memory), share one place with tabs.
  if (route.page === "customize")
    return route.section === "skills" || route.section === "agents" ? "agents" : null;
  if (route.page === "notifications" || route.page === "inbox") return "notifications";
  if (route.page === "monitor" && route.tab === "rules") return "notifications";
  if (route.page === "library" && (route.section === "skills" || route.section === "memory"))
    return "agents";
  if (
    route.page === "knowledge" ||
    route.page === "corpus" ||
    route.page === "context" ||
    route.page === "knowledge-graph"
  )
    return "agents";
  if (
    ["agents", "agent", "agent-setup", "agent-network", "assignments", "agent-control"].includes(
      route.page,
    )
  )
    return "agents";
  if (["operations", "action-policy", "usage", "monitor", "audit"].includes(route.page))
    return "operations";
  return null;
}

/** Sidebar destinations and page tabs use the same permission predicates. */
export function hubTabs(
  hub: WorkspaceHub,
  route: Route,
  can: (permission: string) => boolean,
): HubTab[] {
  const nav = defaultCopy.navigation;
  if (hub === "customize") {
    return (
      [
        ["agents", nav.agents, can("agents:read")],
        ["skills", nav.skills, can("projects:read")],
        ["connections", nav.connections, can("runs:read")],
        ["tools", "MCPs", can("runs:read")],
      ] as const
    )
      .filter(([, , visible]) => visible)
      .map(([section, label]) => ({
        label,
        route: { page: "customize" as const, section },
        active: route.page === "customize" && route.section === section,
      }));
  }

  const rows: (HubTab & { visible: boolean })[] =
    hub === "notifications"
      ? []
      : hub === "library"
        ? [
            {
              label: nav.skills,
              route: { page: "library", section: "skills" },
              active: route.page === "library" && route.section === "skills",
              visible: can("projects:read"),
            },
            {
              label: nav.memory,
              route: { page: "library", section: "memory" },
              active: route.page === "library" && route.section === "memory",
              visible: can("runs:read") || can("projects:read") || can("agents:read"),
            },
            {
              label: "Saved files",
              route: { page: "library", section: "deliverables" },
              active:
                route.page === "artifacts" ||
                (route.page === "library" && route.section === "deliverables"),
              visible: can("artifacts:read"),
            },
          ]
        : hub === "agents"
          ? [
              {
                label: nav.agents,
                route: { page: "agents" },
                active: [
                  "agents",
                  "agent",
                  "agent-setup",
                  "agent-network",
                  "assignments",
                  "agent-control",
                ].includes(route.page),
                visible: can("agents:read"),
              },
              {
                label: nav.skills,
                route: { page: "library", section: "skills" },
                active:
                  (route.page === "library" && route.section === "skills") ||
                  (route.page === "customize" && route.section === "skills"),
                visible: can("projects:read"),
              },
              {
                label: "Knowledge",
                route: { page: "knowledge" },
                active:
                  route.page === "knowledge" ||
                  route.page === "corpus" ||
                  route.page === "context" ||
                  route.page === "knowledge-graph",
                visible: can("knowledge:read"),
              },
            ]
          : [
              {
                label: nav.context,
                route: { page: "operations", section: "context" },
                active: route.page === "operations" && route.section === "context",
                visible: can("knowledge:read"),
              },
              {
                label: nav.integrations,
                route: { page: "operations", section: "integrations" },
                active: route.page === "operations" && route.section === "integrations",
                visible: can("clients:read") || can("knowledge:read"),
              },
              {
                label: nav.policies,
                route: { page: "action-policy" },
                active: route.page === "action-policy",
                visible: can("agents:read"),
              },
              {
                label: nav.usage,
                route: { page: "usage" },
                active: route.page === "usage",
                visible: can("runs:read"),
              },
              {
                label: nav.health,
                route: { page: "monitor", tab: "health" },
                active: route.page === "monitor" && route.tab === "health",
                visible: can("alerts:read"),
              },
              {
                label: nav.audit,
                route: { page: "audit" },
                active: route.page === "audit",
                visible: can("projects:govern"),
              },
            ];
  return rows.filter((row) => row.visible).map(({ visible: _, ...tab }) => tab);
}
