import { defaultCopy } from "@humagen/components/copy";
import { ArrowDownIcon } from "@humagen/render-web";
import { type RefObject, useCallback, useEffect, useReducer, useRef } from "react";
import { type LatestLook, latestLook, latestReduce, latestStart } from "./latestState.ts";

const NEAR_END_PX = 48;

/**
 * Measures a scrolling list that grows at its end. `scroller` goes on the
 * element that scrolls and `content` on the one inside it that grows.
 * With `follow`, the end stays on view while the reader is at it. `look` is
 * what the LatestButton shows, and `jump` goes to the end.
 */
export function useLatest({
  working,
  follow,
  startAtEnd = true,
}: {
  working: boolean;
  follow: boolean;
  /** False for a list opened on something earlier in it. */
  startAtEnd?: boolean;
}): {
  scroller: RefObject<HTMLDivElement | null>;
  content: RefObject<HTMLDivElement | null>;
  look: LatestLook | null;
  onScroll: () => void;
  jump: () => void;
  /** Leave the end, for a list that reveals something earlier in it. */
  release: () => void;
  /** Whether the end is kept on view as the list grows. */
  stuck: RefObject<boolean>;
} {
  const scroller = useRef<HTMLDivElement>(null);
  const content = useRef<HTMLDivElement>(null);
  const stuck = useRef(follow && startAtEnd);
  const [state, dispatch] = useReducer(latestReduce, latestStart);
  const live = useRef(working);
  live.current = working;

  const toEnd = useCallback((smooth: boolean) => {
    const el = scroller.current;
    if (!el) return;
    const reduce = window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;
    el.scrollTo({ top: el.scrollHeight, behavior: smooth && !reduce ? "smooth" : "auto" });
  }, []);

  const measure = useCallback(() => {
    const el = scroller.current;
    if (!el) return true;
    const atEnd = el.scrollHeight - el.scrollTop - el.clientHeight < NEAR_END_PX;
    if (follow) stuck.current = atEnd;
    dispatch({ type: "moved", atEnd });
    return atEnd;
  }, [follow]);

  useEffect(() => {
    const el = content.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    let height = el.scrollHeight;
    let count = el.childElementCount;
    const observer = new ResizeObserver(() => {
      const taller = el.scrollHeight > height;
      const more = el.childElementCount > count;
      height = el.scrollHeight;
      count = el.childElementCount;
      if (follow && stuck.current) {
        toEnd(false);
        return;
      }
      // A reader opening something above also makes the list taller: only
      // an answer being written, or a new entry, counts as arriving.
      if (!measure() && taller && (live.current || more)) dispatch({ type: "grew" });
    });
    observer.observe(el);
    // The list itself shrinking (the keyboard opening on a phone) keeps the latest in view too.
    if (scroller.current) observer.observe(scroller.current);
    return () => observer.disconnect();
  }, [toEnd, measure, follow]);

  const jump = useCallback(() => {
    if (follow) stuck.current = true;
    toEnd(true);
  }, [follow, toEnd]);
  const release = useCallback(() => {
    stuck.current = false;
    dispatch({ type: "moved", atEnd: false });
  }, []);

  return {
    scroller,
    content,
    look: latestLook(state, working),
    onScroll: measure,
    jump,
    release,
    stuck,
  };
}

const words = defaultCopy.latest;

/**
 * The control that returns to the end of a list, over its bottom edge. It
 * is drawn only while the end is off view. While an answer is being written
 * a ring turns around the arrow (it stands still for a reader who asked for
 * less motion); after, the arrow is plain, with a dot if something arrived
 * unseen. Its name says which. It never takes focus by appearing; used from
 * the keyboard, it hands focus to the list it scrolled.
 */
export function LatestButton({
  look,
  onJump,
  scroller,
}: {
  look: LatestLook | null;
  onJump: () => void;
  scroller: RefObject<HTMLElement | null>;
}) {
  if (!look) return null;
  const name = look === "working" ? words.working : look === "unseen" ? words.unseen : words.jump;
  return (
    <>
      <div
        aria-hidden="true"
        className="pointer-events-none absolute inset-x-0 bottom-0 h-16 bg-gradient-to-t from-aui-bg via-aui-bg/90 to-transparent"
      />
      <button
        type="button"
        data-layer="thread-jump"
        data-look={look}
        aria-label={name}
        title={name}
        onClick={(event) => {
          // The button leaves once the end is on view: focus goes to the list, not nowhere.
          if (document.activeElement === event.currentTarget)
            scroller.current?.focus({ preventScroll: true });
          onJump();
        }}
        className="aui-pop-in aui-latest absolute bottom-3 left-1/2 z-10 inline-flex size-8 -translate-x-1/2 items-center justify-center rounded-full border border-aui-border bg-aui-bg text-aui-fg shadow-md transition-colors hover:bg-aui-surface focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-aui-accent"
      >
        <ArrowDownIcon size={14} />
        {look === "unseen" && <span aria-hidden="true" className="aui-latest-dot" />}
      </button>
    </>
  );
}
