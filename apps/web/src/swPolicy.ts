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

/** A script the build wrote, as the bundler describes it. */
export interface BuiltScript {
  fileName: string;
  /** Whether the page or the build names it directly. */
  isEntry: boolean;
  /** The scripts it loads with itself. */
  imports: readonly string[];
  /** The scripts it loads on demand. */
  dynamicImports: readonly string[];
  /** The stylesheets it brings. */
  css?: readonly string[];
}

/**
 * The console's shell: the scripts and stylesheets the first screen loads,
 * whatever page it opens on. That is the page's entry, what the entry reads
 * once the branding is in (main.tsx reads the console that way, not with
 * itself), and everything those load with themselves. What the console then
 * reads on demand, a page at a time (pages.ts), is not part of it. The
 * worker is not its own shell.
 */
export function shellFiles(scripts: readonly BuiltScript[], worker = "sw.js"): Set<string> {
  const byName = new Map(scripts.map((script) => [script.fileName, script]));
  const shell = new Set<string>();
  const take = (name: string) => {
    const script = byName.get(name);
    if (!script || shell.has(name)) return;
    shell.add(name);
    for (const css of script.css ?? []) shell.add(css);
    for (const next of script.imports) take(next);
  };
  for (const entry of scripts) {
    if (!entry.isEntry || entry.fileName === worker) continue;
    take(entry.fileName);
    for (const next of entry.dynamicImports) take(next);
  }
  return shell;
}

/**
 * The files a build precaches, from every file it wrote: the shell's scripts
 * and stylesheets, and the offline screen. A page's own script, read when
 * the page is opened, is kept the first time it is asked for instead
 * (`strategyFor` answers it cache-first), so a first visit is not sent the
 * pages it never opens. Fonts come in many subsets a page mostly never asks
 * for, and are kept the same way. The page, the worker, source maps and the
 * files branding can replace at run time (the manifest and icons, served
 * from public/) are left out, so a deploy or a change of branding always
 * shows.
 */
export function precacheList(files: readonly string[], shell: ReadonlySet<string>): string[] {
  return files
    .filter((f) => (shell.has(f) && /\.(js|css)$/.test(f)) || `/${f}` === OFFLINE_PATH)
    .map((f) => `/${f}`)
    .sort();
}

/**
 * Whether an answer to a built asset is kept. The server answers a path it
 * does not have with the page, so after a deploy an old script's address
 * gets HTML: that is never kept as the script.
 */
export function keeps(answer: { ok: boolean; type: string; contentType: string | null }): boolean {
  return answer.ok && answer.type === "basic" && !/^text\/html\b/i.test(answer.contentType ?? "");
}
