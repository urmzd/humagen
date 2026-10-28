import { defaultCopy } from "@humagen/components";
import { useId, useRef, useState } from "react";
import { ago, useInsideGutter } from "./ui.tsx";

const words = defaultCopy.threads;

/** A message's exact local date and time, available without a pointer. */
export function MessageTime({ at }: { at: string }) {
  const id = useId();
  const root = useRef<HTMLSpanElement>(null);
  const panel = useRef<HTMLSpanElement>(null);
  const [hovered, setHovered] = useState(false);
  const [focused, setFocused] = useState(false);
  const [activated, setActivated] = useState(false);
  const [dismissed, setDismissed] = useState(false);
  const open = !dismissed && (hovered || focused || activated);
  const shift = useInsideGutter(panel, root, open);
  const date = new Date(at);
  if (!Number.isFinite(date.getTime())) return null;
  const exact = date.toLocaleString(undefined, { dateStyle: "full", timeStyle: "long" });
  return (
    <span
      ref={root}
      className="relative inline-flex"
      onMouseEnter={() => {
        setHovered(true);
        setDismissed(false);
      }}
      onMouseLeave={() => setHovered(false)}
    >
      <button
        type="button"
        aria-label={words.posted(exact)}
        aria-describedby={open ? id : undefined}
        className="min-h-6 rounded-aui text-left focus-visible:outline-2 focus-visible:outline-aui-accent"
        onFocus={() => {
          setFocused(true);
          setDismissed(false);
        }}
        onBlur={() => {
          setFocused(false);
          setActivated(false);
        }}
        onClick={() => {
          setActivated(true);
          setDismissed(false);
        }}
        onKeyDown={(event) => {
          if (event.key === "Escape") {
            setDismissed(true);
            setActivated(false);
          }
        }}
      >
        <time dateTime={at}>{ago(at)}</time>
      </button>
      {open && (
        <span
          ref={panel}
          id={id}
          role="tooltip"
          style={{ transform: `translateX(${shift}px)` }}
          className="absolute left-0 top-full z-40 mt-1 w-max max-w-[min(20rem,75vw)] rounded-aui border border-aui-border bg-aui-pop px-2.5 py-1.5 text-aui-xs text-aui-fg shadow-lg"
        >
          {exact}
        </span>
      )}
    </span>
  );
}
