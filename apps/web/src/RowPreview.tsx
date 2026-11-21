import { defaultCopy } from "@humagen/components";
import { type ReactNode, useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import type { ThreadSummary } from "./api.ts";
import { age } from "./conversationList.ts";
import { PEEK_WIDTH, placePeek } from "./peek.ts";

/** How long a pointer rests on a row before its preview opens, and a finger holds for one. */
export const PEEK_MS = 450;
export const HOLD_MS = 500;

/** Closes the preview that is showing: one shows at a time, by pointer or by keyboard. */
let closeShown: (() => void) | undefined;

/**
 * Opens a preview of something on hover, after the pointer rests, on a long
 * press with a finger, or when keyboard focus rests on it. A long press does
 * not also click: the click that follows it is swallowed. Leaving, scrolling
 * or Escape closes it.
 */
export function usePeek() {
  const [at, setAt] = useState<DOMRect | null>(null);
  const timer = useRef<number | undefined>(undefined);
  const held = useRef(false);
  const stop = useCallback(() => window.clearTimeout(timer.current), []);
  const close = useCallback(() => {
    stop();
    setAt(null);
    if (closeShown === close) closeShown = undefined;
  }, [stop]);
  const show = useCallback(
    (el: HTMLElement) => {
      if (closeShown !== close) closeShown?.();
      closeShown = close;
      setAt(el.getBoundingClientRect());
    },
    [close],
  );
  useEffect(() => {
    if (!at) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && close();
    window.addEventListener("keydown", onKey);
    window.addEventListener("scroll", close, true);
    document.addEventListener("pointerdown", close, true);
    return () => {
      window.removeEventListener("keydown", onKey);
      window.removeEventListener("scroll", close, true);
      document.removeEventListener("pointerdown", close, true);
    };
  }, [at, close]);
  useEffect(
    () => () => {
      stop();
      if (closeShown === close) closeShown = undefined;
    },
    [stop, close],
  );
  const bind = {
    onPointerEnter: (e: React.PointerEvent<HTMLElement>) => {
      if (e.pointerType !== "mouse") return;
      const el = e.currentTarget;
      stop();
      timer.current = window.setTimeout(() => show(el), PEEK_MS);
    },
    onPointerLeave: (e: React.PointerEvent<HTMLElement>) => {
      if (e.pointerType === "mouse") close();
    },
    onFocus: (e: React.FocusEvent<HTMLElement>) => {
      // Keyboard focus only: a click or a tap focuses it too, and opens it.
      const el = e.currentTarget;
      if (!el.matches(":focus-visible")) return;
      stop();
      // After the rest, so the scroll that brings it into view does not close it.
      timer.current = window.setTimeout(() => show(el), PEEK_MS);
    },
    onBlur: close,
    onTouchStart: (e: React.TouchEvent<HTMLElement>) => {
      const el = e.currentTarget;
      held.current = false;
      stop();
      timer.current = window.setTimeout(() => {
        held.current = true;
        show(el);
      }, HOLD_MS);
    },
    onTouchMove: stop,
    onTouchEnd: stop,
    onContextMenu: (e: React.MouseEvent) => {
      // A long press on a phone opens the preview, not the browser's menu.
      if (held.current) e.preventDefault();
    },
    onClickCapture: (e: React.MouseEvent) => {
      // Opening it, by a click or by Enter, puts the preview away.
      if (!held.current) {
        close();
        return;
      }
      held.current = false;
      e.preventDefault();
      e.stopPropagation();
    },
  };
  return { at, bind, close };
}

/**
 * A card over the page, beside what it previews and centred on it; drawn into
 * the body so no list clips it. It is measured before it paints, so a title
 * that wraps stays centred.
 */
export function PeekCard({
  at,
  id,
  onClose,
  children,
}: {
  at: DOMRect;
  id: string;
  onClose: () => void;
  children: ReactNode;
}) {
  const card = useRef<HTMLDivElement>(null);
  const [height, setHeight] = useState<number>();
  // Every render: React skips the update when the height has not changed.
  useLayoutEffect(() => {
    setHeight(card.current?.offsetHeight);
  });
  const place = placePeek(
    at,
    { width: window.innerWidth, height: window.innerHeight },
    { width: PEEK_WIDTH, height: height ?? 64 },
  );
  return createPortal(
    <div
      ref={card}
      id={id}
      role="tooltip"
      data-peek
      onPointerDown={onClose}
      className="humagen-glass-pop humagen-pop fixed z-[60] flex flex-col gap-1 rounded-aui-lg px-3.5 py-3"
      style={{ top: place.top, left: place.left, width: place.width }}
    >
      {children}
    </div>,
    document.body,
  );
}

/**
 * What a sidebar row's preview says: its whole title, its status in words when
 * it has one, and how long ago it moved. The row's badge is a glyph alone, so
 * this is where a finger or a keyboard reads why it failed or what it waits
 * for. The row's own description already says it to a screen reader.
 */
export function ConversationPeek({ task, status }: { task: ThreadSummary; status?: string }) {
  const words = defaultCopy.sidebar;
  const when = age(task.updated_at, Date.now());
  return (
    <>
      <span className="line-clamp-3 break-words text-aui-sm text-aui-fg">
        {task.title || "Untitled conversation"}
      </span>
      {status && (
        <span aria-hidden data-peek-status className="break-words text-aui-xs text-aui-fg">
          {status}
        </span>
      )}
      <span className="text-aui-xs text-aui-muted tabular-nums">{words.movedAgo(when)}</span>
    </>
  );
}
