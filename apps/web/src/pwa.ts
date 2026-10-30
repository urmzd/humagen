/**
 * The installed app's service worker, from the page's side: registering it,
 * noticing a new version and handing the choice to the person. A new version
 * waits until they accept; only then does it take over and the page reload.
 * Nothing reloads on its own, so a draft or a decision in progress is never
 * lost to a deploy.
 *
 * The browser's objects come in as arguments, so the tests drive every step
 * without a real worker.
 */

/** The part of a ServiceWorker this file uses. */
export interface WorkerLike {
  state: string;
  postMessage(message: unknown): void;
  addEventListener(type: "statechange", fn: () => void): void;
}

/** The part of a ServiceWorkerRegistration this file uses. */
export interface RegistrationLike {
  waiting: WorkerLike | null;
  installing: WorkerLike | null;
  update(): Promise<unknown>;
  addEventListener(type: "updatefound", fn: () => void): void;
}

/** The part of navigator.serviceWorker this file uses. */
export interface ContainerLike {
  controller: unknown;
  register(url: string, options: RegistrationOptions): Promise<RegistrationLike>;
  addEventListener(type: "controllerchange", fn: () => void): void;
}

/** What the prompt shows: nothing, or a new version waiting to be accepted. */
export interface UpdateState {
  waiting: boolean;
}

/** A small store for the prompt, read with useSyncExternalStore. */
export interface Updates {
  subscribe(fn: () => void): () => void;
  get(): UpdateState;
  /** Take the waiting version and reload once it is in control. */
  accept(): void;
  /** Keep working on this version; the prompt comes back with the next one. */
  dismiss(): void;
}

/** How often an open console asks whether there is a new version: hourly. */
export const CHECK_EVERY_MS = 60 * 60 * 1000;

export interface StartOptions {
  container: ContainerLike;
  /** Reloads the page. Called only after the person accepted. */
  reload: () => void;
  /** Runs `fn` every `ms`; setInterval in the browser. */
  every?: (fn: () => void, ms: number) => void;
  /** Calls `fn` when the page is shown again, such as an app brought back to the front. */
  onShown?: (fn: () => void) => void;
}

/**
 * Registers /sw.js and returns the store the prompt reads. A first install
 * is not an update: with no worker in control yet there is nothing to
 * replace, so nothing is offered.
 */
export function startWorker(opts: StartOptions): Updates {
  const listeners = new Set<() => void>();
  let state: UpdateState = { waiting: false };
  let waiting: WorkerLike | null = null;
  let accepted = false;
  const set = (next: UpdateState) => {
    state = next;
    for (const fn of listeners) fn();
  };
  const offer = (worker: WorkerLike) => {
    if (!opts.container.controller) return;
    waiting = worker;
    set({ waiting: true });
  };
  const watch = (worker: WorkerLike | null) => {
    if (!worker) return;
    worker.addEventListener("statechange", () => {
      if (worker.state === "installed") offer(worker);
    });
  };

  opts.container.addEventListener("controllerchange", () => {
    if (accepted) opts.reload();
  });

  opts.container
    .register("/sw.js", { scope: "/", updateViaCache: "none" })
    .then((reg) => {
      if (reg.waiting) offer(reg.waiting);
      watch(reg.installing);
      reg.addEventListener("updatefound", () => watch(reg.installing));
      const check = () => {
        reg.update().catch(() => {});
      };
      opts.every?.(check, CHECK_EVERY_MS);
      opts.onShown?.(check);
    })
    .catch(() => {
      // No worker (a private window, an old browser): the console works without one.
    });

  return {
    subscribe(fn) {
      listeners.add(fn);
      return () => listeners.delete(fn);
    },
    get: () => state,
    accept() {
      if (!waiting) return;
      accepted = true;
      waiting.postMessage({ type: "SKIP_WAITING" });
    },
    dismiss() {
      set({ waiting: false });
    },
  };
}

/** A store that never offers anything, for development and browsers without workers. */
const idle: UpdateState = { waiting: false };
export const noUpdates: Updates = {
  subscribe: () => () => {},
  get: () => idle,
  accept() {},
  dismiss() {},
};

/** Starts the worker in the browser, where there is one to start. */
export function browserUpdates(): Updates {
  if (typeof navigator === "undefined" || !("serviceWorker" in navigator)) return noUpdates;
  return startWorker({
    container: navigator.serviceWorker as unknown as ContainerLike,
    reload: () => window.location.reload(),
    every: (fn, ms) => {
      window.setInterval(fn, ms);
    },
    onShown: (fn) =>
      document.addEventListener("visibilitychange", () => {
        if (document.visibilityState === "visible") fn();
      }),
  });
}
