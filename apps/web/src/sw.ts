/**
 * The console's service worker, built to /sw.js by vite.config.ts, which
 * writes the build's precache list and version into it. What it does with
 * each request is in swPolicy.ts.
 *
 * A new version installs beside the running one and waits: it takes over
 * only when the person accepts the prompt (pwa.ts), never in the middle of
 * their work. Nothing here imports the console, and nothing exports, so the
 * built file is a classic worker script.
 */
import { cacheName, OFFLINE_PATH, staleCaches, strategyFor } from "./swPolicy.ts";

/** The parts of the worker's global scope this file uses. */
interface WorkerScope {
  location: { origin: string };
  skipWaiting(): Promise<void>;
  clients: { claim(): Promise<void> };
  addEventListener(type: "install" | "activate", fn: (e: ExtendableEvent) => void): void;
  addEventListener(type: "fetch", fn: (e: FetchEvent) => void): void;
  addEventListener(type: "message", fn: (e: { data: unknown }) => void): void;
}
interface ExtendableEvent {
  waitUntil(p: Promise<unknown>): void;
}
interface FetchEvent extends ExtendableEvent {
  request: Request;
  respondWith(r: Promise<Response>): void;
}

const scope = self as unknown as WorkerScope;

/** Replaced at build time with {"version": ..., "precache": [...]}. */
const manifest = JSON.parse("__HUMAGEN_SW_MANIFEST__") as { version: string; precache: string[] };
const CACHE = cacheName(manifest.version);
const precached = new Set(manifest.precache);

scope.addEventListener("install", (event) => {
  // `cache: "reload"` skips the browser's HTTP cache, so the copies are fresh.
  event.waitUntil(
    caches
      .open(CACHE)
      .then((cache) =>
        cache.addAll(manifest.precache.map((url) => new Request(url, { cache: "reload" }))),
      ),
  );
});

scope.addEventListener("activate", (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((names) =>
        Promise.all(staleCaches(names, manifest.version).map((n) => caches.delete(n))),
      )
      .then(() => scope.clients.claim()),
  );
});

// The prompt's Reload: the waiting version takes over, then the page reloads.
scope.addEventListener("message", (event) => {
  const data = event.data as { type?: string } | null;
  if (data?.type === "SKIP_WAITING") void scope.skipWaiting();
});

scope.addEventListener("fetch", (event) => {
  const req = event.request;
  const strategy = strategyFor(req, scope.location.origin, precached);
  if (strategy === "page") {
    event.respondWith(
      fetch(req).catch(async () => {
        const offline = await caches.match(OFFLINE_PATH, { cacheName: CACHE });
        return offline ?? Response.error();
      }),
    );
  } else if (strategy === "cache-first") {
    event.respondWith(
      caches.match(req, { cacheName: CACHE }).then(
        (hit) =>
          hit ??
          fetch(req).then((res) => {
            // A hashed name never changes, so a good answer is kept for next time.
            if (res.ok && res.type === "basic") {
              const copy = res.clone();
              event.waitUntil(caches.open(CACHE).then((cache) => cache.put(req, copy)));
            }
            return res;
          }),
      ),
    );
  }
  // network-only: not answered here, so the browser fetches it untouched.
});
