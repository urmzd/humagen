import { defaultCopy } from "@humagen/components";
import {
  DiffIcon,
  EllipsisIcon,
  FolderIcon,
  HistoryIcon,
  LayersIcon,
  ListChecksIcon,
  MessageSquareIcon,
  MonitorIcon,
  SearchIcon,
  TerminalIcon,
  ZapIcon,
} from "@humagen/render-web";
import { type ComponentType, useEffect, useRef } from "react";
import { href } from "./route.ts";
import { menuRow, Popover } from "./ui.tsx";

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
  | "sidechat"
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
  sidechat: MessageSquareIcon,
  search: SearchIcon,
  background: ZapIcon,
  progress: HistoryIcon,
};

/** The views with a button of their own in the header, after Details, on a wide header. */
export const PRIMARY_VIEWS = ["terminal", "changes", "browser", "files"] as const;
/** The views in the More Views menu. On a phone the primary ones join them there. */
export const SECONDARY_VIEWS = [
  "artifacts",
  "sidechat",
  "search",
  "background",
  "progress",
] as const;

/** Whether the keys are a Mac's, for the shortcut a row prints. */
const onMac = () =>
  typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform ?? "");

/** The view a key press opens: Command or Control with ; for Side Chat, and with Shift F for Search. */
export function viewShortcut(event: {
  key: string;
  metaKey: boolean;
  ctrlKey: boolean;
  shiftKey: boolean;
  altKey: boolean;
  isComposing?: boolean;
}): SessionView | undefined {
  if (!(event.metaKey || event.ctrlKey) || event.altKey || event.isComposing) return undefined;
  if (event.key === ";" && !event.shiftKey) return "sidechat";
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
}: {
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
  const toggle = (v: SessionView) => (active === v ? onClose() : onOpen(v));
  const mac = onMac();
  return (
    // biome-ignore lint/a11y/useSemanticElements: a fieldset would restyle the header; the group names the controls
    <div role="group" aria-label={words.views} className="flex items-center gap-1">
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
            className={`${headerIcon} max-md:hidden`}
            aria-label={words.view[v]}
            title={words.view[v]}
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
              const keys =
                v === "sidechat" || v === "search"
                  ? words.shortcut[v][mac ? "mac" : "other"]
                  : undefined;
              const body = (
                <>
                  <Mark size={16} />
                  <span className="flex-1">{words.view[v]}</span>
                  {keys && (
                    <kbd className="font-sans text-aui-muted text-aui-xs" aria-hidden>
                      {keys}
                    </kbd>
                  )}
                </>
              );
              const cls = `${menuRow()} ${primary ? "md:hidden" : ""}`;
              return v === "background" ? (
                <a
                  key={v}
                  role="menuitem"
                  href={href({ page: "automations", tab: "background" })}
                  data-session-view={v}
                  className={cls}
                  onClick={close}
                >
                  {body}
                </a>
              ) : (
                <button
                  key={v}
                  type="button"
                  role="menuitem"
                  data-session-view={v}
                  aria-keyshortcuts={
                    v === "sidechat"
                      ? "Meta+; Control+;"
                      : v === "search"
                        ? "Meta+Shift+F Control+Shift+F"
                        : undefined
                  }
                  className={cls}
                  onClick={() => {
                    close();
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
