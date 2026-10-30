import assert from "node:assert/strict";
import { test } from "node:test";
import { offlinePage } from "./offlinePage.ts";
import {
  CHECK_EVERY_MS,
  type ContainerLike,
  type RegistrationLike,
  startWorker,
  type WorkerLike,
} from "./pwa.ts";
import { syncThemeColor, THEME_COLORS } from "./standalone.ts";
import {
  CACHE_PREFIX,
  cacheName,
  OFFLINE_PATH,
  precacheList,
  serverPath,
  staleCaches,
  strategyFor,
} from "./swPolicy.ts";
import { installViewport, keyboardInset } from "./viewport.ts";

const ORIGIN = "https://console.example";
const precached = new Set(["/assets/App-abc.js", OFFLINE_PATH]);
const get = (path: string, mode = "cors") => ({ url: `${ORIGIN}${path}`, method: "GET", mode });

test("the worker never touches the API, sign-in or any other path the server answers", () => {
  for (const path of [
    "/api/me",
    "/api",
    "/api/events?since=1",
    "/auth/login",
    "/auth/callback?code=x",
    "/branding/font/display.woff2",
    "/connectors/slack/events",
    "/healthz",
  ]) {
    assert.equal(strategyFor(get(path), ORIGIN, precached), "network-only", path);
    // Not even a page load: /auth/login is navigated to, and must reach the server.
    assert.equal(strategyFor(get(path, "navigate"), ORIGIN, precached), "network-only", path);
  }
  assert.equal(serverPath("/apiary"), false, "a page whose name only starts like one is the app's");
  assert.equal(serverPath("/authors"), false);
});

test("a page always comes from the network, so a deploy is never hidden behind an old copy", () => {
  for (const path of ["/", "/inbox", "/conversations/t-1", "/offline.html"]) {
    assert.equal(strategyFor(get(path, "navigate"), ORIGIN, precached), "page", path);
  }
});

test("built assets come from the cache; anything else, another origin and writes are left alone", () => {
  assert.equal(strategyFor(get("/assets/App-abc.js"), ORIGIN, precached), "cache-first");
  assert.equal(
    strategyFor(get("/assets/plex-latin-400.woff2"), ORIGIN, precached),
    "cache-first",
    "a font is kept the first time it is used",
  );
  assert.equal(strategyFor(get("/manifest.webmanifest"), ORIGIN, precached), "network-only");
  assert.equal(strategyFor(get("/icon-192.png"), ORIGIN, precached), "network-only");
  assert.equal(
    strategyFor(
      { url: "https://cdn.example/x.js", method: "GET", mode: "cors" },
      ORIGIN,
      precached,
    ),
    "network-only",
  );
  assert.equal(
    strategyFor({ ...get("/assets/App-abc.js"), method: "POST" }, ORIGIN, precached),
    "network-only",
  );
  assert.equal(
    strategyFor({ url: "not a url", method: "GET", mode: "cors" }, ORIGIN, precached),
    "network-only",
  );
});

test("a build precaches its scripts, styles and offline screen, never the page or what branding replaces", () => {
  assert.deepEqual(
    precacheList([
      "index.html",
      "sw.js",
      "offline.html",
      "manifest.webmanifest",
      "icon-192.png",
      "assets/App-abc.js",
      "assets/App-abc.js.map",
      "assets/index-def.css",
      "assets/plex-latin-400.woff2",
    ]),
    ["/assets/App-abc.js", "/assets/index-def.css", "/offline.html"],
  );
});

test("an activated version deletes its own older caches and nobody else's", () => {
  assert.equal(cacheName("v2"), `${CACHE_PREFIX}v2`);
  assert.deepEqual(
    staleCaches([cacheName("v1"), cacheName("v2"), "someone-else", cacheName("v0")], "v2"),
    [cacheName("v1"), cacheName("v0")],
  );
});

test("the offline screen is the copy's words, escaped, with no script the content policy would refuse", () => {
  const html = offlinePage({
    offlineTitle: "You are <offline>",
    offlineBody: "Body & more",
    offlineRetry: "Try Again",
  });
  assert.match(html, /<h1>You are &lt;offline&gt;<\/h1>/);
  assert.match(html, /Body &amp; more/);
  assert.match(html, /<a href="">Try Again<\/a>/, "Try Again reloads the address it was opened at");
  assert.doesNotMatch(html, /<script/);
  assert.match(html, /safe-area-inset-bottom/);
});

/** A fake service worker container that records what the page asked of it. */
function fakeBrowser(opts: { controller: boolean; waiting?: boolean }) {
  const listeners: Record<string, (() => void)[]> = {};
  const on = (type: string, fn: () => void) => {
    listeners[type] ??= [];
    listeners[type].push(fn);
  };
  const fire = (type: string) => {
    for (const fn of listeners[type] ?? []) fn();
  };
  const worker = (state: string): WorkerLike & { messages: unknown[]; set(s: string): void } => {
    const own: (() => void)[] = [];
    const w = {
      state,
      messages: [] as unknown[],
      postMessage(m: unknown) {
        w.messages.push(m);
      },
      addEventListener(_t: "statechange", fn: () => void) {
        own.push(fn);
      },
      set(s: string) {
        w.state = s;
        for (const fn of own) fn();
      },
    };
    return w;
  };
  const waiting = opts.waiting ? worker("installed") : null;
  let updates = 0;
  const reg: RegistrationLike & { installing: ReturnType<typeof worker> | null } = {
    waiting,
    installing: null,
    update() {
      updates++;
      return Promise.resolve();
    },
    addEventListener: (t, fn) => on(t, fn),
  };
  const registered: { url: string; options: RegistrationOptions }[] = [];
  const container: ContainerLike = {
    controller: opts.controller ? {} : null,
    register(url, options) {
      registered.push({ url, options });
      return Promise.resolve(reg);
    },
    addEventListener: (t, fn) => on(t, fn),
  };
  return { container, reg, worker, fire, registered, waiting, updates: () => updates };
}

const flush = () => new Promise((resolve) => setTimeout(resolve, 0));

test("the console registers /sw.js for the whole app and bypasses the HTTP cache for it", async () => {
  const b = fakeBrowser({ controller: true });
  startWorker({ container: b.container, reload: () => {} });
  await flush();
  assert.deepEqual(b.registered, [
    { url: "/sw.js", options: { scope: "/", updateViaCache: "none" } },
  ]);
});

test("a new version waits for the person: it is offered, and nothing reloads until they accept", async () => {
  const b = fakeBrowser({ controller: true });
  let reloads = 0;
  const updates = startWorker({ container: b.container, reload: () => reloads++ });
  await flush();
  assert.equal(updates.get().waiting, false);
  let told = 0;
  updates.subscribe(() => told++);

  const next = b.worker("installing");
  b.reg.installing = next;
  b.fire("updatefound");
  next.set("installed");
  assert.equal(updates.get().waiting, true);
  assert.equal(told, 1);

  // A controller change the person did not ask for (another tab took the update) does not reload this one.
  b.fire("controllerchange");
  assert.equal(reloads, 0);

  updates.accept();
  assert.deepEqual(next.messages, [{ type: "SKIP_WAITING" }]);
  assert.equal(reloads, 0, "the page reloads only once the new version is in control");
  b.fire("controllerchange");
  assert.equal(reloads, 1);
});

test("Later puts the prompt away and keeps the running version", async () => {
  const b = fakeBrowser({ controller: true, waiting: true });
  let reloads = 0;
  const updates = startWorker({ container: b.container, reload: () => reloads++ });
  await flush();
  assert.equal(updates.get().waiting, true, "a version already waiting is offered on load");
  updates.dismiss();
  assert.equal(updates.get().waiting, false);
  b.fire("controllerchange");
  assert.equal(reloads, 0);
  assert.deepEqual(b.waiting?.messages, []);
});

test("the first install is not an update: with nothing in control, nothing is offered", async () => {
  const b = fakeBrowser({ controller: false });
  const updates = startWorker({ container: b.container, reload: () => {} });
  await flush();
  const first = b.worker("installing");
  b.reg.installing = first;
  b.fire("updatefound");
  first.set("installed");
  assert.equal(updates.get().waiting, false);
  updates.accept();
  assert.deepEqual(first.messages, [], "accept with nothing offered sends nothing");
});

test("an open console asks for a new version hourly and when it is shown again", async () => {
  const b = fakeBrowser({ controller: true });
  let every: { fn: () => void; ms: number } | undefined;
  let shown: (() => void) | undefined;
  startWorker({
    container: b.container,
    reload: () => {},
    every: (fn, ms) => {
      every = { fn, ms };
    },
    onShown: (fn) => {
      shown = fn;
    },
  });
  await flush();
  assert.equal(every?.ms, CHECK_EVERY_MS);
  every?.fn();
  shown?.();
  assert.equal(b.updates(), 2);
});

test("no worker (a private window, an old browser) leaves the console working without one", async () => {
  const container: ContainerLike = {
    controller: null,
    register: () => Promise.reject(new Error("SecurityError")),
    addEventListener() {},
  };
  const updates = startWorker({ container, reload: () => {} });
  await flush();
  assert.equal(updates.get().waiting, false);
});

test("keyboardInset: Safari's keyboard is what covers the layout viewport; Chrome's resize leaves none", () => {
  const vv = (height: number, scale = 1) => ({ height, scale, addEventListener() {} });
  // iPhone 13: an 844px layout viewport, a 336px keyboard over it.
  assert.equal(keyboardInset(844, vv(508)), 336);
  // Chrome with interactive-widget=resizes-content: the layout viewport shrank with it.
  assert.equal(keyboardInset(508, vv(508)), 0);
  // The toolbar sliding as the page scrolls is not a keyboard.
  assert.equal(keyboardInset(844, vv(810)), 0);
  // Pinch-zoomed: the visual viewport is smaller for another reason.
  assert.equal(keyboardInset(844, vv(300, 2)), 0);
  assert.equal(keyboardInset(844, null), 0);
});

test("installViewport writes the keyboard's height to the root and undoes Safari's pan", () => {
  const handlers: (() => void)[] = [];
  const vv = {
    height: 844,
    scale: 1,
    addEventListener: (_: string, fn: () => void) => handlers.push(fn),
  };
  const props: Record<string, string> = {};
  const dataset: Record<string, string | undefined> = {};
  const scrolls: number[][] = [];
  installViewport({
    innerHeight: 844,
    visualViewport: vv,
    document: {
      documentElement: { style: { setProperty: (n, v) => (props[n] = v) }, dataset },
    },
    scrollTo: (x, y) => scrolls.push([x, y]),
  });
  assert.equal(props["--humagen-keyboard"], "0px");
  assert.equal("keyboard" in dataset, false);
  vv.height = 508;
  for (const fn of handlers) fn();
  assert.equal(props["--humagen-keyboard"], "336px");
  assert.equal(dataset.keyboard, "");
  assert.deepEqual(scrolls.at(-1), [0, 0]);
  vv.height = 844;
  for (const fn of handlers) fn();
  assert.equal(props["--humagen-keyboard"], "0px");
  assert.equal("keyboard" in dataset, false);
});

test("the installed app's status bar follows the theme the person picked", () => {
  const meta = () => {
    const m = { content: "", setAttribute: (_: string, v: string) => (m.content = v) };
    return m;
  };
  const metas = [meta(), meta()];
  const asked: string[] = [];
  const doc = {
    querySelectorAll: (selector: string) => {
      asked.push(selector);
      return metas;
    },
  };
  syncThemeColor(doc, "light");
  assert.deepEqual(
    asked,
    ['meta[name="theme-color"][media]'],
    "branding's single color is left alone",
  );
  assert.deepEqual(
    metas.map((m) => m.content),
    [THEME_COLORS.light, THEME_COLORS.light],
  );
  syncThemeColor(doc, "dark");
  assert.deepEqual(
    metas.map((m) => m.content),
    [THEME_COLORS.dark, THEME_COLORS.dark],
  );
});
