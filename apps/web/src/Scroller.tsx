import { defaultCopy } from "@humagen/components/copy";
import { type ReactNode, useCallback, useEffect, useRef, useState } from "react";

const arrow =
  "absolute top-1/2 z-10 inline-flex size-8 -translate-y-1/2 items-center justify-center rounded-full border border-aui-border bg-aui-bg text-aui-muted shadow-sm hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent";

/**
 * A row that is centered while it fits and scrolls sideways when it does not:
 * the edge with more to see fades to gray and gets a chevron that moves the
 * row by most of its width.
 */
export function Scroller({
  label,
  children,
  className = "",
}: {
  label: string;
  children: ReactNode;
  className?: string;
}) {
  const row = useRef<HTMLUListElement>(null);
  const [edges, setEdges] = useState({ start: false, end: false });
  const measure = useCallback(() => {
    const el = row.current;
    if (!el) return;
    setEdges({
      start: el.scrollLeft > 1,
      end: el.scrollLeft + el.clientWidth < el.scrollWidth - 1,
    });
  }, []);
  useEffect(() => {
    measure();
    const el = row.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, [measure]);
  const move = (dir: 1 | -1) => {
    const el = row.current;
    el?.scrollBy({ left: dir * el.clientWidth * 0.8, behavior: "smooth" });
  };
  const fade = (side: "l" | "r") =>
    `pointer-events-none absolute inset-y-0 ${side === "l" ? "left-0 bg-gradient-to-r" : "right-0 bg-gradient-to-l"} w-12 from-aui-bg to-transparent`;
  return (
    // The chevrons sit inside the caller's padding, never on the screen's edge.
    <div className={className}>
      <div className="relative">
        <ul
          ref={row}
          aria-label={label}
          onScroll={measure}
          className="flex snap-x gap-2 overflow-x-auto scroll-smooth [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
        >
          {/* Margins of auto center the row while it fits and fall to zero once it overflows. */}
          <li aria-hidden className="ml-auto" />
          {children}
          <li aria-hidden className="mr-auto" />
        </ul>
        {edges.start && (
          <>
            <span aria-hidden className={fade("l")} />
            <button
              type="button"
              aria-label={defaultCopy.chat.scrollBack}
              className={`${arrow} left-0`}
              onClick={() => move(-1)}
            >
              <svg
                aria-hidden="true"
                width={14}
                height={14}
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth={2}
              >
                <path d="M15 6l-6 6 6 6" />
              </svg>
            </button>
          </>
        )}
        {edges.end && (
          <>
            <span aria-hidden className={fade("r")} />
            <button
              type="button"
              aria-label={defaultCopy.chat.scrollOn}
              className={`${arrow} right-0`}
              onClick={() => move(1)}
            >
              <svg
                aria-hidden="true"
                width={14}
                height={14}
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth={2}
              >
                <path d="M9 6l6 6-6 6" />
              </svg>
            </button>
          </>
        )}
      </div>
    </div>
  );
}
