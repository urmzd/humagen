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
 * A project's tabs. The inbox is the default, so `/projects/{name}` opens what
 * is new there; an unknown tab falls back to it. More arrive with later phases
 * of docs/architecture/projects.md.
 */
export type ProjectTab =
  | "inbox"
  | "overview"
  | "context"
  | "agents"
  | "channels"
  | "schedules"
  | "people"
  | "limits"
  | "history";
export const PROJECT_TABS: ProjectTab[] = [
  "inbox",
  "overview",
  "context",
  "agents",
  "channels",
  "schedules",
  "people",
  "limits",
  "history",
];

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

export type SettingsSection =
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

export type ConversationPanel = "activity" | "context" | "people" | "deliverables";
const CONVERSATION_PANELS: ConversationPanel[] = ["activity", "context", "people", "deliverables"];
export type ArtifactTab = "preview" | "versions" | "provenance";
const ARTIFACT_TABS: ArtifactTab[] = ["preview", "versions", "provenance"];
export interface ConversationView {
  contextTab?: "sources" | "instructions";
  panel?: ConversationPanel;
  filter?: "all" | "handoffs" | "tools" | "decisions";
  showTools?: boolean;
  run?: string;
  artifact?: string;
  artifactTab?: ArtifactTab;
  /** One-based slide selection. */
  slide?: number;
}

export type LibrarySection = "sources" | "skills" | "memory" | "deliverables";
export type YouSection = "profile" | "notifications" | "privacy" | "agents" | "connections";
export type Route =
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
  | { page: "notifications"; message?: string; alert?: string; kind?: string }
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
  /** Every project, searched with `q` and narrowed to one kind. */
  | { page: "projects"; q?: string; kind?: ProjectKindFilter }
  /** One project at a tab, and in its inbox, at a message. The workspace itself is the project named "", written `~`. */
  | { page: "project"; name: string; tab: ProjectTab; message?: string; fork?: boolean }
  /** One of a project's channels, with its shared threads. */
  | { page: "channel"; project: string; channel: string }
  | { page: "schedules"; name?: string }
  | { page: "monitor"; tab: MonitorTab }
  /** The inbox, at a message, at the message an alert raised, or narrowed to one kind. */
  | { page: "inbox"; message?: string; alert?: string; kind?: string }
  | { page: "context"; source?: string; tab?: SourceTab }
  | { page: "agents" }
  | { page: "agent"; name: string | null; tab: AgentTab }
  | { page: "artifacts"; artifact?: string }
  /** Your connections: the tools that act as you, and the credential you gave each. `provider` opens one to connect. */
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
  new: "New conversation",
  conversation: "Conversation",
  projects: "Projects",
  project: "Project",
  channel: "Channel",
  schedules: "Schedules",
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
  return `${unread > 0 ? `(${unread}) ` : ""}${place} · ${product}`;
}

/** The address of a route, path and query. */
export function href(r: Route): string {
  switch (r.page) {
    case "library": {
      const q = new URLSearchParams();
      if (r.scope) q.set("scope", r.scope);
      if (r.project !== undefined) q.set("project", r.project);
      if (r.owner !== undefined) q.set("owner", r.owner);
      if (r.skill) q.set("skill", r.skill);
      if (r.version && Number.isSafeInteger(r.version) && r.version > 0)
        q.set("version", String(r.version));
      if (r.artifact) q.set("artifact", r.artifact);
      if (r.file) q.set("file", r.file);
      if (r.libraryTab) q.set("tab", r.libraryTab);
      return `/library/${r.section}${q.size ? `?${q}` : ""}`;
    }
    case "operations":
      return `/operations/${r.section}${r.selected ? `/${enc(r.selected)}${r.section === "integrations" && r.tab && r.tab !== "connect" ? `/${r.tab}` : ""}` : ""}`;
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
      return "/operations/audit";
    case "usage": {
      const q = new URLSearchParams();
      if (r.days && r.days !== 14) q.set("days", String(r.days));
      if (r.day) q.set("day", r.day);
      if (r.display === "table") q.set("display", "table");
      return `/operations/usage${q.size ? `?${q}` : ""}`;
    }
    case "you":
      return `/you${r.section === "profile" ? "" : `/${r.section}`}${r.section === "connections" && r.provider ? `/${enc(r.provider)}` : ""}`;
    case "new": {
      const q = new URLSearchParams();
      if (r.project !== undefined) q.set("project", r.project === "" ? "~" : r.project);
      if (r.channel) q.set("channel", r.channel);
      if (r.agent) q.set("agent", r.agent);
      if (r.ask) q.set("ask", r.ask);
      const query = q.toString();
      return query ? `/?${query}` : "/";
    }
    case "conversation":
      if (r.fork) return `/conversations/${enc(r.id)}/fork/${enc(r.fork)}`;
      if (r.bringBack) return `/conversations/${enc(r.id)}/bring-back/${enc(r.bringBack)}`;
      if (r.share) return `/conversations/${enc(r.id)}/share`;
      if (r.panel) {
        const base = `/conversations/${enc(r.id)}/${r.panel}`;
        if (r.panel === "activity") {
          const q = new URLSearchParams();
          if (r.filter && r.filter !== "all") q.set("filter", r.filter);
          if (r.showTools === false) q.set("tools", "0");
          return `${base}${r.run ? `/${enc(r.run)}` : ""}${q.size ? `?${q}` : ""}`;
        }
        if (r.panel === "context" && r.contextTab === "instructions") return `${base}/instructions`;
        if (r.panel === "deliverables" && r.artifact)
          return `${base}/${enc(r.artifact)}${r.artifactTab ? `/${r.artifactTab}` : ""}${r.slide ? `?slide=${r.slide}` : ""}`;
        return base;
      }
      return r.message
        ? `/conversations/${enc(r.id)}?${new URLSearchParams({ message: r.message })}`
        : `/conversations/${enc(r.id)}`;
    case "projects": {
      const q = new URLSearchParams();
      if (r.q) q.set("q", r.q);
      if (r.kind) q.set("kind", r.kind);
      const query = q.toString();
      return query ? `/projects?${query}` : "/projects";
    }
    case "project": {
      if (r.fork) return `/projects/${enc(r.name || "~")}/fork`;
      // Names never hold "/", so nesting lives in the tree, never in the path.
      const base = `/projects/${r.name === "" ? "~" : enc(r.name)}`;
      if (r.tab !== "inbox") return `${base}/${r.tab}`;
      return r.message ? `${base}?message=${enc(r.message)}` : base;
    }
    case "channel":
      return `/projects/${r.project === "" ? "~" : enc(r.project)}/channels/${enc(r.channel)}`;
    case "schedules":
      return r.name ? `/schedules/${enc(r.name)}` : "/schedules";
    case "monitor":
      return r.tab === "overview" ? "/monitor" : `/monitor/${r.tab}`;
    case "notifications":
    case "inbox": {
      const q = new URLSearchParams();
      if (r.message) q.set("message", r.message);
      if (r.alert) q.set("alert", r.alert);
      if (r.kind) q.set("kind", r.kind);
      const query = q.toString();
      const path = r.page === "notifications" ? "/notifications" : "/inbox";
      return query ? `${path}?${query}` : path;
    }
    case "context":
      if (!r.source) return "/context";
      return !r.tab || r.tab === "documents"
        ? `/context/${enc(r.source)}`
        : `/context/${enc(r.source)}/${r.tab}`;
    case "agents":
      return "/agents";
    case "agent": {
      const base = r.name === null ? "/agents/new" : `/agents/${enc(r.name)}`;
      return r.tab === "prompt" ? base : `${base}/${r.tab}`;
    }
    case "artifacts":
      return r.artifact ? `/artifacts/${enc(r.artifact)}` : "/artifacts";
    case "connections":
      return r.provider ? `/connections/${enc(r.provider)}` : "/connections";
    case "directory":
      return r.tab === "people" ? "/directory" : `/directory/${r.tab}`;
    case "settings": {
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
      return inboxAt(q);
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
        page: "inbox",
        alert: q.get("alert") ?? undefined,
        kind: q.get("alert") ? undefined : "alert",
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

/** The route an address names. Anything it does not recognize is New. */
export function parse(pathname: string, search = ""): Route {
  // An absent selection is left out, so two routes to one place are equal.
  return Object.fromEntries(
    Object.entries(read(pathname, search)).filter(([, v]) => v !== undefined),
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
    case "notifications":
      return {
        page: "notifications",
        message: q.get("message") ?? undefined,
        alert: q.get("alert") ?? undefined,
        kind: q.get("kind") ?? undefined,
      };
    case "library":
      return {
        artifact: queryText(q, "artifact", 160),
        file: queryText(q, "file", 256),
        libraryTab: oneOf(q.get("tab"), ["overview", "contents", "versions"] as const),
        page: "library",
        section: pick(a, ["sources", "skills", "memory", "deliverables"] as const, "sources"),
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
    case "assignments":
      return { page: "assignments", id: a, agent: q.get("agent") || undefined };
    case "agent-network":
      return { page: "agent-network", handoff: a === "handoffs" ? b : undefined };
    case "agent-controls":
      return { page: "agent-control" };
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
          ...(b === "context" && parts[3] === "instructions"
            ? { contextTab: "instructions" as const }
            : {}),
          ...(b === "activity"
            ? {
                run: parts[3],
                filter: oneOf(q.get("filter"), ["all", "handoffs", "tools", "decisions"] as const),
                showTools: q.get("tools") === "0" ? false : undefined,
              }
            : {}),
          ...(b === "deliverables" && parts[3]
            ? {
                artifact: parts[3],
                artifactTab: oneOf(parts[4], ARTIFACT_TABS),
                slide: Number.isInteger(slide) && slide >= 1 && slide <= 80 ? slide : undefined,
              }
            : {}),
        };
      }
      return { page: "conversation", id: a, message: q.get("message") || undefined };
    case "projects": {
      if (!a)
        return { page: "projects", q: q.get("q") || undefined, kind: kindParam(q.get("kind")) };
      if (b === "fork")
        return { page: "project", name: a === "~" ? "" : a, tab: "inbox", fork: true };
      // A channel is a page of its own, under the project's Channels tab.
      if (b === "channels" && parts[3])
        return { page: "channel", project: a === "~" ? "" : a, channel: parts[3] };
      const tab = pick(b, PROJECT_TABS, "inbox");
      return {
        page: "project",
        name: a === "~" ? "" : a,
        tab,
        message: tab === "inbox" ? (q.get("message") ?? undefined) : undefined,
      };
    }
    case "schedules":
      return { page: "schedules", name: a };
    case "monitor":
      return { page: "monitor", tab: pick(a, MONITOR_TABS, "overview") };
    // Alerts became Monitor. Its rules and overview moved there; what fired is in the inbox.
    case "alerts":
      if (q.get("alert")) return { page: "inbox", alert: q.get("alert") ?? undefined };
      if (a === "rules" || a === "overview") return { page: "monitor", tab: a };
      return { page: "inbox", kind: "alert" };
    case "inbox":
      return inboxAt(q);
    // Knowledge is called Context. Old links open the same place.
    case "context":
    case "knowledge":
      return a
        ? { page: "context", source: a, tab: pick(b, SOURCE_TABS, "documents") }
        : { page: "context" };
    case "agents":
      if (b === "setup") return { page: "agent-setup", name: a === "new" ? undefined : a };
      if (b === "control") return { page: "agent-control", agent: a };
      if (!a) return { page: "agents" };
      return {
        page: "agent",
        name: a === "new" ? null : a,
        tab: a === "new" ? "prompt" : pick(b === "knowledge" ? "context" : b, AGENT_TABS, "prompt"),
      };
    case "artifacts":
      return { page: "artifacts", artifact: a };
    case "connections":
      return { page: "connections", provider: a };
    case "directory":
      return { page: "directory", tab: pick(a, DIRECTORY_TABS, "people") };
    case "settings":
      return {
        page: "settings",
        section: pick(a, SETTINGS_SECTIONS, "profile"),
        permission: q.get("permission") ?? undefined,
      };
    default:
      return { page: "new" };
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
    if (canonical !== window.location.pathname + window.location.search) {
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
      show(next, !opts?.replace, () => {
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
