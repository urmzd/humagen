import type { ConversationView, WorkspaceSection } from "./route.ts";

/**
 * The Workspace pane's tabs. A tab is a kind and, for one item, a reference:
 * "files" lists the session's files and "file:src/a.ts" is one of them. The
 * open tabs and the active one are the pane's address (`?tabs=&active=`), so
 * a copied link reopens the same pane.
 */
export const PANE_KINDS = [
  "sandboxes",
  "sandbox",
  "files",
  "file",
  "scratchpads",
  "scratchpad",
  "memories",
  "memory",
  "artifacts",
  "artifact",
  "issue",
  "pull",
  "review",
] as const;
export type PaneKind = (typeof PANE_KINDS)[number];
const LISTS: PaneKind[] = ["sandboxes", "files", "scratchpads", "memories", "artifacts"];
export interface PaneTab {
  kind: PaneKind;
  ref?: string;
}
/** The most tabs a pane keeps open; opening another closes the oldest. */
export const MAX_PANE_TABS = 12;

export const tabKey = (tab: PaneTab): string => (tab.ref ? `${tab.kind}:${tab.ref}` : tab.kind);

export function parseTab(key: string): PaneTab | undefined {
  const at = key.indexOf(":");
  const kind = (at < 0 ? key : key.slice(0, at)) as PaneKind;
  const ref = at < 0 ? undefined : key.slice(at + 1);
  if (!PANE_KINDS.includes(kind) || ref === "") return undefined;
  // A list has no reference; an item needs one.
  if (LISTS.includes(kind) !== (ref === undefined)) return undefined;
  return ref === undefined ? { kind } : { kind, ref };
}

/** The tab an address from before tabs named: a section and its selection. */
function legacyTab(view: ConversationView): PaneTab {
  const section: WorkspaceSection = view.workspaceSection ?? "files";
  switch (section) {
    case "sandboxes":
      return view.sandbox ? { kind: "sandbox", ref: view.sandbox } : { kind: "sandboxes" };
    case "scratchpads":
      return view.scratchpad
        ? { kind: "scratchpad", ref: view.scratchpad }
        : { kind: "scratchpads" };
    case "memories":
      return view.memoryRun ? { kind: "memory", ref: view.memoryRun } : { kind: "memories" };
    case "artifacts":
      return { kind: "artifacts" };
    default:
      return view.file ? { kind: "file", ref: view.file } : { kind: "files" };
  }
}

/** The pane's open tabs, oldest first. */
export function paneTabs(view: ConversationView): PaneTab[] {
  const tabs = (view.tabs ?? []).flatMap((key) => {
    const tab = parseTab(key);
    return tab ? [tab] : [];
  });
  return tabs.length ? tabs : [legacyTab(view)];
}

/** The active tab's key: the one named, else the newest. */
export function activeKey(view: ConversationView): string {
  const keys = paneTabs(view).map(tabKey);
  return view.active && keys.includes(view.active) ? view.active : (keys.at(-1) as string);
}

/** Opens a tab, or shows it when it is open, and makes it active. */
export function openTab(view: ConversationView, tab: PaneTab): ConversationView {
  const key = tabKey(tab);
  let keys = paneTabs(view).map(tabKey);
  if (!keys.includes(key)) keys = [...keys, key];
  while (keys.length > MAX_PANE_TABS) {
    const oldest = keys.findIndex((k) => k !== key);
    keys.splice(oldest, 1);
  }
  return paneView(view, keys, key);
}

/** Replaces one tab with another in its place, as a list does when it opens an item. */
export function replaceTab(view: ConversationView, from: string, to: PaneTab): ConversationView {
  const key = tabKey(to);
  const out: string[] = [];
  for (const k of paneTabs(view).map(tabKey)) {
    const next = k === from ? key : k;
    if (!out.includes(next)) out.push(next);
  }
  if (!out.includes(key)) out.push(key);
  return paneView(view, out, key);
}

/** Closes a tab. The neighbor before it becomes active; closing the last closes the pane. */
export function closeTab(view: ConversationView, key: string): ConversationView | undefined {
  const keys = paneTabs(view).map(tabKey);
  const at = keys.indexOf(key);
  if (at < 0) return view;
  const rest = keys.filter((k) => k !== key);
  if (!rest.length) return undefined;
  const active = activeKey(view) === key ? (rest[Math.max(0, at - 1)] as string) : activeKey(view);
  return paneView(view, rest, active);
}

/**
 * The view for a set of tabs. The section fields from before tabs are
 * dropped, so the tabs alone say what is open; the file view mode is kept.
 */
function paneView(view: ConversationView, keys: string[], active: string): ConversationView {
  const out: ConversationView = { panel: "workspace", tabs: keys, active };
  if (view.workspaceTab) out.workspaceTab = view.workspaceTab;
  if (view.artifactTab) out.artifactTab = view.artifactTab;
  if (view.slide) out.slide = view.slide;
  if (view.compare) out.compare = view.compare;
  if (view.expanded) out.expanded = true;
  if (view.split) out.split = true;
  return out;
}

/** Opens the pane on one tab, or adds the tab to the open pane. */
export function openPane(view: ConversationView, tab: PaneTab): ConversationView {
  if (view.panel !== "workspace")
    return { panel: "workspace", tabs: [tabKey(tab)], active: tabKey(tab) };
  return openTab(view, tab);
}
