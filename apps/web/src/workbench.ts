import { defaultCopy } from "@humagen/components";
import type { HumanActivityInput, HumanActivityRecord, ResponseState } from "@humagen/core";
import {
  DiffIcon,
  ListChecksIcon,
  MessageSquareIcon,
  MonitorIcon,
  SearchIcon,
  TerminalIcon,
} from "@humagen/render-web";
import { type ComponentType, createContext, useContext } from "react";
import type { Run } from "./adapter.ts";
import { lazyView } from "./lazy.tsx";
import {
  type ConversationView,
  MAX_BENCH_REF,
  WORKBENCH_TAB_IDS,
  type WorkbenchTabId,
} from "./route.ts";
import { DetailsTab } from "./SessionPanel.tsx";
import type { SessionSummary } from "./sessionSummary.ts";
import type { Reference as Ref } from "./trace.ts";
import { changesAvailable } from "./workbenchChanges.ts";
import { ranCommands } from "./workbenchTerminal.ts";

/*
 * The session's workbench: the pane Details opens, as tabs. The thread stays
 * on the left; a tab shows what the agent is working with (a browser, a
 * terminal, its changes) or a tool of the reader's own (a side chat, a
 * search). This file is the registry: what each tab is called, what it
 * draws, and when it has anything to show. Workbench.tsx draws the pane.
 * docs/architecture/workbench.md is the contract for a tab's author.
 */

/** What a tab's `available` is asked with: what the pane knows of the session. */
export interface WorkbenchSession {
  /** The conversation the pane is open on. */
  thread: string;
  /** Every run of the conversation the page has read, oldest first. */
  runs: Run[];
  /** Each answer's activity, folded from its trace. Undefined while it is read. */
  responses: ResponseState[] | undefined;
  /** What the session did, sorted (sessionSummary.ts). Undefined while it is read. */
  summary: SessionSummary | undefined;
  /** Whether the reader may post in the conversation. A tab that acts for them needs it. */
  canPost: boolean;
}

/** What a tab's view is given. */
export interface WorkbenchTabProps extends WorkbenchSession {
  /** Where the session started, when it started somewhere. */
  origin?: Ref;
  /** The tab's own selection, from the address (`?benchRef=`): a terminal, a file, a query. */
  selection?: string;
  /** Sets or clears the tab's selection, at its address. At most MAX_BENCH_REF characters. */
  onSelection: (ref?: string) => void;
  /** Opens another tab of the workbench, on a selection if it has one. */
  openTab: (id: WorkbenchTabId, ref?: string) => void;
  /** Opens a saved file in the Workspace pane. */
  openFile?: (artifactId: string) => void;
  /**
   * Records something the reader did by hand in the tab. It shows above the
   * composer and goes with their next message; nothing is sent before that.
   */
  record: (input: HumanActivityInput) => HumanActivityRecord | undefined;
  /** Puts words in the composer for the reader to send. Absent when they may not post. */
  ask?: (text: string) => void;
  /** The pane fills the page. */
  expanded: boolean;
}

/** What a tab's own change declares: one entry in `entries` below. */
export interface WorkbenchTabEntry {
  /** What the tab draws. Every tab but Details is read when opened: `lazyView(() => import(...), ...)`. */
  view: ComponentType<WorkbenchTabProps>;
  /**
   * Whether the session has anything for the tab. A tab that says no is left
   * out of the strip, and its address opens Details. Absent means always.
   * It is asked on every draw, so it reads what it is given and nothing else.
   */
  available?: (session: WorkbenchSession) => boolean;
  /**
   * The view lays out its own height and scrolling, as a terminal or a
   * browser does. Without it the pane pads the view and scrolls it.
   */
  fill?: boolean;
}

/** A tab of the workbench, as the pane draws it. */
export interface WorkbenchTab extends WorkbenchTabEntry {
  id: WorkbenchTabId;
  label: string;
  icon: ComponentType<{ size?: number }>;
}

// What every tab is called and marked with. All six are here from the start,
// so a tab's own change touches neither this table nor copy's `workbench.tab`.
const icons: Record<WorkbenchTabId, ComponentType<{ size?: number }>> = {
  details: ListChecksIcon,
  browser: MonitorIcon,
  terminal: TerminalIcon,
  changes: DiffIcon,
  sidechat: MessageSquareIcon,
  search: SearchIcon,
};

/*
 * The tabs. A tab's own change replaces its one line with its entry and adds
 * its one file; nothing else in this file changes. Keep the comment line
 * above each slot: it is what lets two tabs' changes merge without a conflict.
 *
 *   browser: {
 *     view: lazyView(() => import("./WorkbenchBrowser.tsx"), (m) => m.WorkbenchBrowser),
 *     available: (session) => (session.summary?.environment.length ?? 0) > 0,
 *     fill: true,
 *   },
 *
 * The strip's order is WORKBENCH_TAB_IDS (route.ts), not the order here.
 */
const entries: Record<WorkbenchTabId, WorkbenchTabEntry | undefined> = {
  // Details: progress, agents, outputs, environment and what was used (SessionPanel.tsx).
  details: { view: DetailsTab },
  // Browser slot: the agent's browser or computer view (WorkbenchBrowser.tsx).
  browser: {
    view: lazyView(
      () => import("./WorkbenchBrowser.tsx"),
      (m) => m.WorkbenchBrowser,
    ),
    available: (session) => session.summary?.environment.some((e) => e.kind === "browser") ?? false,
    fill: true,
  },
  // Terminal slot: a terminal in the session's sandbox (WorkbenchTerminal.tsx).
  terminal: {
    view: lazyView(
      () => import("./WorkbenchTerminal.tsx"),
      (m) => m.WorkbenchTerminal,
    ),
    available: (session) =>
      ranCommands(session.responses) ||
      (session.summary?.environment.some((e) => e.kind === "sandbox") ?? false),
    fill: true,
  },
  // Changes slot: what the session changed, to keep or discard (WorkbenchChanges.tsx).
  changes: {
    view: lazyView(
      () => import("./WorkbenchChanges.tsx"),
      (m) => m.WorkbenchChanges,
    ),
    available: changesAvailable,
  },
  // Side chat slot: a chat beside the thread that does not post in it (WorkbenchSidechat.tsx).
  sidechat: {
    view: lazyView(
      () => import("./WorkbenchSidechat.tsx"),
      (m) => m.WorkbenchSidechat,
    ),
    available: (session) => session.canPost,
    fill: true,
  },
  // Search slot: pull requests, artifacts and tools (WorkbenchSearch.tsx).
  search: {
    view: lazyView(
      () => import("./WorkbenchSearch.tsx"),
      (m) => m.WorkbenchSearch,
    ),
    fill: true,
  },
};

/** Every tab that has been built, in the strip's order. */
export const WORKBENCH_TABS: readonly WorkbenchTab[] = WORKBENCH_TAB_IDS.flatMap((id) => {
  const entry = entries[id];
  return entry ? [{ ...entry, id, label: defaultCopy.workbench.tab[id], icon: icons[id] }] : [];
});

/**
 * The tabs a session's strip shows: Details, those built whose `available`
 * does not say no, and the one the address names, which a link or a card in
 * the thread may open before the session has anything for it. That tab says
 * so itself, with a title and one action.
 */
export function workbenchTabs(
  session: WorkbenchSession,
  open?: WorkbenchTabId,
  tabs: readonly WorkbenchTab[] = WORKBENCH_TABS,
): WorkbenchTab[] {
  return tabs.filter(
    (tab) => tab.id === "details" || tab.id === open || (tab.available?.(session) ?? true),
  );
}

/** The tab an address opens among those shown: the one it names, else Details, else the first. */
export function activeBench(
  view: ConversationView,
  shown: readonly WorkbenchTab[],
): WorkbenchTab | undefined {
  return (
    shown.find((tab) => tab.id === view.bench) ??
    shown.find((tab) => tab.id === "details") ??
    shown[0]
  );
}

/**
 * The view with the workbench open on a tab, on a selection if one is given.
 * Whether the pane fills the page is kept; everything else of another panel
 * is dropped, as opening any panel drops it.
 */
export function openBench(
  view: ConversationView,
  id: WorkbenchTabId,
  ref?: string,
): ConversationView {
  const next: ConversationView = { panel: "details" };
  if (id !== "details") next.bench = id;
  if (id !== "details" && ref) next.benchRef = ref.slice(0, MAX_BENCH_REF);
  if (view.panel === "details" && view.expanded) next.expanded = true;
  return next;
}

/** Opens a workbench tab from anywhere under a session's page. Null where there is no workbench. */
export const WorkbenchOpen = createContext<((id: WorkbenchTabId, ref?: string) => void) | null>(
  null,
);

/**
 * The way to open a workbench tab from a card in the thread, as a card for
 * browser work opens the Browser tab. Undefined outside a session's page and
 * for a tab that is not built, so a card draws no link that leads nowhere.
 */
export function useOpenWorkbench(
  id: WorkbenchTabId,
  tabs: readonly WorkbenchTab[] = WORKBENCH_TABS,
): ((ref?: string) => void) | undefined {
  const open = useContext(WorkbenchOpen);
  return open && tabs.some((tab) => tab.id === id) ? (ref) => open(id, ref) : undefined;
}
