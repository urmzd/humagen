import { ArrowDownIcon } from "@agent-ui/render-web";
import { type ReactNode, useCallback, useEffect, useRef, useState } from "react";

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
}: {
  children: ReactNode;
  label?: string;
  className?: string;
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
      <div
        ref={scroller}
        role="log"
        aria-label={label}
        aria-live="off"
        // biome-ignore lint/a11y/noNoninteractiveTabindex: scrollable region must be keyboard reachable
        tabIndex={0}
        onScroll={onScroll}
        className="min-h-0 flex-1 overflow-y-auto overscroll-contain outline-none focus-visible:ring-2 focus-visible:ring-aui-accent focus-visible:ring-inset [scrollbar-gutter:stable]"
      >
        <div ref={content} className="flex flex-col gap-6 px-1 py-4">
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
          className="aui-pop-in absolute bottom-3 left-1/2 inline-flex h-8 -translate-x-1/2 items-center gap-1.5 rounded-full border border-aui-border bg-aui-pop px-3 text-aui-fg text-aui-xs shadow-aui-md transition-colors hover:bg-aui-surface focus-visible:outline-2 focus-visible:outline-aui-accent"
        >
          <ArrowDownIcon size={13} />
          Jump to latest
        </button>
      )}
    </div>
  );
}
