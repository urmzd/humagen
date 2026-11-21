import {
  forwardRef,
  type ReactNode,
  type RefObject,
  useEffect,
  useLayoutEffect,
  useRef,
} from "react";
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

/** The Jump to latest control and the fade under it (`h-16`), plus 16px of air. */
const JUMP_CLEAR_PX = 80;

export function Thread({
  children,
  label = "Conversation",
  className,
  column,
  turns,
  busy = false,
  focus,
  initialApproval,
  footer,
}: {
  /**
   * What floats over the bottom of the conversation: its composer. The
   * messages are one surface that scrolls underneath it, behind a soft fade,
   * and the list keeps enough room at its end for the last message to clear
   * it as it grows (`--aui-thread-dock-h`).
   */
  footer?: ReactNode;
  /** An addressed message to reveal without scrolling the surrounding page. */
  focus?: string;
  /** Latest actionable run on first open; an explicit message link takes precedence. */
  initialApproval?: { run: string; question: string };
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
  const { scroller, content, look, onScroll, jump, release, stuck } = useLatest({
    working: busy,
    follow: true,
    startAtEnd: !focus,
  });
  const revealed = useRef<string | undefined>(undefined);
  const positioned = useRef(false);
  const root = useRef<HTMLDivElement>(null);
  const dock = useRef<HTMLDivElement>(null);
  const docked = footer !== undefined && footer !== null && footer !== false;

  useComposerDock({
    enabled: docked,
    host: root,
    dock,
    scroller,
    // A reader at the end stays there as the composer grows.
    onMeasure: (list) => {
      if (stuck.current) list.scrollTop = list.scrollHeight;
    },
  });

  useEffect(() => {
    if (positioned.current) return;
    positioned.current = true;
    if (focus || !initialApproval) return;
    const root = scroller.current;
    const run = root?.querySelector<HTMLElement>(
      `[data-turn-id="${CSS.escape(initialApproval.run)}"]`,
    );
    // One pending approval has its own action row; several share the run's batch row.
    const cards = run?.querySelectorAll<HTMLElement>(
      '[data-aui-block="approval"], [data-aui-block="batch"]',
    );
    const card = Array.from(cards ?? []).find((item) => {
      const id = item.dataset.auiBlockId ?? "";
      return (
        id.startsWith(`approval:${initialApproval.question}:r`) ||
        id === `batch:${initialApproval.run}:actions`
      );
    });
    if (!root || !card) return;
    const end = root.scrollHeight - root.clientHeight;
    const view = root.getBoundingClientRect();
    const box = card.getBoundingClientRect();
    // The list's end keeps room for the composer, so a row that starts on view
    // there is clear of it: stay at the end and keep following.
    if (root.scrollTop + box.top - view.top >= end) {
      root.scrollTo({ top: end, behavior: "auto" });
      return;
    }
    // Earlier in the list, the row stops clear of the composer that floats over
    // the list's end and of the Jump to latest control drawn above it.
    const edge = Math.min(view.bottom, dock.current?.getBoundingClientRect().top ?? view.bottom);
    root.scrollTo({ top: root.scrollTop + box.bottom - edge + JUMP_CLEAR_PX, behavior: "auto" });
    if (root.scrollHeight - root.scrollTop - root.clientHeight >= 48) release();
  }, [focus, initialApproval, scroller, release]);

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
    <div
      ref={root}
      data-docked={docked || undefined}
      className={`relative flex min-h-0 flex-col ${className ?? ""}`}
    >
      {busy && (
        <div aria-hidden className="aui-busy-bar absolute inset-x-0 top-0 z-(--aui-z-sticky)" />
      )}
      {wave && <TurnRail turns={wave} scroller={scroller} />}
      <div
        ref={scroller}
        role="log"
        aria-label={label}
        aria-live="off"
        // biome-ignore lint/a11y/noNoninteractiveTabindex: scrollable region must be keyboard reachable
        tabIndex={0}
        onScroll={onScroll}
        // Reached by keyboard to scroll it, the log shows a thin neutral line along
        // its top edge: the conversation is one surface under the composer, so a
        // ring round the whole log would box the page in again.
        className={`aui-scroll min-h-0 flex-1 overflow-y-auto overscroll-contain outline-none focus-visible:shadow-[inset_0_2px_0_0_color-mix(in_oklab,var(--aui-fg)_45%,transparent)] [scrollbar-gutter:stable] ${wave ? "md:px-8" : ""}`}
      >
        <div
          ref={content}
          className={`aui-thread-turns flex flex-col gap-5 pt-4 ${docked ? "pb-[calc(var(--aui-thread-dock-h,8rem)+1.5rem)]" : "pb-12"} ${column ?? "px-1"}`}
        >
          {children}
        </div>
      </div>
      {docked && <ComposerDock ref={dock}>{footer}</ComposerDock>}
      <div
        className="pointer-events-none absolute inset-x-0 top-0 z-(--aui-z-composer)"
        style={{ bottom: docked ? "var(--aui-thread-dock-h, 0px)" : 0 }}
      >
        <LatestButton look={look} onJump={jump} scroller={scroller} fade={!docked} />
      </div>
    </div>
  );
}

/**
 * Keeps a floating composer's height, and the width of the scrollbar it must
 * not cover, as variables on its host: `--aui-thread-dock-h` and
 * `--aui-thread-gutter`. Whatever scrolls under the composer pads its end by
 * the first, so its last line clears the composer however tall it grows.
 */
export function useComposerDock({
  enabled,
  host,
  dock,
  scroller,
  onMeasure,
}: {
  enabled: boolean;
  host: RefObject<HTMLElement | null>;
  dock: RefObject<HTMLElement | null>;
  scroller: RefObject<HTMLElement | null>;
  /** Called after each measure with the scrolling list, to keep a reader where they were. */
  onMeasure?: (list: HTMLElement) => void;
}) {
  const after = useRef(onMeasure);
  after.current = onMeasure;
  useLayoutEffect(() => {
    const el = dock.current;
    const root = host.current;
    if (!enabled || !el || !root) return;
    const measure = () => {
      const list = scroller.current;
      root.style.setProperty("--aui-thread-dock-h", `${Math.ceil(el.offsetHeight)}px`);
      if (list) {
        root.style.setProperty("--aui-thread-gutter", `${list.offsetWidth - list.clientWidth}px`);
        after.current?.(list);
      }
    };
    measure();
    if (typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(measure);
    observer.observe(el);
    return () => observer.disconnect();
  }, [enabled, host, dock, scroller]);
}

/**
 * The composer floating over the bottom of a surface that scrolls under it:
 * the conversation's, and a project's page. Words pass under it through a
 * fade, not against a hard edge, and it stays clear of the scrollbar so the
 * bar stays reachable. Its host is `relative` and measures it with
 * `useComposerDock`.
 */
export const ComposerDock = forwardRef<HTMLDivElement, { children: ReactNode }>(
  function ComposerDock({ children }, ref) {
    return (
      <div
        ref={ref}
        data-layer="composer-dock"
        className="pointer-events-none absolute bottom-0 left-0 z-(--aui-z-composer) right-[var(--aui-thread-gutter,0px)]"
      >
        <div
          aria-hidden
          className="h-8 bg-gradient-to-t from-aui-bg to-transparent"
          data-layer="composer-fade"
        />
        <div className="pointer-events-auto bg-aui-bg">{children}</div>
      </div>
    );
  },
);
