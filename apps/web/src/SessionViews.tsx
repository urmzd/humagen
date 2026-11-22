import { defaultCopy } from "@humagen/components";
import {
  DiffIcon,
  EllipsisIcon,
  FolderIcon,
  HistoryIcon,
  LayersIcon,
  ListChecksIcon,
  MonitorIcon,
  SearchIcon,
  TerminalIcon,
  ZapIcon,
} from "@humagen/render-web";
import { type ComponentType, useEffect, useRef, useState } from "react";
import { ApiError, call } from "./api.ts";
import { menuRow, Popover } from "./ui.tsx";
import { workspaceFiles } from "./workspace-files.ts";

const words = defaultCopy.sessionPanel;

/**
 * A view of the session a person can open from its header: the panel
 * (Details), a workbench tab, a tab of the Workspace pane, the session's
 * progress, or the background work it asked for.
 */
export type SessionView =
  | "details"
  | "terminal"
  | "changes"
  | "browser"
  | "files"
  | "artifacts"
  | "search"
  | "background"
  | "progress";

type Icon = ComponentType<{ size?: number }>;

const icon: Record<SessionView, Icon> = {
  details: ListChecksIcon,
  terminal: TerminalIcon,
  changes: DiffIcon,
  browser: MonitorIcon,
  files: FolderIcon,
  artifacts: LayersIcon,
  search: SearchIcon,
  background: ZapIcon,
  progress: HistoryIcon,
};

/** The views with a button of their own in the header, after Details, on a wide header. */
export const PRIMARY_VIEWS = ["terminal", "changes", "browser", "files"] as const;
/** The views in the More Views menu. On a phone the primary ones join them there. */
export const SECONDARY_VIEWS = ["artifacts", "search", "background", "progress"] as const;

/** Whether the keys are a Mac's, for the shortcut a row prints. */
const onMac = () =>
  typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform ?? "");

/** The view a key press opens: Command or Control with Shift F for Search. */
export function viewShortcut(event: {
  key: string;
  metaKey: boolean;
  ctrlKey: boolean;
  shiftKey: boolean;
  altKey: boolean;
  isComposing?: boolean;
}): SessionView | undefined {
  if (!(event.metaKey || event.ctrlKey) || event.altKey || event.isComposing) return undefined;
  if (event.shiftKey && event.key.toLowerCase() === "f") return "search";
  return undefined;
}

/** Opens a view on its shortcut while the session's page is open. */
export function useViewShortcuts(
  open: (view: SessionView) => void,
  allowed: (view: SessionView) => boolean,
) {
  const latest = useRef({ open, allowed });
  latest.current = { open, allowed };
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      const view = viewShortcut(event);
      if (!view || !latest.current.allowed(view)) return;
      event.preventDefault();
      latest.current.open(view);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);
}

const focus =
  "rounded-aui text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-aui-accent aria-pressed:bg-aui-hover aria-pressed:text-aui-fg";
const headerIcon = `inline-flex size-9 shrink-0 pointer-coarse:size-11 items-center justify-center ${focus}`;

/**
 * The header's view controls, in a fixed order: Details, then Terminal,
 * Changes, Browser and Files, then More Views with the rest. Each is pressed
 * while its view is open, and pressing it again closes the pane. On a phone
 * only Details and More Views show, and the menu holds every view.
 */
export function SessionViews({
  active,
  progress,
  onOpen,
  onClose,
  hidden = [],
  disabled = {},
}: {
  /** Views that cannot open here, with why: said on hover and read out, and the control does nothing. */
  disabled?: Partial<Record<SessionView, string>>;
  /** The view open beside the thread, if one of these is. */
  active?: SessionView;
  /** Calls running or waiting on a person, shown on Details; highlighted while working. */
  progress?: { open: number; working: boolean };
  onOpen: (view: SessionView) => void;
  onClose: () => void;
  /** Views the reader cannot use here, left out. */
  hidden?: readonly SessionView[];
}) {
  const shows = (v: SessionView) => !hidden.includes(v);
  // A control that cannot open says why when pressed, as a touch screen has no hover.
  const [said, setSaid] = useState<string>();
  useEffect(() => {
    if (!said) return;
    const t = setTimeout(() => setSaid(undefined), 6000);
    return () => clearTimeout(t);
  }, [said]);
  const toggle = (v: SessionView) => {
    const off = disabled[v];
    if (off) return setSaid(off);
    setSaid(undefined);
    return active === v ? onClose() : onOpen(v);
  };
  const mac = onMac();
  return (
    // biome-ignore lint/a11y/useSemanticElements: a fieldset would restyle the header; the group names the controls
    <div role="group" aria-label={words.views} className="relative flex items-center gap-1">
      <p
        role="status"
        data-testid="view-unavailable"
        className={
          said
            ? "absolute top-full right-0 z-30 mt-2 w-72 max-w-[calc(100vw-2rem)] rounded-aui border border-aui-border bg-aui-bg px-3 py-2 text-aui-sm text-aui-fg shadow-lg"
            : "sr-only"
        }
      >
        {said}
      </p>
      <button
        type="button"
        data-session-details
        data-working={progress?.working ? "" : undefined}
        className={`${headerIcon} gap-0.5 ${progress?.working ? "text-aui-accent" : ""}`}
        aria-label={defaultCopy.details.open(progress?.open ?? 0)}
        title={defaultCopy.details.open(progress?.open ?? 0)}
        aria-pressed={active === "details"}
        onClick={() => toggle("details")}
      >
        <ListChecksIcon size={18} />
        {!!progress?.open && <span className="text-aui-xs tabular-nums">{progress.open}</span>}
      </button>
      {PRIMARY_VIEWS.filter(shows).map((v) => {
        const Mark = icon[v];
        return (
          <button
            key={v}
            type="button"
            data-session-view={v}
            className={`${headerIcon} max-md:hidden aria-disabled:cursor-not-allowed aria-disabled:opacity-40 aria-disabled:hover:bg-transparent`}
            aria-label={words.view[v]}
            title={disabled[v] ?? words.view[v]}
            aria-description={disabled[v]}
            aria-disabled={disabled[v] ? true : undefined}
            aria-pressed={active === v}
            onClick={() => toggle(v)}
          >
            <Mark size={18} />
          </button>
        );
      })}
      <Popover
        label={words.more}
        popup="menu"
        align="right"
        narrow
        className={headerIcon}
        button={<EllipsisIcon size={18} />}
      >
        {(close) => (
          <div role="menu" aria-label={words.more} className="flex min-w-52 flex-col">
            {[...PRIMARY_VIEWS, ...SECONDARY_VIEWS].filter(shows).map((v) => {
              const Mark = icon[v];
              const primary = (PRIMARY_VIEWS as readonly string[]).includes(v);
              const keys = v === "search" ? words.shortcut[v][mac ? "mac" : "other"] : undefined;
              const off = disabled[v];
              const body = (
                <>
                  <Mark size={16} />
                  <span className="flex min-w-0 flex-1 flex-col">
                    <span>{words.view[v]}</span>
                    {off && <span className="text-aui-muted text-aui-xs">{off}</span>}
                  </span>
                  {keys && (
                    <kbd className="font-sans text-aui-muted text-aui-xs" aria-hidden>
                      {keys}
                    </kbd>
                  )}
                </>
              );
              const cls = `${menuRow()} ${primary ? "md:hidden" : ""}`;
              return (
                <button
                  key={v}
                  type="button"
                  role="menuitem"
                  data-session-view={v}
                  aria-keyshortcuts={v === "search" ? "Meta+Shift+F Control+Shift+F" : undefined}
                  className={`${cls} aria-disabled:opacity-60`}
                  aria-disabled={off ? true : undefined}
                  onClick={() => {
                    close();
                    if (off) return setSaid(off);
                    onOpen(v);
                  }}
                >
                  {body}
                </button>
              );
            })}
          </div>
        )}
      </Popover>
    </div>
  );
}

/** Whether a session's browser can be acted on (`GET /api/sessions/{id}/browser`). */
export interface BrowserAvailability {
  available: boolean;
  providers?: string[];
  reason?: string;
}

/**
 * Which of the header's views cannot open for this session, and why: the
 * Terminal where the deployment runs tools on the server or the sandbox
 * cannot be reached, and the Browser where the session's agent has none.
 * Read when the session's page opens.
 */
export function useUnavailableViews(
  thread: string,
  isSession: boolean,
  api: {
    sandboxes: (thread: string) => Promise<{ session: { isolated: boolean; commands: boolean } }>;
    browser: (thread: string) => Promise<BrowserAvailability>;
  } = {
    sandboxes: workspaceFiles.sandboxes,
    browser: (id) =>
      call<BrowserAvailability>("GET", `/api/sessions/${encodeURIComponent(id)}/browser`),
  },
): Partial<Record<SessionView, string>> {
  const [off, setOff] = useState<Partial<Record<SessionView, string>>>({});
  const u = words.unavailable;
  // biome-ignore lint/correctness/useExhaustiveDependencies: read once per session.
  useEffect(() => {
    let on = true;
    setOff({});
    api.sandboxes(thread).then(
      (b) =>
        on &&
        setOff((o) => ({
          ...o,
          terminal: !b.session.isolated && !b.session.commands ? u.terminalHost : undefined,
        })),
      () => on && setOff((o) => ({ ...o, terminal: u.terminalUnreachable })),
    );
    if (!isSession) setOff((o) => ({ ...o, browser: u.browser }));
    else
      api.browser(thread).then(
        (b) => on && setOff((o) => ({ ...o, browser: b.available ? undefined : u.browser })),
        (e) =>
          on &&
          setOff((o) => ({
            ...o,
            browser: e instanceof ApiError && e.status === 404 ? u.browser : u.browserUnknown,
          })),
      );
    return () => {
      on = false;
    };
  }, [thread, isSession]);
  return off;
}
