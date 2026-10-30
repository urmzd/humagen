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

interface WindowLike {
  innerHeight: number;
  visualViewport?: VisualViewportLike | null;
  document: {
    documentElement: {
      style: { setProperty(name: string, value: string): void };
      dataset: Record<string, string | undefined>;
    };
  };
  scrollTo(x: number, y: number): void;
}

/** Follows the visual viewport and writes the keyboard's height to the root. */
export function installViewport(win: WindowLike): void {
  const vv = win.visualViewport;
  if (!vv) return;
  const root = win.document.documentElement;
  let last = -1;
  const apply = () => {
    const inset = keyboardInset(win.innerHeight, vv);
    if (inset === last) return;
    last = inset;
    root.style.setProperty("--humagen-keyboard", `${inset}px`);
    if (inset > 0) {
      root.dataset.keyboard = "";
      // Safari pans the page to show the field; the console is now short
      // enough to show it in place, so the pan is undone.
      win.scrollTo(0, 0);
    } else delete root.dataset.keyboard;
  };
  vv.addEventListener("resize", apply);
  vv.addEventListener("scroll", apply);
  apply();
}
