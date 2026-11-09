import { type ReactNode, useEffect, useRef } from "react";
import { LatestButton, useLatest } from "./Latest.tsx";
import { TurnRail, type TurnRailEntry } from "./TurnRail";

/**
 * Scrolling message list that sticks to the bottom while content streams in,
 * unless the reader has scrolled up; then the LatestButton appears, and
 * shows whether an answer is still being written below (`busy`).
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
  const { scroller, content, look, onScroll, jump, release } = useLatest({
    working: busy,
    follow: true,
    startAtEnd: !focus,
  });
  const revealed = useRef<string | undefined>(undefined);

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
    release();
    root.scrollTo({
      top:
        root.scrollTop +
        target.getBoundingClientRect().top -
        root.getBoundingClientRect().top -
        root.clientHeight / 3,
      behavior: "auto",
    });
  }, [focus, turns, scroller, release]);

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
      <LatestButton look={look} onJump={jump} scroller={scroller} />
    </div>
  );
}
