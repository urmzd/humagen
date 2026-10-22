/**
 * Names the console had before the product was renamed to Humagen, still
 * read. This is the one file in the console that names them.
 */

/** Where the sidebar's state was kept before the rename. */
export const OLD_SIDEBAR_KEY = "warp.sidebar";

/**
 * Moves a stored value from its old key to its new one, once: a value only
 * under the old key is copied and the old key removed; with both, the new
 * one is kept and the old removed. Storage can throw; nothing here may.
 */
export function moveStored(
  from: string,
  to: string,
  storage: () => Storage = () => window.localStorage,
): void {
  try {
    const s = storage();
    const old = s.getItem(from);
    if (old === null) return;
    if (s.getItem(to) === null) s.setItem(to, old);
    s.removeItem(from);
  } catch {
    // A per-viewer convenience: without storage the default stands.
  }
}

/** The server the dev server proxies to: HUMAGEN_API, else the old WARP_API with a warning. */
export function apiTarget(
  env: Record<string, string | undefined>,
  warn: (line: string) => void = console.warn,
): string {
  const now = env.HUMAGEN_API?.trim();
  if (now) return now;
  const old = env.WARP_API?.trim();
  if (old) {
    warn("WARP_API is the old name of HUMAGEN_API; it still works, rename it");
    return old;
  }
  return "http://127.0.0.1:8080";
}
