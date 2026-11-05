import { defaultCopy } from "@humagen/components/copy";
import type { Route } from "./route.ts";

export type WorkspaceHub = "library" | "agents" | "operations" | "notifications" | "customize";
export interface HubTab {
  label: string;
  route: Route;
  active: boolean;
}

export function workspaceHub(route: Route): WorkspaceHub | null {
  if (route.page === "customize") return "customize";
  if (route.page === "notifications" || route.page === "inbox") return "notifications";
  if (route.page === "monitor" && route.tab === "rules") return "notifications";
  if (route.page === "library" || route.page === "artifacts" || route.page === "context")
    return "library";
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
        ["tools", "Your tool servers", can("runs:read")],
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
              label: nav.sources,
              route: { page: "library", section: "sources" },
              active:
                route.page === "context" ||
                (route.page === "library" && route.section === "sources"),
              visible: can("knowledge:read"),
            },
            {
              label: defaultCopy.catalog.title,
              route: { page: "library", section: "catalog" },
              active: route.page === "library" && route.section === "catalog",
              visible: can("knowledge:read"),
            },
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
              label: nav.deliverables,
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
                active: ["agents", "agent", "agent-setup"].includes(route.page),
                visible: can("agents:read"),
              },
              {
                label: nav.network,
                route: { page: "agent-network" },
                active: route.page === "agent-network",
                visible: can("runs:read"),
              },
              {
                label: nav.assignments,
                route: { page: "assignments" },
                active: route.page === "assignments",
                visible: can("runs:read"),
              },
              {
                label: nav.controls,
                route: { page: "agent-control" },
                active: route.page === "agent-control",
                visible: can("agents:read"),
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
