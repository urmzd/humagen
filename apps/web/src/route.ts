import { useCallback, useEffect, useState } from "react";

/**
 * Where the console is, as an address. Every page, tab and selection a person
 * can reach has one, so a reload, a copied link and the back button all land in
 * the same place.
 *
 * Pages are paths; a selection inside a page (an alert, an inbox message) is a
 * query parameter. Old `?view=` links still open the page they named.
 */

export type AgentTab = "prompt" | "model" | "access" | "queue" | "context" | "memory" | "evals" | "alerts" | "revisions";
export const AGENT_TABS: AgentTab[] = ["prompt", "model", "access", "queue", "context", "memory", "evals", "alerts", "revisions"];

export type SourceTab = "documents" | "search" | "variants" | "evals";
export const SOURCE_TABS: SourceTab[] = ["documents", "search", "variants", "evals"];

/** Monitor: how runs are going (overview), and the rules that watch them. What a rule fires lands in the inbox. */
export type MonitorTab = "overview" | "rules";
export const MONITOR_TABS: MonitorTab[] = ["overview", "rules"];

/** The Directory: who and what is in the workspace, for everyone in it. */
export type DirectoryTab = "people" | "agents" | "projects";
export const DIRECTORY_TABS: DirectoryTab[] = ["people", "agents", "projects"];

/**
 * A project's tabs. The inbox is the default, so `/projects/{name}` opens what
 * is new there; an unknown tab falls back to it. More arrive with later phases
 * of docs/projects.md.
 */
export type ProjectTab = "inbox" | "overview" | "context" | "agents" | "people" | "limits" | "history" | "schedules" | "channels";
export const PROJECT_TABS: ProjectTab[] = ["inbox", "overview", "context", "agents", "people", "limits", "history", "schedules", "channels"];

/**
 * What a project is for, as the Projects page narrows to it with `?kind=`:
 * one of warp's kinds, or one the workspace defines, which has the same
 * shape. A value of any other shape falls back to every kind.
 */
export type ProjectKindFilter = string;
const KIND_SHAPE = /^[a-z][a-z0-9-]{0,31}$/;
/** A kind as an address names it, or nothing when it is not shaped as one. */
export const kindParam = (value: string | null | undefined): ProjectKindFilter | undefined => (value && KIND_SHAPE.test(value) ? value : undefined);

export type SettingsSection = "profile" | "appearance" | "workspace" | "routing" | "models" | "limits" | "connections" | "access";
export const SETTINGS_SECTIONS: SettingsSection[] = ["profile", "appearance", "workspace", "routing", "models", "limits", "connections", "access"];

export type Route =
  /** A new conversation, optionally with an agent picked and a first message written for it. */
  | { page: "new"; project?: string; channel?: string; agent?: string; ask?: string }
  /** A conversation, at one of its people's messages when `message` is set. */
  | { page: "task"; id: string; message?: string }
  /** Every project, searched with `q` and narrowed to one kind. */
  | { page: "projects"; q?: string; kind?: ProjectKindFilter }
  /** One project at a tab, and in its inbox, at a message. The workspace itself is the project named "", written `~`. */
  | { page: "project"; name: string; tab: ProjectTab; message?: string }
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
const oneOf = <T extends string>(value: string | null | undefined, allowed: readonly T[]): T | undefined =>
  value && (allowed as readonly string[]).includes(value) ? (value as T) : undefined;
const pick = <T extends string>(value: string | undefined, allowed: readonly T[], fallback: T): T => oneOf(value, allowed) ?? fallback;

/** The address of a route, path and query. */
export function href(r: Route): string {
  switch (r.page) {
    case "new": {
      const q = new URLSearchParams();
      if (r.project) q.set("project", r.project);
      if (r.channel) q.set("channel", r.channel);
      if (r.agent) q.set("agent", r.agent);
      if (r.ask) q.set("ask", r.ask);
      const query = q.toString();
      return query ? `/?${query}` : "/";
    }
    case "task":
      return r.message ? `/tasks/${enc(r.id)}?${new URLSearchParams({ message: r.message })}` : `/tasks/${enc(r.id)}`;
    case "projects": {
      const q = new URLSearchParams();
      if (r.q) q.set("q", r.q);
      if (r.kind) q.set("kind", r.kind);
      const query = q.toString();
      return query ? `/projects?${query}` : "/projects";
    }
    case "project": {
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
    case "inbox": {
      const q = new URLSearchParams();
      if (r.message) q.set("message", r.message);
      if (r.alert) q.set("alert", r.alert);
      if (r.kind) q.set("kind", r.kind);
      const query = q.toString();
      return query ? `/inbox?${query}` : "/inbox";
    }
    case "context":
      if (!r.source) return "/context";
      return !r.tab || r.tab === "documents" ? `/context/${enc(r.source)}` : `/context/${enc(r.source)}/${r.tab}`;
    case "agents":
      return "/agents";
    case "agent": {
      const base = r.name === null ? "/agents/new" : `/agents/${enc(r.name)}`;
      return r.tab === "prompt" ? base : `${base}/${r.tab}`;
    }
    case "artifacts":
      return r.artifact ? `/artifacts/${enc(r.artifact)}` : "/artifacts";
    case "directory":
      return r.tab === "people" ? "/directory" : `/directory/${r.tab}`;
    case "settings": {
      const base = `/settings/${r.section}`;
      return r.section === "access" && r.permission ? `${base}?permission=${enc(r.permission)}` : base;
    }
  }
}

function inboxAt(q: URLSearchParams): Route {
  return { page: "inbox", message: q.get("message") ?? undefined, alert: q.get("alert") ?? undefined, kind: q.get("kind") ?? undefined };
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
      return { page: "inbox", alert: q.get("alert") ?? undefined, kind: q.get("alert") ? undefined : "alert" };
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
  return Object.fromEntries(Object.entries(read(pathname, search)).filter(([, v]) => v !== undefined)) as Route;
}

function read(pathname: string, search: string): Route {
  const q = new URLSearchParams(search);
  const parts = pathname.split("/").filter(Boolean).map(dec);
  const [head, a, b] = parts;
  switch (head) {
    case undefined:
      return (
        legacy(q) ?? {
          page: "new",
          project: q.get("project") ?? undefined,
          channel: q.get("channel") ?? undefined,
          agent: q.get("agent") ?? undefined,
          ask: q.get("ask") ?? undefined,
        }
      );
    case "tasks":
      return a ? { page: "task", id: a, message: q.get("message") || undefined } : { page: "new" };
    case "projects": {
      if (!a) return { page: "projects", q: q.get("q") || undefined, kind: kindParam(q.get("kind")) };
      // A channel is a page of its own, under the project's Channels tab.
      if (b === "channels" && parts[3]) return { page: "channel", project: a === "~" ? "" : a, channel: parts[3] };
      const tab = pick(b, PROJECT_TABS, "inbox");
      return { page: "project", name: a === "~" ? "" : a, tab, message: tab === "inbox" ? (q.get("message") ?? undefined) : undefined };
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
      return a ? { page: "context", source: a, tab: pick(b, SOURCE_TABS, "documents") } : { page: "context" };
    case "agents":
      if (!a) return { page: "agents" };
      return {
        page: "agent",
        name: a === "new" ? null : a,
        tab: a === "new" ? "prompt" : pick(b === "knowledge" ? "context" : b, AGENT_TABS, "prompt"),
      };
    case "artifacts":
      return { page: "artifacts", artifact: a };
    case "directory":
      return { page: "directory", tab: pick(a, DIRECTORY_TABS, "people") };
    case "settings":
      return { page: "settings", section: pick(a, SETTINGS_SECTIONS, "profile"), permission: q.get("permission") ?? undefined };
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
export function useRoute(): [Route, (next: Route, opts?: { replace?: boolean }) => void] {
  const [route, setRoute] = useState<Route>(() => parse(window.location.pathname, window.location.search));
  useEffect(() => {
    const back = () => setRoute(parse(window.location.pathname, window.location.search));
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
  }, []);
  const go = useCallback((next: Route, opts?: { replace?: boolean }) => {
    const to = href(next);
    try {
      if (to !== window.location.pathname + window.location.search) {
        if (opts?.replace) window.history.replaceState(null, "", to);
        else window.history.pushState(null, "", to);
      }
    } catch {
      // The address just does not follow.
    }
    setRoute(next);
    window.scrollTo?.(0, 0);
  }, []);
  return [route, go];
}
