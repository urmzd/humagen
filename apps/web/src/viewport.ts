/**
 * The app's height on a phone's home screen.
 *
 * Installed on an iPhone, with the status bar drawn over the page
 * (black-translucent), Safari sizes the viewport as if the status bar still
 * took its own strip: 100dvh comes out short by the status bar's height, and
 * the page ends that far above the bottom of the screen. Standalone, the app
 * fills the screen, so the screen's own size is its height there.
 */
export interface Screen {
  /** The window's inner size. */
  innerWidth: number;
  innerHeight: number;
  /** The screen's size. iOS reports it in portrait whichever way the phone is held. */
  screenWidth: number;
  screenHeight: number;
  /** Installed and opened from the home screen. */
  standalone: boolean;
  /** iOS or iPadOS, where the short viewport happens. */
  ios: boolean;
}

/** The height #root takes, in px, or null to keep the stylesheet's 100dvh. */
export function appHeight(s: Screen): number | null {
  if (!s.standalone || !s.ios) return null;
  const landscape = s.innerWidth > s.innerHeight;
  const tall = Math.max(s.screenWidth, s.screenHeight);
  const short = Math.min(s.screenWidth, s.screenHeight);
  const screen = landscape ? short : tall;
  // Only ever grow: where Safari already gets it right, the two agree.
  return screen > s.innerHeight ? screen : null;
}

function read(): Screen {
  const nav = navigator as Navigator & { standalone?: boolean };
  return {
    innerWidth: window.innerWidth,
    innerHeight: window.innerHeight,
    screenWidth: window.screen.width,
    screenHeight: window.screen.height,
    standalone: nav.standalone === true || window.matchMedia("(display-mode: standalone)").matches,
    ios:
      /iPad|iPhone|iPod/.test(nav.userAgent) ||
      (nav.platform === "MacIntel" && nav.maxTouchPoints > 1),
  };
}

/** Keeps --humagen-app-height on <html> in step with the screen. */
export function trackAppHeight(): void {
  const apply = () => {
    const h = appHeight(read());
    const root = document.documentElement.style;
    if (h === null) root.removeProperty("--humagen-app-height");
    else root.setProperty("--humagen-app-height", `${h}px`);
  };
  apply();
  window.addEventListener("resize", apply);
  window.addEventListener("orientationchange", apply);
}
