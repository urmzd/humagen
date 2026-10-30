import { flushSync } from "react-dom";

/*
 * Changing screens without a jump.
 *
 * Every read the console makes goes through `track`, so it knows how many are
 * in flight. Going to another screen (`swap`) keeps the screen on view until
 * the next one has read what it needs, up to `HOLD_MS`, then cross-fades the
 * content area to it with the View Transitions API. The sidebar and the
 * page's title are named, so they stay put while only the content moves. A
 * load slower than the hold shows the next screen's skeleton, which every
 * page draws at the shape of what it is waiting for. Where view transitions
 * are not supported, the swap is instant, as it was.
 */

/** How long a screen may wait on its reads before it shows anyway, with its skeleton. */
export const HOLD_MS = 320;

/** How long the console's first screen may wait on its reads, hidden, before it shows. */
export const FIRST_HOLD_MS = 900;

let inflight = 0;
const listeners = new Set<() => void>();

function changed() {
  for (const listener of listeners) listener();
}

/** Count a read while it is in flight. What it resolves to is unchanged. */
export function track<T>(read: Promise<T>): Promise<T> {
  inflight++;
  changed();
  const done = () => {
    inflight--;
    changed();
  };
  read.then(done, done);
  return read;
}

/** How many reads are in flight. */
export function pending(): number {
  return inflight;
}

/**
 * Resolves once no read has been in flight for a moment (so a read that
 * starts another is waited for too, and React has drawn what arrived), or
 * after `maxMs`, whichever is first.
 */
export function settled(maxMs: number, quietMs = 32): Promise<void> {
  return new Promise((resolve) => {
    let quiet: ReturnType<typeof setTimeout> | undefined;
    const finish = () => {
      clearTimeout(quiet);
      clearTimeout(cap);
      listeners.delete(check);
      resolve();
    };
    const check = () => {
      clearTimeout(quiet);
      if (inflight === 0) quiet = setTimeout(finish, quietMs);
    };
    const cap = setTimeout(finish, maxMs);
    listeners.add(check);
    check();
  });
}

type ViewTransitionDocument = Document & {
  startViewTransition?: (update: () => Promise<void> | void) => {
    finished: Promise<void>;
  };
};

const TITLE = "humagen-title";

/** Name the title of the screen on view, so it holds its place while the content changes. */
function nameTitle(): HTMLElement | null {
  const title = document.querySelector<HTMLElement>("main header h1");
  if (title) title.style.viewTransitionName = TITLE;
  return title;
}

/**
 * Apply a change of screen. `update` sets the new route; it is drawn at once
 * where view transitions are not supported, and in a transition where they are.
 */
export function swap(update: () => void): void {
  const doc = document as ViewTransitionDocument;
  if (typeof doc.startViewTransition !== "function" || document.visibilityState !== "visible") {
    update();
    return;
  }
  const before = nameTitle();
  const root = document.documentElement;
  root.dataset.routeSwap = "";
  try {
    doc
      .startViewTransition(async () => {
        if (before) before.style.viewTransitionName = "";
        flushSync(update);
        await settled(HOLD_MS);
        nameTitle();
      })
      .finished.finally(() => {
        delete root.dataset.routeSwap;
        for (const title of document.querySelectorAll<HTMLElement>("main header h1"))
          title.style.viewTransitionName = "";
      })
      .catch(() => {});
  } catch {
    delete root.dataset.routeSwap;
    if (before) before.style.viewTransitionName = "";
    update();
  }
}

/** Fade an element in, once: the first screen, when its reads have landed. */
export function arrive(element: HTMLElement | null): void {
  if (!element || typeof element.animate !== "function") return;
  const reduced = window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;
  // The theme's tokens (theme.css), so this moves the way a change of screen does.
  const token = (name: string, fallback: string) =>
    getComputedStyle(document.documentElement).getPropertyValue(name).trim() || fallback;
  element.animate([{ opacity: 0 }, { opacity: 1 }], {
    duration: Number.parseFloat(token(reduced ? "--aui-dur-press" : "--aui-dur-route", "220")),
    easing: reduced ? "linear" : token("--aui-ease-arrive", "ease-out"),
  });
}
