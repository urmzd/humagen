import { moveStored, OLD_SIDEBAR_KEY } from "./legacy.ts";

/** Where this browser keeps whether the sidebar is open. */
export const SIDEBAR_KEY = "humagen.sidebar";

/** A per-viewer convenience, so it lives in the browser and must never throw. */
function stored(key: string, fallback: boolean): boolean {
  try {
    const v = window.localStorage.getItem(key);
    return v === null ? fallback : v === "1";
  } catch {
    return fallback;
  }
}

/**
 * Whether the sidebar starts open: as this browser left it, under the key it
 * had before the rename too (moved once), else open on a wide window.
 */
export function sidebarStartsOpen(width: number): boolean {
  moveStored(OLD_SIDEBAR_KEY, SIDEBAR_KEY);
  return stored(SIDEBAR_KEY, width >= 768);
}
