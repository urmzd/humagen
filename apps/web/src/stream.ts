/*
 * Event streams that never starve the page of connections.
 *
 * A browser allows six HTTP/1.1 connections to one origin, shared by every
 * tab on it, and an event stream holds one for as long as it is open. A few
 * console tabs, each with the workspace's stream and a session's, used them
 * all: every other request, in every tab, then waited behind them, so pages
 * drew blank and navigation hung while the server was idle. A stream opened
 * here keeps to two rules:
 *
 * - It is closed once its tab has been hidden for `HIDDEN_GRACE_MS` and
 *   opened again when the tab is shown, so a tab nobody is looking at holds
 *   no connection.
 * - It first takes one of `STREAM_SLOTS` slots, which every tab on the
 *   origin shares (Web Locks), so streams never hold more than that many
 *   connections between them. A stream with no slot calls `poll` every
 *   `SLOTLESS_POLL_MS` instead and tries again, so its tab stays current,
 *   only less promptly. Where there are no Web Locks it opens as before.
 */

/** How long a tab may be hidden before its streams are closed. */
export const HIDDEN_GRACE_MS = 15_000;

/** How many event streams every tab on the origin may hold between them: two short of the browser's six. */
export const STREAM_SLOTS = 4;

/** How often a stream that has no slot reads again and asks for one. */
export const SLOTLESS_POLL_MS = 10_000;

/** The part of `document` this file uses, so a test can stand in for it. */
export type Visibility = {
  visibilityState: string;
  addEventListener(type: "visibilitychange", listener: () => void): void;
  removeEventListener(type: "visibilitychange", listener: () => void): void;
};

/** The part of `navigator.locks` this file uses. */
export type Locks = {
  request(
    name: string,
    options: { ifAvailable: true },
    held: (lock: unknown) => Promise<void> | undefined,
  ): Promise<unknown>;
};

export type StreamOptions = {
  /** Read the state again: called while the stream waits for a slot. */
  poll?: () => void;
  page?: Visibility;
  locks?: Locks;
  graceMs?: number;
  pollMs?: number;
};

const slotName = (i: number) => `humagen-stream-${i}`;

/** Takes a free slot, resolving to the way to give it back, or to nothing when all are taken. */
async function takeSlot(locks: Locks): Promise<(() => void) | undefined> {
  for (let i = 0; i < STREAM_SLOTS; i++) {
    const release = await new Promise<(() => void) | undefined>((got) => {
      locks
        .request(slotName(i), { ifAvailable: true }, (lock) => {
          if (!lock) {
            got(undefined);
            return undefined;
          }
          // The slot is held until this resolves.
          return new Promise<void>((done) => got(done));
        })
        .catch(() => got(undefined));
    });
    if (release) return release;
  }
  return undefined;
}

function browserLocks(): Locks | undefined {
  if (typeof document === "undefined" || typeof navigator === "undefined") return undefined;
  return (navigator as { locks?: Locks }).locks;
}

/**
 * Opens `url` as an event stream and hands each new EventSource to `wire`,
 * which attaches its handlers. `resumed` is true when the source is not the
 * stream's first, or opened only after waiting: whatever was sent before it
 * was missed, so the caller reads its state again. Returns a way to stop.
 */
export function openStream(
  url: string,
  wire: (source: EventSource, resumed: boolean) => void,
  options: StreamOptions = {},
): () => void {
  const page = options.page ?? (typeof document === "undefined" ? undefined : document);
  const locks = options.locks ?? browserLocks();
  const graceMs = options.graceMs ?? HIDDEN_GRACE_MS;
  const pollMs = options.pollMs ?? SLOTLESS_POLL_MS;
  let source: EventSource | undefined;
  let release: (() => void) | undefined;
  let grace: ReturnType<typeof setTimeout> | undefined;
  let retry: ReturnType<typeof setTimeout> | undefined;
  // Whether the stream should be open, and which attempt to open it is current.
  let wanted = false;
  let attempt = 0;
  let missed = false;
  let stopped = false;

  const connect = () => {
    source = new EventSource(url);
    wire(source, missed);
    missed = false;
  };
  const start = () => {
    wanted = true;
    if (!locks) {
      connect();
      return;
    }
    const mine = ++attempt;
    void takeSlot(locks).then((slot) => {
      if (mine !== attempt || !wanted) {
        slot?.();
        return;
      }
      if (slot) {
        release = slot;
        connect();
        return;
      }
      missed = true;
      retry = setTimeout(() => {
        options.poll?.();
        start();
      }, pollMs);
    });
  };
  const close = () => {
    wanted = false;
    attempt++;
    clearTimeout(retry);
    source?.close();
    source = undefined;
    release?.();
    release = undefined;
  };
  const pause = () => {
    grace = undefined;
    if (stopped || page?.visibilityState !== "hidden") return;
    close();
    missed = true;
  };
  const changed = () => {
    if (stopped) return;
    clearTimeout(grace);
    grace = undefined;
    if (page?.visibilityState === "hidden") grace = setTimeout(pause, graceMs);
    else if (!wanted) start();
  };
  start();
  page?.addEventListener("visibilitychange", changed);
  // A tab opened in the background is hidden from the start.
  if (page?.visibilityState === "hidden") grace = setTimeout(pause, graceMs);
  return () => {
    stopped = true;
    clearTimeout(grace);
    page?.removeEventListener("visibilitychange", changed);
    close();
  };
}
