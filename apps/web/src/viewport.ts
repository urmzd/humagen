/**
 * Keeps the composer above a phone's on-screen keyboard.
 *
 * Chrome on Android resizes the page for the keyboard (the viewport meta's
 * `interactive-widget=resizes-content`), so `100dvh` already ends at the
 * keyboard. Safari on iOS does not: the keyboard covers the bottom of the
 * page and only the visual viewport shrinks. The difference between the two
 * is the keyboard's height, written to `--humagen-keyboard` on the root, and
 * the console's height subtracts it (styles.css). Where the browser resized
 * the page itself, the difference is zero and nothing changes.
 */

/** The parts of window.visualViewport this file reads. */
export interface VisualViewportLike {
  height: number;
  scale: number;
  addEventListener(type: "resize" | "scroll", fn: () => void): void;
}

/**
 * How much of the layout viewport the keyboard covers, in whole pixels. Zero
 * when nothing covers it, and zero while the person has pinched to zoom,
 * where the visual viewport is smaller for another reason.
 */
export function keyboardInset(layoutHeight: number, vv: VisualViewportLike | null): number {
  if (!vv || vv.scale > 1.01) return 0;
  const covered = layoutHeight - vv.height;
  // Browser chrome that slides as the page scrolls moves it by a few pixels.
  return covered > 40 ? Math.round(covered) : 0;
}

/**
 * The app's height when installed on an iPhone or iPad.
 *
 * Opened from the home screen, with the status bar drawn over the page
 * (black-translucent), Safari sizes the viewport as if the status bar still
 * took its own strip: `100dvh` and innerHeight come out short by its height,
 * and the page ends that far above the bottom of the screen. Installed, the
 * app fills the screen, so the screen's size is its height there. iOS reports
 * the screen in portrait whichever way it is held. Null keeps `100dvh`: in a
 * tab, off iOS, or where Safari already gets it right.
 */
export function standaloneHeight(
  inner: { width: number; height: number },
  screen: { width: number; height: number },
): number | null {
  const landscape = inner.width > inner.height;
  const full = landscape
    ? Math.min(screen.width, screen.height)
    : Math.max(screen.width, screen.height);
  return full > inner.height ? full : null;
}

interface WindowLike {
  innerHeight: number;
  innerWidth?: number;
  screen?: { width: number; height: number };
  navigator?: {
    userAgent: string;
    platform?: string;
    maxTouchPoints?: number;
    standalone?: boolean;
  };
  matchMedia?: (query: string) => { matches: boolean };
  visualViewport?: VisualViewportLike | null;
  document: {
    addEventListener?(type: "focusin" | "focusout", fn: () => void): void;
    documentElement: {
      style: { setProperty(name: string, value: string): void };
      dataset: Record<string, string | undefined>;
    };
  };
  scrollTo(x: number, y: number): void;
}

/** Installed from the home screen of an iPhone or iPad, where the viewport comes out short. */
export function isIOSStandalone(win: WindowLike): boolean {
  const nav = win.navigator;
  if (!nav) return false;
  const ios =
    /iPad|iPhone|iPod/.test(nav.userAgent) ||
    (nav.platform === "MacIntel" && (nav.maxTouchPoints ?? 0) > 1);
  const standalone =
    nav.standalone === true || win.matchMedia?.("(display-mode: standalone)").matches === true;
  return ios && standalone;
}

/**
 * Follows the visual viewport and writes the keyboard's height to the root,
 * and the app's height where iOS sizes it short on the home screen. While the
 * keyboard is up the height is left to `100dvh`, which the keyboard's height
 * is measured against.
 */
export function installViewport(win: WindowLike): void {
  const vv = win.visualViewport;
  if (!vv) return;
  const root = win.document.documentElement;
  const ios = isIOSStandalone(win);
  let last = "";
  const apply = () => {
    const inset = keyboardInset(win.innerHeight, vv);
    const tall =
      ios && inset === 0 && win.screen && win.innerWidth !== undefined
        ? standaloneHeight({ width: win.innerWidth, height: win.innerHeight }, win.screen)
        : null;
    const height = tall === null ? "100dvh" : `${tall}px`;
    if (`${inset}:${height}` === last) return;
    last = `${inset}:${height}`;
    root.style.setProperty("--humagen-app-height", height);
    root.style.setProperty("--humagen-keyboard", `${inset}px`);
    if (inset > 0) root.dataset.keyboard = "";
    else delete root.dataset.keyboard;
    // Safari pans the page to show a field as the keyboard opens, and can
    // leave it panned after the keyboard closes, a strip of nothing under
    // the console. The console is always exactly as tall as what shows, so
    // either way the pan is undone.
    win.scrollTo(0, 0);
  };
  vv.addEventListener("resize", apply);
  vv.addEventListener("scroll", apply);
  // Safari can report the keyboard's last size after its animation ends,
  // without a resize, so a field gaining or losing focus checks again then.
  const settle = () => {
    for (const ms of [80, 400]) setTimeout(apply, ms);
  };
  win.document.addEventListener?.("focusin", settle);
  win.document.addEventListener?.("focusout", settle);
  apply();
}
