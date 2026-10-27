import { type ReactNode, useEffect, useRef, useState } from "react";

/** Retain an outgoing visual shell; reopening cancels its pending removal. */
export function usePresence(open: boolean, ms = 110) {
  const [held, setHeld] = useState(open);
  const [reduced, setReduced] = useState(() =>
    typeof window === "undefined"
      ? false
      : !!window.matchMedia?.("(prefers-reduced-motion: reduce)").matches,
  );
  useEffect(() => {
    const media = window.matchMedia?.("(prefers-reduced-motion: reduce)");
    if (!media) return;
    const change = () => setReduced(media.matches);
    change();
    media.addEventListener("change", change);
    return () => media.removeEventListener("change", change);
  }, []);
  useEffect(() => {
    if (open) {
      setHeld(true);
      return;
    }
    const timer = window.setTimeout(() => setHeld(false), reduced ? 0 : ms);
    return () => window.clearTimeout(timer);
  }, [open, ms, reduced]);
  return { mounted: open || (held && !reduced), leaving: !open && held };
}

export function Presence({
  when,
  ms,
  children,
}: {
  when: boolean;
  ms?: number;
  children: (leaving: boolean) => ReactNode;
}) {
  const { mounted, leaving } = usePresence(when, ms);
  const snapshot = useRef(children);
  if (when) snapshot.current = children;
  return mounted ? snapshot.current(leaving) : null;
}
