/**
 * The installed app's status bar. index.html colors it for the device's
 * scheme, one `theme-color` per `prefers-color-scheme`. A person who picks a
 * theme other than the device's gets a status bar in that theme instead, so
 * a light console never sits under a black bar. A deployment's branding that
 * sets one color of its own (a single meta without a media query) keeps it.
 */

/** The page's background in each theme, as index.html and the manifest name it. */
export const THEME_COLORS = { light: "#ffffff", dark: "#000000" } as const;

interface DocumentLike {
  querySelectorAll(selector: string): ArrayLike<{
    setAttribute(name: string, value: string): void;
  }>;
}

/**
 * Points every per-scheme theme-color meta at the theme on screen. With the
 * theme set to follow the device, that is the scheme the media query picks,
 * so the metas say what they said before.
 */
export function syncThemeColor(doc: DocumentLike, theme: "light" | "dark"): void {
  const metas = doc.querySelectorAll('meta[name="theme-color"][media]');
  for (let i = 0; i < metas.length; i++) metas[i]?.setAttribute("content", THEME_COLORS[theme]);
}
