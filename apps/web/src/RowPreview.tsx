import { defaultCopy } from "@humagen/components";
import { type ReactNode, useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import type { ThreadSummary } from "./api.ts";
import { age } from "./conversationList.ts";
import { PEEK_WIDTH, placePeek } from "./peek.ts";

/** How long a pointer rests on a row before its preview opens, and a finger holds for one. */
export const PEEK_MS = 450;
export const HOLD_MS = 500;

/**
 * Opens a preview of something on hover, after the pointer rests, or on a
 * long press with a finger. A long press does not also click: the click that
 * follows it is swallowed. Leaving, scrolling or Escape closes it.
 */
export function usePeek() {
  const [at, setAt] = useState<DOMRect | null>(null);
  const timer = useRef<number | undefined>(undefined);
  const held = useRef(false);
  const stop = useCallback(() => window.clearTimeout(timer.current), []);
  const close = useCallback(() => {
    stop();
    setAt(null);
  }, [stop]);
  useEffect(() => {
    if (!at) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && close();
    window.addEventListener("keydown", onKey);
    window.addEventListener("scroll", close, true);
    return () => {
      window.removeEventListener("keydown", onKey);
      window.removeEventListener("scroll", close, true);
    };
  }, [at, close]);
  useEffect(() => stop, [stop]);
  const bind = {
    onPointerEnter: (e: React.PointerEvent<HTMLElement>) => {
      if (e.pointerType !== "mouse") return;
      const el = e.currentTarget;
      stop();
      timer.current = window.setTimeout(() => setAt(el.getBoundingClientRect()), PEEK_MS);
    },
    onPointerLeave: (e: React.PointerEvent<HTMLElement>) => {
      if (e.pointerType === "mouse") close();
    },
    onTouchStart: (e: React.TouchEvent<HTMLElement>) => {
      const el = e.currentTarget;
      held.current = false;
      stop();
      timer.current = window.setTimeout(() => {
        held.current = true;
        setAt(el.getBoundingClientRect());
      }, HOLD_MS);
    },
    onTouchMove: stop,
    onTouchEnd: stop,
    onContextMenu: (e: React.MouseEvent) => {
      // A long press on a phone opens the preview, not the browser's menu.
      if (held.current) e.preventDefault();
    },
    onClickCapture: (e: React.MouseEvent) => {
      if (!held.current) return;
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

/** What a sidebar row's preview says: its whole title and how long ago it moved. */
export function ConversationPeek({ task }: { task: ThreadSummary }) {
  const words = defaultCopy.sidebar;
  const when = age(task.updated_at, Date.now());
  return (
    <>
      <span className="line-clamp-3 break-words text-aui-sm text-aui-fg">
        {task.title || "Untitled conversation"}
      </span>
      <span className="text-aui-xs text-aui-muted tabular-nums">{words.movedAgo(when)}</span>
    </>
  );
}
