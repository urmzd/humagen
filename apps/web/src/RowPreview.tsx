import { defaultCopy } from "@humagen/components";
import { AgentInitials } from "@humagen/web";
import { type ReactNode, useCallback, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import type { ThreadSummary } from "./api.ts";
import { PersonMark } from "./PersonMark.tsx";
import { placePeek, updatedWhen } from "./peek.ts";

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
    document.addEventListener("pointerdown", close, true);
    return () => {
      window.removeEventListener("keydown", onKey);
      window.removeEventListener("scroll", close, true);
      document.removeEventListener("pointerdown", close, true);
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

/** A card over the page, beside what it previews; drawn into the body so no list clips it. */
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
  const place = placePeek(at, { width: window.innerWidth, height: window.innerHeight });
  return createPortal(
    <div
      id={id}
      role="tooltip"
      data-peek
      onPointerDown={onClose}
      className="humagen-pop fixed z-[60] flex flex-col gap-1 rounded-aui border border-[var(--humagen-rule)] bg-aui-bg px-3 py-2.5 text-aui-xs shadow-lg"
      style={{ top: place.top, left: place.left, width: place.width }}
    >
      {children}
    </div>,
    document.body,
  );
}

/** What a sidebar row's preview says: title, project, when it moved, and its last line by whom. */
export function ConversationPeek({
  task,
  project,
}: {
  task: ThreadSummary;
  /** The project's title, or none when it is in no project. */
  project?: string;
}) {
  const words = defaultCopy.sidebar;
  return (
    <>
      <span className="line-clamp-2 text-aui-sm font-medium text-aui-fg">
        {task.title || "Untitled conversation"}
      </span>
      <span className="truncate text-aui-2xs text-aui-muted">
        {project || words.outsideProject} | {words.updated(updatedWhen(task.updated_at))}
      </span>
      {task.last && (
        <span className="mt-1 flex items-start gap-1.5 text-aui-muted">
          {task.last_by &&
            (task.last_agent ? (
              <AgentInitials name={task.last_by} size={16} />
            ) : (
              <PersonMark name={task.last_by} size={16} />
            ))}
          <span className="line-clamp-2 min-w-0 flex-1 text-aui-fg">{task.last}</span>
        </span>
      )}
    </>
  );
}
