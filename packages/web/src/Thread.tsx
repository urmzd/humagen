import { ArrowDownIcon } from "@vultus/render-web";
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
export function Thread({
  children,
  label = "Conversation",
  className,
  turns,
  busy = false,
}: {
  children: ReactNode;
  label?: string;
  className?: string;
  /** Turns for the left-edge rail (see railEntry). Omit to hide the rail. */
  turns?: TurnRailEntry[];
  /** Shows a thin indeterminate bar at the top while a turn is in flight. */
  busy?: boolean;
}) {
  const scroller = useRef<HTMLDivElement>(null);
  const content = useRef<HTMLDivElement>(null);
  const stuck = useRef(true);
  const [atBottom, setAtBottom] = useState(true);

  const toBottom = useCallback((smooth: boolean) => {
    const el = scroller.current;
    if (!el) return;
    const reduce = window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;
    el.scrollTo({ top: el.scrollHeight, behavior: smooth && !reduce ? "smooth" : "auto" });
  }, []);

  useEffect(() => {
    const el = content.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(() => {
      if (stuck.current) toBottom(false);
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, [toBottom]);

  const onScroll = () => {
    const el = scroller.current;
    if (!el) return;
    const near = el.scrollHeight - el.scrollTop - el.clientHeight < NEAR_BOTTOM_PX;
    stuck.current = near;
    setAtBottom(near);
  };

  return (
    <div className={`relative flex min-h-0 flex-col ${className ?? ""}`}>
      {busy && <div aria-hidden className="aui-busy-bar absolute inset-x-0 top-0 z-10" />}
      {turns && turns.length > 1 && <TurnRail turns={turns} scroller={scroller} />}
      <div
        ref={scroller}
        role="log"
        aria-label={label}
        aria-live="off"
        // biome-ignore lint/a11y/noNoninteractiveTabindex: scrollable region must be keyboard reachable
        tabIndex={0}
        onScroll={onScroll}
        className="aui-scroll min-h-0 flex-1 overflow-y-auto overscroll-contain outline-none focus-visible:ring-2 focus-visible:ring-aui-accent focus-visible:ring-inset [scrollbar-gutter:stable]"
      >
        <div
          ref={content}
          className={`flex flex-col gap-5 py-4 pr-1 ${turns && turns.length > 1 ? "pl-1 md:pl-9" : "pl-1"}`}
        >
          {children}
        </div>
      </div>
      {!atBottom && (
        <button
          type="button"
          onClick={() => {
            stuck.current = true;
            toBottom(true);
          }}
          className="aui-pop-in aui-glass absolute bottom-3 left-1/2 inline-flex h-7 -translate-x-1/2 items-center gap-1.5 rounded-full px-3 text-aui-fg text-aui-xs transition-colors hover:bg-aui-surface focus-visible:outline-2 focus-visible:outline-aui-accent"
        >
          <ArrowDownIcon size={13} />
          Jump to latest
        </button>
      )}
    </div>
  );
}
