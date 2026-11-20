import {
  CheckCircleIcon,
  ClipboardListIcon,
  ListChecksIcon,
  PauseIcon,
  SlidersIcon,
  UsersIcon,
} from "@humagen/render-web";
import type { Preferences } from "./api.ts";
import type { Route } from "./route.ts";

/*
 * What the sidebar's account row needs of settings: the person's picture and
 * where Workspace settings opens. They live apart from Settings.tsx so the
 * sidebar, which every page shows, does not load the settings pages.
 */

/** A person's picture, or their initial when they have none. */
export function Avatar({
  name,
  prefs,
  size = 28,
}: {
  name: string;
  prefs: Preferences | null;
  size?: number;
}) {
  if (prefs?.has_photo) {
    return (
      <img
        src={`/api/preferences/photo?v=${prefs.photo_version}`}
        alt=""
        width={size}
        height={size}
        className="shrink-0 rounded-full object-cover"
        style={{ width: size, height: size }}
      />
    );
  }
  return (
    <span
      aria-hidden
      className="inline-flex shrink-0 items-center justify-center rounded-full bg-aui-hover font-medium text-aui-fg"
      style={{
        width: size,
        height: size,
        fontSize: Math.max(12, Math.round(size * 0.42)),
        lineHeight: 1,
      }}
    >
      {(name.trim()[0] ?? "?").toUpperCase()}
    </span>
  );
}

// Workspace pages shown inside workspace settings. Schedules and Projects live in the sidebar.
export const linked = [
  {
    label: "People",
    icon: <UsersIcon size={16} />,
    route: { page: "directory", tab: "people" } as Route,
    permission: "people:read",
  },
  {
    label: "Usage",
    icon: <ClipboardListIcon size={16} />,
    route: { page: "usage" } as Route,
    permission: "runs:read",
  },
  {
    label: "Health",
    icon: <CheckCircleIcon size={16} />,
    route: { page: "monitor", tab: "health" } as Route,
    permission: "alerts:read",
  },
  {
    label: "Policies",
    icon: <SlidersIcon size={16} />,
    route: { page: "action-policy" } as Route,
    permission: "agents:read",
  },
  {
    label: "Audit",
    icon: <ListChecksIcon size={16} />,
    route: { page: "audit" } as Route,
    permission: "projects:govern",
  },
  {
    label: "Agent controls",
    icon: <PauseIcon size={16} />,
    route: { page: "agent-control" } as Route,
    permission: "agents:write",
  },
];

/**
 * Where Workspace settings opens for someone: its first section for an admin,
 * else the first workspace page they may read, else nowhere.
 */
export function workspaceSettingsRoute(
  can: (permission: string) => boolean,
  roles: string[] = [],
): Route | null {
  if (roles.includes("admin") || can("agents:write") || can("projects:govern"))
    return { page: "settings", section: "workspace" } as Route;
  return linked.find((item) => can(item.permission))?.route ?? null;
}
