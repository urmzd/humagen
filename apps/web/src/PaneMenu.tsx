import { defaultCopy } from "@humagen/components";
import { ChevronRightIcon } from "@humagen/render-web";
import {
  Fragment,
  type HTMLAttributes,
  type KeyboardEvent,
  type ReactNode,
  type Ref,
  type RefObject,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";

/*
 * One menu of rows, any of which may open a pane in the menu's place. The
 * composer's + menu and the usage sheet are both this; it knows rows, panes
 * and keys, never what they are about.
 *
 * The keys are one ladder. Up and Down move between the rows: in a pane that
 * is Back, then the pane's first field, then its choices and its rows. Right
 * opens a root row's pane. Left goes back a pane, except in a field or on a
 * radio, where it keeps its own meaning. Escape goes back exactly one step:
 * a pane first, then the menu. Going back puts focus on the row the pane was
 * opened from.
 *
 * Inside a pane, mark each control the arrow keys stop at with
 * `data-pane-item`: "choice" for a choice listed under the pane's field,
 * "action" for any other.
 */

/** What a row opens in the menu's place, under Back and its title. */
export interface PaneMenuPane {
  title?: string;
  body?: ReactNode;
  /** Rows under the body, after a rule. */
  rows?: PaneMenuRow[];
  /** The content takes the keys itself, as a form does: only Escape, Left outside a field and Back stay the menu's. */
  ownKeys?: boolean;
}

export interface PaneMenuRow {
  /** Names the row among its siblings: focus comes back to it by this. */
  id: string;
  label: string;
  icon?: ReactNode;
  /** The current value, in muted text at the right. */
  value?: string;
  /** Whether a chevron ends the row. Left out, a row that opens a pane has one. */
  chevron?: boolean;
  pane?: PaneMenuPane;
  /** What a row without a pane does. */
  onSelect?: () => void;
  /** A line under the row, such as why it cannot change. */
  note?: string;
}

/** One row of a pane menu, and of the choices a pane lists. */
export const paneMenuRow =
  "flex min-h-11 w-full min-w-0 items-center gap-2.5 rounded-aui px-2.5 py-2 text-left text-aui-sm text-aui-fg hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent aria-checked:bg-aui-hover disabled:cursor-default disabled:text-aui-muted disabled:hover:bg-transparent";

const ITEM = "[data-pane-item]:not(:disabled)";
const SHEET =
  "humagen-glass-pop aui-pop-in rounded-aui-lg text-left outline-none max-sm:fixed max-sm:inset-x-0 max-sm:bottom-0 max-sm:z-(--aui-z-sheet) max-sm:rounded-b-none max-sm:pb-[max(1rem,env(safe-area-inset-bottom))] sm:absolute sm:z-(--aui-z-popover)";
const PANE =
  "min-w-0 motion-safe:animate-aui-pane data-[direction=back]:motion-safe:animate-aui-pane-back";

export function PaneMenu({
  label,
  rows = [],
  children,
  onClose,
  onLeave,
  onKey,
  sheet = false,
  anchor,
  className = "",
  ref,
  ...rest
}: {
  /** The menu's name. */
  label: string;
  rows?: PaneMenuRow[];
  /** What the root shows under its rows. */
  children?: ReactNode;
  /** Escape at the root. */
  onClose: () => void;
  /** A key the menu does not take, with the row whose pane is open (null at the root). */
  onKey?: (event: KeyboardEvent<HTMLDivElement>, pane: string | null) => void;
  /**
   * Below the sm breakpoint a sheet along the bottom over a dimmed page;
   * from sm up a panel, placed against its control by `className`. Without
   * it the host places the menu itself.
   */
  sheet?: boolean;
  /** The control that opens a sheet: a press on it is not a press outside. */
  anchor?: RefObject<HTMLElement | null>;
  /** A press outside a sheet: put it away and leave focus where the press put it. */
  onLeave?: () => void;
  ref?: Ref<HTMLDivElement>;
} & Omit<
  HTMLAttributes<HTMLDivElement>,
  "role" | "aria-label" | "tabIndex" | "onKeyDown" | "children"
>) {
  const [path, setPath] = useState<string[]>([]);
  // A sheet arrives whole; a menu on the bar slides its first rows in.
  const [direction, setDirection] = useState<"forward" | "back" | undefined>(
    sheet ? undefined : "forward",
  );
  const panel = useRef<HTMLDivElement | null>(null);
  const backButton = useRef<HTMLButtonElement>(null);
  const returnTo = useRef<string | undefined>(undefined);
  const setPanel = useCallback(
    (node: HTMLDivElement | null) => {
      panel.current = node;
      if (typeof ref === "function") ref(node);
      else if (ref) ref.current = node;
    },
    [ref],
  );

  // The open pane, found in the rows as they are now: a row that is gone takes its pane with it.
  let pane: PaneMenuPane | undefined;
  const at: string[] = [];
  let among = rows;
  for (const id of path) {
    const next = among.find((row) => row.id === id)?.pane;
    if (!next) break;
    pane = next;
    among = next.rows ?? [];
    at.push(id);
  }
  const key = at.join("/");

  const open = (id: string) => {
    setDirection("forward");
    returnTo.current = undefined;
    setPath([...at, id]);
  };
  const back = () => {
    if (at.length === 0) return onClose();
    setDirection("back");
    returnTo.current = at[at.length - 1];
    setPath(at.slice(0, -1));
  };

  useEffect(() => {
    const from = returnTo.current;
    returnTo.current = undefined;
    const menu = panel.current;
    if (!menu) return;
    const row =
      from === undefined
        ? undefined
        : [...menu.querySelectorAll<HTMLElement>("[data-pane-row]")].find(
            (item) => item.dataset.paneRow === from,
          );
    if (row) row.focus();
    else if (key === "") (menu.querySelector<HTMLElement>(ITEM) ?? menu).focus();
    else (menu.querySelector<HTMLElement>("input") ?? backButton.current)?.focus();
  }, [key]);

  useEffect(() => {
    if (!sheet) return;
    const down = (event: PointerEvent) => {
      const target = event.target as Node;
      if (!panel.current?.contains(target) && !anchor?.current?.contains(target))
        (onLeave ?? onClose)();
    };
    document.addEventListener("pointerdown", down);
    return () => document.removeEventListener("pointerdown", down);
  }, [sheet, anchor, onLeave, onClose]);

  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.nativeEvent.isComposing || event.defaultPrevented) return;
    const pressed = event.key;
    if (pressed === "Escape") {
      event.preventDefault();
      event.stopPropagation();
      back();
      return;
    }
    const target = event.target as HTMLElement;
    const editing =
      target instanceof HTMLInputElement ||
      target instanceof HTMLTextAreaElement ||
      target.isContentEditable;
    if (pressed === "ArrowLeft" && pane && !editing && target.getAttribute("role") !== "radio") {
      event.preventDefault();
      back();
      return;
    }
    const field = panel.current?.querySelector<HTMLInputElement>("input");
    if (pane?.ownKeys) {
      if (pressed === "ArrowDown" && target === backButton.current) {
        event.preventDefault();
        field?.focus();
      }
      return;
    }
    if (
      pressed === "ArrowRight" &&
      target.dataset.paneRow &&
      target.hasAttribute("aria-haspopup")
    ) {
      event.preventDefault();
      target.click();
      return;
    }
    if (pressed === "ArrowDown" || pressed === "ArrowUp") {
      const items = [...(panel.current?.querySelectorAll<HTMLElement>(ITEM) ?? [])];
      if (items.length === 0) return;
      event.preventDefault();
      const down = pressed === "ArrowDown";
      const choice = items.find((item) => item.dataset.paneItem === "choice");
      if (field && document.activeElement === field)
        (down ? (choice ?? items[0]) : backButton.current)?.focus();
      else if (field && (down ? target === backButton.current : target === choice)) field.focus();
      else {
        const i = items.indexOf(document.activeElement as HTMLElement);
        items[down ? (i + 1) % items.length : (i <= 0 ? items.length : i) - 1]?.focus();
      }
      return;
    }
    onKey?.(event, at[at.length - 1] ?? null);
  };

  // Right opens a root row's pane; a row inside a pane opens on a press or Enter.
  const list = (items: PaneMenuRow[], root: boolean) =>
    items.map((row) => (
      <Fragment key={row.id}>
        <button
          type="button"
          data-pane-item="action"
          data-pane-row={row.id}
          aria-haspopup={row.pane && root ? "dialog" : undefined}
          className={paneMenuRow}
          onClick={() => (row.pane ? open(row.id) : row.onSelect?.())}
        >
          {row.icon}
          <span className="min-w-0 flex-1">{row.label}</span>
          {row.value && <span className="truncate text-aui-xs text-aui-muted">{row.value}</span>}
          {(row.chevron ?? !!row.pane) && <ChevronRightIcon size={16} className="text-aui-muted" />}
        </button>
        {row.note && <p className="px-2.5 pb-2 text-aui-xs text-aui-muted">{row.note}</p>}
      </Fragment>
    ));

  return (
    <>
      {sheet && (
        <div aria-hidden className="fixed inset-0 z-(--aui-z-sheet) bg-black/30 sm:hidden" />
      )}
      <div
        {...rest}
        ref={setPanel}
        role="dialog"
        aria-label={label}
        tabIndex={sheet ? -1 : undefined}
        onKeyDown={onKeyDown}
        className={sheet ? `${SHEET} ${className}` : className}
      >
        <div key={key} data-direction={direction} className={direction ? PANE : "min-w-0"}>
          {pane ? (
            <>
              <div className="mb-1 flex min-w-0 items-center gap-2 border-b border-aui-border pb-1">
                <button
                  ref={backButton}
                  type="button"
                  data-pane-item="action"
                  className="inline-flex min-h-11 shrink-0 items-center gap-2 rounded-aui px-2.5 py-2 text-aui-xs text-aui-muted hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
                  onClick={back}
                >
                  <ChevronRightIcon size={16} className="rotate-180" />
                  {defaultCopy.chat.picker.back}
                </button>
                {pane.title && (
                  <h2 className="min-w-0 truncate text-aui-sm font-medium">{pane.title}</h2>
                )}
              </div>
              {pane.body}
              {pane.rows && pane.rows.length > 0 && (
                <div className="mt-1 border-t border-aui-border pt-1">{list(pane.rows, false)}</div>
              )}
            </>
          ) : (
            <>
              {list(rows, true)}
              {children}
            </>
          )}
        </div>
      </div>
    </>
  );
}
