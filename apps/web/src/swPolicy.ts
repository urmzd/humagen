/**
 * What the service worker (sw.ts) does with each request. Pure, so the rules
 * are tested without a worker: the worker reads a request and acts on what
 * these return.
 *
 * The page itself always comes from the network, so a deploy is never hidden
 * behind an old copy; offline, the worker answers with the offline screen
 * instead. Built assets, whose names carry a hash of their content, come from
 * the cache. The API, sign-in and every other path the server answers itself
 * are never touched: auth and tenant data stay live.
 */

/** Paths the server answers itself. The worker never reads, caches or answers them. */
export const SERVER_PATHS = ["/api", "/auth", "/branding", "/connectors", "/healthz"] as const;

/** The offline screen, built with the console (vite.config.ts) and precached. */
export const OFFLINE_PATH = "/offline.html";

/** Every cache this worker owns starts with this, so it never deletes another's. */
export const CACHE_PREFIX = "humagen-shell-";

export type Strategy =
  /** Leave the request alone: the browser fetches it as if there were no worker. */
  | "network-only"
  /** The page: from the network, or the offline screen when there is none. */
  | "page"
  /**
   * A built asset, whose name carries a hash of its content so it never
   * changes: from the cache, or from the network and then kept.
   */
  | "cache-first";

export interface RequestFacts {
  url: string;
  method: string;
  /** `Request.mode`: "navigate" for a page load. */
  mode: string;
}

/** Whether a path is one the server answers itself. */
export function serverPath(pathname: string): boolean {
  return SERVER_PATHS.some((p) => pathname === p || pathname.startsWith(`${p}/`));
}

/**
 * How to answer a request, given the worker's origin and the paths it
 * precached. Anything not a same-origin GET is left alone.
 */
export function strategyFor(
  req: RequestFacts,
  origin: string,
  precached: ReadonlySet<string>,
): Strategy {
  if (req.method !== "GET") return "network-only";
  let url: URL;
  try {
    url = new URL(req.url);
  } catch {
    return "network-only";
  }
  if (url.origin !== origin) return "network-only";
  if (serverPath(url.pathname)) return "network-only";
  if (req.mode === "navigate") return "page";
  if (precached.has(url.pathname) || url.pathname.startsWith("/assets/")) return "cache-first";
  return "network-only";
}

/** The cache a version of the console keeps its shell in. */
export function cacheName(version: string): string {
  return `${CACHE_PREFIX}${version}`;
}

/** Caches an activated worker deletes: its own older ones, never another's. */
export function staleCaches(names: readonly string[], version: string): string[] {
  const keep = cacheName(version);
  return names.filter((n) => n.startsWith(CACHE_PREFIX) && n !== keep);
}

/**
 * The files a build precaches, from every file it wrote: its scripts and
 * stylesheets, and the offline screen. Fonts come in many subsets a page
 * mostly never asks for, so each is kept the first time it is used instead.
 * The page, the worker, source maps and the files branding can replace at
 * run time (the manifest and icons, served from public/) are left out, so a
 * deploy or a change of branding always shows.
 */
export function precacheList(files: readonly string[]): string[] {
  return files
    .filter((f) => (f.startsWith("assets/") && /\.(js|css)$/.test(f)) || `/${f}` === OFFLINE_PATH)
    .map((f) => `/${f}`)
    .sort();
}
