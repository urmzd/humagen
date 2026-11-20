/**
 * The screenshots this console was given of a session's browser, newest
 * last. They are kept in memory only: they survive moving between pages and
 * are gone on reload, as what a person did by hand is. The platform keeps
 * no screenshot, so there is nothing older to read.
 */
import { useSyncExternalStore } from "react";
import type { BrowserElement, BrowserView } from "./browserApi.ts";

export interface BrowserShot {
  id: string;
  /** When the console was given it, in ms. */
  at: number;
  action: BrowserView["action"];
  /** What the person did that led to it, in words. */
  what: string;
  provider: string;
  url?: string;
  title?: string;
  /** The image, as a data address. */
  src: string;
  elements: BrowserElement[];
  more: number;
}

/** How many screenshots a session keeps, and how many sessions keep any. */
export const MAX_BROWSER_SHOTS = 12;
export const MAX_BROWSER_SESSIONS = 6;

const none: readonly BrowserShot[] = [];

export function createBrowserShots() {
  const kept = new Map<string, readonly BrowserShot[]>();
  const heard = new Set<() => void>();
  let n = 0;
  return {
    list: (session: string): readonly BrowserShot[] => kept.get(session) ?? none,
    add(
      session: string,
      view: BrowserView,
      what: string,
      at = Date.now(),
    ): BrowserShot | undefined {
      const image = view.screenshot;
      // Only an image is drawn, whatever the answer says it is.
      if (!image?.data || !/^image\/(png|jpeg|webp)$/.test(image.media_type)) return undefined;
      n += 1;
      const shot: BrowserShot = {
        id: `shot-${n}`,
        at,
        action: view.action,
        what,
        provider: view.provider,
        url: view.url,
        title: view.title,
        src: `data:${image.media_type};base64,${image.data}`,
        elements: view.elements ?? [],
        more: view.more ?? 0,
      };
      const next = [...(kept.get(session) ?? none), shot].slice(-MAX_BROWSER_SHOTS);
      kept.delete(session);
      kept.set(session, next);
      for (const old of [...kept.keys()].slice(0, -MAX_BROWSER_SESSIONS)) kept.delete(old);
      for (const tell of heard) tell();
      return shot;
    },
    clear(session?: string) {
      if (session) kept.delete(session);
      else kept.clear();
      for (const tell of heard) tell();
    },
    subscribe(tell: () => void) {
      heard.add(tell);
      return () => {
        heard.delete(tell);
      };
    },
  };
}

export type BrowserShots = ReturnType<typeof createBrowserShots>;

/** The console's one record. */
export const browserShots = createBrowserShots();

export function useBrowserShots(
  session: string,
  store: BrowserShots = browserShots,
): readonly BrowserShot[] {
  return useSyncExternalStore(
    store.subscribe,
    () => store.list(session),
    () => none,
  );
}
