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

/** Narrower than this, the sidebar is a drawer over the page. */
export const DRAWER_BELOW = 768;

/**
 * Whether the sidebar starts open: as this browser left it, under the key it
 * had before the rename too (moved once), else open on a wide window. On a
 * phone it is a drawer over the page, so it always starts closed: a drawer
 * left open must not cover the page the next time the app opens.
 */
export function sidebarStartsOpen(width: number): boolean {
  moveStored(OLD_SIDEBAR_KEY, SIDEBAR_KEY);
  if (width < DRAWER_BELOW) return false;
  return stored(SIDEBAR_KEY, true);
}

/** Where this browser keeps the width the sidebar was dragged to. */
export const SIDEBAR_WIDTH_KEY = "humagen.sidebar.width";

/**
 * The open sidebar's width, in pixels: by default, and the bounds a drag
 * keeps to. It is drawn no wider than a third of the window, so a tablet's
 * page keeps its room.
 */
export const SIDEBAR_WIDTH = { min: 240, initial: 296, max: 440 } as const;

/** A width kept within the sidebar's bounds, whole pixels. */
export function clampSidebarWidth(width: number): number {
  if (!Number.isFinite(width)) return SIDEBAR_WIDTH.initial;
  return Math.round(Math.min(SIDEBAR_WIDTH.max, Math.max(SIDEBAR_WIDTH.min, width)));
}

/**
 * The width this browser last dragged the sidebar to, else the default. It
 * is per browser, like whether the sidebar is open, since it follows the
 * screen it was set on.
 */
export function storedSidebarWidth(): number {
  try {
    const v = window.localStorage.getItem(SIDEBAR_WIDTH_KEY);
    return v === null ? SIDEBAR_WIDTH.initial : clampSidebarWidth(Number(v));
  } catch {
    return SIDEBAR_WIDTH.initial;
  }
}

/**
 * Draws the sidebar at a width: the `--humagen-sidebar-w` the sidebar and the
 * loading shell's placeholder both read. `keep` also remembers it.
 */
export function applySidebarWidth(width: number, keep = false): number {
  const w = clampSidebarWidth(width);
  try {
    document.documentElement.style.setProperty("--humagen-sidebar-w", `${w}px`);
    if (keep) window.localStorage.setItem(SIDEBAR_WIDTH_KEY, String(w));
  } catch {
    // A page without storage still draws the sidebar at its width.
  }
  return w;
}

// The loading shell draws a placeholder at the sidebar's width before the
// sidebar itself loads, so the width is set as soon as this module is.
if (typeof document !== "undefined") applySidebarWidth(storedSidebarWidth());
