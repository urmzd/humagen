import { useEffect, useState } from "react";
import { DRAWER_BELOW } from "./sidebarState.ts";

const query = `(min-width: ${DRAWER_BELOW}px)`;

/** Whether the window is wider than a phone's, kept as it changes. */
export function useWide(): boolean {
  const read = () =>
    typeof window === "undefined"
      ? true
      : (window.matchMedia?.(query).matches ?? window.innerWidth >= DRAWER_BELOW);
  const [wide, setWide] = useState(read);
  useEffect(() => {
    const list = window.matchMedia?.(query);
    if (!list) return;
    const on = () => setWide(list.matches);
    list.addEventListener("change", on);
    return () => list.removeEventListener("change", on);
  }, []);
  return wide;
}
