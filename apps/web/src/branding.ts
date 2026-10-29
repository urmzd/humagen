import type { Branding } from "./api.ts";

export type { Branding };

/** The page's own name, which the server writes into index.html from the deployment's branding. */
function servedName(): string {
  if (typeof document === "undefined") return "Humagen";
  const meta = document.querySelector<HTMLMetaElement>('meta[name="application-name"]');
  return meta?.content || "Humagen";
}

/** What the console calls itself before the workspace's branding is read. */
export function servedBranding(name = servedName()): Branding {
  return {
    name,
    wordmark: name === "Humagen" ? "humagen" : name,
    title: name,
    logo: null,
    logo_alt: name,
    style: null,
  };
}

/**
 * The data-accent a person's accent choice is drawn with. workspace, and no
 * choice yet, is the workspace's branded accent, which is red without one.
 */
export function accentAttribute(accent?: string): string {
  return !accent || accent === "workspace" ? "brand" : accent;
}
