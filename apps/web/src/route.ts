import { useCallback, useEffect, useState } from "react";

/**
 * Where the console is, as an address. Every page, tab and selection a person
 * can reach has one, so a reload, a copied link and the back button all land in
 * the same place.
 *
 * Pages are paths; a selection inside a page (an alert, an inbox message) is a
 * query parameter. Old `?view=` links still open the page they named.
 */

export type AgentTab = "prompt" | "model" | "access" | "queue" | "knowledge" | "memory" | "evals" | "alerts" | "revisions";
export const AGENT_TABS: AgentTab[] = ["prompt", "model", "access", "queue", "knowledge", "memory", "evals", "alerts", "revisions"];

export type SourceTab = "documents" | "search" | "variants" | "evals";
export const SOURCE_TABS: SourceTab[] = ["documents", "search", "variants", "evals"];

export type AlertsTab = "open" | "rules" | "overview";
export const ALERTS_TABS: AlertsTab[] = ["open", "rules", "overview"];

export type SettingsSection = "profile" | "appearance" | "workspace" | "routing" | "models" | "limits" | "connections" | "access";
export const SETTINGS_SECTIONS: SettingsSection[] = ["profile", "appearance", "workspace", "routing", "models", "limits", "connections", "access"];

export type Route =
  | { page: "new"; project?: string }
  | { page: "task"; id: string }
  | { page: "projects" }
  | { page: "project"; name: string }
  | { page: "schedules"; name?: string }
  | { page: "alerts"; tab: AlertsTab; alert?: string }
  | { page: "inbox"; message?: string }
  | { page: "knowledge"; source?: string; tab?: SourceTab }
  | { page: "agents" }
  | { page: "agent"; name: string | null; tab: AgentTab }
  | { page: "artifacts" }
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
const pick = <T extends string>(value: string | undefined, allowed: readonly T[], fallback: T): T =>
  value && (allowed as readonly string[]).includes(value) ? (value as T) : fallback;

/** The address of a route, path and query. */
export function href(r: Route): string {
  switch (r.page) {
    case "new":
      return r.project ? `/?project=${enc(r.project)}` : "/";
    case "task":
      return `/tasks/${enc(r.id)}`;
    case "projects":
      return "/projects";
    case "project":
      return `/projects/${enc(r.name)}`;
    case "schedules":
      return r.name ? `/schedules/${enc(r.name)}` : "/schedules";
    case "alerts": {
      const base = r.tab === "open" ? "/alerts" : `/alerts/${r.tab}`;
      return r.alert ? `${base}?alert=${enc(r.alert)}` : base;
    }
    case "inbox":
      return r.message ? `/inbox?message=${enc(r.message)}` : "/inbox";
    case "knowledge":
      if (!r.source) return "/knowledge";
      return !r.tab || r.tab === "documents" ? `/knowledge/${enc(r.source)}` : `/knowledge/${enc(r.source)}/${r.tab}`;
    case "agents":
      return "/agents";
    case "agent": {
      const base = r.name === null ? "/agents/new" : `/agents/${enc(r.name)}`;
      return r.tab === "prompt" ? base : `${base}/${r.tab}`;
    }
    case "artifacts":
      return "/artifacts";
    case "settings": {
      const base = `/settings/${r.section}`;
      return r.section === "access" && r.permission ? `${base}?permission=${enc(r.permission)}` : base;
    }
  }
}

/** Links from before pages had paths: `/?view=agents`, `/?view=alerts&alert=…`. */
function legacy(q: URLSearchParams): Route | null {
  const v = q.get("view");
  if (!v) return q.has("message") ? { page: "inbox", message: q.get("message") ?? undefined } : null;
  switch (v) {
    case "home":
    case "chat":
    case "runs":
      return { page: "new" };
    case "inbox":
      return { page: "inbox", message: q.get("message") ?? undefined };
    case "projects":
    case "schedules":
    case "agents":
    case "knowledge":
      return { page: v };
    case "storage":
      return { page: "artifacts" };
    case "evals":
      return { page: "agents" };
    case "analytics":
      return { page: "alerts", tab: "overview" };
    case "alerts":
      return { page: "alerts", tab: "open", alert: q.get("alert") ?? undefined };
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
      return legacy(q) ?? (q.get("project") ? { page: "new", project: q.get("project") ?? undefined } : { page: "new" });
    case "tasks":
      return a ? { page: "task", id: a } : { page: "new" };
    case "projects":
      return a ? { page: "project", name: a } : { page: "projects" };
    case "schedules":
      return { page: "schedules", name: a };
    case "alerts":
      return { page: "alerts", tab: pick(a, ALERTS_TABS, "open"), alert: q.get("alert") ?? undefined };
    case "inbox":
      return { page: "inbox", message: q.get("message") ?? undefined };
    case "knowledge":
      return a ? { page: "knowledge", source: a, tab: pick(b, SOURCE_TABS, "documents") } : { page: "knowledge" };
    case "agents":
      if (!a) return { page: "agents" };
      return { page: "agent", name: a === "new" ? null : a, tab: a === "new" ? "prompt" : pick(b, AGENT_TABS, "prompt") };
    case "artifacts":
      return { page: "artifacts" };
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
