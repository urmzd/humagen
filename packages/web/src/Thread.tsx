import { ArrowDownIcon } from "@humagen/render-web";
import { type ReactNode, useCallback, useEffect, useRef, useState } from "react";
import { TurnRail, type TurnRailEntry } from "./TurnRail";

const NEAR_BOTTOM_PX = 48;

/**
 * Scrolling message list that sticks to the bottom while content streams in,
 * unless the reader has scrolled up; then a "Jump to latest" button appears.
 *
 * role="log" with live announcements off: AgentMessage announces state
 * changes itself, so screen readers don't hear every token.
 */
/** The fewest turns a conversation has before its turn rail shows. */
export const RAIL_MIN_TURNS = 6;

export function Thread({
  children,
  label = "Conversation",
  className,
  column,
  turns,
  busy = false,
  focus,
}: {
  /** An addressed message to reveal without scrolling the surrounding page. */
  focus?: string;
  children: ReactNode;
  label?: string;
  className?: string;
  /**
   * The column the messages read in, inside a list that spans its whole
   * pane, so the wave sits at the pane's far left and the scrollbar at its
   * far right, both clear of the messages.
   */
  column?: string;
  /** Turns for the left-edge rail (see railEntry). Omit to hide the rail. */
  turns?: TurnRailEntry[];
  /** Shows a thin indeterminate bar at the top while a turn is in flight. */
  busy?: boolean;
}) {
  const scroller = useRef<HTMLDivElement>(null);
  const content = useRef<HTMLDivElement>(null);
  const stuck = useRef(!focus);
  const [atBottom, setAtBottom] = useState(true);
  const revealed = useRef<string | undefined>(undefined);

  const toBottom = useCallback((smooth: boolean) => {
    const el = scroller.current;
    if (!el) return;
    const reduce = window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;
    el.scrollTo({ top: el.scrollHeight, behavior: smooth && !reduce ? "smooth" : "auto" });
  }, []);

  useEffect(() => {
    if (!focus) {
      revealed.current = undefined;
      return;
    }
    if (revealed.current === focus || (turns && !turns.some((turn) => turn.id === focus))) return;
    const root = scroller.current;
    const target = root?.querySelector<HTMLElement>(`[data-turn-id="${CSS.escape(focus)}"]`);
    if (!root || !target) return;
    revealed.current = focus;
    stuck.current = false;
    setAtBottom(false);
    root.scrollTo({
      top:
        root.scrollTop +
        target.getBoundingClientRect().top -
        root.getBoundingClientRect().top -
        root.clientHeight / 3,
      behavior: "auto",
    });
  }, [focus, turns]);

  useEffect(() => {
    const el = content.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(() => {
      if (stuck.current) toBottom(false);
    });
    observer.observe(el);
    // The list itself shrinking (the keyboard opening on a phone) keeps the latest in view too.
    if (scroller.current) observer.observe(scroller.current);
    return () => observer.disconnect();
  }, [toBottom]);

  const onScroll = () => {
    const el = scroller.current;
    if (!el) return;
    const near = el.scrollHeight - el.scrollTop - el.clientHeight < NEAR_BOTTOM_PX;
    stuck.current = near;
    setAtBottom(near);
  };

  // The rail is a way around a long conversation; a short one needs none.
  const wave = turns && turns.length >= RAIL_MIN_TURNS ? turns : null;
  return (
    <div className={`relative flex min-h-0 flex-col ${className ?? ""}`}>
      {busy && <div aria-hidden className="aui-busy-bar absolute inset-x-0 top-0 z-10" />}
      {wave && <TurnRail turns={wave} scroller={scroller} />}
      <div
        ref={scroller}
        role="log"
        aria-label={label}
        aria-live="off"
        // biome-ignore lint/a11y/noNoninteractiveTabindex: scrollable region must be keyboard reachable
        tabIndex={0}
        onScroll={onScroll}
        className={`aui-scroll min-h-0 flex-1 overflow-y-auto overscroll-contain outline-none focus-visible:ring-2 focus-visible:ring-aui-accent focus-visible:ring-inset [scrollbar-gutter:stable] ${wave ? "md:px-8" : ""}`}
      >
        <div
          ref={content}
          className={`aui-thread-turns flex flex-col gap-5 pt-4 pb-12 ${column ?? "px-1"}`}
        >
          {children}
        </div>
      </div>
      {!atBottom && (
        <>
          <div
            aria-hidden="true"
            className="pointer-events-none absolute inset-x-0 bottom-0 h-16 bg-gradient-to-t from-aui-bg via-aui-bg/90 to-transparent"
          />
          <button
            type="button"
            data-layer="thread-jump"
            onClick={() => {
              stuck.current = true;
              toBottom(true);
            }}
            className="aui-pop-in absolute bottom-3 left-1/2 z-10 inline-flex h-7 -translate-x-1/2 items-center gap-1.5 rounded-full border border-aui-border bg-aui-bg px-3 text-aui-fg text-aui-xs shadow-md transition-colors hover:bg-aui-surface focus-visible:outline-2 focus-visible:outline-aui-accent"
          >
            <ArrowDownIcon size={13} />
            Jump to latest
          </button>
        </>
      )}
    </div>
  );
}
