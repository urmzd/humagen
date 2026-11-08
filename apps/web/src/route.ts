import { defaultCopy } from "@humagen/components/copy";
import { useCallback, useEffect, useRef, useState } from "react";
import { swap } from "./transition.ts";

/**
 * Where the console is, as an address. Every page, tab and selection a person
 * can reach has one, so a reload, a copied link and the back button all land in
 * the same place.
 *
 * Pages are paths; a selection inside a page (an alert, an inbox message) is a
 * query parameter. Old `?view=` links still open the page they named.
 */

export type AgentTab =
  | "prompt"
  | "model"
  | "access"
  | "queue"
  | "context"
  | "memory"
  | "evals"
  | "alerts"
  | "revisions";
export const AGENT_TABS: AgentTab[] = [
  "prompt",
  "model",
  "access",
  "queue",
  "context",
  "memory",
  "evals",
  "alerts",
  "revisions",
];

export type SourceTab = "documents" | "search" | "variants" | "evals";
export const SOURCE_TABS: SourceTab[] = ["documents", "search", "variants", "evals"];

/** Monitor: how runs are going (overview), the rules that watch them, and the health of what the deployment runs on. What a rule fires lands in the inbox. */
export type MonitorTab = "overview" | "rules" | "health";
export const MONITOR_TABS: MonitorTab[] = ["overview", "rules", "health"];

/** The Directory: who and what is in the workspace, for everyone in it. */
export type DirectoryTab = "people" | "agents" | "projects";
export const DIRECTORY_TABS: DirectoryTab[] = ["people", "agents", "projects"];

/**
 * What a project is for, as the Projects page narrows to it with `?kind=`:
 * one of humagen's kinds, or one the workspace defines, which has the same
 * shape. A value of any other shape falls back to every kind.
 */
export type ProjectKindFilter = string;
const KIND_SHAPE = /^[a-z][a-z0-9-]{0,31}$/;
/** A kind as an address names it, or nothing when it is not shaped as one. */
export const kindParam = (value: string | null | undefined): ProjectKindFilter | undefined =>
  value && KIND_SHAPE.test(value) ? value : undefined;

/** How the Projects page orders its cards besides last activity, the default, which the address leaves out. */
export type ProjectSort = "name" | "created";
export const PROJECT_SORTS: ProjectSort[] = ["name", "created"];

export type SettingsSection =
  | "notifications"
  | "profile"
  | "appearance"
  | "workspace"
  | "routing"
  | "models"
  | "limits"
  | "connections"
  | "access"
  | "read-audit"
  | "deletions";
export const SETTINGS_SECTIONS: SettingsSection[] = [
  "notifications",
  "profile",
  "appearance",
  "workspace",
  "routing",
  "models",
  "limits",
  "connections",
  "access",
  "read-audit",
  "deletions",
];

export type ConversationPanel =
  | "overview"
  | "activity"
  | "context"
  | "people"
  | "deliverables"
  | "workspace";
const CONVERSATION_PANELS: ConversationPanel[] = [
  "overview",
  "activity",
  "context",
  "people",
  "deliverables",
  "workspace",
];
/** The Workspace panel's sections. Files is the default, at the panel's own address. */
export type WorkspaceSection = "sandboxes" | "files" | "scratchpads" | "memories" | "artifacts";
export const WORKSPACE_SECTIONS: WorkspaceSection[] = [
  "sandboxes",
  "files",
  "scratchpads",
  "memories",
  "artifacts",
];
export type ArtifactTab = "preview" | "versions" | "provenance";
const ARTIFACT_TABS: ArtifactTab[] = ["preview", "versions", "provenance"];
export interface ConversationView {
  contextTab?: "sources" | "instructions";
  panel?: ConversationPanel;
  filter?: "all" | "handoffs" | "tools" | "decisions";
  showTools?: boolean;
  run?: string;
  step?: string;
  artifact?: string;
  artifactTab?: ArtifactTab;
  /** The Workspace section; absent is Files. */
  workspaceSection?: WorkspaceSection;
  /** The run whose nested sandbox is open in Sandboxes. */
  sandbox?: string;
  /** The scratchpad open in Scratchpads. */
  scratchpad?: string;
  /** The run whose memory is open in Memories. */
  memoryRun?: string;
  /** The Workspace pane's open tabs (pane.ts), oldest first, and the active one. */
  tabs?: string[];
  active?: string;
  /** The artifact version the selected one is compared with. */
  compare?: string;
  /** The pane fills the page, or shows two tabs side by side. */
  expanded?: boolean;
  split?: boolean;
  file?: string;
  workspaceTab?: "edit" | "preview" | "changes";
  /** One-based slide selection. */
  slide?: number;
}

export type LibrarySection = "sources" | "catalog" | "skills" | "memory" | "deliverables";
/** The reader's own views of their notifications, besides the Inbox, which is the default. */
export const NOTIFICATION_VIEWS = ["unread", "hidden", "done"] as const;
export type NotificationView = (typeof NOTIFICATION_VIEWS)[number];

export type YouSection = "profile" | "notifications" | "privacy" | "agents" | "connections";
/** Where a conversation came from, as History filters it. */
export type HistorySource = "all" | "chat" | "github" | "slack" | "scheduled" | "assignments";
/** What History keeps by what a conversation is doing. */
export type HistoryStatus = "needs" | "running" | "failed";
export type CustomizeSection = "agents" | "skills" | "connections" | "tools";
export type SessionStatus = "working" | "waiting" | "done" | "failed" | "stopped";
export type Route =
  | { page: "not-found"; path: string }
  | { page: "sessions"; status?: SessionStatus; agent?: string; project?: string; q?: string }
  | {
      page: "alerts";
      tab?: "watching";
      state?: "open" | "snoozed" | "resolved";
      priority?: "urgent" | "normal" | "low";
      mine?: boolean;
      source?: string;
      alert?: string;
    }
  | {
      page: "history";
      q?: string;
      source?: HistorySource;
      status?: HistoryStatus;
    }
  | {
      page: "customize";
      section: CustomizeSection;
      provider?: string;
      scope?: "personal" | "workspace" | "project" | "agent";
      project?: string;
      skill?: string;
      version?: number;
      file?: string;
      libraryTab?: "overview" | "contents" | "versions";
    }
  | {
      page: "library";
      section: LibrarySection;
      scope?: "personal" | "workspace" | "project" | "agent";
      project?: string;
      owner?: string;
      skill?: string;
      version?: number;
      artifact?: string;
      file?: string;
      catalogQuery?: string;
      libraryTab?: "overview" | "contents" | "versions";
    }
  | {
      page: "operations";
      section: "context" | "integrations";
      selected?: string;
      tab?: "connect" | "resources" | "links" | "access" | "events" | "actions";
    }
  | { page: "assignments"; id?: string; agent?: string }
  | { page: "agent-network"; handoff?: string }
  | { page: "agent-control"; agent?: string }
  | { page: "action-policy"; scope?: "workspace" | "agent" | "project" | "personal"; name?: string }
  | {
      page: "notifications";
      message?: string;
      alert?: string;
      kind?: string;
      /** The reader's view; the Inbox, the default, is left out of the address. */
      view?: NotificationView;
    }
  | { page: "audit" }
  | { page: "usage"; days?: 7 | 14 | 30; day?: string; display?: "chart" | "table" }
  | { page: "you"; section: YouSection; provider?: string }
  | { page: "agent-setup"; name?: string }
  /** A new conversation, optionally with an agent picked and a first message written for it. */
  | { page: "new"; project?: string; channel?: string; agent?: string; ask?: string }
  /**
   * A conversation, at one of its people's messages when `message` is set,
   * or with its Share dialog open (`/conversations/{id}/share`) when
   * `share` is. Links from when they were called tasks, `/tasks/{id}`,
   * open the same conversation.
   */
  | ({
      page: "conversation";
      id: string;
      message?: string;
      share?: boolean;
      fork?: string;
      bringBack?: string;
    } & ConversationView)
  /** Every project, searched with `q`, narrowed to one kind and ordered by `sort`, last activity when absent. */
  | { page: "projects"; q?: string; kind?: ProjectKindFilter; sort?: ProjectSort }
  /**
   * One project: a page of its own, with no tabs. The workspace itself is the
   * project named "", written `~`. `fork` opens the Fork Project sheet over it.
   */
  | { page: "project"; name: string; fork?: boolean }
  /** One of a project's channels, with its shared threads. */
  | { page: "channel"; project: string; channel: string }
  | { page: "schedules"; name?: string }
  | { page: "automations" }
  | { page: "monitor"; tab: MonitorTab }
  /** The inbox, at a message, at the message an alert raised, or narrowed to one kind. */
  | { page: "inbox"; message?: string; alert?: string; kind?: string; view?: NotificationView }
  | { page: "context"; source?: string; tab?: SourceTab }
  | { page: "agents"; state?: string }
  | { page: "agent"; name: string | null; tab: AgentTab }
  | { page: "artifacts"; artifact?: string }
  /** Connections: apps and MCPs. `provider` opens one in the side sheet. */
  | { page: "connections"; provider?: string }
  | { page: "directory"; tab: DirectoryTab }
  | { page: "settings"; section: SettingsSection; permission?: string };

export type Page = Route["page"];

const enc = encodeURIComponent;
function dec(s: string): string {
  try {
    return decodeURIComponent(s);
  } catch {
    return s;
  }
}
const oneOf = <T extends string>(
  value: string | null | undefined,
  allowed: readonly T[],
): T | undefined =>
  value && (allowed as readonly string[]).includes(value) ? (value as T) : undefined;
const pick = <T extends string>(value: string | undefined, allowed: readonly T[], fallback: T): T =>
  oneOf(value, allowed) ?? fallback;

const PAGE_TITLES: Record<Page, string> = {
  "not-found": "Page not found",
  sessions: "Sessions",
  alerts: "Alerts",
  history: defaultCopy.history.title,
  customize: "Customize",
  library: "Library",
  operations: "Operations",
  assignments: "Assignments",
  "agent-network": "Agent Network",
  "agent-control": "Agent Controls",
  "action-policy": "Action Policies",
  audit: "Audit",
  usage: "Usage",
  you: "You",
  "agent-setup": "New Agent",
  notifications: "Notifications",
  new: "New",
  conversation: "Session",
  projects: "Projects",
  project: "Project",
  channel: "Channel",
  schedules: "Schedules",
  automations: "Schedules",
  monitor: "Monitor",
  inbox: "Notifications",
  context: "Context",
  agents: "Agents",
  agent: "Agent",
  artifacts: "Artifacts",
  connections: "Connections",
  directory: "Directory",
  settings: "Settings",
};

/**
 * The pages titled as the sidebar names them, so a title follows the
 * workspace's own words for them (branding.copy).
 */
const NAVIGATION_TITLES: Partial<Record<Page, keyof typeof defaultCopy.navigation>> = {
  library: "library",
  operations: "operations",
  assignments: "assignments",
  "agent-network": "network",
  "agent-control": "controls",
  "action-policy": "policies",
  audit: "audit",
  usage: "usage",
  you: "you",
  notifications: "notifications",
  inbox: "notifications",
  projects: "projects",
  schedules: "schedules",
  agents: "agents",
};

function pageTitle(page: Page): string {
  const key = NAVIGATION_TITLES[page];
  const named = key ? defaultCopy.navigation[key] : undefined;
  return typeof named === "string" ? named : PAGE_TITLES[page];
}

/** The browser tab's title for a route: the place first, then the product, with the unread count in front. */
export function documentTitle(r: Route, unread = 0, product = "Humagen"): string {
  let place = pageTitle(r.page);
  if (r.page === "agent" && r.name) place = r.name;
  else if (r.page === "project") place = r.name === "" ? "Workspace" : r.name;
  else if (r.page === "channel") place = `#${r.channel}`;
  return `${unread > 0 ? `(${unread}) ` : ""}${place} - ${product}`;
}

/** The address of a route, path and query. */
export function href(r: Route): string {
  switch (r.page) {
    case "not-found":
      return r.path;
    case "sessions": {
      const q = new URLSearchParams();
      for (const key of ["q", "status", "agent", "project"] as const)
        if (r[key]) q.set(key, r[key]);
      return `/sessions${q.size ? `?${q}` : ""}`;
    }
    case "alerts": {
      const q = new URLSearchParams();
      for (const key of ["state", "priority", "source", "alert"] as const)
        if (r[key]) q.set(key, r[key]);
      if (r.mine === false) q.set("mine", "false");
      return `/alerts${r.tab === "watching" ? "/watching" : ""}${q.size ? `?${q}` : ""}`;
    }
    case "history":
      return href({
        page: "sessions",
        q: r.q,
        status:
          r.status === "needs"
            ? "waiting"
            : r.status === "running"
              ? "working"
              : r.status === "failed"
                ? "failed"
                : undefined,
      });
    case "library": {
      if (r.section === "sources") return "/connections";
      if (r.section === "catalog")
        return `/connections/catalog${r.catalogQuery ? `?q=${enc(r.catalogQuery)}` : ""}`;
      const q = new URLSearchParams();
      if (r.scope) q.set("scope", r.scope);
      if (r.project !== undefined) q.set("project", r.project);
      if (r.owner !== undefined) q.set("owner", r.owner);
      if (r.skill) q.set("skill", r.skill);
      if (r.version && Number.isSafeInteger(r.version) && r.version > 0)
        q.set("version", String(r.version));
      if (r.artifact) q.set("artifact", r.artifact);
      if (r.file) q.set("file", r.file);
      if (r.catalogQuery) q.set("q", r.catalogQuery);
      if (r.libraryTab) q.set("tab", r.libraryTab);
      return `/library/${r.section}${q.size ? `?${q}` : ""}`;
    }
    case "operations":
      return "/connections";
    case "assignments":
      return `${r.id ? `/assignments/${enc(r.id)}` : "/assignments"}${r.agent ? `?agent=${enc(r.agent)}` : ""}`;
    case "agent-network":
      return r.handoff ? `/agent-network/handoffs/${enc(r.handoff)}` : "/agent-network";
    case "agent-control":
      return r.agent ? `/agents/${enc(r.agent)}/control` : "/agent-controls";
    case "agent-setup":
      return r.name ? `/agents/${enc(r.name)}/setup` : "/agents/new/setup";
    case "action-policy":
      return r.scope &&
        r.scope !== "workspace" &&
        !(r.scope === "project" && (!r.name || r.name === "~"))
        ? `/operations/policies/${r.scope}${r.name ? `/${enc(r.name)}` : ""}`
        : "/operations/policies";
    case "audit":
      return "/settings/audit";
    case "usage": {
      const q = new URLSearchParams();
      if (r.days && r.days !== 14) q.set("days", String(r.days));
      if (r.day) q.set("day", r.day);
      if (r.display === "table") q.set("display", "table");
      return `/settings/usage${q.size ? `?${q}` : ""}`;
    }
    case "you":
      return r.section === "connections"
        ? href({ page: "connections", provider: r.provider })
        : r.section === "notifications"
          ? "/settings/notifications"
          : r.section === "agents"
            ? "/agents"
            : r.section === "privacy"
              ? "/settings/access"
              : "/settings/profile";
    case "new": {
      const q = new URLSearchParams();
      if (r.project !== undefined) q.set("project", r.project === "" ? "~" : r.project);
      if (r.channel) q.set("channel", r.channel);
      if (r.agent) q.set("agent", r.agent);
      if (r.ask) q.set("ask", r.ask);
      const query = q.toString();
      return query ? `/?${query}` : "/";
    }
    case "conversation": {
      const q = new URLSearchParams();
      const panels = {
        workspace: "files",
        context: "sources",
        activity: "steps",
        overview: "sessions",
        people: "people",
        deliverables: "files",
      };
      if (r.panel) q.set("panel", panels[r.panel]);
      if (r.panel === "deliverables") q.set("legacy_panel", "deliverables");
      for (const key of [
        "message",
        "fork",
        "bringBack",
        "contextTab",
        "filter",
        "run",
        "step",
        "artifact",
        "artifactTab",
        "workspaceSection",
        "sandbox",
        "scratchpad",
        "memoryRun",
        "active",
        "compare",
        "file",
        "workspaceTab",
      ] as const)
        if (r[key]) q.set(key, r[key]);
      if (r.share) q.set("share", "1");
      if (r.showTools === false) q.set("tools", "0");
      if (r.tabs?.length) q.set("tabs", r.tabs.map(encodeURIComponent).join(","));
      if (r.expanded) q.set("full", "1");
      if (r.split) q.set("split", "1");
      if (r.slide) q.set("slide", String(r.slide));
      return `/sessions/${enc(r.id)}${q.size ? `?${q}` : ""}`;
    }
    case "projects": {
      const q = new URLSearchParams();
      if (r.q) q.set("q", r.q);
      if (r.kind) q.set("kind", r.kind);
      if (r.sort) q.set("sort", r.sort);
      const query = q.toString();
      return query ? `/projects?${query}` : "/projects";
    }
    case "project": {
      // Names never hold "/", so nesting lives in the tree, never in the path.
      const base = `/projects/${r.name === "" ? "~" : enc(r.name)}`;
      return r.fork ? `${base}/fork` : base;
    }
    case "channel":
      return href({ page: "sessions", project: r.project });
    case "automations":
      return "/scheduled";
    case "schedules":
      return r.name ? `/schedules/${enc(r.name)}` : "/schedules";
    case "monitor":
      return r.tab === "rules"
        ? "/alerts/watching"
        : r.tab === "health"
          ? "/settings/health"
          : "/sessions";
    case "notifications":
    case "inbox":
      return href({ page: "alerts", alert: r.alert });
    case "context":
      if (!r.source) return "/connections";
      return !r.tab || r.tab === "documents"
        ? `/connections/sources/${enc(r.source)}`
        : `/connections/sources/${enc(r.source)}/${r.tab}`;
    case "agents":
      return r.state ? `/agents?state=${enc(r.state)}` : "/agents";
    case "agent": {
      const base = r.name === null ? "/agents/new" : `/agents/${enc(r.name)}`;
      return r.tab === "prompt" ? base : `${base}/${r.tab}`;
    }
    case "artifacts":
      return r.artifact ? `/artifacts/${enc(r.artifact)}` : "/artifacts";
    case "customize":
      if (r.section === "skills") return href({ ...r, page: "library", section: "skills" });
      if (r.section === "agents") return "/agents";
      return href({ page: "connections", provider: r.section === "tools" ? "other" : r.provider });
    case "connections":
      return r.provider ? `/settings/connections/${enc(r.provider)}` : "/settings/connections";
    case "directory":
      return r.tab === "agents"
        ? "/agents"
        : r.tab === "projects"
          ? "/projects"
          : "/settings/people";
    case "settings": {
      if (r.section === "connections") return "/settings/connections";
      const base = `/settings/${r.section}`;
      return r.section === "access" && r.permission
        ? `${base}?permission=${enc(r.permission)}`
        : base;
    }
  }
}

function inboxAt(q: URLSearchParams): Route {
  return {
    page: "inbox",
    message: q.get("message") ?? undefined,
    alert: q.get("alert") ?? undefined,
    kind: q.get("kind") ?? undefined,
    view: oneOf(q.get("view"), NOTIFICATION_VIEWS),
  };
}

/** Links from before pages had paths: `/?view=agents`, `/?view=alerts&alert=…`. */
function legacy(q: URLSearchParams): Route | null {
  const v = q.get("view");
  if (!v) return q.has("message") ? inboxAt(q) : null;
  switch (v) {
    case "home":
    case "chat":
    case "runs":
      return { page: "new" };
    case "inbox":
      return { page: "alerts" };
    case "projects":
    case "schedules":
    case "agents":
      return { page: v };
    case "knowledge":
    case "context":
      return { page: "context" };
    case "storage":
      return { page: "artifacts" };
    case "evals":
      return { page: "agents" };
    case "analytics":
    case "monitor":
      return { page: "monitor", tab: "overview" };
    // What fired belongs in the inbox; an alert's link opens the message it raised.
    case "alerts":
      return {
        page: "alerts",
        alert: q.get("alert") ?? undefined,
      };
    case "connections":
    case "limits":
      return { page: "settings", section: v };
    case "access":
      return { page: "settings", section: "access", permission: q.get("permission") ?? undefined };
    case "settings":
      return { page: "settings", section: "profile" };
    default:
      return null;
  }
}

/** The route an address names. Unknown addresses keep an explicit missing-page state. */
export function parse(pathname: string, search = ""): Route {
  // An absent selection is left out, so two routes to one place are equal.
  return Object.fromEntries(
    Object.entries(
      (() => {
        const route = read(pathname, search);
        const url = new URL(href(route), "http://localhost");
        return read(url.pathname, url.search);
      })(),
    ).filter(([, v]) => v !== undefined),
  ) as Route;
}

function queryText(q: URLSearchParams, key: string, max: number): string | undefined {
  const value = q.get(key);
  // biome-ignore lint/suspicious/noControlCharactersInRegex: a NUL in a query value is refused on purpose
  return value !== null && value.length <= max && !/[\u0000\r\n]/.test(value) ? value : undefined;
}

function read(pathname: string, search: string): Route {
  const q = new URLSearchParams(search);
  const parts = pathname.split("/").filter(Boolean).map(dec);
  const [head, a, b, c] = parts;
  switch (head) {
    case "sessions": {
      if (!a)
        return {
          page: "sessions",
          status: oneOf(q.get("status"), [
            "working",
            "waiting",
            "done",
            "failed",
            "stopped",
          ] as const),
          agent: queryText(q, "agent", 200),
          project: queryText(q, "project", 200),
          q: queryText(q, "q", 200),
        };
      if (b) return read(`/conversations/${enc(a)}/${parts.slice(2).map(enc).join("/")}`, search);
      const panels: Record<string, ConversationPanel> = {
        files: "workspace",
        sources: "context",
        steps: "activity",
        sessions: "overview",
        people: "people",
        workspace: "workspace",
        context: "context",
        activity: "activity",
        overview: "overview",
        deliverables: "deliverables",
      };
      const slide = Number(q.get("slide"));
      return {
        page: "conversation",
        id: a,
        panel:
          q.get("legacy_panel") === "deliverables" ? "deliverables" : panels[q.get("panel") ?? ""],
        message: queryText(q, "message", 512),
        fork: queryText(q, "fork", 512),
        bringBack: queryText(q, "bringBack", 512),
        share: q.get("share") === "1" || undefined,
        contextTab: oneOf(q.get("contextTab"), ["sources", "instructions"] as const),
        filter: oneOf(q.get("filter"), ["all", "handoffs", "tools", "decisions"] as const),
        showTools: q.get("tools") === "0" ? false : undefined,
        run: queryText(q, "run", 512),
        step: queryText(q, "step", 512),
        artifact: queryText(q, "artifact", 512),
        artifactTab: oneOf(q.get("artifactTab"), ARTIFACT_TABS),
        workspaceSection: oneOf(q.get("workspaceSection"), WORKSPACE_SECTIONS),
        sandbox: queryText(q, "sandbox", 512),
        scratchpad: queryText(q, "scratchpad", 512),
        memoryRun: queryText(q, "memoryRun", 512),
        tabs: q.get("tabs")?.split(",").map(dec),
        active: queryText(q, "active", 512),
        compare: queryText(q, "compare", 512),
        file: queryText(q, "file", 4096),
        expanded: q.get("full") === "1" || undefined,
        split: q.get("split") === "1" || undefined,
        workspaceTab: oneOf(q.get("workspaceTab"), ["edit", "preview", "changes"] as const),
        slide: Number.isInteger(slide) && slide >= 1 && slide <= 80 ? slide : undefined,
      };
    }
    case "history":
    case "pull-requests":
      return {
        page: "sessions",
        q: queryText(q, "q", 200),
        status:
          q.get("status") === "needs"
            ? "waiting"
            : q.get("status") === "running"
              ? "working"
              : q.get("status") === "failed"
                ? "failed"
                : undefined,
      };
    case "notifications":
      return { page: "alerts", alert: q.get("alert") ?? undefined };
    case "library":
      return {
        artifact: queryText(q, "artifact", 160),
        file: queryText(q, "file", 256),
        catalogQuery: queryText(q, "q", 1000),
        libraryTab: oneOf(q.get("tab"), ["overview", "contents", "versions"] as const),
        page: "library",
        section: pick(
          a,
          ["sources", "catalog", "skills", "memory", "deliverables"] as const,
          "sources",
        ),
        scope: oneOf(q.get("scope"), ["personal", "workspace", "project", "agent"] as const),
        project: queryText(q, "project", 63),
        owner: queryText(q, "owner", 512),
        skill: queryText(q, "skill", 160),
        version:
          Number.isSafeInteger(Number(q.get("version"))) && Number(q.get("version")) > 0
            ? Number(q.get("version"))
            : undefined,
      };
    case "operations":
      if (a === "audit") return { page: "audit" };
      if (a === "usage") {
        const days = Number(q.get("days"));
        const day = q.get("day");
        const validDay =
          day &&
          /^\d{4}-\d{2}-\d{2}$/.test(day) &&
          !Number.isNaN(Date.parse(`${day}T00:00:00Z`)) &&
          new Date(`${day}T00:00:00Z`).toISOString().slice(0, 10) === day;
        return {
          page: "usage",
          days: days === 7 || days === 30 ? days : 14,
          day: validDay ? day : undefined,
          display: q.get("display") === "table" ? "table" : "chart",
        };
      }
      if (a === "policies")
        return {
          page: "action-policy",
          scope: oneOf(b, ["workspace", "agent", "project", "personal"] as const),
          name: parts[3],
        };
      return {
        page: "operations",
        section: a === "integrations" ? "integrations" : "context",
        selected: b,
        tab:
          a === "integrations" && b
            ? oneOf(c, ["connect", "resources", "links", "access", "events", "actions"] as const)
            : undefined,
      };
    // Generic hubs move to the new navigation; addressed work keeps its action and filters.
    case "assignments":
      return a || q.get("agent")
        ? { page: "assignments", id: a, agent: q.get("agent") || undefined }
        : { page: "sessions" };
    case "agent-network":
      return a === "handoffs" && b ? { page: "agent-network", handoff: b } : { page: "agents" };
    case "agent-controls":
      return { page: "agent-control" };
    case "agent-models":
      // Shared model chains are gone; every agent sets its own.
      return { page: "agents" };
    case "you":
      return {
        page: "you",
        section: pick(
          a,
          ["profile", "notifications", "privacy", "agents", "connections"] as const,
          "profile",
        ),
        provider: a === "connections" ? b : undefined,
      };
    case undefined:
      return (
        legacy(q) ?? {
          page: "new",
          project: q.get("project") === "~" ? "" : (q.get("project") ?? undefined),
          channel: q.get("channel") ?? undefined,
          agent: q.get("agent") ?? undefined,
          ask: q.get("ask") ?? undefined,
        }
      );
    // Conversations were called tasks; their old addresses open them.
    case "conversations":
    case "tasks":
      if (!a) return { page: "new" };
      if (b === "fork" && c) return { page: "conversation", id: a, fork: c };
      if (b === "bring-back" && c) return { page: "conversation", id: a, bringBack: c };
      if (b === "share") return { page: "conversation", id: a, share: true };
      if (oneOf(b, CONVERSATION_PANELS)) {
        const slide = Number(q.get("slide"));
        return {
          page: "conversation",
          id: a,
          panel: b as ConversationPanel,
          ...(b === "workspace"
            ? {
                workspaceSection:
                  parts[3] === "files" ? undefined : oneOf(parts[3], WORKSPACE_SECTIONS),
                sandbox: parts[3] === "sandboxes" ? parts[4] : undefined,
                scratchpad: parts[3] === "scratchpads" ? parts[4] : undefined,
                memoryRun: parts[3] === "memories" ? parts[4] : undefined,
                tabs: q.get("tabs")
                  ? (q.get("tabs") as string).split(",").flatMap((t) => {
                      try {
                        return t ? [decodeURIComponent(t)] : [];
                      } catch {
                        return [];
                      }
                    })
                  : undefined,
                active: q.get("active") || undefined,
                expanded: q.get("full") === "1" || undefined,
                split: q.get("split") === "1" || undefined,
                compare: q.get("compare") || undefined,
                artifactTab: oneOf(q.get("artifact_tab"), ARTIFACT_TABS),
                slide: Number.isInteger(slide) && slide >= 1 && slide <= 80 ? slide : undefined,
                file: q.get("file") || undefined,
                workspaceTab: oneOf(q.get("tab"), ["edit", "preview", "changes"] as const),
              }
            : {}),
          ...(b === "context" && parts[3] === "instructions"
            ? { contextTab: "instructions" as const }
            : {}),
          ...(b === "activity"
            ? {
                run: parts[3],
                step: queryText(q, "step", 512),
                filter: oneOf(q.get("filter"), ["all", "handoffs", "tools", "decisions"] as const),
                showTools: q.get("tools") === "0" ? false : undefined,
              }
            : {}),
          ...(b === "deliverables" && parts[3]
            ? {
                artifact: parts[3],
                artifactTab: oneOf(parts[4], ARTIFACT_TABS),
                compare: q.get("compare") || undefined,
                slide: Number.isInteger(slide) && slide >= 1 && slide <= 80 ? slide : undefined,
              }
            : {}),
        };
      }
      return { page: "conversation", id: a, message: q.get("message") || undefined };
    case "projects": {
      if (!a)
        return {
          page: "projects",
          q: q.get("q") || undefined,
          kind: kindParam(q.get("kind")),
          sort: oneOf(q.get("sort"), PROJECT_SORTS),
        };
      // A project is one page. Its old tabs (context, agents, people, limits,
      // inbox, overview, schedules, history) and its old channel pages open it.
      if (b === "fork") return { page: "project", name: a === "~" ? "" : a, fork: true };
      return { page: "project", name: a === "~" ? "" : a };
    }
    // Scheduled was called Automations; a schedule without a name is the list.
    case "scheduled":
    case "automations":
      return { page: "automations" };
    case "schedules":
      return a ? { page: "schedules", name: a } : { page: "automations" };
    case "monitor":
      return { page: "monitor", tab: pick(a, MONITOR_TABS, "overview") };
    case "alerts":
      return {
        page: "alerts",
        tab: a === "watching" || a === "rules" ? "watching" : undefined,
        state: oneOf(q.get("state"), ["open", "snoozed", "resolved"] as const),
        priority: oneOf(q.get("priority"), ["urgent", "normal", "low"] as const),
        mine: q.get("mine") === "false" ? false : undefined,
        source: queryText(q, "source", 200),
        alert: queryText(q, "alert", 512),
      };
    case "inbox":
      return { page: "alerts", alert: q.get("alert") ?? undefined };
    // Knowledge is called Context. Old links open the same place.
    case "context":
    case "knowledge":
      return a
        ? { page: "context", source: a, tab: pick(b, SOURCE_TABS, "documents") }
        : { page: "context" };
    case "agents":
      if (b === "setup") return { page: "agent-setup", name: a === "new" ? undefined : a };
      if (b === "control") return { page: "agent-control", agent: a };
      if (!a) return { page: "agents", state: queryText(q, "state", 80) };
      return {
        page: "agent",
        name: a === "new" ? null : a,
        tab: a === "new" ? "prompt" : pick(b === "knowledge" ? "context" : b, AGENT_TABS, "prompt"),
      };
    case "artifacts":
      return { page: "artifacts", artifact: a };
    case "customize":
      if (a === "skills")
        return {
          ...(parse("/library/skills", search) as Extract<Route, { page: "library" }>),
          page: "customize",
          section: "skills",
        };
      return {
        page: "customize",
        section: pick(a, ["agents", "skills", "connections", "tools"] as const, "agents"),
        provider: q.get("provider") || undefined,
      };
    case "connections":
      if (a === "sources" && b)
        return { page: "context", source: b, tab: pick(c, SOURCE_TABS, "documents") };
      if (a === "catalog")
        return { page: "library", section: "catalog", catalogQuery: queryText(q, "q", 1000) };
      return { page: "connections", provider: a };
    case "directory":
      // People is the directory; agents and projects have pages of their own.
      if (a === "agents") return { page: "agents" };
      if (a === "projects") return { page: "projects" };
      return { page: "directory", tab: "people" };
    case "settings":
      if (a === "people") return { page: "directory", tab: "people" };
      // Connections is a section of your settings, with a path of its own.
      if (a === "connections") return { page: "connections", provider: b };
      if (a === "health") return { page: "monitor", tab: "health" };
      if (a === "audit") return { page: "audit" };
      if (a === "usage") return read("/operations/usage", search);
      // Models are chosen per agent now; an old link lands on the agents.
      if (a === "models") return { page: "agents" };
      return {
        page: "settings",
        section: pick(a, SETTINGS_SECTIONS, "profile"),
        permission: q.get("permission") ?? undefined,
      };
    default:
      return { page: "not-found", path: `/${parts.map(enc).join("/")}` };
  }
}

/** Whether two routes name the same address. */
export const same = (x: Route, y: Route) => href(x) === href(y);

/**
 * The route on screen and a way to go elsewhere. Going somewhere adds a history
 * entry unless `replace` is set; back and forward move between them.
 */
/**
 * Which screen a route draws: its page and the one thing the page is about.
 * Going to another screen is drawn as a transition; a tab, a filter or a
 * selection on the same screen changes in place.
 */
export function screenOf(r: Route): string {
  switch (r.page) {
    case "conversation":
      return `conversation:${r.id}`;
    case "project":
      return `project:${r.name}`;
    case "channel":
      return `channel:${r.project}/${r.channel}`;
    case "agent":
    case "agent-setup":
      return `${r.page}:${r.name ?? ""}`;
    case "agent-control":
      return `agent-control:${r.agent ?? ""}`;
    case "action-policy":
      return `action-policy:${r.scope ?? ""}:${r.name ?? ""}`;
    case "library":
    case "operations":
    case "you":
    case "settings":
      return `${r.page}:${r.section}`;
    case "inbox":
      return "notifications";
    default:
      return r.page;
  }
}

export function useRoute(): [Route, (next: Route, opts?: { replace?: boolean }) => void] {
  const [route, setRoute] = useState<Route>(() =>
    parse(window.location.pathname, window.location.search),
  );
  // The route on view, read by `go` without making it change on every route.
  const shown = useRef(route);
  shown.current = route;
  // A change of screen is a transition; anything on the same screen, or put in
  // place of the address (typing in a filter), is applied at once. The address
  // changes in the same step as the screen, so the two always agree.
  const show = useCallback((next: Route, transition: boolean, address?: () => void) => {
    if (screenOf(next) !== screenOf(shown.current)) {
      const leaving = new CustomEvent("humagen:before-navigate", {
        cancelable: true,
        detail: { from: shown.current, to: next },
      });
      if (!window.dispatchEvent(leaving)) {
        // Back/Forward already changed the address before popstate was sent.
        if (!address) window.history.replaceState(null, "", href(shown.current));
        return;
      }
    }
    const apply = () => {
      address?.();
      shown.current = next;
      setRoute(next);
    };
    if (transition && screenOf(next) !== screenOf(shown.current)) swap(apply);
    else apply();
  }, []);
  useEffect(() => {
    const back = () => show(parse(window.location.pathname, window.location.search), true);
    window.addEventListener("popstate", back);
    // An old `?view=` link is rewritten to the page's own address.
    const canonical = href(parse(window.location.pathname, window.location.search));
    if (
      window.location.pathname !== "/connections/callback" &&
      canonical !== window.location.pathname + window.location.search
    ) {
      try {
        window.history.replaceState(null, "", canonical);
      } catch {
        // The address just does not follow.
      }
    }
    return () => window.removeEventListener("popstate", back);
  }, [show]);
  const go = useCallback(
    (next: Route, opts?: { replace?: boolean }) => {
      const to = href(next);
      const url = new URL(to, window.location.origin);
      show(parse(url.pathname, url.search), !opts?.replace, () => {
        try {
          if (to !== window.location.pathname + window.location.search) {
            if (opts?.replace) window.history.replaceState(null, "", to);
            else window.history.pushState(null, "", to);
          }
        } catch {
          // The address just does not follow.
        }
      });
      window.scrollTo?.(0, 0);
    },
    [show],
  );
  return [route, go];
}
